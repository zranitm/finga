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
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// east is the yaw of a row's aim at a mob placed east of it.
const east = float32(math.Pi / 2)

func TestFightAndGrow(t *testing.T) {
	t.Parallel()
	// One stack per row, so the rows run side by side and each stays under its own login throttle.

	t.Run("C1/mobs-appear-wander-and-are-the-same-for-everyone", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.CreateCharacter(t, "fiona", "Fiona", "bowman")
		// Between the slimes' camp and the mushrooms', where the block of interest cells holds both: the snapshot holds the mobs of those cells alone.
		inView := e2etest.Spots(t).InView
		s.Place(t, id, "willowmere", inView, 134, 0)
		p, snap := s.Join(t, token, id)
		if len(snap.Mobs) < 3 {
			t.Fatalf("Sunmeadow snapshot has %d mobs", len(snap.Mobs))
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
		game := p.Game(3 * time.Second)
		for !moved {
			m, ok := game.Next()
			if !ok {
				break
			}
			if m.Type != "Delta" {
				continue
			}
			for _, mv := range m.Data.(*clientv1.Delta).Mobs {
				for _, sm := range snap.Mobs {
					if sm.GetId() == mv.GetId() && (sm.X != mv.X || sm.Z != mv.Z) {
						moved = true
					}
				}
			}
		}
		if !moved {
			t.Fatal("no mob moved within 3 s")
		}
		token2, id2 := s.CreateCharacter(t, "greg", "Greg", "warrior")
		s.Place(t, id2, "willowmere", gamedata.Point{X: inView.X + 0.625, Z: inView.Z}, 144, 0)
		_, snap2 := s.Join(t, token2, id2)
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
		token, id := s.CreateCharacter(t, "hana", "Hana", "bowman")
		s.Place(t, id, "willowmere", e2etest.Spots(t).SlimeField, 134, 0)
		p := e2etest.NewLooter(s.Join(t, token, id))
		token2, id2 := s.CreateCharacter(t, "ivan", "Ivan", "warrior")
		s.Place(t, id2, "willowmere", gamedata.Point{X: e2etest.Spots(t).SlimeField.X - 0.625, Z: e2etest.Spots(t).SlimeField.Z}, 144, 0)
		watcher, _ := s.Join(t, token2, id2)
		p.Await(300*time.Millisecond, nil)
		var hit *clientv1.Damage
		ok := p.AttackUntil(20*time.Second, func(m wsconn.Message) bool {
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
		game := watcher.Game(3 * time.Second)
		for !seen {
			m, ok := game.Next()
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

	t.Run("C3/at-zero-hp-the-character-dies-and-gets-up-beside-the-nearest-waypoint", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		data, err := load.Dir(s.Data)
		if err != nil {
			t.Fatal(err)
		}
		willow, _ := data.Map("willowmere")
		_, up, _ := data.GetUp(willow, e2etest.Spots(t).AmongSlimes.X, e2etest.Spots(t).AmongSlimes.Z) // where the data puts a get-up, the nearest stone's arrival
		token, id := s.CreateCharacter(t, "joan", "Joan", "warrior")
		s.Place(t, id, "willowmere", e2etest.Spots(t).AmongSlimes, 1, 10) // one hit from death; level 1 with 10 EXP
		p, _ := s.Hello(t, token, id)
		p.DieAmongSlimes(t)
		r := p.Expect("Respawned").Data.(*clientv1.Respawned)
		if r.GetCharacterId() != id || r.GetMapId() != "willowmere" || r.GetHp() != 72 || r.GetX() != up.X || r.GetZ() != up.Z {
			t.Fatalf("respawned %+v; want 72 HP in Willowmere beside the stone at %+v", r, up)
		}
		// The death penalty took 5% of level 1's 20 EXP (10 → 9); a get-up on the same map isn't checkpointed at once, so a restart's flush saves it with the stone's position.
		s.Restart()
		var exp int64
		var mapID string
		var x, z float64
		if err := s.DB.QueryRow(context.Background(), `SELECT exp, map_id, pos_x, pos_z FROM characters WHERE id = $1`, id).Scan(&exp, &mapID, &x, &z); err != nil {
			t.Fatal(err)
		}
		if exp != 9 || mapID != "willowmere" || x != up.X || z != up.Z {
			t.Fatalf("saved exp %d on map %s at (%v, %v), want 9 on willowmere at %+v", exp, mapID, x, z, up)
		}
	})

	t.Run("C4/a-kill-gives-exp-and-levels-up-and-the-level-is-saved", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.CreateCharacter(t, "kim", "Kim", "bowman")
		s.Place(t, id, "willowmere", e2etest.Spots(t).SlimeField, 134, 18) // two EXP short of level 2
		p := e2etest.NewLooter(s.Join(t, token, id))
		p.Await(300*time.Millisecond, nil)
		var lu *clientv1.LevelUp
		var gained bool
		ok := p.AttackUntil(40*time.Second, func(m wsconn.Message) bool {
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
			list := s.Call("GET", "/api/characters", p.Token, nil)
			for _, c := range list.Body["characters"].([]any) {
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
		token, id := s.CreateCharacter(t, "lena", "Lena", "warrior")
		if _, err := s.DB.Exec(context.Background(), `UPDATE characters SET level = 2, unspent_points = 5 WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		l, w := s.Hello(t, token, id)
		before := w.GetCharacter().GetSheet()
		if p := before.GetPoints(); p.GetUnspent() != 5 || !p.GetFreeResetLeft() || p.GetSpent().GetStr() != 0 || before.GetAttackLengthTicks() == 0 {
			t.Fatalf("welcome sheet %+v; want the level's 5 points unspent, the free reset left and an attack length", before)
		}
		saved := func(want string, q string) {
			t.Helper()
			var ok bool
			for dl := time.Now().Add(5 * time.Second); time.Now().Before(dl) && !ok; time.Sleep(100 * time.Millisecond) {
				if err := s.DB.QueryRow(context.Background(), q, id).Scan(&ok); err != nil {
					t.Fatal(err)
				}
			}
			if !ok {
				t.Fatalf("not saved within 5 s: %s", want)
			}
		}

		l.Send(wsconn.Message{ReqID: "s-1", Data: &clientv1.SpendPoints{ExpectedUnspent: 5, Points: &clientv1.Stats{Str: 3, Vit: 2}}})
		got := l.Gather("Response", "StatsChanged")
		if !got["Response"].Data.(*clientv1.Response).GetOk() {
			t.Fatalf("spend answered %+v", got["Response"])
		}
		spent := got["StatsChanged"].Data.(*clientv1.StatsChanged).GetSheet()
		if p := spent.GetPoints(); p.GetUnspent() != 0 || p.GetSpent().GetStr() != 3 || p.GetSpent().GetVit() != 2 ||
			spent.GetPrimary().GetStr() != before.GetPrimary().GetStr()+3 || spent.GetDerived().GetMaxHp() <= before.GetDerived().GetMaxHp() {
			t.Fatalf("sheet after spending %+v; want 3 STR and 2 VIT on top of %+v", spent, before)
		}
		saved("3 STR and 2 VIT spent", `SELECT str_points = 3 AND vit_points = 2 AND unspent_points = 0 FROM characters WHERE id = $1`)

		l.Send(wsconn.Message{ReqID: "s-2", Data: &clientv1.SpendPoints{ExpectedUnspent: 5, Points: &clientv1.Stats{Dex: 1}}})
		if r := l.Gather("Response")["Response"]; r.ReqID != "s-2" || r.Data.(*clientv1.Response).GetCode() != code("POINTS_CHANGED") {
			t.Fatalf("a spend from a stale count answered %+v; want POINTS_CHANGED", r)
		}

		l.Send(wsconn.Message{ReqID: "r-1", Data: &clientv1.ResetPoints{ExpectedUnspent: 0}})
		got = l.Gather("Response", "StatsChanged")
		if !got["Response"].Data.(*clientv1.Response).GetOk() {
			t.Fatalf("reset answered %+v", got["Response"])
		}
		if after := got["StatsChanged"].Data.(*clientv1.StatsChanged).GetSheet(); !proto.Equal(after.GetPrimary(), before.GetPrimary()) ||
			after.GetPoints().GetUnspent() != 5 || after.GetPoints().GetFreeResetLeft() {
			t.Fatalf("sheet after the reset %+v; want the stats of %+v, 5 points back and no free reset left", after, before)
		}
		saved("the reset", `SELECT str_points = 0 AND vit_points = 0 AND unspent_points = 5 AND free_reset_used FROM characters WHERE id = $1`)

		l.Send(wsconn.Message{ReqID: "r-2", Data: &clientv1.ResetPoints{ExpectedUnspent: 5}})
		if r := l.Gather("Response")["Response"]; r.ReqID != "r-2" || r.Data.(*clientv1.Response).GetCode() != code("NO_FREE_RESET") {
			t.Fatalf("a second reset answered %+v; want NO_FREE_RESET", r)
		}
	})

	t.Run("C6/a-bat's-spit-is-magic-and-meets-magic-defense", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.CreateCharacter(t, "nell", "Nell", "warrior")
		s.Place(t, id, "greyfang_cave", e2etest.Spots(t).FarBats, 400, 0) // 2 m west of the bats' camp in the flat hall, with no solid between, and out of the boars' sight
		// 100 INT: magic defense 3 + 0.5 × 107 + 0.5 × 7 = 60, physical defense 5 + 7 = 12.
		if _, err := s.DB.Exec(context.Background(), `UPDATE characters SET level = 21, int_points = 100 WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		p, _ := s.Join(t, token, id) // the bats see them and come to spit
		game := p.Game(30 * time.Second)
		for {
			m, ok := game.Next()
			if !ok {
				break
			}
			d, isDamage := m.Data.(*clientv1.Damage)
			if !isDamage || d.Target != "p:"+id || d.Miss {
				continue
			}
			// Attack 21 · 100 / 160 = 13.125, ±10%: 12 to 14; against the physical 12 it would be 17 to 21.
			if !strings.HasPrefix(d.Source, "m:") || d.Amount < 12 || d.Amount > 14 {
				t.Fatalf("damage %+v; want a bat's spit of 12 to 14, meeting magic defense", d)
			}
			return
		}
		t.Fatal("no bat's spit hit the player in 30 s")
	})

	t.Run("C7/skill-points-evolve-a-skill-and-a-passive-feeds-the-sheet-saved-at-once", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.CreateCharacter(t, "mira", "Mira", "beginner")
		if _, err := s.DB.Exec(context.Background(), `UPDATE characters SET unspent_skill_points = 12 WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		l, w := s.Hello(t, token, id)
		if sk := w.GetCharacter().GetSheet().GetSkills(); sk.GetUnspent() != 12 || len(sk.GetInvested()) != 0 {
			t.Fatalf("welcome skills %+v; want 12 unspent and none invested", sk)
		}

		// Ten in Strike is Heavy Strike; two in Second Wind add 0.5 + 0.2 HP a second.
		l.Send(wsconn.Message{ReqID: "k-1", Data: &clientv1.SpendSkillPoints{ExpectedUnspent: 12, Points: map[string]int32{"strike": 10, "second_wind": 2}}})
		got := l.Gather("Response", "StatsChanged")
		if !got["Response"].Data.(*clientv1.Response).GetOk() {
			t.Fatalf("spend answered %+v", got["Response"])
		}
		sheet := got["StatsChanged"].Data.(*clientv1.StatsChanged).GetSheet()
		if sk := sheet.GetSkills(); sk.GetUnspent() != 0 || sk.GetInvested()["strike"] != 10 || sk.GetInvested()["second_wind"] != 2 {
			t.Fatalf("skills after spending %+v; want 10 in Strike, 2 in Second Wind and none left", sk)
		}
		if d := sheet.GetDerivedSkills(); math.Abs(d.GetHpRegen()-0.7) > 1e-9 {
			t.Fatalf("the skills' share %+v; want 0.7 HP a second", d)
		}
		var saved bool
		for dl := time.Now().Add(5 * time.Second); time.Now().Before(dl) && !saved; time.Sleep(100 * time.Millisecond) {
			if err := s.DB.QueryRow(context.Background(), `SELECT unspent_skill_points = 0 AND (SELECT count(*) FROM character_skills WHERE character_id = $1 AND (skill_id, points) IN (('strike', 10), ('second_wind', 2))) = 2 FROM characters WHERE id = $1`, id).Scan(&saved); err != nil {
				t.Fatal(err)
			}
		}
		if !saved {
			t.Fatal("the skill points were not saved within 5 s")
		}

		refused := func(req string, in *clientv1.SpendSkillPoints, want string) {
			t.Helper()
			l.Send(wsconn.Message{ReqID: req, Data: in})
			if r := l.Gather("Response")["Response"]; r.ReqID != req || r.Data.(*clientv1.Response).GetCode() != code(want) {
				t.Fatalf("%s answered %+v; want %s", req, r, want)
			}
		}
		refused("k-2", &clientv1.SpendSkillPoints{ExpectedUnspent: 12, Points: map[string]int32{"strike": 1}}, "SKILL_POINTS_CHANGED")
		refused("k-3", &clientv1.SpendSkillPoints{ExpectedUnspent: 0, Points: map[string]int32{"fireball": 1}}, "SKILL_NOT_LEARNABLE")
		refused("k-4", &clientv1.SpendSkillPoints{ExpectedUnspent: 0, Points: map[string]int32{"strike": -1}}, "BAD_SKILL_POINTS")
		time.Sleep(600 * time.Millisecond) // under the points limit
		refused("k-5", &clientv1.SpendSkillPoints{ExpectedUnspent: 0, Points: map[string]int32{"strike": 1}}, "NOT_ENOUGH_SKILL_POINTS")

		_, again := s.Welcome(t, "mira", id)
		if sk := again.GetCharacter().GetSheet().GetSkills(); sk.GetInvested()["strike"] != 10 || sk.GetInvested()["second_wind"] != 2 || sk.GetUnspent() != 0 {
			t.Fatalf("welcomed back with skills %+v; want the points kept", sk)
		}
	})

	t.Run("C8/a-skill-on-the-bar-is-kept-and-throws-at-a-slime-once-a-cooldown", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.CreateCharacter(t, "nico", "Nico", "beginner")
		// A point in Throw and in Second Wind, as a spend leaves them.
		if _, err := s.DB.Exec(context.Background(), `INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'throw', 1, now(), now()), ($1, 'second_wind', 1, now(), now())`, id); err != nil {
			t.Fatal(err)
		}
		path := "/api/characters/" + id + "/skill-bar"
		bar := []any{"throw", "", "", "", "", "", "", ""}
		s.MustStatus(s.Call("PUT", path, token, map[string]any{"slots": bar}), http.StatusOK)
		if bad := s.Call("PUT", path, token, map[string]any{"slots": []string{"second_wind", "", "", "", "", "", "", ""}}); bad.Status != http.StatusBadRequest || bad.Body["code"] != float64(code("BAD_SKILL_BAR")) {
			t.Fatalf("a passive on the bar answered %d %v; want BAD_SKILL_BAR", bad.Status, bad.Body)
		}
		list := s.Call("GET", "/api/characters", token, nil)
		if got := list.Body["characters"].([]any)[0].(map[string]any)["skill_bar"]; !reflect.DeepEqual(got, bar) {
			t.Fatalf("listed skill bar %v; want %v", got, bar)
		}

		s.Place(t, id, "willowmere", e2etest.Spots(t).SlimeField, 135, 0)
		if _, err := s.DB.Exec(context.Background(), `UPDATE characters SET energy = 10 WHERE id = $1`, id); err != nil { // five Throws
			t.Fatal(err)
		}
		p, _ := s.Hello(t, token, id)
		token2, id2 := s.CreateCharacter(t, "omar", "Omar", "beginner")
		s.Place(t, id2, "willowmere", gamedata.Point{X: e2etest.Spots(t).SlimeField.X - 0.625, Z: e2etest.Spots(t).SlimeField.Z}, 135, 0)
		watcher, _ := s.Join(t, token2, id2)
		p.Await(300*time.Millisecond, nil)

		use := func(req, skill string, aim float32) {
			p.Send(wsconn.Message{ReqID: req, Data: &clientv1.UseSkill{Skill: skill, Aim: aim}})
		}
		use("u-0", "second_wind", 0)
		refusals := map[string]int32{}
		var used *clientv1.SkillUsed
		var hit *clientv1.Damage
		next, n := p.Game(0), 0                                                                            // until the next pair of Throws, in the world's time
		for game := p.Game(60 * time.Second); (hit == nil || refusals["u-1-again"] == 0) && game.Left(); { // a 4.5 s cooldown in the server's ticks
			if !next.Left() {
				aim := p.AimAt(p.Nearest("green_slime"))
				n++
				use(fmt.Sprintf("u-%d", n), "throw", aim)
				use(fmt.Sprintf("u-%d-again", n), "throw", aim) // inside its cooldown
				next = p.Game(2100 * time.Millisecond)
			}
			m, ok := p.Read(100 * time.Millisecond)
			if !ok {
				continue
			}
			p.Observe(m)
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
		for game := watcher.Game(3 * time.Second); !seen; {
			m, ok := game.Next()
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
