package progression

import (
	"math"

	"finga/internal/gamedata"
	"finga/internal/kernel"
)

// deathPenaltyRate is the share of the current level's requirement lost on death.
const deathPenaltyRate = 0.05

// AddStats adds b to a, stat by stat.
func AddStats(a, b gamedata.Stats) gamedata.Stats {
	return gamedata.Stats{STR: a.STR + b.STR, DEX: a.DEX + b.DEX, INT: a.INT + b.INT, AGI: a.AGI + b.AGI, VIT: a.VIT + b.VIT}
}

// An item's whole numbers in the units of the derived stats.
const (
	tenthsOfAPercent = 1000 // rates, crit damage and penetration
	tenthsOfAPoint   = 10   // regen
)

// DeriveStats computes the fifteen derived stats of a character of class at level, with own primary stats (the class's starting stats plus the points spent) wearing worn. Worn primary stats count as own ones, toward the curves too; each flat stat is the class's base at level plus each point's amount plus the worn bonus; a rate is its curve plus the worn rate, held to its limit; penetration comes from gear alone. HP and MP round down.
func DeriveStats(r gamedata.StatRules, class kernel.Class, level int, own gamedata.Stats, worn ItemStats) Derived {
	p := AddStats(own, worn.Primary())
	base := r.Classes[class]
	flat := func(g gamedata.Grows, of func(gamedata.Flats[float64]) float64) float64 {
		pp := r.PerPoint
		return g.At(level) +
			of(pp.STR)*float64(p.STR) + of(pp.DEX)*float64(p.DEX) + of(pp.INT)*float64(p.INT) +
			of(pp.AGI)*float64(p.AGI) + of(pp.VIT)*float64(p.VIT)
	}
	rate := func(c gamedata.Curve, gear int) float64 {
		points := float64(max(p.Of(c.Stat), 0))
		return min(c.Base+(c.Limit-c.Base)*points/(points+c.K)+float64(gear)/tenthsOfAPercent, c.Limit)
	}
	return Derived{
		MaxHP:           int(math.Floor(flat(base.MaxHP, func(f gamedata.Flats[float64]) float64 { return f.MaxHP }))) + worn.MaxHP,
		MaxMP:           int(math.Floor(flat(base.MaxMP, func(f gamedata.Flats[float64]) float64 { return f.MaxMP }))) + worn.MaxMP,
		PhysicalAttack:  flat(base.PhysicalAttack, func(f gamedata.Flats[float64]) float64 { return f.PhysicalAttack }) + float64(worn.PhysicalAttack),
		MagicAttack:     flat(base.MagicAttack, func(f gamedata.Flats[float64]) float64 { return f.MagicAttack }) + float64(worn.MagicAttack),
		PhysicalDefense: flat(base.PhysicalDefense, func(f gamedata.Flats[float64]) float64 { return f.PhysicalDefense }) + float64(worn.PhysicalDefense),
		MagicDefense:    flat(base.MagicDefense, func(f gamedata.Flats[float64]) float64 { return f.MagicDefense }) + float64(worn.MagicDefense),
		Accuracy:        flat(base.Accuracy, func(f gamedata.Flats[float64]) float64 { return f.Accuracy }) + float64(worn.Accuracy),
		Evasion:         flat(base.Evasion, func(f gamedata.Flats[float64]) float64 { return f.Evasion }) + float64(worn.Evasion),
		CritRate:        rate(r.Curves.CritRate, worn.CritRate),
		CritDamage:      flat(base.CritDamage, func(f gamedata.Flats[float64]) float64 { return f.CritDamage }) + float64(worn.CritDamage)/tenthsOfAPercent,
		Penetration:     min(float64(worn.Penetration)/tenthsOfAPercent, r.Penetration),
		HPRegen:         flat(base.HPRegen, func(f gamedata.Flats[float64]) float64 { return f.HPRegen }) + float64(worn.HPRegen)/tenthsOfAPoint,
		MPRegen:         flat(base.MPRegen, func(f gamedata.Flats[float64]) float64 { return f.MPRegen }) + float64(worn.MPRegen)/tenthsOfAPoint,
		CooldownSpeed:   rate(r.Curves.CooldownSpeed, worn.CooldownSpeed),
		MoveSpeed:       rate(r.Curves.MoveSpeed, worn.MoveSpeed),
	}
}

// Award is the result of AwardExp.
type Award struct {
	Progress     Progress
	LevelsGained int
}

// AwardExp adds EXP and resolves level-ups; the caller gives each new level's points and restores HP and MP after one.
func AwardExp(curve ExpCurve, p Progress, amount int64) Award {
	a := Award{Progress: p}
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
