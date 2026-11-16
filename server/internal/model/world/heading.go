package world

import "math"

// YawOf is the yaw a heading faces, radians from north (+z) toward east (+x): 3d-world.md section 3.
func YawOf(hx, hz float64) float32 { return float32(math.Atan2(hx, hz)) }

// HeadingOf is the unit heading a yaw faces.
func HeadingOf(yaw float32) (hx, hz float64) { return math.Sincos(float64(yaw)) }
