package e2e_test

import (
	"context"
	"math"
	"slices"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// D1: a take the item module is slow to answer never holds the player still. With the items table locked, the player walks on, the world applying each input, until the lock goes and the take answers with each item taken. Held past the world's budget for the call, each pick is answered UNAVAILABLE inside the gateway's wait, not the whole request INTERNAL, and the items come once the module answers. Judged by frames and Responses; the lock is held only as long as each step needs.
func TestASlowTakeNeverHoldsThePlayerStill(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, append(e2etest.QuickSlimes(), everyKill)...))
	token, id := s.CreateCharacter(t, "nell", "Nell", "bowman")
	s.Place(t, id, "forest", e2etest.SlimeField, 134, 0)
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

	first := pouches[0]
	if !l.WalkTo(gamedata.Point{X: first.GetX(), Z: first.GetZ()}, 20*time.Second) {
		t.Fatalf("could not reach pouch %d", first.GetId())
	}
	release := held()
	defer release()
	l.Send(wsconn.Message{ReqID: "slow", Data: &clientv1.TakeLoot{Picks: e2etest.All(first)}})
	fromX, fromZ := l.X, l.Z
	moved := l.Until(10*time.Second, func() bool {
		if math.Hypot(l.X-fromX, l.Z-fromZ) >= 2 {
			return true
		}
		_ = l.Input(e2etest.Toward(1, 0))
		return false
	})
	if !moved || answer("slow") != nil {
		t.Fatalf("moved %v, %.2f m, with the take answered %+v; want the player walking on while the take waits", moved, math.Hypot(l.X-fromX, l.Z-fromZ), answer("slow"))
	}
	_ = l.Input(&clientv1.Input{})
	release()
	if !l.Until(10*time.Second, func() bool { return answer("slow") != nil }) {
		t.Fatal("no answer to the take after the lock went")
	}
	if r := answer("slow"); !r.GetOk() || len(r.GetPicks()) != len(first.GetItems()) || slices.ContainsFunc(e2etest.Codes(r), func(c int32) bool { return c != 0 }) {
		t.Fatalf("the take answered %+v; want each item taken once the module answered", r)
	}

	second := pouches[1]
	if !l.WalkTo(gamedata.Point{X: second.GetX(), Z: second.GetZ()}, 20*time.Second) {
		t.Fatalf("could not reach pouch %d", second.GetId())
	}
	gained := l.Inventory
	release = held()
	defer release()
	l.Send(wsconn.Message{ReqID: "stuck", Data: &clientv1.TakeLoot{Picks: e2etest.All(second)}})
	if !l.Until(10*time.Second, func() bool { return answer("stuck") != nil }) {
		t.Fatal("no answer to the take under the lock")
	}
	if r := answer("stuck"); !r.GetOk() || len(r.GetPicks()) != len(second.GetItems()) || slices.ContainsFunc(e2etest.Codes(r), func(c int32) bool { return c != code("UNAVAILABLE") }) {
		t.Fatalf("the take under the lock answered %+v; want each pick UNAVAILABLE", r)
	}
	release()
	if !l.Until(10*time.Second, func() bool {
		return l.Inventory > gained && len(l.LootOf(second.GetId(), clientv1.LootKind_LOOT_KIND_TAKEN)) == 1
	}) {
		t.Fatalf("%d InventoryChanged, loot %v; want the items asked again and taken once the module answered", l.Inventory-gained, l.Loots)
	}
}
