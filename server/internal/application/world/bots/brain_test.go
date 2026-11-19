package bots_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"finga/internal/application/world/bots"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/kernel"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
	"finga/internal/shared/steer"
)

const tick = 50 * time.Millisecond

// fight is a bot of a class at a tier on the ring, with the kit's skills and its class's first build, and an enemy to face.
type fight struct {
	t      *testing.T
	data   *gamedata.Data
	setup  bots.Setup
	brain  *bots.Brain
	ring   *bots.Ring
	me, it bots.Seen
	self   bots.Self
	holds  func(root, skill string, lock model.EntityRef) bool
}

func newFight(t *testing.T, class kernel.Class, tier gamedata.BotTier) *fight {
	t.Helper()
	data := gamedatatest.Shipped(t)
	c, _ := data.Class(class)
	m, _ := data.Map("ring")
	build := data.Pvp().Bots.Builds[class][0]
	mv := data.Movement()
	s := bots.Setup{Seed: [2]uint64{1, 2}, Tier: tier, Class: c, Bar: build.Bar, Order: build.Order, Skills: map[string]bots.Skill{}, Map: m, Radius: mv.Radius, Height: mv.Height, Tick: tick}
	for _, sk := range data.Skills() {
		kit := 0
		if !sk.Granted {
			kit = max(sk.Stages[len(sk.Stages)-1].At, 1)
		}
		if points := sk.PointsIn(kit); sk.Kind == gamedata.Active && points > 0 && c.Learnable(sk) {
			s.Skills[sk.ID] = bots.Skill{Skill: sk, Spec: sk.Spec(points, "")}
		}
	}
	at := m.Sides[0][0].At
	body := func(x, z float64) model.Body { return model.Body{X: x, Z: z, Radius: mv.Radius, Height: mv.Height} }
	f := &fight{t: t, data: data, setup: s, brain: bots.New(s), ring: bots.NewRing(20)}
	f.me = bots.Seen{ID: kernel.NewCharacterID(), Side: 1, Body: body(at.X, at.Z), HP: 100, MaxHP: 100, Up: true}
	enemy, _ := data.Class("warrior")
	f.it = bots.Seen{ID: kernel.NewCharacterID(), Side: 2, Body: body(at.X, at.Z+1.5), HP: 100, MaxHP: 100, Up: true, Attack: enemy.Attack.Shape}
	f.self = bots.Self{ID: f.me.ID, Side: 1, State: sim.State{X: at.X, Z: at.Z, OnGround: true, HZ: 1}, Energy: 10, Ready: map[string]uint64{}}
	return f
}

// enemyAt puts the enemy dist metres north of the bot.
func (f *fight) enemyAt(dist float64) { f.it.Body.Z = f.self.State.Z + dist }

// decide records the fighters on every tick up to at, then has the bot decide at it.
func (f *fight) decide(at uint64) bots.Decision {
	for k := at - min(at, 19); k <= at; k++ {
		f.ring.Record(k, []bots.Seen{f.me, f.it})
	}
	return f.brain.Decide(bots.View{Tick: at, Fighting: true, Self: f.self, Ring: f.ring, Holds: f.holds})
}

// swing is the enemy's Charge started at start, aimed at yaw from where it stands, at the bot as its lock: a blow that lands after it starts, as few do.
func (f *fight) swing(start uint64, yaw float64) bots.Started {
	sk, _ := f.data.Skill("charge")
	st := sk.Spec(sk.Stages[len(sk.Stages)-1].At, "")
	return f.started(start, model.TicksIn(st.Length, tick), st.Timing, st.Cancelable, &st.Shape, yaw, st.Move.Distance)
}

// attack is the enemy's basic attack started at start, aimed at the bot: its blow lands as it starts.
func (f *fight) attack(start uint64) bots.Started {
	c, _ := f.data.Class("warrior")
	return f.started(start, model.TicksIn(c.Attack.Length, tick), c.Attack.Timing, true, &c.Attack.Shape, f.towardMe(), 0)
}

// started keeps the enemy's action in the ring as shown at start: a blow's shape aimed at yaw at the bot, after a dash of toward metres; no blow for a buff.
func (f *fight) started(start, length uint64, t gamedata.Timing, cancelable bool, blow *gamedata.Shape, yaw, toward float64) bots.Started {
	hitAt, cancelAt := model.ActionPoints(length, t.Hit, t.Cancel)
	s := bots.Started{By: f.it.ID, Shown: start, Start: start, Hit: start + hitAt, Next: start + length}
	if blow != nil {
		s.Blow, s.Shape, s.Aim, s.Lock, s.Toward = true, *blow, model.Aim{X: f.it.Body.X, Z: f.it.Body.Z, Yaw: yaw}, model.EntityRef{Player: f.me.ID}, toward
	}
	if cancelable {
		s.Next = start + cancelAt
	}
	f.ring.Start(s)
	return s
}

func (f *fight) towardMe() float64 {
	return float64(steer.YawTo(nav.Point{X: f.it.Body.X, Z: f.it.Body.Z}, nav.Point{X: f.me.Body.X, Z: f.me.Body.Z}))
}

func sure() gamedata.BotTier {
	return gamedata.BotTier{ID: "sure", Reaction: 100 * time.Millisecond, Defend: 1, Chain: 1, BreakFree: 300 * time.Millisecond}
}

func defends(d bots.Decision) string {
	if d.Skill != nil && (d.Skill.Skill == "bastion" || d.Skill.Skill == "roll_back") {
		return d.Skill.Skill
	}
	return ""
}

// Defend: a Charge that will reach the bot is blocked facing the swinger, or dodged with the block cooling; not when aimed away, unseen, or at a tier that never defends.
func TestABotDefendsASwingThatReachesItAtTheHit(t *testing.T) {
	f := newFight(t, "warrior", sure())
	s := f.swing(20, f.towardMe())
	d := f.decide(22)
	if defends(d) != "bastion" || math.Abs(float64(d.Skill.Aim)-float64(steer.YawTo(nav.Point{X: f.me.Body.X, Z: f.me.Body.Z}, nav.Point{X: f.it.Body.X, Z: f.it.Body.Z}))) > 1e-6 {
		t.Fatalf("seeing a swing that reaches it: %+v, want Bastion facing the swinger", d.Skill)
	}
	if s.Hit <= 22 {
		t.Fatalf("the swing's hit at %d, want it after the decision", s.Hit)
	}

	f = newFight(t, "warrior", sure())
	f.self.Ready["bastion"] = 1000
	f.swing(20, f.towardMe())
	d = f.decide(22)
	bx, bz := d.Skill.Way.X, d.Skill.Way.Z
	if defends(d) != "roll_back" || !(bz < -0.99) || math.Abs(math.Hypot(bx, bz)-1) > 1e-9 {
		t.Fatalf("with Bastion cooling down: %+v, want Roll Back straight away from the swinger", d.Skill)
	}

	f = newFight(t, "warrior", sure())
	f.enemyAt(5)
	f.swing(20, f.towardMe())
	if d := f.decide(22); defends(d) != "bastion" {
		t.Fatalf("a Charge from 5 m, past the swinger's own reach: %+v, want Bastion, as it lands from the end of its dash", d.Skill)
	}

	for _, c := range []struct {
		name  string
		tier  func(gamedata.BotTier) gamedata.BotTier
		yaw   func(*fight) float64
		start uint64
	}{
		{"aimed away, the swinger's own attack too short for a next blow", func(t gamedata.BotTier) gamedata.BotTier { return t }, func(f *fight) float64 { f.it.Attack = gamedata.Shape{}; return f.towardMe() + math.Pi }, 20},
		{"not yet seen", func(t gamedata.BotTier) gamedata.BotTier { return t }, (*fight).towardMe, 21},
		{"a tier that never defends", func(t gamedata.BotTier) gamedata.BotTier { t.Defend = 0; return t }, (*fight).towardMe, 20},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFight(t, "warrior", c.tier(sure()))
			f.swing(c.start, c.yaw(f))
			if d := f.decide(22); defends(d) != "" {
				t.Fatalf("pressed %s", defends(d))
			}
		})
	}
}

// A slow reaction can't beat a fast swing: a bot that sees the swing only as it lands has nothing that runs over the hit.
func TestASlowReactionCantBeatAFastSwing(t *testing.T) {
	slow := sure()
	slow.Reaction = 450 * time.Millisecond
	f := newFight(t, "warrior", slow)
	s := f.swing(20, f.towardMe())
	for at := uint64(20); at <= s.Hit; at++ {
		if d := f.decide(at); defends(d) != "" {
			t.Fatalf("at %d, the hit at %d: pressed %s", at, s.Hit, defends(d))
		}
	}
}

// Break free: held by a hard status, the bot presses its class's break-free from its root once its tier's break_free has passed, and nothing before; a tier that never defends stays held.
func TestABotBreaksFreeAfterItsTiersDelay(t *testing.T) {
	f := newFight(t, "warrior", sure())
	f.self.Hard = true
	for at := uint64(20); at < 26; at += 2 {
		if d := f.decide(at); d.Skill != nil || d.Attack != nil || d.Keys != (sim.Input{}) {
			t.Fatalf("held at %d: %+v, want nothing before 300 ms", at, d)
		}
	}
	d := f.decide(26)
	if d.Skill == nil || d.Skill.Skill != "unyielding" || d.Skill.Root != "roll_back" {
		t.Fatalf("held 300 ms: %+v, want Unyielding from Roll Back", d.Skill)
	}

	never := sure()
	never.Defend = 0
	f = newFight(t, "warrior", never)
	f.self.Hard = true
	for at := uint64(20); at < 60; at += 2 {
		if d := f.decide(at); d.Skill != nil {
			t.Fatalf("at %d: pressed %s", at, d.Skill.Skill)
		}
	}
}

// Follow up: a follow-up a key on the bar offers, by the world's judgement, is taken from its root at the tier's chain chance; at none it isn't.
func TestABotTakesTheFollowUpItsKeysOffer(t *testing.T) {
	for _, chain := range []float64{1, 0} {
		tier := sure()
		tier.Chain = chain
		f := newFight(t, "warrior", tier)
		f.enemyAt(5)
		f.holds = func(root, skill string, lock model.EntityRef) bool {
			return root == "strike" && skill == "charge" && lock.Player == f.it.ID
		}
		d := f.decide(20)
		took := d.Skill != nil && d.Skill.Skill == "charge" && d.Skill.Root == "strike"
		if took != (chain == 1) {
			t.Fatalf("at chain %v: %+v", chain, d.Skill)
		}
	}
}

// Range: a warrior walks in to its attack's reach and attacks there; a bowman walks in from past 9 m to 6 m, stands between, and steps away inside 3 m.
func TestABotKeepsItsRange(t *testing.T) {
	f := newFight(t, "warrior", sure())
	f.enemyAt(8)
	if d := f.decide(20); d.Keys != (sim.Input{Up: true}) || d.Attack != nil {
		t.Fatalf("a warrior 8 m off: %+v, want it walking north, not attacking", d)
	}
	f.enemyAt(1.2)
	if d := f.decide(22); d.Keys != (sim.Input{}) || d.Attack == nil || d.Attack.Lock.Player != f.it.ID {
		t.Fatalf("a warrior 1.2 m off: %+v, want it standing and attacking", d)
	}

	f = newFight(t, "bowman", sure())
	for k, c := range []struct {
		dist float64
		keys sim.Input
	}{
		{12, sim.Input{Up: true}},
		{7, sim.Input{Up: true}}, // still walking in, to 6 m
		{5.5, sim.Input{}},
		{7, sim.Input{}},
		{2, sim.Input{Down: true}},
	} {
		f.enemyAt(c.dist)
		if d := f.decide(uint64(20 + 2*k)); d.Keys != c.keys {
			t.Fatalf("a bowman %v m off: %+v, want %+v", c.dist, d.Keys, c.keys)
		}
	}
}

// A bot aims at its target off by up to its tier's aim, the same each time from one seed.
func TestABotAimsOffByItsTiersAim(t *testing.T) {
	loose := sure()
	loose.Aim = 0.2
	var aims []float32
	for range 2 {
		f := newFight(t, "warrior", loose)
		f.enemyAt(1.2)
		var got []float32
		for at := uint64(20); at < 60; at += 2 {
			if d := f.decide(at); d.Attack != nil {
				got = append(got, d.Attack.Aim)
			}
		}
		if len(got) == 0 {
			t.Fatal("no attack")
		}
		if aims != nil && !slices.Equal(aims, got) {
			t.Fatalf("aims %v then %v from one seed", aims, got)
		}
		aims = got
	}
	for _, a := range aims {
		if math.Abs(float64(a)) > 0.2+1e-6 {
			t.Fatalf("aimed %v off north, past 0.2", a)
		}
	}
}

// guards is the tick of the bot's first dodge or block from from to to, deciding every tick, and the skill; 0 for none.
func (f *fight) guards(from, to uint64) (uint64, string) {
	for at := from; at <= to; at++ {
		if d := f.decide(at); defends(d) != "" {
			return at, defends(d)
		}
	}
	return 0, ""
}

// The next blow: a bot guards the blow that may follow a basic attack, at its cancel point, when its tier sees the attack in time.
func TestASlowTierMissesAShortActionAFastTierGuards(t *testing.T) {
	data := gamedatatest.Shipped(t)
	for _, tier := range data.Pvp().Bots.Tiers {
		tier.Defend = 1
		f := newFight(t, "warrior", tier)
		s := f.attack(20)
		at, _ := f.guards(20, s.Next)
		reaction := model.TicksIn(tier.Reaction, tick)
		switch {
		case at != 0 && at < s.Shown+reaction:
			t.Fatalf("%s guarded at %d, before its reaction of %d ticks since %d", tier.ID, at, reaction, s.Shown)
		case (at != 0) != (s.Shown+reaction <= s.Next):
			t.Fatalf("%s, %d ticks of reaction against a next blow %d ticks on: guarded at %d", tier.ID, reaction, s.Next-s.Shown, at)
		}
		t.Logf("%s: reaction %d ticks, next blow at +%d, guarded at %d", tier.ID, reaction, s.Next-s.Shown, at)
	}
}

// Counterplay, each tier: a blow held past the bot's guess lands, the guard over and cooling.
func TestABlowHeldPastTheGuessLandsOnEachTier(t *testing.T) {
	data := gamedatatest.Shipped(t)
	for _, tier := range data.Pvp().Bots.Tiers {
		t.Run(tier.ID, func(t *testing.T) {
			tier.Defend = 1
			f := newFight(t, "warrior", tier)
			reaction := model.TicksIn(tier.Reaction, tick)
			s := f.started(20, reaction+6, gamedata.Timing{}, false, nil, 0, 0) // a buff's long action, which every tier sees in time
			at, id := f.guards(20, s.Next)
			if at == 0 {
				t.Fatalf("no guard over the next blow at %d", s.Next)
			}
			st := f.setup.Skills[id].Spec
			over := at + model.TicksIn(max(st.Dodge, st.Block), tick)
			f.self.Ready[id] = at + model.TicksIn(st.Cooldown, tick) // the world starts its cooldown
			blow := f.attack(over)                                   // held past the guard
			if later, other := f.guards(at+1, blow.Hit); later != 0 {
				t.Fatalf("guarded again at %d with %s, before the held blow at %d", later, other, blow.Hit)
			}
			if f.self.Ready[id] <= blow.Hit {
				t.Fatalf("%s ready again at %d, by the held blow at %d", id, f.self.Ready[id], blow.Hit)
			}
		})
	}
}

// No guard before the bot has seen the action: through its reaction since the world showed it, it presses nothing, though a guard then would cover the next blow; once it sees it, it guards.
func TestNoGuardBeforeTheBotHasSeenTheAction(t *testing.T) {
	f := newFight(t, "warrior", sure())
	s := f.started(20, 6, gamedata.Timing{}, false, nil, 0, 0)
	if at, id := f.guards(18, s.Shown+1); at != 0 {
		t.Fatalf("guarded at %d with %s, before seeing it at %d", at, id, s.Shown+2)
	}
	if at, _ := f.guards(s.Shown+2, s.Next); at != s.Shown+2 {
		t.Fatalf("guarded at %d, want as soon as it saw it, at %d", at, s.Shown+2)
	}
}

// An enemy seen down or held has no blow coming: a hard status ends an action under way, so the bot guards neither its swing nor its next blow.
func TestNoGuardAgainstAnEnemyDownOrHeld(t *testing.T) {
	for _, c := range []struct {
		name string
		set  func(*bots.Seen)
	}{
		{"held", func(s *bots.Seen) { s.Hard = true }},
		{"down", func(s *bots.Seen) { s.Up = false }},
	} {
		f := newFight(t, "warrior", sure())
		f.swing(20, f.towardMe())
		c.set(&f.it)
		if d := f.decide(22); defends(d) != "" {
			t.Errorf("an enemy %s: %+v, want no guard", c.name, d.Skill)
		}
	}
}

// One guard for each action, even one longer than the ring: a 2 s blow landing at 1.5 s is guarded once as its hit comes, and not again while it still runs.
func TestOneGuardForAnActionLongerThanTheRing(t *testing.T) {
	f := newFight(t, "warrior", sure())
	c, _ := f.data.Class("warrior")
	s := f.started(20, 40, gamedata.Timing{Hit: 0.75}, false, &c.Attack.Shape, f.towardMe(), 0)
	var guards []uint64
	for at := uint64(22); at <= s.Hit; at++ {
		if d := f.decide(at); defends(d) != "" {
			guards = append(guards, at)
			f.self.Ready[d.Skill.Skill] = at + 40 // cooling, as the world shows it
		}
	}
	if len(guards) != 1 {
		t.Fatalf("guards at %v against a blow landing at %d, want one", guards, s.Hit)
	}
}
