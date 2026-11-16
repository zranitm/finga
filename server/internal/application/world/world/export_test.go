package world

import "finga/internal/application/world/instance"

// MapActor is the map named mapID, for the external tests that hand a player off from it as its tick would.
func (w *World) MapActor(mapID string) *instance.Instance { return w.instances[mapID] }
