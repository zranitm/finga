package e2e_test

import (
	"context"
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// combatTicks is actions.yaml's combat, 5 s, in the world's 20 Hz ticks.
const combatTicks = 100

// C20 §9: a Beginner with 24 points in Strike is refused Concussion with TRAIT_LOCKED; with 25 they choose it, and Crushing Blow stuns a slime.
// A change during the fight is refused with IN_COMBAT; walked out of the slimes' sight, it goes through once the last fight event is 5 s of ticks past.
// Judged by the world's ticks: the choice let through came 5 s of ticks after the last Damage naming the player, and none sent after a frame that late was refused.
func TestATraitOpensAtTheLastStageAndWaitsOutAFight(t *testing.T) {
	t.Parallel()
	// Slimes that live through Crushing Blow, so its stun shows.
	s := newStackOver(t, e2etest.GameData(t, e2etest.DataEdit{File: "mobs.yaml", Old: "hp: 34\n", New: "hp: 1000\n"}))
	token, id := s.CreateCharacter(t, "trait", "Trait", "beginner")
	for _, q := range []string{
		`INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'strike', 24, now(), now())`,
		`UPDATE characters SET energy = 10, unspent_skill_points = 1 WHERE id = $1`,
	} {
		if _, err := s.DB.Exec(context.Background(), q, id); err != nil {
			t.Fatal(err)
		}
	}
	s.Place(t, id, "forest", e2etest.OutOfSight, 134, 0)
	l, _ := s.Hello(t, token, id)

	var frame, lastFight, chosenAt uint64 // the latest frame's tick, the latest Damage's naming the player, and the latest sheet's
	var fights []uint64                   // each Damage's naming the player
	var chosen *clientv1.StatsChanged
	answers := map[string]*clientv1.Response{}
	stunned, used := false, ""
	watch := func(m wsconn.Message) {
		switch d := m.Data.(type) {
		case *clientv1.Delta:
			frame = m.Tick
			for _, mv := range d.GetMobs() {
				stunned = stunned || slices.ContainsFunc(mv.GetStatuses(), func(s *clientv1.StatusView) bool { return s.GetId() == "stun" })
			}
		case *clientv1.Damage:
			if d.GetSource() == "p:"+id || d.GetTarget() == "p:"+id {
				lastFight, fights = max(lastFight, m.Tick), append(fights, m.Tick)
			}
		case *clientv1.Response:
			answers[m.ReqID] = d
		case *clientv1.SkillUsed:
			if d.GetCaster() == id {
				used = d.GetTrait()
			}
		case *clientv1.StatsChanged:
			if d.GetSheet() != nil {
				chosen, chosenAt = d, m.Tick
			}
		}
	}
	ask := func(req string, data any) *clientv1.Response {
		t.Helper()
		l.Send(wsconn.Message{ReqID: req, Data: data})
		l.Pump(5*time.Second, func(m wsconn.Message) bool { watch(m); return answers[req] != nil })
		if answers[req] == nil {
			t.Fatalf("%s unanswered within 5 s", req)
		}
		return answers[req]
	}
	sheetWith := func(trait string) bool {
		return chosen != nil && chosen.GetSheet().GetSkills().GetTraits()["strike"] == trait
	}

	if r := ask("locked", &clientv1.ChooseTrait{Skill: "strike", Trait: "concussion"}); r.GetCode() != code("TRAIT_LOCKED") {
		t.Fatalf("Concussion at 24 points answered %+v, want TRAIT_LOCKED", r)
	}
	if r := ask("spend", &clientv1.SpendSkillPoints{ExpectedUnspent: 1, Points: map[string]int32{"strike": 1}}); !r.GetOk() {
		t.Fatalf("the 25th point answered %+v", r)
	}
	if r := ask("choose", &clientv1.ChooseTrait{Skill: "strike", Trait: "concussion"}); !r.GetOk() {
		t.Fatalf("Concussion at 25 points answered %+v", r)
	}
	if !l.Pump(5*time.Second, func(m wsconn.Message) bool { watch(m); return sheetWith("concussion") }) {
		t.Fatalf("no sheet with Concussion within 5 s; last %+v", chosen)
	}

	// Into the slimes: Crushing Blow at the nearest, again once it's ready if it missed, then a change mid-fight.
	var strikes []string
	var next time.Time
	trail := []gamedata.Point{{X: l.X, Z: l.Z}} // where the chase went, to walk back along
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline) && (!stunned || used == "" || answers["fight"] == nil); {
		if err := l.Chase(); err != nil {
			t.Fatal(err)
		}
		if last := trail[len(trail)-1]; math.Hypot(l.X-last.X, l.Z-last.Z) > 1 {
			trail = append(trail, gamedata.Point{X: l.X, Z: l.Z})
		}
		if m := l.Nearest("green_slime"); !stunned && !time.Now().Before(next) && m != nil && math.Hypot(m.GetX()-l.X, m.GetZ()-l.Z) < 2 {
			req := fmt.Sprintf("strike-%d", len(strikes))
			strikes, next = append(strikes, req), time.Now().Add(7500*time.Millisecond) // Concussion's 7 s cooldown
			l.Send(wsconn.Message{ReqID: req, Seq: l.Seq, Data: &clientv1.UseSkill{Skill: "strike", ChainRoot: "strike", Aim: l.AimAt(m), Target: e2etest.MobRef(m.GetId())}})
		}
		if stunned && used != "" && answers["fight"] == nil {
			l.Send(wsconn.Message{ReqID: "fight", Data: &clientv1.ChooseTrait{Skill: "strike", Trait: "swift"}})
			answers["fight"] = &clientv1.Response{} // sent; its answer replaces this
		}
		l.Pump(50*time.Millisecond, func(m wsconn.Message) bool { watch(m); return false })
	}
	for _, req := range strikes {
		if r := answers[req]; r != nil && !r.GetOk() {
			t.Fatalf("%s answered %+v", req, r)
		}
	}
	if len(strikes) == 0 || used != "concussion" || !stunned {
		t.Fatalf("Strike pressed %d times, used with %q, a slime stunned %v; want Crushing Blow with Concussion stunning one", len(strikes), used, stunned)
	}
	l.Pump(5*time.Second, func(m wsconn.Message) bool {
		watch(m)
		return answers["fight"].GetCode() != 0 || answers["fight"].GetOk()
	})
	if r := answers["fight"]; r.GetCode() != code("IN_COMBAT") {
		t.Fatalf("a change mid-fight answered %+v, want IN_COMBAT", r)
	}

	// Back out of the slimes' sight along the chase's trail, jumping when stuck as WalkTo does, then a change every 600 ms until one goes through.
	lastX, lastZ, stuck := l.X, l.Z, 0
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline) && len(trail) > 0; {
		to := trail[len(trail)-1]
		if math.Hypot(to.X-l.X, to.Z-l.Z) < 0.5 {
			trail = trail[:len(trail)-1]
			continue
		}
		if math.Hypot(l.X-lastX, l.Z-lastZ) < 0.03 {
			stuck++
		} else {
			stuck = 0
		}
		lastX, lastZ = l.X, l.Z
		in := e2etest.Toward(to.X-l.X, to.Z-l.Z)
		in.Jump = stuck >= 3
		if err := l.Input(in); err != nil {
			t.Fatal(err)
		}
		l.Pump(50*time.Millisecond, func(m wsconn.Message) bool { watch(m); return false })
	}
	if err := l.Input(&clientv1.Input{}); err != nil {
		t.Fatal(err)
	}
	sentAfter := map[string]uint64{} // each try's req, and the frame it went after
	var through string
	for k, deadline := 0, time.Now().Add(15*time.Second); time.Now().Before(deadline) && through == ""; k++ {
		req := fmt.Sprintf("out-%d", k)
		sentAfter[req] = frame
		r := ask(req, &clientv1.ChooseTrait{Skill: "strike", Trait: "swift"})
		switch {
		case r.GetOk():
			through = req
		case r.GetCode() != code("IN_COMBAT"):
			t.Fatalf("%s answered %+v", req, r)
		case sentAfter[req] >= lastFight+combatTicks:
			t.Fatalf("%s refused IN_COMBAT, sent after frame %d, 5 s of ticks past the last fight event at %d", req, sentAfter[req], lastFight)
		}
		l.Pump(600*time.Millisecond, func(m wsconn.Message) bool { watch(m); return false })
	}
	if through == "" || !l.Pump(5*time.Second, func(m wsconn.Message) bool { watch(m); return sheetWith("") }) {
		t.Fatalf("no change went through within 15 s of leaving; the last fight event at tick %d, the latest frame %d", lastFight, frame)
	}
	before := slices.Max(slices.DeleteFunc(fights, func(at uint64) bool { return at > chosenAt })) // a slime may hit again once it's chosen
	if chosenAt < before+combatTicks {
		t.Fatalf("Swift chosen at tick %d, under 5 s of ticks after the last fight event at %d", chosenAt, before)
	}
	t.Logf("chosen at tick %d, %d ticks after the last fight event; %d tries refused, from (%.1f, %.1f)", chosenAt, chosenAt-before, len(sentAfter)-1, l.X, l.Z)
}
