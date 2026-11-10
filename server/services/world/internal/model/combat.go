package model

// Combat is pure: randomness comes from the caller's Roll, so a fight replays from a seed.

import (
	"math"

	"finga/internal/gamedata"
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

// HitChance is what the chance to hit is held between, from stats.yaml.
type HitChance struct {
	Min, Max float64
}

// Hit is one resolved attack.
type Hit struct {
	Miss   bool
	Crit   bool
	Damage int // 0 on a miss
}

const (
	baseHitChance  = 0.80
	hitPerAccuracy = 0.02
	minVariation   = 0.90
	maxVariation   = 1.10
	minDamage      = 1
)

// Resolve meets the attack of the attack's damage type with the defense of that type; it draws from r in a fixed order (hit, variation, crit), so replays match.
func Resolve(a Attacker, d Defender, limits HitChance, r Roll) Hit {
	chance := math.Min(limits.Max, math.Max(limits.Min, baseHitChance+hitPerAccuracy*(a.Accuracy-d.Evasion)))
	if r.Float64() >= chance {
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
