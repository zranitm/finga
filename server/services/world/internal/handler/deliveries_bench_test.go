package handler

import (
	"fmt"
	"testing"

	"google.golang.org/protobuf/proto"

	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/services/world"
)

// busyFrame is a tick on a map where every one of n players moved and ten mobs walked, for n players on one gateway.
func busyFrame(n int) world.Frame {
	var f world.Frame
	for i := range n {
		f.Players = append(f.Players, world.PlayerView{
			ID: kernel.NewCharacterID(), Name: fmt.Sprintf("Player%03d", i), Class: kernel.Class("warrior"), Level: 12, HP: 180, MaxHP: 204,
			State: sim.State{X: 100 + float64(i), Y: 608, VX: 200, OnGround: true, Facing: 1},
		})
		f.Recipients = append(f.Recipients, world.Recipient{Character: kernel.NewCharacterID(), Connection: fmt.Sprintf("c-%d", i), AckSeq: uint32(i)}) //nolint:gosec // a benchmark's index
	}
	for i := range 10 {
		f.Mobs = append(f.Mobs, world.MobView{ID: uint64(i), Type: "green_slime", X: float64(i * 40), Y: 590, Facing: 1, HP: 30, MaxHP: 30})
	}
	return f
}

// One tick's frame on the world's stream, as the facade hands it over, for n players on one gateway: turned into world.v1 and marshaled.
// The facade's BenchmarkAMapsFrameIntoTheFacade is the half before it.
func BenchmarkAFrameOnTheWorldStream(b *testing.B) {
	for _, n := range []int{10, 50, 200} {
		b.Run(fmt.Sprintf("players=%d", n), func(b *testing.B) {
			d := world.Delivery{Tick: 9, Event: busyFrame(n)}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := proto.Marshal(deliverResponse(d)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
