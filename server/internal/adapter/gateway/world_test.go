package gateway

import (
	"context"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"testing"
	"time"

	"finga/internal/application/world"
	model "finga/internal/model/gateway"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
	"finga/pkg/copytest"
	"finga/pkg/hub"
)

// worldModule records the last call: the presence and the rest of its arguments; Join answers with view, and TakeLoot with picks and err.
type worldModule struct {
	p        world.Presence
	args     any
	view     world.View
	picks    []world.PickAnswer
	channels world.Channels
	bosses   []world.AreaBoss
	err      error
}

// The arguments of each world call after its presence.
type (
	joinArgs struct {
		Gateway string
		Account kernel.AccountID
	}
	inputArgs struct {
		Seq  uint32
		In   sim.Input
		View uint32
	}
	sayArgs     struct{ Name, Text string }
	npcArgs     struct{ NPC string }
	questArgs   struct{ Quest string }
	npcQuest    struct{ NPC, Quest string }
	promoteArgs struct {
		NPC   string
		Class kernel.Class
	}
	equipArgs struct {
		Key       string
		Item      kernel.ItemID
		EquipSlot string
	}
	lookArgs struct {
		Key  string
		Look world.Look
	}
	takeArgs    struct{ Picks []world.Pick }
	unequipArgs struct {
		Key       string
		EquipSlot string
		ToSlot    *int
	}
	useArgs struct {
		Key      string
		Item     kernel.ItemID
		Template string
	}
	pointsArgs struct {
		Expected int
		Points   gamedata.Stats
	}
	skillPointsArgs struct {
		Expected int
		Points   map[string]int
	}
	traitArgs struct{ Skill, Trait string }
	skillArgs struct {
		Skill  string
		Root   string
		Aim    float32
		Target world.EntityRef
		Way    sim.Way
		Seq    uint32
	}
	teleportArgs struct {
		MapID    string
		Waypoint string
		Seq      uint32
	}
	switchArgs struct {
		Channel int
		Seq     uint32
	}
	aimArgs struct {
		Aim    float32
		Target world.EntityRef
	}
	resetArgs struct{ Expected int }
)

func (w *worldModule) record(p world.Presence, args any) error {
	w.p, w.args = p, args
	return nil
}

func (w *worldModule) Subscribe(string) *hub.Sub[world.Delivery] { return nil }

func (w *worldModule) Join(_ context.Context, gateway string, account kernel.AccountID, p world.Presence) (world.View, error) {
	return w.view, w.record(p, joinArgs{gateway, account})
}

func (w *worldModule) Leave(_ context.Context, p world.Presence) error { return w.record(p, nil) }

func (w *worldModule) Drop(_ context.Context, p world.Presence) error { return w.record(p, nil) }

func (w *worldModule) Input(_ context.Context, p world.Presence, seq uint32, in sim.Input, view uint32) error {
	return w.record(p, inputArgs{seq, in, view})
}

func (w *worldModule) Attack(_ context.Context, p world.Presence, aim float32, target world.EntityRef) error {
	return w.record(p, aimArgs{aim, target})
}

func (w *worldModule) EnterPortal(_ context.Context, p world.Presence) error {
	return w.record(p, nil)
}

func (w *worldModule) Say(_ context.Context, p world.Presence, name, text string) error {
	return w.record(p, sayArgs{name, text})
}

func (w *worldModule) Equip(_ context.Context, p world.Presence, key string, item kernel.ItemID, equipSlot string) error {
	return w.record(p, equipArgs{key, item, equipSlot})
}

func (w *worldModule) SetLook(_ context.Context, p world.Presence, key string, look world.Look) error {
	return w.record(p, lookArgs{key, look})
}

func (w *worldModule) TakeLoot(_ context.Context, p world.Presence, picks []world.Pick) ([]world.PickAnswer, error) {
	_ = w.record(p, takeArgs{picks})
	return w.picks, w.err
}

func (w *worldModule) UseItem(_ context.Context, p world.Presence, key string, item kernel.ItemID, template string) error {
	return w.record(p, useArgs{key, item, template})
}

func (w *worldModule) Unequip(_ context.Context, p world.Presence, key, equipSlot string, toSlot *int) error {
	return w.record(p, unequipArgs{key, equipSlot, toSlot})
}

func (w *worldModule) SpendPoints(_ context.Context, p world.Presence, expected int, points gamedata.Stats) error {
	return w.record(p, pointsArgs{expected, points})
}

func (w *worldModule) ResetPoints(_ context.Context, p world.Presence, expected int) error {
	return w.record(p, resetArgs{expected})
}

func (w *worldModule) SpendSkillPoints(_ context.Context, p world.Presence, expected int, points map[string]int) error {
	return w.record(p, skillPointsArgs{expected, points})
}

func (w *worldModule) ChooseTrait(_ context.Context, p world.Presence, skill, trait string) error {
	return w.record(p, traitArgs{skill, trait})
}

func (w *worldModule) UseSkill(_ context.Context, p world.Presence, use world.SkillUse, seq uint32) error {
	return w.record(p, skillArgs{use.Skill, use.Root, use.Aim, use.Target, use.Way, seq})
}

func (w *worldModule) Teleport(_ context.Context, p world.Presence, mapID, waypoint string, seq uint32) error {
	return w.record(p, teleportArgs{mapID, waypoint, seq})
}

func (w *worldModule) SwitchChannel(_ context.Context, p world.Presence, n int, seq uint32) error {
	return w.record(p, switchArgs{n, seq})
}

func (w *worldModule) Channels(_ context.Context, p world.Presence) (world.Channels, error) {
	_ = w.record(p, nil)
	return w.channels, w.err
}

func (w *worldModule) Bosses(_ context.Context, p world.Presence) ([]world.AreaBoss, error) {
	_ = w.record(p, nil)
	return w.bosses, w.err
}

func (w *worldModule) Promote(_ context.Context, p world.Presence, npc string, class kernel.Class) error {
	return w.record(p, promoteArgs{npc, class})
}

func (w *worldModule) Talk(_ context.Context, p world.Presence, npc string) error {
	return w.record(p, npcArgs{npc})
}

func (w *worldModule) AcceptQuest(_ context.Context, p world.Presence, npc, quest string) error {
	return w.record(p, npcQuest{npc, quest})
}

func (w *worldModule) AbandonQuest(_ context.Context, p world.Presence, quest string) error {
	return w.record(p, questArgs{quest})
}

func (w *worldModule) HandInQuest(_ context.Context, p world.Presence, npc, quest string) error {
	return w.record(p, npcQuest{npc, quest})
}

func (w *worldModule) Forfeit(_ context.Context, p world.Presence) error {
	return w.record(p, struct{}{})
}

// toWorld is what a use case hands the world adapter, and atWorld what the module gets.
type (
	toWorld[In any] struct {
		P  model.Presence
		In In
	}
	atWorld[Args any] struct {
		P    world.Presence
		Args Args
	}
)

// worldCall diffs one world call: call makes it with what the use case hands over, and the module's arguments are Args.
func worldCall[In, Args any](w *worldModule, renames map[string]string, call func(model.Presence, In) error) []string {
	match := copytest.Match{Renames: map[string]string{"P.Connection": "P.Conn", "In": "Args"}}
	maps.Copy(match.Renames, renames)
	return copytest.Diff(match, func(in toWorld[In]) atWorld[Args] {
		_ = call(in.P, in.In)
		args, _ := w.args.(Args)
		return atWorld[Args]{P: w.p, Args: args}
	})
}

// A take's picks come back in their order, each refusal as its public failure; a take refused whole is the call's failure.
func TestATakesPicksComeBackWithTheirRefusals(t *testing.T) {
	w := &worldModule{picks: []world.PickAnswer{{Pouch: 3, Item: 1}, {Pouch: 3, Item: 2, Err: kernel.ErrUseTabFull}}}
	a := NewWorld(w, slog.New(slog.DiscardHandler), nil)
	got, err := a.TakeLoot(t.Context(), model.Presence{}, []model.LootPick{{Pouch: 3, Item: 1}, {Pouch: 3, Item: 2}})
	if err != nil || len(got) != 2 || got[0].LootPick != (model.LootPick{Pouch: 3, Item: 1}) || got[0].Err != nil || model.PublicOf(got[1].Err) != model.UseTabFull {
		t.Fatalf("answers %+v (%v); want the first taken and the second refused for its tab", got, err)
	}
	w.picks, w.err = nil, kernel.ErrCharacterDead
	if _, err := a.TakeLoot(t.Context(), model.Presence{}, nil); model.PublicOf(err) != model.CharacterDead {
		t.Fatalf("a dead player's take failed with %v, want CHARACTER_DEAD", err)
	}
}

// Each world call reaches the module with the player's presence and every field of its request.
func TestEachWorldRequestKeepsItsFields(t *testing.T) {
	w := &worldModule{}
	a := NewWorld(w, slog.New(slog.DiscardHandler), nil)
	ctx := t.Context()
	type (
		input struct {
			Seq  uint32
			Keys model.Keys
			View uint32
		}
		say   struct{ Name, Text string }
		skill struct {
			Skill     string
			ChainRoot string
			Aim       float32
			Target    model.Target
			Way       model.Way
			Seq       uint32
		}
		teleport struct {
			MapID    string
			Waypoint string
			Seq      uint32
		}
		switched struct {
			Channel int
			Seq     uint32
		}
		aimed struct {
			Aim    float32
			Target model.Target
		}
		expected  struct{ Expected int }
		npc       struct{ NPC string }
		quest     struct{ Quest string }
		promotion struct{ NPC, Class string }
	)
	key := map[string]string{"In.IdempotencyKey": "Args.Key"}
	for name, diff := range map[string]func() []string{
		"Leave": func() []string {
			return worldCall[struct{}, struct{}](w, nil, func(p model.Presence, _ struct{}) error { return a.Leave(ctx, p) })
		},
		"Drop": func() []string {
			return worldCall[struct{}, struct{}](w, nil, func(p model.Presence, _ struct{}) error { return a.Drop(ctx, p) })
		},
		"Input": func() []string {
			return worldCall[input, inputArgs](w, map[string]string{"In.Keys": "Args.In"}, func(p model.Presence, in input) error {
				return a.Input(ctx, p, in.Seq, in.Keys, in.View)
			})
		},
		"Attack": func() []string {
			return worldCall[aimed, aimArgs](w, map[string]string{"In.Target.Character": "Args.Target.Player"}, func(p model.Presence, in aimed) error { return a.Attack(ctx, p, in.Aim, in.Target) })
		},
		"EnterPortal": func() []string {
			return worldCall[struct{}, struct{}](w, nil, func(p model.Presence, _ struct{}) error { return a.EnterPortal(ctx, p) })
		},
		"Forfeit": func() []string {
			return worldCall[struct{}, struct{}](w, nil, func(p model.Presence, _ struct{}) error { return a.Forfeit(ctx, p) })
		},
		"Equip": func() []string {
			return worldCall[model.ItemEquip, equipArgs](w, key, func(p model.Presence, in model.ItemEquip) error { return a.Equip(ctx, p, in) })
		},
		"SetLook": func() []string {
			return worldCall[model.LookSet, lookArgs](w, key, func(p model.Presence, in model.LookSet) error { return a.SetLook(ctx, p, in) })
		},
		"TakeLoot": func() []string {
			type take struct{ Picks []model.LootPick }
			return worldCall[take, takeArgs](w, nil, func(p model.Presence, in take) error {
				_, err := a.TakeLoot(ctx, p, in.Picks)
				return err
			})
		},
		"Unequip": func() []string {
			return worldCall[model.ItemUnequip, unequipArgs](w, key, func(p model.Presence, in model.ItemUnequip) error { return a.Unequip(ctx, p, in) })
		},
		"UseItem": func() []string {
			return worldCall[model.ItemUse, useArgs](w, key, func(p model.Presence, in model.ItemUse) error { return a.UseItem(ctx, p, in) })
		},
		"SpendPoints": func() []string {
			return worldCall[model.PointsSpend, pointsArgs](w, map[string]string{"In.ExpectedUnspent": "Args.Expected"}, func(p model.Presence, in model.PointsSpend) error {
				return a.SpendPoints(ctx, p, in)
			})
		},
		"SpendSkillPoints": func() []string {
			return worldCall[model.SkillPointsSpend, skillPointsArgs](w, map[string]string{"In.ExpectedUnspent": "Args.Expected"}, func(p model.Presence, in model.SkillPointsSpend) error {
				return a.SpendSkillPoints(ctx, p, in)
			})
		},
		"ChooseTrait": func() []string {
			return worldCall[model.TraitChoice, traitArgs](w, nil, func(p model.Presence, in model.TraitChoice) error { return a.ChooseTrait(ctx, p, in) })
		},
		"UseSkill": func() []string {
			return worldCall[skill, skillArgs](w, map[string]string{"In.ChainRoot": "Args.Root", "In.Target.Character": "Args.Target.Player"}, func(p model.Presence, in skill) error {
				return a.UseSkill(ctx, p, model.SkillUse{Skill: in.Skill, ChainRoot: in.ChainRoot, Aim: in.Aim, Target: in.Target, Way: in.Way}, in.Seq)
			})
		},
		"Teleport": func() []string {
			return worldCall[teleport, teleportArgs](w, nil, func(p model.Presence, in teleport) error { return a.Teleport(ctx, p, in.MapID, in.Waypoint, in.Seq) })
		},
		"SwitchChannel": func() []string {
			return worldCall[switched, switchArgs](w, nil, func(p model.Presence, in switched) error { return a.SwitchChannel(ctx, p, in.Channel, in.Seq) })
		},
		"Channels": func() []string {
			return worldCall[struct{}, struct{}](w, nil, func(p model.Presence, _ struct{}) error {
				_, err := a.Channels(ctx, p)
				return err
			})
		},
		"Bosses": func() []string {
			return worldCall[struct{}, struct{}](w, nil, func(p model.Presence, _ struct{}) error {
				_, err := a.Bosses(ctx, p)
				return err
			})
		},
		"ResetPoints": func() []string {
			return worldCall[expected, resetArgs](w, nil, func(p model.Presence, in expected) error { return a.ResetPoints(ctx, p, in.Expected) })
		},
		"Say": func() []string {
			return worldCall[say, sayArgs](w, nil, func(p model.Presence, in say) error { return a.Say(ctx, p, in.Name, in.Text) })
		},
		"Talk": func() []string {
			return worldCall[npc, npcArgs](w, nil, func(p model.Presence, in npc) error { return a.Talk(ctx, p, in.NPC) })
		},
		"Promote": func() []string {
			return worldCall[promotion, promoteArgs](w, nil, func(p model.Presence, in promotion) error { return a.Promote(ctx, p, in.NPC, in.Class) })
		},
		"AcceptQuest": func() []string {
			return worldCall[model.QuestAsk, npcQuest](w, nil, func(p model.Presence, in model.QuestAsk) error { return a.AcceptQuest(ctx, p, in) })
		},
		"AbandonQuest": func() []string {
			return worldCall[quest, questArgs](w, nil, func(p model.Presence, in quest) error { return a.AbandonQuest(ctx, p, in.Quest) })
		},
		"HandInQuest": func() []string {
			return worldCall[model.QuestAsk, npcQuest](w, nil, func(p model.Presence, in model.QuestAsk) error { return a.HandInQuest(ctx, p, in) })
		},
	} {
		if got := diff(); len(got) > 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// Unequip hands the module its own copy of the slot asked for, so the request can't change it after.
func TestUnequipCopiesItsSlot(t *testing.T) {
	w := &worldModule{}
	slot := 3
	if err := NewWorld(w, slog.New(slog.DiscardHandler), nil).Unequip(t.Context(), model.Presence{}, model.ItemUnequip{ToSlot: &slot}); err != nil {
		t.Fatal(err)
	}
	if got := w.args.(unequipArgs).ToSlot; got == &slot || *got != 3 {
		t.Fatalf("slot %p (%d), the request's %p", got, *got, &slot)
	}
	if err := NewWorld(w, slog.New(slog.DiscardHandler), nil).Unequip(t.Context(), model.Presence{}, model.ItemUnequip{}); err != nil || w.args.(unequipArgs).ToSlot != nil {
		t.Fatalf("no slot asked for became %v (%v)", w.args, err)
	}
}

// joinedRenames are a world.View's fields under the Joined's names.
var joinedRenames = map[string]string{
	"ID": "Character.ID", "Name": "Character.Name", "Class": "Character.Class", "Level": "Character.Level", "Exp": "Character.Exp",
	"Stats": "Character.Stats", "HP": "Character.HP", "MaxHP": "Character.MaxHP", "Energy": "Character.Energy",
	"Sheet": "Character.Sheet", "Sheet.Skills.Points": "Character.Sheet.Skills.Invested",
	"Quests.key": "Quests.Quest", "Quests.value": "Quests",
}

// A join reaches the module with every argument, and the view it answers reaches the use cases with every field.
func TestAJoinKeepsItsFields(t *testing.T) {
	w := &worldModule{}
	a := NewWorld(w, slog.New(slog.DiscardHandler), nil)
	type join struct {
		Gateway, Conn string
		Account       kernel.AccountID
		Character     kernel.CharacterID
	}
	asked := copytest.Diff(copytest.Match{Renames: map[string]string{"Gateway": "Args.Gateway", "Account": "Args.Account", "Conn": "P.Conn", "Character": "P.Character"}},
		func(j join) atWorld[joinArgs] {
			_, _ = a.Join(t.Context(), j.Gateway, j.Conn, j.Account, j.Character)
			return atWorld[joinArgs]{P: w.p, Args: w.args.(joinArgs)}
		})
	if len(asked) > 0 {
		t.Errorf("asked: %v", asked)
	}
	match := copytest.Match{
		Renames: joinedRenames,
		Skip: map[string]string{
			"Timers":                     "time left at now: TestAWelcomeCarriesTheSkillTimersTheWorldJoinedWith (ws) pins timersLeft",
			"Sheet.Points.FreeResetUsed": "the sheet says whether the reset is left: TestTheSheetSaysWhetherTheFreeResetIsLeft",
		},
		Computed: map[string]string{"Character.Sheet.Points.FreeResetLeft": "FreeResetUsed negated: TestTheSheetSaysWhetherTheFreeResetIsLeft"},
		Enums: map[reflect.Type]any{
			reflect.TypeFor[world.QuestStatus](): world.QuestFinished,
			reflect.TypeFor[model.QuestStatus](): model.QuestFinished,
		},
	}
	answered := copytest.Diff(match, func(v world.View) model.Joined {
		w.view = v
		out, err := a.Join(t.Context(), "", "", kernel.AccountID{}, kernel.CharacterID{})
		if err != nil {
			t.Fatal(err)
		}
		return out
	})
	if len(answered) > 0 {
		t.Errorf("answered: %v", answered)
	}
}

// The sheet says the free reset is left until it is used.
func TestTheSheetSaysWhetherTheFreeResetIsLeft(t *testing.T) {
	for used, left := range map[bool]bool{false: true, true: false} {
		if got := statSheet(world.Sheet{Points: world.Points{FreeResetUsed: used}}).Points.FreeResetLeft; got != left {
			t.Errorf("used %v: left %v", used, got)
		}
	}
}

// The channel list the module answers reaches the use cases with every field.
// A boss list keeps each area's boss, its time back in whole milliseconds.
func TestABossListKeepsItsFields(t *testing.T) {
	w := &worldModule{bosses: []world.AreaBoss{{Area: "west", BackIn: 1500 * time.Millisecond}, {Area: "east", Up: true}}}
	got, err := NewWorld(w, slog.New(slog.DiscardHandler), nil).Bosses(t.Context(), model.Presence{})
	if want := []model.AreaBoss{{Area: "west", BackInMs: 1500}, {Area: "east", Up: true}}; err != nil || !slices.Equal(got, want) {
		t.Fatalf("%+v, %v; want %+v", got, err, want)
	}
}

func TestAChannelListKeepsItsFields(t *testing.T) {
	if got := copytest.Diff(copytest.Match{}, func(in world.Channels) model.Channels {
		w := &worldModule{channels: in}
		out, err := NewWorld(w, slog.New(slog.DiscardHandler), nil).Channels(t.Context(), model.Presence{})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}); len(got) > 0 {
		t.Fatal(got)
	}
}
