package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"finga/pkg/uuidv7"
	"finga/services/item/internal/application"
	"finga/services/item/internal/model"
	"finga/services/item/itemtest"
)

// Concurrent changes to one inventory each see the others' slots: the owner's lock orders them.
func TestConcurrentAddsFindTheirOwnSlots(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name    string
		seed    bool
		starter bool
	}{
		{name: "an empty inventory"},
		{name: "an inventory with an item", seed: true},
		{name: "beside the starter grant", starter: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			want := 0
			if tt.seed {
				h.add(t, uuidv7.NewString(), "wooden_sword", 1)
				want++
			}
			const n = 8
			errs := make([]error, n+1)
			var starter application.Result
			var wg sync.WaitGroup
			for i := range n {
				wg.Go(func() {
					_, errs[i] = h.svc.Add(ctx, h.actor, application.AddInput{Key: uuidv7.NewString(), TemplateID: "wooden_sword", Quantity: 1, Cause: model.Cause{Type: model.CauseDrop, ID: uuidv7.New()}, Stats: h.roll("wooden_sword")})
				})
			}
			if tt.starter {
				wg.Go(func() { starter, errs[n] = h.svc.GrantStarter(ctx, h.actor, "warrior") })
			}
			wg.Wait()
			for i, err := range errs {
				if err != nil {
					t.Fatalf("change %d: %v", i, err)
				}
			}
			want += n
			want += len(starter.Changed) + len(starter.Equipment) // the kit's new stacks and its worn sword; it finds none of its own templates to stack onto

			if got := len(h.inventory(t)); got != want {
				t.Fatalf("%d items, want %d", got, want)
			}
			mismatches, err := itemtest.CheckInvariants(ctx, h.pool, h.data)
			if err != nil || len(mismatches) > 0 {
				t.Fatalf("invariants: %v %v", mismatches, err)
			}
		})
	}
}

// A swap racing a pickup on a full Equip tab: whichever goes first, the swap works and the pickup finds no slot, not even the one the swap passes through.
func TestASwapAndAPickupRacingOnAFullEquipTab(t *testing.T) {
	ctx := context.Background()
	for run := range 5 {
		t.Run(fmt.Sprint(run), func(t *testing.T) {
			h, wearing, tab := fullEquipTab(t)
			var swapped, picked error
			var wg sync.WaitGroup
			wg.Go(func() { _, swapped = h.equip(uuidv7.NewString(), tab[7].ID, "", 1) })
			wg.Go(func() {
				_, picked = h.svc.Add(ctx, h.actor, application.AddInput{Key: uuidv7.NewString(), TemplateID: "leather_cap", Quantity: 1, Cause: model.Cause{Type: model.CauseDrop, ID: uuidv7.New()}, Stats: h.roll("leather_cap")})
			})
			wg.Wait()
			if swapped != nil || !errors.Is(picked, model.ErrEquipTabFull) {
				t.Fatalf("swap: %v; pickup: %v, want the Equip tab full", swapped, picked)
			}
			if off, on := h.held(t, wearing.ID), h.held(t, tab[7].ID); off.Worn() || off.Slot != 7 || on.EquipSlot != "weapon" {
				t.Fatalf("swapped out %+v, in %+v", off, on)
			}
			checkInvariants(t, h.pool, h.data)
		})
	}
}
