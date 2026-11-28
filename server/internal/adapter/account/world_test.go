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
		Node    string
		Account kernel.AccountID
	}
	sayArgs struct{ Name, Text string }
)

func (w *worldModule) record(p world.Presence, args any) error {
	w.p, w.args = p, args
	return nil
}

func (w *worldModule) Subscribe(string) *hub.Sub[world.Delivery] { return nil }

func (w *worldModule) Join(_ context.Context, node string, account kernel.AccountID, p world.Presence) (world.View, error) {
	return w.view, w.record(p, joinArgs{node, account})
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
	match := copytest.Match{Renames: map[string]string{"Connection": "Conn"}, Computed: map[string]string{"Seq": seqWhy}}
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
	got := copytest.Diff(copytest.Match{Renames: map[string]string{"P.Connection": "P.Conn", "Name": "Args.Name", "Text": "Args.Text"}, Computed: map[string]string{"P.Seq": seqWhy}}, func(in say) said {
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

// A join reaches the module with every argument, and the view it answers reaches the use cases with every field.
func TestAJoinKeepsItsFields(t *testing.T) {
	w := &worldModule{}
	a := NewWorld(w, slog.New(slog.DiscardHandler), nil)
	type join struct {
		Node, Conn string
		Account    kernel.AccountID
		Character  kernel.CharacterID
	}
	asked := copytest.Diff(copytest.Match{Renames: map[string]string{"Node": "Args.Node", "Account": "Args.Account", "Conn": "P.Conn", "Character": "P.Character"}, Computed: map[string]string{"P.Seq": seqWhy}},
		func(j join) atWorld[joinArgs] {
			_, _ = a.Join(t.Context(), j.Node, j.Conn, j.Account, j.Character)
			return atWorld[joinArgs]{P: w.p, Args: w.args.(joinArgs)}
		})
	if len(asked) > 0 {
		t.Errorf("asked: %v", asked)
	}
	const view = "the world's own view, handed on whole for the world's handler: checked below"
	match := copytest.Match{
		Renames: map[string]string{"ID": "Player.ID", "Name": "Player.Name", "Class": "Player.Class", "Level": "Player.Level", "HP": "Player.HP", "MaxHP": "Player.MaxHP"},
		Skip:    map[string]string{"Exp": view, "Stats": view, "Energy": view, "Sheet": view, "Quests": view, "Timers": view},
	}
	var handedOn any
	answered := copytest.Diff(match, func(v world.View) model.Joined {
		w.view = v
		out, err := a.Join(t.Context(), "", "", kernel.AccountID{}, kernel.CharacterID{})
		if err != nil {
			t.Fatal(err)
		}
		handedOn = out.View
		if !reflect.DeepEqual(handedOn, v) {
			t.Errorf("the view went on as %+v, want %+v", handedOn, v)
		}
		out.View = nil // checked above, so its fields are no copy's to find
		return out
	})
	if len(answered) > 0 {
		t.Errorf("answered: %v", answered)
	}
}

// seqWhy is why no account field sets a presence's Seq: it is the input a client message came during, which only the world's own handler stamps.
const seqWhy = "the input a client message was sent during, which the world's handler stamps and the account module never does"
