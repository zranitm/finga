package instance_test

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/internal/shared/sim"
)

// bossData is the shipped data with King Slime still, its HP set, a 3 cm bite, and a leather cap its only drop, back respawn after it falls.
func bossData(t *testing.T, hp int, respawn string, share float64) *gamedata.Data {
	t.Helper()
	return worldtest.LoadDataWith(t, map[string]func(string) string{"mobs.yaml": func(s string) string {
		start := strings.Index(s, "  - id: king_slime\n")
		if start < 0 {
			t.Fatal("no king_slime in mobs.yaml")
		}
		end := len(s)
		if next := strings.Index(s[start+1:], "\n  - id: "); next >= 0 {
			end = start + 1 + next + 1
		}
		king := fmt.Sprintf("  - {id: king_slime, name: King Slime, boss: true, level: 6, hp: %d, attack: 24, damage: physical, physical_defense: 8, magic_defense: 5, accuracy: 32, evasion: 4, exp: 840, speed: 0, radius: 1.5, height: 2.625, behavior: patrol, attacks: [{use: bite, reach: 0.03125}], look: {model: green_blob}, drops: [{item: leather_cap, chance: 1, min: 1, max: 1}]}\n", hp)
		s = s[:start] + king + s[end:]
		s = regexp.MustCompile(`(?m)^  respawn: .*$`).ReplaceAllString(s, "  respawn: "+respawn)
		return regexp.MustCompile(`(?m)^  drop_share: .*$`).ReplaceAllString(s, fmt.Sprintf("  drop_share: %g", share))
	}})
}

// bossArena puts King Slime at kingZ on the arena's line, north of a slime that respawns two ticks after a kill.
func bossArena(t *testing.T) *gamedata.Map {
	t.Helper()
	m := arena(t, 2*worldtest.TickInterval)
	m.Boss = &gamedata.BossSpot{Mob: "king_slime", At: gamedata.Point{X: arenaX, Z: kingZ}, Radius: 0.01}
	return m
}

const (
	kingZ  = slimeZ + 6.25
	atKing = kingZ - 2.75 // in a swing of the king's 1.5 m body, the slime behind
)

// armed is a warrior at z on the arena's line swinging a sword of the given attack: 200 kills a slime or a weak king in one blow.
func armed(name string, z float64, attack int) model.Player {
	p := worldtest.NewPlayer(name, arenaX, z)
	p.Worn = model.Worn{Revision: 1, Items: []model.WornItem{{Template: "wooden_sword", Stats: progression.ItemStats{PhysicalAttack: attack}}}}
	return p
}

// runBossArena runs m at the fixed roll, its drops granted by the fake item module, with o's parties.
func runBossArena(t *testing.T, data *gamedata.Data, m *gamedata.Map, roll float64, o worldtest.Options) (*instance.Instance, func()) {
	t.Helper()
	inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{NewRoll: worldtest.Rolls(roll), Items: newFakeItems(), Parties: o.Parties})
	settle(worldtest.TickInterval)
	return inst, stop
}

// swing attacks once and waits out the warrior's 900 ms attack.
func swing(inst *instance.Instance, id kernel.CharacterID) {
	_ = inst.Attack(id, 0, model.EntityRef{})
	settle(19 * worldtest.TickInterval)
}

func bossUps(s *worldtest.Sink) []model.FieldBoss       { return worldtest.Of[model.FieldBoss](s) }
func bossGones(s *worldtest.Sink) []model.FieldBossGone { return worldtest.Of[model.FieldBossGone](s) }

// joinedTo is the Snapshot s joined to.
func joinedTo(s *worldtest.Sink) model.Snapshot { return s.All()[0].(model.Snapshot) }

// kingOf is the boss up in the Snapshot s joined to.
func kingOf(t *testing.T, s *worldtest.Sink) uint64 {
	t.Helper()
	b := joinedTo(s).FieldBoss
	if b == nil {
		t.Fatal("joined to no boss")
	}
	return b.Mob
}

// untilTick lets the map run until s has the frame of tick n.
func untilTick(s *worldtest.Sink, n uint64) {
	for s.Last().Tick < n {
		settle(worldtest.TickInterval)
	}
}

// expAfterBoss is the EXP s was given once the boss fell.
func expAfterBoss(s *worldtest.Sink) int64 {
	var total int64
	fell := false
	for _, e := range s.All() {
		switch ev := e.(type) {
		case model.FieldBossGone:
			fell = true
		case model.ExpGained:
			if fell {
				total += ev.Amount
			}
		}
	}
	return total
}

// dealt is the HP each player took from mob, in the order s saw the blows, overkill left out.
func dealt(s *worldtest.Sink, mob uint64, hp int) map[kernel.CharacterID]int {
	out := map[kernel.CharacterID]int{}
	for _, d := range worldtest.Of[model.Damage](s) {
		if d.Target.Mob != mob || !d.Target.IsMob() || d.Miss {
			continue
		}
		took := min(d.Amount, hp)
		hp -= took
		out[d.Source.Player] += took
	}
	return out
}

// dropsOf is every drop s saw fall of template, by owner, collected or not.
func dropsOf(s *worldtest.Sink, template string) map[kernel.CharacterID]int {
	out := map[kernel.CharacterID]int{}
	for _, d := range s.Deltas() {
		for _, v := range d.Drops {
			if v.Template == template {
				out[v.Owner]++
			}
		}
	}
	return out
}

func bossExp(dealt, hp int) int64 { return int64(math.Round(840 * float64(dealt) / float64(hp))) }

// A map's boss is up from its start, at its spot with all its HP, before anyone is there to hear it: the first to join find it in their Snapshot, and kills raise no other.
func TestTheMapsBossIsUpFromItsStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := runBossArena(t, bossData(t, 100, "5m", 0.01), bossArena(t), 0.5, worldtest.Options{})
		defer stop()
		hunter := armed("Hunter", nextToSlime, 200)
		s := join(t, inst, hunter)
		snap := joinedTo(s)
		king, ok := mobsIn(s)[kingOf(t, s)]
		if *snap.FieldBoss != (model.FieldBoss{Tick: snap.Tick, Mob: king.ID, Type: "king_slime"}) || snap.FieldBossGone != nil || !ok || king.X != arenaX || king.Z != kingZ || king.HP != 100 {
			t.Fatalf("joined to %+v, %+v, the king %+v; want it up at its spot with all its HP", snap.FieldBoss, snap.FieldBossGone, king)
		}
		swing(inst, hunter.ID)
		swing(inst, hunter.ID)
		if ups := bossUps(s); len(ups) != 0 {
			t.Fatalf("raised %+v; want no boss heard rising, kills raising none", ups)
		}
	})
}

// A beaten boss is gone from the map until respawn has run from the tick it fell, to the tick: then it's back at its spot, a new mob with all its HP, which the map hears.
func TestABeatenBossIsBackAfterItsRespawn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := runBossArena(t, bossData(t, 100, "2s", 0.01), bossArena(t), 0.5, worldtest.Options{})
		defer stop()
		slayer := armed("Slayer", atKing, 5000)
		s := join(t, inst, slayer)
		fallen := kingOf(t, s)
		swing(inst, slayer.ID)
		g := bossGones(s)
		kill := worldtest.Of[model.Damage](s)[0]
		if len(g) != 1 || g[0] != (model.FieldBossGone{Tick: kill.Tick, Mob: fallen, Type: "king_slime", BackIn: 2 * time.Second}) {
			t.Fatalf("gone %+v; want the king beaten on tick %d, back in 2 s", g, kill.Tick)
		}
		untilTick(s, g[0].Tick+39)
		if ups := bossUps(s); len(ups) != 0 {
			t.Fatalf("the king was back at %+v, before its 40 ticks", ups)
		}
		untilTick(s, g[0].Tick+40)
		ups := bossUps(s)
		if len(ups) != 1 || ups[0].Tick != g[0].Tick+40 || ups[0].Mob == fallen || ups[0].Type != "king_slime" {
			t.Fatalf("back %+v; want a new king on tick %d", ups, g[0].Tick+40)
		}
		if king, ok := mobsIn(s)[ups[0].Mob]; !ok || king.X != arenaX || king.Z != kingZ || king.HP != 100 {
			t.Fatalf("the king back %+v; want it at its spot with all its HP", king)
		}
	})
}

// A boss nobody beats never leaves: with a player beside it all along, it's still up long after.
func TestAnUnbeatenBossNeverLeaves(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := bossArena(t)
		m.Spawns = nil // the king alone, so nothing else sends the watcher off
		inst, stop := runBossArena(t, bossData(t, 100, "2s", 0.01), m, 0.5, worldtest.Options{})
		defer stop()
		watcher := armed("Watcher", atKing, 10)
		s := join(t, inst, watcher)
		king := kingOf(t, s)
		settle(15 * time.Minute)
		removed := slices.ContainsFunc(s.Deltas(), func(d worldtest.Delta) bool { return slices.Contains(d.RemovedMobs, king) })
		if g := bossGones(s); len(g) != 0 || removed || s.Last().Tick < 18000 {
			t.Fatalf("gone %+v, removed %v, the last frame on tick %d; want the king up 15 minutes on", g, removed, s.Last().Tick)
		}
	})
}

// A beaten boss pays each player alone by the HP they took from it: EXP by their share, and a drop roll of their own once their share reaches the drop share; a bystander gets nothing.
func TestABeatenBossPaysEachPlayerByTheHPTheyTook(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const hp = 400
		inst, stop := runBossArena(t, bossData(t, hp, "5m", 0.3), bossArena(t), 0.5, worldtest.Options{})
		defer stop()
		hunter, ayla, robin := armed("Hunter", nextToSlime, 200), armed("Ayla", atKing, 40), armed("Robin", atKing, 80)
		s := join(t, inst, hunter)
		sa, sr := join(t, inst, ayla), join(t, inst, robin)
		swing(inst, hunter.ID)
		king := kingOf(t, s)
		swing(inst, ayla.ID)
		for range 40 {
			if len(bossGones(s)) > 0 {
				break
			}
			swing(inst, robin.ID)
		}
		if g := bossGones(s); len(g) != 1 || g[0].Mob != king {
			t.Fatalf("gone %+v; want the king beaten", g)
		}
		took := dealt(s, king, hp)
		if took[ayla.ID]+took[robin.ID] != hp || took[ayla.ID] == 0 || 10*took[ayla.ID] >= 3*hp {
			t.Fatalf("took %v; want Ayla under 30%% of the %d HP and Robin the rest", took, hp)
		}
		if got, want := expAfterBoss(sa), bossExp(took[ayla.ID], hp); got != want {
			t.Errorf("Ayla got %d EXP, want %d", got, want)
		}
		if got, want := expAfterBoss(sr), bossExp(took[robin.ID], hp); got != want {
			t.Errorf("Robin got %d EXP, want %d", got, want)
		}
		if got := expAfterBoss(s); got != 0 {
			t.Errorf("the hunter, who never hit the king, got %d EXP", got)
		}
		if caps := dropsOf(s, "leather_cap"); len(caps) != 1 || caps[robin.ID] != 1 {
			t.Fatalf("caps by owner %v; want Robin's roll alone, Ayla being under the drop share", caps)
		}
	})
}

// A party's damage pools, its members' gone or dead included, and its members alive on the map split the group's EXP with the party bonus, each rolling drops of their own. A member dead when it falls, one on another map and one who left collect nothing.
func TestAPartyPoolsItsDamageAndOnlyThePresentCollect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const hp = 400
		ayla, bram, cora, eli, lone := armed("Ayla", atKing, 40), armed("Bram", nextToSlime, 200), armed("Cora", nextToSlime, 10), armed("Eli", atKing, 40), armed("Lone", atKing, 5000)
		cora.HP = 1 // by Bram, whom the respawned slime goes after: dies to its hop, which comes every 2.5 s
		away := kernel.NewCharacterID()
		parties := &fakeParties{parties: [][]kernel.CharacterID{{bram.ID, ayla.ID, cora.ID, away, eli.ID}}}
		inst, stop := runBossArena(t, bossData(t, hp, "5m", 0.01), bossArena(t), 0.5, worldtest.Options{Parties: parties})
		defer stop()
		sa, sb, se, sl := join(t, inst, ayla), join(t, inst, bram), join(t, inst, eli), join(t, inst, lone)
		swing(inst, bram.ID)
		king := kingOf(t, sb)
		swing(inst, ayla.ID)
		swing(inst, eli.ID)
		_ = inst.Leave(eli.ID, "", nil)
		sc := join(t, inst, cora)
		untilDead(t, sc, cora.ID)
		swing(inst, lone.ID) // the last blow, within Cora's 3 s dead
		took := dealt(sb, king, hp)
		if g := bossGones(sb); len(g) != 1 || g[0].Mob != king || took[ayla.ID] == 0 || took[eli.ID] == 0 || took[lone.ID] == 0 || len(worldtest.Of[model.Died](sb)) != 1 {
			t.Fatalf("gone %+v, took %v; want Ayla, Eli and Lone to have beaten the king, Cora dead", g, took)
		}
		group := bossExp(took[ayla.ID]+took[eli.ID], hp) * 11 / 10
		if got, want := expAfterBoss(sa), group-group/2; got != want {
			t.Errorf("Ayla got %d EXP, want %d: half the party's share with its bonus, and what doesn't divide", got, want)
		}
		if got, want := expAfterBoss(sb), group/2; got != want {
			t.Errorf("Bram got %d EXP, want %d: half the party's share though he never hit it", got, want)
		}
		if got, want := expAfterBoss(sl), bossExp(took[lone.ID], hp); got != want {
			t.Errorf("Lone got %d EXP, want %d", got, want)
		}
		if expAfterBoss(sc) != 0 || expAfterBoss(se) != 0 {
			t.Error("the dead member or the one who left got EXP")
		}
		if caps := dropsOf(sb, "leather_cap"); len(caps) != 3 || caps[ayla.ID] != 1 || caps[bram.ID] != 1 || caps[lone.ID] != 1 {
			t.Fatalf("caps by owner %v; want one each for Ayla, Bram and Lone", caps)
		}
	})
}

// A player joining finds the boss in their Snapshot while it's up, and while it's beaten its time back instead, counted to the tick of their Snapshot.
func TestAPlayerJoiningSeesTheBossOrItsTimeBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := runBossArena(t, bossData(t, 100, "2s", 0.01), bossArena(t), 0.5, worldtest.Options{})
		defer stop()
		slayer := armed("Slayer", atKing, 5000)
		s := join(t, inst, slayer)
		fallen := kingOf(t, s)
		swing(inst, slayer.ID)
		g := bossGones(s)[0]
		late := joinedTo(join(t, inst, armed("Late", spawnZ, 10)))
		want := model.FieldBossGone{Tick: late.Tick, Mob: fallen, Type: "king_slime", BackIn: 2*time.Second - time.Duration(late.Tick-g.Tick)*worldtest.TickInterval}
		if late.FieldBoss != nil || late.FieldBossGone == nil || *late.FieldBossGone != want || want.BackIn <= 0 {
			t.Fatalf("joined to %+v, %+v; want the king down, %+v", late.FieldBoss, late.FieldBossGone, want)
		}
		untilTick(s, g.Tick+40)
		back := joinedTo(join(t, inst, armed("Later", spawnZ, 10)))
		if back.FieldBossGone != nil || back.FieldBoss == nil || back.FieldBoss.Mob != bossUps(s)[0].Mob {
			t.Fatalf("joined to %+v, %+v; want the king back up", back.FieldBoss, back.FieldBossGone)
		}
	})
}

// C10 decision 11: a standing boss fights only once hit, so a player walking the forest's way from the town to the cave, stopping 5 s right by King Slime in its sight, is never chased: it keeps to its circle and attacks nobody.
func TestAPlayerWalkingPastACalmBossIsNeverChased(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		shipped, _ := data.Map("forest")
		forest := *shipped
		forest.Spawns = nil // the king alone
		inst, stop := worldtest.RunMap(t, data, &forest, nil, worldtest.Options{NewRoll: worldtest.Rolls(0.5), Items: newFakeItems()})
		defer stop()
		walker := worldtest.NewPlayer("Walker", forest.PlayerSpawn.X, 19) // a metre south of where players appear, clear of the oak at (16, 20)
		s := join(t, inst, walker)
		king, spot := kingOf(t, s), forest.Boss
		closest, stopped := math.Inf(1), 0
		for seq := uint32(1); seq <= 1200; seq++ {
			v, _ := s.Own(walker.ID)
			if v.State.X >= 66 {
				break
			}
			k := mobsIn(s)[king]
			closest = min(closest, math.Hypot(k.X-v.State.X, k.Z-v.State.Z))
			ahead := k.X-v.State.X > -3 && k.X-v.State.X < 5 && math.Abs(k.Z-v.State.Z) < 3 // round its body by the north or south
			in := sim.Input{Right: true, Up: ahead && v.State.Z >= k.Z, Down: ahead && v.State.Z < k.Z}
			if closest < 3.5 && stopped < 100 {
				in, stopped = sim.Input{}, stopped+1 // 5 s at its side
			}
			_ = inst.Input(walker.ID, seq, in, 0)
			tick(1)
			if math.Hypot(k.X-spot.At.X, k.Z-spot.At.Z) > spot.Radius+1e-9 {
				t.Fatalf("tick %d: the king at (%.2f, %.2f) left its circle", s.Last().Tick, k.X, k.Z)
			}
		}
		if v, _ := s.Own(walker.ID); v.State.X < 66 {
			t.Fatalf("the walker stopped at %+v, short of the cave's portal", v.State)
		}
		if closest > 3.5 || stopped < 100 {
			t.Fatalf("the walker passed %.2f m from the king at the closest, stopping %d ticks; want 100 within 3.5 m", closest, stopped)
		}
		if a := worldtest.Of[model.MobAttacked](s); len(a) != 0 {
			t.Fatalf("the king attacked %+v", a)
		}
	})
}
