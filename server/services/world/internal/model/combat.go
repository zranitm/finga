package model

// Combat is pure: randomness comes from the caller's Roll, so a fight replays from a seed.

import (
	"math"

	"finga/internal/gamedata"
	"finga/internal/progression"
	"finga/internal/sim"
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

// PlayerDefender is a player's side of a mob's touch.
func PlayerDefender(d progression.Derived) Defender {
	return Defender{PhysicalDefense: d.PhysicalDefense, MagicDefense: d.MagicDefense, Evasion: d.Evasion}
}

// MobAttacker is a mob's contact attack, its one attack number of its damage type.
func MobAttacker(m gamedata.Mob) Attacker {
	a := Attacker{Damage: m.Damage, Accuracy: float64(m.Accuracy)}
	if m.Damage == gamedata.Magic {
		a.MagicAttack = float64(m.Attack)
	} else {
		a.PhysicalAttack = float64(m.Attack)
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

// Rect is an axis-aligned box with its top-left at (X, Y): sim's, so both test overlaps alike.
type Rect = sim.Rect

// Target is a candidate for an attack: its index is returned when hit.
type Target struct {
	Box Rect
}

// MeleeBox is the Warrior's hit box in front of an attacker whose feet centre is (x, y).
func MeleeBox(x, y float64, facing int, width, height float64) Rect {
	if facing < 0 {
		return Rect{X: x - width, Y: y - height, W: width, H: height}
	}
	return Rect{X: x, Y: y - height, W: width, H: height}
}

// NearestInBox is the target overlapping box whose centre is closest to (x, y).
func NearestInBox(x, y float64, box Rect, targets []Target) (index int, ok bool) {
	best := math.Inf(1)
	for i, t := range targets {
		if !box.Overlaps(t.Box) {
			continue
		}
		cx, cy := t.Box.X+t.Box.W/2, t.Box.Y+t.Box.H/2
		if d := (cx-x)*(cx-x) + (cy-y)*(cy-y); d < best {
			best, index, ok = d, i, true
		}
	}
	return index, ok
}

// FirstInLine is the first target a horizontal shot from (x, y) crosses within reach.
func FirstInLine(x, y float64, facing int, reach float64, targets []Target) (index int, ok bool) {
	best := math.Inf(1)
	for i, t := range targets {
		if y < t.Box.Y || y >= t.Box.Y+t.Box.H {
			continue
		}
		var dist float64
		switch {
		case facing >= 0 && t.Box.X+t.Box.W > x:
			dist = math.Max(0, t.Box.X-x)
		case facing < 0 && t.Box.X < x:
			dist = math.Max(0, x-(t.Box.X+t.Box.W))
		default:
			continue
		}
		if dist <= reach && dist < best {
			best, index, ok = dist, i, true
		}
	}
	return index, ok
}
