package e2e_test

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// place moves a character straight in the database, as a checkpoint would have left it.
func (s *stack) place(t *testing.T, charID, mapID string, x, y float64, hp int, exp int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tag, err := s.db.Exec(ctx, `UPDATE characters SET map_id = $2, pos_x = $3, pos_y = $4, hp = $5, exp = $6 WHERE id = $1`, charID, mapID, x, y, hp, exp)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("place: %v (%d rows)", err, tag.RowsAffected())
	}
}

func (s *stack) createCharacter(t *testing.T, username, charName, class string) (token, id string) {
	t.Helper()
	s.mustStatus(s.call("POST", "/api/accounts", "", map[string]string{"username": username, "password": "correct horse"}), http.StatusCreated)
	token = s.call("POST", "/api/sessions", "", map[string]string{"username": username, "password": "correct horse"}).body["token"].(string)
	return token, s.newCharacter(t, token, charName, class)
}

// newCharacter creates a Beginner and, for another class, sets it in the database as a promotion would, its HP and MP full from the next join.
func (s *stack) newCharacter(t *testing.T, token, name, class string) string {
	t.Helper()
	created := s.call("POST", "/api/characters", token, map[string]string{"name": name})
	s.mustStatus(created, http.StatusCreated)
	id := created.body["id"].(string)
	if class == "beginner" {
		return id
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tag, err := s.db.Exec(ctx, `UPDATE characters SET class = $2, hp = 1000000, mp = 1000000 WHERE id = $1`, id, class)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("make %s a %s: %v (%d rows)", name, class, err, tag.RowsAffected())
	}
	return id
}

func (s *stack) join(t *testing.T, token, id string) (*player, *clientv1.Snapshot) {
	t.Helper()
	p := s.connect(t, id)
	_, snap := p.hello(token)
	return p, snap
}

// dieAmongSlimes walks at the nearest live slime on the player's own level until the player dies, turning only once it is behind; it reads no further than Died.
func (p *player) dieAmongSlimes(t *testing.T) {
	t.Helper()
	right := true
	seq := uint32(1)
	p.send(wsconn.Message{Seq: seq, Data: &clientv1.Input{Right: true}}) // towards the forest's slime field, x 200..800, until a slime shows in a Delta
	var hits, misses int
	var me *clientv1.PlayerView
	mobs := map[uint32]*clientv1.MobView{}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if want, ok := towardSlime(me, mobs); ok && want != right {
			right = want
			seq++
			p.send(wsconn.Message{Seq: seq, Data: &clientv1.Input{Right: right, Left: !right}})
		}
		m, ok := p.read(100 * time.Millisecond)
		if !ok {
			if p.closed(0) {
				t.Fatal("the connection closed while waiting for Died")
			}
			continue
		}
		switch d := m.Data.(type) {
		case *clientv1.Died:
			if d.GetCharacterId() == p.id {
				return
			}
		case *clientv1.Damage:
			if d.Target == "p:"+p.id {
				if d.Miss {
					misses++
				} else {
					hits++
				}
			}
		case *clientv1.Delta:
			for _, v := range d.Players {
				if v.GetId() == p.id {
					me = v
				}
			}
			for _, mv := range d.Mobs {
				mobs[mv.GetId()] = mv
			}
			for _, id := range d.GetRemovedMobs() {
				delete(mobs, id)
			}
		}
	}
	t.Fatalf("never died walking into the slimes: %d hits, %d misses, me %+v, mobs %+v", hits, misses, me, mobs)
}

// towardSlime is which way the nearest live green slime on the player's level is: the branch slimes overhead can't be walked into. It is false with none seen yet, or with one underfoot, so the player walks on through it.
func towardSlime(me *clientv1.PlayerView, mobs map[uint32]*clientv1.MobView) (right, ok bool) {
	if me == nil {
		return false, false
	}
	nearest := math.Inf(1)
	for _, m := range mobs {
		if dx := m.GetX() - me.GetX(); m.GetType() == "green_slime" && m.GetHp() > 0 && math.Abs(m.GetY()-me.GetY()) < 32 && math.Abs(dx) < math.Abs(nearest) {
			nearest = dx
		}
	}
	if math.IsInf(nearest, 1) || math.Abs(nearest) < 8 {
		return false, false
	}
	return nearest > 0, true
}

// attackUntil sends Attack every 850 ms until pred sees a message or d passes.
func (p *player) attackUntil(d time.Duration, pred func(wsconn.Message) bool) bool {
	deadline := time.Now().Add(d)
	next := time.Now()
	seq := uint32(1000)
	for time.Now().Before(deadline) {
		if !time.Now().Before(next) {
			seq++
			p.send(wsconn.Message{Seq: seq, Data: &clientv1.Attack{}})
			next = time.Now().Add(850 * time.Millisecond)
		}
		m, ok := p.read(100 * time.Millisecond)
		if ok && pred(m) {
			return true
		}
	}
	return false
}

// face sends a one-tick nudge so the character faces right (or left).
func (p *player) face(right bool) {
	p.send(wsconn.Message{Seq: 500, Data: &clientv1.Input{Right: right, Left: !right}})
	time.Sleep(60 * time.Millisecond)
	p.send(wsconn.Message{Seq: 501, Data: &clientv1.Input{}})
}

func TestFightAndGrow(t *testing.T) {
	t.Parallel()
	// One stack per row, so the rows run side by side and each stays under its own login throttle.

	t.Run("C1/mobs-appear-wander-and-are-the-same-for-everyone", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.createCharacter(t, "fiona", "Fiona", "bowman")
		s.place(t, id, "forest", 380, 608, 134, 0)
		p, snap := s.join(t, token, id)
		if len(snap.Mobs) < 3 {
			t.Fatalf("forest snapshot has %d mobs", len(snap.Mobs))
		}
		types := map[string]bool{}
		for _, m := range snap.Mobs {
			types[m.Type] = true
			if m.GetHp() <= 0 || m.GetMaxHp() <= 0 {
				t.Fatalf("mob %+v", m)
			}
		}
		if !types["green_slime"] || !types["orange_mushroom"] {
			t.Fatalf("mob types %v", types)
		}
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
			for _, mv := range m.Data.(*clientv1.Delta).Mobs {
				for _, sm := range snap.Mobs {
					if sm.GetId() == mv.GetId() && sm.X != mv.X {
						moved = true
					}
				}
			}
		}
		if !moved {
			t.Fatal("no mob moved within 3 s")
		}
		token2, id2 := s.createCharacter(t, "greg", "Greg", "warrior")
		s.place(t, id2, "forest", 400, 608, 144, 0)
		_, snap2 := s.join(t, token2, id2)
		if len(snap2.Mobs) != len(snap.Mobs) {
			t.Fatalf("second player sees %d mobs, first saw %d", len(snap2.Mobs), len(snap.Mobs))
		}
		for _, a := range snap.Mobs {
			found := false
			for _, b := range snap2.Mobs {
				found = found || (a.GetId() == b.GetId() && a.Type == b.Type)
			}
			if !found {
				t.Fatalf("mob %d is missing from the second snapshot", a.GetId())
			}
		}
	})

	t.Run("C2/attacks-hit-mobs-in-range-and-everyone-sees-the-damage", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.createCharacter(t, "hana", "Hana", "bowman")
		s.place(t, id, "forest", 380, 608, 134, 0)
		p, _ := s.join(t, token, id)
		token2, id2 := s.createCharacter(t, "ivan", "Ivan", "warrior")
		s.place(t, id2, "forest", 360, 608, 144, 0)
		watcher, _ := s.join(t, token2, id2)
		p.drain(300 * time.Millisecond)
		p.face(true)
		var hit *clientv1.Damage
		ok := p.attackUntil(20*time.Second, func(m wsconn.Message) bool {
			if m.Type != "Damage" {
				return false
			}
			d := m.Data.(*clientv1.Damage)
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
		seen := false
		deadline := time.Now().Add(3 * time.Second)
		for !seen && time.Now().Before(deadline) {
			m, ok := watcher.read(time.Until(deadline))
			if !ok {
				break
			}
			if m.Type == "Damage" && proto.Equal(m.Data.(*clientv1.Damage), hit) {
				seen = true
			}
		}
		if !seen {
			t.Fatal("the other player did not see the same damage event")
		}
	})

	t.Run("C3/at-zero-hp-the-character-dies-and-respawns-in-town", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.createCharacter(t, "joan", "Joan", "warrior")
		s.place(t, id, "forest", 380, 608, 1, 10) // one hit from death; level 1 with 10 EXP
		p, _ := s.join(t, token, id)
		p.dieAmongSlimes(t)
		r := p.expect("Respawned").Data.(*clientv1.Respawned)
		if r.GetCharacterId() != id || r.GetMapId() != "town" || r.GetHp() != 72 {
			t.Fatalf("respawned %+v", r)
		}
		if mc := p.expect("MapChange").Data.(*clientv1.MapChange); mc.GetMapId() != "town" {
			t.Fatalf("map change %+v", mc)
		}
		snap := p.expect("Snapshot").Data.(*clientv1.Snapshot)
		if len(snap.Mobs) != 0 {
			t.Fatal("the town has mobs")
		}
		// The death penalty took 5% of level 1's 20 EXP (10 → 9), saved with the town position.
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
		t.Parallel()
		s := newStack(t)
		token, id := s.createCharacter(t, "kim", "Kim", "bowman")
		s.place(t, id, "forest", 380, 608, 134, 18) // two EXP short of level 2
		p, _ := s.join(t, token, id)
		p.drain(300 * time.Millisecond)
		p.face(true)
		var lu *clientv1.LevelUp
		var gained bool
		ok := p.attackUntil(40*time.Second, func(m wsconn.Message) bool {
			switch m.Type {
			case "ExpGained":
				gained = true
			case "LevelUp":
				lu = m.Data.(*clientv1.LevelUp)
				return true
			}
			return false
		})
		if !ok || !gained {
			t.Fatalf("no level-up in 40 s (exp gained: %v)", gained)
		}
		// The level's points wait unspent, so DEX stays the starting 7.
		if lu.GetCharacter().GetLevel() != 2 || lu.GetCharacter().GetId() != id || lu.GetCharacter().GetHp() != lu.GetCharacter().GetMaxHp() || lu.GetCharacter().GetStats().GetDex() != 7 || lu.GetCharacter().GetSheet().GetPoints().GetUnspent() != 5 || lu.GetCharacter().GetSheet().GetSkills().GetUnspent() != 5 {
			t.Fatalf("level-up %+v", lu.Character)
		}
		dl := time.Now().Add(5 * time.Second)
		for time.Now().Before(dl) {
			list := s.call("GET", "/api/characters", p.token, nil)
			for _, c := range list.body["characters"].([]any) {
				if cm := c.(map[string]any); cm["id"] == id && cm["level"] == float64(2) {
					return
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("the level-up was not saved within 5 s")
	})

	t.Run("C5/a-level's-points-are-spent-and-reset-over-the-socket-and-saved-at-once", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.createCharacter(t, "lena", "Lena", "warrior")
		// MP full (level 2's 45), so no regen sends a StatsChanged of its own.
		if _, err := s.db.Exec(context.Background(), `UPDATE characters SET level = 2, unspent_points = 5, mp = 45 WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		l, w := s.hello(t, token, id)
		before := w.GetCharacter().GetSheet()
		if p := before.GetPoints(); p.GetUnspent() != 5 || !p.GetFreeResetLeft() || p.GetSpent().GetStr() != 0 || before.GetAttackCooldownTicks() == 0 {
			t.Fatalf("welcome sheet %+v; want the level's 5 points unspent, the free reset left and an attack cooldown", before)
		}
		saved := func(want string, q string) {
			t.Helper()
			var ok bool
			for dl := time.Now().Add(5 * time.Second); time.Now().Before(dl) && !ok; time.Sleep(100 * time.Millisecond) {
				if err := s.db.QueryRow(context.Background(), q, id).Scan(&ok); err != nil {
					t.Fatal(err)
				}
			}
			if !ok {
				t.Fatalf("not saved within 5 s: %s", want)
			}
		}

		l.send(wsconn.Message{ReqID: "s-1", Data: &clientv1.SpendPoints{ExpectedUnspent: 5, Points: &clientv1.Stats{Str: 3, Vit: 2}}})
		got := l.gather("Response", "StatsChanged")
		if !got["Response"].Data.(*clientv1.Response).GetOk() {
			t.Fatalf("spend answered %+v", got["Response"])
		}
		spent := got["StatsChanged"].Data.(*clientv1.StatsChanged).GetSheet()
		if p := spent.GetPoints(); p.GetUnspent() != 0 || p.GetSpent().GetStr() != 3 || p.GetSpent().GetVit() != 2 ||
			spent.GetPrimary().GetStr() != before.GetPrimary().GetStr()+3 || spent.GetDerived().GetMaxHp() <= before.GetDerived().GetMaxHp() {
			t.Fatalf("sheet after spending %+v; want 3 STR and 2 VIT on top of %+v", spent, before)
		}
		saved("3 STR and 2 VIT spent", `SELECT str_points = 3 AND vit_points = 2 AND unspent_points = 0 FROM characters WHERE id = $1`)

		l.send(wsconn.Message{ReqID: "s-2", Data: &clientv1.SpendPoints{ExpectedUnspent: 5, Points: &clientv1.Stats{Dex: 1}}})
		if r := l.gather("Response")["Response"]; r.ReqID != "s-2" || r.Data.(*clientv1.Response).GetCode() != code("POINTS_CHANGED") {
			t.Fatalf("a spend from a stale count answered %+v; want POINTS_CHANGED", r)
		}

		l.send(wsconn.Message{ReqID: "r-1", Data: &clientv1.ResetPoints{ExpectedUnspent: 0}})
		got = l.gather("Response", "StatsChanged")
		if !got["Response"].Data.(*clientv1.Response).GetOk() {
			t.Fatalf("reset answered %+v", got["Response"])
		}
		if after := got["StatsChanged"].Data.(*clientv1.StatsChanged).GetSheet(); !proto.Equal(after.GetPrimary(), before.GetPrimary()) ||
			after.GetPoints().GetUnspent() != 5 || after.GetPoints().GetFreeResetLeft() {
			t.Fatalf("sheet after the reset %+v; want the stats of %+v, 5 points back and no free reset left", after, before)
		}
		saved("the reset", `SELECT str_points = 0 AND vit_points = 0 AND unspent_points = 5 AND free_reset_used FROM characters WHERE id = $1`)

		l.send(wsconn.Message{ReqID: "r-2", Data: &clientv1.ResetPoints{ExpectedUnspent: 5}})
		if r := l.gather("Response")["Response"]; r.ReqID != "r-2" || r.Data.(*clientv1.Response).GetCode() != code("NO_FREE_RESET") {
			t.Fatalf("a second reset answered %+v; want NO_FREE_RESET", r)
		}
	})

	t.Run("C6/a-bat's-touch-is-magic-and-meets-magic-defense", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.createCharacter(t, "nell", "Nell", "warrior")
		s.place(t, id, "cave", 1500, 384, 400, 0) // on the pillar top, where the bats fly
		// 100 INT: magic defense 3 + 0.5 × 107 + 0.5 × 7 = 60, physical defense 5 + 7 = 12.
		if _, err := s.db.Exec(context.Background(), `UPDATE characters SET level = 21, int_points = 100 WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		p, _ := s.join(t, token, id)
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			m, ok := p.read(time.Until(deadline))
			if !ok {
				break
			}
			d, isDamage := m.Data.(*clientv1.Damage)
			if !isDamage || d.Target != "p:"+id || d.Miss {
				continue
			}
			// Attack 22 · 100 / 160 = 13.75, ±10%: 12 to 15; against the physical 12 it would be 18 to 22.
			if !strings.HasPrefix(d.Source, "m:") || d.Amount < 12 || d.Amount > 15 {
				t.Fatalf("damage %+v; want a bat's touch of 12 to 15, meeting magic defense", d)
			}
			return
		}
		t.Fatal("no bat touched the player in 30 s")
	})

	t.Run("C7/skill-points-evolve-a-skill-and-a-passive-feeds-the-sheet-saved-at-once", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.createCharacter(t, "mira", "Mira", "beginner")
		// HP and MP full from creation, so no regen sends a StatsChanged of its own.
		if _, err := s.db.Exec(context.Background(), `UPDATE characters SET unspent_skill_points = 12 WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		l, w := s.hello(t, token, id)
		if sk := w.GetCharacter().GetSheet().GetSkills(); sk.GetUnspent() != 12 || len(sk.GetInvested()) != 0 {
			t.Fatalf("welcome skills %+v; want 12 unspent and none invested", sk)
		}

		// Ten in Strike is Heavy Strike; two in Second Wind add 0.5 + 0.2 HP and MP a second.
		l.send(wsconn.Message{ReqID: "k-1", Data: &clientv1.SpendSkillPoints{ExpectedUnspent: 12, Points: map[string]int32{"strike": 10, "second_wind": 2}}})
		got := l.gather("Response", "StatsChanged")
		if !got["Response"].Data.(*clientv1.Response).GetOk() {
			t.Fatalf("spend answered %+v", got["Response"])
		}
		sheet := got["StatsChanged"].Data.(*clientv1.StatsChanged).GetSheet()
		if sk := sheet.GetSkills(); sk.GetUnspent() != 0 || sk.GetInvested()["strike"] != 10 || sk.GetInvested()["second_wind"] != 2 {
			t.Fatalf("skills after spending %+v; want 10 in Strike, 2 in Second Wind and none left", sk)
		}
		if d := sheet.GetDerivedSkills(); math.Abs(d.GetHpRegen()-0.7) > 1e-9 || math.Abs(d.GetMpRegen()-0.7) > 1e-9 {
			t.Fatalf("the skills' share %+v; want 0.7 HP and MP a second", d)
		}
		var saved bool
		for dl := time.Now().Add(5 * time.Second); time.Now().Before(dl) && !saved; time.Sleep(100 * time.Millisecond) {
			if err := s.db.QueryRow(context.Background(), `SELECT unspent_skill_points = 0 AND (SELECT count(*) FROM character_skills WHERE character_id = $1 AND (skill_id, points) IN (('strike', 10), ('second_wind', 2))) = 2 FROM characters WHERE id = $1`, id).Scan(&saved); err != nil {
				t.Fatal(err)
			}
		}
		if !saved {
			t.Fatal("the skill points were not saved within 5 s")
		}

		refused := func(req string, in *clientv1.SpendSkillPoints, want string) {
			t.Helper()
			l.send(wsconn.Message{ReqID: req, Data: in})
			if r := l.gather("Response")["Response"]; r.ReqID != req || r.Data.(*clientv1.Response).GetCode() != code(want) {
				t.Fatalf("%s answered %+v; want %s", req, r, want)
			}
		}
		refused("k-2", &clientv1.SpendSkillPoints{ExpectedUnspent: 12, Points: map[string]int32{"strike": 1}}, "SKILL_POINTS_CHANGED")
		refused("k-3", &clientv1.SpendSkillPoints{ExpectedUnspent: 0, Points: map[string]int32{"fireball": 1}}, "SKILL_NOT_LEARNABLE")
		refused("k-4", &clientv1.SpendSkillPoints{ExpectedUnspent: 0, Points: map[string]int32{"strike": -1}}, "BAD_SKILL_POINTS")
		time.Sleep(600 * time.Millisecond) // under the points limit
		refused("k-5", &clientv1.SpendSkillPoints{ExpectedUnspent: 0, Points: map[string]int32{"strike": 1}}, "NOT_ENOUGH_SKILL_POINTS")

		_, again := s.welcome(t, "mira", id)
		if sk := again.GetCharacter().GetSheet().GetSkills(); sk.GetInvested()["strike"] != 10 || sk.GetInvested()["second_wind"] != 2 || sk.GetUnspent() != 0 {
			t.Fatalf("welcomed back with skills %+v; want the points kept", sk)
		}
	})

	t.Run("C8/a-skill-on-the-bar-is-kept-and-throws-at-a-slime-once-a-cooldown", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.createCharacter(t, "nico", "Nico", "beginner")
		// A point in Throw and in Second Wind, as a spend leaves them.
		if _, err := s.db.Exec(context.Background(), `INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'throw', 1, now(), now()), ($1, 'second_wind', 1, now(), now())`, id); err != nil {
			t.Fatal(err)
		}
		path := "/api/characters/" + id + "/skill-bar"
		bar := []any{"throw", "", "", "", "", ""}
		s.mustStatus(s.call("PUT", path, token, map[string]any{"slots": bar}), http.StatusOK)
		if bad := s.call("PUT", path, token, map[string]any{"slots": []string{"second_wind", "", "", "", "", ""}}); bad.status != http.StatusBadRequest || bad.body["code"] != float64(code("BAD_SKILL_BAR")) {
			t.Fatalf("a passive on the bar answered %d %v; want BAD_SKILL_BAR", bad.status, bad.body)
		}
		list := s.call("GET", "/api/characters", token, nil)
		if got := list.body["characters"].([]any)[0].(map[string]any)["skill_bar"]; !reflect.DeepEqual(got, bar) {
			t.Fatalf("listed skill bar %v; want %v", got, bar)
		}

		s.place(t, id, "forest", 380, 608, 135, 0)
		p, _ := s.join(t, token, id)
		token2, id2 := s.createCharacter(t, "omar", "Omar", "beginner")
		s.place(t, id2, "forest", 360, 608, 135, 0)
		watcher, _ := s.join(t, token2, id2)
		p.drain(300 * time.Millisecond)

		use := func(req, skill string) {
			p.send(wsconn.Message{ReqID: req, Data: &clientv1.UseSkill{Skill: skill}})
		}
		use("u-0", "second_wind")
		refusals := map[string]int32{}
		var used *clientv1.SkillUsed
		var hit *clientv1.Damage
		var me *clientv1.PlayerView
		mobs := map[uint32]*clientv1.MobView{}
		next, n := time.Now(), 0
		for deadline := time.Now().Add(30 * time.Second); (hit == nil || refusals["u-1-again"] == 0) && time.Now().Before(deadline); {
			if !time.Now().Before(next) {
				if right, ok := towardSlime(me, mobs); ok {
					p.face(right)
				}
				n++
				use(fmt.Sprintf("u-%d", n), "throw")
				use(fmt.Sprintf("u-%d-again", n), "throw") // inside its cooldown
				next = time.Now().Add(2100 * time.Millisecond)
			}
			m, ok := p.read(100 * time.Millisecond)
			if !ok {
				continue
			}
			switch d := m.Data.(type) {
			case *clientv1.Response:
				if !d.GetOk() {
					refusals[m.ReqID] = d.GetCode()
				}
			case *clientv1.SkillUsed:
				used = d
			case *clientv1.Damage:
				if d.Source == "p:"+id && !d.Miss {
					hit = d
				}
			case *clientv1.Delta:
				for _, v := range d.Players {
					if v.GetId() == id {
						me = v
					}
				}
				for _, mv := range d.Mobs {
					mobs[mv.GetId()] = mv
				}
				for _, mid := range d.GetRemovedMobs() {
					delete(mobs, mid)
				}
			}
		}
		if hit == nil || !strings.HasPrefix(hit.Target, "m:") {
			t.Fatalf("no Throw hit a slime in 30 s; refusals %v", refusals)
		}
		if used.GetCaster() != id || used.GetSkill() != "throw" || used.GetStage() != 0 {
			t.Fatalf("skill used %+v", used)
		}
		if refusals["u-0"] != code("SKILL_PASSIVE") || refusals["u-1-again"] != code("SKILL_COOLING_DOWN") || refusals["u-1"] != 0 {
			t.Fatalf("refusals %v; want Second Wind passive and each second Throw cooling down", refusals)
		}
		seen := false
		for deadline := time.Now().Add(3 * time.Second); !seen && time.Now().Before(deadline); {
			m, ok := watcher.read(time.Until(deadline))
			if !ok {
				break
			}
			if u, isUsed := m.Data.(*clientv1.SkillUsed); isUsed && u.GetCaster() == id {
				seen = true
			}
		}
		if !seen {
			t.Fatal("the other player did not see the skill used")
		}
	})
}
