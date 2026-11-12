package model

// Combat is pure: randomness comes from the caller's Roll, so a fight replays from a seed.

import (
	"cmp"
	"math"
	"slices"

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

// Reached are the targets a shape reaches from an attacker whose feet centre is (x, y), nearest first, at most most of them.
//   - melee: those overlapping the box in front, by distance from the feet to their centre;
//   - ranged: those a horizontal line at half the attacker's height crosses along the facing within range, by distance along it;
//   - area: those within radius of the attacker's middle, by distance to their nearest point.
func Reached(x, y float64, facing int, height float64, s gamedata.Shape, targets []Target, most int) []int {
	type found struct {
		index int
		dist  float64
	}
	var hits []found
	for i, t := range targets {
		b := t.Box
		switch s.Kind {
		case gamedata.MeleeShape:
			if MeleeBox(x, y, facing, s.Width, s.Height).Overlaps(b) {
				cx, cy := b.X+b.W/2, b.Y+b.H/2
				hits = append(hits, found{i, math.Hypot(cx-x, cy-y)})
			}
		case gamedata.RangedShape:
			line := y - height/2
			if line < b.Y || line >= b.Y+b.H {
				continue
			}
			var dist float64
			switch {
			case facing >= 0 && b.X+b.W > x:
				dist = math.Max(0, b.X-x)
			case facing < 0 && b.X < x:
				dist = math.Max(0, x-(b.X+b.W))
			default:
				continue
			}
			if dist <= s.Range {
				hits = append(hits, found{i, dist})
			}
		case gamedata.AreaShape:
			cx, cy := x, y-height/2
			dx := math.Max(0, math.Max(b.X-cx, cx-(b.X+b.W)))
			dy := math.Max(0, math.Max(b.Y-cy, cy-(b.Y+b.H)))
			if d := math.Hypot(dx, dy); d <= s.Radius {
				hits = append(hits, found{i, d})
			}
		}
	}
	slices.SortStableFunc(hits, func(a, b found) int { return cmp.Compare(a.dist, b.dist) })
	out := make([]int, 0, min(most, len(hits)))
	for _, h := range hits[:min(most, len(hits))] {
		out = append(out, h.index)
	}
	return out
}
