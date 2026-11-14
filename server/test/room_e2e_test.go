package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/progression"
	"finga/pkg/uuidv7"
)

// TestRoomForAReward is the user's rule of 2026-11-12: a reward that doesn't fit is refused for the first short tab, and goes through once the slots the client counts are free, a drop waiting for room taking one of them first.
func TestRoomForAReward(t *testing.T) {
	t.Parallel()
	// A slime dies to a hit and drops a cap; a drop waits a minute for room, looked at each second.
	data := gameData(t, append(quickSlimes(), slimesDrop(oneCap), lootTimes("1s", "60s", "1s"), guideByThePortal())...)

	// waitingCap hunts the forest until a cap drops, which a full Equip tab leaves waiting, then goes to the town.
	waitingCap := func(t *testing.T, l *looter) *clientv1.DropView {
		t.Helper()
		l.face(true)
		cap, ok := l.hunt(30*time.Second, "leather_cap")
		if !ok {
			t.Fatal("no cap dropped in 30 s of hunting")
		}
		if !l.until(5*time.Second, func() bool { return len(l.lootOf(cap.GetId(), clientv1.LootKind_LOOT_KIND_WAITING)) == 1 }) {
			t.Fatalf("loot %v; want the cap waiting for room", l.loots)
		}
		l.hop(t, 64, "town", 1216)
		return cap
	}
	// freeOne discards a cap from the bag.
	freeOne := func(t *testing.T, s *stack, l *looter, req string) {
		t.Helper()
		var held string
		if err := s.db.QueryRow(context.Background(), `SELECT id::text FROM items WHERE owner_id = $1 AND template_id = 'leather_cap' AND location = 'inventory' LIMIT 1`, l.id).Scan(&held); err != nil {
			t.Fatal(err)
		}
		if r := l.command(t, req, &clientv1.InventoryDiscard{IdempotencyKey: key(), ItemId: held, Quantity: 1}); !r.GetOk() {
			t.Fatalf("discard: %+v", r)
		}
	}
	collected := func(t *testing.T, l *looter, cap *clientv1.DropView) {
		t.Helper()
		if !l.until(5*time.Second, func() bool { return len(l.lootOf(cap.GetId(), clientv1.LootKind_LOOT_KIND_COLLECTED)) == 1 }) {
			t.Fatalf("loot %v; want the waiting cap collected into the freed slot", l.loots)
		}
	}

	t.Run("H1/a-hand-in-refused-for-a-full-tab-goes-through-once-the-reward-and-the-waiting-drop-have-room", func(t *testing.T) {
		t.Parallel()
		s := newStackOver(t, data)
		token, id := s.createCharacter(t, "hana", "Hana", "bowman")
		for _, q := range []string{
			`UPDATE characters SET level = 2 WHERE id = $1`,
			`INSERT INTO character_quests (character_id, quest_id, status, progress, times_finished, created_at, updated_at) VALUES ($1, 'slime_cleanup', 'finished', '{}', 1, now(), now())`,
			`INSERT INTO character_quests (character_id, quest_id, status, progress, times_finished, created_at, updated_at) VALUES ($1, 'sticky_samples', 'active', '{0}', 0, now(), now())`,
		} {
			if _, err := s.db.Exec(context.Background(), q, id); err != nil {
				t.Fatal(err)
			}
		}
		for range 5 {
			s.give(t, id, "slime_gel", nil)
		}
		for range 24 {
			s.give(t, id, "leather_cap", &progression.ItemStats{PhysicalDefense: 3})
		}
		s.place(t, id, "forest", 380, 608, 134, 0)
		l, _ := s.hello(t, token, id)
		cap := waitingCap(t, l)
		if !l.walkTo(guideX, 30*time.Second) {
			t.Fatalf("Hana could not reach the guide (at %.0f)", l.x)
		}
		handIn := &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "sticky_samples"}
		if r := l.command(t, "h-1", handIn); r.GetOk() || r.GetCode() != code("EQUIP_TAB_FULL") {
			t.Fatalf("handing in with a full Equip tab: %+v", r)
		}
		// One slot freed: the waiting cap takes it first, so the reward still has none.
		freeOne(t, s, l, "d-1")
		collected(t, l, cap)
		if r := l.command(t, "h-2", handIn); r.GetOk() || r.GetCode() != code("EQUIP_TAB_FULL") {
			t.Fatalf("handing in after one slot freed, the cap in it: %+v", r)
		}
		// The second the client counts: the hand-in goes through.
		freeOne(t, s, l, "d-2")
		if r := l.command(t, "h-3", handIn); !r.GetOk() {
			t.Fatalf("handing in with room: %+v", r)
		}
		after := s.inventoryTotals(t, id)
		if after["slime_gel"] != 0 || after["leather_cap"] != 24 {
			t.Fatalf("held %v; want the gels taken and 24 caps: two freed, the waiting one and the reward", after)
		}
		s.checkInvariants(t)
	})

	t.Run("C8/a-promotion-refused-for-a-full-tab-gives-its-weapon-once-under-its-key-when-there-is-room", func(t *testing.T) {
		t.Parallel()
		s := newStackOver(t, data)
		ctx := context.Background()
		token, id := s.createCharacter(t, "pavo", "Pavo", "beginner")
		for _, q := range []string{
			`UPDATE characters SET level = 10, str_points = 23 WHERE id = $1`,
			`INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'strike', 10, now(), now())`,
			`INSERT INTO character_quests (character_id, quest_id, status, progress, times_finished, created_at, updated_at) VALUES ($1, 'warriors_trial', 'finished', '{}', 1, now(), now())`,
		} {
			if _, err := s.db.Exec(ctx, q, id); err != nil {
				t.Fatal(err)
			}
		}
		for range 24 {
			s.give(t, id, "leather_cap", &progression.ItemStats{PhysicalDefense: 3})
		}
		s.place(t, id, "forest", 380, 608, 134, 0)
		l, _ := s.hello(t, token, id)
		cap := waitingCap(t, l)
		// Beside the Warrior Trainer, at x 1120.
		if !l.walkTo(1100, 30*time.Second) {
			t.Fatalf("Pavo could not reach the trainer (at %.0f)", l.x)
		}
		gift := uuidv7.Derived(uuid.MustParse(id), "promotion:warrior").String()
		keyed := func() int {
			var n int
			if err := s.db.QueryRow(ctx, `SELECT count(*) FROM idempotency_keys WHERE key = $1`, gift).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		promote := &clientv1.Promote{NpcId: "warrior_trainer", Class: "warrior"}
		if r := l.command(t, "p-1", promote); r.GetOk() || r.GetCode() != code("EQUIP_TAB_FULL") {
			t.Fatalf("promoting with a full Equip tab: %+v", r)
		}
		if n := keyed(); n != 0 {
			t.Fatalf("%d replies kept under the gift's key after a refusal; want none, so a retry gives it", n)
		}
		freeOne(t, s, l, "d-1")
		collected(t, l, cap)
		if r := l.command(t, "p-2", promote); r.GetOk() || r.GetCode() != code("EQUIP_TAB_FULL") {
			t.Fatalf("promoting after one slot freed, the cap in it: %+v", r)
		}
		freeOne(t, s, l, "d-2")
		if r := l.command(t, "p-3", promote); !r.GetOk() {
			t.Fatalf("promoting with room: %+v", r)
		}
		if rows, _, _ := s.itemTotals(t, id, "wooden_sword"); rows != 1 {
			t.Fatalf("%d wooden swords in the bag; want the one gift", rows)
		}
		if n := keyed(); n != 1 {
			t.Fatalf("%d replies kept under the gift's key; want the one that gave it", n)
		}
		s.checkInvariants(t)
	})
}
