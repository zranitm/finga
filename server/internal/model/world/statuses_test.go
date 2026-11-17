package world_test

import (
	"testing"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
)

// The statuses as statuses.yaml gives them, a factor unnamed being 1.
var (
	ones = gamedata.Status{MoveSpeed: 1, Accuracy: 1, Attack: 1, PhysicalDefense: 1, MagicDefense: 1, Moving: 1}
	stun = with(ones, func(s *gamedata.Status) {
		s.ID, s.Group, s.Blocks = "stun", gamedata.Hard, gamedata.BlocksMove|gamedata.BlocksAct
	})
	freeze = with(stun, func(s *gamedata.Status) { s.ID, s.Shatter = "freeze", 1.5 })
	sleep  = with(stun, func(s *gamedata.Status) { s.ID, s.Wakes = "sleep", true })
	pin    = with(ones, func(s *gamedata.Status) { s.ID, s.Group, s.Blocks = "pin", gamedata.Hard, gamedata.BlocksMove })
	slow   = with(ones, func(s *gamedata.Status) { s.ID, s.Group, s.MoveSpeed = "slow", gamedata.Soft, 0.6 })
	blind  = with(ones, func(s *gamedata.Status) { s.ID, s.Group, s.Accuracy = "blind", gamedata.Soft, 0.5 })
	weaken = with(ones, func(s *gamedata.Status) { s.ID, s.Group, s.Attack = "weaken", gamedata.Weaken, 0.75 })
	broken = with(ones, func(s *gamedata.Status) {
		s.ID, s.Group, s.PhysicalDefense, s.MagicDefense = "armor_break", gamedata.Weaken, 0.7, 0.7
	})
	poison = with(ones, func(s *gamedata.Status) { s.ID, s.Group = "poison", gamedata.Dot })
	bleed  = with(poison, func(s *gamedata.Status) { s.ID, s.Moving = "bleed", 2 })
	blinds = with(ones, func(s *gamedata.Status) { s.ID, s.Group = gamedata.LoseSight, gamedata.Aggro })
)

func with(s gamedata.Status, f func(*gamedata.Status)) gamedata.Status {
	f(&s)
	return s
}

var (
	ann = model.EntityRef{Player: kernel.CharacterID{1}}
	bo  = model.EntityRef{Player: kernel.CharacterID{2}}
)

// lasting is st for ticks, by ann; a damage over time ticks every 20 for 10 attack.
func lasting(st gamedata.Status, ticks uint64) model.Infliction {
	in := model.Infliction{Status: st, Ticks: ticks, By: ann}
	if st.Group == gamedata.Dot {
		in.Every, in.Power = 20, model.Attacker{PhysicalAttack: 10}
	}
	return in
}

const immunity = 40

func TestInflictingGoesSuperArmorThenImmunityThenRestart(t *testing.T) {
	var s model.Statuses
	if s.Inflict(lasting(stun, 30), 100, 0.5, 0, 0) {
		t.Fatal("super armor took a stun")
	}
	if !s.Inflict(lasting(slow, 30), 100, 0.5, 0, 0) {
		t.Fatal("super armor refused a slow")
	}
	if st, _ := s.Find("slow", ann); st.Until != 115 {
		t.Errorf("a slow of 30 ticks on super armor ends at %d, want 115: half its time", st.Until)
	}
	if !s.Inflict(lasting(stun, 30), 100, 0, 0, 0) || !s.Blocks(gamedata.BlocksAct) {
		t.Fatal("a stun didn't take")
	}
	s.Tick(130, immunity, 0, 0) // the stun ends by time: immune to hard statuses up to 170
	if s.Blocks(gamedata.BlocksAct) {
		t.Fatal("the stun outlasted its time")
	}
	if s.Inflict(lasting(pin, 30), 169, 0, 0, 0) {
		t.Error("a pin took in the hard immunity")
	}
	if !s.Inflict(lasting(slow, 30), 169, 0, 0, 0) {
		t.Error("the hard immunity refused a slow")
	}
	if !s.Inflict(lasting(pin, 30), 170, 0, 0, 0) {
		t.Error("a pin refused once the immunity ran out")
	}
	// The same status restarts with the new time and inflicter, and never stacks.
	in := lasting(slow, 10)
	in.By = bo
	s.Inflict(in, 180, 0, 0, 0)
	if st, _ := s.Find("slow", ann); st.Until != 190 || st.By != bo || s.MoveSpeed() != 0.6 {
		t.Errorf("the slow restarted to %d by %v, factor %v; want 190, by bo, 0.6 once", st.Until, st.By, s.MoveSpeed())
	}
}

func TestDamageOverTimeTicksOnItsBeatPerInflicter(t *testing.T) {
	var s model.Statuses
	s.Inflict(lasting(poison, 80), 0, 0, 0, 0)
	theirs := lasting(poison, 80)
	theirs.By, theirs.Power = bo, model.Attacker{PhysicalAttack: 30}
	s.Inflict(theirs, 10, 0, 0, 0)
	got := map[model.EntityRef]int{}
	for tick := uint64(1); tick <= 100; tick++ {
		dots, _ := s.Tick(tick, immunity, 0, 0)
		for _, d := range dots {
			got[d.By]++
			if d.By == bo && d.Power.PhysicalAttack != 30 {
				t.Fatalf("bo's poison ticked with %v", d.Power)
			}
		}
		if tick == 50 {
			s.Inflict(lasting(poison, 80), 50, 0, 0, 0) // ann's restart: on to 130, on its beat
		}
	}
	if got[ann] != 5 || got[bo] != 4 {
		t.Errorf("ticks %v; want ann's 5 by tick 100 (20 to 100, restarted at 50), bo's 4 (30 to 90), each beside the other", got)
	}
}

func TestBleedTicksHarderWhileMoving(t *testing.T) {
	var s model.Statuses
	s.Inflict(lasting(bleed, 60), 0, 0, 5, 5)
	if dots, _ := s.Tick(20, immunity, 5, 5); len(dots) != 1 || dots[0].Power.PhysicalAttack != 10 {
		t.Errorf("standing still: %+v, want 10", dots)
	}
	if dots, _ := s.Tick(40, immunity, 6, 5); len(dots) != 1 || dots[0].Power.PhysicalAttack != 20 {
		t.Errorf("having moved: %+v, want 20", dots)
	}
	if dots, _ := s.Tick(60, immunity, 6, 5); len(dots) != 1 || dots[0].Power.PhysicalAttack != 10 {
		t.Errorf("still since: %+v, want 10", dots)
	}
}

func TestShatterAndWakingEndTheirStatusAndStartImmunity(t *testing.T) {
	var s model.Statuses
	if f := s.Shatter(0, immunity); f != 1 {
		t.Errorf("nothing frozen: factor %v, want 1", f)
	}
	s.Inflict(lasting(freeze, 40), 0, 0, 0, 0)
	if f := s.Shatter(10, immunity); f != 1.5 || s.Blocks(gamedata.BlocksAct) {
		t.Errorf("the shattering hit's factor %v, frozen still %v; want 1.5 and the freeze over", f, s.Blocks(gamedata.BlocksAct))
	}
	if s.Inflict(lasting(pin, 5), 49, 0, 0, 0) || !s.Inflict(lasting(pin, 5), 50, 0, 0, 0) {
		t.Error("a shatter didn't start the hard immunity, for 40 ticks")
	}
	var z model.Statuses
	z.Inflict(lasting(sleep, 100), 0, 0, 0, 0)
	z.Inflict(lasting(slow, 100), 0, 0, 0, 0)
	if !z.Wake(10, immunity) || z.Blocks(gamedata.BlocksAct) || z.MoveSpeed() != 0.6 || z.Inflict(lasting(pin, 5), 49, 0, 0, 0) {
		t.Error("damage didn't end the sleep alone and start the immunity")
	}
	if z.Wake(11, immunity) {
		t.Error("woke what wasn't asleep")
	}
}

func TestStatusFactorsMeetInResolve(t *testing.T) {
	var s model.Statuses
	for _, st := range []gamedata.Status{blind, weaken, broken, slow} {
		s.Inflict(lasting(st, 100), 0, 0, 0, 0)
	}
	a := s.Attacking(model.Attacker{PhysicalAttack: 100, MagicAttack: 40, Accuracy: 20, CritRate: 0.1})
	if a != (model.Attacker{PhysicalAttack: 75, MagicAttack: 30, Accuracy: 10, CritRate: 0.1}) {
		t.Errorf("attacking %+v", a)
	}
	d := s.Defending(model.Defender{PhysicalDefense: 10, MagicDefense: 20, Evasion: 5})
	if d != (model.Defender{PhysicalDefense: 7, MagicDefense: 14, Evasion: 5}) {
		t.Errorf("defending %+v", d)
	}
	if s.MoveSpeed() != 0.6 {
		t.Errorf("move speed %v", s.MoveSpeed())
	}
}

func TestLosingSightIsPerInflicter(t *testing.T) {
	var s model.Statuses
	s.Inflict(lasting(blinds, 40), 0, 0, 0, 0)
	theirs := lasting(blinds, 20)
	theirs.By = bo
	s.Inflict(theirs, 0, 0, 0, 0)
	s.Tick(20, immunity, 0, 0)
	if _, ok := s.Find(gamedata.LoseSight, ann); !ok {
		t.Error("bo's smoke ended ann's")
	}
	if _, ok := s.Find(gamedata.LoseSight, bo); ok {
		t.Error("bo's smoke outlasted its time")
	}
}

func TestLeavingAndDeathEndStatuses(t *testing.T) {
	var s model.Statuses
	s.Inflict(lasting(poison, 100), 0, 0, 0, 0)
	theirs := lasting(poison, 100)
	theirs.By = bo
	s.Inflict(theirs, 0, 0, 0, 0)
	s.Inflict(lasting(stun, 10), 0, 0, 0, 0)
	s.DropBy(ann)
	if _, ok := s.Find("poison", ann); ok {
		t.Error("ann's poison outlasted her leaving")
	}
	if _, ok := s.Find("poison", bo); !ok || !s.Blocks(gamedata.BlocksAct) {
		t.Error("ann's leaving ended another's poison or her stun")
	}
	s.Tick(10, immunity, 0, 0)
	s.Clear()
	if s.Blocks(gamedata.BlocksAct) || s.MoveSpeed() != 1 {
		t.Error("a death left statuses")
	}
	if _, ok := s.Find("poison", bo); ok {
		t.Error("a death left a poison")
	}
	if !s.Inflict(lasting(pin, 5), 11, 0, 0, 0) {
		t.Error("a death left the hard immunity the stun's end started")
	}
}

func TestSteadyIsResolveWithoutTheRoll(t *testing.T) {
	a := model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 50, Accuracy: 1, CritRate: 1, CritDamage: 1, Penetration: 0.5}
	d := model.Defender{PhysicalDefense: 40, Evasion: 1e9}
	if got := model.Steady(a, d); got != 42 { // 50 × 100 / (100 + 20), never a MISS or a crit
		t.Errorf("steady %d, want 42", got)
	}
	if got := model.Steady(model.Attacker{Damage: gamedata.Magic, MagicAttack: 0.1}, d); got != 1 {
		t.Errorf("steady %d on nothing, want at least 1", got)
	}
}
