package progression

import (
	"finga/internal/gamedata"
	"finga/internal/kernel"
)

// Unmet names the first requirement of an item a character falls short of.
type Unmet int

// The requirements, in the order Meets checks them.
const (
	NothingUnmet Unmet = iota
	LevelUnmet
	ClassUnmet
	StatsUnmet
)

// Meets reports the first of an item's requirements a character falls short of: the level, the class for a weapon, and each stat minimum against the character's own stats, which leave gear out so one item's bonus never unlocks another.
func Meets(it gamedata.Item, level int, class kernel.Class, own gamedata.Stats) Unmet {
	need := it.Requires
	switch {
	case level < it.MinLevel:
		return LevelUnmet
	case it.Class != "" && it.Class != class:
		return ClassUnmet
	case own.STR < need.STR || own.DEX < need.DEX || own.INT < need.INT || own.AGI < need.AGI || own.VIT < need.VIT:
		return StatsUnmet
	}
	return NothingUnmet
}
