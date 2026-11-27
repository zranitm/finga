package world

import (
	"runtime"
	"slices"
	"sync"
	"weak"

	clientv1 "finga/api/gen/client/v1"
	model "finga/internal/model/gateway"
	"finga/pkg/wsconn"
)

// Frames shares each frame part's encoding among the players sent it, each frame's for as long as it lives, as several maps' frames interleave on a gateway's outboxes.
type Frames struct {
	mu     sync.Mutex
	frames map[weak.Pointer[model.Frame]]*encoding
	frame  *model.Frame // the last one asked for, whose players come in a run
	last   *encoding
}

// encoding is a frame's parts as encoded, each made on its first use, and the last player's list of them, which the next sent the same share.
type encoding struct {
	parts []*wsconn.Shared
	last  []int
	out   []*wsconn.Shared
}

// newFrames makes the cache a gateway's outboxes share.
func newFrames() *Frames { return &Frames{frames: map[weak.Pointer[model.Frame]]*encoding{}} }

func (f *Frames) shared(frame *model.Frame, parts []int) []*wsconn.Shared {
	f.mu.Lock()
	defer f.mu.Unlock()
	if frame != f.frame {
		key := weak.Make(frame)
		e, ok := f.frames[key]
		if !ok {
			e = &encoding{parts: make([]*wsconn.Shared, len(frame.Parts))}
			f.frames[key] = e
			runtime.AddCleanup(frame, f.forget, key)
		}
		f.frame, f.last = frame, e
	}
	e := f.last
	if e.last != nil && slices.Equal(parts, e.last) {
		return e.out
	}
	out := make([]*wsconn.Shared, len(parts))
	for i, k := range parts {
		if e.parts[k] == nil {
			p := frame.Parts[k]
			e.parts[k] = wsconn.NewShared(&clientv1.Delta{
				Players: playerViews(p.Players), Steps: playerSteps(p.Steps), Mobs: mobViews(p.Mobs), MobSteps: mobSteps(p.MobSteps),
				RemovedPlayers: p.RemovedPlayers, RemovedMobs: instanceIDs(p.RemovedMobs), Boss: mobView(p.Boss),
			})
		}
		out[i] = e.parts[k]
	}
	e.last, e.out = parts, out
	return out
}

// forget drops a collected frame's encodings.
func (f *Frames) forget(key weak.Pointer[model.Frame]) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.frames, key)
}
