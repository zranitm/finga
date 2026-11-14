package item

import (
	"maps"
	"reflect"
	"testing"

	"finga/internal/kernel"
	"finga/pkg/copytest"
	"finga/pkg/uuidv7"
	"finga/services/item/internal/application"
	"finga/services/item/internal/model"
)

// owners skips each item's owner below the result at prefix: callers asked as that owner, so it doesn't come back.
func owners(prefix string) map[string]string {
	const why = "the owner is the actor or character asked for"
	return map[string]string{prefix + "Changed.Item.Owner": why, prefix + "Equipment.Item.Owner": why, prefix + "Worn.Owner": why}
}

// causes fill each request's cause with a valid one; causeTypes' own test maps the rest.
var causes = map[reflect.Type]any{reflect.TypeFor[CauseType](): CauseDrop, reflect.TypeFor[model.CauseType](): model.CauseDrop}

type (
	// asked is a request as callers send it, and sent as it reaches the module.
	asked[Req any] struct {
		Actor Actor
		Req   Req
	}
	sent[In any] struct {
		Actor model.Actor
		In    In
	}
	// handedInAsked is HandedIn's question as callers ask it, and handedInSent as it reaches the module.
	handedInAsked struct {
		Owner kernel.CharacterID
		Asked []Completion
	}
	handedInSent struct {
		Owner kernel.CharacterID
		Asked []application.Completion
	}
)

// Each conversion between the facade and the module carries every field, so one added on both sides and missed in between shows.
func TestEveryConversionKeepsEveryField(t *testing.T) {
	ctx := t.Context()
	f := &fakeService{}
	m := newModule(t, f)
	results := map[string]func() (Result, error){
		"Add":          func() (Result, error) { return m.Add(ctx, actor, AddRequest{Key: key, Cause: Cause{Type: CauseDrop}}) },
		"Move":         func() (Result, error) { return m.Move(ctx, actor, MoveRequest{Key: key}) },
		"Discard":      func() (Result, error) { return m.Discard(ctx, actor, DiscardRequest{Key: key}) },
		"DiscardMany":  func() (Result, error) { return m.DiscardMany(ctx, actor, DiscardManyRequest{Key: key}) },
		"Use":          func() (Result, error) { res, _, err := m.Use(ctx, actor, UseRequest{Key: key}); return res, err },
		"GrantStarter": func() (Result, error) { return m.GrantStarter(ctx, actor) },
		"Equip":        func() (Result, error) { return m.Equip(ctx, actor, EquipRequest{Key: key}) },
		"Unequip":      func() (Result, error) { return m.Unequip(ctx, actor, UnequipRequest{Key: key}) },
		"HandIn":       func() (Result, error) { return m.HandIn(ctx, actor, HandInRequest{}) },
	}
	for name, call := range results {
		got := copytest.Diff(copytest.Match{Skip: owners("")}, func(res application.Result) Result {
			f.result = res
			out, err := call()
			if err != nil {
				t.Fatal(err)
			}
			return out
		})
		if len(got) > 0 {
			t.Errorf("%s's result: %v", name, got)
		}
	}

	const keyCheck = "a valid key stands in for the fill, as checkKey refuses others; each call checks it reached the module as sent"
	keys := map[string]string{"Req.Key": keyCheck}
	for name, diff := range map[string]func() []string{
		"Inventory": func() []string {
			return copytest.Diff(copytest.Match{Skip: map[string]string{"Items.Owner": "the owner is the character asked for"}}, func(inv model.Inventory) Inventory {
				f.inventory = inv
				out, err := m.Inventory(ctx, actor.Character)
				if err != nil || f.owner != actor.Character {
					t.Fatalf("asked for %v's inventory, err %v", f.owner, err)
				}
				return out
			})
		},
		"Add's request": func() []string {
			match := copytest.Match{
				Renames:  map[string]string{"Req": "In"},
				Skip:     keys,
				Computed: map[string]string{"In.Key": keyCheck, "In.Cause.Quest": "only a hand-in's cause names a quest, which no Add sends"},
				Enums:    causes,
			}
			return copytest.Diff(match, func(a asked[AddRequest]) sent[application.AddInput] {
				a.Req.Key = key
				if _, err := m.Add(ctx, a.Actor, a.Req); err != nil {
					t.Fatal(err)
				}
				in := f.in.(application.AddInput)
				if in.Key != key {
					t.Errorf("Add sent key %q, not %q", in.Key, key)
				}
				return sent[application.AddInput]{f.actor, in}
			})
		},
		"AddMany's drops": func() []string {
			match := copytest.Match{
				Renames:  map[string]string{"Req": "In"},
				Skip:     keys,
				Computed: map[string]string{"In.Key": keyCheck, "In.Cause.Quest": "only a hand-in's cause names a quest, which no drop sends"},
				Enums:    causes,
			}
			return copytest.Diff(match, func(a asked[[]AddRequest]) sent[[]application.AddInput] {
				want := make([]string, len(a.Req))
				for i := range a.Req {
					want[i] = "k-" + uuidv7.NewString()
					a.Req[i].Key = want[i]
				}
				if _, err := m.AddMany(ctx, a.Actor, a.Req); err != nil {
					t.Fatal(err)
				}
				for i, in := range f.drops {
					if in.Key != want[i] {
						t.Errorf("drop %d sent key %q, not %q", i, in.Key, want[i])
					}
				}
				return sent[[]application.AddInput]{f.actor, f.drops}
			})
		},
		"AddMany's batch": func() []string {
			skip := owners("Outcomes.Result.")
			maps.Copy(skip, map[string]string{"Changed.Item.Owner": "the owner is the actor asked for"})
			return copytest.Diff(copytest.Match{Skip: skip}, func(b application.Batch) Batch { // each outcome's error left nil; TestAddManyAnswersEachDrop maps the errors
				f.batch = b
				drops := make([]AddRequest, len(b.Outcomes))
				for i := range drops {
					drops[i] = AddRequest{Key: "k-" + uuidv7.NewString(), Cause: Cause{Type: CauseDrop}}
				}
				out, err := m.AddMany(ctx, actor, drops)
				if err != nil {
					t.Fatal(err)
				}
				return out
			})
		},
		"HandIn's request": func() []string {
			return copytest.Diff(copytest.Match{Renames: map[string]string{"Req": "In"}}, func(a asked[HandInRequest]) sent[application.HandInInput] {
				if _, err := m.HandIn(ctx, a.Actor, a.Req); err != nil {
					t.Fatal(err)
				}
				return sent[application.HandInInput]{f.actor, f.in.(application.HandInInput)}
			})
		},
		"HandedIn's completions": func() []string {
			return copytest.Diff(copytest.Match{}, func(a handedInAsked) handedInSent {
				if _, err := m.HandedIn(ctx, a.Owner, a.Asked); err != nil {
					t.Fatal(err)
				}
				return handedInSent{f.owner, f.asked}
			})
		},
		"HandedIn's answers": func() []string {
			skip := owners("Result.")
			maps.Copy(skip, map[string]string{
				"ID":          "the hand-in's cause ID stays in the module",
				"Fingerprint": "the lines a hand-in was made with, which only the module compares",
			})
			return copytest.Diff(copytest.Match{Skip: skip}, func(found []application.HandIn) []HandedIn {
				f.handedIn = found
				out, err := m.HandedIn(ctx, actor.Character, nil)
				if err != nil {
					t.Fatal(err)
				}
				return out
			})
		},
	} {
		if got := diff(); len(got) > 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
}
