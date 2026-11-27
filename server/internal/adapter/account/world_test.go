package account

import (
	"context"
	"log/slog"
	"reflect"
	"testing"

	"finga/internal/application/world"
	model "finga/internal/model/account"
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

// atWorld is what the module gets: the presence and the arguments after it.
type atWorld[Args any] struct {
	P    world.Presence
	Args Args
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
