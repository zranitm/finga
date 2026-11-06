package handler

import (
	"testing"

	"google.golang.org/protobuf/proto"

	worldv1 "finga/api/gen/world/v1"
	"finga/internal/kernel"
	"finga/pkg/hub"
	"finga/services/world/internal/model"
)

// A tick's frame reaches its gateway's stream once, whole, with each recipient's own ack.
func TestAFrameGoesToItsGatewayOnce(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](8).Subscribe("gw")
	a, b := &streamSink{sub: sub, character: "a", conn: "c-a"}, &streamSink{sub: sub, character: "b", conn: "c-b"}
	if a.Stream() != b.Stream() {
		t.Fatal("two players on one gateway have two streams")
	}
	frame := &model.Frame{
		Tick: 3, Players: []model.PlayerView{{ID: kernel.NewCharacterID(), Name: "Ayla"}}, Mobs: []model.MobView{{ID: 4}},
		Drops: []model.DropView{{ID: 5}}, Removed: []kernel.CharacterID{kernel.NewCharacterID()}, RemovedMobs: []uint64{6}, RemovedDrops: []uint64{7},
	}
	ayla, robin := kernel.NewCharacterID(), kernel.NewCharacterID()
	if !a.Stream().SendFrame(frame, []model.Recipient{{Character: ayla, Connection: "c-a", AckSeq: 8}, {Character: robin, Connection: "c-b"}}) {
		t.Fatal("the stream refused the frame")
	}
	if n := len(sub.C()); n != 1 {
		t.Fatalf("%d messages on the stream, want 1", n)
	}
	got := <-sub.C()
	want := &worldv1.DeliverResponse{Tick: 3, Event: &worldv1.DeliverResponse_Frame{Frame: &worldv1.Frame{
		Delta: encodeFrame(frame),
		Recipients: []*worldv1.Recipient{
			{CharacterId: ayla.String(), ConnectionId: "c-a", AckSeq: 8},
			{CharacterId: robin.String(), ConnectionId: "c-b"},
		},
	}}}
	if !proto.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if d := got.GetFrame().GetDelta(); len(d.GetPlayers()) != 1 || len(d.GetMobs()) != 1 || len(d.GetDrops()) != 1 || len(d.GetRemovedPlayers()) != 1 ||
		len(d.GetRemovedMobs()) != 1 || len(d.GetRemovedDrops()) != 1 {
		t.Fatalf("the frame lost a part: %v", d)
	}
}

// A gateway whose stream is full refuses the frame, which drops its players.
func TestAFullStreamRefusesTheFrame(t *testing.T) {
	sub := hub.New[*worldv1.DeliverResponse](1).Subscribe("gw")
	s := &streamSink{sub: sub}
	if !s.Send(model.Detached{}) {
		t.Fatal("the first message was refused")
	}
	if s.Stream().SendFrame(&model.Frame{}, []model.Recipient{{Character: kernel.NewCharacterID()}}) {
		t.Fatal("a full stream took the frame")
	}
}
