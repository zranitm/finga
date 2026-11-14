package model_test

import (
	"math/rand/v2"
	"slices"
	"testing"

	"finga/internal/gamedata"
	"finga/modules/world/internal/model"
)

// rolls hands out a fixed sequence, then repeats the last value.
type rolls struct {
	seq []float64
	i   int
}

func (r *rolls) Float64() float64 {
	v := r.seq[min(r.i, len(r.seq)-1)]
	r.i++
	return v
}

// hit is stats.yaml's hit curve and limits.
var hit = gamedata.HitCurve{Top: 0.97, K: 0.12, Power: 2, Min: 0.20, Max: 0.99}

func TestResolve(t *testing.T) {
	warrior := model.Attacker{PhysicalAttack: 21, Accuracy: 21, CritRate: 0.06, CritDamage: 0.525}
	slime := model.Defender{PhysicalDefense: 2, Evasion: 2}
	tests := []struct {
		name string
		a    model.Attacker
		d    model.Defender
		seq  []float64 // hit roll, variation roll, crit roll
		want model.Hit
	}{
		// hit chance 0.97 ÷ (1 + 0.12·(2/21)²) = 0.969; raw 21·1.0 = 21; 21·100/102 = 20.59 → 21
		{"average hit", warrior, slime, []float64{0.5, 0.5, 0.5}, model.Hit{Damage: 21}},
		// variation 0.90: 18.9·100/102 = 18.53 → 19
		{"low variation", warrior, slime, []float64{0.5, 0.0, 0.5}, model.Hit{Damage: 19}},
		// variation just under 1.10: 23.1·100/102 = 22.6 → 23
		{"high variation", warrior, slime, []float64{0.5, 0.999999, 0.5}, model.Hit{Damage: 23}},
		// no evasion: the curve's top, 0.97
		{"a roll at the chance misses", warrior, model.Defender{}, []float64{0.97, 0.5, 0.5}, model.Hit{Miss: true}},
		{"a roll just below the chance hits", warrior, model.Defender{}, []float64{0.969, 0.5, 0.5}, model.Hit{Damage: 21}},
		// evasion equal to accuracy: 0.97 ÷ 1.12 = 0.866
		{"evasion as high as accuracy", warrior, model.Defender{Evasion: 21}, []float64{0.866, 0.5, 0.5}, model.Hit{Damage: 21}},
		{"and a roll past it misses", warrior, model.Defender{Evasion: 21}, []float64{0.867, 0.5, 0.5}, model.Hit{Miss: true}},
		// crit: 21 · 1.525 = 32.0 → 32
		{"crit", warrior, slime, []float64{0.5, 0.5, 0.059}, model.Hit{Damage: 32, Crit: true}},
		{"a crit roll at the rate is not a crit", warrior, slime, []float64{0.5, 0.5, 0.06}, model.Hit{Damage: 21}},
		// no accuracy against evasion: 0, clamped to 0.20
		{"hit chance floors at 20%", model.Attacker{PhysicalAttack: 10}, model.Defender{Evasion: 50}, []float64{0.19, 0.5, 0.5}, model.Hit{Damage: 10}},
		{"and a roll of 0.20 misses there", model.Attacker{PhysicalAttack: 10}, model.Defender{Evasion: 50}, []float64{0.20, 0.5, 0.5}, model.Hit{Miss: true}},
		// 10·100/(100+100) = 5
		{"defense halves at 100", model.Attacker{PhysicalAttack: 10}, model.Defender{PhysicalDefense: 100}, []float64{0.5, 0.5, 0.5}, model.Hit{Damage: 5}},
		// penetration 50%: defense 50 → 10·100/150 = 6.67 → 7
		{"penetration cuts defense", model.Attacker{PhysicalAttack: 10, Penetration: 0.5}, model.Defender{PhysicalDefense: 100}, []float64{0.5, 0.5, 0.5}, model.Hit{Damage: 7}},
		// 1·0.9·100/1100 = 0.08 → max(1, 0) = 1
		{"minimum damage is 1", model.Attacker{PhysicalAttack: 1}, model.Defender{PhysicalDefense: 1000}, []float64{0.5, 0.0, 0.5}, model.Hit{Damage: 1}},
		// 10·100/(100+100) = 5: magic meets magic defense, whatever the physical numbers
		{"a magic attack meets magic defense", model.Attacker{Damage: gamedata.Magic, MagicAttack: 10, PhysicalAttack: 1000}, model.Defender{MagicDefense: 100}, []float64{0.5, 0.5, 0.5}, model.Hit{Damage: 5}},
		{"a physical attack ignores magic defense", model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 10, MagicAttack: 1000}, model.Defender{MagicDefense: 100}, []float64{0.5, 0.5, 0.5}, model.Hit{Damage: 10}},
		{"a magic attack ignores physical defense", model.Attacker{Damage: gamedata.Magic, MagicAttack: 10}, model.Defender{PhysicalDefense: 100}, []float64{0.5, 0.5, 0.5}, model.Hit{Damage: 10}},
		// penetration 50%: magic defense 50 → 10·100/150 = 6.67 → 7
		{"penetration cuts magic defense too", model.Attacker{Damage: gamedata.Magic, MagicAttack: 10, Penetration: 0.5}, model.Defender{MagicDefense: 100}, []float64{0.5, 0.5, 0.5}, model.Hit{Damage: 7}},
		{"a crit on minimum damage rounds from the formula", model.Attacker{PhysicalAttack: 1, CritRate: 1, CritDamage: 0.5}, model.Defender{PhysicalDefense: 1000}, []float64{0.5, 0.0, 0.0}, model.Hit{Damage: 2, Crit: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := model.Resolve(tt.a, tt.d, hit, &rolls{seq: tt.seq})
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
	t.Run("the hit chance is held between the limits it is given", func(t *testing.T) {
		narrow := gamedata.HitCurve{Top: 0.97, K: 0.12, Power: 2, Min: 0.5, Max: 0.6}
		sure, unsure := model.Attacker{PhysicalAttack: 10, Accuracy: 100}, model.Attacker{PhysicalAttack: 10}
		for _, c := range []struct {
			a    model.Attacker
			roll float64
			miss bool
		}{{sure, 0.59, false}, {sure, 0.6, true}, {unsure, 0.49, false}, {unsure, 0.5, true}} {
			d := model.Defender{}
			if c.a == unsure {
				d.Evasion = 100
			}
			if h := model.Resolve(c.a, d, narrow, &rolls{seq: []float64{c.roll, 0.5, 0.5}}); h.Miss != c.miss {
				t.Errorf("accuracy %v, roll %v: %+v, want miss %v", c.a.Accuracy, c.roll, h, c.miss)
			}
		}
	})
	t.Run("a miss draws once, so the next roll is the next attack's", func(t *testing.T) {
		r := &rolls{seq: []float64{0.995, 0.5, 0.5, 0.5}}
		if h := model.Resolve(warrior, slime, hit, r); !h.Miss {
			t.Fatalf("first: %+v", h)
		}
		if r.i != 1 {
			t.Fatalf("%d draws for a miss, want 1", r.i)
		}
	})
	t.Run("the same seed gives the same sequence of hits", func(t *testing.T) {
		a := rand.New(rand.NewPCG(7, 11)) //nolint:gosec // deterministic test
		b := rand.New(rand.NewPCG(7, 11)) //nolint:gosec // deterministic test
		for range 100 {
			if model.Resolve(warrior, slime, hit, a) != model.Resolve(warrior, slime, hit, b) {
				t.Fatal("replays diverged")
			}
		}
	})
}

// A skill's power multiplies both attacks and leaves the rest of the attacker as it was.
func TestAttackerTimes(t *testing.T) {
	a := model.Attacker{Damage: gamedata.Magic, PhysicalAttack: 10, MagicAttack: 20, Accuracy: 30, CritRate: 0.1, CritDamage: 0.5, Penetration: 0.2}
	want := model.Attacker{Damage: gamedata.Magic, PhysicalAttack: 15, MagicAttack: 30, Accuracy: 30, CritRate: 0.1, CritDamage: 0.5, Penetration: 0.2}
	if got := a.Times(1.5); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestMeleeBox(t *testing.T) {
	tests := []struct {
		name   string
		facing int
		want   model.Rect
	}{
		{"facing right: the box starts at the feet and spans forward", 1, model.Rect{X: 100, Y: 544, W: 64, H: 56}},
		{"facing left: the box ends at the feet", -1, model.Rect{X: 36, Y: 544, W: 64, H: 56}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := model.MeleeBox(100, 600, tt.facing, 64, 56); got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// first is the one target Reached returns for a shape that hits one, or -1.
func first(x, y float64, facing int, height float64, s gamedata.Shape, targets []model.Target) int {
	if got := model.Reached(x, y, facing, height, s, targets, 1); len(got) == 1 {
		return got[0]
	}
	return -1
}

func TestAMeleeShapeReachesTheNearestInItsBox(t *testing.T) {
	swing := gamedata.Shape{Kind: gamedata.MeleeShape, Width: 64, Height: 56} // x 100..164, y 544..600
	mob := func(x float64) model.Target { return model.Target{Box: model.Rect{X: x - 16, Y: 572, W: 32, H: 28}} }
	tests := []struct {
		name    string
		targets []model.Target
		want    int
	}{
		{"nothing in range does nothing", []model.Target{mob(300), mob(50)}, -1},
		{"one in the box", []model.Target{mob(300), mob(130)}, 1},
		{"the nearest of two in the box", []model.Target{mob(150), mob(120)}, 1},
		{"a box touching the edge does not count", []model.Target{mob(180)}, -1}, // 164..196: starts where the box ends
		{"a mob behind the attacker is not hit", []model.Target{mob(80)}, -1},
		{"a mob on a higher platform is out of the box", []model.Target{{Box: model.Rect{X: 110, Y: 400, W: 32, H: 28}}}, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := first(100, 600, 1, 56, swing, tt.targets); got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestARangedShapeReachesTheFirstInLine(t *testing.T) {
	shot := gamedata.Shape{Kind: gamedata.RangedShape, Range: 420}
	mob := func(x float64) model.Target { return model.Target{Box: model.Rect{X: x - 16, Y: 572, W: 32, H: 28}} }
	// From feet at 600 with a height of 56, the shot leaves at 572, inside a 28 px mob on the same floor.
	tests := []struct {
		name    string
		facing  int
		targets []model.Target
		want    int
	}{
		{"the first in line, not the nearest by index", 1, []model.Target{mob(400), mob(250)}, 1},
		{"beyond the range is missed", 1, []model.Target{mob(600)}, -1},
		{"exactly at the range counts", 1, []model.Target{mob(536)}, 0}, // box starts at 520 = 100 + 420
		{"behind the attacker is missed", 1, []model.Target{mob(50)}, -1},
		{"facing left hits the other way", -1, []model.Target{mob(50), mob(300)}, 0},
		{"a mob overlapping the attacker counts at distance zero", 1, []model.Target{mob(100)}, 0},
		{"a mob on another height is missed", 1, []model.Target{{Box: model.Rect{X: 200, Y: 400, W: 32, H: 28}}}, -1},
		{"nothing", 1, nil, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := first(100, 600, tt.facing, 56, shot, tt.targets); got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestAShapeReachesUpToItsMostNearestFirst(t *testing.T) {
	mob := func(x, y float64) model.Target {
		return model.Target{Box: model.Rect{X: x - 16, Y: y - 28, W: 32, H: 28}}
	}
	targets := []model.Target{mob(400, 600), mob(160, 600), mob(250, 600), mob(40, 600), mob(110, 300)}
	for _, c := range []struct {
		name  string
		shape gamedata.Shape
		most  int
		want  []int
	}{
		{"a wide swing takes the two in front", gamedata.Shape{Kind: gamedata.MeleeShape, Width: 200, Height: 56}, 3, []int{1, 2}},
		{"a line takes them in the order it crosses them", gamedata.Shape{Kind: gamedata.RangedShape, Range: 420}, 3, []int{1, 2, 0}},
		{"and no more than its most", gamedata.Shape{Kind: gamedata.RangedShape, Range: 420}, 2, []int{1, 2}},
		// From the middle at (100, 572): the nearest points of 40's and 160's boxes are 44 away, 250's 134, 110 on high 244 above.
		{"an area takes those within its radius, either side", gamedata.Shape{Kind: gamedata.AreaShape, Radius: 140}, 5, []int{1, 3, 2}},
		{"and not past it", gamedata.Shape{Kind: gamedata.AreaShape, Radius: 40}, 5, []int{}},
	} {
		if got := model.Reached(100, 600, 1, 56, c.shape, targets, c.most); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
