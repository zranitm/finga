package e2e_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/progression"
	"finga/pkg/uuidv7"
	"finga/test/e2etest"
)

// TestRoomForAReward is the user's rule of 2026-11-12: a reward that doesn't fit is refused for the first short tab, and goes through once the slots the client counts are free. Nothing else waits for room: an item a pouch holds stays in it.
func TestRoomForAReward(t *testing.T) {
	t.Parallel()
	// freeOne discards a cap from the bag.
	freeOne := func(t *testing.T, s *e2etest.Stack, l *e2etest.Looter, req string) {
		t.Helper()
		var held string
		if err := s.DB.QueryRow(context.Background(), `SELECT id::text FROM items WHERE owner_id = $1 AND template_id = 'leather_cap' AND location = 'inventory' LIMIT 1`, l.ID).Scan(&held); err != nil {
			t.Fatal(err)
		}
		if r := l.Command(t, req, &clientv1.InventoryDiscard{IdempotencyKey: e2etest.Key(), ItemId: held, Quantity: 1}); !r.GetOk() {
			t.Fatalf("discard: %+v", r)
		}
	}
	t.Run("H1/a-hand-in-refused-for-a-full-tab-goes-through-once-the-reward-has-room", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.CreateCharacter(t, "hana", "Hana", "bowman")
		for _, q := range []string{
			`UPDATE characters SET level = 2 WHERE id = $1`,
			`INSERT INTO character_quests (character_id, quest_id, status, progress, times_finished, created_at, updated_at) VALUES ($1, 'slime_cleanup', 'finished', '{}', 1, now(), now())`,
			`INSERT INTO character_quests (character_id, quest_id, status, progress, times_finished, created_at, updated_at) VALUES ($1, 'sticky_samples', 'active', '{0}', 0, now(), now())`,
		} {
			if _, err := s.DB.Exec(context.Background(), q, id); err != nil {
				t.Fatal(err)
			}
		}
		for range 5 {
			s.Give(t, id, "slime_gel", nil)
		}
		for range 24 {
			s.Give(t, id, "leather_cap", &progression.ItemStats{PhysicalDefense: 3})
		}
		s.Place(t, id, "willowmere", e2etest.Spots(t).ByTheGuide, 134, 0)
		l, _ := s.Hello(t, token, id)
		handIn := &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "sticky_samples"}
		if r := l.Command(t, "h-1", handIn); r.GetOk() || r.GetCode() != code("EQUIP_TAB_FULL") {
			t.Fatalf("handing in with a full Equip tab: %+v", r)
		}
		// The one slot the client counts: the hand-in goes through.
		freeOne(t, s, l, "d-1")
		if r := l.Command(t, "h-2", handIn); !r.GetOk() {
			t.Fatalf("handing in with room: %+v", r)
		}
		after := s.InventoryTotals(t, id)
		if after["slime_gel"] != 0 || after["leather_cap"] != 24 {
			t.Fatalf("held %v; want the gels taken and 24 caps: one freed and the reward", after)
		}
		s.CheckInvariants(t)
	})

	t.Run("C8/a-promotion-refused-for-a-full-tab-gives-its-weapon-once-under-its-key-when-there-is-room", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		ctx := context.Background()
		token, id := s.CreateCharacter(t, "pavo", "Pavo", "beginner")
		for _, q := range []string{
			`UPDATE characters SET level = 10, str_points = 23 WHERE id = $1`,
			`INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'strike', 10, now(), now())`,
			`INSERT INTO character_quests (character_id, quest_id, status, progress, times_finished, created_at, updated_at) VALUES ($1, 'warriors_trial', 'finished', '{}', 1, now(), now())`,
		} {
			if _, err := s.DB.Exec(ctx, q, id); err != nil {
				t.Fatal(err)
			}
		}
		for range 24 {
			s.Give(t, id, "leather_cap", &progression.ItemStats{PhysicalDefense: 3})
		}
		s.Place(t, id, "willowmere", e2etest.Spots(t).ByTheTrainer, 134, 0)
		l, _ := s.Hello(t, token, id)
		gift := uuidv7.Derived(uuid.MustParse(id), "promotion:warrior").String()
		keyed := func() int {
			var n int
			if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM idempotency_keys WHERE key = $1`, gift).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		promote := &clientv1.Promote{NpcId: "warrior_trainer", Class: "warrior"}
		if r := l.Command(t, "p-1", promote); r.GetOk() || r.GetCode() != code("EQUIP_TAB_FULL") {
			t.Fatalf("promoting with a full Equip tab: %+v", r)
		}
		if n := keyed(); n != 0 {
			t.Fatalf("%d replies kept under the gift's key after a refusal; want none, so a retry gives it", n)
		}
		freeOne(t, s, l, "d-1")
		if r := l.Command(t, "p-2", promote); !r.GetOk() {
			t.Fatalf("promoting with room: %+v", r)
		}
		if rows, _, _ := s.ItemTotals(t, id, "wooden_sword"); rows != 1 {
			t.Fatalf("%d wooden swords in the bag; want the one gift", rows)
		}
		if n := keyed(); n != 1 {
			t.Fatalf("%d replies kept under the gift's key; want the one that gave it", n)
		}
		s.CheckInvariants(t)
	})
}
