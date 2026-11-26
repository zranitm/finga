package e2etest

import (
	"context"
	"math"
	"net/http"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/pkg/wsconn"
)

// Spots on the shipped regions the rows stand at, in metres, on open ground; Willowmere's Sunmeadow holds the slimes' circle at (650, 250), 8 m round, and the town's trainer and guide stand in Willowbrook.
var (
	SlimeField    = gamedata.Point{X: 641.5, Z: 250} // the circle's west edge, which its slimes see
	AmongSlimes   = gamedata.Point{X: 650, Z: 250}   // the middle of the slimes' circle, where they're on a player at once
	OutOfSight    = gamedata.Point{X: 625, Z: 250}   // west of the circle, farther than any mob sees
	ClearOfSlimes = gamedata.Point{X: 610, Z: 250}   // farther west, out of the slimes' sight, even of those that chase a player there till they let them go
	ByTheTrainer  = gamedata.Point{X: 550.5, Z: 124} // in talking reach of the Warrior Trainer, at (550, 125)
	CaveGate      = gamedata.Point{X: 450, Z: 780}   // Willowmere's gate to Greyfang Cave
	CaveMouth     = gamedata.Point{X: 192, Z: 32}    // the cave's gate back, where a hop lands
	SlimesGate    = gamedata.Point{X: 626, Z: 250}   // where GateByTheSlimes stands the cave's gate
)

// GateByTheSlimes moves Willowmere's gate to the cave from the cliffs to SlimesGate, 16 m west of the slimes' circle, so a hunt and a hop are a few steps; the circle keeps clear of it by a mob's sight.
var GateByTheSlimes = DataEdit{File: "regions/willowmere.yaml", Old: "name: cave_mouth, at: [450, 780],", New: "name: cave_mouth, at: [626, 250],"}

// Place moves a character straight in the database, as a checkpoint would have left it; the join stands it on the ground at (x, z).
func (s *Stack) Place(t *testing.T, charID, mapID string, at gamedata.Point, hp int, exp int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tag, err := s.DB.Exec(ctx, `UPDATE characters SET map_id = $2, pos_x = $3, pos_y = 0, pos_z = $4, hp = $5, exp = $6 WHERE id = $1`, charID, mapID, at.X, at.Z, hp, exp)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("place: %v (%d rows)", err, tag.RowsAffected())
	}
}

// CreateCharacter signs username up and creates their character, returning the session's token and the character's id.
func (s *Stack) CreateCharacter(t *testing.T, username, charName, class string) (token, id string) {
	t.Helper()
	s.MustStatus(s.Call("POST", "/api/accounts", "", map[string]string{"username": username, "password": "correct horse"}), http.StatusCreated)
	token = s.Call("POST", "/api/sessions", "", map[string]string{"username": username, "password": "correct horse"}).Body["token"].(string)
	return token, s.newCharacter(t, token, charName, class)
}

// newCharacter creates a Beginner and, for another class, sets it in the database as a promotion would, its HP full from the next join and its energy full for the skills under test.
func (s *Stack) newCharacter(t *testing.T, token, name, class string) string {
	t.Helper()
	created := s.Call("POST", "/api/characters", token, map[string]string{"name": name})
	s.MustStatus(created, http.StatusCreated)
	id := created.Body["id"].(string)
	if class == "beginner" {
		return id
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tag, err := s.DB.Exec(ctx, `UPDATE characters SET class = $2, hp = 1000000, energy = 10 WHERE id = $1`, id, class)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("make %s a %s: %v (%d rows)", name, class, err, tag.RowsAffected())
	}
	return id
}

// Join connects the character id and says Hello with token, returning the player and the Snapshot it joined to.
func (s *Stack) Join(t *testing.T, token, id string) (*Player, *clientv1.Snapshot) {
	t.Helper()
	p := s.Connect(t, id)
	_, snap := p.Hello(token)
	return p, snap
}

// DieAmongSlimes walks into the nearest slime on the looter's level, without jumping, until its hops kill them; it gives up after 30 s of the world's ticks.
func (l *Looter) DieAmongSlimes(t *testing.T) {
	t.Helper()
	var hits, misses int
	died := l.During(30*time.Second, func(m wsconn.Message) bool {
		switch d := m.Data.(type) {
		case *clientv1.Died:
			return d.GetCharacterId() == l.ID
		case *clientv1.Damage:
			if d.Target == "p:"+l.ID && d.Miss {
				misses++
			} else if d.Target == "p:"+l.ID {
				hits++
			}
		}
		return false
	}, nil, l.Chase)
	if !died {
		t.Fatalf("never died beside the slimes: %d hits, %d misses, at (%v, %v), closed %v, mobs %+v", hits, misses, l.X, l.Z, l.Closed(0), l.Mobs)
	}
}

// Chase walks toward the nearest live slime on the looter's level, sending an input only when the way changes; with none, or one standing there, it keeps on.
func (l *Looter) Chase() error {
	m := l.Nearest("green_slime")
	if m == nil {
		return nil
	}
	in := Toward(m.GetX()-l.X, m.GetZ()-l.Z)
	if !in.GetLeft() && !in.GetRight() && !in.GetUp() && !in.GetDown() || proto.Equal(in, l.heading) {
		return nil
	}
	l.heading = in
	return l.Input(in)
}

// ChaseUntil chases the nearest live slime, deciding once a frame, until pred meets a message; it gives up once d of the world's ticks pass, or when no frame comes for Stalled.
func (l *Looter) ChaseUntil(d time.Duration, pred func(wsconn.Message) bool) bool {
	return l.During(d, pred, nil, l.Chase)
}

// Toward is the keys that walk (dx, dz) metres, one of 8 ways: an axis within a fifth of a metre, under a tick's walk, is left alone.
func Toward(dx, dz float64) *clientv1.Input {
	const near = 0.2
	return &clientv1.Input{Right: dx > near, Left: dx < -near, Up: dz > near, Down: dz < -near}
}

// yawTo is the yaw from (x, z) toward (tx, tz), in radians from north toward east, as the client aims.
func yawTo(x, z, tx, tz float64) float32 { return float32(math.Atan2(tx-x, tz-z)) }

// swingEvery is how often a looter's hunting swings go, in the world's time.
const swingEvery = 850 * time.Millisecond

// swings is an act that swings at the nearest mob once every swingEvery of the world's ticks, the first at once.
func (l *Looter) swings() func() error {
	next, started := uint64(0), false
	return func() error {
		if started && l.elapsed < next {
			return nil
		}
		next, started = l.elapsed+l.ticksIn(swingEvery), true
		return l.Swing()
	}
}

// HuntUntil swings at the nearest mob every swingEvery until done holds, calling also, when set, once a frame, as to read another player alongside; it gives up once d of the world's ticks pass, or when no frame comes for Stalled.
func (l *Looter) HuntUntil(d time.Duration, done func() bool, also func()) bool {
	swing := l.swings()
	return l.During(d, nil, done, func() error {
		if also != nil {
			also()
		}
		return swing()
	})
}

// AttackUntil swings at the nearest mob every swingEvery until pred sees a message; it gives up once d of the world's ticks pass, or when no frame comes for Stalled.
func (l *Looter) AttackUntil(d time.Duration, pred func(wsconn.Message) bool) bool {
	return l.During(d, pred, nil, l.swings())
}
