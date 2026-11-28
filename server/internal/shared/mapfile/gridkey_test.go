package mapfile

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"finga/internal/shared/sim"
)

// derived are the unexported fields of the geometry and the params, which come from the exported ones and so are not in the key: a new unexported field fails the test until it is named here or hashed.
var derived = map[string]bool{"Geometry.solids": true, "Geometry.blockers": true, "Terrain.steepX": true, "Terrain.steepZ": true}

// fullGeometry is a geometry with something in every slice, so each field of an element can be changed.
func fullGeometry() (sim.Geometry, sim.Params) {
	g := sim.Geometry{
		SizeX: 20, SizeZ: 30,
		Terrain:   sim.Terrain{Cell: 2, NX: 3, NZ: 2, Heights: []float64{0, 1, 2, 0, 1, 2}, Barrier: []bool{false, true, false, false, false, true}},
		Boxes:     []sim.Box{{X: 1, Z: 2, HalfW: 3, HalfD: 4, UX: 1, UZ: 0, Top: 5}},
		Cylinders: []sim.Cylinder{{X: 1, Z: 2, Radius: 3, Top: 4}},
		Blockers:  []sim.Blocker{{X: 1, Z: 2, Radius: 3, Bottom: 4, Top: 5}},
	}
	p := sim.Params{Dt: 1, WalkSpeed: 2, JumpVelocity: 3, Gravity: 4, MaxFallSpeed: 5, Radius: 6, Height: 7, EnemyHeight: 8, Step: 9, MaxSlope: 10, Snap: 11, Sprint: sim.Sprint{After: 12, Speed: 13}}
	return g, p
}

// fields lists the path of every exported field of v and its elements, calling each with v's own address to change: structs through their fields, slices through their first element.
func fields(t *testing.T, v reflect.Value, path string, visit func(path string, f reflect.Value)) {
	t.Helper()
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			f, name := v.Field(i), v.Type().Field(i).Name
			if !v.Type().Field(i).IsExported() {
				if !derived[v.Type().Name()+"."+name] {
					t.Errorf("%s.%s is unexported and not named as derived: hash it in contentKey, or name it in derived", path, name)
				}
				continue
			}
			fields(t, f, path+"."+name, visit)
		}
	case reflect.Slice:
		visit(path+" (length)", v)
		if v.Len() > 0 {
			fields(t, v.Index(0), path+"[0]", visit)
		}
	default:
		visit(path, v)
	}
}

// change makes f a different value.
func change(f reflect.Value) {
	switch f.Kind() {
	case reflect.Float64:
		f.SetFloat(f.Float() + 0.5)
	case reflect.Int:
		f.SetInt(f.Int() + 1)
	case reflect.Bool:
		f.SetBool(!f.Bool())
	case reflect.Slice:
		f.Set(reflect.Append(f, reflect.Zero(f.Type().Elem())))
	default:
		panic(fmt.Sprintf("no change for %s", f.Kind()))
	}
}

// Every exported field of the geometry and the params, each element's too, is in the key: a field the key missed would let two different maps share a walk grid, and players walk the wrong one. A slice's length counts, and a field added later fails here until it is hashed.
func TestEveryFieldOfTheGeometryAndParamsIsInTheKey(t *testing.T) {
	baseG, baseP := fullGeometry()
	base := contentKey(baseG, baseP)
	var paths []string
	for _, target := range []string{"Geometry", "Params"} {
		g, p := fullGeometry()
		root := reflect.ValueOf(&g).Elem()
		if target == "Params" {
			root = reflect.ValueOf(&p).Elem()
		}
		fields(t, root, target, func(path string, _ reflect.Value) { paths = append(paths, path) })
		for _, want := range slices.Clone(paths) {
			g, p := fullGeometry()
			r := reflect.ValueOf(&g).Elem()
			if target == "Params" {
				r = reflect.ValueOf(&p).Elem()
			}
			fields(t, r, target, func(path string, f reflect.Value) {
				if path == want {
					change(f)
				}
			})
			if want[:len(target)] == target && contentKey(g, p) == base {
				t.Errorf("changing %s leaves the key the same", want)
			}
		}
		paths = nil
	}
}

// The key is the same for the same content, whichever copy, and differs with a height.
func TestTheKeyIsTheContents(t *testing.T) {
	g, p := fullGeometry()
	g2, p2 := fullGeometry()
	if contentKey(g, p) != contentKey(g2, p2) {
		t.Fatal("equal geometries have different keys")
	}
	g2.Terrain.Heights[3] += 1e-9
	if contentKey(g, p) == contentKey(g2, p2) {
		t.Fatal("a height one part in a billion off made no difference")
	}
}
