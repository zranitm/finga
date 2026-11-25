package world

import (
	"testing/synctest"

	"finga/internal/application/world/instance"
	"finga/internal/shared/kernel"
)

// ChannelSweep is how often the World looks for idle channels, for the tests that wait one out.
const ChannelSweep = channelSweep

// MapActor is the map named mapID, for the external tests that hand a player off from it as its tick would.
func (w *World) MapActor(mapID string) *instance.Instance { return w.instances[mapID] }

// Opened is the instance opened on demand under id, a match's or a channel's, or nil.
func (w *World) Opened(id string) *instance.Instance {
	inst, _ := w.opened.get(id)
	return inst
}

// StopMatchOf stops the map of id's match as the World's stop would, leaving id fighting in it and their presence on it, as a rejoin that read both just before the stop finds them; run in a synctest bubble.
func (w *World) StopMatchOf(id kernel.CharacterID) {
	match, _ := w.fighting.of(id)
	w.opened.shut(match)
	synctest.Wait()
	w.fighting.enter(match, []kernel.CharacterID{id})
}
