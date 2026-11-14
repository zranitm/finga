package e2e_test

import (
	"context"
	"slices"
	"testing"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/progression"
	"finga/pkg/uuidv7"
	"finga/pkg/wsconn"
)

// D2: a full Equip tab goes in one discard, all of it or none, each item with its own event.
func TestDiscardManyEmptiesAFullTabAllOrNothing(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	_, id := s.createCharacter(t, "tidy", "Tidy", "beginner")
	ctx := context.Background()
	// inTab is the Equip tab's items, by ID, as the database has them.
	inTab := func() []string {
		t.Helper()
		rows, err := s.db.Query(ctx, `SELECT id::text FROM items WHERE owner_id = $1 AND location = 'inventory' AND tab = 'equip' ORDER BY slot`, id)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var item string
			if err := rows.Scan(&item); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, item)
		}
		return ids
	}
	for range 24 - len(inTab()) {
		s.give(t, id, "wooden_sword", &progression.ItemStats{PhysicalAttack: 11})
	}
	full := inTab()
	if len(full) != 24 {
		t.Fatalf("the Equip tab holds %d, want its 24", len(full))
	}
	discarded := func() int {
		t.Helper()
		var n int
		if err := s.db.QueryRow(ctx, `SELECT count(*) FROM item_events e JOIN items i ON i.id = e.item_id WHERE i.owner_id = $1 AND e.kind = 'discarded'`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	p, _ := s.welcome(t, "tidy", id)

	// One item no longer held refuses the batch, and the rest stay.
	p.send(wsconn.Message{ReqID: "d-1", Data: &clientv1.InventoryDiscardMany{IdempotencyKey: "k-" + uuidv7.NewString(), ItemIds: append(slices.Clone(full[:12]), uuidv7.NewString())}})
	if r := p.expect("Response").Data.(*clientv1.Response); r.GetOk() || r.Code != code("ITEM_NOT_FOUND") {
		t.Fatalf("a batch with an item not held: %+v", r)
	}
	if len(inTab()) != 24 || discarded() != 0 {
		t.Fatalf("a refused batch left %d of 24 and %d discarded events", len(inTab()), discarded())
	}

	// The whole tab in one request.
	key := "k-" + uuidv7.NewString()
	p.send(wsconn.Message{ReqID: "d-2", Data: &clientv1.InventoryDiscardMany{IdempotencyKey: key, ItemIds: full}})
	if r := p.expect("Response"); r.ReqID != "d-2" || !r.Data.(*clientv1.Response).GetOk() {
		t.Fatalf("discarding the full tab: %+v", r)
	}
	ic := p.expect("InventoryChanged").Data.(*clientv1.InventoryChanged)
	emptied := 0
	for _, sl := range ic.Slots {
		if sl.GetTab() == "equip" && sl.Item == nil {
			emptied++
		}
	}
	if emptied != 24 || len(ic.Slots) != 24 {
		t.Fatalf("%d slots changed, %d of them emptied Equip slots; want 24", len(ic.Slots), emptied)
	}
	if len(inTab()) != 0 || discarded() != 24 {
		t.Fatalf("%d left in the tab, %d discarded events; want none and 24", len(inTab()), discarded())
	}

	// Sent again, as after a lost answer, it replays: the same revision, nothing more discarded.
	p.send(wsconn.Message{ReqID: "d-3", Data: &clientv1.InventoryDiscardMany{IdempotencyKey: key, ItemIds: full}})
	if r := p.expect("Response").Data.(*clientv1.Response); !r.GetOk() {
		t.Fatalf("the retry: %+v", r)
	}
	if again := p.expect("InventoryChanged").Data.(*clientv1.InventoryChanged); again.Revision != ic.Revision || discarded() != 24 {
		t.Fatalf("the retry at revision %d with %d discarded events; want %d and 24", again.Revision, discarded(), ic.Revision)
	}
	s.checkInvariants(t)
}
