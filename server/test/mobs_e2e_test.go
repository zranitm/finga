package e2e_test

import (
	"math"
	"strconv"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// loneSlime keeps one slime on the forest's ground, so a test knows which mob comes.
func loneSlime() dataEdit {
	return dataEdit{"maps/forest.tmj", `{"name": "max", "type": "int", "value": 5}`, `{"name": "max", "type": "int", "value": 1}`}
}

// biters have the forest's green slimes outlast any fight in super armor and bite, 24 px out; still ones don't walk.
func biters(still bool) []dataEdit {
	speed := "32"
	if still {
		speed = "0"
	}
	return []dataEdit{
		{"mobs.yaml", "    name: Green Slime\n    level: 1\n    hp: 34\n", "    name: Green Slime\n    level: 1\n    hp: 1000000\n"},
		{"mobs.yaml", "    exp: 8\n    speed: 32\n    width: 32\n    height: 28\n    behavior: patrol\n    attacks: [{use: hop}]\n", "    exp: 8\n    speed: " + speed + "\n    width: 32\n    height: 28\n    behavior: patrol\n    super_armor: true\n    attacks: [{use: bite}]\n"},
	}
}

// groundSlime is the forest's one slime on its ground as a scout sees it, joining at the map's far right, out of every slime's sight.
func groundSlime(t *testing.T, s *stack) *clientv1.MobView {
	t.Helper()
	token, id := s.createCharacter(t, "scout", "Scout", "beginner")
	s.place(t, id, "forest", 1900, 608, 135, 0)
	_, snap := s.join(t, token, id)
	for _, m := range snap.GetMobs() {
		if m.GetType() == "green_slime" && m.GetY() == 608 {
			return m
		}
	}
	t.Fatal("no green slime on the forest's ground")
	return nil
}

// walk holds right or left for d, an input a tick as a client sends them, then lets go.
func (p *player) walk(right bool, d time.Duration) {
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		p.send(wsconn.Message{Seq: p.nextSeq(), Data: &clientv1.Input{Right: right, Left: !right}})
	}
	p.send(wsconn.Message{Seq: p.nextSeq(), Data: &clientv1.Input{}})
}

// windupOf is the tick of mob's next windup, waiting up to d.
func (p *player) windupOf(t *testing.T, mob uint32, d time.Duration) uint64 {
	t.Helper()
	for deadline := time.Now().Add(d); time.Now().Before(deadline); {
		m, ok := p.read(time.Until(deadline))
		if a, is := m.Data.(*clientv1.MobAttacked); ok && is && a.GetMob() == mob {
			return m.Tick
		}
	}
	t.Fatalf("no windup from mob %d in %s", mob, d)
	return 0
}

// C12: mobs block players, bite after a windup the player sees, and a mob a player hits goes after them.
func TestMobsFightBack(t *testing.T) {
	t.Parallel()

	t.Run("C12/walking-into-a-mob-stops-the-player-at-its-side", func(t *testing.T) {
		t.Parallel()
		s := newStackOver(t, gameData(t, append(stillSlimes(), loneSlime())...))
		slime := groundSlime(t, s)
		token, id := s.createCharacter(t, "wall", "Wall", "warrior")
		s.place(t, id, "forest", slime.GetX()-100, 608, 144, 0)
		p, _ := s.join(t, token, id)
		side := slime.GetX() - 16 - 12 // the slime's left edge, less half the player's width
		p.send(wsconn.Message{Seq: p.nextSeq(), Data: &clientv1.Input{Right: true}})
		furthest := math.Inf(-1)
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
			if v, ok := p.position(id, time.Until(deadline)); ok {
				furthest = max(furthest, v.GetX())
			}
		}
		if furthest > side+0.01 || furthest < side-1 {
			t.Fatalf("the player got to x %v pressing into the slime; want them stopped at its side, %v", furthest, side)
		}
	})

	t.Run("C12/a-bite-lands-after-its-windup-and-stepping-out-dodges-it", func(t *testing.T) {
		t.Parallel()
		s := newStackOver(t, gameData(t, append(biters(true), loneSlime())...))
		slime := groundSlime(t, s)
		token, id := s.createCharacter(t, "biten", "Biten", "warrior")
		s.place(t, id, "forest", slime.GetX()-40, 608, 144, 0) // 12 px clear of it, inside the bite's 24
		p, _ := s.join(t, token, id)
		source, windup := "m:"+strconv.FormatUint(uint64(slime.GetId()), 10), uint64(12) // 600 ms
		bitten := func(m wsconn.Message) bool {
			d, ok := m.Data.(*clientv1.Damage)
			return ok && d.GetSource() == source && d.GetTarget() == "p:"+id
		}

		started := p.windupOf(t, slime.GetId(), 3*time.Second)
		p.walk(false, 300*time.Millisecond) // 48 px: out of the bite's reach before it lands
		for {
			m, ok := p.read(2 * time.Second)
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

		p.walk(true, 800*time.Millisecond) // back against it
		started = p.windupOf(t, slime.GetId(), 3*time.Second)
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
			m, ok := p.read(time.Until(deadline))
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
		s := newStackOver(t, gameData(t, append(biters(false), loneSlime())...))
		slime := groundSlime(t, s)
		d := 1.0 // the way the attacker stands from the slime and walks off: toward the middle of its ground
		if slime.GetX() > 500 {
			d = -1
		}
		nearToken, nearID := s.createCharacter(t, "near", "Near", "warrior")
		s.place(t, nearID, "forest", slime.GetX()-d*30, 608, 144, 0)
		near, snap := s.join(t, nearToken, nearID)
		at := settled(t, near, snap, slime.GetId()) // the slime has come to the near player and holds there, biting
		token, id := s.createCharacter(t, "hitter", "Hitter", "warrior")
		s.place(t, id, "forest", at+d*50, 608, 144, 0)
		p, _ := s.join(t, token, id)
		p.face(d < 0)
		var blow uint64
		if !p.attackUntil(5*time.Second, func(m wsconn.Message) bool {
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
		p.walk(d > 0, 1200*time.Millisecond)
		// The slime has until its 5 s provoke runs out, counted in the server's ticks: on a loaded runner they lag the wall clock.
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
			m, ok := p.read(time.Until(deadline))
			if !ok {
				continue
			}
			if m.Tick > blow+100 {
				break
			}
			if dl, is := m.Data.(*clientv1.Delta); is {
				for _, mv := range dl.GetMobs() {
					if mv.GetId() == slime.GetId() && d*(mv.GetX()-at) >= 40 && float64(mv.GetFacing()) == d {
						return
					}
				}
			}
		}
		t.Fatalf("the slime stayed by the near player at %v; want it gone 40 px after the player who hit it within its 5 s provoke", at)
	})
}

// settled is where mob stands once it has stopped moving for half a second, as p sees it from snap on: a mob that never moves sends no Delta.
func settled(t *testing.T, p *player, snap *clientv1.Snapshot, mob uint32) float64 {
	t.Helper()
	x, since := math.NaN(), time.Now()
	for _, mv := range snap.GetMobs() {
		if mv.GetId() == mob {
			x = mv.GetX()
		}
	}
	for deadline := time.Now().Add(6 * time.Second); time.Now().Before(deadline); {
		m, ok := p.read(50 * time.Millisecond)
		if dl, is := m.Data.(*clientv1.Delta); ok && is {
			for _, mv := range dl.GetMobs() {
				if mv.GetId() == mob && mv.GetX() != x {
					x, since = mv.GetX(), time.Now()
				}
			}
		}
		if !math.IsNaN(x) && time.Since(since) > 500*time.Millisecond {
			return x
		}
	}
	t.Fatalf("mob %d never settled; last at %v", mob, x)
	return 0
}
