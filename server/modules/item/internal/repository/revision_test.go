package repository

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/modules/item/internal/application"
	"finga/modules/item/internal/model"
	"finga/pkg/uuidv7"
)

// An inventory's revision counts its changes: each takes the next, a replay keeps the one it took, and a read shows the revision of what it lists.
func TestEachChangeTakesTheInventorysNextRevision(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	if r := h.revision(t); r != 0 {
		t.Fatalf("a new inventory is at revision %d, want 0", r)
	}
	pickup := application.AddInput{Key: "r-add", TemplateID: "red_potion", Quantity: 10, Cause: model.Cause{Type: model.CauseDrop, ID: uuidv7.New()}}
	added, err := h.svc.Add(ctx, h.actor, pickup)
	if err != nil {
		t.Fatal(err)
	}
	potion := added.Changed[0].Item.ID
	moved, err := h.svc.Move(ctx, h.actor, application.MoveInput{Key: "r-move", ItemID: potion, Tab: gamedata.Use, Slot: 5})
	if err != nil {
		t.Fatal(err)
	}
	discarded, err := h.svc.Discard(ctx, h.actor, application.DiscardInput{Key: "r-discard", ItemID: potion, Quantity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if added.Revision != 1 || moved.Revision != 2 || discarded.Revision != 3 || h.revision(t) != 3 {
		t.Fatalf("add, move and discard took %d, %d and %d, and a read shows %d; want 1, 2, 3 and 3", added.Revision, moved.Revision, discarded.Revision, h.revision(t))
	}

	t.Run("a replay keeps the revision its change took", func(t *testing.T) {
		if again, err := h.svc.Add(ctx, h.actor, pickup); err != nil || again.Revision != 1 || h.revision(t) != 3 {
			t.Fatalf("the replay carries %d (%v) and the inventory is at %d, want 1 and 3", again.Revision, err, h.revision(t))
		}
	})
	t.Run("a move that changes nothing takes none", func(t *testing.T) {
		same, err := h.svc.Move(ctx, h.actor, application.MoveInput{Key: "r-stay", ItemID: potion, Tab: gamedata.Use, Slot: 5})
		if err != nil || len(same.Changed) != 0 || same.Revision != 0 || h.revision(t) != 3 {
			t.Fatalf("move to its own slot: %+v (%v), inventory at %d; want no change, no revision, still 3", same, err, h.revision(t))
		}
	})
	t.Run("a refused change takes none", func(t *testing.T) {
		if _, err := h.svc.Discard(ctx, h.actor, application.DiscardInput{Key: "r-too-many", ItemID: potion, Quantity: 1000}); !errors.Is(err, model.ErrBadQuantity) || h.revision(t) != 3 {
			t.Fatalf("discard of too many: %v, inventory at %d; want ErrBadQuantity and still 3", err, h.revision(t))
		}
	})
	t.Run("a starter kit takes one revision for all its slots, and a second grant none", func(t *testing.T) {
		kit, err := h.svc.GrantStarter(ctx, h.actor)
		if err != nil || len(kit.Changed) != 1 || len(kit.Equipment) != 1 || kit.Revision != 4 {
			t.Fatalf("starter kit %+v (%v), want its slots at revision 4", kit, err)
		}
		again, err := h.svc.GrantStarter(ctx, h.actor)
		if err != nil || again.Revision != 0 || h.revision(t) != 4 {
			t.Fatalf("second grant %+v (%v), inventory at %d; want no revision, still 4", again, err, h.revision(t))
		}
	})
	t.Run("each inventory counts its own", func(t *testing.T) {
		other := model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}
		res, err := h.svc.Add(ctx, other, application.AddInput{Key: "r-other", TemplateID: "red_potion", Quantity: 1, Cause: model.Cause{Type: model.CauseDrop, ID: uuidv7.New()}})
		if err != nil || res.Revision != 1 || h.revision(t) != 4 {
			t.Fatalf("another character's first change took %d (%v), this one is at %d; want 1 and 4", res.Revision, err, h.revision(t))
		}
	})
}

// Changes racing on one inventory take distinct revisions in the order they changed their slots: a discard, which locks only its item, as much as an add.
func TestRacingChangesTakeRevisionsInTheirOrder(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	gel := h.add(t, uuidv7.NewString(), "slime_gel", 50).Changed[0].Item.ID
	h.add(t, uuidv7.NewString(), "red_potion", 1)
	const n = 8
	results := make([]application.Result, 2*n)
	errs := make([]error, 2*n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			results[i], errs[i] = h.svc.Add(ctx, h.actor, application.AddInput{Key: uuidv7.NewString(), TemplateID: "red_potion", Quantity: 1, Cause: model.Cause{Type: model.CauseDrop, ID: uuidv7.New()}})
		})
		wg.Go(func() {
			results[n+i], errs[n+i] = h.svc.Discard(ctx, h.actor, application.DiscardInput{Key: uuidv7.NewString(), ItemID: gel, Quantity: 1})
		})
	}
	wg.Wait()
	taken := map[uint64]bool{}
	for i, r := range results {
		if errs[i] != nil {
			t.Fatalf("change %d: %v", i, errs[i])
		}
		if r.Revision <= 2 || r.Revision > 2+2*n || taken[r.Revision] {
			t.Fatalf("change %d took revision %d; want each of 3 to %d once", i, r.Revision, 2+2*n)
		}
		taken[r.Revision] = true
	}
	byRevision := func(a, b application.Result) int { return cmp.Compare(a.Revision, b.Revision) }
	adds, discards := slices.SortedFunc(slices.Values(results[:n]), byRevision), slices.SortedFunc(slices.Values(results[n:]), byRevision)
	for k := range n {
		if q := adds[k].Changed[0].Item.Quantity; q != 2+k {
			t.Fatalf("the add at revision %d left %d potions, want %d: revisions out of the stack's order", adds[k].Revision, q, 2+k)
		}
		if q := discards[k].Changed[0].Item.Quantity; q != 49-k {
			t.Fatalf("the discard at revision %d left %d gels, want %d: revisions out of the stack's order", discards[k].Revision, q, 49-k)
		}
	}
	if r := h.revision(t); r != 2+2*n {
		t.Fatalf("the inventory is at %d, want %d", r, 2+2*n)
	}
}
