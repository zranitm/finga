// Package spend is how the dev tools' characters spend their points, as a player planning a class does: stat points on the promotion's needs, then the class's split; skill points on the promotion's skills, a point in each skill their level opens, then the main one.
package spend

import (
	"maps"
	"slices"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
)

// Stats spends unspent points on what the promotion asks of own stats first, then by the class's split in whole splits; the rest waits for the next level.
func Stats(unspent int, own, need, split gamedata.Stats) gamedata.Stats {
	var out gamedata.Stats
	fill := func(spent *int, have, want int) {
		k := min(max(want-have, 0), unspent)
		*spent += k
		unspent -= k
	}
	fill(&out.STR, own.STR, need.STR)
	fill(&out.DEX, own.DEX, need.DEX)
	fill(&out.INT, own.INT, need.INT)
	fill(&out.AGI, own.AGI, need.AGI)
	fill(&out.VIT, own.VIT, need.VIT)
	if per := split.Total(); per > 0 {
		k := unspent / per
		out.STR += k * split.STR
		out.DEX += k * split.DEX
		out.INT += k * split.INT
		out.AGI += k * split.AGI
		out.VIT += k * split.VIT
	}
	return out
}

// Skills puts unspent skill points into what the promotion asks of each skill first, then a point into each of opened that has none, then all the rest into rest.
func Skills(unspent int32, invested map[string]int32, need map[string]int, opened []string, rest string) map[string]int32 {
	out := map[string]int32{}
	for _, skill := range slices.Sorted(maps.Keys(need)) {
		k := min(max(int32(need[skill])-invested[skill], 0), unspent)
		if k > 0 {
			out[skill] = k
			unspent -= k
		}
	}
	for _, skill := range opened {
		if unspent > 0 && invested[skill] == 0 && out[skill] == 0 {
			out[skill] = 1
			unspent--
		}
	}
	if unspent > 0 && rest != "" {
		out[rest] += unspent
	}
	return out
}

// Opened are the skills class and each class above it teach that a character of level may put points in, the root's first: a planning player puts a point in each, so every skill is on hand; a granted one is on hand already and takes none.
func Opened(d *gamedata.Data, class kernel.Class, level int) []string {
	c, ok := d.Class(class)
	if !ok {
		return nil
	}
	var out []string
	for _, id := range slices.Backward(c.Line) {
		teacher, _ := d.Class(id)
		for _, skill := range teacher.Skills {
			if s, ok := d.Skill(skill); ok && s.Level <= level && !s.Granted {
				out = append(out, skill)
			}
		}
	}
	return out
}

// MainSkill is where the rest of a character's skill points go: the promotion's first skill until it is promoted, then its class's first.
func MainSkill(future gamedata.Class, promoted bool) string {
	if promoted {
		if len(future.Skills) > 0 {
			return future.Skills[0]
		}
		return ""
	}
	if keys := slices.Sorted(maps.Keys(future.Promotion.Skills)); len(keys) > 0 {
		return keys[0]
	}
	return ""
}
