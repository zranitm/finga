package nav_test

import (
	"slices"
	"testing"

	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/mapfile"
	"finga/internal/shared/nav"
)

// arrivalMargin is how far past a mob's sight every spawn and boss circle keeps from where players come in or talk, so a mob at its circle's edge doesn't see them.
const arrivalMargin = 2.0

// undesigned are the hunting maps still flat placeholders, kept from the calm rule until their own designs place their spawns.
var undesigned = []struct{ mapID, why string }{
	{"sunken_ruins", "a flat placeholder until its own design"},
	{"icefall", "a flat placeholder until its own design"},
	{"glacier_peak", "a flat placeholder until its own design"},
	{"fallen_keep", "a flat placeholder until its own design"},
}

// On every designed hunting map, no mob sees a player arrive or stand at an NPC: each spawn and the boss keep their circles a mob's sight plus arrivalMargin from the player spawn, every portal and every NPC, so a fight is one a player walks into.
func TestNoSpawnSeesAnArrivalOrAnNPC(t *testing.T) {
	data := gamedatatest.Shipped(t)
	clear := data.MobRules().Sight + arrivalMargin
	maps := shipped(t).maps
	kept := map[string]bool{}
	for _, u := range undesigned {
		kept[u.mapID] = true
		if !slices.ContainsFunc(maps, func(m *mapfile.Map) bool { return m.ID == u.mapID }) {
			t.Errorf("undesigned lists %q, which no shipped map is", u.mapID)
		}
	}
	for _, m := range maps {
		if m.Kind != "hunting" || kept[m.ID] {
			continue
		}
		t.Run(m.ID, func(t *testing.T) {
			type place struct {
				what string
				at   nav.Point
			}
			arrivals := []place{{"the player spawn", mapfile.Point(m.PlayerSpawn.At)}}
			for _, pt := range m.Portals {
				arrivals = append(arrivals, place{"portal " + pt.Name, mapfile.Point(pt.At)})
			}
			for _, n := range m.NPCs {
				arrivals = append(arrivals, place{"NPC " + n.ID, mapfile.Point(n.At)})
			}
			type home struct {
				what string
				at   nav.Point
				r    float64
			}
			var homes []home
			for _, s := range m.Spawns {
				homes = append(homes, home{"spawn " + s.Name, mapfile.Point(s.At), s.Radius})
			}
			if b := m.Boss; b != nil {
				homes = append(homes, home{"the boss", mapfile.Point(b.At), b.Radius})
			}
			for _, a := range arrivals {
				for _, h := range homes {
					if gap := nav.Distance(a.at, h.at) - h.r; gap < clear {
						t.Errorf("%s's circle comes within %.2f m of %s; it keeps %.2f m, a mob's sight and %.0f m more", h.what, gap, a.what, clear, arrivalMargin)
					}
				}
			}
		})
	}
}
