package progression

import "finga/internal/gamedata"

// Unmet names the first requirement of an item a character falls short of.
type Unmet int

// The requirements, in the order Meets checks them; the stats go STR, DEX, INT, AGI, VIT.
const (
	NothingUnmet Unmet = iota
	LevelUnmet
	ClassUnmet
	STRUnmet
	DEXUnmet
	INTUnmet
	AGIUnmet
	VITUnmet
)

// WornItem is one worn item: its template and its roll.
type WornItem struct {
	Item  gamedata.Item
	Stats ItemStats
}

// Active sums the rolls of the worn items whose requirements a character meets with its own stats; an item below its requirement stays worn and adds nothing.
func Active(worn []WornItem, level int, class gamedata.Class, own gamedata.Stats) ItemStats {
	var sum ItemStats
	for _, w := range worn {
		if Meets(w.Item, level, class, own) == NothingUnmet {
			sum = sum.Plus(w.Stats)
		}
	}
	return sum
}

// Meets reports the first of an item's requirements a character falls short of: the level, the class for a weapon (its own or one above it), and each stat minimum against the character's own stats, which leave gear out so one item's bonus never unlocks another.
func Meets(it gamedata.Item, level int, class gamedata.Class, own gamedata.Stats) Unmet {
	need := it.Requires
	switch {
	case level < it.MinLevel:
		return LevelUnmet
	case !class.Fits(it.Class):
		return ClassUnmet
	case own.STR < need.STR:
		return STRUnmet
	case own.DEX < need.DEX:
		return DEXUnmet
	case own.INT < need.INT:
		return INTUnmet
	case own.AGI < need.AGI:
		return AGIUnmet
	case own.VIT < need.VIT:
		return VITUnmet
	}
	return NothingUnmet
}
