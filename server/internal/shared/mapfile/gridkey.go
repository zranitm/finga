package mapfile

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"math"

	"finga/internal/shared/sim"
)

// contentKey is what a walk grid depends on, hashed: every exported field of the geometry and the params, in a fixed order, each slice with its length so two don't run together. Two geometries that differ in any of them have different keys, which TestEveryFieldOfTheGeometryAndParamsIsInTheKey holds by reflection; the unexported fields are derived from these.
func contentKey(g sim.Geometry, p sim.Params) [sha256.Size]byte {
	h := &keyWriter{Hash: sha256.New()}
	h.floats(p.Dt, p.WalkSpeed, p.JumpVelocity, p.Gravity, p.MaxFallSpeed, p.Radius, p.Height, p.EnemyHeight, p.Step, p.MaxSlope, p.Snap, p.Sprint.After, p.Sprint.Speed)
	h.floats(g.SizeX, g.SizeZ, g.Terrain.Cell)
	h.ints(g.Terrain.NX, g.Terrain.NZ)
	h.slice(len(g.Terrain.Heights))
	h.floatSlice(g.Terrain.Heights)
	h.slice(len(g.Terrain.Barrier))
	h.bools(g.Terrain.Barrier)
	h.slice(len(g.Boxes))
	for _, b := range g.Boxes {
		h.floats(b.X, b.Z, b.HalfW, b.HalfD, b.UX, b.UZ, b.Top)
	}
	h.slice(len(g.Cylinders))
	for _, c := range g.Cylinders {
		h.floats(c.X, c.Z, c.Radius, c.Top)
	}
	h.slice(len(g.Blockers))
	for _, b := range g.Blockers {
		h.floats(b.X, b.Z, b.Radius, b.Bottom, b.Top)
	}
	var key [sha256.Size]byte
	h.Sum(key[:0])
	return key
}

// keyWriter feeds numbers to a hash by their bits, through a buffer, so a million heights make a few thousand writes.
type keyWriter struct {
	hash.Hash
	buf [4096]byte
	n   int
}

func (w *keyWriter) put(v uint64) {
	if w.n+8 > len(w.buf) {
		w.flush()
	}
	binary.LittleEndian.PutUint64(w.buf[w.n:], v)
	w.n += 8
}

func (w *keyWriter) flush() {
	_, _ = w.Write(w.buf[:w.n]) // a hash's Write never fails
	w.n = 0
}

// Sum appends the hash of everything put, flushing the buffer first.
func (w *keyWriter) Sum(b []byte) []byte {
	w.flush()
	return w.Hash.Sum(b)
}

func (w *keyWriter) floats(vs ...float64) {
	for _, v := range vs {
		w.put(math.Float64bits(v))
	}
}

func (w *keyWriter) floatSlice(vs []float64) {
	for _, v := range vs {
		w.put(math.Float64bits(v))
	}
}

func (w *keyWriter) ints(vs ...int) {
	for _, v := range vs {
		w.put(uint64(v)) //nolint:gosec // a dimension, hashed by its bits
	}
}

func (w *keyWriter) slice(n int) { w.put(uint64(n)) } //nolint:gosec // a length

// bools packs 64 flags to a word.
func (w *keyWriter) bools(vs []bool) {
	var word uint64
	for i, v := range vs {
		if v {
			word |= 1 << (i % 64)
		}
		if i%64 == 63 {
			w.put(word)
			word = 0
		}
	}
	if len(vs)%64 != 0 {
		w.put(word)
	}
}
