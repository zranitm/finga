package main

import (
	"encoding/json"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/gamedata"
	"finga/internal/gamedata/load"
	"finga/internal/kernel"
	"finga/internal/sim"
)

func testWorld(t *testing.T) (*gamedata.Data, *world) {
	t.Helper()
	data, err := load.Dir("../../data")
	if err != nil {
		t.Fatal(err)
	}
	climbs, err := loadClimbs("../../data", data)
	if err != nil {
		t.Fatal(err)
	}
	return data, newWorld(data, climbs)
}

func class(t *testing.T, data *gamedata.Data, id string) gamedata.Class {
	t.Helper()
	c, ok := data.Class(kernel.Class(id))
	if !ok {
		t.Fatalf("no class %s", id)
	}
	return c
}

func TestTheBotsWalkTheTownAndTheMapsUpToAPromotionsLevel(t *testing.T) {
	_, w := testWorld(t)
	if got := slices.Sorted(maps.Keys(w.maps)); !slices.Equal(got, []string{"cave", "forest", "town"}) {
		t.Fatalf("the bots' maps are %v", got)
	}
	for _, c := range []struct{ from, to, portal string }{
		{"town", "cave", "to_forest"},
		{"cave", "town", "from_forest"},
		{"forest", "cave", "to_cave"},
	} {
		if p, ok := w.portalTo(c.from, c.to); !ok || p.Name != c.portal {
			t.Errorf("from %s to %s takes %q, want %q", c.from, c.to, p.Name, c.portal)
		}
	}
	if _, ok := w.portalTo("shroom_grove", "town"); ok {
		t.Error("a way from a map the bots don't walk")
	}
}

func TestAMobIsHuntedOnTheGroundElseUpTheClimb(t *testing.T) {
	_, w := testWorld(t)
	boars := w.spots("wild_boar")
	if len(boars) == 0 || boars[0] != (spot{mapID: "cave", from: 224, to: 656, y: 608}) {
		t.Errorf("boars are hunted at %+v, want the cave's floor first", boars)
	}
	bats := w.spots("cave_bat")
	if len(bats) != 1 || bats[0].mapID != "cave" || bats[0].climb == nil || bats[0].from != 1400 || bats[0].to != 1672 || bats[0].y != 384 {
		t.Errorf("bats are hunted at %+v, want up the cave's climb on the pillar", bats)
	}
	slimes := spot{mapID: "forest", from: 224, to: 776, y: 608}
	forest := spot{mapID: "forest", from: 224, to: 1776, y: 608} // the slimes' field to the mushrooms' end
	for _, c := range []struct {
		level int
		want  spot
	}{{1, slimes}, {4, slimes}, {5, forest}, {10, forest}, {11, boars[0]}, {20, boars[0]}} {
		if s, ok := w.levelSpot(c.level); !ok || s != c.want {
			t.Errorf("level %d hunts at %+v, want %+v", c.level, s, c.want)
		}
	}
}

func TestABotWorksItsTrialThenPromotes(t *testing.T) {
	data, w := testWorld(t)
	warrior, thief, magician := class(t, data, "warrior"), class(t, data, "thief"), class(t, data, "magician")
	active := func(progress ...int32) *clientv1.QuestState {
		return &clientv1.QuestState{Status: clientv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: progress}
	}
	done := &clientv1.QuestState{Status: clientv1.QuestStatus_QUEST_STATUS_FINISHED, TimesFinished: 1}
	holding := func(n int) func(string) int { return func(string) int { return n } }
	ready := gamedata.Stats{STR: 30, DEX: 7, INT: 7, AGI: 7, VIT: 7}
	for _, c := range []struct {
		name   string
		future gamedata.Class
		p      plan
		failed bool
		want   string // the goal's why
		where  string // its map
	}{
		{"under the trial's level", warrior, plan{class: "beginner", level: 3, holding: holding(0)}, false, "for warriors_trial's level", "forest"},
		{"at its level", warrior, plan{class: "beginner", level: 10, holding: holding(0)}, false, "to take warriors_trial", "town"},
		{"boars to kill", warrior, plan{class: "beginner", level: 10, quests: map[string]*clientv1.QuestState{"warriors_trial": active(5)}, holding: holding(0)}, false, "wild_boar for warriors_trial", "cave"},
		{"the boars killed", warrior, plan{class: "beginner", level: 10, quests: map[string]*clientv1.QuestState{"warriors_trial": active(20)}, holding: holding(0)}, false, "to hand in warriors_trial", "town"},
		{"every condition met", warrior, plan{class: "beginner", level: 10, own: ready, invested: map[string]int32{"strike": 10}, quests: map[string]*clientv1.QuestState{"warriors_trial": done}, holding: holding(0)}, false, "to promote", "town"},
		{"a skill point short", warrior, plan{class: "beginner", level: 10, own: ready, invested: map[string]int32{"strike": 9}, quests: map[string]*clientv1.QuestState{"warriors_trial": done}, holding: holding(0)}, false, "for the promotion", "forest"},
		{"promoted", warrior, plan{class: "warrior", level: 12, holding: holding(0)}, false, "as a warrior", "cave"},
		{"wings to collect", thief, plan{class: "beginner", level: 10, quests: map[string]*clientv1.QuestState{"thiefs_trial": active(0)}, holding: holding(3)}, false, "cave_bat for thiefs_trial", "cave"},
		{"the climb failed", thief, plan{class: "beginner", level: 10, quests: map[string]*clientv1.QuestState{"thiefs_trial": active(0)}, holding: holding(3)}, true, "thiefs_trial's drop is out of reach", "forest"},
		{"the gel held, the guide not met", magician, plan{class: "beginner", level: 10, quests: map[string]*clientv1.QuestState{"magicians_trial": active(0, 0)}, holding: holding(15)}, false, "for magicians_trial", "town"},
		{"the gel short", magician, plan{class: "beginner", level: 10, quests: map[string]*clientv1.QuestState{"magicians_trial": active(0, 1)}, holding: holding(14)}, false, "green_slime for magicians_trial", "forest"},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := w.next(c.future, c.p, c.failed)
			where := g.spot.mapID
			if g.kind == talkGoal {
				where = g.npc.MapID
			}
			if g.why != c.want || where != c.where {
				t.Fatalf("goal %q on %s, want %q on %s", g.why, where, c.want, c.where)
			}
		})
	}
}

func TestPointsGoWhereThePromotionAsksFirst(t *testing.T) {
	split := gamedata.Stats{STR: 2, DEX: 2, AGI: 1} // the Bowman's: 25 DEX at level 10 on its own
	if got := statPoints(10, gamedata.Stats{DEX: 25}, gamedata.Stats{DEX: 30}, split); got != (gamedata.Stats{STR: 2, DEX: 7, AGI: 1}) {
		t.Errorf("10 points with 5 DEX to go spend %+v", got)
	}
	if got := statPoints(3, gamedata.Stats{DEX: 30}, gamedata.Stats{DEX: 30}, split); got != (gamedata.Stats{}) {
		t.Errorf("3 points, the need met, spend %+v; want them kept for a whole split", got)
	}
	if got := skillPoints(5, map[string]int32{"strike": 8}, map[string]int{"strike": 10}, "strike"); !maps.Equal(got, map[string]int32{"strike": 5}) {
		t.Errorf("skill points before promotion go %v", got)
	}
	if got := skillPoints(5, map[string]int32{"strike": 10}, nil, "slash_blast"); !maps.Equal(got, map[string]int32{"slash_blast": 5}) {
		t.Errorf("skill points after promotion go %v", got)
	}
}

func TestTheBotPromotesOnlyWhenTheTrainerShowsEveryConditionMet(t *testing.T) {
	data, _ := testWorld(t)
	offer := func(have int32) *clientv1.PromotionOffer {
		return &clientv1.PromotionOffer{Class: "warrior", Conditions: []*clientv1.PromotionCondition{
			{Kind: clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_LEVEL, Need: 10, Have: 10},
			{Kind: clientv1.PromotionConditionKind_PROMOTION_CONDITION_KIND_SKILL, Id: "strike", Need: 10, Have: have},
		}}
	}
	b := &bot{data: data, future: class(t, data, "warrior")}
	met := b.answer(&clientv1.NpcDialog{NpcId: "warrior_trainer", Promotion: offer(10)})
	if want := []proto.Message{&clientv1.Promote{NpcId: "warrior_trainer", Class: "warrior"}}; !slices.EqualFunc(met, want, proto.Equal) {
		t.Errorf("every condition met answers %v", met)
	}
	if got := b.answer(&clientv1.NpcDialog{NpcId: "warrior_trainer", Promotion: offer(9)}); len(got) != 0 {
		t.Errorf("a condition short answers %v", got)
	}
	b.future = class(t, data, "thief")
	if got := b.answer(&clientv1.NpcDialog{NpcId: "warrior_trainer", Promotion: offer(10)}); len(got) != 0 {
		t.Errorf("another class's trainer answers %v", got)
	}
}

func TestTheCaveClimbReachesThePillarByTheGamesOwnMovement(t *testing.T) {
	data, w := testWorld(t)
	c, cave, mv := w.climbs["cave"], w.maps["cave"], data.Movement()
	ground := cave.PlayerSpawn.Y
	for _, start := range []struct {
		from sim.State
		hop  bool // jumping whenever it walks, as a bot did live when its updates came in bursts
	}{
		{sim.State{X: 100, Y: ground, OnGround: true}, false},  // the entrance
		{sim.State{X: 1000, Y: ground, OnGround: true}, false}, // under the shelf
		{sim.State{X: 1299, Y: ground, OnGround: true}, false}, // by the pillar's west face, where its rope is nearer but walled off
		{sim.State{X: 1290, Y: 500}, false},                    // falling there, as knocked off the shelf
		{sim.State{X: 1800, Y: ground, OnGround: true}, false}, // east of the pillar
		{sim.State{X: 100, Y: ground, OnGround: true}, true},
		{sim.State{X: 872, Y: ground, OnGround: true}, true}, // where a hop carried it over the floor rope and back again
	} {
		from := start.from
		s := from
		s.Facing, s.Ground, s.DropPlatform = 1, sim.NoPlatform, sim.NoPlatform
		stuck, held := stall{x: s.X}, &clientv1.Input{}
		for step := 0; !c.on(s.X, s.Y); step++ {
			if float64(step)*mv.Dt > 120 {
				t.Fatalf("from (%.0f, %.0f), hopping %v: not on the pillar after two minutes, at (%.0f, %.0f)", from.X, from.Y, start.hop, s.X, s.Y)
			}
			jump := stuck.jump(s.X, s.OnGround, s.Climbing, held)
			in := c.step(s.X, s.Y, s.Climbing, s.OnGround, held)
			held = in
			s = sim.Step(s, sim.Input{Left: in.GetLeft(), Right: in.GetRight(), Up: in.GetUp(), Jump: (start.hop || jump) && (in.GetLeft() || in.GetRight())}, cave.Geometry, mv)
		}
	}
}

func TestAStallIsAWayPressedOnTheGroundThatGoesNowhere(t *testing.T) {
	right, still := &clientv1.Input{Right: true}, &clientv1.Input{}
	for _, c := range []struct {
		name               string
		onGround, climbing bool
		held               *clientv1.Input
		want               bool
	}{
		{"against a wall", true, false, right, true},
		{"in the air, its updates late", false, false, right, false},
		{"on a rope", false, true, right, false},
		{"standing to attack", true, false, still, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := stall{x: 500}
			for k := range 4 {
				if got := s.jump(500, c.onGround, c.climbing, c.held); got != (c.want && k == 3) {
					t.Fatalf("decision %d: jump %v", k+1, got)
				}
			}
		})
	}
	s := stall{x: 500}
	for range 3 {
		s.jump(500, true, false, right)
	}
	for k := range 3 {
		if s.jump(501, true, false, right) {
			t.Fatalf("decision %d after a step on: jump; the step starts the count again", k+1)
		}
	}
}

func TestAFullTabGivesUpItsLastItemNoQuestCollects(t *testing.T) {
	items := map[string]*clientv1.ItemView{}
	for _, it := range []*clientv1.ItemView{
		{ItemId: "sword", Template: "wooden_sword", Tab: "equip", Slot: 0},
		{ItemId: "cap", Template: "leather_cap", Tab: "equip", Slot: 7},
		{ItemId: "worn", Template: "iron_helm"},
		{ItemId: "wing", Template: "bat_wing", Tab: "etc", Slot: 2},
		{ItemId: "gel", Template: "slime_gel", Tab: "etc", Slot: 9},
	} {
		items[it.GetTab()+"/"+it.GetItemId()] = it
	}
	collects := func(template string) bool { return template == "slime_gel" }
	for tab, want := range map[string]string{"equip": "cap", "etc": "wing", "use": ""} {
		if got := spare(items, tab, collects).GetItemId(); got != want {
			t.Errorf("a full %s tab gives up %q, want %q", tab, got, want)
		}
	}
}

func TestTheTabFullCodesAreTheGatewaysOwn(t *testing.T) {
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
	for code, tab := range tabFull {
		if want := strings.ToUpper(tab) + "_TAB_FULL"; !slices.Contains(codes, entry{code, want}) {
			t.Errorf("code %d is not the gateway's %s", code, want)
		}
	}
}

func TestAClimbNamingWhatTheMapLacksFailsAtStart(t *testing.T) {
	data, err := load.Dir("../../data")
	if err != nil {
		t.Fatal(err)
	}
	saved := climbRoutes
	t.Cleanup(func() { climbRoutes = saved })
	climbRoutes = map[string][]string{"cave": {"floor_rope", "no_such_rope", "pillar_top"}}
	if _, err := loadClimbs("../../data", data); err == nil || !strings.Contains(err.Error(), "no_such_rope") {
		t.Fatalf("a missing rope loads with %v", err)
	}
}

func TestABotGoesAfterTheNearestPreyOnItsSpot(t *testing.T) {
	_, w := testWorld(t)
	s := spot{mapID: "forest", from: 224, to: 1776, y: 608}
	mob := func(id uint32, kind string, x, y float64) *clientv1.MobView {
		return &clientv1.MobView{Id: id, Type: kind, X: x, Y: y}
	}
	mobs := map[uint32]*clientv1.MobView{
		1: mob(1, "green_slime", 300, 400),      // up on a branch
		2: mob(2, "wild_boar", 520, 608),        // not its prey
		3: mob(3, "orange_mushroom", 1900, 608), // past the spot's end
		4: mob(4, "green_slime", 760, 608),
		5: mob(5, "orange_mushroom", 300, 608),
		6: mob(6, "green_slime", 300, 608), // as near as 5: the lower ID wins
	}
	if got := nearest(mobs, s, []string{"green_slime", "orange_mushroom"}, 500); got.GetId() != 5 {
		t.Errorf("goes after mob %d, want 5", got.GetId())
	}
	if got := nearest(mobs, s, []string{"wild_boar", "cave_bat"}, 500); got.GetId() != 2 {
		t.Errorf("goes after mob %d, want the boar", got.GetId())
	}
	if got := nearest(mobs, s, []string{"cave_bat"}, 500); got != nil {
		t.Errorf("goes after %v with none of its prey there", got)
	}
	if got := nearest(mobs, spot{from: 400, to: 600, y: 608}, nil, 500); got.GetId() != 2 {
		t.Errorf("clearing its way, goes after mob %d, want the boar beside it", got.GetId())
	}
	if got := w.levelPrey(5); !slices.Equal(got, []string{"green_slime", "orange_mushroom"}) {
		t.Errorf("a level 5 bot hunts %v", got)
	}
}

func TestTheBotCountsTheTrialAmongThePromotionsConditions(t *testing.T) {
	data, _ := testWorld(t)
	p := plan{level: 10, own: gamedata.Stats{STR: 42, DEX: 7, INT: 7, AGI: 7, VIT: 7}, invested: map[string]int32{"strike": 45},
		quests: map[string]*clientv1.QuestState{"warriors_trial": {Status: clientv1.QuestStatus_QUEST_STATUS_ACTIVE}}}
	warrior := class(t, data, "warrior").Promotion
	if meets(warrior, p) {
		t.Error("met with the trial under way, so a trainer's no would hold the bot back past its hand-in")
	}
	p.quests["warriors_trial"] = &clientv1.QuestState{Status: clientv1.QuestStatus_QUEST_STATUS_FINISHED, TimesFinished: 1}
	if !meets(warrior, p) {
		t.Error("not met with the trial finished")
	}
}
