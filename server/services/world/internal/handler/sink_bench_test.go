package handler

import (
	"fmt"
	"testing"

	"google.golang.org/protobuf/proto"

	worldv1 "finga/api/gen/world/v1"
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

// One tick's frame on the world's side, for n players on one gateway: built and marshaled for its stream.
func BenchmarkAMapsFrameOnTheWorldStream(b *testing.B) {
	for _, n := range []int{10, 50, 200} {
		b.Run(fmt.Sprintf("players=%d", n), func(b *testing.B) {
			sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
			to := make([]model.Recipient, n)
			for i := range to {
				to[i] = model.Recipient{Character: kernel.NewCharacterID(), Connection: fmt.Sprintf("c-%d", i), AckSeq: uint32(i)} //nolint:gosec // a benchmark's index
			}
			stream := (&streamSink{sub: sub}).Stream()
			b.ReportAllocs()
			for b.Loop() {
				if !stream.SendFrame(busyFrame(n), to) {
					b.Fatal("refused")
				}
				if _, err := proto.Marshal(<-sub.C()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
