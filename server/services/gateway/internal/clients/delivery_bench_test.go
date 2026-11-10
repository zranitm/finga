package clients

import (
	"fmt"
	"testing"

	"google.golang.org/protobuf/proto"

	worldv1 "finga/api/gen/world/v1"
	"finga/services/gateway/internal/model"
)

// busyFrame is a tick on a map where every one of n players moved and ten mobs walked, for each of them.
func busyFrame(n int) *worldv1.Frame {
	f := &worldv1.Frame{Delta: &worldv1.Delta{}}
	for i := range n {
		id := fmt.Sprintf("0190a0f4-1c2d-7e3f-8a4b-%012d", i)
		f.Delta.Players = append(f.Delta.Players, &worldv1.PlayerView{
			Id: id, Name: fmt.Sprintf("Player%03d", i), Class: "warrior", Level: 12,
			Hp: 180, MaxHp: 204, X: 100 + float64(i), Y: 608, Vx: 200, Facing: 1, OnGround: true,
		})
		f.Recipients = append(f.Recipients, &worldv1.Recipient{CharacterId: id, ConnectionId: fmt.Sprintf("c-%d", i), AckSeq: uint32(i)}) //nolint:gosec // a benchmark's index
	}
	for i := range 10 {
		f.Delta.Mobs = append(f.Delta.Mobs, &worldv1.MobView{Id: uint64(i), Type: "green_slime", X: float64(i * 40), Y: 590, Facing: 1, Hp: 30, MaxHp: 30})
	}
	return f
}

// One tick's frame off the world stream for n players: unmarshaled once and handed to each, on one goroutine.
func BenchmarkAMapsFrameOffTheWorldStream(b *testing.B) {
	for _, n := range []int{10, 50, 200} {
		b.Run(fmt.Sprintf("players=%d", n), func(b *testing.B) {
			raw, err := proto.Marshal(&worldv1.DeliverResponse{Tick: 9, Event: &worldv1.DeliverResponse_Frame{Frame: busyFrame(n)}})
			if err != nil {
				b.Fatal(err)
			}
			got := 0
			deliver := func(model.WorldEvent) { got++ }
			b.ReportAllocs()
			for b.Loop() {
				var m worldv1.DeliverResponse
				if err := proto.Unmarshal(raw, &m); err != nil {
					b.Fatal(err)
				}
				delivered(&m, deliver)
			}
			if got == 0 {
				b.Fatal("nothing delivered")
			}
		})
	}
}
