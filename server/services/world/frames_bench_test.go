package world

import (
	"fmt"
	"testing"

	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/pkg/hub"
	"finga/services/world/internal/model"
)

// busyFrame is a tick on a map where every one of n players moved and ten mobs walked.
func busyFrame(n int) *model.Frame {
	f := &model.Frame{Tick: 9}
	for i := range n {
		f.Players = append(f.Players, model.PlayerView{
			ID: kernel.NewCharacterID(), Name: fmt.Sprintf("Player%03d", i), Class: kernel.Class("warrior"), Level: 12, HP: 180, MaxHP: 204,
			State: sim.State{X: 100 + float64(i), Y: 608, VX: 200, OnGround: true, Facing: 1},
		})
	}
	for i := range 10 {
		f.Mobs = append(f.Mobs, model.MobView{ID: uint64(i), Type: "green_slime", X: float64(i * 40), Y: 590, Facing: 1, HP: 30, MaxHP: 30})
	}
	return f
}

// One tick's frame for n players on one gateway, built by the map and handed to the gateway's stream as the facade's own types.
// The handler's BenchmarkAFrameOnTheWorldStream is the half after it.
func BenchmarkAMapsFrameIntoTheFacade(b *testing.B) {
	for _, n := range []int{10, 50, 200} {
		b.Run(fmt.Sprintf("players=%d", n), func(b *testing.B) {
			sub := hub.New[Delivery](1).Subscribe("gw")
			to := make([]model.Recipient, n)
			for i := range to {
				to[i] = model.Recipient{Character: kernel.NewCharacterID(), Connection: fmt.Sprintf("c-%d", i), AckSeq: uint32(i)} //nolint:gosec // a benchmark's index
			}
			s := stream{sub: sub}
			b.ReportAllocs()
			for b.Loop() {
				if !s.SendFrame(busyFrame(n), to) {
					b.Fatal("refused")
				}
				<-sub.C()
			}
		})
	}
}
