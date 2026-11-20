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

// Spots on the shipped maps the rows stand at, in metres; each is open ground at height 0 unless it says otherwise.
var (
	SlimeField     = gamedata.Point{X: 13.5, Z: 12} // the forest's west, at the edge of the slimes' circle, which sees it
	AmongSlimes    = gamedata.Point{X: 20, Z: 12}   // the middle of the slimes' circle, where they're on a player at once
	OutOfSight     = gamedata.Point{X: 1, Z: 6}     // the forest's south-west corner, farther than any mob of the forest sees
	ClearOfSlimes  = gamedata.Point{X: 2, Z: 30}    // the forest's north-west, out of the slimes' sight, even of those that chase a player there till they let them go
	TownPortal     = gamedata.Point{X: 46, Z: 16}   // the town's to_forest
	ForestFromTown = gamedata.Point{X: 2, Z: 20}    // the forest's way back to the town
	ForestToCave   = gamedata.Point{X: 70, Z: 20}
	CaveFromForest = gamedata.Point{X: 2, Z: 16}
	ByTheTrainer   = gamedata.Point{X: 36.5, Z: 13} // in talking reach of the town's Warrior Trainer, at 36, 14
)

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
	died := l.during(30*time.Second, func(m wsconn.Message) bool {
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
	return l.during(d, pred, nil, l.Chase)
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
		if started && l.tick < next {
			return nil
		}
		next, started = l.tick+l.ticksIn(swingEvery), true
		return l.Swing()
	}
}

// HuntUntil swings at the nearest mob every swingEvery until done holds, calling also, when set, once a frame, as to read another player alongside; it gives up once d of the world's ticks pass, or when no frame comes for Stalled.
func (l *Looter) HuntUntil(d time.Duration, done func() bool, also func()) bool {
	swing := l.swings()
	return l.during(d, nil, done, func() error {
		if also != nil {
			also()
		}
		return swing()
	})
}

// AttackUntil swings at the nearest mob every swingEvery until pred sees a message; it gives up once d of the world's ticks pass, or when no frame comes for Stalled.
func (l *Looter) AttackUntil(d time.Duration, pred func(wsconn.Message) bool) bool {
	return l.during(d, pred, nil, l.swings())
}
