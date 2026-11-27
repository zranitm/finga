package account

import (
	"testing"

	"finga/internal/application/world"
	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
	"finga/pkg/copytest"
)

// deliveryOf is the events a stream's delivery becomes.
func deliveryOf(d world.Delivery) []model.WorldEvent {
	var got []model.WorldEvent
	delivered(d, func(ev model.WorldEvent) { got = append(got, ev) })
	return got
}

// A frame becomes one delivery per recipient, all sharing one copy of it and its tick, each with its recipient.
func TestAFrameBecomesOneDeliveryPerRecipientSharingTheFrame(t *testing.T) {
	a, b := kernel.NewCharacterID(), kernel.NewCharacterID()
	got := deliveryOf(world.Delivery{Tick: 9, Event: world.Frame{
		Parts:      []world.Part{{RemovedMobs: []uint64{1}}},
		Recipients: []world.Recipient{{Character: a, Connection: "c-a", AckSeq: 3}, {Character: b, Connection: "c-b", AckSeq: 4}},
	}})
	if len(got) != 2 {
		t.Fatalf("%d events", len(got))
	}
	var frame *world.Frame
	for i, want := range []struct {
		character kernel.CharacterID
		conn      string
		ack       uint32
	}{{a, "c-a", 3}, {b, "c-b", 4}} {
		d, ok := got[i].(model.WorldDelivery)
		if !ok || d.Character != want.character || d.Connection != want.conn || d.Delivery.Tick != 9 || d.Delivery.Starts || d.Delivery.MapID != "" {
			t.Fatalf("event %d is %+v", i, got[i])
		}
		framed, ok := d.Delivery.Event.(world.Framed)
		if !ok || framed.Recipient.AckSeq != want.ack || framed.Recipient.Character != want.character {
			t.Fatalf("event %d carries %+v", i, d.Delivery.Event)
		}
		if frame != nil && framed.Frame != frame {
			t.Fatal("the recipients' deliveries share no frame")
		}
		frame = framed.Frame
	}
	if len(frame.Parts) != 1 || len(frame.Parts[0].RemovedMobs) != 1 {
		t.Fatalf("the shared frame is %+v", frame)
	}
}

// A Snapshot starts a join's ticks and a teleport names the map the player is on from then; no other event does either.
func TestASnapshotStartsTheTicksAndATeleportNamesItsMap(t *testing.T) {
	for _, c := range []struct {
		event  world.Event
		starts bool
		mapID  string
	}{
		{world.Snapshot{MapID: "town"}, true, ""},
		{world.Teleport{MapID: "forest"}, false, "forest"},
		{world.Damage{Amount: 3}, false, ""},
		{world.WaypointFound{Waypoint: "w"}, false, ""},
	} {
		got := deliveryOf(world.Delivery{Character: kernel.NewCharacterID(), Conn: "c", Tick: 4, Event: c.event})
		d, ok := got[0].(model.WorldDelivery)
		if len(got) != 1 || !ok || d.Delivery.Starts != c.starts || d.Delivery.MapID != c.mapID || d.Delivery.Tick != 4 {
			t.Errorf("%T became %+v", c.event, got)
		}
	}
}

// Every world event but a frame and a detach reaches its player as it is, with the tick, connection and character the world named.
func TestEveryOtherWorldEventReachesItsPlayerAsItIs(t *testing.T) {
	type streamed struct {
		Character kernel.CharacterID
		Conn      string
		Tick      uint64
	}
	type delivered struct {
		Character  kernel.CharacterID
		Connection string
		Delivery   struct{ Tick uint64 }
	}
	m := copytest.Match{Renames: map[string]string{"Conn": "Connection", "Tick": "Delivery.Tick"}}
	if got := copytest.Diff(m, func(s streamed) delivered {
		out := deliveryOf(world.Delivery{Character: s.Character, Conn: s.Conn, Tick: s.Tick, Event: world.ExpGained{Amount: 1}})
		d := out[0].(model.WorldDelivery)
		return delivered{d.Character, d.Connection, struct{ Tick uint64 }{d.Delivery.Tick}}
	}); len(got) > 0 {
		t.Error(got)
	}
}

// A detach names the connection and the character that must join again.
func TestADetachNamesItsConnectionAndCharacter(t *testing.T) {
	id := kernel.NewCharacterID()
	got := deliveryOf(world.Delivery{Character: id, Conn: "c-1", Event: world.Detached{}})
	if d, ok := got[0].(model.Detached); len(got) != 1 || !ok || d.Connection != "c-1" || d.Character != id {
		t.Fatalf("became %+v", got)
	}
}
