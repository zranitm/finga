package main

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	"finga/internal/gamedata/gamedatatest"
	"finga/internal/progression"
)

func testerNamed(t *testing.T, username string) tester {
	t.Helper()
	for _, tr := range testers {
		if tr.username == username {
			return tr
		}
	}
	t.Fatalf("no tester %s", username)
	return tester{}
}

func TestEachCappedTesterIsItsClassAtTheCapWithItsPromotionMetAndEveryPointPlaced(t *testing.T) {
	data := gamedatatest.Shipped(t)
	rules := data.StatRules()
	for _, name := range []string{"test_warrior", "test_thief", "test_bowman", "test_magician"} {
		t.Run(name, func(t *testing.T) {
			tr := testerNamed(t, name)
			sd, err := plan(data, tr)
			if err != nil {
				t.Fatal(err)
			}
			if sd.class.ID != tr.future || sd.level != data.LevelCap() {
				t.Fatalf("a %s at level %d; want a %s at the cap, %d", sd.class.ID, sd.level, tr.future, data.LevelCap())
			}
			own := progression.AddStats(data.StartClass().BaseStats, sd.stats)
			need := sd.class.Promotion.Stats
			if own.STR < need.STR || own.DEX < need.DEX || own.INT < need.INT || own.AGI < need.AGI || own.VIT < need.VIT {
				t.Errorf("own stats %+v short of the promotion's %+v", own, need)
			}
			if left := sd.unspent - sd.stats.Total(); left < 0 || left >= sd.class.AutoAssign.Total() {
				t.Errorf("%d of %d points left; want less than a whole split", left, sd.unspent)
			}
			total := int32(0)
			for _, n := range sd.skills {
				total += n
			}
			if int(total) != sd.unspentSkills || sd.unspentSkills != (sd.level-1)*rules.SkillPointsPerLevel {
				t.Errorf("skills %v place %d of %d", sd.skills, total, sd.unspentSkills)
			}
			for skill, n := range sd.class.Promotion.Skills {
				if int(sd.skills[skill]) < n {
					t.Errorf("%s has %d points; the promotion asks %d", skill, sd.skills[skill], n)
				}
			}
			if sd.skills[sd.class.Skills[0]] == 0 {
				t.Errorf("nothing in %s, the class's first skill, after the promotion: %v", sd.class.Skills[0], sd.skills)
			}
			if len(sd.quests) != 1 || sd.quests[0].GetStatus() != characterv1.QuestStatus_QUEST_STATUS_FINISHED || sd.quests[0].GetTimesFinished() != 1 {
				t.Errorf("quests %v; want its trial finished once", sd.quests)
			}
			// Before its points are spent, as the world caps them at the first join.
			d := progression.DeriveStats(rules, sd.class.ID, sd.level, data.StartClass().BaseStats, progression.ItemStats{})
			if sd.hp != d.MaxHP || sd.energy != data.Energy().Max || sd.hp <= 0 {
				t.Errorf("HP %d and energy %d; want full at the first join, %d and %d", sd.hp, sd.energy, d.MaxHP, data.Energy().Max)
			}
		})
	}
}

func TestCappedGearIsTheBestEachSlotTakesAndAllOfItCanBeWorn(t *testing.T) {
	data := gamedatatest.Shipped(t)
	sd, err := plan(data, testerNamed(t, "test_warrior"))
	if err != nil {
		t.Fatal(err)
	}
	own := progression.AddStats(data.StartClass().BaseStats, sd.stats)
	slots := map[string]bool{}
	for _, p := range sd.gear {
		it, _ := data.Item(p.template)
		if slots[p.slot] {
			t.Errorf("two pieces for %s", p.slot)
		}
		slots[p.slot] = true
		if u := progression.Meets(it, sd.level, sd.class, own); u != progression.NothingUnmet {
			t.Errorf("%s in %s can't be worn: %v", p.template, p.slot, u)
		}
		if p.roll == nil || !p.roll.Fits(it.Rolls) {
			t.Errorf("%s rolled %+v, outside its ranges", p.template, p.roll)
		}
		slot, _ := data.EquipSlot(p.slot)
		for _, other := range data.Items() {
			if other.Slot == slot.Kind && other.MinLevel > it.MinLevel && progression.Meets(other, sd.level, sd.class, own) == progression.NothingUnmet {
				t.Errorf("%s in %s, though %s is a later band it can wear", p.template, p.slot, other.ID)
			}
		}
	}
	if len(slots) != len(data.EquipSlots()) {
		t.Errorf("gear for %d of %d slots: %v", len(slots), len(data.EquipSlots()), sd.gear)
	}
	if weapon, _ := data.Item(sd.gear[0].template); weapon.Class != "warrior" {
		t.Errorf("a Warrior's weapon is %s, a %q one", weapon.ID, weapon.Class)
	}
	want := map[string]int{"white_potion": 100}
	for _, p := range sd.potions {
		if want[p.template] != p.quantity {
			t.Errorf("%d %s", p.quantity, p.template)
		}
		delete(want, p.template)
	}
	if len(want) > 0 {
		t.Errorf("no %v among %v", want, sd.potions)
	}
}

func TestThePromotableTesterMeetsEveryConditionButHandingInItsTrial(t *testing.T) {
	data := gamedatatest.Shipped(t)
	sd, err := plan(data, testerNamed(t, "test_promote"))
	if err != nil {
		t.Fatal(err)
	}
	warrior, _ := data.Class("warrior")
	if sd.class.ID != data.StartClass().ID || sd.level != warrior.Promotion.Level {
		t.Fatalf("a %s at level %d; want a Beginner at the Warrior's promotion level", sd.class.ID, sd.level)
	}
	own := progression.AddStats(data.StartClass().BaseStats, sd.stats)
	if own.STR < warrior.Promotion.Stats.STR || int(sd.skills["strike"]) < warrior.Promotion.Skills["strike"] {
		t.Errorf("own %+v and skills %v; the Warrior's promotion asks STR %d and Strike %d", own, sd.skills, warrior.Promotion.Stats.STR, warrior.Promotion.Skills["strike"])
	}
	trial, _ := data.Quest("warriors_trial")
	q := sd.quests[0]
	if len(sd.quests) != 1 || q.GetQuestId() != trial.ID || q.GetStatus() != characterv1.QuestStatus_QUEST_STATUS_ACTIVE || int(q.GetProgress()[0]) != trial.Objectives[0].Count {
		t.Errorf("quests %v; want the Warrior's Trial active and done", sd.quests)
	}
	if len(sd.gear) != 0 || len(sd.potions) != 0 {
		t.Errorf("gear %v and potions %v; want neither", sd.gear, sd.potions)
	}
}

func TestTheNewTesterIsLeftAsCreated(t *testing.T) {
	data := gamedatatest.Shipped(t)
	sd, err := plan(data, testerNamed(t, "test_new"))
	if err != nil {
		t.Fatal(err)
	}
	if sd.level != 1 || sd.class.ID != data.StartClass().ID || sd.unspent != 0 || len(sd.skills) != 0 || len(sd.quests) != 0 || len(sd.gear) != 0 {
		t.Errorf("%+v; want a Beginner at level 1 with nothing more", sd)
	}
}

func TestOnlyALocalGatewayIsSeededUnlessRemote(t *testing.T) {
	for url, want := range map[string]bool{
		"http://localhost:8080": true, "http://127.0.0.1:48080": true, "http://[::1]:8080": true,
		"http://gateway:8080":       true, // a Compose service on the stack's own network
		"https://finga.example.com": false, "http://10.0.0.5:8080": false, "not a url\x7f": false,
	} {
		if got := local(url); got != want {
			t.Errorf("local(%q) = %v; want %v", url, got, want)
		}
	}
}

func TestGearIsWornWhereItsSlotDoesntHoldItAlready(t *testing.T) {
	gear := []piece{{template: "knight_sword", slot: "weapon"}, {template: "silver_ring", slot: "ring_1"}, {template: "silver_ring", slot: "ring_2"}, {template: "knight_helm", slot: "hat"}}
	items := []*itemv1.Item{
		{Id: "club", TemplateId: "wooden_club", EquipSlot: "weapon"}, // a starter worn in its slot
		{Id: "sword", TemplateId: "knight_sword"},
		{Id: "ring-a", TemplateId: "silver_ring", EquipSlot: "ring_1"},
		{Id: "ring-b", TemplateId: "silver_ring"},
	}
	got := toWear(items, gear)
	want := []wearing{{item: "sword", slot: "weapon"}, {item: "ring-b", slot: "ring_2"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("wear %v; want %v (the helm isn't in the bag)", got, want)
	}
}

func TestRateLimitedIsTheGatewaysOwnCode(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/errors/codes.json")
	if err != nil {
		t.Fatal(err)
	}
	type entry struct {
		Code int32  `json:"code"`
		Name string `json:"name"`
	}
	var codes []entry
	if err := json.Unmarshal(raw, &codes); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(codes, entry{rateLimited, "RATE_LIMITED"}) {
		t.Errorf("code %d is not the gateway's RATE_LIMITED", rateLimited)
	}
}
