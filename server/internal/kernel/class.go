package kernel

import "errors"

// Class is a character class's ID, as stored in characters.class; game data decides which IDs are classes.
type Class string

// ErrUnknownClass is a class ID that game data doesn't have.
var ErrUnknownClass = errors.New("kernel: unknown class")

// String is the stored form.
func (c Class) String() string { return string(c) }
