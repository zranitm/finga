package gamedata

import (
	"time"

	"finga/internal/kernel"
)

// AttackKind is how a class's basic attack finds its target.
type AttackKind string

// The attack kinds.
const (
	Melee  AttackKind = "melee"  // a box in front of the character
	Ranged AttackKind = "ranged" // a straight shot along the facing direction
)

// Attack is a class's basic attack: its shape and what it deals.
type Attack struct {
	Kind     AttackKind
	Damage   DamageType
	Width    float64 // melee box in front of the character
	Height   float64
	Range    float64 // ranged shot length
	Cooldown time.Duration
}

// Grant is a starter item: a template and a quantity.
type Grant struct {
	Item     string
	Quantity int
}

// Class is a playable class.
type Class struct {
	ID           kernel.Class
	Name         string
	Attack       Attack
	BaseStats    Stats // primary stats at level 1
	AutoAssign   Stats // how Auto-assign spends a level's points
	StarterItems []Grant
}

// Classes lists the playable classes in file order.
func (d *Data) Classes() []Class { return d.p.Classes }

// Class finds a playable class.
func (d *Data) Class(id kernel.Class) (Class, bool) { return find(d.p.Classes, d.classIdx, id) }
