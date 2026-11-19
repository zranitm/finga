package bots

import "slices"

// Ring is what a match's fighters looked like and started over the last gamedata.BotMemory, one frame a tick, which its bots read as of their reaction ago.
type Ring struct {
	frames  []frame // by tick, round the ring
	started []Started
}

type frame struct {
	tick uint64
	set  bool
	seen []Seen
}

// NewRing keeps the frames from back ticks ago to now, so a reaction of back ticks sees its own.
func NewRing(back uint64) *Ring { return &Ring{frames: make([]frame, back+1)} }

// Record keeps the fighters as they are at tick, in place of the frame a ring ago.
func (r *Ring) Record(tick uint64, seen []Seen) {
	f := &r.frames[tick%uint64(len(r.frames))]
	f.tick, f.set, f.seen = tick, true, append(f.seen[:0], seen...)
	span := uint64(len(r.frames))
	r.started = slices.DeleteFunc(r.started, func(s Started) bool { return max(s.Hit, s.Next)+span < tick })
}

// Start keeps an action a fighter started.
func (r *Ring) Start(s Started) { r.started = append(r.started, s) }

// As is the fighters as of tick of, or the oldest frame kept, and the actions shown by then still to hit or follow; the fighters are good until the next Record.
func (r *Ring) As(of, now uint64) ([]Seen, []Started) {
	var started []Started
	for _, s := range r.started {
		if s.Shown <= of && max(s.Hit, s.Next) >= now {
			started = append(started, s)
		}
	}
	if f := r.frames[of%uint64(len(r.frames))]; f.set && f.tick == of {
		return f.seen, started
	}
	var oldest *frame
	for k := range r.frames {
		if f := &r.frames[k]; f.set && (oldest == nil || f.tick < oldest.tick) {
			oldest = f
		}
	}
	if oldest == nil {
		return nil, started
	}
	return oldest.seen, started
}
