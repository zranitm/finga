package progression

import (
	"math"

	"finga/internal/gamedata"
)

// deathPenaltyRate is the share of the current level's requirement lost on death.
const deathPenaltyRate = 0.05

// AddStats adds b to a, stat by stat.
func AddStats(a, b gamedata.Stats) gamedata.Stats {
	return gamedata.Stats{STR: a.STR + b.STR, DEX: a.DEX + b.DEX, INT: a.INT + b.INT, AGI: a.AGI + b.AGI, VIT: a.VIT + b.VIT}
}

// DeriveStats computes the derived stats; rates are capped and HP and MP round down.
func DeriveStats(f gamedata.Formulas, s gamedata.Stats, level int, eq Equipment) Derived {
	return Derived{
		MaxHP:       int(math.Floor(eval(f.MaxHP, s, level))),
		MaxMP:       int(math.Floor(eval(f.MaxMP, s, level))),
		AttackPower: eval(f.AttackPower, s, level) + eq.WeaponAttack,
		Accuracy:    eval(f.Accuracy, s, level),
		Evasion:     eval(f.Evasion, s, level),
		Defense:     eval(f.Defense, s, level) + eq.ArmorDefense,
		CritRate:    capped(f.CritRate, eval(f.CritRate, s, level)),
		CritDamage:  capped(f.CritDamage, eval(f.CritDamage, s, level)),
		Penetration: capped(f.Penetration, eval(f.Penetration, s, level)+eq.Penetration),
	}
}

func eval(f gamedata.Formula, s gamedata.Stats, level int) float64 {
	return f.Base +
		f.Coef.STR*float64(s.STR) + f.Coef.DEX*float64(s.DEX) + f.Coef.INT*float64(s.INT) +
		f.Coef.AGI*float64(s.AGI) + f.Coef.VIT*float64(s.VIT) +
		f.PerLevel*float64(level-1)
}

func capped(f gamedata.Formula, v float64) float64 {
	if f.Cap > 0 && v > f.Cap {
		return f.Cap
	}
	return v
}

// AllocationPlan says where a new level's points go.
type AllocationPlan func(newLevel int) gamedata.Stats

// PresetPlan allocates the class's level-up preset.
func PresetPlan(class gamedata.Class) AllocationPlan {
	return func(int) gamedata.Stats { return class.LevelUpPoints }
}

// Award is the result of AwardExp.
type Award struct {
	Progress     Progress
	Stats        gamedata.Stats
	LevelsGained int
}

// AwardExp adds EXP and resolves level-ups; the caller restores HP and MP after one.
func AwardExp(curve ExpCurve, p Progress, amount int64, stats gamedata.Stats, plan AllocationPlan) Award {
	a := Award{Progress: p, Stats: stats}
	if p.Level >= curve.LevelCap() {
		a.Progress.Exp = 0
		return a
	}
	a.Progress.Exp += amount
	for {
		need, ok := curve.ExpToNext(a.Progress.Level)
		if !ok { // at the cap
			a.Progress.Exp = 0
			return a
		}
		if a.Progress.Exp < need {
			return a
		}
		a.Progress.Exp -= need
		a.Progress.Level++
		a.LevelsGained++
		a.Stats = AddStats(a.Stats, plan(a.Progress.Level))
	}
}

// DeathPenalty takes 5% of the level's requirement outside towns, never below the level's start.
func DeathPenalty(curve ExpCurve, p Progress, inTown bool) Progress {
	if inTown {
		return p
	}
	need, ok := curve.ExpToNext(p.Level)
	if !ok {
		return p
	}
	loss := int64(math.Floor(float64(need) * deathPenaltyRate))
	p.Exp = max(0, p.Exp-loss)
	return p
}
