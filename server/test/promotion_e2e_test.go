package e2e_test

import (
	"context"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

func TestPromotion(t *testing.T) {
	t.Parallel()
	t.Run("C8/a-beginner-meeting-every-condition-becomes-a-warrior-with-the-trainers-weapon", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		ctx := context.Background()
		token, id := s.CreateCharacter(t, "petra", "Petra", "beginner")
		// Level 10, STR 30 without gear, ten points in Strike and the Warrior's Trial finished.
		for _, q := range []string{
			`UPDATE characters SET level = 10, str_points = 23 WHERE id = $1`,
			`INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'strike', 10, now(), now())`,
			`INSERT INTO character_quests (character_id, quest_id, status, progress, times_finished, created_at, updated_at) VALUES ($1, 'warriors_trial', 'finished', '{}', 1, now(), now())`,
		} {
			if _, err := s.DB.Exec(ctx, q, id); err != nil {
				t.Fatal(err)
			}
		}
		s.Place(t, id, "town", e2etest.ByTheTrainer, 50, 0)
		p := e2etest.NewLooter(s.Join(t, token, id))

		if r := p.Command(t, "t-1", &clientv1.Talk{NpcId: "warrior_trainer"}); !r.GetOk() {
			t.Fatalf("talk: %+v", r)
		}
		offer := e2etest.Take(t, p, "NpcDialog", &p.Dialogs, 5*time.Second, nil).GetPromotion()
		if offer.GetClass() != "warrior" || len(offer.GetConditions()) != 4 {
			t.Fatalf("the trainer offers %+v; want the Warrior's four conditions", offer)
		}
		for _, c := range offer.GetConditions() {
			if c.GetHave() < c.GetNeed() {
				t.Fatalf("condition %+v unmet", c)
			}
		}
		if r := p.Command(t, "p-1", &clientv1.Promote{NpcId: "warrior_trainer", Class: "thief"}); r.GetOk() || r.GetCode() != code("NOT_YOUR_NEXT_CLASS") {
			t.Fatalf("promoting to a class the trainer doesn't train: %+v", r)
		}

		p.Send(wsconn.Message{ReqID: "p-2", Data: &clientv1.Promote{NpcId: "warrior_trainer", Class: "warrior"}})
		got := p.Gather("Promoted", "InventoryChanged", "Response")
		if r := got["Response"]; r.ReqID != "p-2" || !r.Data.(*clientv1.Response).GetOk() {
			t.Fatalf("promote answered %+v", r)
		}
		c := got["Promoted"].Data.(*clientv1.Promoted).GetCharacter()
		if c.GetClass() != "warrior" || c.GetHp() != c.GetMaxHp() {
			t.Fatalf("promoted %+v; want a Warrior at full HP", c)
		}
		gift := false
		for _, sl := range got["InventoryChanged"].Data.(*clientv1.InventoryChanged).GetSlots() {
			gift = gift || sl.GetItem().GetTemplate() == "wooden_sword"
		}
		if !gift {
			t.Fatalf("inventory changed %+v; want the wooden sword", got["InventoryChanged"].Data)
		}
		if rows, _, _ := s.ItemTotals(t, id, "wooden_sword"); rows != 1 {
			t.Fatalf("%d wooden swords in the bag; want 1", rows)
		}

		if r := p.Command(t, "p-3", &clientv1.Promote{NpcId: "warrior_trainer", Class: "warrior"}); r.GetOk() || r.GetCode() != code("NOT_YOUR_NEXT_CLASS") {
			t.Fatalf("promoting again: %+v", r)
		}
		// The class is saved at once, so a fresh login comes back a Warrior.
		_, again := s.Welcome(t, "petra", id)
		if again.GetCharacter().GetClass() != "warrior" {
			t.Fatalf("welcomed back as %q; want a Warrior", again.GetCharacter().GetClass())
		}
		s.CheckInvariants(t)
	})
}
