package push

import (
	"fmt"
	"testing"

	"finga/internal/protocol"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/model"
)

// busyFrame is a tick on a map where every one of n players moved and ten mobs walked.
func busyFrame(n int) *model.Frame {
	f := &model.Frame{}
	for i := range n {
		f.Players = append(f.Players, model.PlayerView{
			ID: fmt.Sprintf("0190a0f4-1c2d-7e3f-8a4b-%012d", i), Name: fmt.Sprintf("Player%03d", i), Class: "warrior", Level: 12,
			HP: 180, MaxHP: 204, X: 100 + float64(i), Y: 608, VX: 200, Facing: 1, OnGround: true,
		})
	}
	for i := range 10 {
		f.Mobs = append(f.Mobs, model.MobView{ID: uint64(i), Type: "green_slime", X: float64(i * 40), Y: 590, Facing: 1, HP: 30, MaxHP: 30})
	}
	return f
}

// One tick's frame going out to its n players: each one's message made (on the stream's goroutine) and
// encoded (on its connection's writer).
func BenchmarkAMapsFrameToThePlayers(b *testing.B) {
	codec := protocol.ServerCodec(wsconn.Binary)
	for _, n := range []int{10, 50, 200} {
		b.Run(fmt.Sprintf("players=%d", n), func(b *testing.B) {
			o := &Outbox{frames: NewFrames()}
			b.ReportAllocs()
			for b.Loop() {
				frame := busyFrame(n)
				for i := range n {
					body := o.body(model.Delta{Frame: frame, AckSeq: uint32(i)}) //nolint:gosec // a benchmark's index
					if _, err := codec.Encode(wsconn.Message{Tick: 9, Data: body}); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
