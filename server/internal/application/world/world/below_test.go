package world_test

import (
	"context"
	"math"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
)

// above is a small town region on the land with the gates the place below and the region beside lead to, and no mob to step while players wait out a channel's idle.
const above = `name: Test Above
size: [128, 128]
cell: 1
seed: 9
look: {sky: day}
ground: {noise: {scale: 60, amplitude: 1, octaves: 1}}
edge: {width: 16, height: [6, 10], biome: meadow}
colour: {slope: 0.25, high: 100}
areas:
  - {id: above, name: Above, area: [[16, 16], [112, 16], [112, 112], [16, 112]], biome: meadow, kind: town}
waypoints:
  - {id: above, name: Above, at: [64, 40]}
gates:
  - {name: down, at: [40, 80], radius: 1.6, to: {region: test_hollow, gate: up}}
  - {name: east, at: [96, 80], radius: 1.6, to: {region: test_beside, gate: west}}
start: {at: [64, 48], yaw: 0}
biomes:
  meadow:
    colours: {flat: grass, slope: dirt, steep: stone, high: stone}
`

// hollow is a place below the region above, a cave whose gate up leads to its gate down.
const hollow = `name: Test Hollow
under: test_above
size: [128, 128]
cell: 1
seed: 5
look: {sky: cave}
ground: {noise: {scale: 40, amplitude: 0.5, octaves: 1}}
edge: {width: 8, height: [2, 4], biome: rock}
colour: {slope: 0.25, high: 100}
rock: {height: 4.5, rough: 1}
features:
  - {hall: {area: [[16, 52], [56, 52], [56, 76], [16, 76]], flat: true}}
  - {tunnel: {path: [[52, 64], [84, 64]], width: 6}}
  - {hall: {area: [[80, 48], [112, 48], [112, 80], [80, 80]]}}
areas:
  - {id: hollow, name: The Hollow, area: [[8, 8], [120, 8], [120, 120], [8, 120]], levels: [10, 14], biome: cave, kind: hunting,
     boss: {at: [100, 64], radius: 5, mob: vampire_bat}}
camps:
  - {area: hollow, name: bats, at: [90, 56], radius: 4, mob: cave_bat, max: 2, respawn: 45s}
waypoints:
  - {id: hollow, name: The Hollow, at: [30, 64]}
gates:
  - {name: up, at: [22, 64], radius: 1.6, to: {region: test_above, gate: down}}
start: {at: [26, 64], yaw: 90}
biomes:
  cave:
    colours: {flat: cave_floor, slope: cave_floor, steep: cave_rock, high: cave_rock}
  rock:
    colours: {flat: cave_rock, slope: cave_rock, steep: cave_rock, high: cave_rock}
`

// beside is a region on the land beside the region above, through its gate east.
const beside = `name: Test Beside
size: [128, 128]
cell: 1
seed: 7
look: {sky: day}
ground: {noise: {scale: 60, amplitude: 1, octaves: 1}}
edge: {width: 16, height: [6, 10], biome: meadow}
colour: {slope: 0.25, high: 100}
areas:
  - {id: beside, name: Beside, area: [[16, 16], [112, 16], [112, 112], [16, 112]], levels: [1, 4], biome: meadow, kind: hunting,
     boss: {at: [90, 64], radius: 5, mob: king_slime}}
gates:
  - {name: west, at: [24, 64], radius: 1.6, to: {region: test_above, gate: east}}
start: {at: [30, 64], yaw: 90}
biomes:
  meadow:
    colours: {flat: grass, slope: dirt, steep: stone, high: stone}
`

// belowData is the shipped data with the region above, the hollow below it through its gate down, and the region beside it through its gate east.
func belowData(t *testing.T) *gamedata.Data {
	t.Helper()
	return worldtest.LoadDataWith(t, map[string]func(string) string{
		"regions/test_above.yaml":  func(string) string { return above },
		"regions/test_hollow.yaml": func(string) string { return hollow },
		"regions/test_beside.yaml": func(string) string { return beside },
	})
}

// enterAbove joins name to the region above at its start, and gives where they landed.
func (w *testWorld) enterAbove(t *testing.T, data *gamedata.Data, name string) (model.Player, *worldtest.Sink) {
	t.Helper()
	m, _ := data.Map("test_above")
	p, s := worldtest.NewPlayer(name, m.PlayerSpawn.X, m.PlayerSpawn.Z), &worldtest.Sink{}
	w.enter(t, p, "test_above", "c-"+name, s)
	return p, s
}

// through hands name, on conn, through the gate they stand in, and gives the newest snapshot s heard after.
func (w *testWorld) through(t *testing.T, p model.Player, s *worldtest.Sink) model.Snapshot {
	t.Helper()
	if err := w.EnterPortal(context.Background(), p.ID, "c-"+p.Name, 0); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * worldtest.TickInterval)
	synctest.Wait()
	snaps := worldtest.Of[model.Snapshot](s)
	return snaps[len(snaps)-1]
}

// Through a gate between a region and a place below, a player lands in the channel with the same number, opened if it's closed, though another has room; a place below opens none until one is needed, and closes its first once idle too (underground-and-underwater.md section 6). A gate between two regions on the land keeps the least full.
func TestAGateBelowKeepsTheChannel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := belowData(t)
		heard := &opens{}
		w := channelWorldOn(t, data, worldtest.Options{ChannelCap: 2, ChannelIdle: idle, Channels: heard})
		region, _ := data.Map("test_above")
		down := portalNamed(t, region, "down")
		ayla, _ := w.enterAbove(t, data, "Ayla")
		bo, sb := worldtest.NewPlayer("Bo", down.At.X, down.At.Z), &worldtest.Sink{}
		w.enter(t, bo, "test_above", "c-Bo", sb)
		kai, sk := worldtest.NewPlayer("Kai", down.At.X, down.At.Z), &worldtest.Sink{}
		w.enter(t, kai, "test_above", "c-Kai", sk)
		if channelOf(t, sk) != 2 || !slices.Equal(heard.of("test_hollow"), []int{0}) || w.Opened("test_hollow/1") != nil {
			t.Fatalf("Kai joined %d, the hollow heard %v open; want channel 2, the hollow with none", channelOf(t, sk), heard.of("test_hollow"))
		}
		if got := w.through(t, kai, sk); got.MapID != "test_hollow" || got.Channel != 2 || w.Opened("test_hollow/1") != nil {
			t.Fatalf("Kai went down to %s %d; want the hollow's channel 2, its 1 not opened", got.MapID, got.Channel)
		}
		if err := w.Leave(context.Background(), bo.ID, "c-Bo"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(closed)
		synctest.Wait()
		if w.Opened("test_above/2") != nil {
			t.Fatal("the region above's channel 2 still open, empty past the idle time")
		}
		if got := w.through(t, kai, sk); got.MapID != "test_above" || got.Channel != 2 {
			t.Fatalf("Kai went up to %s %d; want the region above's channel 2 opened again, though 1 has room", got.MapID, got.Channel)
		}
		if got := w.open(t, ayla); !slices.Equal(got, []model.ChannelLoad{{Channel: 1, Players: 1}, {Channel: 2, Players: 1}}) {
			t.Fatalf("the region above's channels %+v", got)
		}
		time.Sleep(closed)
		synctest.Wait()
		if got := heard.stops("test_hollow"); !slices.Equal(got, []int{2}) || !slices.Equal(heard.of("test_hollow"), []int{0, 1, 0}) {
			t.Fatalf("the hollow heard %v stopped, %v open; want its channel 2 closed once idle, none left", got, heard.of("test_hollow"))
		}
		east := portalNamed(t, region, "east")
		mira, sm := worldtest.NewPlayer("Mira", east.At.X, east.At.Z), &worldtest.Sink{}
		w.enter(t, mira, "test_above", "c-Mira", sm)
		nia, sn := worldtest.NewPlayer("Nia", east.At.X, east.At.Z), &worldtest.Sink{}
		w.enter(t, nia, "test_above", "c-Nia", sn)
		if channelOf(t, sm) != 1 || channelOf(t, sn) != 2 {
			t.Fatalf("Mira joined %d, Nia %d; want 1 and 2, the least full in turn", channelOf(t, sm), channelOf(t, sn))
		}
		if got := w.through(t, nia, sn); got.MapID != "test_beside" || got.Channel != 1 {
			t.Fatalf("Nia went east to %s %d; want the region beside's least full, its channel 1", got.MapID, got.Channel)
		}
	})
}

// A gate below keeps each player's own channel, not their party's: a party split across the region's channels 1 and 2 goes down split, though the place's 1 has room.
func TestAPartyGoesDownByItsOwnChannels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := belowData(t)
		ps := &parties{}
		w := channelWorldOn(t, data, worldtest.Options{ChannelCap: 2, Parties: ps})
		region, _ := data.Map("test_above")
		down := portalNamed(t, region, "down")
		ayla, sa := worldtest.NewPlayer("Ayla", down.At.X, down.At.Z), &worldtest.Sink{}
		w.enter(t, ayla, "test_above", "c-Ayla", sa)
		w.enterAbove(t, data, "Cy")
		bo, sb := worldtest.NewPlayer("Bo", down.At.X, down.At.Z), &worldtest.Sink{}
		w.enter(t, bo, "test_above", "c-Bo", sb)
		if channelOf(t, sa) != 1 || channelOf(t, sb) != 2 {
			t.Fatalf("Ayla joined %d, Bo %d; want 1 and 2", channelOf(t, sa), channelOf(t, sb))
		}
		ps.mu.Lock()
		ps.all = [][]kernel.CharacterID{{ayla.ID, bo.ID}}
		ps.mu.Unlock()
		if got := w.through(t, ayla, sa); got.MapID != "test_hollow" || got.Channel != 1 {
			t.Fatalf("Ayla went down to %s %d; want the hollow's 1", got.MapID, got.Channel)
		}
		if got := w.through(t, bo, sb); got.MapID != "test_hollow" || got.Channel != 2 {
			t.Fatalf("Bo went down to %s %d; want the hollow's 2, his own, not Ayla's 1 with room", got.MapID, got.Channel)
		}
	})
}

// A join on a place below with none open opens its first channel; a death inside gets up at the place's own waypoint; its boss is told to those inside alone, and chat inside reaches only the place's channel.
func TestAPlaceBelowKeepsItsOwn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := belowData(t)
		w := channelWorldOn(t, data, worldtest.Options{})
		place, _ := data.Map("test_hollow")
		_, so := w.enterAbove(t, data, "Ayla")
		bo, sb := worldtest.NewPlayer("Bo", place.PlayerSpawn.X, place.PlayerSpawn.Z), &worldtest.Sink{}
		w.enter(t, bo, "test_hollow", "c-Bo", sb)
		if channelOf(t, sb) != 1 || w.Opened("test_hollow/1") == nil {
			t.Fatalf("Bo joined the hollow's %d; want its channel 1, opened", channelOf(t, sb))
		}
		if got := worldtest.Of[model.Snapshot](sb)[0]; got.Boss == nil || got.Boss.Type != "vampire_bat" {
			t.Fatalf("Bo's snapshot told the boss %+v; want the hollow's vampire bat up", got.Boss)
		}
		if err := w.Say(bo.ID, "c-Bo", bo.Name, "down here"); err != nil {
			t.Fatal(err)
		}
		var kai model.Player
		for _, s := range place.Spawns {
			if s.Mob == "cave_bat" {
				kai = worldtest.NewPlayer("Kai", s.At.X, s.At.Z)
			}
		}
		kai.HP = 1
		sk := &worldtest.Sink{}
		w.enter(t, kai, "test_hollow", "c-Kai", sk)
		for range 120 {
			if len(worldtest.Of[model.Respawned](sk)) > 0 {
				break
			}
			_ = w.Attack(kai.ID, "c-Kai", 0, model.EntityRef{})
			time.Sleep(time.Second)
			synctest.Wait()
		}
		_, at, _ := data.GetUp(place, kai.X, kai.Z)
		got := worldtest.Of[model.Respawned](sk)
		if wp := place.Waypoints[0]; len(got) != 1 || got[0].MapID != "test_hollow" || got[0].X != at.X || got[0].Z != at.Z || math.Hypot(at.X-wp.At.X, at.Z-wp.At.Z) > 3 {
			t.Fatalf("Kai got up %+v; want once in the hollow, beside its waypoint at %v", got, place.Waypoints[0].At)
		}
		if heard := worldtest.Of[model.Chat](sb); len(heard) != 1 || heard[0].Text != "down here" {
			t.Fatalf("Bo heard %+v; want their own line", heard)
		}
		for _, e := range so.All() {
			switch ev := e.(type) {
			case model.Chat:
				t.Fatalf("Ayla, in the region above, heard %+v from the hollow", ev)
			case model.FieldBoss:
				if ev.Type == "vampire_bat" {
					t.Fatalf("Ayla, in the region above, heard the hollow's boss: %+v", ev)
				}
			}
		}
	})
}

// A join that has to open a channel, a character saved in a place below, can come before Run does, as a player's can just after a boot: it waits in the channel's inbox and lands once Run starts it, rather than being refused as stopped.
func TestAJoinToAPlaceBelowBeforeRunLandsOnceRunStarts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := belowData(t)
		w := newWorldOn(t, data, &fakeSaver{}, worldtest.Options{CheckpointInterval: time.Hour})
		place, _ := data.Map("test_hollow")
		p, s := worldtest.NewPlayer("Ayla", place.PlayerSpawn.X, place.PlayerSpawn.Z), &worldtest.Sink{}
		landed := make(chan error, 1)
		go func() { landed <- w.join(p, "test_hollow", "c-Ayla", s) }()
		synctest.Wait()
		select {
		case err := <-landed:
			t.Fatalf("joined before Run: %v", err)
		default:
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); _ = w.Run(ctx) }()
		synctest.Wait()
		if err := <-landed; err != nil {
			t.Fatalf("the join that came before Run: %v", err)
		}
		if channelOf(t, s) != 1 || w.Opened("test_hollow/1") == nil {
			t.Fatalf("landed on channel %d; want the hollow's first, opened by Run", channelOf(t, s))
		}
		cancel()
		<-done
	})
}
