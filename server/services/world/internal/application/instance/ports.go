package instance

import (
	"time"

	"finga/internal/kernel"
	"finga/services/world/internal/model"
)

// Sink is where a map sends a player's events. Send must not block; false drops the player.
type Sink interface {
	Send(model.Event) bool
	// Stream is what the sink sends on; a tick's frame goes once to each stream, for all its players.
	Stream() Stream
}

// Stream carries the frames of every player on it. SendFrame must not block; false drops every recipient.
type Stream interface {
	SendFrame(f *model.Frame, to []model.Recipient) bool
}

// Checkpoints takes a map's checkpoints to save; Put must not block or lose one, though a newer one may replace it.
type Checkpoints interface {
	Put(cp model.Checkpoint)
}

// Mover moves a player to another map's instance; it must not block the calling tick.
type Mover interface {
	Transfer(p model.Player, sink Sink, mapID string)
	// Dropped tells of a player the map removed because its sink failed.
	Dropped(from *Instance, id kernel.CharacterID, conn string)
}

// TickObserver sees each tick's duration against its budget.
type TickObserver interface {
	ObserveTick(mapID string, took, budget time.Duration)
}

// Parties answers party questions from inside a tick, and takes what the map saw of a member.
type Parties interface {
	Members(id kernel.CharacterID) []kernel.CharacterID
	SameParty(a, b kernel.CharacterID) bool
	Update(id kernel.CharacterID, mapID string, level, hp, maxHP int)
}
