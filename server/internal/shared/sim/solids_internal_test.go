package sim

import (
	"slices"
	"testing"
)

// A walk over several buckets gives each index once, ascending, though a thing lies in many of them.
func TestAWalkGivesEachIndexOnceInOrder(t *testing.T) {
	bounds := [][4]float64{{1, 1, 30, 30}, {9, 9, 10, 10}, {17, 1, 18, 2}, {1, 17, 2, 18}, {25, 25, 31, 31}}
	var b buckets
	b.fill(len(bounds), bucketM, 32, 32, func(i int) (x0, z0, x1, z1 float64) {
		return bounds[i][0], bounds[i][1], bounds[i][2], bounds[i][3]
	})
	for _, tt := range []struct {
		x0, z0, x1, z1 float64
		want           []int
	}{
		{0, 0, 31, 31, []int{0, 1, 2, 3, 4}},
		{9, 1, 17, 17, []int{0, 1, 2}}, // buckets 1 to 2 along x, 0 to 2 along z
		{24, 24, 31, 31, []int{0, 4}},
	} {
		var got []int
		n := b.near(len(bounds), tt.x0, tt.z0, tt.x1, tt.z1)
		for i, ok := n.next(); ok; i, ok = n.next() {
			got = append(got, i)
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("walk over (%v, %v)–(%v, %v): %v, want %v", tt.x0, tt.z0, tt.x1, tt.z1, got, tt.want)
		}
	}
}
