package model

import "finga/pkg/errs"

// Class is a character class. The game has four (OD-03); which ones a
// player can pick in a given stage is decided by game data, not here
// (Stage 1: Warrior and Bowman, S1-Q1).
type Class string

// The four classes, as stored in characters.class.
const (
	Warrior  Class = "warrior"
	Bowman   Class = "bowman"
	Thief    Class = "thief"
	Magician Class = "magician"
)

var errUnknownClass = errs.Invalid("unknown class")

// Classes lists every class, in display order.
func Classes() []Class {
	return []Class{Warrior, Bowman, Thief, Magician}
}

// ParseClass parses the stored form; an unknown value is invalid_argument.
func ParseClass(s string) (Class, error) {
	for _, c := range Classes() {
		if string(c) == s {
			return c, nil
		}
	}
	return "", errUnknownClass
}

// String is the stored form.
func (c Class) String() string { return string(c) }
