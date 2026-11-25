package main

import (
	"maps"
	"slices"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/nav"
)

// regionWorld is a soak's world on the test region.
func regionWorld(t *testing.T) (*gamedata.Data, *world) {
	t.Helper()
	data := gamedatatest.Shipped(t)
	w, err := newWorld(data, "test_region")
	if err != nil {
		t.Fatal(err)
	}
	return data, w
}

// A soak's bots walk its region alone, and only hunt there; a map that isn't a region is refused, and an area's boss is never prey.
func TestASoakWalksItsRegionAlone(t *testing.T) {
	data, w := regionWorld(t)
	if got := slices.Sorted(maps.Keys(w.maps)); !slices.Equal(got, []string{"test_region"}) || w.region.ID != "test_region" {
		t.Fatalf("the soak's maps are %v", got)
	}
	for _, bad := range []string{"town", "nowhere"} {
		if _, err := newWorld(data, bad); err == nil {
			t.Errorf("a soak in %s", bad)
		}
	}
	for _, boss := range []string{"king_slime", "vampire_bat", "spore_mother", "moth_empress"} {
		if slices.Contains(w.levelPrey(99), boss) {
			t.Errorf("the area boss %s is prey", boss)
		}
	}
	if g := w.next(class(t, data, "warrior"), plan{class: "beginner", level: 10, holding: func(string) int { return 0 }}); g.kind != huntGoal || g.spot.mapID != "test_region" || g.why != "in Test Region" {
		t.Errorf("a soak's bot at its trial's level goes %q on %s; want a hunt in the test region", g.why, g.spot.mapID)
	}
}

// In a region a bot hunts at a camp of its prey in an area of its band, its index picking among them; in no band, among those of the highest band under it (large-regions.md section 11).
func TestARegionsBotHuntsACampOfItsBand(t *testing.T) {
	data, w := regionWorld(t)
	m, _ := data.Map("test_region")
	for _, c := range []struct {
		level int
		camps []string // the mobs of the camps it picks among, in the plan's order
	}{
		{1, []string{"green_slime"}},
		{5, []string{"green_slime", "orange_mushroom"}}, // between the west's band and the south's
		{12, []string{"wild_boar"}},                     // the bats, 14, aren't its prey yet
		{15, []string{"wild_boar", "cave_bat"}},         // its band's spore slimes, 15, aren't its prey yet
		{16, []string{"spore_slime"}},
		{30, []string{"glow_slime"}}, // past every band; the snail is an elite
	} {
		for pick := range len(c.camps) + 1 {
			s, ok := w.levelSpot(c.level, pick)
			k := slices.IndexFunc(m.Spawns, func(sp gamedata.Spawn) bool { return point(sp.At) == s.at && sp.Radius == s.radius })
			if want := c.camps[pick%len(c.camps)]; !ok || s.mapID != "test_region" || k < 0 || m.Spawns[k].Mob != want {
				t.Errorf("level %d, pick %d hunts at %+v; want the %s camp", c.level, pick, s, want)
			}
		}
	}
}

// Where bands overlap, a bot hunts in every band that holds its level, not only the highest: the south's widened to 10–16 shares level 16 with the north's 15–18.
func TestARegionsBotHuntsInEveryBandHoldingItsLevel(t *testing.T) {
	data, w := regionWorld(t)
	shipped, _ := data.Map("test_region")
	m := *shipped
	m.Areas = slices.Clone(shipped.Areas)
	k := slices.IndexFunc(m.Areas, func(a gamedata.Area) bool { return a.ID == "south" })
	m.Areas[k].MaxLevel = 16
	want := []string{"wild_boar", "cave_bat", "spore_slime"}
	for pick := range len(want) {
		s, ok := w.campSpot(&m, 16, pick)
		i := slices.IndexFunc(m.Spawns, func(sp gamedata.Spawn) bool { return point(sp.At) == s.at && sp.Radius == s.radius })
		if !ok || i < 0 || m.Spawns[i].Mob != want[pick] {
			t.Errorf("pick %d hunts at %+v; want the %s camp", pick, s, want[pick])
		}
	}
}

// Past every band, a bot hunts in the highest band under its level alone, whatever order the plan lists the camps in.
func TestARegionsBotPastEveryBandHuntsInTheHighest(t *testing.T) {
	data, w := regionWorld(t)
	shipped, _ := data.Map("test_region")
	m := *shipped
	m.Spawns = slices.Clone(shipped.Spawns)
	slices.Reverse(m.Spawns)
	for pick := range 3 {
		s, ok := w.campSpot(&m, 30, pick)
		i := slices.IndexFunc(m.Spawns, func(sp gamedata.Spawn) bool { return point(sp.At) == s.at && sp.Radius == s.radius })
		if !ok || i < 0 || m.Spawns[i].Mob != "glow_slime" {
			t.Errorf("pick %d hunts at %+v; want the glow slimes' camp", pick, s)
		}
	}
}

// A soak's bot off its region teleports to its main town, holding still till it lands; once there it stays.
func TestASoaksBotTeleportsIntoItsRegion(t *testing.T) {
	data, w := regionWorld(t)
	b := &bot{data: data, world: w, mapID: "town", held: &clientv1.Input{Up: true}}
	if !b.enterRegion() || len(b.actions) != 1 || b.actions[0].Data.(*clientv1.Teleport).GetWaypoint() != "town" || b.held.GetUp() || !b.channelling() {
		t.Fatalf("off the region: actions %v, held %v", b.actions, b.held)
	}
	b.teleportEnded(true) // cancelled
	b.actions = nil
	if !b.enterRegion() || len(b.actions) != 0 {
		t.Fatalf("backed off: actions %v", b.actions)
	}
	b.mapID = "test_region"
	if b.enterRegion() {
		t.Fatal("in the region, still going there")
	}
}

// A long trip in a region teleports to the far camp's nearest waypoint once it's found, holding still while it channels; a camp near, or near its waypoint already, is walked to.
func TestARegionsBotTeleportsToAFarCampsWaypoint(t *testing.T) {
	data, w := regionWorld(t)
	b := &bot{data: data, world: w, mapID: "test_region", found: map[string]bool{"west": true}, held: &clientv1.Input{}}
	// The glow slimes' camp, 25 m off the woods' waypoint; the bot stands by the west field's.
	glow := spot{mapID: "test_region", at: nav.Point{X: 215, Z: 100}, radius: 6}
	b.x, b.z = 60, 87
	if b.tripTo(glow) || len(b.actions) != 0 {
		t.Fatal("teleported to a waypoint not found")
	}
	b.found["woods"] = true
	if !b.tripTo(glow) || len(b.actions) != 1 || b.actions[0].Data.(*clientv1.Teleport).GetWaypoint() != "woods" || !b.channelling() {
		t.Fatalf("a far trip: actions %v", b.actions)
	}
	b.teleportAt = time.Now().Add(-teleportWait - time.Millisecond)
	if b.channelling() {
		t.Error("still channelling past the wait")
	}
	b.actions = nil
	b.teleportEnded(true)
	if b.tripTo(glow) {
		t.Error("teleported while backed off")
	}
	b.teleportAfter = time.Time{}
	b.x, b.z = 185, 100
	if b.tripTo(glow) {
		t.Error("teleported for a camp near")
	}
	b.mapID = "town"
	if b.tripTo(spot{mapID: "town", at: nav.Point{X: 500, Z: 500}}) {
		t.Error("teleported on a map with no waypoints")
	}
}

// A region's bot keeps clear of every area's boss circle, as it hears only its own area's boss.
func TestARegionsBotKeepsClearOfItsAreaBosses(t *testing.T) {
	data, _ := regionWorld(t)
	b := &bot{data: data, mapID: "test_region"}
	if !b.nearBoss("test_region", nav.Point{X: 48, Z: 112}) || !b.nearBoss("test_region", nav.Point{X: 200, Z: 200}) {
		t.Error("by the west's king slime or the woods' moth empress, not near a boss")
	}
	if b.nearBoss("test_region", nav.Point{X: 56, Z: 50}) {
		t.Error("at the slimes' camp, near a boss")
	}
}

// A meter keeps what a bot is sent, and its busiest whole second's.
func TestAMeterKeepsTheBusiestWholeSecond(t *testing.T) {
	var m meter
	t0 := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	m.add(100, t0)
	m.add(50, t0.Add(500*time.Millisecond))
	m.add(400, t0.Add(1200*time.Millisecond))
	if total, peak := m.read(); total != 550 || peak != 150 {
		t.Fatalf("read %d, %d; want 550 all told, 150 the busiest second ended", total, peak)
	}
	m.add(10, t0.Add(2100*time.Millisecond))
	if total, peak := m.read(); total != 560 || peak != 400 {
		t.Fatalf("read %d, %d; want 560, and 400 once its second ended", total, peak)
	}
}
