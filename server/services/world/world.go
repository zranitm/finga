// Package world is the world module's facade: the maps, the players on them and each gateway's stream; its types and kernel errors are the module's contract.
package world

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	"finga/internal/facade"
	"finga/internal/gamedata"
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/pkg/hub"
	"finga/pkg/panics"
	"finga/services/world/config"
	"finga/services/world/internal/application/announce"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/application/itemcalls"
	"finga/services/world/internal/application/loot"
	"finga/services/world/internal/application/party"
	game "finga/services/world/internal/application/world"
	"finga/services/world/internal/clients"
	"finga/services/world/internal/local"
	"finga/services/world/internal/metrics"
	"finga/services/world/internal/model"
)

// Module is the world module; every call to it goes through these methods.
type Module struct {
	calls    *facade.Calls
	svc      service
	deliver  *hub.Hub[Delivery]
	tickRate int
}

// service is the module's application, as the facade calls it.
type service interface {
	Join(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, conn string, sink instance.Sink) (model.View, error)
	Leave(ctx context.Context, id kernel.CharacterID, conn string) error
	Offline(ctx context.Context, id kernel.CharacterID, do func(ctx context.Context) error) (bool, error)
	Input(id kernel.CharacterID, conn string, seq uint32, in sim.Input, view uint32) error
	Attack(id kernel.CharacterID, conn string, seq uint32) error
	EnterPortal(id kernel.CharacterID, conn string) error
	Say(id kernel.CharacterID, conn, name, text string) error
	Equip(ctx context.Context, id kernel.CharacterID, conn, key string, item kernel.ItemID, equipSlot string) error
	UseItem(ctx context.Context, id kernel.CharacterID, conn, key string, item kernel.ItemID, template string) error
	Unequip(ctx context.Context, id kernel.CharacterID, conn, key, equipSlot string, toSlot *int) error
	SpendPoints(ctx context.Context, id kernel.CharacterID, conn string, expected int, points gamedata.Stats) error
	ResetPoints(ctx context.Context, id kernel.CharacterID, conn string, expected int) error
	SpendSkillPoints(ctx context.Context, id kernel.CharacterID, conn string, expected int, points map[string]int) error
	UseSkill(ctx context.Context, id kernel.CharacterID, conn, skill string, seq uint32) error
	Promote(ctx context.Context, id kernel.CharacterID, conn, npc string, class kernel.Class) error
	Talk(ctx context.Context, id kernel.CharacterID, conn, npc string) error
	AcceptQuest(ctx context.Context, id kernel.CharacterID, conn, npc, quest string) error
	AbandonQuest(ctx context.Context, id kernel.CharacterID, conn, quest string) error
	HandInQuest(ctx context.Context, id kernel.CharacterID, conn, npc, quest string) error
}

// Deps are what the module is built from; the other modules come over gRPC, in Remote, or in the process, in Local.
type Deps struct {
	Module *host.Module // its logs, panics, tasks, metrics and traces
	Config config.Config
	Data   *gamedata.Data
	ID     string // random per process start: the party copy watches under it
	Remote Remote
	Local  Local
}

// Remote are the other modules the world calls, over gRPC.
type Remote struct {
	Character characterv1.CharacterServiceClient
	Item      itemv1.ItemServiceClient
	Social    socialv1.SocialServiceClient
}

// Local are the other modules the world calls, in the same process.
type Local struct {
	Character local.CharacterModule
	Item      local.ItemModule
	Social    local.SocialModule
}

// ports are the other modules as the world's application calls them.
type ports struct {
	social interface {
		party.Social
		announce.Social
	}
	equipment interface {
		instance.Equipment
		instance.HandIns
		instance.Gifts
		instance.Potions
		game.HandedIns
		game.Wardrobe
	}
	loot       loot.Items
	saver      game.Saver
	characters game.Characters
	starter    game.Starter
}

// portsOf adapts the other modules over gRPC or in the process, whichever d has.
func (d Deps) portsOf(rec *panics.Recorder) (ports, error) {
	remote, allRemote := given(d.Remote.Character, d.Remote.Item, d.Remote.Social)
	inProcess, allLocal := given(d.Local.Character, d.Local.Item, d.Local.Social)
	switch {
	case remote == inProcess, remote && !allRemote, inProcess && !allLocal:
		return ports{}, errors.New("world: give all of Remote or all of Local")
	case inProcess:
		l, equipment := d.Local, local.NewEquipment(d.Local.Item)
		return ports{
			social: local.NewSocial(l.Social, rec), equipment: equipment, loot: local.NewLoot(l.Item),
			saver: local.NewSaver(l.Character), characters: local.NewCharacters(l.Character), starter: local.NewStarter(l.Item),
		}, nil
	default:
		r := d.Remote
		return ports{
			social: clients.NewSocial(r.Social, rec), equipment: clients.NewEquipment(r.Item), loot: clients.NewLoot(r.Item),
			saver: clients.NewSaver(r.Character), characters: clients.NewCharacters(r.Character), starter: clients.NewStarter(r.Item),
		}, nil
	}
}

// given reports whether any of the modules is set, and whether all are.
func given(modules ...any) (anySet, allSet bool) {
	allSet = true
	for _, m := range modules {
		anySet = anySet || m != nil
		allSet = allSet && m != nil
	}
	return anySet, allSet
}

// New builds the maps and adds their tasks; it refuses a tick rate the movement or an action can't keep to.
func New(_ context.Context, d Deps) (*Module, error) {
	m, cfg, data := d.Module, d.Config, d.Data
	if err := wholeSteps(cfg.TickRateHz, data.Movement().Dt); err != nil {
		return nil, err
	}
	if err := actionsLastATick(data, cfg.TickRateHz, cfg.TickInterval()); err != nil {
		return nil, err
	}
	m.TraceRoots(loot.EntryPoint)
	log, rec := m.Logger, m.Panics
	meters, err := metrics.New(m.Registry(), game.DropReasons())
	if err != nil {
		return nil, fmt.Errorf("metrics: %w", err)
	}
	others, err := d.portsOf(rec)
	if err != nil {
		return nil, err
	}
	parties := party.NewCopy(party.Deps{Social: others.social, Logger: log}, party.Options{World: d.ID})
	notices := announce.New(announce.Deps{Social: others.social, Logger: log})
	equipment := others.equipment
	itemCalls := itemcalls.NewPool(itemcalls.Deps{Observer: meters, Panics: rec}, itemcalls.Options{Workers: cfg.ItemCalls.Workers})
	maps := game.New(game.Deps{
		Data:         data,
		NewRoll:      newRoll,
		Observer:     meters,
		Items:        others.loot,
		Batches:      meters,
		Equipment:    equipment,
		HandIns:      equipment,
		Gifts:        equipment,
		Potions:      equipment,
		HandedIns:    equipment,
		Wardrobe:     equipment,
		ItemCalls:    itemCalls,
		Parties:      parties,
		Notices:      notices,
		Traces:       m.Traces(),
		Saver:        others.saver,
		Characters:   others.characters,
		Starter:      others.starter,
		Panics:       rec,
		Logger:       log,
		SaveObserver: meters,
	}, game.Tuning{TickInterval: cfg.TickInterval(), CheckpointInterval: cfg.CheckpointInterval, FlushTimeout: cfg.FlushTimeout, BatchMax: cfg.ItemCalls.BatchMax})
	m.Task("parties", parties.Run, host.Restartable)
	m.Task("announcements", notices.Run, host.Restartable)
	m.Task("world", maps.Run, host.Vital)
	m.Task("saves", maps.Save, host.Vital)
	m.Task("item calls", itemCalls.Run, host.Vital)
	calls, err := m.Calls(errorTable)
	if err != nil {
		return nil, err
	}
	return &Module{calls: calls, svc: maps, deliver: hub.New[Delivery](deliverQueue), tickRate: cfg.TickRateHz}, nil
}

// wholeSteps refuses a rate whose tick splits a movement step, as the client predicts in whole steps; the rate, unlike the interval, is exact.
func wholeSteps(hz int, dt float64) error {
	steps := 1 / (float64(hz) * dt)
	if steps < 1 || math.Abs(steps-math.Round(steps)) > 1e-9 {
		return fmt.Errorf("tick_rate_hz: a %d Hz tick holds %.3g movement steps of %.4gs; it must hold a whole number", hz, steps, dt)
	}
	return nil
}

// actionsLastATick refuses a tick that outlasts an action: a basic attack or an active skill takes at least a tick, so its points fall inside it.
func actionsLastATick(data *gamedata.Data, hz int, tick time.Duration) error {
	for _, c := range data.Classes() {
		if c.Attack.Length < tick {
			return fmt.Errorf("tick_rate_hz: a %d Hz tick lasts %s, longer than class %s's %s attack; every action must last at least a tick", hz, tick, c.ID, c.Attack.Length)
		}
	}
	for _, s := range data.Skills() {
		for _, st := range s.Stages {
			if s.Kind == gamedata.Active && st.Length < tick {
				return fmt.Errorf("tick_rate_hz: a %d Hz tick lasts %s, longer than skill %s's %s at %s; every action must last at least a tick", hz, tick, s.ID, st.Length, st.Name)
			}
		}
	}
	return nil
}

// newRoll is a map's random source, seeded afresh: combat and drops are a game's chance, not a secret.
func newRoll() model.Roll {
	return rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())) //nolint:gosec // game randomness
}

// Join joins the character to its map through the gateway's stream, which must be open; the Snapshot follows on it.
func (m *Module) Join(ctx context.Context, gateway string, account kernel.AccountID, p Presence) (_ View, err error) {
	ctx, call := m.calls.Start(ctx, "Join")
	defer call.End(&err)
	sub, ok := m.deliver.Get(gateway)
	if !ok {
		return View{}, kernel.ErrNotSubscribed
	}
	v, err := m.svc.Join(ctx, account, p.Character, p.Conn, sink{sub: sub, character: p.Character, conn: p.Conn})
	if err != nil {
		return View{}, err
	}
	view := viewOf(v)
	view.TickRateHz = m.tickRate
	return view, nil
}

// Leave removes the player if it joined through the presence's connection, once its map has.
func (m *Module) Leave(ctx context.Context, p Presence) (err error) {
	ctx, call := m.calls.Start(ctx, "Leave")
	defer call.End(&err)
	return m.svc.Leave(ctx, p.Character, p.Conn)
}

// Offline runs do if the character is on no map and no save of it is pending, holding it so no join starts before do returns; ran is false, and do not run, while the world holds the character. A write the world would overwrite with its next save goes through it.
func (m *Module) Offline(ctx context.Context, id kernel.CharacterID, do func(ctx context.Context) error) (ran bool, err error) {
	ctx, call := m.calls.Start(ctx, "Offline")
	defer call.End(&err)
	return m.svc.Offline(ctx, id, do)
}

// Input queues the held keys of input seq, with the view the client last applied.
func (m *Module) Input(ctx context.Context, p Presence, seq uint32, in sim.Input, view uint32) (err error) {
	_, call := m.calls.Start(ctx, "Input")
	defer call.End(&err)
	return m.svc.Input(p.Character, p.Conn, seq, in, view)
}

// Attack queues a basic attack.
func (m *Module) Attack(ctx context.Context, p Presence, seq uint32) (err error) {
	_, call := m.calls.Start(ctx, "Attack")
	defer call.End(&err)
	return m.svc.Attack(p.Character, p.Conn, seq)
}

// EnterPortal takes the portal the character stands in.
func (m *Module) EnterPortal(ctx context.Context, p Presence) (err error) {
	_, call := m.calls.Start(ctx, "EnterPortal")
	defer call.End(&err)
	return m.svc.EnterPortal(p.Character, p.Conn)
}

// Say is a map chat line for everyone on the map.
func (m *Module) Say(ctx context.Context, p Presence, name, text string) (err error) {
	_, call := m.calls.Start(ctx, "Say")
	defer call.End(&err)
	return m.svc.Say(p.Character, p.Conn, name, text)
}

// Equip wears an item and answers once its stats apply.
func (m *Module) Equip(ctx context.Context, p Presence, key string, item kernel.ItemID, equipSlot string) (err error) {
	ctx, call := m.calls.Start(ctx, "Equip")
	defer call.End(&err)
	return m.svc.Equip(ctx, p.Character, p.Conn, key, item, equipSlot)
}

// UseItem drinks a potion and answers once its HP applies.
func (m *Module) UseItem(ctx context.Context, p Presence, key string, item kernel.ItemID, template string) (err error) {
	ctx, call := m.calls.Start(ctx, "UseItem")
	defer call.End(&err)
	return m.svc.UseItem(ctx, p.Character, p.Conn, key, item, template)
}

// Unequip puts a worn item back in the Equip tab, at toSlot if set, and answers once the stats apply.
func (m *Module) Unequip(ctx context.Context, p Presence, key, equipSlot string, toSlot *int) (err error) {
	ctx, call := m.calls.Start(ctx, "Unequip")
	defer call.End(&err)
	return m.svc.Unequip(ctx, p.Character, p.Conn, key, equipSlot, toSlot)
}

// SpendPoints adds unspent points to the stats, if expected are unspent, and answers once the new stats apply.
func (m *Module) SpendPoints(ctx context.Context, p Presence, expected int, points gamedata.Stats) (err error) {
	ctx, call := m.calls.Start(ctx, "SpendPoints")
	defer call.End(&err)
	return m.svc.SpendPoints(ctx, p.Character, p.Conn, expected, points)
}

// ResetPoints returns every spent point to unspent with the free reset, if expected are unspent, and answers once the new stats apply.
func (m *Module) ResetPoints(ctx context.Context, p Presence, expected int) (err error) {
	ctx, call := m.calls.Start(ctx, "ResetPoints")
	defer call.End(&err)
	return m.svc.ResetPoints(ctx, p.Character, p.Conn, expected)
}

// SpendSkillPoints invests unspent skill points, if expected are unspent, and answers once the new sheet applies.
func (m *Module) SpendSkillPoints(ctx context.Context, p Presence, expected int, points map[string]int) (err error) {
	ctx, call := m.calls.Start(ctx, "SpendSkillPoints")
	defer call.End(&err)
	return m.svc.SpendSkillPoints(ctx, p.Character, p.Conn, expected, points)
}

// UseSkill uses an active skill with input seq and answers once the tick resolves it.
func (m *Module) UseSkill(ctx context.Context, p Presence, skill string, seq uint32) (err error) {
	ctx, call := m.calls.Start(ctx, "UseSkill")
	defer call.End(&err)
	return m.svc.UseSkill(ctx, p.Character, p.Conn, skill, seq)
}

// Promote moves the character to the class the NPC within reach trains and answers once it applies.
func (m *Module) Promote(ctx context.Context, p Presence, npc string, class kernel.Class) (err error) {
	ctx, call := m.calls.Start(ctx, "Promote")
	defer call.End(&err)
	return m.svc.Promote(ctx, p.Character, p.Conn, npc, class)
}

// Talk sends the character the dialog of an NPC within reach, and answers once it is queued.
func (m *Module) Talk(ctx context.Context, p Presence, npc string) (err error) {
	ctx, call := m.calls.Start(ctx, "Talk")
	defer call.End(&err)
	return m.svc.Talk(ctx, p.Character, p.Conn, npc)
}

// AcceptQuest takes a quest the NPC within reach gives, and answers once it is in the log.
func (m *Module) AcceptQuest(ctx context.Context, p Presence, npc, quest string) (err error) {
	ctx, call := m.calls.Start(ctx, "AcceptQuest")
	defer call.End(&err)
	return m.svc.AcceptQuest(ctx, p.Character, p.Conn, npc, quest)
}

// AbandonQuest drops an active quest and its progress.
func (m *Module) AbandonQuest(ctx context.Context, p Presence, quest string) (err error) {
	ctx, call := m.calls.Start(ctx, "AbandonQuest")
	defer call.End(&err)
	return m.svc.AbandonQuest(ctx, p.Character, p.Conn, quest)
}

// HandInQuest hands a ready quest in, and answers once the item module's answer applies.
func (m *Module) HandInQuest(ctx context.Context, p Presence, npc, quest string) (err error) {
	ctx, call := m.calls.Start(ctx, "HandInQuest")
	defer call.End(&err)
	return m.svc.HandInQuest(ctx, p.Character, p.Conn, npc, quest)
}
