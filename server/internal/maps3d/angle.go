package maps3d

import "math"

// Every number the sim reads is + − × ÷ and sqrt, each product rounded (float64), so every CPU gives the same bits; math.Sincos doesn't.

// sincos is the sine and cosine of deg degrees. It reduces to a quarter turn exactly, so a multiple of 90° gives exactly 0 and ±1.
func sincos(deg float64) (sin, cos float64) {
	d := math.Mod(deg, 360)
	if d < 0 {
		d += 360
	}
	q := min(math.Floor(d/90), 3)
	r := d - float64(90*q)
	var s, c float64
	if r <= 45 {
		s, c = quarter(r)
	} else {
		c, s = quarter(90 - r)
	}
	switch q {
	case 0:
		sin, cos = s, c
	case 1:
		sin, cos = c, -s
	case 2:
		sin, cos = -s, -c
	default:
		sin, cos = -c, s
	}
	return unsigned(sin), unsigned(cos)
}

// Taylor coefficients after the leading 1, by power of x²: sin x = x(1 − x²/3! + x⁴/5! …), cos x = 1 − x²/2! + x⁴/4! …
var (
	sinTerms = [...]float64{-1.0 / 6, 1.0 / 120, -1.0 / 5040, 1.0 / 362880, -1.0 / 39916800, 1.0 / 6227020800, -1.0 / 1307674368000}
	cosTerms = [...]float64{-1.0 / 2, 1.0 / 24, -1.0 / 720, 1.0 / 40320, -1.0 / 3628800, 1.0 / 479001600, -1.0 / 87178291200, 1.0 / 20922789888000}
)

// quarter is the sine and cosine of r degrees, 0 to 45, by Horner's rule on the series: the first terms left out are below 1e-16 there.
func quarter(r float64) (sin, cos float64) {
	x := float64(r * (math.Pi / 180))
	x2 := float64(x * x)
	return float64(x * (1 + float64(x2*horner(sinTerms[:], x2)))), 1 + float64(x2*horner(cosTerms[:], x2))
}

// horner is c[0] + x(c[1] + x(c[2] + …)), each product rounded on its own.
func horner(c []float64, x float64) float64 {
	v := c[len(c)-1]
	for i := len(c) - 2; i >= 0; i-- {
		v = c[i] + float64(x*v)
	}
	return v
}

// unsigned turns -0 into 0, so a turned box's axis is written the same way whichever quarter it came from.
func unsigned(v float64) float64 {
	if v == 0 {
		return 0
	}
	return v
}
