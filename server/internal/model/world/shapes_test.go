package model_test

import (
	"math"
	"slices"
	"testing"

	"finga/internal/modules/world/internal/model"
	"finga/internal/shared/gamedata"
)

// slime is a green slime's body with its feet at (x, y, z).
func slime(x, y, z float64) model.Body {
	return model.Body{X: x, Y: y, Z: z, Radius: 0.5, Height: 0.875}
}

// north is an attacker at the origin aiming north, along +z.
var north = model.Aim{}

func TestAnArcReachesTheBodiesItsSectorMeets(t *testing.T) {
	swing := gamedata.Shape{Kind: gamedata.ArcShape, Radius: 2.375, Angle: 120, Height: 1.75}
	for _, tt := range []struct {
		name string
		aim  model.Aim
		s    gamedata.Shape
		body model.Body
		want bool
	}{
		{"in front", north, swing, slime(0, 0, 2), true},
		{"its circle reaching in past the radius", north, swing, slime(0, 0, 2.8), true},
		{"beyond the radius and its own", north, swing, slime(0, 0, 3), false},
		{"inside the angle, off the aim", north, swing, slime(1.5, 0, 1.5), true},
		{"beside the attacker, outside the angle", north, swing, slime(2, 0, 0), false},
		{"a body wide enough to reach the sector's edge", north, swing, model.Body{X: 2, Radius: 1.1, Height: 3}, true},
		{"behind the attacker", north, swing, slime(0, 0, -1.5), false},
		{"over the attacker's feet", north, swing, slime(0, 0, -0.3), true},
		{"a full circle reaches behind", north, gamedata.Shape{Kind: gamedata.ArcShape, Radius: 2.375, Angle: 360, Height: 1.75}, slime(0, 0, -1.5), true},
		{"turned east, the east", model.Aim{Yaw: math.Pi / 2}, swing, slime(2, 0, 0), true},
		{"turned east, not the north", model.Aim{Yaw: math.Pi / 2}, swing, slime(0, 0, 2), false},
		{"on ground above its height", north, swing, slime(0, 1.8, 2), false},
		{"with its top below the attacker's feet", north, swing, slime(0, -1, 2), false},
		{"its top reaching the feet's level", north, swing, slime(0, -0.875, 2), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(model.Reached(tt.aim, tt.s, []model.Body{tt.body}, 1)) == 1; got != tt.want {
				t.Fatalf("reached %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAShotReachesTheBodiesItsLaneMeets(t *testing.T) {
	shot := gamedata.Shape{Kind: gamedata.ShotShape, Range: 13.125, Width: 0.6}
	for _, tt := range []struct {
		name string
		aim  model.Aim
		body model.Body
		want bool
	}{
		{"along the aim", north, slime(0, 0, 10), true},
		{"its circle reaching the lane's end", north, slime(0, 0, 13.5), true},
		{"past the range", north, slime(0, 0, 14), false},
		{"its circle reaching the lane's side", north, slime(0.7, 0, 5), true},
		{"beside the lane", north, slime(1, 0, 5), false},
		{"behind the attacker", north, slime(0, 0, -2), false},
		{"over the attacker", north, slime(0, 0, -0.3), true},
		{"turned west, the west", model.Aim{Yaw: -math.Pi / 2}, slime(-6, 0, 0), true},
		{"on other ground, as a shot has no height", north, slime(0, 3, 6), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(model.Reached(tt.aim, shot, []model.Body{tt.body}, 1)) == 1; got != tt.want {
				t.Fatalf("reached %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAShapeReachesUpToItsMostNearestFirst(t *testing.T) {
	bodies := []model.Body{slime(0, 0, 8), slime(0, 0, 2), slime(0.5, 0, 4), slime(-1.8, 0, 0), slime(0, 0, -1.9)}
	for _, c := range []struct {
		name  string
		shape gamedata.Shape
		most  int
		want  []int
	}{
		{"an arc takes those in it by their centres' distance", gamedata.Shape{Kind: gamedata.ArcShape, Radius: 4, Angle: 120, Height: 1.75}, 5, []int{1, 2}},
		{"a shot takes them in the order it meets them", gamedata.Shape{Kind: gamedata.ShotShape, Range: 13.125, Width: 0.6}, 5, []int{1, 2, 0}},
		{"and no more than its most", gamedata.Shape{Kind: gamedata.ShotShape, Range: 13.125, Width: 0.6}, 2, []int{1, 2}},
		// The nearest points of the bodies at 1.8 and 1.9 m are 1.3 and 1.4 m away, the one at 2 m 1.5.
		{"an area takes those its circle meets, any way round", gamedata.Shape{Kind: gamedata.AreaShape, Radius: 1.5}, 5, []int{3, 4, 1}},
		{"and not past it", gamedata.Shape{Kind: gamedata.AreaShape, Radius: 1.2}, 5, []int{}},
	} {
		if got := model.Reached(north, c.shape, bodies, c.most); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
