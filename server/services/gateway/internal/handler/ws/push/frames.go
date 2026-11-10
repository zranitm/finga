package push

import (
	"sync"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/model"
)

// Frames shares each frame's encoding among its players; it keeps only the latest, as a frame goes to all in turn.
type Frames struct {
	mu      sync.Mutex
	frame   *model.Frame
	encoded *wsconn.Shared
}

// NewFrames makes the cache a gateway's outboxes share.
func NewFrames() *Frames { return &Frames{} }

func (f *Frames) shared(frame *model.Frame) *wsconn.Shared {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.frame != frame {
		f.frame, f.encoded = frame, wsconn.NewShared(&clientv1.Delta{
			Players: playerViews(frame.Players), Mobs: mobViews(frame.Mobs), Drops: dropViews(frame.Drops),
			RemovedPlayers: frame.RemovedPlayers, RemovedMobs: instanceIDs(frame.RemovedMobs), RemovedDrops: instanceIDs(frame.RemovedDrops),
			CollectedDrops: instanceIDs(frame.CollectedDrops),
		})
	}
	return f.encoded
}
