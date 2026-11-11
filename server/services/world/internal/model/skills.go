package model

import "maps"

// Skills are a character's skill points: those invested in each skill, and those left to spend. Only its map's tick touches them; anything read off the tick, such as a checkpoint or a sheet, takes a Clone.
type Skills struct {
	Points  map[string]int // by skill; one with none isn't listed
	Unspent int
}

// Clone copies the points, so the copy shares nothing.
func (s Skills) Clone() Skills { return Skills{Points: maps.Clone(s.Points), Unspent: s.Unspent} }
