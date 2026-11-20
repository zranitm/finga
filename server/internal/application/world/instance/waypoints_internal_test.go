package instance

import (
	"maps"
	"slices"
	"testing"
	"time"

	"pgregory.net/rapid"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// A cell's list holds every stone a scan finds within reach of any spot in it, on grids of any cell and size, an edge cell holding what's past it.
func TestACellListsEveryStoneAScanFindsInReach(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cell := rapid.Float64Range(4, 48).Draw(t, "cell")
		sx, sz := rapid.Float64Range(1, 200).Draw(t, "sizeX"), rapid.Float64Range(1, 200).Draw(t, "sizeZ")
		n := newInterest(gamedata.Interest{Cell: cell, Behind: 1, Ahead: 1, Side: 1}, sx, sz)
		ws := rapid.SliceOfN(rapid.Custom(func(t *rapid.T) gamedata.Waypoint {
			return gamedata.Waypoint{At: gamedata.Point{X: rapid.Float64Range(-20, sx+20).Draw(t, "x"), Z: rapid.Float64Range(-20, sz+20).Draw(t, "z")}}
		}), 1, 6).Draw(t, "stones")
		stones := stonesNear(ws, n)
		for range 50 {
			x, z := rapid.Float64Range(-30, sx+30).Draw(t, "px"), rapid.Float64Range(-30, sz+30).Draw(t, "pz")
			listed := stones[n.of(x, z)]
			for k, w := range ws {
				if reaches(x-w.At.X, z-w.At.Z) && !slices.Contains(listed, k) {
					t.Fatalf("(%g, %g) reaches stone %d at %+v, but its cell %d lists %v", x, z, k, w.At, n.of(x, z), listed)
				}
			}
		}
	})
}

// A living player finds a stone within reach, the edge counting, once and for good, and is told; the dead find none.
func TestALivingPlayerFindsAStoneInReachOnce(t *testing.T) {
	m := &gamedata.Map{Waypoints: []gamedata.Waypoint{{ID: "a", At: gamedata.Point{X: 50, Z: 50}}, {ID: "b", At: gamedata.Point{X: 150, Z: 50}}}}
	n := newInterest(gamedata.Interest{Cell: 32, Behind: 1, Ahead: 1, Side: 1}, 200, 100)
	then, now := time.Unix(1_800_000_000, 0), time.Unix(1_800_000_600, 0)
	for _, tt := range []struct {
		name  string
		x, z  float64
		dead  bool
		found model.Waypoints
		want  []string
	}{
		{"12 m off", 50, 62, false, nil, []string{"a"}},
		{"a hair past 12 m", 50, 62.001, false, nil, nil},
		{"12 m off, in the cell east of the stone's", 162, 50, false, nil, []string{"b"}}, // b at x 150 is in the cell from 128, the player in the one from 160
		{"dead beside it", 50, 51, true, nil, nil},
		{"beside one already found", 50, 51, false, model.Waypoints{"a": then}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			id := kernel.NewCharacterID()
			found := model.Waypoints{}
			maps.Copy(found, tt.found)
			p := &player{ID: id, Waypoints: found, state: sim.State{X: tt.x, Z: tt.z}, dead: tt.dead}
			i := &Instance{m: m, interest: n, stones: stonesNear(m.Waypoints, n), players: map[kernel.CharacterID]*player{id: p}, order: []kernel.CharacterID{id}, tick: 7}
			i.trackWaypoints(now)
			i.trackWaypoints(now.Add(time.Second))
			var told []string
			for _, e := range p.private {
				if f, ok := e.(model.WaypointFound); ok && f.Tick == 7 {
					told = append(told, f.Waypoint)
				}
			}
			if !slices.Equal(told, tt.want) || len(p.private) != len(tt.want) {
				t.Fatalf("told %v in %+v; want %v once", told, p.private, tt.want)
			}
			want := maps.Clone(tt.found)
			for _, w := range tt.want {
				if want == nil {
					want = model.Waypoints{}
				}
				want[w] = now
			}
			if len(want) != len(p.Waypoints) || !maps.EqualFunc(want, p.Waypoints, time.Time.Equal) {
				t.Fatalf("found %v; want %v", p.Waypoints, want)
			}
		})
	}
}
