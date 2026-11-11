package gamedata

import (
	"slices"
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

// Class is a class in the tree every character climbs from its root, the class they start as.
type Class struct {
	ID     kernel.Class
	Name   string
	Parent kernel.Class   // "" for the root
	Line   []kernel.Class // the class, then each class above it up to the root
	Attack Attack
	// The root alone has these: every character starts with them, and a promotion keeps them.
	BaseStats    Stats // primary stats at level 1
	StarterItems []Grant
	AutoAssign   Stats    // how Auto-assign spends a level's points; none on the root, whose players spend their own
	Skills       []string // the skills it teaches, in skills.yaml
}

// Fits reports whether a weapon for class item fits a character of c: one of c's class or a class above it, or one for no class.
func (c Class) Fits(item kernel.Class) bool {
	return item == "" || slices.Contains(c.Line, item)
}

// Classes lists the classes in file order, the root first.
func (d *Data) Classes() []Class { return d.p.Classes }

// Class finds a class.
func (d *Data) Class(id kernel.Class) (Class, bool) { return find(d.p.Classes, d.classIdx, id) }

// StartClass is the root of the tree, the class every new character starts as.
func (d *Data) StartClass() Class { return d.p.Classes[0] }
