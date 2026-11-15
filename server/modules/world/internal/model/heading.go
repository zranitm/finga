package model

import "math"

// YawOf is a 2D facing's yaw (3d-world.md section 3: radians from north toward east), east π/2 and west −π/2, until the 3D sim brings a heading.
func YawOf(facing int) float32 {
	return float32(facing) * math.Pi / 2
}

// FacingOf is the 2D facing a yaw turns into: west for a yaw west of the north-south line, else east.
func FacingOf(yaw float32) int {
	if math.Sin(float64(yaw)) < 0 {
		return -1
	}
	return 1
}
