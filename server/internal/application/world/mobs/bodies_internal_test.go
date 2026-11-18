package mobs

import (
	"slices"
	"testing"
)

// A query over more cells than a walk merges goes through every body, in key order: 30 bodies 1 m apart, each in its own cell, asked for from the middle 12 m out.
func TestAWalkOverManyCellsGoesThroughEveryBody(t *testing.T) {
	c := newCells(1, 40, 40)
	in := make([][]int32, c.nx*c.nz)
	var all []int32
	for i := range int32(30) {
		x := float64(5 + i)
		in[c.of(x, 20)] = append(in[c.of(x, 20)], i)
		all = append(all, i)
	}
	w := near(c, in, all, playerIndex, 20, 20, 20, 20, 12)
	var got []int32
	for i, ok := w.next(); ok; i, ok = w.next() {
		got = append(got, i)
	}
	if !slices.Equal(got, all) {
		t.Fatalf("walked %v; want every body, %v", got, all)
	}
}

// A walk over a few cells merges them by key, though each cell's lie apart: bodies dealt round four cells come back in order.
func TestAWalkMergesCellsInKeyOrder(t *testing.T) {
	c := newCells(8, 16, 16)
	in := make([][]int32, c.nx*c.nz)
	for i := range int32(12) {
		k := int(i) % 4
		in[k] = append(in[k], i)
	}
	w := near(c, in, nil, playerIndex, 4, 4, 12, 12, 0)
	var got []int32
	for i, ok := w.next(); ok; i, ok = w.next() {
		got = append(got, i)
	}
	if want := []int32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}; !slices.Equal(got, want) {
		t.Fatalf("walked %v; want %v", got, want)
	}
}
