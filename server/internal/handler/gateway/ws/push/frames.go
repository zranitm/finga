package push

import (
	"slices"
	"sync"

	clientv1 "finga/api/gen/client/v1"
	model "finga/internal/model/gateway"
	"finga/pkg/wsconn"
)

// Frames shares each frame part's encoding among the players sent it; it keeps only the latest frame's, as a frame goes to all in turn.
type Frames struct {
	mu      sync.Mutex
	frame   *model.Frame
	encoded []*wsconn.Shared // by part, each made on its first use
	parts   []int            // the last player's, whose list next ones sent the same share
	last    []*wsconn.Shared
}

// NewFrames makes the cache a gateway's outboxes share.
func NewFrames() *Frames { return &Frames{} }

func (f *Frames) shared(frame *model.Frame, parts []int) []*wsconn.Shared {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.frame != frame {
		f.frame, f.encoded, f.parts = frame, make([]*wsconn.Shared, len(frame.Parts)), nil
	}
	if f.parts != nil && slices.Equal(parts, f.parts) {
		return f.last
	}
	out := make([]*wsconn.Shared, len(parts))
	for i, k := range parts {
		if f.encoded[k] == nil {
			p := frame.Parts[k]
			f.encoded[k] = wsconn.NewShared(&clientv1.Delta{
				Players: playerViews(p.Players), Mobs: mobViews(p.Mobs),
				RemovedPlayers: p.RemovedPlayers, RemovedMobs: instanceIDs(p.RemovedMobs), Boss: mobView(p.Boss),
			})
		}
		out[i] = f.encoded[k]
	}
	f.parts, f.last = parts, out
	return out
}
