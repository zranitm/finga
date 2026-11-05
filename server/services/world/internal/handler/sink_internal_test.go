package handler

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"finga/internal/kernel"
	"finga/services/world/internal/model"
)

// A tick's Delta is encoded once for everyone on the map; each recipient gets only its own AckSeq.
func TestATicksDeltaIsEncodedOnceForEveryRecipient(t *testing.T) {
	frame := &model.Frame{Tick: 9, Players: []model.PlayerView{{ID: kernel.NewCharacterID(), Name: "Ayla"}}, Removed: []kernel.CharacterID{kernel.NewCharacterID()}}
	a, b := &streamSink{character: "a", conn: "c-a"}, &streamSink{character: "b", conn: "c-b"}
	ma := a.delivery(model.Delta{Frame: frame, AckSeq: 1}).GetDelta()
	mb := b.delivery(model.Delta{Frame: frame, AckSeq: 2}).GetDelta()
	if ma.GetAckSeq() != 1 || mb.GetAckSeq() != 2 {
		t.Fatalf("ack seqs %d, %d", ma.GetAckSeq(), mb.GetAckSeq())
	}
	if ma.GetPlayers()[0] != mb.GetPlayers()[0] || &ma.GetRemovedPlayers()[0] != &mb.GetRemovedPlayers()[0] {
		t.Fatal("each recipient's Delta was encoded again")
	}
}

// Each recipient's copy of the shared Delta carries every field of it.
func TestARecipientsDeltaCarriesTheWholeFrame(t *testing.T) {
	frame := &model.Frame{
		Tick: 3, Players: []model.PlayerView{{ID: kernel.NewCharacterID(), Name: "Ayla"}}, Mobs: []model.MobView{{ID: 4}},
		Drops: []model.DropView{{ID: 5}}, Removed: []kernel.CharacterID{kernel.NewCharacterID()}, RemovedMobs: []uint64{6}, RemovedDrops: []uint64{7},
	}
	got := (&streamSink{}).delivery(model.Delta{Frame: frame, AckSeq: 8}).GetDelta()
	want := encodeFrame(frame)
	want.AckSeq = 8
	if !proto.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func BenchmarkADeltaToTwoHundredPlayers(b *testing.B) {
	views := make([]model.PlayerView, 200)
	for i := range views {
		views[i] = model.PlayerView{ID: kernel.NewCharacterID(), Name: "player"}
	}
	sinks := make([]*streamSink, len(views))
	for i := range sinks {
		sinks[i] = &streamSink{character: views[i].ID.String(), conn: "c"}
	}
	b.ReportAllocs()
	for b.Loop() {
		frame := &model.Frame{Tick: 1, Players: views}
		for _, s := range sinks {
			_ = s.delivery(model.Delta{Frame: frame, AckSeq: 1})
		}
	}
}
