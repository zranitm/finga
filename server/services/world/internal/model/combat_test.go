package model_test

import (
	"math/rand/v2"
	"testing"

	"finga/services/world/internal/model"
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

func TestResolve(t *testing.T) {
	// A level 1 Warrior (page 5: attack 21, accuracy 21, crit 6%, crit
	// damage +52.5%) against a green slime (defense 2, evasion 2).
	warrior := model.Attacker{AttackPower: 21, Accuracy: 21, CritRate: 0.06, CritDamage: 0.525}
	slime := model.Defender{Defense: 2, Evasion: 2}
	tests := []struct {
		name string
		a    model.Attacker
		d    model.Defender
		seq  []float64 // hit roll, variation roll, crit roll
		want model.Hit
	}{
		// hit chance 0.80 + 0.02·19 = 0.99 (the cap); raw 21·1.0 = 21; 21·100/102 = 20.59 → 21
		{"average hit", warrior, slime, []float64{0.5, 0.5, 0.5}, model.Hit{Damage: 21}},
		// variation 0.90: 18.9·100/102 = 18.53 → 19
		{"low variation", warrior, slime, []float64{0.5, 0.0, 0.5}, model.Hit{Damage: 19}},
		// variation just under 1.10: 23.1·100/102 = 22.6 → 23
		{"high variation", warrior, slime, []float64{0.5, 0.999999, 0.5}, model.Hit{Damage: 23}},
		{"a roll at the chance misses (0.99 is the cap)", warrior, slime, []float64{0.99, 0.5, 0.5}, model.Hit{Miss: true}},
		{"a roll just below the chance hits", warrior, slime, []float64{0.989, 0.5, 0.5}, model.Hit{Damage: 21}},
		// crit: 21 · 1.525 = 32.0 → 32
		{"crit", warrior, slime, []float64{0.5, 0.5, 0.059}, model.Hit{Damage: 32, Crit: true}},
		{"a crit roll at the rate is not a crit", warrior, slime, []float64{0.5, 0.5, 0.06}, model.Hit{Damage: 21}},
		// hit chance 0.80 + 0.02·(0 − 50) = −0.2 → clamped to 0.20
		{"hit chance floors at 20%", model.Attacker{AttackPower: 10}, model.Defender{Evasion: 50}, []float64{0.19, 0.5, 0.5}, model.Hit{Damage: 10}},
		{"and a roll of 0.20 misses there", model.Attacker{AttackPower: 10}, model.Defender{Evasion: 50}, []float64{0.20, 0.5, 0.5}, model.Hit{Miss: true}},
		// 10·100/(100+100) = 5
		{"defense halves at 100", model.Attacker{AttackPower: 10}, model.Defender{Defense: 100}, []float64{0.5, 0.5, 0.5}, model.Hit{Damage: 5}},
		// penetration 50%: defense 50 → 10·100/150 = 6.67 → 7
		{"penetration cuts defense", model.Attacker{AttackPower: 10, Penetration: 0.5}, model.Defender{Defense: 100}, []float64{0.5, 0.5, 0.5}, model.Hit{Damage: 7}},
		// 1·0.9·100/1100 = 0.08 → max(1, 0) = 1
		{"minimum damage is 1", model.Attacker{AttackPower: 1}, model.Defender{Defense: 1000}, []float64{0.5, 0.0, 0.5}, model.Hit{Damage: 1}},
		{"a crit on minimum damage rounds from the formula", model.Attacker{AttackPower: 1, CritRate: 1, CritDamage: 0.5}, model.Defender{Defense: 1000}, []float64{0.5, 0.0, 0.0}, model.Hit{Damage: 2, Crit: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := model.Resolve(tt.a, tt.d, &rolls{seq: tt.seq})
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
	t.Run("a miss draws once, so the next roll is the next attack's", func(t *testing.T) {
		r := &rolls{seq: []float64{0.995, 0.5, 0.5, 0.5}}
		if h := model.Resolve(warrior, slime, r); !h.Miss {
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
			if model.Resolve(warrior, slime, a) != model.Resolve(warrior, slime, b) {
				t.Fatal("replays diverged")
			}
		}
	})
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

func TestNearestInBox(t *testing.T) {
	box := model.MeleeBox(100, 600, 1, 64, 56) // x 100..164, y 544..600
	mob := func(x float64) model.Target { return model.Target{Box: model.Rect{X: x - 16, Y: 572, W: 32, H: 28}} }
	tests := []struct {
		name    string
		targets []model.Target
		want    int
		ok      bool
	}{
		{"nothing in range does nothing", []model.Target{mob(300), mob(50)}, 0, false},
		{"one in the box", []model.Target{mob(300), mob(130)}, 1, true},
		{"the nearest of two in the box", []model.Target{mob(150), mob(120)}, 1, true},
		{"a box touching the edge does not count", []model.Target{mob(180)}, 0, false}, // 164..196: starts where the box ends
		{"a mob behind the attacker is not hit", []model.Target{mob(80)}, 0, false},
		{"a mob on a higher platform is out of the box", []model.Target{{Box: model.Rect{X: 110, Y: 400, W: 32, H: 28}}}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := model.NearestInBox(100, 600, box, tt.targets)
			if ok != tt.ok || (ok && got != tt.want) {
				t.Fatalf("got %d, %v; want %d, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestFirstInLine(t *testing.T) {
	mob := func(x float64) model.Target { return model.Target{Box: model.Rect{X: x - 16, Y: 572, W: 32, H: 28}} }
	// The shot leaves at chest height: feet 600, so y 572 (inside a 28 px tall mob standing on the same floor).
	tests := []struct {
		name    string
		facing  int
		targets []model.Target
		want    int
		ok      bool
	}{
		{"the first in line, not the nearest by index", 1, []model.Target{mob(400), mob(250)}, 1, true},
		{"beyond the range is missed", 1, []model.Target{mob(600)}, 0, false},
		{"exactly at the range counts", 1, []model.Target{mob(536)}, 0, true}, // box starts at 520 = 100 + 420
		{"behind the attacker is missed", 1, []model.Target{mob(50)}, 0, false},
		{"facing left hits the other way", -1, []model.Target{mob(50), mob(300)}, 0, true},
		{"a mob overlapping the attacker counts at distance zero", 1, []model.Target{mob(100)}, 0, true},
		{"a mob on another height is missed", 1, []model.Target{{Box: model.Rect{X: 200, Y: 400, W: 32, H: 28}}}, 0, false},
		{"nothing", 1, nil, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := model.FirstInLine(100, 572, tt.facing, 420, tt.targets)
			if ok != tt.ok || (ok && got != tt.want) {
				t.Fatalf("got %d, %v; want %d, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}
