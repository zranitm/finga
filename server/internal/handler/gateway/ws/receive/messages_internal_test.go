package receive

import (
	"errors"
	"math"
	"testing"

	model "finga/internal/model/gateway"
)

// An attack's or a skill's aim passes when finite, and is a malformed body when NaN or infinite.
func TestAnAimMustBeFinite(t *testing.T) {
	for _, tt := range []struct {
		name string
		yaw  float32
		err  error
	}{
		{"finite", -2.5, nil},
		{"zero", 0, nil},
		{"NaN", float32(math.NaN()), model.ErrBadAim},
		{"+Inf", float32(math.Inf(1)), model.ErrBadAim},
		{"-Inf", float32(math.Inf(-1)), model.ErrBadAim},
	} {
		t.Run(tt.name, func(t *testing.T) {
			aim, err := aimOf(tt.yaw)
			if !errors.Is(err, tt.err) || (err == nil && aim != tt.yaw) {
				t.Fatalf("aimOf(%v) = %v, %v; want %v", tt.yaw, aim, err, tt.err)
			}
		})
	}
}
