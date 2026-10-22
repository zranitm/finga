package e2e_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"finga/internal/protocol"
	"finga/pkg/wsconn"
)

// place moves a character straight in the database, the way a checkpoint
// would have left it: the forest with a given HP and EXP. The REST API has
// no such endpoint, and portals arrive in step 14.
func (s *stack) place(t *testing.T, charID, mapID string, x, y float64, hp int, exp int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tag, err := s.db.Exec(ctx, `UPDATE characters SET map_id = $2, pos_x = $3, pos_y = $4, hp = $5, exp = $6 WHERE id = $1`, charID, mapID, x, y, hp, exp)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("place: %v (%d rows)", err, tag.RowsAffected())
	}
}

// createCharacter signs up, logs in and creates a character; it returns
// the token and the character ID without connecting.
func (s *stack) createCharacter(t *testing.T, username, charName, class string) (token, id string) {
	t.Helper()
	s.mustStatus(s.call("POST", "/api/accounts", "", map[string]string{"username": username, "password": "correct horse"}), http.StatusCreated)
	token = s.call("POST", "/api/sessions", "", map[string]string{"username": username, "password": "correct horse"}).body["token"].(string)
	created := s.call("POST", "/api/characters", token, map[string]string{"name": charName, "class": class})
	s.mustStatus(created, http.StatusCreated)
	return token, created.body["id"].(string)
}

// join opens the WebSocket and says Hello for an existing character.
func (s *stack) join(t *testing.T, token, id string) (*player, protocol.Snapshot) {
	t.Helper()
	p := s.connect(t, id)
	p.send(wsconn.Message{Data: protocol.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterID: id}})
	p.expect("Welcome")
	snap := p.expect("Snapshot").Data.(protocol.Snapshot)
	return p, snap
}

// attackUntil sends Attack every 850 ms until pred sees a message or the
// deadline passes.
func (p *player) attackUntil(d time.Duration, pred func(wsconn.Message) bool) bool {
	deadline := time.Now().Add(d)
	next := time.Now()
	seq := uint32(1000)
	for time.Now().Before(deadline) {
		if !time.Now().Before(next) {
			seq++
			p.send(wsconn.Message{Seq: seq, Data: protocol.Attack{}})
			next = time.Now().Add(850 * time.Millisecond)
		}
		m, ok := p.read(100 * time.Millisecond)
		if ok && pred(m) {
			return true
		}
	}
	return false
}

// facing sends a one-tick nudge so the character faces right (or left).
func (p *player) face(right bool) {
	p.send(wsconn.Message{Seq: 500, Data: protocol.Input{Right: right, Left: !right}})
	time.Sleep(60 * time.Millisecond)
	p.send(wsconn.Message{Seq: 501, Data: protocol.Input{}})
}

func TestFightAndGrow(t *testing.T) {
	// One stack per row: the login throttle is five per minute per IP, and
	// every row logs in from the loopback address.

	t.Run("C1/mobs-appear-wander-and-are-the-same-for-everyone", func(t *testing.T) {
		s := newStack(t)
		token, id := s.createCharacter(t, "fiona", "Fiona", "bowman")
		s.place(t, id, "forest", 380, 608, 105, 0)
		p, snap := s.join(t, token, id)
		if len(snap.Mobs) < 3 {
			t.Fatalf("forest snapshot has %d mobs", len(snap.Mobs))
		}
		types := map[string]bool{}
		for _, m := range snap.Mobs {
			types[m.Type] = true
			if m.HP <= 0 || m.MaxHP <= 0 {
				t.Fatalf("mob %+v", m)
			}
		}
		if !types["green_slime"] || !types["orange_mushroom"] {
			t.Fatalf("mob types %v", types)
		}
		// Mobs wander: a delta soon carries a mob at another x.
		moved := false
		deadline := time.Now().Add(3 * time.Second)
		for !moved && time.Now().Before(deadline) {
			m, ok := p.read(time.Until(deadline))
			if !ok {
				break
			}
			if m.Type != "Delta" {
				continue
			}
			for _, mv := range m.Data.(protocol.Delta).Mobs {
				for _, sm := range snap.Mobs {
					if sm.ID == mv.ID && sm.X != mv.X {
						moved = true
					}
				}
			}
		}
		if !moved {
			t.Fatal("no mob moved within 3 s")
		}
		// A second player sees the same mobs.
		token2, id2 := s.createCharacter(t, "greg", "Greg", "warrior")
		s.place(t, id2, "forest", 400, 608, 204, 0)
		_, snap2 := s.join(t, token2, id2)
		if len(snap2.Mobs) != len(snap.Mobs) {
			t.Fatalf("second player sees %d mobs, first saw %d", len(snap2.Mobs), len(snap.Mobs))
		}
		for _, a := range snap.Mobs {
			found := false
			for _, b := range snap2.Mobs {
				found = found || (a.ID == b.ID && a.Type == b.Type)
			}
			if !found {
				t.Fatalf("mob %d is missing from the second snapshot", a.ID)
			}
		}
	})

	t.Run("C2/attacks-hit-mobs-in-range-and-everyone-sees-the-damage", func(t *testing.T) {
		s := newStack(t)
		token, id := s.createCharacter(t, "hana", "Hana", "bowman")
		s.place(t, id, "forest", 380, 608, 105, 0)
		p, _ := s.join(t, token, id)
		token2, id2 := s.createCharacter(t, "ivan", "Ivan", "warrior")
		s.place(t, id2, "forest", 360, 608, 204, 0)
		watcher, _ := s.join(t, token2, id2)
		p.drain(300 * time.Millisecond)
		p.face(true)
		var hit protocol.Damage
		ok := p.attackUntil(20*time.Second, func(m wsconn.Message) bool {
			if m.Type != "Damage" {
				return false
			}
			d := m.Data.(protocol.Damage)
			if d.Source == "p:"+id && !d.Miss {
				hit = d
				return true
			}
			return false
		})
		if !ok {
			t.Fatal("no hit landed in 20 s")
		}
		if !strings.HasPrefix(hit.Target, "m:") || hit.Amount < 1 {
			t.Fatalf("damage %+v", hit)
		}
		// The other player saw the same damage.
		seen := false
		deadline := time.Now().Add(3 * time.Second)
		for !seen && time.Now().Before(deadline) {
			m, ok := watcher.read(time.Until(deadline))
			if !ok {
				break
			}
			if m.Type == "Damage" && m.Data.(protocol.Damage) == hit {
				seen = true
			}
		}
		if !seen {
			t.Fatal("the other player did not see the same damage event")
		}
	})

	t.Run("C3/at-zero-hp-the-character-dies-and-respawns-in-town", func(t *testing.T) {
		s := newStack(t)
		token, id := s.createCharacter(t, "joan", "Joan", "warrior")
		s.place(t, id, "forest", 380, 608, 1, 10) // one hit from death; level 1 with 10 EXP
		p, _ := s.join(t, token, id)
		p.drain(300 * time.Millisecond)
		p.send(wsconn.Message{Seq: 1, Data: protocol.Input{Right: true}}) // walk into the slimes
		died := false
		deadline := time.Now().Add(20 * time.Second)
		for !died && time.Now().Before(deadline) {
			m, ok := p.read(time.Until(deadline))
			if !ok {
				t.Fatal("connection closed")
			}
			if m.Type == "Died" && m.Data.(protocol.Died).CharacterID == id {
				died = true
			}
		}
		if !died {
			t.Fatal("never died walking into the slimes")
		}
		r := p.expect("Respawned").Data.(protocol.Respawned)
		if r.CharacterID != id || r.MapID != "town" || r.HP != 102 {
			t.Fatalf("respawned %+v", r)
		}
		if mc := p.expect("MapChange").Data.(protocol.MapChange); mc.MapID != "town" {
			t.Fatalf("map change %+v", mc)
		}
		snap := p.expect("Snapshot").Data.(protocol.Snapshot)
		if len(snap.Mobs) != 0 {
			t.Fatal("the town has mobs")
		}
		// The death penalty took 5% of level 1's 20 EXP: 10 → 9, and the
		// checkpoint on the way out saved it with the town position.
		var exp int64
		var mapID string
		dl := time.Now().Add(5 * time.Second)
		for time.Now().Before(dl) {
			if err := s.db.QueryRow(context.Background(), `SELECT exp, map_id FROM characters WHERE id = $1`, id).Scan(&exp, &mapID); err != nil {
				t.Fatal(err)
			}
			if exp == 9 && mapID == "town" {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if exp != 9 || mapID != "town" {
			t.Fatalf("saved exp %d on map %s, want 9 on town", exp, mapID)
		}
	})

	t.Run("C4/a-kill-gives-exp-and-levels-up-and-the-level-is-saved", func(t *testing.T) {
		s := newStack(t)
		token, id := s.createCharacter(t, "kim", "Kim", "bowman")
		s.place(t, id, "forest", 380, 608, 105, 18) // two EXP short of level 2
		p, _ := s.join(t, token, id)
		p.drain(300 * time.Millisecond)
		p.face(true)
		var lu protocol.LevelUp
		var gained bool
		ok := p.attackUntil(40*time.Second, func(m wsconn.Message) bool {
			switch m.Type {
			case "ExpGained":
				gained = true
			case "LevelUp":
				lu = m.Data.(protocol.LevelUp)
				return true
			}
			return false
		})
		if !ok || !gained {
			t.Fatalf("no level-up in 40 s (exp gained: %v)", gained)
		}
		if lu.Character.Level != 2 || lu.Character.ID != id || lu.Character.HP != lu.Character.MaxHP || lu.Character.Stats.DEX != 15 {
			t.Fatalf("level-up %+v", lu.Character)
		}
		// Saved right away: the REST list shows level 2.
		dl := time.Now().Add(5 * time.Second)
		for time.Now().Before(dl) {
			list := s.call("GET", "/api/characters", token, nil)
			for _, c := range list.body["characters"].([]any) {
				if cm := c.(map[string]any); cm["id"] == id && cm["level"] == float64(2) {
					return
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("the level-up was not saved within 5 s")
	})
}
