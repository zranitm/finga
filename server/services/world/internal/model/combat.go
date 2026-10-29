package model

// Combat: hit and damage resolution (Low-level design, page 5) as pure
// functions over values: no state, no clock, randomness from a Roll the
// caller passes in, so a fight replays exactly from a seed.

import "math"

// Roll is a source of uniform numbers in [0, 1). A map instance passes its
// seeded generator; tests pass a fixed sequence.
type Roll interface {
	Float64() float64
}

// Attacker are the attacking side's derived stats. Rates are fractions.
type Attacker struct {
	AttackPower float64
	Accuracy    float64
	CritRate    float64
	CritDamage  float64
	Penetration float64
}

// Defender are the defending side's derived stats.
type Defender struct {
	Defense float64
	Evasion float64
}

// Hit is one resolved attack.
type Hit struct {
	Miss   bool
	Crit   bool
	Damage int // 0 on a miss
}

// Constants of the formula (page 5).
const (
	baseHitChance  = 0.80
	hitPerAccuracy = 0.02
	minHitChance   = 0.20
	maxHitChance   = 0.99
	minVariation   = 0.90
	maxVariation   = 1.10
	minDamage      = 1
)

// Resolve applies page 5's formula, drawing from r in a fixed order: the
// hit roll, the damage variation, then the crit roll. A miss stops after
// the first draw, so a replay with the same rolls gives the same fight.
//
//	hit_chance = clamp(0.80 + 0.02 × (accuracy − evasion), 0.20, 0.99)
//	raw        = attack_power × uniform(0.90, 1.10)
//	defense    = target.defense × (1 − penetration)
//	damage     = max(1, round(raw × 100 / (100 + defense)))
//	on crit (roll below crit_rate): damage = round(damage × (1 + crit_damage))
func Resolve(a Attacker, d Defender, r Roll) Hit {
	chance := math.Min(maxHitChance, math.Max(minHitChance, baseHitChance+hitPerAccuracy*(a.Accuracy-d.Evasion)))
	if r.Float64() >= chance {
		return Hit{Miss: true}
	}
	raw := a.AttackPower * (minVariation + (maxVariation-minVariation)*r.Float64())
	defense := d.Defense * (1 - a.Penetration)
	damage := math.Max(minDamage, math.Round(raw*100/(100+defense)))
	hit := Hit{Damage: int(damage)}
	if r.Float64() < a.CritRate {
		hit.Crit = true
		hit.Damage = int(math.Round(damage * (1 + a.CritDamage)))
	}
	return hit
}

// Rect is an axis-aligned box with its top-left at (X, Y), as in sim.
type Rect struct {
	X, Y, W, H float64
}

// Target is a candidate for an attack: its index is returned when hit.
type Target struct {
	Box Rect
}

// MeleeBox is the Warrior's hit area: a box of the given size in front of
// the attacker, whose feet centre is (x, y) and who faces facing (1 or -1).
// The box stands on the attacker's feet line.
func MeleeBox(x, y float64, facing int, width, height float64) Rect {
	if facing < 0 {
		return Rect{X: x - width, Y: y - height, W: width, H: height}
	}
	return Rect{X: x, Y: y - height, W: width, H: height}
}

// NearestInBox is the target overlapping box whose centre is closest to
// (x, y); ok is false when none overlaps.
func NearestInBox(x, y float64, box Rect, targets []Target) (index int, ok bool) {
	best := math.Inf(1)
	for i, t := range targets {
		if !overlaps(box, t.Box) {
			continue
		}
		cx, cy := t.Box.X+t.Box.W/2, t.Box.Y+t.Box.H/2
		if d := (cx-x)*(cx-x) + (cy-y)*(cy-y); d < best {
			best, index, ok = d, i, true
		}
	}
	return index, ok
}

// FirstInLine is the Bowman's target: the first box crossed by a
// horizontal shot from (x, y) in the facing direction, within reach. The
// shot starts at the attacker's edge, so a box behind the attacker is not
// hit.
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

func overlaps(a, b Rect) bool {
	return a.X < b.X+b.W && a.X+a.W > b.X && a.Y < b.Y+b.H && a.Y+a.H > b.Y
}
