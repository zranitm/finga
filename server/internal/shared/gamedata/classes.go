package gamedata

import (
	"slices"

	"finga/internal/shared/kernel"
)

// Attack is a class's basic attack: the arc or shot it finds its one target in, what it deals, and its timing; a skill may always cut it off.
type Attack struct {
	Shape  Shape
	Damage DamageType
	Timing
}

// Grant is a starter item: a template and a quantity.
type Grant struct {
	Item     string
	Quantity int
}

// Promotion is what a class asks of a character of its parent, met at its trainer, and what the trainer hands over.
type Promotion struct {
	Trainer string // the NPC who promotes
	Level   int
	Stats   Stats          // own primary stats, without gear
	Skills  map[string]int // points invested in each
	Quests  []string       // each finished at least once
	Weapon  string         // the class's first weapon, the trainer's gift
}

// Class is a class in the tree every character climbs from its root, the class they start as.
type Class struct {
	ID          kernel.Class
	Name        string
	Description string         // how it fights, as its trainer tells it
	Parent      kernel.Class   // "" for the root
	Line        []kernel.Class // the class, then each class above it up to the root
	Attack      Attack
	Promotion   Promotion // none for the root
	// The root alone has these: every character starts with them, and a promotion keeps them.
	BaseStats    Stats // primary stats at level 1
	StarterItems []Grant
	AutoAssign   Stats    // how Auto-assign spends a level's points; none on the root, whose players spend their own
	Skills       []string // the skills it teaches, in skills.yaml
	Chains       []Chain  // the only chains its players' keys follow; a promotion's replace its parent's
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
