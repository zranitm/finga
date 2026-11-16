package nav_test

import (
	"testing"

	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/mapfile"
	"finga/internal/shared/nav"
)

// arrivalMargin is how far past a mob's sight every spawn and boss circle keeps from where players come in or talk, so a mob at its circle's edge doesn't see them.
const arrivalMargin = 2.0

// On every hunting map, no mob sees a player arrive or stand at an NPC: each spawn and the boss keep their circles a mob's sight plus arrivalMargin from the player spawn, every portal and every NPC, so a fight is one a player walks into.
func TestNoSpawnSeesAnArrivalOrAnNPC(t *testing.T) {
	data := gamedatatest.Shipped(t)
	clear := data.MobRules().Sight + arrivalMargin
	for _, m := range shipped(t).maps {
		if m.Kind != "hunting" {
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
