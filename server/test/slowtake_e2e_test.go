package e2e_test

import (
	"context"
	"slices"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// D1: a take the item module is slow to answer never holds the player still, and its items come once the module answers; held past the call's budget, each pick is UNAVAILABLE, not the whole take INTERNAL. Judged by frames and Responses.
func TestASlowTakeNeverHoldsThePlayerStill(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, append(e2etest.QuickSlimes(), everyKill)...))
	token, id := s.CreateCharacter(t, "nell", "Nell", "bowman")
	s.Place(t, id, "willowmere", e2etest.SlimeField, 134, 0)
	p, snap := s.Join(t, token, id)
	l := e2etest.NewLooter(p, snap)
	ctx := context.Background()
	if !l.HuntPouches(60*time.Second, 2) {
		t.Fatalf("%d pouches in a minute of hunting, want 2", len(l.Seen))
	}
	var pouches []*clientv1.PouchView
	for _, pv := range l.Pouches {
		pouches = append(pouches, pv)
	}
	slices.SortFunc(pouches, func(a, b *clientv1.PouchView) int { return int(a.GetId()) - int(b.GetId()) })
	held := func() func() { // the items table locked against writes, as a busy item module
		tx, err := s.DB.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `LOCK TABLE items IN EXCLUSIVE MODE`); err != nil {
			t.Fatal(err)
		}
		return func() { _ = tx.Rollback(ctx) }
	}
	answer := func(req string) *clientv1.Response {
		for _, r := range l.Responses {
			if r.Req == req {
				return r.R
			}
		}
		return nil
	}

	every := func(r *clientv1.Response, n int, want int32) bool {
		return r.GetOk() && len(r.GetPicks()) == n && !slices.ContainsFunc(e2etest.Codes(r), func(c int32) bool { return c != want })
	}

	first := pouches[0]
	if !l.WalkTo(gamedata.Point{X: first.GetX(), Z: first.GetZ()}, 20*time.Second) {
		t.Fatalf("could not reach pouch %d", first.GetId())
	}
	gained := l.Inventory
	release := held()
	defer release()
	l.Send(wsconn.Message{ReqID: "slow", Data: &clientv1.TakeLoot{Picks: e2etest.All(first)}})
	_ = l.Input(e2etest.Toward(1, 0))
	from, acked, lastX, lastZ := l.Seq, l.Acked(), l.X, l.Z
	walked := 0 // frames, while the take waits, on which the world applied a newer input sent after it and moved the player
	l.Await(10*time.Second, func(m wsconn.Message) bool {
		if answer("slow") != nil {
			return true
		}
		if _, frame := m.Data.(*clientv1.Delta); !frame {
			return false
		}
		if l.Acked() >= from && l.Acked() > acked && (l.X != lastX || l.Z != lastZ) {
			walked++
		}
		acked, lastX, lastZ = l.Acked(), l.X, l.Z
		_ = l.Input(e2etest.Toward(1, 0))
		return walked >= 10
	})
	_ = l.Input(&clientv1.Input{})
	early := answer("slow") // a starved run may pass the call's budget first: then the walk need only have begun
	if walked < 10 && (early == nil || walked == 0) {
		t.Fatalf("walked %d frames with the take answered %+v; want the world applying the player's inputs while the take waits", walked, early)
	}
	release()
	if !l.Until(10*time.Second, func() bool {
		return answer("slow") != nil && l.Inventory > gained && len(l.LootOf(first.GetId(), clientv1.LootKind_LOOT_KIND_TAKEN)) == 1
	}) {
		t.Fatalf("the take answered %+v, %d InventoryChanged; want its items once the lock went", answer("slow"), l.Inventory-gained)
	}
	switch r, n := answer("slow"), len(first.GetItems()); {
	case every(r, n, 0):
		t.Logf("walked %d frames while the take waited; it answered with each item once the lock went", walked)
	case every(r, n, code("UNAVAILABLE")):
		t.Logf("a slow run: walked %d frames, then the take passed its budget; the items came on the retry", walked)
	default:
		t.Fatalf("the take answered %+v; want each item taken, or each pick UNAVAILABLE and taken after", r)
	}

	second := pouches[1]
	if !l.WalkTo(gamedata.Point{X: second.GetX(), Z: second.GetZ()}, 20*time.Second) {
		t.Fatalf("could not reach pouch %d", second.GetId())
	}
	gained = l.Inventory
	release = held()
	defer release()
	l.Send(wsconn.Message{ReqID: "stuck", Data: &clientv1.TakeLoot{Picks: e2etest.All(second)}})
	if !l.Until(10*time.Second, func() bool { return answer("stuck") != nil }) {
		t.Fatal("no answer to the take under the lock")
	}
	if r := answer("stuck"); !every(r, len(second.GetItems()), code("UNAVAILABLE")) {
		t.Fatalf("the take under the lock answered %+v; want each pick UNAVAILABLE", r)
	}
	release()
	if !l.Until(10*time.Second, func() bool {
		return l.Inventory > gained && len(l.LootOf(second.GetId(), clientv1.LootKind_LOOT_KIND_TAKEN)) == 1
	}) {
		t.Fatalf("%d InventoryChanged, loot %v; want the items asked again and taken once the module answered", l.Inventory-gained, l.Loots)
	}
}
