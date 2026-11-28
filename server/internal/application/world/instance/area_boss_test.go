package instance_test

import (
	"slices"
	"testing"
	"testing/synctest"

	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/regiongen"
)

// line is where fields' west area ends and its east starts, going east.
const line = plainSize / 2

// fields is the plain made a region of two hunting areas, west and east of line (large-regions.md section 6): King Slime stands in the west at (kingX, line), the Spore Mother in the east at (240, line), each in a circle r round.
func fields(t *testing.T, kingX, r float64) *gamedata.Map {
	t.Helper()
	m := plain(t, nil, nil)
	m.ID, m.Kind = "fields", gamedata.Region
	nx := int(plainSize / regiongen.AreaCell)
	grid := regiongen.AreaGrid{NX: nx, NZ: nx, Of: make([]int16, nx*nx)}
	for s := range grid.Of {
		if float64(s%nx*regiongen.AreaCell) >= line {
			grid.Of[s] = 1
		}
	}
	m.AreaGrid = grid
	m.Areas = []gamedata.Area{
		{ID: "west", Name: "West Field", Kind: gamedata.Hunting, MinLevel: 1, MaxLevel: 20, Boss: &gamedata.BossSpot{Mob: "king_slime", At: gamedata.Point{X: kingX, Z: line}, Radius: r}},
		{ID: "east", Name: "East Field", Kind: gamedata.Hunting, MinLevel: 1, MaxLevel: 20, Boss: &gamedata.BossSpot{Mob: "spore_mother", At: gamedata.Point{X: 240, Z: line}, Radius: r}},
	}
	return m
}

// Each hunting area of a region has its own boss, up from the channel's start: a player hears of their area's alone, on joining and in the frames that carry its bar, however far from it they stand.
func TestEachAreaOfARegionHasItsOwnBoss(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), fields(t, 80, 6), nil, worldtest.Options{})
		defer stop()
		west, east := join(t, inst, worldtest.NewPlayer("Ayla", 20, 20)), join(t, inst, worldtest.NewPlayer("Robin", 300, 20)) // each 140 m and more from their boss
		join(t, inst, worldtest.NewPlayer("Kai", 80, line-10))                                                                 // by each boss, so its cells stay awake
		join(t, inst, worldtest.NewPlayer("Jade", 240, line-10))
		tick(200) // each boss wanders its circle
		for _, c := range []struct {
			name string
			s    *worldtest.Sink
			boss string
		}{{"west", west, "king_slime"}, {"east", east, "spore_mother"}} {
			snap := joinedTo(c.s)
			if snap.FieldBoss == nil || snap.FieldBoss.Type != c.boss || snap.Boss == nil || snap.Boss.Type != c.boss {
				t.Fatalf("the %s player joined to boss %+v, its bar %+v; want the %s", c.name, snap.FieldBoss, snap.Boss, c.boss)
			}
			bars := 0
			for _, d := range c.s.Deltas() {
				if d.Boss != nil {
					if d.Boss.Type != c.boss {
						t.Fatalf("the %s player's frame of tick %d carries the %s's bar", c.name, d.Tick, d.Boss.Type)
					}
					bars++
				}
			}
			if bars == 0 || len(bossUps(c.s))+len(bossGones(c.s)) != 0 {
				t.Fatalf("the %s player: %d frames with their boss's bar, told %+v and %+v; want its bar as it wanders, and no news", c.name, bars, bossUps(c.s), bossGones(c.s))
			}
		}
	})
}

// A boss beaten in one area leaves the other's up: only those in its area hear it fall and come back, and one who steps in meanwhile is told its time back, as one stepping out is told the other's.
func TestABossBeatenInOneAreaLeavesTheOther(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := runBossArena(t, bossData(t, 100, "2s", 0.01), fields(t, 80, 0.01), 0.5, worldtest.Options{})
		defer stop()
		slayer, far, crosser := armed("Slayer", line-2.75, 5000), worldtest.NewPlayer("Cora", 20, 20), worldtest.NewPlayer("Robin", line+1, 100)
		slayer.X = 80
		ss, sf, sc := join(t, inst, slayer), join(t, inst, far), join(t, inst, crosser)
		king, mother := kingOf(t, ss), kingOf(t, sc)
		swing(inst, slayer.ID)
		gone := bossGones(ss)
		if len(gone) != 1 || gone[0].Mob != king || len(bossGones(sf)) != 1 || len(bossGones(sc)) != 0 {
			t.Fatalf("gone: the slayer heard %+v, the far west player %+v, the east one %+v; want the king's fall heard in the west alone", gone, bossGones(sf), bossGones(sc))
		}
		seq := walkToX(inst, sc, crosser.ID, 0, line-1, 0.3)
		entered := eventsOf[model.AreaEntered](sc)
		if len(entered) != 1 || entered[0].Area != "west" || entered[0].FieldBoss != nil || entered[0].FieldBossGone == nil || entered[0].FieldBossGone.Mob != king {
			t.Fatalf("stepping west: %+v; want the west, its king beaten", entered)
		}
		walkToX(inst, sc, crosser.ID, seq, line+1, 0.3)
		entered = eventsOf[model.AreaEntered](sc)
		if len(entered) != 2 || entered[1].Area != "east" || entered[1].FieldBoss == nil || entered[1].FieldBoss.Mob != mother {
			t.Fatalf("stepping back east: %+v; want the east, its mother up", entered)
		}
		untilTick(ss, gone[0].Tick+40)
		if ups := bossUps(ss); len(ups) != 1 || ups[0].Type != "king_slime" || len(bossUps(sf)) != 1 || len(bossUps(sc)) != 0 {
			t.Fatalf("back: the slayer heard %+v, the far west player %+v, the east one %+v; want the king's return heard in the west alone", ups, bossUps(sf), bossUps(sc))
		}
	})
}

// A Bosses query finds each hunting area's boss as the tick left it, in the areas' order: both up from the start, then the beaten one's time back counting down while the other's still up. A map, whose players hear of its one boss as it comes and goes, has none.
func TestABossesQueryFindsEachAreasBoss(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := runBossArena(t, bossData(t, 100, "2s", 0.01), fields(t, 80, 0.01), 0.5, worldtest.Options{})
		defer stop()
		slayer := armed("Slayer", line-2.75, 5000)
		slayer.X = 80
		s := join(t, inst, slayer)
		got, err := inst.Bosses(t.Context())
		if want := []model.AreaBoss{{Area: "west", Up: true}, {Area: "east", Up: true}}; err != nil || !slices.Equal(got, want) {
			t.Fatalf("at the start: %+v, %v; want %+v", got, err, want)
		}
		swing(inst, slayer.ID)
		gone := bossGones(s)[0]
		untilTick(s, gone.Tick+10)
		before := s.Last().Tick
		got, err = inst.Bosses(t.Context())
		after := s.Last().Tick
		if err != nil || len(got) != 2 || got[0].Area != "west" || got[0].Up || got[1] != (model.AreaBoss{Area: "east", Up: true}) {
			t.Fatalf("after the king fell: %+v, %v; want the west's beaten, the east's up", got, err)
		}
		back := got[0].BackIn
		at := gone.Tick + uint64((gone.BackIn-back)/worldtest.TickInterval) // the tick that answered, by its time back
		if back%worldtest.TickInterval != 0 || at < before || at > after+1 {
			t.Fatalf("the king back in %v, fallen on tick %d back in %v: answered as of tick %d, asked between ticks %d and %d", back, gone.Tick, gone.BackIn, at, before, after)
		}
	})
}

// A region's boss pays those there when it falls: alive and in its area, or within its leash of its spot, where its fight may have gone over the area's line. A party member over the line within it collects; one farther off doesn't.
func TestARegionsBossPaysThoseInItsAreaOrWithinItsLeash(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const hp = 400
		kingX := line - 8
		slayer, over, off := armed("Slayer", line-2.75, 5000), worldtest.NewPlayer("Bram", line+2, line-3), worldtest.NewPlayer("Eli", line+40, line-3) // Bram 10 m from the king's spot, in the east; Eli 48 m
		slayer.X = kingX
		parties := &fakeParties{parties: [][]kernel.CharacterID{{slayer.ID, over.ID, off.ID}}}
		inst, stop := runBossArena(t, bossData(t, hp, "5m", 0.01), fields(t, kingX, 0.01), 0.5, worldtest.Options{Parties: parties})
		defer stop()
		ss, so, sf := join(t, inst, slayer), join(t, inst, over), join(t, inst, off)
		swing(inst, slayer.ID)
		if len(bossGones(ss)) != 1 {
			t.Fatalf("gone %+v; want the king beaten", bossGones(ss))
		}
		sinks := map[kernel.CharacterID]*worldtest.Sink{slayer.ID: ss, over.ID: so, off.ID: sf}
		gained := func(s *worldtest.Sink) (n int64) { // the king's EXP, the only EXP here; Bram, in the east, doesn't hear it fall
			for _, e := range eventsOf[model.ExpGained](s) {
				n += e.Amount
			}
			return n
		}
		if gained(ss) == 0 || gained(so) == 0 || gained(sf) != 0 {
			t.Fatalf("EXP: the slayer %d, Bram over the line %d, Eli farther off %d; want the first two paid", gained(ss), gained(so), gained(sf))
		}
		if caps := boxesOf("leather_cap", sinks); len(caps) != 2 || caps[slayer.ID] != 1 || caps[over.ID] != 1 {
			t.Fatalf("boxes by owner %v; want one each for the slayer and Bram", caps)
		}
	})
}

// A player who walks into an area on foot gets its boss's bar on the tick they cross, as one who arrives by a gate or a login does in their Snapshot, though the boss stands idle and sends no change of its own.
func TestAPlayerWalkingIntoAnAreaGetsItsBossBarAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), fields(t, 80, 0.01), nil, worldtest.Options{}) // a circle too small for the king to wander
		defer stop()
		crosser := worldtest.NewPlayer("Robin", line+1, 100)
		s := join(t, inst, crosser)
		join(t, inst, worldtest.NewPlayer("Kai", 80, line-10)) // by the king, so its cell stays awake
		tick(100)
		for _, d := range s.Deltas() {
			if d.Boss != nil && d.Boss.Type == "king_slime" {
				t.Fatalf("the king's bar came to a player in the east on tick %d, before they crossed", d.Tick)
			}
		}
		walkToX(inst, s, crosser.ID, 0, line-1, 0.3)
		entered := eventsOf[model.AreaEntered](s)
		if len(entered) != 1 || entered[0].Area != "west" || entered[0].FieldBoss == nil {
			t.Fatalf("stepping west: %+v; want the west, its king up", entered)
		}
		for _, d := range s.Deltas() {
			if d.Tick == entered[0].Tick && d.Boss != nil && d.Boss.Type == "king_slime" && d.Boss.HP > 0 {
				return
			}
		}
		t.Fatalf("no frame on tick %d, the tick they entered, carried the king's bar", entered[0].Tick)
	})
}
