package e2e_test

import (
	"math"
	"strconv"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// loneSlime keeps one slime in the forest's slimes' circle, shrunk to a spot of open level ground, so a test knows which mob comes and has room round it.
func loneSlime() e2etest.DataEdit {
	return e2etest.DataEdit{File: "maps/forest.yaml", Old: "{name: slimes, at: [20, 12], radius: 6, mob: green_slime, max: 5,", New: "{name: slimes, at: [20, 24], radius: 0.5, mob: green_slime, max: 1,"}
}

// biters have the forest's green slimes outlast any fight in super armor and bite, 0.75 m out; still ones don't walk.
func biters(still bool) []e2etest.DataEdit {
	speed := "1"
	if still {
		speed = "0"
	}
	return []e2etest.DataEdit{
		{File: "mobs.yaml", Old: "    name: Green Slime\n    level: 1\n    hp: 34\n", New: "    name: Green Slime\n    level: 1\n    hp: 1000000\n"},
		{File: "mobs.yaml", Old: greenSlimeBody, New: "    exp: 8\n    speed: " + speed + "\n    radius: 0.5\n    height: 0.875\n    behavior: patrol\n    super_armor: true\n    attacks: [{use: bite}]\n"},
	}
}

// greenSlimeBody is mobs.yaml's green slime from its EXP to its attacks, which the rows rewrite.
const greenSlimeBody = "    exp: 8\n    speed: 1\n    radius: 0.5\n    height: 0.875\n    behavior: patrol\n    attacks: [{use: hop}]\n"

// groundSlime is the forest's one slime in the slimes' circle as a scout sees it from where no mob sees it; the mesa's slimes stand east of x 40.
func groundSlime(t *testing.T, s *e2etest.Stack) *clientv1.MobView {
	t.Helper()
	token, id := s.CreateCharacter(t, "scout", "Scout", "beginner")
	s.Place(t, id, "forest", e2etest.OutOfSight, 135, 0)
	_, snap := s.Join(t, token, id)
	for _, m := range snap.GetMobs() {
		if m.GetType() == "green_slime" && m.GetX() < 40 {
			return m
		}
	}
	t.Fatal("no green slime in the forest's slimes' circle")
	return nil
}

// beside is the spot dx metres east of the mob, across from it on the ground.
func beside(m *clientv1.MobView, dx float64) gamedata.Point {
	return gamedata.Point{X: m.GetX() + dx, Z: m.GetZ()}
}

// C12: mobs block players, bite after a windup the player sees, and a mob a player hits goes after them.
func TestMobsFightBack(t *testing.T) {
	t.Parallel()

	t.Run("C12/walking-into-a-mob-stops-the-player-at-its-side", func(t *testing.T) {
		t.Parallel()
		s := newStackOver(t, e2etest.GameData(t, append(stillSlimes(), loneSlime())...))
		slime := groundSlime(t, s)
		token, id := s.CreateCharacter(t, "wall", "Wall", "warrior")
		s.Place(t, id, "forest", beside(slime, -3.125), 144, 0)
		p, _ := s.Join(t, token, id)
		side := slime.GetX() - 0.5 - 0.375 // the slime's west edge, less the player's radius
		p.Send(wsconn.Message{Seq: p.NextSeq(), Data: &clientv1.Input{Right: true}})
		// About 13 ticks to its side, a held input waiting out the world's hold; then a second more for any step past it.
		furthest := math.Inf(-1)
		for deadline := time.Now().Add(10 * time.Second); furthest < side-1 && time.Now().Before(deadline); {
			if v, ok := p.Position(id, time.Until(deadline)); ok {
				furthest = max(furthest, v.GetX())
			}
		}
		for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
			if v, ok := p.Position(id, time.Until(deadline)); ok {
				furthest = max(furthest, v.GetX())
			}
		}
		if furthest > side+0.001 || furthest < side-0.03 {
			t.Fatalf("the player got to x %v pressing into the slime; want them stopped at its side, %v", furthest, side)
		}
	})

	t.Run("C12/a-bite-lands-after-its-windup-and-stepping-out-dodges-it", func(t *testing.T) {
		t.Parallel()
		s := newStackOver(t, e2etest.GameData(t, append(biters(true), loneSlime())...))
		slime := groundSlime(t, s)
		token, id := s.CreateCharacter(t, "biten", "Biten", "warrior")
		s.Place(t, id, "forest", beside(slime, -1.25), 144, 0) // 0.375 m clear of it, inside the bite's 0.75
		p, _ := s.Join(t, token, id)
		source, windup := "m:"+strconv.FormatUint(uint64(slime.GetId()), 10), uint64(12) // 600 ms
		bitten := func(m wsconn.Message) bool {
			d, ok := m.Data.(*clientv1.Damage)
			return ok && d.GetSource() == source && d.GetTarget() == "p:"+id
		}

		started := p.WindupOf(t, slime.GetId(), 3*time.Second)
		p.Walk(false, 300*time.Millisecond) // 1.5 m: out of the bite's reach before it lands
		for {
			m, ok := p.Read(2 * time.Second)
			if !ok {
				t.Fatal("no frame for 2 s")
			}
			if bitten(m) {
				t.Fatalf("bitten at tick %d after stepping out of the windup from %d", m.Tick, started)
			}
			if m.Tick > started+windup+10 {
				break
			}
		}

		p.Walk(true, 800*time.Millisecond) // back against it
		started = p.WindupOf(t, slime.GetId(), 3*time.Second)
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
			m, ok := p.Read(time.Until(deadline))
			if ok && bitten(m) {
				if m.Tick != started+windup {
					t.Fatalf("bitten at tick %d; want the windup from %d to land 600 ms on", m.Tick, started)
				}
				return
			}
		}
		t.Fatal("no bite on the player standing against the slime")
	})

	t.Run("C12/a-mob-a-player-hits-goes-after-them-past-a-nearer-player", func(t *testing.T) {
		t.Parallel()
		s := newStackOver(t, e2etest.GameData(t, append(biters(false), loneSlime())...))
		slime := groundSlime(t, s)
		d := 1.0 // the way the attacker stands from the slime and walks off: east, the open side of its spot
		nearToken, nearID := s.CreateCharacter(t, "near", "Near", "warrior")
		nearAt := beside(slime, -d*0.9375)
		s.Place(t, nearID, "forest", nearAt, 144, 0)
		near, snap := s.Join(t, nearToken, nearID)
		at := settled(t, near, snap, slime.GetId()) // the slime has come to the near player and holds there, biting
		if d*(nearAt.X-at.X) > 0 {
			d = -d // it patrolled past the near player's spot before they joined: the hitter stands on its other side, so the near player isn't in its way
		}
		token, id := s.CreateCharacter(t, "hitter", "Hitter", "warrior")
		s.Place(t, id, "forest", gamedata.Point{X: at.X + d*1.5625, Z: at.Z}, 144, 0)
		p := e2etest.NewLooter(s.Join(t, token, id))
		var blow uint64
		if !p.AttackUntil(5*time.Second, func(m wsconn.Message) bool {
			dm, ok := m.Data.(*clientv1.Damage)
			if ok && dm.GetSource() == "p:"+id && dm.GetTarget() == "m:"+strconv.FormatUint(uint64(slime.GetId()), 10) {
				blow = m.Tick
				return true
			}
			return false
		}) {
			t.Fatal("the hitter's blows never reached the slime")
		}
		if blow == 0 {
			t.Fatal("the blow came with no tick")
		}
		p.Walk(d > 0, 1200*time.Millisecond)
		// The slime has until its 5 s provoke runs out, counted in the server's ticks: on a loaded runner they lag the wall clock.
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
			m, ok := p.Read(time.Until(deadline))
			if !ok {
				continue
			}
			if m.Tick > blow+100 {
				break
			}
			if dl, is := m.Data.(*clientv1.Delta); is {
				for _, mv := range dl.GetMobs() {
					if mv.GetId() == slime.GetId() && d*(mv.GetX()-at.X) >= 1.25 && d*math.Sin(float64(mv.GetYaw())) > math.Sqrt(0.5) { // turned within 45° of the way the hitter went
						return
					}
				}
			}
		}
		t.Fatalf("the slime stayed by the near player at %+v; want it gone 1.25 m after the player who hit it within its 5 s provoke", at)
	})
}

// settled is where mob stands once it has stopped moving for half a second, as p sees it from snap on: a mob that never moves sends no Delta.
func settled(t *testing.T, p *e2etest.Player, snap *clientv1.Snapshot, mob uint32) gamedata.Point {
	t.Helper()
	at, since := gamedata.Point{X: math.NaN(), Z: math.NaN()}, time.Now()
	for _, mv := range snap.GetMobs() {
		if mv.GetId() == mob {
			at = gamedata.Point{X: mv.GetX(), Z: mv.GetZ()}
		}
	}
	for deadline := time.Now().Add(6 * time.Second); time.Now().Before(deadline); {
		m, ok := p.Read(50 * time.Millisecond)
		if dl, is := m.Data.(*clientv1.Delta); ok && is {
			for _, mv := range dl.GetMobs() {
				if now := (gamedata.Point{X: mv.GetX(), Z: mv.GetZ()}); mv.GetId() == mob && now != at {
					at, since = now, time.Now()
				}
			}
		}
		if !math.IsNaN(at.X) && time.Since(since) > 500*time.Millisecond {
			return at
		}
	}
	t.Fatalf("mob %d never settled; last at %+v", mob, at)
	return gamedata.Point{}
}
