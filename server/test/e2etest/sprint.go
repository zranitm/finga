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

// Through is whether the newest frame is ticks past the last hit, or past the first frame moving when none hit; not before one of them.
func (r *Recording) Through(ticks uint64) bool {
	if len(r.Paces) == 0 {
		return false
	}
	from := r.start()
	if n := len(r.Hits); n > 0 {
		from = r.Hits[n-1]
	}
	if from == 0 {
		return false // none moving yet and none hit: a late first frame is no end of the walk
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

// Fault is what the frames say against walking out of combat as large-regions.md section 7 has it, "" when they agree: a count of perTick sub-steps for each tick the server stepped the player since combat's last tick, at most steps, none while in combat (a hit's tick and the combat ticks after it), and once the steps reach steps/perTick the walk's speed times mult. The server steps a player once a tick however many inputs it acks, and a starved runner merges frames, so a step is only known where the ack rose between two frames: at least one however long the gap, which is the count's floor. A skipped frame at combat's end leaves as many steps unknown as ticks it skipped, which the count allows.
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
	for i, p := range r.Paces {
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
		walked, gap := r.stepped(i, base)
		if want := min(steps, perTick*(walked-gap)); p.Sprint < want {
			return fmt.Sprintf("tick %d, %d steps into the walk out of combat: a sprint count of %d, want %d or more", p.Tick, walked, p.Sprint, want)
		}
		if walked > (steps+perTick-1)/perTick+gap {
			if p.Sprint != steps || math.Abs(p.Speed-walk*mult) > 1e-6 {
				return fmt.Sprintf("tick %d, %d steps into the walk out of combat: count %d at %.4f m/s, want %d at %.4f", p.Tick, walked, p.Sprint, p.Speed, steps, walk*mult)
			}
			reached = true
		}
	}
	if !reached {
		return "no frame showed the sprint within the recording"
	}
	return ""
}

// stepped is the steps the server took of the player by frame i, from the newest frame of the walk at or before tick: one for each frame after it whose ack rose on the one before, and how many ticks that frame falls short of tick, each of which could have stepped the player unseen; with no such frame it is from the walk's first frame, which is a step.
func (r *Recording) stepped(i int, tick uint64) (steps, gap int) {
	start := r.start()
	from := -1
	for k, p := range slices.Backward(r.Paces) {
		if p.Tick <= tick && p.Tick >= start {
			from, gap = k, int(tick-p.Tick) //nolint:gosec // a few hundred ticks
			break
		}
	}
	if from < 0 {
		from = slices.IndexFunc(r.Paces, func(p Pace) bool { return p.Tick == start })
		steps = 1
	}
	for k := from + 1; k <= i; k++ {
		if r.Paces[k].Ack > r.Paces[k-1].Ack {
			steps++
		}
	}
	return steps, gap
}
