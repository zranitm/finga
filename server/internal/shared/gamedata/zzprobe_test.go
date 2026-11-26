package gamedata_test

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/nav"
)

// ZZPROBE="id,x,z;..." points; ZZSCAN="id,x0,z0,x1,z1" box scan
func TestZZProbe(t *testing.T) {
	spec := os.Getenv("ZZPROBE")
	scan := os.Getenv("ZZSCAN")
	if spec == "" && scan == "" {
		t.Skip()
	}
	d := gamedatatest.Shipped(t)
	if scan != "" {
		var id string
		var x0, z0, x1, z1 float64
		fmt.Sscanf(strings.ReplaceAll(scan, ",", " "), "%s %f %f %f %f", &id, &x0, &z0, &x1, &z1)
		m, _ := d.Map(id)
		lo, hi, bad := math.Inf(1), math.Inf(-1), 0
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				h := m.Geometry.Terrain.Height(x, z)
				lo, hi = math.Min(lo, h), math.Max(hi, h)
				if !m.Grid().Fits(nav.Point{X: x, Z: z}, d.Movement().Radius+0.25) || m.Geometry.SolidOver(x, z, h-5) {
					bad++
					fmt.Printf("  blocked (%v,%v)\n", x, z)
				}
			}
		}
		fmt.Printf("scan %s: height %.2f..%.2f, %d blocked\n", scan, lo, hi, bad)
	}
	if spec == "" {
		return
	}
	for _, q := range strings.Split(spec, ";") {
		var id string
		var x, z float64
		fmt.Sscanf(strings.ReplaceAll(q, ",", " "), "%s %f %f", &id, &x, &z)
		m, ok := d.Map(id)
		if !ok {
			fmt.Println(id, "no map")
			continue
		}
		g := m.Geometry
		fmt.Printf("%s (%v,%v): h=%.2f barred=%v fits=%v town=%v area=%d\n", id, x, z, g.Terrain.Height(x, z), g.Terrain.Barred(x, z), m.Grid().Fits(nav.Point{X: x, Z: z}, d.Movement().Radius), m.TownAt(x, z), m.AreaAt(x, z))
	}
}
