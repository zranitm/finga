package account

import (
	"context"
	"log/slog"
	"reflect"
	"testing"

	"finga/internal/application/world"
	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
	"finga/pkg/copytest"
	"finga/pkg/hub"
)

// worldModule records the last call: the presence and the rest of its arguments; Join answers with view.
type worldModule struct {
	p    world.Presence
	args any
	view world.View
}

// The arguments of each world call after its presence.
type (
	joinArgs struct {
		Gateway string
		Account kernel.AccountID
	}
	sayArgs struct{ Name, Text string }
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

func (w *worldModule) Say(_ context.Context, p world.Presence, name, text string) error {
	return w.record(p, sayArgs{name, text})
}

// Leave, Drop and Say reach the module with the player's presence, the connection included, and Say with its line.
func TestLeaveDropAndSayKeepTheirFields(t *testing.T) {
	w := &worldModule{}
	a := NewWorld(w, slog.New(slog.DiscardHandler), nil)
	match := copytest.Match{Renames: map[string]string{"Connection": "Conn"}}
	for name, call := range map[string]func(model.Presence) error{
		"Leave": func(p model.Presence) error { return a.Leave(t.Context(), p) },
		"Drop":  func(p model.Presence) error { return a.Drop(t.Context(), p) },
	} {
		got := copytest.Diff(match, func(p model.Presence) world.Presence {
			_ = call(p)
			return w.p
		})
		if len(got) > 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
	type say struct {
		P          model.Presence
		Name, Text string
	}
	type said struct {
		P    world.Presence
		Args sayArgs
	}
	got := copytest.Diff(copytest.Match{Renames: map[string]string{"P.Connection": "P.Conn", "Name": "Args.Name", "Text": "Args.Text"}}, func(in say) said {
		_ = a.Say(t.Context(), in.P, in.Name, in.Text)
		return said{w.p, w.args.(sayArgs)}
	})
	if len(got) > 0 {
		t.Errorf("Say: %v", got)
	}
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
