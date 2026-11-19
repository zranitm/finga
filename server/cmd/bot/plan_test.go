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
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/kernel"
	"finga/internal/shared/nav"
	"finga/pkg/wsconn"
)

func testWorld(t *testing.T) (*gamedata.Data, *world) {
	t.Helper()
	data := gamedatatest.Shipped(t)
	return data, newWorld(data)
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

func TestAMobIsHuntedAtItsWidestSpawnAndALevelRoundItsMapsPrey(t *testing.T) {
	data, w := testWorld(t)
	slimes := w.spots("green_slime")
	if len(slimes) != 2 || slimes[0].mapID != "forest" || slimes[0].radius < slimes[1].radius {
		t.Errorf("slimes are hunted at %+v, want the forest's widest circle first", slimes)
	}
	if got := w.spots("vampire_bat"); len(got) != 0 {
		t.Errorf("a field boss is hunted at %+v", got)
	}
	for _, c := range []struct {
		level int
		mapID string
	}{{1, "forest"}, {4, "forest"}, {5, "forest"}, {10, "forest"}, {11, "cave"}, {20, "cave"}} {
		s, ok := w.levelSpot(c.level)
		if !ok || s.mapID != c.mapID {
			t.Errorf("level %d hunts on %q, want %s", c.level, s.mapID, c.mapID)
			continue
		}
		m, _ := data.Map(c.mapID)
		for _, sp := range m.Spawns {
			if !slices.Contains(w.levelPrey(c.level), sp.Mob) {
				continue
			}
			if d := nav.Distance(s.at, point(sp.At)) + sp.Radius; d > s.radius+1e-9 {
				t.Errorf("level %d's spot %+v leaves %s's spawn at %+v out", c.level, s, sp.Mob, sp.At)
			}
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
		want   string // the goal's why
		where  string // its map
	}{
		{"under the trial's level", warrior, plan{class: "beginner", level: 3, holding: holding(0)}, "for warriors_trial's level", "forest"},
		{"at its level", warrior, plan{class: "beginner", level: 10, holding: holding(0)}, "to take warriors_trial", "town"},
		{"boars to kill", warrior, plan{class: "beginner", level: 10, quests: map[string]*clientv1.QuestState{"warriors_trial": active(5)}, holding: holding(0)}, "wild_boar for warriors_trial", "cave"},
		{"the boars killed", warrior, plan{class: "beginner", level: 10, quests: map[string]*clientv1.QuestState{"warriors_trial": active(20)}, holding: holding(0)}, "to hand in warriors_trial", "town"},
		{"every condition met", warrior, plan{class: "beginner", level: 10, own: ready, invested: map[string]int32{"strike": 10}, quests: map[string]*clientv1.QuestState{"warriors_trial": done}, holding: holding(0)}, "to promote", "town"},
		{"a skill point short", warrior, plan{class: "beginner", level: 10, own: ready, invested: map[string]int32{"strike": 9}, quests: map[string]*clientv1.QuestState{"warriors_trial": done}, holding: holding(0)}, "for the promotion", "forest"},
		{"promoted", warrior, plan{class: "warrior", level: 12, holding: holding(0)}, "as a warrior", "cave"},
		{"wings to collect", thief, plan{class: "beginner", level: 10, quests: map[string]*clientv1.QuestState{"thiefs_trial": active(0)}, holding: holding(3)}, "cave_bat for thiefs_trial", "cave"},
		{"the gel held, the guide not met", magician, plan{class: "beginner", level: 10, quests: map[string]*clientv1.QuestState{"magicians_trial": active(0, 0)}, holding: holding(15)}, "for magicians_trial", "town"},
		{"the gel short", magician, plan{class: "beginner", level: 10, quests: map[string]*clientv1.QuestState{"magicians_trial": active(0, 1)}, holding: holding(14)}, "green_slime for magicians_trial", "forest"},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := w.next(c.future, c.p)
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

func TestABotGoesAfterTheNearestPreyOnItsSpot(t *testing.T) {
	_, w := testWorld(t)
	s := spot{mapID: "forest", at: nav.Point{X: 20, Z: 20}, radius: 8}
	mob := func(id uint32, kind string, x, z float64) *clientv1.MobView {
		return &clientv1.MobView{Id: id, Type: kind, X: x, Z: z, Hp: 10}
	}
	dead := mob(7, "green_slime", 20, 20)
	dead.Hp = 0
	mobs := map[uint32]*clientv1.MobView{
		1: mob(1, "green_slime", 20, 31.5),   // past the spot's margin
		2: mob(2, "wild_boar", 22, 20),       // not its prey
		3: mob(3, "orange_mushroom", 35, 20), // past the spot's margin
		4: mob(4, "green_slime", 27, 20),
		5: mob(5, "orange_mushroom", 18, 20),
		6: mob(6, "green_slime", 18, 20), // as near as 5: the lower ID wins
		7: dead,
	}
	from := nav.Point{X: 20, Z: 20}
	if got := nearest(mobs, s, []string{"green_slime", "orange_mushroom"}, from, nil); got.GetId() != 5 {
		t.Errorf("goes after mob %d, want 5", got.GetId())
	}
	if got := nearest(mobs, s, []string{"wild_boar", "cave_bat"}, from, nil); got.GetId() != 2 {
		t.Errorf("goes after mob %d, want the boar", got.GetId())
	}
	if got := nearest(mobs, s, []string{"cave_bat"}, from, nil); got != nil {
		t.Errorf("goes after %v with none of its prey there", got)
	}
	if got := nearest(mobs, spot{at: nav.Point{X: 22, Z: 20}}, nil, from, nil); got.GetId() != 2 {
		t.Errorf("on a small spot, goes after mob %d, want the boar on it", got.GetId())
	}
	prey := []string{"green_slime", "orange_mushroom"}
	if got := nearest(mobs, s, prey, from, func(m *clientv1.MobView) bool { return m.GetId() == 5 }); got.GetId() != 6 {
		t.Errorf("skipping mob 5, goes after mob %d, want 6", got.GetId())
	}
	if got := w.levelPrey(5); !slices.Equal(got, []string{"green_slime", "orange_mushroom"}) {
		t.Errorf("a level 5 bot hunts %v", got)
	}
	if got := w.levelPrey(10); slices.Contains(got, "king_slime") || !slices.Contains(got, "orange_mushroom") {
		t.Errorf("a level 10 bot hunts %v; want the forest's mobs, not its field boss", got)
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

// C12: a bot pressing into a mob at its height fights it; one beside, above or behind doesn't stop it.
func TestABotFightsTheMobInItsWay(t *testing.T) {
	data := gamedatatest.Shipped(t)
	right, left, upRight := &clientv1.Input{Right: true}, &clientv1.Input{Left: true}, &clientv1.Input{Up: true, Right: true}
	slime := func(x, y, z float64) *clientv1.MobView { // radius 0.5 to the bot's 0.375
		return &clientv1.MobView{Id: 1, Type: "green_slime", X: x, Y: y, Z: z, Hp: 10}
	}
	for _, c := range []struct {
		name string
		held *clientv1.Input
		mob  *clientv1.MobView
		want bool
	}{
		{"pressed into its side", right, slime(11, 0, 10), true},
		{"pressed into it from the east", left, slime(9, 0, 10), true},
		{"partly in, as lag leaves it", right, slime(10.7, 0, 10), true},
		{"ahead on a diagonal", upRight, slime(10.8, 0, 10.8), true},
		{"wholly inside it", right, slime(10, 0, 10), false},
		{"a step short of it", right, slime(11.5, 0, 10), false},
		{"beside the way", &clientv1.Input{Up: true}, slime(11, 0, 10), false},
		{"walking away from it", left, slime(11, 0, 10), false},
		{"standing still beside it", &clientv1.Input{}, slime(11, 0, 10), false},
		{"on a ledge above", right, slime(11, 1.8, 10), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := &bot{data: data, x: 10, z: 10, held: c.held, mobs: map[uint32]*clientv1.MobView{1: c.mob}}
			if got := b.inTheWay() != nil; got != c.want {
				t.Fatalf("in the way %v, want %v", got, c.want)
			}
		})
	}
}

// C12: a map's boss stands for a bot until it hears it beaten there, from the fall or a Snapshot, and again once it's back or the bot sees it appear; a map without one has none.
func TestABotKeepsOffAMapWhileItsBossIsUp(t *testing.T) {
	data := gamedatatest.Shipped(t)
	b := &bot{data: data, mapID: "forest"}
	if !b.bossUp("cave") || !b.bossUp("forest") || b.bossUp("town") {
		t.Fatalf("hearing nothing: cave %v, forest %v, town %v; want the bosses up, the town none", b.bossUp("cave"), b.bossUp("forest"), b.bossUp("town"))
	}
	b.observe(wsconn.Message{Data: &clientv1.FieldBoss{MobId: 7, Type: "king_slime"}})
	b.observe(wsconn.Message{Data: &clientv1.FieldBossGone{MobId: 7, Type: "king_slime", BackInMs: 60_000}})
	if b.bossUp("forest") || !b.bossUp("cave") || b.boss != 0 {
		t.Fatalf("after the forest's boss fell: forest %v, cave %v, its mob %d", b.bossUp("forest"), b.bossUp("cave"), b.boss)
	}
	b.observe(wsconn.Message{Data: &clientv1.FieldBoss{MobId: 8, Type: "king_slime"}})
	if !b.bossUp("forest") || b.boss != 8 {
		t.Fatalf("a boss seen back: up %v, its mob %d", b.bossUp("forest"), b.boss)
	}
	b.observe(wsconn.Message{Data: &clientv1.Snapshot{FieldBossGone: &clientv1.FieldBossGone{MobId: 8, Type: "king_slime", BackInMs: 60_000}}})
	if b.bossUp("forest") {
		t.Fatal("a Snapshot of the boss down left it up")
	}
	b.observe(wsconn.Message{Data: &clientv1.FieldBossGone{MobId: 8, Type: "king_slime"}})
	if !b.bossUp("forest") {
		t.Fatal("a boss whose time back has come is still down")
	}
}
