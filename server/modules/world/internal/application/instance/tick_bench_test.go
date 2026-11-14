package instance_test

import (
	"testing"

	"finga/internal/gamedata/gamedatatest"
	"finga/modules/world/internal/application/instance"
	"finga/modules/world/worldtest"
)

// One tick of every shipped map with nobody on it, as most of the world is most of the time: their mobs patrol for no one.
func BenchmarkAnEmptyWorldsTick(b *testing.B) {
	data := gamedatatest.Shipped(b)
	var maps []*instance.Instance
	for _, m := range data.Maps() {
		maps = append(maps, worldtest.NewInstance(m, data, nil, worldtest.Options{}))
	}
	b.ReportAllocs()
	for n := uint64(1); b.Loop(); n++ {
		for _, inst := range maps {
			inst.Step(n, worldtest.TickInterval)
		}
	}
}
