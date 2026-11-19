package bots_test

import (
	"testing"

	"finga/internal/application/world/bots"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// decideIn records seen on every tick up to at, then has the bot decide at it, team its side's.
func (f *fight) decideIn(team *bots.Team, at uint64, seen ...bots.Seen) bots.Decision {
	for k := at - min(at, 19); k <= at; k++ {
		f.ring.Record(k, seen)
	}
	return f.brain.Decide(bots.View{Tick: at, Fighting: true, Self: f.self, Ring: f.ring, Team: team, Holds: f.holds})
}

// other is a fighter on side, dx and dz metres from the bot, with hp of 100.
func (f *fight) other(side int, dx, dz float64, hp int) bots.Seen {
	s := f.it
	s.ID, s.Side, s.HP, s.MaxHP = kernel.NewCharacterID(), side, hp, 100
	s.Body.X, s.Body.Z = f.self.State.X+dx, f.self.State.Z+dz
	return s
}

// south is whether keys walk the bot south, round the ring's posts as its path goes.
func south(k sim.Input) bool { return k.Down && !k.Up }

// lockOf is who a decision's attack or skill is locked on.
func lockOf(d bots.Decision) kernel.CharacterID {
	if d.Attack != nil {
		return d.Attack.Lock.Player
	}
	if d.Skill != nil {
		return d.Skill.Target.Player
	}
	return kernel.CharacterID{}
}

// A side's bots fight the enemy with the lowest HP share within 12 m of one of them, and hold it 2 s: a bot that sees another lower meanwhile stays on it, unless it goes down, and lets it go between fights. In 1v1 a bot fights the nearest.
func TestASideFocusesItsLowestEnemyNearIt(t *testing.T) {
	f := newFight(t, "warrior", sure())
	f.self.Buffs = []string{"war_cry"}
	full, low := f.other(2, 0, 1.2, 100), f.other(2, 0, -1.3, 30)
	if got := lockOf(f.decideIn(nil, 20, f.me, full, low)); got != full.ID {
		t.Fatalf("in 1v1 the bot fought %v, want the nearest", got)
	}
	team := &bots.Team{}
	if got := lockOf(f.decideIn(team, 22, f.me, full, low)); got != low.ID {
		t.Fatalf("the bot fought %v, want the enemy at 30%%", got)
	}
	mate := newFight(t, "warrior", sure()) // a teammate's own brain, sharing the side's focus
	mate.self.Buffs = f.self.Buffs
	full.HP, low.HP = 50, 100
	if got := lockOf(mate.decideIn(team, 24, mate.me, full, low)); got != low.ID {
		t.Fatalf("within the hold a teammate fought %v, want the side's focus", got)
	}
	if got := lockOf(mate.decideIn(team, 22+40, mate.me, full, low)); got != full.ID {
		t.Fatalf("after 2 s the side fought %v, want the one now lowest", got)
	}
	f.brain.Decide(bots.View{Tick: 64, Self: f.self, Ring: f.ring, Team: team}) // the round ends
	full.HP, low.HP = 100, 30
	if got := lockOf(f.decideIn(team, 66, f.me, full, low)); got != low.ID {
		t.Fatalf("the next round's fight began on %v, want a focus picked afresh", got)
	}

	f = newFight(t, "warrior", sure())
	f.self.Buffs = []string{"war_cry"}
	team = &bots.Team{}
	full, low = f.other(2, 0, 1.2, 100), f.other(2, 0, -1.3, 30)
	f.decideIn(team, 20, f.me, full, low)
	low.Up = false
	if got := lockOf(f.decideIn(team, 22, f.me, full, low)); got != full.ID {
		t.Fatalf("with its focus down the bot fought %v, want the other at once", got)
	}

	f = newFight(t, "warrior", sure())
	f.self.Buffs = []string{"war_cry"}
	team = &bots.Team{}
	full, far := f.other(2, 0, 1.2, 100), f.other(2, 0, -15, 30)
	if got := lockOf(f.decideIn(team, 20, f.me, full, far)); got != full.ID {
		t.Fatalf("with the low enemy 15 m from the side the bot fought %v, want the nearest", got)
	}
	near := f.other(1, 0, -13, 100) // a teammate 2 m from it
	d := f.decideIn(&bots.Team{}, 22, f.me, full, far, near)
	if d.Keys != (sim.Input{Down: true}) || d.Attack != nil {
		t.Fatalf("with a teammate 2 m from the low enemy: %+v, want the bot walking south to it, not attacking", d)
	}
}

// A bot heals the lowest of its side under 60%, itself among them, once its heal would take them, as the world deals a heal; out of reach it walks to them and keeps its energy. Not with its heal cooling, and not in 1v1.
func TestAHealGoesToTheLowestOfTheSide(t *testing.T) {
	heal := func(d bots.Decision) bool { return d.Skill != nil && d.Skill.Skill == "heal" }
	setUp := func() *fight {
		f := newFight(t, "magician", sure())
		f.self.HP, f.self.MaxHP = 100, 100
		f.enemyAt(5.5) // in a magician's band: it holds still
		return f
	}
	f := setUp()
	if d := f.decideIn(&bots.Team{}, 20, f.me, f.it, f.other(1, 2, 0, 50)); !heal(d) {
		t.Fatalf("a teammate at 50%% 2 m off: %+v, want a heal", d)
	}
	f = setUp()
	if d := f.decideIn(&bots.Team{}, 20, f.me, f.it, f.other(1, 2, 0, 70)); heal(d) {
		t.Fatalf("a teammate at 70%%: %+v, want no heal", d)
	}
	f = setUp()
	f.self.HP = 50
	if d := f.decideIn(&bots.Team{}, 20, f.me, f.it); !heal(d) {
		t.Fatalf("the bot at 50%%: %+v, want it healing itself", d)
	}
	f = setUp()
	f.self.HP = 30
	if d := f.decideIn(nil, 20, f.me, f.it); heal(d) {
		t.Fatalf("in 1v1 at 30%%: %+v, want no heal", d)
	}
	f = setUp()
	if d := f.decideIn(&bots.Team{}, 20, f.me, f.it, f.other(1, 0, -10, 40)); d.Skill != nil || !south(d.Keys) {
		t.Fatalf("a teammate at 40%% 10 m south: %+v, want the bot walking to them, pressing no skill", d)
	}
	f = setUp()
	f.self.Ready["heal"] = 1 << 20
	if d := f.decideIn(&bots.Team{}, 20, f.me, f.it, f.other(1, 0, -10, 40)); heal(d) || d.Keys != (sim.Input{}) {
		t.Fatalf("its heal cooling: %+v, want it holding its range", d)
	}
	// Hurt itself, with two hurt nearer than the lowest, a heal's three targets go to itself and those two.
	f = setUp()
	f.self.HP = 55
	lowest := f.other(1, 0, -3, 40)
	d := f.decideIn(&bots.Team{}, 20, f.me, f.it, f.other(1, 1, 0, 50), f.other(1, -2, 0, 45), lowest)
	if heal(d) || !south(d.Keys) {
		t.Fatalf("the lowest third nearest: %+v, want the bot walking to them, no heal yet", d)
	}
}
