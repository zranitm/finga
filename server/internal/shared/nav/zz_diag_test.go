package nav_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/nav"
)

func TestZZDiag(t *testing.T) {
	data := gamedatatest.Shipped(t)
	p := shipped(t).params
	rise := nav.JumpRise(p)
	want := os.Getenv("DIAG")
	for _, m := range huntingGrounds(t, data) {
		g := m.grid()
		player := g.Reach(m.starts, p.Radius, rise)
		for _, h := range m.homes {
			if m.id+"/"+h.what != want {
				continue
			}
			def, _ := data.Mob(h.mob)
			leash := h.r + data.MobRules().Leash
			walk := g.WalkWithin(h.at, def.Radius, h.at, leash)
			bs := bands(def, p)
			var sb strings.Builder
			fmt.Fprintf(&sb, "home %v leash %v\n", h.at, leash)
			for z := h.at.Z + leash; z >= h.at.Z-leash; z -= 1 {
				for x := h.at.X - leash; x <= h.at.X+leash; x += 1 {
					q := nav.Point{X: x + 0.25, Z: z + 0.25}
					c := byte(' ')
					if nav.Distance(q, h.at) <= leash {
						switch {
						case !player.Has(q):
							c = '#'
						case lands(g, walk, q, g.GroundAt(q), bs):
							c = '.'
						case walk.Has(q):
							c = 'w'
						default:
							c = 'X'
						}
					}
					sb.WriteByte(c)
				}
				sb.WriteByte('\n')
			}
			t.Log(sb.String())
		}
	}
}
