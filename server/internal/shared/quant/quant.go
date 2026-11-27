// Package quant is the whole-number units the wire gives a body's place and way: what the server rounds a view to and the client reads it back as, so the two keep the same baseline for the steps between views (large-regions.md section 5).
package quant

import "math"

// Whole numbers per metre, per metre a second and per unit of a heading's part, and per radian of a yaw.
const (
	PerMetre   = 100.0  // a position, in centimetres
	PerSpeed   = 1000.0 // a velocity, in millimetres a second
	PerHeading = 1000.0 // a heading's part, in thousandths
	PerYaw     = 1000.0 // a yaw, in thousandths of a radian
)

// round is v to the nearest whole number, halves toward +∞, so the client's Math.floor(v + 0.5) agrees at every value.
func round(v float64) int32 { return int32(math.Floor(v + 0.5)) } //nolint:gosec // a place, a speed or a heading's part fits an int32 by far

// Position is metres as whole centimetres.
func Position(m float64) int32 { return round(float64(m * PerMetre)) }

// Speed is metres a second as whole millimetres a second.
func Speed(v float64) int32 { return round(float64(v * PerSpeed)) }

// Heading is a heading's part as whole thousandths.
func Heading(h float64) int32 { return round(float64(h * PerHeading)) }

// Yaw is radians as whole thousandths.
func Yaw(y float32) int32 { return round(float64(float64(y) * PerYaw)) }

// Metres is whole centimetres as metres.
func Metres(cm int32) float64 { return float64(cm) / PerMetre }

// MetresPerSecond is whole millimetres a second as metres a second.
func MetresPerSecond(mm int32) float64 { return float64(mm) / PerSpeed }

// Unit is whole thousandths as a heading's part.
func Unit(milli int32) float64 { return float64(milli) / PerHeading }

// Radians is whole thousandths as a yaw.
func Radians(milli int32) float32 { return float32(float64(milli) / PerYaw) }
