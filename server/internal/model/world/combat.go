package world

// Combat is pure: randomness comes from the caller's Roll, so a fight replays from a seed.

import (
	"math"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/progression"
)

// Roll is a source of uniform numbers in [0, 1).
type Roll interface {
	Float64() float64
}

// Attacker are the attacking side's derived stats and what its attack deals. Rates are fractions.
type Attacker struct {
	Damage         gamedata.DamageType
	PhysicalAttack float64
	MagicAttack    float64
	Accuracy       float64
	CritRate       float64
	CritDamage     float64
	Penetration    float64
}

// Defender are the defending side's derived stats.
type Defender struct {
	PhysicalDefense float64
	MagicDefense    float64
	Evasion         float64
}

// PlayerAttacker is a player's basic attack, of their class's damage type.
func PlayerAttacker(d progression.Derived, damage gamedata.DamageType) Attacker {
	return Attacker{
		Damage: damage, PhysicalAttack: d.PhysicalAttack, MagicAttack: d.MagicAttack,
		Accuracy: d.Accuracy, CritRate: d.CritRate, CritDamage: d.CritDamage, Penetration: d.Penetration,
	}
}

// Times is the attack with both attack stats multiplied by power, as a skill's power multiplies the matching attack.
func (a Attacker) Times(power float64) Attacker {
	a.PhysicalAttack *= power
	a.MagicAttack *= power
	return a
}

// PlayerDefender is a player's side of a mob's touch.
func PlayerDefender(d progression.Derived) Defender {
	return Defender{PhysicalDefense: d.PhysicalDefense, MagicDefense: d.MagicDefense, Evasion: d.Evasion}
}

// MobAttacker is one of a mob's attacks: power times its attack number, of its damage type.
func MobAttacker(m gamedata.Mob, power float64) Attacker {
	a := Attacker{Damage: m.Damage, Accuracy: float64(m.Accuracy)}
	if m.Damage == gamedata.Magic {
		a.MagicAttack = float64(m.Attack) * power
	} else {
		a.PhysicalAttack = float64(m.Attack) * power
	}
	return a
}

// MobDefender is a mob's side of a player's attack.
func MobDefender(m gamedata.Mob) Defender {
	return Defender{PhysicalDefense: float64(m.PhysicalDefense), MagicDefense: float64(m.MagicDefense), Evasion: float64(m.Evasion)}
}

// Hit is one resolved attack.
type Hit struct {
	Miss   bool
	Crit   bool
	Damage int // 0 on a miss
}

const (
	minVariation = 0.90
	maxVariation = 1.10
	minDamage    = 1
)

// Resolve meets the attack of the attack's damage type with the defense of that type; it draws from r in a fixed order (hit, variation, crit), so replays match.
func Resolve(a Attacker, d Defender, curve gamedata.HitCurve, r Roll) Hit {
	if r.Float64() >= curve.Chance(a.Accuracy, d.Evasion) {
		return Hit{Miss: true}
	}
	attack, defense := a.PhysicalAttack, d.PhysicalDefense
	if a.Damage == gamedata.Magic {
		attack, defense = a.MagicAttack, d.MagicDefense
	}
	raw := attack * (minVariation + (maxVariation-minVariation)*r.Float64())
	defense *= 1 - a.Penetration
	damage := math.Max(minDamage, math.Round(raw*100/(100+defense)))
	hit := Hit{Damage: int(damage)}
	if r.Float64() < a.CritRate {
		hit.Crit = true
		hit.Damage = int(math.Round(damage * (1 + a.CritDamage)))
	}
	return hit
}
