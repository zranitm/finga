package sim_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"finga/internal/shared/regiongen"
	"finga/internal/shared/sim"
)

// The vectors in testdata/movement are made here from the scenarios below and replayed exactly by web/src/sim; -update rewrites them.
var update = flag.Bool("update", false, "rewrite testdata/movement from the scenarios")

const vectorsDir = "../../../testdata/movement"

// vector is one file: a scenario with its params, inputs and the state after each sub-step.
type vector struct {
	Name        string       `json:"name"`
	Notes       string       `json:"notes"`
	Region      string       `json:"region,omitempty"` // a test plan the web client keeps baked: Geometry is its ground, written as the name alone
	Params      sim.Params   `json:"params"`
	SpeedFactor float64      `json:"speed_factor,omitempty"` // unset is 1
	Geometry    sim.Geometry `json:"geometry"`
	Start       sim.State    `json:"start"`
	Inputs      []inputRun   `json:"inputs"`
	Expected    []sim.State  `json:"expected"`
}

// inputRun holds one input for Repeat sub-steps; Blockers, when set, are the mobs from this run on, Held and Slow, when set, a status the world sets before its first sub-step, Breaks a break-free ending the hold after them, EndsSprint an attack or skill pressed, and Move, when set, starts last; Fighting steps them as a body in combat.
type inputRun struct {
	Input      sim.Input     `json:"input"`
	Repeat     int           `json:"repeat"`
	Blockers   []sim.Blocker `json:"blockers,omitempty"`
	Held       int           `json:"held,omitempty"`
	Slow       *sim.Slow     `json:"slow,omitempty"`
	Breaks     bool          `json:"breaks,omitempty"`
	EndsSprint bool          `json:"ends_sprint,omitempty"`
	Move       *moveStart    `json:"move,omitempty"`
	Fighting   bool          `json:"fighting,omitempty"`
}

// moveStart is a skill's move as the world starts it at an input after its press: a dash or leap over Steps sub-steps, or a blink.
type moveStart struct {
	Kind    string  `json:"kind"`
	UX      float64 `json:"ux"`
	UZ      float64 `json:"uz"`
	Length  float64 `json:"length"`
	Steps   int     `json:"steps,omitempty"`
	Stop    bool    `json:"stop,omitempty"`
	Through bool    `json:"through,omitempty"`
}

// mobs is how the move meets mobs' bodies.
func (m moveStart) mobs() sim.Mobs {
	switch {
	case m.Through:
		return sim.ThroughMobs
	case m.Stop:
		return sim.StopAtMob
	}
	return sim.SlideOnMobs
}

// The scenarios of docs/tech/3d-world.md §5, each a file.
var requiredVectors = []string{
	"walk-north", "walk-north-east", "walk-east", "walk-south-east", "walk-south", "walk-south-west", "walk-west", "walk-north-west",
	"walk-opposite-keys", "walk-fast",
	"slide-box", "box-corner", "slide-box-rotated", "box-corner-rotated", "slide-cylinder", "round-cylinder", "round-cylinder-east", "round-cylinder-top", "round-cylinder-top-north", "round-cylinder-top-west", "round-cylinder-top-south", "round-cylinder-top-sliver", "round-cylinder-top-inside", "cylinder-middle",
	"ramp-up-axis", "ramp-up-diagonal", "cliff-stop-axis", "cliff-stop-diagonal", "cliff-straddles-cells",
	"jump-onto-cliff", "jump-short-of-cliff", "step-off-cliff", "land-on-steep-cell", "walk-down-slope",
	"jump", "jump-onto-box", "jump-short-of-box", "jump-onto-stump", "jump-across-step", "step-onto-box",
	"fall-off-mesa", "fall-clamp",
	"mob-walk-into", "mob-walk-out", "mob-walk-along", "mob-jump-over",
	"map-edge",
	"dash-open", "dash-into-box", "dash-along-box", "dash-up-cliff", "dash-off-mesa", "dash-at-mob", "dash-along-mob", "dash-ends-mid-input",
	"leap-open", "leap-into-box", "leap-along-box", "leap-up-cliff", "leap-off-mesa", "leap-at-mob",
	"blink-open", "blink-into-box", "blink-along-box", "blink-up-cliff", "blink-off-mesa", "blink-at-edge", "blink-through-mob", "blink-behind-mob", "blink-into-mob",
	"held-keys", "held-mid-air", "held-mid-dash", "slow-walk", "slow-walk-fast", "slow-dash", "held-and-slow-run-out",
	"leap-breaks-free", "blink-breaks-free",
	"leap-through-mob", "leap-through-into-mob", "leap-through-touching-mobs", "leap-through-to-gap", "leap-through-into-box", "leap-through-never-clear", "leap-through-round-trunk",
	"sprint-after-walking", "sprint-jump-keeps", "sprint-fast", "sprint-and-combat", "sprint-ends-on-press", "sprint-two-presses",
	"walk-never-sprints", "walk-switch-while-walking", "walk-switch-standing", "walk-switch-in-combat", "walk-held-keeps-mode",
	"region-forest-sprint", "region-wall-slide", "region-steps-on-corners",
	"deep-walk-stops-at-bank", "deep-jump-stops-in-air", "deep-dash-stops-at-bank", "deep-leap-stops-at-bank", "deep-blink-stops-short",
	"deep-ford-crosses", "deep-slides-into-ford", "deep-walks-out", "deep-corners-west-bank", "deep-corners-east-bank",
	"bake-tiny-seam-sprint", "bake-tiny-house-seam", "bake-tiny-river-bank", "bake-tiny-ford", "bake-tiny-blink-seam",
	"bake-steep-cliff", "bake-steep-cliff-leap", "bake-steep-ring", "bake-steep-off-plateau", "bake-steep-ridge",
}

// params are §3's starting values in metres.
var params = sim.Params{
	Dt:           1.0 / 60,
	WalkSpeed:    5,
	JumpVelocity: 12.34,
	Gravity:      38,
	MaxFallSpeed: 22.5,
	Radius:       0.375,
	Height:       1.75,
	Step:         0.3,
	MaxSlope:     0.7,
	Snap:         0.5,
}

// sprinting is params with movement.yaml's sprint (large-regions.md section 7): 2 s of walking out of combat, then 1.6 times as fast.
var sprinting = func() sim.Params {
	p := params
	p.Sprint = sim.Sprint{After: 2, Speed: 1.6}
	return p
}()

var (
	east, west, north, south = sim.Input{Right: true}, sim.Input{Left: true}, sim.Input{Up: true}, sim.Input{Down: true}
	northEast, southEast     = sim.Input{Up: true, Right: true}, sim.Input{Down: true, Right: true}
	jumpEast, still          = sim.Input{Right: true, Jump: true}, sim.Input{}
	walkEast, walkStill      = sim.Input{Right: true, Walk: true}, sim.Input{Walk: true} // walk mode's
)

// ground is a sizeX by sizeZ map whose terrain samples, cell apart, are h(x, z).
func ground(sizeX, sizeZ, cell float64, h func(x, z float64) float64) sim.Geometry {
	nx, nz := int(sizeX/cell)+1, int(sizeZ/cell)+1
	heights := make([]float64, 0, nx*nz)
	for iz := range nz {
		for ix := range nx {
			heights = append(heights, h(float64(ix)*cell, float64(iz)*cell))
		}
	}
	return sim.Geometry{
		SizeX: sizeX, SizeZ: sizeZ,
		Terrain:   sim.Terrain{Cell: cell, NX: nx, NZ: nz, Heights: heights},
		Boxes:     []sim.Box{},
		Cylinders: []sim.Cylinder{},
	}
}

func flat() sim.Geometry { return ground(12, 12, 12, func(_, _ float64) float64 { return 0 }) }

// long is flat ground 80 m east to west, room for a sprint.
func long() sim.Geometry { return ground(80, 12, 4, func(_, _ float64) float64 { return 0 }) }

// rising is ground 12 by 6 that is low west of from, rises to top at to, and stays there.
func rising(from, to, top float64) sim.Geometry {
	return ground(12, 6, 0.5, func(x, _ float64) float64 {
		switch {
		case x <= from:
			return 0
		case x >= to:
			return top
		}
		return top * (x - from) / (to - from)
	})
}

func withBoxes(g sim.Geometry, boxes ...sim.Box) sim.Geometry { g.Boxes = boxes; return g }

func standing(x, y, z float64) sim.State {
	return sim.State{X: x, Y: y, Z: z, OnGround: true, HZ: 1}
}

func runs(rs ...any) []inputRun {
	var out []inputRun
	for i := 0; i < len(rs); i += 2 {
		out = append(out, inputRun{Input: rs[i].(sim.Input), Repeat: rs[i+1].(int)})
	}
	return out
}

// rotated is a box at (8, 6), 3 by 2, turned so its long side runs along (0.6, 0.8).
var rotated = sim.Box{X: 8, Z: 6, HalfW: 1.5, HalfD: 1, UX: 0.6, UZ: 0.8, Top: 2}

// trunk is flat ground with a tree trunk at (8, 6), 1 m across the radius.
func trunk() sim.Geometry {
	g := flat()
	g.Cylinders = []sim.Cylinder{{X: 8, Z: 6, Radius: 1, Top: 7}}
	return g
}

// mob is a slime's circle at (8, 6), 1 m tall.
var mob = []sim.Blocker{{X: 8, Z: 6, Radius: 0.5, Bottom: 0, Top: 1}}

// tall is a box at (8, 6) 2 by 6, 2.5 m tall, its west face at x 7.
var tall = sim.Box{X: 8, Z: 6, HalfW: 1, HalfD: 3, UX: 1, Top: 2.5}

// dash, leap, through and blink start a skill's move along the unit (ux, uz): a dash or leap of 12 or 18 sub-steps, 200 or 300 ms, stopping at a mob's body; through, a break-free's leap, passing mobs.
func dash(ux, uz, length float64) *moveStart {
	return &moveStart{Kind: "dash", UX: ux, UZ: uz, Length: length, Steps: 12, Stop: true}
}
func leap(ux, uz, length float64) *moveStart {
	return &moveStart{Kind: "leap", UX: ux, UZ: uz, Length: length, Steps: 18, Stop: true}
}
func through(ux, uz, length float64) *moveStart {
	return &moveStart{Kind: "leap", UX: ux, UZ: uz, Length: length, Steps: 18, Through: true}
}
func blink(ux, uz, length float64) *moveStart {
	return &moveStart{Kind: "blink", UX: ux, UZ: uz, Length: length}
}

// moving is a run of n sub-steps holding in, the move starting before its first, then 20 more with no keys.
func moving(m *moveStart, in sim.Input, n int, blockers ...sim.Blocker) []inputRun {
	return []inputRun{{Input: in, Repeat: n, Blockers: blockers, Move: m}, {Input: still, Repeat: 20}}
}

// moveScenarios are skill-roster.md §3's: each move along open ground, into a wall, along a wall at an angle, up a steep cell, off a mesa and at a mob.
func moveScenarios() []vector {
	return []vector{
		{Name: "dash-open", Notes: "A 5 m dash along (0.6, 0.8) over 12 sub-steps on flat ground: 25 m/s in place of the walk, then standing.",
			Geometry: flat(), Start: standing(3, 0, 2), Inputs: moving(dash(0.6, 0.8, 5), still, 15)},
		{Name: "dash-into-box", Notes: "A 6 m dash east into a box's west face: it stops at contact and the move runs out against it.",
			Geometry: withBoxes(flat(), tall), Start: standing(3, 0, 6), Inputs: moving(dash(1, 0, 6), still, 15)},
		{Name: "dash-along-box", Notes: "A 5 m dash along (0.8, 0.6) into the same face: x stops at contact and z slides on along it.",
			Geometry: withBoxes(flat(), tall), Start: standing(4, 0, 3), Inputs: moving(dash(0.8, 0.6, 5), still, 15)},
		{Name: "dash-up-cliff", Notes: "A dash east at a 0.4 m cliff, steep at 0.8 per metre: on the ground its cell is a wall.",
			Geometry: rising(6, 6.5, 0.4), Start: standing(3, 0, 3), Inputs: moving(dash(1, 0, 5), still, 15)},
		{Name: "dash-off-mesa", Notes: "A dash west off a 1.5 m mesa: it runs on in the air and the body falls as it ends.",
			Geometry: rising(6, 6.5, 1.5), Start: standing(9, 1.5, 3), Inputs: moving(dash(-1, 0, 5), still, 15)},
		{Name: "dash-at-mob", Notes: "A 6 m dash east at a mob: it ends at the mob's body, the walk back from the next sub-step.",
			Geometry: flat(), Start: standing(3, 0, 6), Inputs: moving(dash(1, 0, 6), still, 15, mob...)},
		{Name: "dash-along-mob", Notes: "A dash that doesn't stop, along (0.8, 0.6) at a mob: x stops at its body and z slides the body round it.",
			Geometry: flat(), Start: standing(4, 0, 4.2), Inputs: moving(&moveStart{Kind: "dash", UX: 0.8, UZ: 0.6, Length: 6, Steps: 12}, still, 15, mob...)},
		{Name: "dash-ends-mid-input", Notes: "A dash of 5 sub-steps while east is held: the walk takes over from the sixth, partway through an input of three.",
			Geometry: flat(), Start: standing(3, 0, 6), Inputs: moving(&moveStart{Kind: "dash", UX: 0, UZ: 1, Length: 2, Steps: 5, Stop: true}, east, 9)},
		{Name: "leap-open", Notes: "A 4 m leap east over 18 sub-steps: the hop comes down as the move ends, a rounding above the ground, and lands the sub-step after.",
			Geometry: flat(), Start: standing(3, 0, 6), Inputs: moving(leap(1, 0, 4), still, 20)},
		{Name: "leap-into-box", Notes: "A 6 m leap east into a 2.5 m box: x stops at its face while the hop rises and falls.",
			Geometry: withBoxes(flat(), tall), Start: standing(3, 0, 6), Inputs: moving(leap(1, 0, 6), still, 20)},
		{Name: "leap-along-box", Notes: "A 5 m leap along (0.8, 0.6) into the box: z slides along its face in the air.",
			Geometry: withBoxes(flat(), tall), Start: standing(4, 0, 3), Inputs: moving(leap(0.8, 0.6, 5), still, 20)},
		{Name: "leap-up-cliff", Notes: "A leap east at the 0.4 m cliff a dash stops at: the feet clear its foot but drop below its top, so the body lands on its face, and the rest of the move carries it up onto the top, which isn't steep.",
			Geometry: rising(6, 6.5, 0.4), Start: standing(3, 0, 3), Inputs: moving(leap(1, 0, 5), still, 20)},
		{Name: "leap-off-mesa", Notes: "A leap west off the 1.5 m mesa: it hops, runs out in the air and falls to the foot.",
			Geometry: rising(6, 6.5, 1.5), Start: standing(9, 1.5, 3), Inputs: moving(leap(-1, 0, 5), still, 25)},
		{Name: "leap-at-mob", Notes: "A leap east at a 1 m mob: the low hop doesn't clear it, so it ends at its body and falls.",
			Geometry: flat(), Start: standing(3, 0, 6), Inputs: moving(leap(1, 0, 6), still, 20, mob...)},
		{Name: "blink-open", Notes: "An 8 m blink along (0.6, 0.8) on flat ground: there at the start of the input's first sub-step.",
			Geometry: flat(), Start: standing(2, 0, 2), Inputs: moving(blink(0.6, 0.8, 8), still, 3)},
		{Name: "blink-into-box", Notes: "An 8 m blink east at a box's west face: the last point tested short of it, 6.5, 0.125 m off contact.",
			Geometry: withBoxes(flat(), tall), Start: standing(3, 0, 6), Inputs: moving(blink(1, 0, 8), still, 3)},
		{Name: "blink-along-box", Notes: "A blink along (0.8, 0.6) into the box: it stops at the first step whose x would enter it, no sliding.",
			Geometry: withBoxes(flat(), tall), Start: standing(4, 0, 3), Inputs: moving(blink(0.8, 0.6, 5), still, 3)},
		{Name: "blink-up-cliff", Notes: "A blink east at the 0.4 m cliff: it stops at the last point before the steep cell.",
			Geometry: rising(6, 6.5, 0.4), Start: standing(3, 0, 3), Inputs: moving(blink(1, 0, 5), still, 3)},
		{Name: "blink-off-mesa", Notes: "A blink west off the 1.5 m mesa: it ends in the air at the mesa's height and falls.",
			Geometry: rising(6, 6.5, 1.5), Start: standing(9, 1.5, 3), Inputs: moving(blink(-1, 0, 6), still, 15)},
		{Name: "blink-at-edge", Notes: "A blink east toward the map's edge: the last point tested short of it, 11.5.",
			Geometry: flat(), Start: standing(8, 0, 6), Inputs: moving(blink(1, 0, 8), still, 3)},
		{Name: "blink-through-mob", Notes: "A 6 m blink east through a mob: mobs don't stop a blink, which ends beyond it.",
			Geometry: flat(), Start: standing(4, 0, 6), Inputs: moving(blink(1, 0, 6), still, 3, mob...)},
		{Name: "blink-behind-mob", Notes: "Shadow Step: a blink east to the mob's far contact ring, 4 m to its centre plus both radii, ends just behind it.",
			Geometry: flat(), Start: standing(4, 0, 6), Inputs: moving(blink(1, 0, 4.875), still, 3, mob...)},
		{Name: "blink-into-mob", Notes: "A blink east whose length ends inside a mob: it ends at the farthest point tested outside its body, 7.",
			Geometry: flat(), Start: standing(4, 0, 6), Inputs: moving(blink(1, 0, 4.25), still, 3, mob...)},
	}
}

// slowed is the slow status of data/statuses.yaml for n sub-steps.
func slowed(n int) *sim.Slow { return &sim.Slow{Left: n, Factor: 0.6} }

// statusScenarios are statuses.md §4's: held with keys and a jump pressed, held mid-air and mid-dash, slowed walking and dashing, and both running out mid-press; and §13's break-frees, which end the hold the world set that input before their move starts.
func statusScenarios() []vector {
	return []vector{
		{Name: "held-keys", Notes: "Held 30 sub-steps with east and jump pressed: no walk, no jump, the heading kept; then it walks and jumps.",
			Geometry: flat(), Start: standing(6, 0, 6), Inputs: []inputRun{{Input: jumpEast, Repeat: 40, Held: 30}}},
		{Name: "held-mid-air", Notes: "A leap east, held 6 sub-steps in: its move ends and the body falls straight, then walks east once the hold runs out.",
			Geometry: flat(), Start: standing(3, 0, 6), Inputs: []inputRun{{Input: still, Repeat: 6, Move: leap(1, 0, 4)}, {Input: east, Repeat: 40, Held: 30}}},
		{Name: "held-mid-dash", Notes: "A dash east, held 4 sub-steps in: it stops where it is.",
			Geometry: flat(), Start: standing(3, 0, 6), Inputs: []inputRun{{Input: still, Repeat: 4, Move: dash(1, 0, 5)}, {Input: east, Repeat: 20, Held: 12}}},
		{Name: "slow-walk", Notes: "Walking north-east slowed to 0.6 for 30 sub-steps, then at full speed.",
			Geometry: flat(), Start: standing(3, 0, 3), Inputs: []inputRun{{Input: northEast, Repeat: 50, Slow: slowed(30)}}},
		{Name: "slow-walk-fast", Notes: "Walking east at a move speed factor of 1.3, slowed to 0.6 of that for 30 sub-steps.", SpeedFactor: 1.3,
			Geometry: flat(), Start: standing(2, 0, 6), Inputs: []inputRun{{Input: east, Repeat: 50, Slow: slowed(30)}}},
		{Name: "slow-dash", Notes: "A dash east while slowed keeps its speed; the walk after it is slowed until the slow runs out.",
			Geometry: flat(), Start: standing(2, 0, 6), Inputs: []inputRun{{Input: east, Repeat: 12, Slow: slowed(30), Move: dash(1, 0, 5)}, {Input: east, Repeat: 30}}},
		{Name: "held-and-slow-run-out", Notes: "Walking east, then held 10 and slowed 30 sub-steps mid-press: still, then slowed, then at full speed.",
			Geometry: flat(), Start: standing(2, 0, 6), Inputs: []inputRun{{Input: east, Repeat: 10}, {Input: east, Repeat: 50, Held: 10, Slow: slowed(30)}}},
		{Name: "leap-breaks-free", Notes: "Held with east pressed; 9 sub-steps in, held 21 more that input, a break-free (Vanish, Recoil) ends the hold and leaps 4 m back west over 18 sub-steps; then it walks east.",
			Geometry: flat(), Start: sim.State{X: 8, Z: 6, OnGround: true, HX: 1}, Inputs: []inputRun{{Input: east, Repeat: 9, Held: 30}, {Input: still, Repeat: 24, Held: 21, Breaks: true, Move: &moveStart{Kind: "leap", UX: -1, Length: 4, Steps: 18}}, {Input: east, Repeat: 12}}},
		{Name: "blink-breaks-free", Notes: "Held with north pressed; 6 sub-steps in, held 24 more that input, a break-free (Arcane Release) ends the hold and blinks 4 m north at once, walking on north.",
			Geometry: flat(), Start: standing(6, 0, 3), Inputs: []inputRun{{Input: north, Repeat: 6, Held: 30}, {Input: north, Repeat: 15, Held: 24, Breaks: true, Move: blink(0, 1, 4)}}},
		{Name: "leap-through-mob", Notes: "A break-free's 4 m leap west, the mob touching the body's back: it passes through the mob and lands 4 m on, at 4.875.",
			Geometry: flat(), Start: standing(8.875, 0, 6), Inputs: moving(through(-1, 0, 4), still, 20, mob...)},
		{Name: "leap-through-into-mob", Notes: "A through leap east whose 5 m ends at the mob's centre: run out inside its body, it ends at the farthest sub-step it reached clear of it, 6.889, short of the contact ring at 7.125.",
			Geometry: flat(), Start: standing(3, 0, 6), Inputs: moving(through(1, 0, 5), still, 20, mob...)},
		{Name: "leap-through-touching-mobs", Notes: "A through leap east ending where two touching mobs meet, at 7: no room for a body between them, so it ends before the first, the farthest sub-step clear of both.",
			Geometry: flat(), Start: standing(3, 0, 6), Inputs: moving(through(1, 0, 4), still, 20, sim.Blocker{X: 6.5, Z: 6, Radius: 0.5, Top: 1}, sim.Blocker{X: 7.5, Z: 6, Radius: 0.5, Top: 1})},
		{Name: "leap-through-to-gap", Notes: "A through leap east past one mob, ending inside a second 3.5 m on: it ends in the gap between them, 7.978, the farthest sub-step clear of both.",
			Geometry: flat(), Start: standing(3, 0, 6), Inputs: moving(through(1, 0, 5.6), still, 20, sim.Blocker{X: 5.5, Z: 6, Radius: 0.5, Top: 1}, sim.Blocker{X: 9, Z: 6, Radius: 0.5, Top: 1})},
		{Name: "leap-through-into-box", Notes: "A through leap east past a mob into the 2.5 m box: mobs don't stop it, solids still do, at the box's face.",
			Geometry: withBoxes(flat(), tall), Start: standing(3, 0, 6), Inputs: moving(through(1, 0, 6), still, 20, sim.Blocker{X: 5, Z: 6, Radius: 0.5, Top: 1})},
		{Name: "leap-through-never-clear", Notes: "A through leap west from inside the mob's body, a mob having walked into the held player, ending at its centre: it reached no point clear of the body, so it ends where it set off, 8.5, as a blink does.",
			Geometry: flat(), Start: standing(8.5, 0, 6), Inputs: moving(through(-1, 0, 0.5), still, 20, mob...)},
		{Name: "leap-through-round-trunk", Notes: "A through leap east into the trunk just north of its centre: it rounds the trunk north as a walk does, and the mob on that way round doesn't stop it.",
			Geometry: trunk(), Start: standing(4, 0, 6.3), Inputs: moving(through(1, 0, 6), still, 20, sim.Blocker{X: 7.2, Z: 7.4, Radius: 0.5, Top: 1})},
	}
}

// sprintScenarios are large-regions.md section 7's: walking breaks into a sprint after 2 s, a jump keeps it, move speed counts under it, and a fight or a press ends it.
func sprintScenarios() []vector {
	vs := []vector{
		{Name: "sprint-after-walking", Notes: "Walking east out of combat: 120 sub-steps, 2 s, at the walk, then 1.6 times as fast; letting go ends it, and walking on counts again from 0.",
			Geometry: long(), Start: standing(2, 0, 6), Inputs: runs(east, 130, still, 2, east, 5)},
		{Name: "sprint-jump-keeps", Notes: "A jump 110 sub-steps into a walk east: the count runs on through the air, the sprint starting at 121 before it lands.",
			Geometry: long(), Start: standing(2, 0, 6), Inputs: runs(east, 110, jumpEast, 1, east, 40)},
		{Name: "sprint-fast", Notes: "Walking east at a move speed factor of 1.3: the sprint multiplies it, after the same 2 s.", SpeedFactor: 1.3,
			Geometry: long(), Start: standing(2, 0, 6), Inputs: runs(east, 125)},
		{Name: "sprint-and-combat", Notes: "Sprinting east, then 10 sub-steps in combat, which the world steps with no sprint: back to the walk at once, and out of combat the count starts again from 0.",
			Geometry: long(), Start: standing(2, 0, 6), Inputs: []inputRun{{Input: east, Repeat: 125}, {Input: east, Repeat: 10, Fighting: true}, {Input: east, Repeat: 125}}},
		{Name: "sprint-ends-on-press", Notes: "Sprinting east, an attack or skill pressed: from the input after the press, back to the walk, sprinting again 2 s on.",
			Geometry: long(), Start: standing(2, 0, 6), Inputs: []inputRun{{Input: east, Repeat: 125}, {Input: east, Repeat: 125, EndsSprint: true}}},
		{Name: "sprint-two-presses", Notes: "Sprinting east, two presses two inputs apart: each ends the count from the input after it, so the sprint comes back 2 s after the second.",
			Geometry: long(), Start: standing(2, 0, 6), Inputs: []inputRun{{Input: east, Repeat: 125}, {Input: east, Repeat: 6, EndsSprint: true}, {Input: east, Repeat: 125, EndsSprint: true}}},
	}
	for i := range vs {
		vs[i].Params = sprinting
	}
	return vs
}

// walkScenarios are large-regions.md section 7's walk mode: it never sprints, and switched back while walking out of combat it sprints from the next sub-step, else after the usual 2 s; a hold keeps the mode.
func walkScenarios() []vector {
	vs := []vector{
		{Name: "walk-never-sprints", Notes: "Sprinting east, then walk mode: back to the walk on the switch's own sub-step, and never sprinting through 2 s of it.",
			Geometry: long(), Start: standing(2, 0, 6), Inputs: runs(east, 125, walkEast, 130)},
		{Name: "walk-switch-while-walking", Notes: "Walking east in walk mode, then back to sprint mode while walking: the switch's sub-step walks, its count straight at 120, and the next sprints.",
			Geometry: long(), Start: standing(2, 0, 6), Inputs: runs(walkEast, 60, east, 5)},
		{Name: "walk-switch-standing", Notes: "Walking east in walk mode, stopping, switching back while standing, then walking on: the count starts from 0, sprinting 2 s on.",
			Geometry: long(), Start: standing(2, 0, 6), Inputs: runs(walkEast, 30, walkStill, 2, still, 2, east, 125)},
		{Name: "walk-switch-in-combat", Notes: "Walking east in walk mode, switched back in combat, which steps with no sprint: out of combat the count starts from 0, so a switch in a fight stores no sprint.",
			Geometry: long(), Start: standing(2, 0, 6), Inputs: []inputRun{{Input: walkEast, Repeat: 30}, {Input: east, Repeat: 10, Fighting: true}, {Input: east, Repeat: 125}}},
		{Name: "walk-held-keeps-mode", Notes: "Walking east in walk mode, held 10 sub-steps: the hold empties the keys but keeps the mode, so switching back as it ends, while walking, sprints from the next sub-step.",
			Geometry: long(), Start: standing(2, 0, 6), Inputs: []inputRun{{Input: walkEast, Repeat: 30}, {Input: walkEast, Repeat: 10, Held: 10}, {Input: east, Repeat: 5}}},
	}
	for i := range vs {
		vs[i].Params = sprinting
	}
	return vs
}

// regionScenarios walk bodies over many 8 m buckets among solids lying on and across their lines, as a region's do (large-regions.md section 5).
func regionScenarios() []vector {
	level := func(_, _ float64) float64 { return 0 }
	forest := ground(120, 120, 40, level) // trees in rows 7 m apart, so they fall on and across the bucket lines
	for ix := 1; ix < 17; ix++ {
		for iz := 1; iz < 17; iz++ {
			forest.Cylinders = append(forest.Cylinders, sim.Cylinder{X: float64(ix)*7 + 0.3*float64(iz%3), Z: float64(iz)*7 - 0.2*float64(ix%4), Radius: 0.5 + 0.1*float64((ix+iz)%4), Top: 4})
		}
	}
	wall := ground(120, 120, 40, level) // 80 m long, turned 20°, over a dozen buckets
	wall.Boxes = []sim.Box{{X: 60, Z: 60, HalfW: 40, HalfD: 0.4, UX: math.Cos(20 * math.Pi / 180), UZ: math.Sin(20 * math.Pi / 180), Top: 3}}
	steps := ground(64, 32, 4, level) // low steps centred on bucket corners, each over four buckets
	for k := 1; k < 7; k++ {
		steps.Boxes = append(steps.Boxes, sim.Box{X: 8 * float64(k), Z: 16, HalfW: 1, HalfD: 1, UX: 1, Top: 0.2 + 0.04*float64(k%3)})
	}
	return []vector{
		{Name: "region-forest-sprint", Notes: "Walking east 10 s along the north side of a row of trees on and across the bucket lines: into a sprint after 2 s, sliding round each tree it grazes.",
			Params: sprinting, Geometry: forest, Start: standing(1, 0, 7.3), Inputs: runs(east, 600)},
		{Name: "region-wall-slide", Notes: "Walking north into a wall turned 20° and 80 m long, then north-east, sliding along it over three buckets.",
			Params: params, Geometry: wall, Start: standing(40, 0, 45), Inputs: runs(north, 120, northEast, 360)},
		{Name: "region-steps-on-corners", Notes: "Walking east over low steps centred on bucket corners, then a jump and on: each step is read from all four of its buckets.",
			Params: params, Geometry: steps, Start: standing(2, 0, 16), Inputs: runs(east, 240, jumpEast, 1, east, 120)},
	}
}

// river is ground 24 by 12, cell 1, cut north to south by a river: samples x 11 to 13 lie at bed, under deep water, but for a ford at z 5 to 7, dry and level with the banks.
func river(bed float64) sim.Geometry {
	in := func(x, z float64) bool { return x >= 11 && x <= 13 && (z < 5 || z > 7) }
	g := ground(24, 12, 1, func(x, z float64) float64 {
		if in(x, z) {
			return bed
		}
		return 0
	})
	t := &g.Terrain
	t.Barrier = make([]bool, t.NX*t.NZ)
	for iz := range t.NZ {
		for ix := range t.NX {
			t.Barrier[iz*t.NX+ix] = in(float64(ix), float64(iz))
		}
	}
	return g
}

// deepScenarios are large-regions.md decision 9's: no body moves into deep water from out of it, at any height, by any move; a ford crosses, and a body already in it walks out.
func deepScenarios() []vector {
	return []vector{
		{Name: "deep-walk-stops-at-bank", Notes: "Walking east at a river whose bed lies 1.5 m down: the cell holding its first deep sample is a wall though the way is downhill, so the body stops short of x 10.",
			Geometry: river(-1.5), Start: standing(6, 0, 3), Inputs: runs(east, 90)},
		{Name: "deep-jump-stops-in-air", Notes: "Jumping east from the bank: the deep cell stops the body in the air at any height, and it lands back on the bank.",
			Geometry: river(-1.5), Start: standing(8.8, 0, 3), Inputs: runs(jumpEast, 1, east, 60)},
		{Name: "deep-dash-stops-at-bank", Notes: "An 8 m dash east across the river: it stops at the deep cell and the move runs out against it.",
			Geometry: river(-1.5), Start: standing(7, 0, 3), Inputs: moving(dash(1, 0, 8), still, 15)},
		{Name: "deep-leap-stops-at-bank", Notes: "An 8 m leap east across the river: the hop is stopped in the air at the deep cell and falls back on the bank.",
			Geometry: river(-1.5), Start: standing(7, 0, 3), Inputs: moving(leap(1, 0, 8), still, 25)},
		{Name: "deep-blink-stops-short", Notes: "An 8 m blink east across the river: it ends at the last point tested short of the deep cell, 9.75.",
			Geometry: river(-1.5), Start: standing(7, 0, 3), Inputs: moving(blink(1, 0, 8), still, 3)},
		{Name: "deep-ford-crosses", Notes: "Walking east along the ford: it's dry, so the body crosses the river to the far bank.",
			Geometry: river(-1.5), Start: standing(6, 0, 6), Inputs: runs(east, 180)},
		{Name: "deep-slides-into-ford", Notes: "Walking north-east at the bank: x stops at the deep cell while z slides the body north into the ford; across it the ford's north side holds z while x goes on, and past the river it walks north-east again.",
			Geometry: river(-1.5), Start: standing(8, 0, 2), Inputs: runs(northEast, 240)},
		{Name: "deep-corners-west-bank", Notes: "Walking east just north of the ford, at z 7.5, into a cell deep only at its north-east sample, then south to z 4.5 and east into one deep only at its south-east: each stops the body as a wholly deep cell does.",
			Geometry: river(-1.5), Start: standing(8, 0, 7.5), Inputs: runs(east, 30, south, 36, east, 30)},
		{Name: "deep-corners-east-bank", Notes: "Walking west from the far bank at z 7.5, into a cell deep only at its north-west sample, then south to z 4.5 and west into one deep only at its south-west: each stops the body.",
			Geometry: river(-1.5), Start: standing(16, 0, 7.5), Inputs: runs(west, 30, south, 36, west, 30)},
		{Name: "deep-walks-out", Notes: "A body already in deep water, on level ground: it walks west through the deep cells and out, as only a move into deep water from out of it is stopped.",
			Geometry: river(0), Start: standing(12, 0, 3), Inputs: runs(west, 60)},
	}
}

func scenarios() []vector {
	walk := func(name, way string, in sim.Input) vector {
		return vector{Name: name, Notes: "Walking " + way + " from the middle of flat ground, then letting go: the heading stays.", Geometry: flat(), Start: standing(6, 0, 6), Inputs: runs(in, 30, still, 5)}
	}
	vs := []vector{
		walk("walk-north", "north", north),
		walk("walk-north-east", "north-east, √½ on each axis", northEast),
		walk("walk-east", "east", east),
		walk("walk-south-east", "south-east", southEast),
		walk("walk-south", "south", south),
		walk("walk-south-west", "south-west", sim.Input{Down: true, Left: true}),
		walk("walk-west", "west", west),
		walk("walk-north-west", "north-west", sim.Input{Up: true, Left: true}),
		{Name: "walk-opposite-keys", Notes: "Opposite keys cancel and keep the heading; with a third key held, it alone walks.", Geometry: flat(), Start: sim.State{X: 6, Z: 6, OnGround: true, HX: 1},
			Inputs: runs(sim.Input{Left: true, Right: true}, 10, sim.Input{Up: true, Down: true}, 10, sim.Input{Left: true, Right: true, Up: true, Down: true}, 10,
				sim.Input{Left: true, Right: true, Up: true}, 10, sim.Input{Up: true, Down: true, Right: true}, 10)},
		{Name: "walk-fast", Notes: "Walking north-east at a move speed factor of 1.3.", SpeedFactor: 1.3, Geometry: flat(), Start: standing(4, 0, 4), Inputs: runs(northEast, 30)},
		{Name: "slide-box", Notes: "Walking north-east into a box's west face: x stops at contact and z slides along it, then round its corner.",
			Geometry: withBoxes(flat(), sim.Box{X: 8, Z: 6, HalfW: 1, HalfD: 3, UX: 1, Top: 2}), Start: standing(5, 0, 3), Inputs: runs(northEast, 110)},
		{Name: "box-corner", Notes: "Walking east on a line 0.2 m south of a box's south-west corner stops on the rounded corner, further east than the face stops it; south-east then slides off it.",
			Geometry: withBoxes(flat(), sim.Box{X: 8, Z: 6, HalfW: 1, HalfD: 3, UX: 1, Top: 2}), Start: standing(5, 0, 2.8), Inputs: runs(east, 40, southEast, 20, east, 30)},
		{Name: "slide-box-rotated", Notes: "Walking north-east into the middle of a box turned to (0.6, 0.8): x stops at its slanted face each sub-step and z, moving away from it, slides the body along it and past its corner.",
			Geometry: withBoxes(flat(), rotated), Start: standing(4.5, 0, 4.4), Inputs: runs(northEast, 100)},
		{Name: "box-corner-rotated", Notes: "Walking east just inside the turned box's west corner at (6.3, 5.4) stops on it; south-east then slides off it.",
			Geometry: withBoxes(flat(), rotated), Start: standing(4, 0, 5.2), Inputs: runs(east, 40, southEast, 40)},
		{Name: "slide-cylinder", Notes: "Walking north-east into a tree trunk: the body slides round it.",
			Geometry: func() sim.Geometry {
				g := flat()
				g.Cylinders = []sim.Cylinder{{X: 8, Z: 6, Radius: 1, Top: 7}}
				return g
			}(), Start: standing(5, 0, 4.5), Inputs: runs(northEast, 90)},
		{Name: "round-cylinder", Notes: "Walking north-east at a trunk, meeting it near its middle: both keys' moves stop at its rim, so the body slides round it along the tangent, then walks on.",
			Geometry: trunk(), Start: standing(5, 0, 3.4), Inputs: runs(northEast, 120)},
		{Name: "round-cylinder-east", Notes: "Walking east at a trunk 0.3 m north of its middle: x stops at its rim, and the body slides north round it, slowly at first, then on east.",
			Geometry: trunk(), Start: standing(5, 0, 6.3), Inputs: runs(east, 95)},
		{Name: "round-cylinder-top", Notes: "Walking east 0.9 m north of a trunk's middle: the body slides round it to the rim's northmost point, stops there for the step that would pass it, then walks on east; it once stuck there, its x leg cutting into the trunk.",
			Geometry: trunk(), Start: standing(5, 0, 6.9), Inputs: runs(east, 90)},
		{Name: "round-cylinder-top-north", Notes: "The same walking north 0.9 m east of the trunk's middle, past the rim's eastmost point.",
			Geometry: trunk(), Start: standing(8.9, 0, 2), Inputs: runs(north, 90)},
		{Name: "round-cylinder-top-west", Notes: "The same walking west 0.9 m south of the trunk's middle, past the rim's southmost point.",
			Geometry: trunk(), Start: standing(11, 0, 5.1), Inputs: runs(west, 90)},
		{Name: "round-cylinder-top-south", Notes: "The same walking south 0.9 m west of the trunk's middle, past the rim's westmost point.",
			Geometry: trunk(), Start: standing(7.1, 0, 10), Inputs: runs(south, 90)},
		{Name: "round-cylinder-top-sliver", Notes: "Walking east along z 7.975, 0.975 m north of a 0.6 m trunk's middle as near as a float gets, which leaves the body 1 ulp inside touching: x stops at a 50 nm sliver of it, rounding stands the body on the rim's northmost point 1 µm clear, and it walks on; it once stuck there.",
			Geometry: func() sim.Geometry {
				g := flat()
				g.Cylinders = []sim.Cylinder{{X: 8.3, Z: 7, Radius: 0.6, Top: 4}}
				return g
			}(), Start: standing(5, 0, 7.975), Inputs: runs(east, 60)},
		{Name: "round-cylinder-top-inside", Notes: "Walking east from 1.4 cm inside a trunk's rim, just west of its northmost point: rounding over that point stands the body clear of the trunk, 1 µm past touching, and it walks on; at its own depth it would stay inside and stick.",
			Geometry: trunk(), Start: standing(7.95, 0, 7.36), Inputs: runs(east, 40)},
		{Name: "cylinder-middle", Notes: "Walking east straight at a trunk's middle: nothing of the move runs along its tangent, so the body stays at its rim.",
			Geometry: trunk(), Start: standing(5, 0, 6), Inputs: runs(east, 60)},
		{Name: "ramp-up-axis", Notes: "Walking east up a 0.5 rise per metre, under max_slope, onto the flat top 2 m up.", Geometry: rising(4, 8, 2), Start: standing(2, 0, 3), Inputs: runs(east, 110)},
		{Name: "ramp-up-diagonal", Notes: "Walking north-east up the same ramp.", Geometry: rising(4, 8, 2), Start: standing(2, 0, 0.5), Inputs: runs(northEast, 110)},
		{Name: "cliff-stop-axis", Notes: "Walking east into a 1.0 rise per metre, over max_slope: the steep cell is a wall.", Geometry: rising(6, 7, 1), Start: standing(4, 0, 3), Inputs: runs(east, 50)},
		{Name: "cliff-stop-diagonal", Notes: "Walking north-east into the same cliff: x stops and z walks on along its foot.", Geometry: rising(6, 7, 1), Start: standing(4, 0, 1), Inputs: runs(northEast, 60)},
		{Name: "cliff-straddles-cells", Notes: "A 0.8 m cliff between 6.25 and 6.75 is spread by the samples across two cells, each rising 0.4 in 0.5 m: both steep.",
			Geometry: rising(6.25, 6.75, 0.8), Start: standing(4, 0, 3), Inputs: runs(east, 50)},
		{Name: "jump-onto-cliff", Notes: "A jump east from the foot of a 1.2 m cliff: in the air its steep cell is a wall until the feet reach its top, then the body moves in and lands on top.",
			Geometry: rising(6, 6.5, 1.2), Start: standing(5.9, 0, 3), Inputs: runs(jumpEast, 1, east, 40)},
		{Name: "jump-short-of-cliff", Notes: "The same jump at a 2.5 m cliff: the feet never reach its top, so the body never enters its steep cell and comes down at its foot.",
			Geometry: rising(6, 6.5, 2.5), Start: standing(5.9, 0, 3), Inputs: runs(jumpEast, 1, east, 40)},
		{Name: "step-off-cliff", Notes: "Walking west off the top of a 1.2 m cliff: into its steep cell the body doesn't snap down but falls, and lands at its foot.",
			Geometry: rising(6, 6.5, 1.2), Start: standing(7.5, 1.2, 3), Inputs: runs(west, 40)},
		{Name: "land-on-steep-cell", Notes: "Dropped onto the face of a 1.2 m cliff, a body with no keys held lands and stands still.",
			Geometry: rising(6, 6.5, 1.2), Start: sim.State{X: 6.25, Y: 2.5, Z: 3, HZ: 1}, Inputs: runs(still, 40)},
		{Name: "walk-down-slope", Notes: "Walking west down the ramp: the ground falls under 0.5 m a sub-step, so the body follows it and stays on the ground.", Geometry: rising(4, 8, 2), Start: standing(10, 2, 3), Inputs: runs(west, 110)},
		{Name: "jump", Notes: "A standing jump on flat ground.", Geometry: flat(), Start: standing(6, 0, 6), Inputs: runs(sim.Input{Jump: true}, 1, still, 45)},
		{Name: "jump-onto-box", Notes: "A jump east onto a box 1.2 m tall, higher than a step: blocked below its top plus a step, landed on above.",
			Geometry: withBoxes(flat(), sim.Box{X: 8, Z: 6, HalfW: 1, HalfD: 1, UX: 1, Top: 1.2}), Start: standing(5, 0, 6), Inputs: runs(east, 8, jumpEast, 1, east, 30)},
		{Name: "jump-short-of-box", Notes: "The same jump at a box 2.5 m tall: the feet never rise above its top less a step.",
			Geometry: withBoxes(flat(), sim.Box{X: 8, Z: 6, HalfW: 1, HalfD: 1, UX: 1, Top: 2.5}), Start: standing(5, 0, 6), Inputs: runs(east, 8, jumpEast, 1, east, 45)},
		{Name: "jump-onto-stump", Notes: "A jump east onto a stump, a cylinder 1 m tall: blocked below its top plus a step, then stood on once the feet's centre is over it.",
			Geometry: func() sim.Geometry {
				g := flat()
				g.Cylinders = []sim.Cylinder{{X: 8, Z: 6, Radius: 0.6, Top: 1}}
				return g
			}(), Start: standing(6, 0, 6), Inputs: runs(jumpEast, 1, east, 25, still, 10)},
		{Name: "jump-across-step", Notes: "Jumping as the body walks onto a box 0.25 m tall: rising past its lip lifts the feet onto it without ending the jump, and it lands on top.",
			Geometry: withBoxes(flat(), sim.Box{X: 8, Z: 6, HalfW: 2.5, HalfD: 1, UX: 1, Top: 0.25}), Start: standing(5.45, 0, 6), Inputs: runs(jumpEast, 1, east, 40)},
		{Name: "step-onto-box", Notes: "A box 0.25 m tall, within a step: walked onto once the feet's centre is over it, and walked off the far side.",
			Geometry: withBoxes(flat(), sim.Box{X: 8, Z: 6, HalfW: 1, HalfD: 1, UX: 1, Top: 0.25}), Start: standing(5, 0, 6), Inputs: runs(east, 80)},
		{Name: "fall-off-mesa", Notes: "Walking west off a mesa 1.5 m high: the ground falls more than snap, so the body falls and lands.", Geometry: rising(6, 6.5, 1.5), Start: standing(9, 1.5, 3), Inputs: runs(west, 60)},
		{Name: "fall-clamp", Notes: "Falling 30 m: the speed stops growing at max_fall_speed, then the body lands.", Geometry: flat(), Start: sim.State{X: 6, Y: 30, Z: 6, HZ: 1}, Inputs: runs(still, 110)},
		{Name: "mob-walk-into", Notes: "Walking east into a mob's circle stops at contact.", Geometry: flat(), Start: standing(5, 0, 6), Inputs: []inputRun{{Input: east, Repeat: 50, Blockers: mob}}},
		{Name: "mob-walk-out", Notes: "A body overlapping a mob can't move closer, east, and walks out, west.", Geometry: flat(), Start: standing(7.6, 0, 6),
			Inputs: []inputRun{{Input: east, Repeat: 10, Blockers: mob}, {Input: west, Repeat: 20}}},
		{Name: "mob-walk-along", Notes: "Walking north-east at a mob, meeting it north of west: x stops at contact while z, moving away, slides the body round it.", Geometry: flat(), Start: standing(5, 0, 4.05), Inputs: []inputRun{{Input: northEast, Repeat: 70, Blockers: mob}}},
		{Name: "mob-jump-over", Notes: "A jump clears a mob 1 m tall, passes over it and lands beyond.", Geometry: flat(), Start: standing(5.5, 0, 6), Inputs: []inputRun{{Input: east, Repeat: 6, Blockers: mob}, {Input: jumpEast, Repeat: 1}, {Input: east, Repeat: 50}}},
		{Name: "map-edge", Notes: "The map's west edge stops the body at its radius; south-west then slides it into the corner.", Geometry: flat(), Start: standing(1, 0, 1.5), Inputs: runs(west, 15, sim.Input{Down: true, Left: true}, 30)},
	}
	vs = append(vs, moveScenarios()...)
	vs = append(vs, statusScenarios()...)
	vs = append(vs, deepScenarios()...)
	for i := range vs {
		vs[i].Params = params
	}
	vs = append(vs, sprintScenarios()...)
	vs = append(vs, walkScenarios()...)
	vs = append(vs, regionScenarios()...)
	return append(vs, bakeScenarios()...)
}

// baked is test plan name generated as the server generates a region, the ground of the bake the web client keeps of it in web/src/testing/regions.
func baked(name string) sim.Geometry {
	read := func(path string) []byte {
		raw, err := os.ReadFile(path)
		if err != nil {
			panic(err)
		}
		return raw
	}
	p, err := regiongen.Decode(read(filepath.Join("../../../cmd/regionbake/testdata", name+".yaml")))
	if err != nil {
		panic(err)
	}
	k, err := regiongen.DecodeKit(read("../../../data/kit.yaml"))
	if err != nil {
		panic(err)
	}
	r, err := regiongen.Generate(p, k, params)
	if err != nil {
		panic(err)
	}
	return r.Geometry
}

// bakeScenarios walk the test plans' bakes, which the client reads chunk by chunk: its seams, the solids each chunk carries at the server's indices, deep water, and the steepest ground the wide plan has.
func bakeScenarios() []vector {
	tiny, steep := baked("tiny"), baked("steep")
	on := func(g sim.Geometry, x, z float64) sim.State { return standing(x, g.Terrain.Height(x, z), z) }
	return []vector{
		{Name: "bake-tiny-seam-sprint", Notes: "Sprinting east 8 s through the tiny plan's oaks, over the seam between chunks at x 64.",
			Region: "tiny", Params: sprinting, Geometry: tiny, Start: on(tiny, 34, 44), Inputs: runs(east, 480)},
		{Name: "bake-tiny-house-seam", Notes: "Walking north-east into the house over the seam at x 64, which only chunk 0_1 carries: the body slides east along its south face, across the seam, and on north-east past its corner.",
			Region: "tiny", Params: params, Geometry: tiny, Start: on(tiny, 60, 76), Inputs: runs(northEast, 180)},
		{Name: "bake-tiny-river-bank", Notes: "Walking east at the river: its deep water stops the body at the bank, and a jump east stops in the air and falls back.",
			Region: "tiny", Params: params, Geometry: tiny, Start: on(tiny, 86, 100), Inputs: runs(east, 120, jumpEast, 1, east, 60)},
		{Name: "bake-tiny-ford", Notes: "Walking east across the ford at z 30: no deep water stops the body, and the ground round the ford rises gently, so it climbs out on the far bank.",
			Region: "tiny", Params: params, Geometry: tiny, Start: on(tiny, 86, 30), Inputs: runs(east, 240)},
		{Name: "bake-tiny-blink-seam", Notes: "A 10.5 m blink east from chunk 0_0: it stops at x 64, short of an oak just over the seam that only chunk 1_0 carries.",
			Region: "tiny", Params: params, Geometry: tiny, Start: on(tiny, 56, 40), Inputs: moving(blink(1, 0, 10.5), still, 3)},
		{Name: "bake-steep-cliff", Notes: "Walking east into the plateau's cliff 12 m high, then north-east along its foot.",
			Region: "steep", Params: params, Geometry: steep, Start: on(steep, 46, 64), Inputs: runs(east, 120, northEast, 180)},
		{Name: "bake-steep-cliff-leap", Notes: "An 8 m leap east at the plateau's cliff: the hop stops against it and falls back.",
			Region: "steep", Params: params, Geometry: steep, Start: on(steep, 50, 60), Inputs: moving(leap(1, 0, 8), still, 40)},
		{Name: "bake-steep-ring", Notes: "Walking west into the ring's cliff, a step over 40 m, then south-west along its foot.",
			Region: "steep", Params: params, Geometry: steep, Start: on(steep, 22, 40), Inputs: runs(west, 120, sim.Input{Down: true, Left: true}, 180)},
		{Name: "bake-steep-off-plateau", Notes: "Walking west off the plateau's top over its cliff: the body falls 12 m and lands.",
			Region: "steep", Params: params, Geometry: steep, Start: on(steep, 70, 60), Inputs: runs(west, 240)},
		{Name: "bake-steep-ridge", Notes: "Walking north at the ridge 28 m high: its flank is steep from the foot, so the body stops; then north-east it slides along the foot.",
			Region: "steep", Params: params, Geometry: steep, Start: on(steep, 40, 84), Inputs: runs(north, 60, northEast, 180)},
	}
}

func TestVectors(t *testing.T) {
	scenarios := scenarios()
	names := map[string]bool{}
	for _, v := range scenarios {
		names[v.Name] = true
		t.Run(v.Name, func(t *testing.T) {
			v.Expected = replay(v)
			var written any = v
			if v.Region != "" {
				written = struct {
					vector
					Geometry *sim.Geometry `json:"geometry,omitempty"` // the bake's, which the client reads
				}{vector: v}
			}
			want, err := json.MarshalIndent(written, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			want = append(want, '\n')
			file := filepath.Join(vectorsDir, v.Name+".json")
			if *update {
				if err := os.WriteFile(file, want, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			got, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("%v; run with -update", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s differs from its scenario replayed: %s; run with -update after a deliberate change", file, firstDifference(got, v))
			}
		})
	}
	for _, name := range requiredVectors {
		if !names[name] {
			t.Errorf("no scenario %s", name)
		}
	}
	files, err := filepath.Glob(filepath.Join(vectorsDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if name := strings.TrimSuffix(filepath.Base(file), ".json"); !names[name] {
			t.Errorf("%s has no scenario", file)
		}
	}
	if len(names) != len(requiredVectors) {
		t.Errorf("%d scenarios, %d required: list each in requiredVectors", len(names), len(requiredVectors))
	}
}

// firstDifference names the first sub-step whose state the file gets wrong, or says the scenario itself changed.
func firstDifference(file []byte, v vector) string {
	var saved vector
	if err := json.Unmarshal(file, &saved); err != nil {
		return err.Error()
	}
	for i := range min(len(saved.Expected), len(v.Expected)) {
		if saved.Expected[i] != v.Expected[i] {
			return fmt.Sprintf("sub-step %d: the file has %+v, the replay %+v", i+1, saved.Expected[i], v.Expected[i])
		}
	}
	if len(saved.Expected) != len(v.Expected) {
		return fmt.Sprintf("%d states in the file, %d replayed", len(saved.Expected), len(v.Expected))
	}
	return "the scenario changed"
}

func replay(v vector) []sim.State {
	p := v.Params
	if v.SpeedFactor != 0 {
		p = p.Faster(v.SpeedFactor)
	}
	g := v.Geometry
	deep := g.Terrain.Barrier
	g.Terrain = sim.NewTerrain(g.Terrain.Cell, g.Terrain.NX, g.Terrain.NZ, g.Terrain.Heights, p.MaxSlope)
	g.Terrain.Barrier = deep
	g = sim.Indexed(g)        // as every map is; the TS replay reads them the same way
	var blocking sim.Blockers // bucketed as the world buckets its mobs
	blocking.Set(g.Blockers, g.SizeX, g.SizeZ)
	g = g.Blocking(&blocking)
	s := v.Start
	var out []sim.State
	for _, run := range v.Inputs {
		if run.Blockers != nil {
			blocking.Set(run.Blockers, g.SizeX, g.SizeZ)
			g = g.Blocking(&blocking)
		}
		if run.Held != 0 {
			s.Held = run.Held
		}
		if run.Slow != nil {
			s.Slow = *run.Slow
		}
		if run.Breaks {
			s.Held = 0
		}
		if run.EndsSprint {
			s = sim.EndSprint(s)
		}
		if m := run.Move; m != nil {
			if m.Kind == "blink" {
				s = sim.Blink(s, sim.Way{X: m.UX, Z: m.UZ, Length: m.Length}, g, p)
			} else {
				s = sim.Dash(s, sim.Way{X: m.UX, Z: m.UZ, Length: m.Length}, m.Steps, m.mobs(), m.Kind == "leap", p)
			}
		}
		stepping := p
		if run.Fighting {
			stepping = p.Fighting()
		}
		for range run.Repeat {
			s = sim.Step(s, run.Input, g, stepping)
			out = append(out, s)
		}
	}
	return out
}

// The required list and the scenarios name the same vectors, each once.
func TestRequiredVectorsAreDistinct(t *testing.T) {
	sorted := slices.Clone(requiredVectors)
	slices.Sort(sorted)
	if len(slices.Compact(sorted)) != len(requiredVectors) {
		t.Fatal("a required vector is listed twice")
	}
}
