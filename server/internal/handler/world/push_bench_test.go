package world_test

import (
	"fmt"
	"log/slog"
	"testing"

	"finga/internal/handler/transport"
	"finga/internal/handler/world"
	model "finga/internal/model/gateway"
	"finga/internal/shared/protocol"
	"finga/pkg/wsconn"
)

// encodingConn is the socket a player's outbox writes to: what reaches it is encoded, as the writer does on a real one.
type encodingConn struct {
	codec *wsconn.ProtoCodec
	b     *testing.B
}

func (*encodingConn) ID() string        { return "c-1" }
func (*encodingConn) Close(int, string) {}
func (c *encodingConn) Send(m wsconn.Message) error {
	if _, err := c.codec.Encode(m); err != nil {
		c.b.Fatal(err)
	}
	return nil
}

// silent counts nothing.
type silent struct{}

func (silent) MessageIn(string)        {}
func (silent) MessageOut(string)       {}
func (silent) WelcomeSent(string, int) {}
func (silent) SlowConsumer()           {}
func (silent) Unencoded(string)        {}

// outbox is a player's outbox on the world's encoders.
func outbox(b *testing.B) *transport.Outbox {
	enc := transport.NewEncoders()
	if err := world.Encoders(enc); err != nil {
		b.Fatal(err)
	}
	return transport.NewOutbox(&encodingConn{codec: protocol.ServerCodec(wsconn.Binary), b: b}, silent{}, slog.New(slog.DiscardHandler), enc)
}

// busyFrame is a tick on a map where every one of n players moved and ten mobs walked, all in one cell.
func busyFrame(n int) *model.Frame {
	f := &model.Part{}
	for i := range n {
		f.Players = append(f.Players, model.PlayerView{
			ID: fmt.Sprintf("0190a0f4-1c2d-7e3f-8a4b-%012d", i), Name: fmt.Sprintf("Player%03d", i), Class: "warrior", Level: 12,
			HP: 180, MaxHP: 204, X: 3 + float64(i)/8, Z: 20, VX: 5, HZ: 1, OnGround: true,
		})
	}
	for i := range 10 {
		f.Mobs = append(f.Mobs, model.MobView{ID: uint64(i), Type: "green_slime", X: float64(i), Z: 25, Yaw: 1.5, HP: 30, MaxHP: 30})
	}
	return &model.Frame{Parts: []model.Part{*f}}
}

// One tick's frame going out to its n players: each message made on the stream's goroutine, encoded on its writer.
func BenchmarkAMapsFrameToThePlayers(b *testing.B) {
	for _, n := range []int{10, 50, 200} {
		b.Run(fmt.Sprintf("players=%d", n), func(b *testing.B) {
			o := outbox(b)
			b.ReportAllocs()
			for b.Loop() {
				frame, parts := busyFrame(n), []int{0} // one list for all, as the gateway's adapter shares one
				for i := range n {
					o.Deliver(model.Delivery{Tick: 9, Push: model.Delta{Frame: frame, Parts: parts, AckSeq: uint32(i)}}) //nolint:gosec // a benchmark's index
				}
			}
		})
	}
}

// Two maps' frames going out at once through one gateway, their players' messages interleaved as the maps' streams are: each frame's parts are still encoded once.
func BenchmarkTwoMapsFramesToTheirPlayers(b *testing.B) {
	for _, n := range []int{10, 50} {
		b.Run(fmt.Sprintf("players=%d", n), func(b *testing.B) {
			o := outbox(b)
			b.ReportAllocs()
			for b.Loop() {
				maps, parts := []*model.Frame{busyFrame(n), busyFrame(n)}, []int{0}
				for i := range 2 * n {
					o.Deliver(model.Delivery{Tick: 9, Push: model.Delta{Frame: maps[i%2], Parts: parts, AckSeq: uint32(i)}}) //nolint:gosec // a benchmark's index
				}
			}
		})
	}
}
