package e2etest

import (
	"fmt"
	"math"
	"slices"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// Pace is the player's own view in one frame, as a sprint reads it: the frame's tick, the input the server had applied by then, the sprint's count in sub-steps, and the speed in m/s.
type Pace struct {
	Tick   uint64
	Ack    uint32
	Sprint int
	Speed  float64
}

// Recording is the frames' paces of one player, and the ticks of the Damage that named them.
type Recording struct {
	id    string
	Paces []Pace
	Hits  []uint64
}

// Record starts a recording of the looter's own frames.
func (l *Looter) Record() *Recording { return &Recording{id: l.ID} }

// Take notes m: a Delta carrying the player's own view, or a Damage naming them.
func (r *Recording) Take(m wsconn.Message) {
	switch d := m.Data.(type) {
	case *clientv1.Delta:
		for _, v := range d.GetPlayers() {
			if v.GetId() == r.id {
				r.Paces = append(r.Paces, Pace{Tick: m.Tick, Ack: d.GetAckSeq(), Sprint: int(v.GetSprint()), Speed: math.Hypot(v.GetVx(), v.GetVz())})
			}
		}
	case *clientv1.Damage:
		if me := "p:" + r.id; d.GetSource() == me || d.GetTarget() == me {
			r.Hits = append(r.Hits, m.Tick)
		}
	}
}

// Hit is whether a Damage named the player yet.
func (r *Recording) Hit() bool { return len(r.Hits) > 0 }

// Through is whether the newest frame is ticks past the last hit, or past the first frame moving when none hit.
func (r *Recording) Through(ticks uint64) bool {
	if len(r.Paces) == 0 {
		return false
	}
	from := r.start()
	if n := len(r.Hits); n > 0 {
		from = r.Hits[n-1]
	}
	return r.Paces[len(r.Paces)-1].Tick >= from+ticks
}

// start is the tick of the first frame the player moves in; 0 for none.
func (r *Recording) start() uint64 {
	for _, p := range r.Paces {
		if p.Speed > 0 {
			return p.Tick
		}
	}
	return 0
}

// Fault is what the frames say against walking out of combat as large-regions.md section 7 has it, "" when they agree: a count of perTick sub-steps for each input the server applied since combat's last tick, at most steps, none while in combat (a hit's tick and the combat ticks after it), and once the inputs reach steps the walk's speed times mult. Frames merged on a starved runner skip ticks, so the walk is counted in acked inputs, not ticks; a skipped frame at combat's end leaves as many inputs unknown as ticks it skipped, which the count allows.
func (r *Recording) Fault(combat uint64, perTick, steps int, mult float64) string {
	start := r.start()
	if start == 0 {
		return "the player never moved"
	}
	walk, seen := 0.0, map[float64]int{} // the walk's speed is the one most frames at a count short of a sprint have, which a push's few don't
	for _, p := range r.Paces {
		if p.Speed > 0 && p.Sprint < steps {
			at := math.Round(p.Speed*1e6) / 1e6
			if seen[at]++; seen[at] > seen[walk] {
				walk = at
			}
		}
	}
	reached := false
	for _, p := range r.Paces {
		var hit uint64
		hurt := false
		for _, h := range r.Hits {
			if h < p.Tick && (!hurt || h > hit) {
				hit, hurt = h, true
			}
		}
		base := start - 1
		switch {
		case hurt:
			if p.Tick < hit+combat {
				if p.Sprint != 0 {
					return fmt.Sprintf("tick %d, %d ticks after a hit at %d, in combat: a sprint count of %d", p.Tick, p.Tick-hit, hit, p.Sprint)
				}
				continue
			}
			base = hit + combat - 1
		case len(r.Hits) > 0, p.Tick < start:
			continue
		}
		from, gap := r.applied(base)
		walked := int(p.Ack - from) //nolint:gosec // a few hundred inputs
		if want := min(steps, perTick*(walked-gap)); p.Sprint < want {
			return fmt.Sprintf("tick %d, %d inputs into the walk out of combat: a sprint count of %d, want %d or more", p.Tick, walked, p.Sprint, want)
		}
		if walked > (steps+perTick-1)/perTick+gap {
			if p.Sprint != steps || math.Abs(p.Speed-walk*mult) > 1e-6 {
				return fmt.Sprintf("tick %d, %d inputs into the walk out of combat: count %d at %.4f m/s, want %d at %.4f", p.Tick, walked, p.Sprint, p.Speed, steps, walk*mult)
			}
			reached = true
		}
	}
	if !reached {
		return "no frame showed the sprint within the recording"
	}
	return ""
}

// applied is the input the server had applied by tick, from the newest frame at or before it, and how many ticks that frame falls short of tick, each of which could have applied an input the frames don't show; a walk with no frame before it starts from the first one's input less one.
func (r *Recording) applied(tick uint64) (ack uint32, gap int) {
	for _, p := range slices.Backward(r.Paces) {
		if p.Tick <= tick {
			return p.Ack, int(tick - p.Tick) //nolint:gosec // a few hundred ticks
		}
	}
	return r.Paces[0].Ack - 1, 0
}
