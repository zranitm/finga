package repository

import (
	"context"
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
					_, errs[i] = h.svc.Add(ctx, h.actor, application.AddInput{Key: uuidv7.NewString(), TemplateID: "wooden_sword", Quantity: 1, Cause: model.Cause{Type: model.CauseDrop, ID: uuidv7.New()}})
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
			want += len(starter.Changed) // the kit's new stacks; it finds none of its own templates to stack onto

			if got := len(h.inventory(t)); got != want {
				t.Fatalf("%d items, want %d", got, want)
			}
			mismatches, err := itemtest.CheckInvariants(ctx, h.pool)
			if err != nil || len(mismatches) > 0 {
				t.Fatalf("invariants: %v %v", mismatches, err)
			}
		})
	}
}
