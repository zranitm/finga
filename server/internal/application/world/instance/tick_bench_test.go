package instance_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
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

// One tick of the start town with 50 players walking about it, each frame carrying them all to all.
func BenchmarkABusyTownsTick(b *testing.B) {
	data := gamedatatest.Shipped(b)
	town := worldtest.Town(data)
	inst := worldtest.NewInstance(town, data, nil, worldtest.Options{})
	r := rand.New(rand.NewPCG(4, 1))
	ids := make([]kernel.CharacterID, 50)
	var joined sync.WaitGroup
	for k := range ids {
		p := worldtest.NewPlayer(fmt.Sprintf("P%02d", k), town.PlayerSpawn.X+r.Float64()*8-4, town.PlayerSpawn.Z+r.Float64()*8-4)
		ids[k] = p.ID
		joined.Go(func() { _ = inst.Join(context.Background(), p, quiet{}) })
	}
	done := make(chan struct{})
	go func() { joined.Wait(); close(done) }()
	n := uint64(0)
	for waiting := true; waiting; {
		select {
		case <-done:
			waiting = false
		default:
			n++
			inst.Step(n, worldtest.TickInterval)
		}
	}
	keys := make([]sim.Input, len(ids))
	seq := uint32(0)
	b.ReportAllocs()
	for b.Loop() {
		n++
		seq++
		for k, id := range ids {
			if r.IntN(40) == 0 {
				keys[k] = sim.Input{Left: r.IntN(3) == 0, Right: r.IntN(3) == 0, Up: r.IntN(2) == 0, Down: r.IntN(3) == 0}
			}
			_ = inst.Input(id, seq, keys[k], 0)
		}
		inst.Step(n, worldtest.TickInterval)
	}
}

// quiet is a player's sink and stream that take everything and keep nothing.
type quiet struct{}

func (quiet) Send(model.Event) bool                          { return true }
func (q quiet) Stream() instance.Stream                      { return q }
func (quiet) SendFrame(*model.Frame, []model.Recipient) bool { return true }
