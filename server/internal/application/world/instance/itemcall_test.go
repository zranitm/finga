package instance_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
	"finga/pkg/transient"
)

// A command's item-module call that gets no answer is unavailable 3.5 s in, every try included, inside the 5 s the server waits: the command hears the module isn't there, never the server's own deadline, which it would count a fault.
func TestAnUnansweredItemCallIsUnavailableInsideTheServersWait(t *testing.T) {
	for _, tt := range []struct {
		name string
		opts func() worldtest.Options // an item module that never answers
		ask  func(inst *instance.Instance, id kernel.CharacterID) <-chan error
	}{
		{"an equip", func() worldtest.Options {
			return worldtest.Options{Equipment: &fakeEquipment{gate: make(chan struct{})}}
		},
			func(inst *instance.Instance, id kernel.CharacterID) <-chan error { return equipAsync(inst, id, "c1") }},
		{"an unequip", func() worldtest.Options {
			return worldtest.Options{Equipment: &fakeEquipment{gate: make(chan struct{})}}
		},
			func(inst *instance.Instance, id kernel.CharacterID) <-chan error {
				done := make(chan error, 1)
				go func() { done <- inst.Unequip(context.Background(), id, "c1", "k-1", "weapon", nil) }()
				return done
			}},
		{"a look", func() worldtest.Options { return worldtest.Options{Looks: &fakeLooks{gate: make(chan struct{})}} },
			func(inst *instance.Instance, id kernel.CharacterID) <-chan error {
				return setLookAsync(inst, id, "k-1", model.Look{Outfit: "knight_plate"})
			}},
		{"a potion", func() worldtest.Options { return worldtest.Options{Potions: &fakePotions{gate: make(chan struct{})}} },
			func(inst *instance.Instance, id kernel.CharacterID) <-chan error {
				return useAsync(inst, id, "red_potion")
			}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				opts := tt.opts()
				opts.ItemCalls = runningPool(t)
				inst, stop := worldtest.Run(t, data, nil, opts)
				defer stop()
				p := atSpawn(data, "Ayla")
				p.HP, p.Connection = 10, "c1"
				join(t, inst, p)
				began := time.Now()
				err := <-tt.ask(inst, p.ID)
				if took := time.Since(began); !errors.Is(err, transient.ErrUnavailable) || errors.Is(err, context.DeadlineExceeded) || took < 3500*time.Millisecond || took >= 4*time.Second {
					t.Fatalf("%v after %v; want unavailable at 3.5 s, inside the server's 5", err, took)
				}
			})
		})
	}
}

// A command whose item call starts late, the queues and ticks before it having taken most of the server's 5 s as a starved box can, still hears the module isn't there before its own deadline: its wait gives up half a second short of it, never with the deadline itself, which the server would answer as a fault.
func TestALateItemCallIsUnavailableBeforeTheCommandsDeadline(t *testing.T) {
	const left = 2 * time.Second // of the command's 5 s when the world takes it: under callFor
	for _, tt := range []struct {
		name string
		opts func() worldtest.Options // an item module that never answers
		ask  func(ctx context.Context, inst *instance.Instance, id kernel.CharacterID) error
	}{
		{"an equip", func() worldtest.Options {
			return worldtest.Options{Equipment: &fakeEquipment{gate: make(chan struct{})}}
		},
			func(ctx context.Context, inst *instance.Instance, id kernel.CharacterID) error {
				return inst.Equip(ctx, id, "c1", "k-1", kernel.NewItemID(), "")
			}},
		{"an unequip", func() worldtest.Options {
			return worldtest.Options{Equipment: &fakeEquipment{gate: make(chan struct{})}}
		},
			func(ctx context.Context, inst *instance.Instance, id kernel.CharacterID) error {
				return inst.Unequip(ctx, id, "c1", "k-1", "weapon", nil)
			}},
		{"a look", func() worldtest.Options { return worldtest.Options{Looks: &fakeLooks{gate: make(chan struct{})}} },
			func(ctx context.Context, inst *instance.Instance, id kernel.CharacterID) error {
				return inst.SetLook(ctx, id, "c1", "k-1", model.Look{Outfit: "knight_plate"})
			}},
		{"a potion", func() worldtest.Options { return worldtest.Options{Potions: &fakePotions{gate: make(chan struct{})}} },
			func(ctx context.Context, inst *instance.Instance, id kernel.CharacterID) error {
				return inst.UseItem(ctx, id, "c1", "k-1", kernel.NewItemID(), "red_potion")
			}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				opts := tt.opts()
				opts.ItemCalls = runningPool(t)
				inst, stop := worldtest.Run(t, data, nil, opts)
				defer stop()
				p := atSpawn(data, "Ayla")
				p.HP, p.Connection = 10, "c1"
				join(t, inst, p)
				ctx, cancel := context.WithTimeout(context.Background(), left)
				defer cancel()
				began := time.Now()
				err := tt.ask(ctx, inst, p.ID)
				if took := time.Since(began); !errors.Is(err, transient.ErrUnavailable) || errors.Is(err, context.DeadlineExceeded) || took != left-500*time.Millisecond {
					t.Fatalf("%v after %v; want unavailable half a second before the command's %v", err, took, left)
				}
			})
		})
	}
}
