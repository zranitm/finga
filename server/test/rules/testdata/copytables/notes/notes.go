// Package notes is a sum type of notes, each marked by note(), and a type without the mark.
package notes

// Hello is a note.
type Hello struct{ To string }

func (Hello) note() {}

// Bye is a note.
type Bye struct{ To string }

func (*Bye) note() {}

// Draft isn't one: it has no note().
type Draft struct{}
