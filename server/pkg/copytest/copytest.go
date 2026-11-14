// Package copytest checks that a conversion carries every field: it fills each field with a value no other has, converts, and finds where each value landed.
package copytest

import (
	"fmt"
	"maps"
	"math/bits"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Match is how a conversion's fields pair up, by path: the field names from the root, a map's keys as key and its values as value, indices left out (Skills.Points.key).
type Match struct {
	Renames  map[string]string // a source path, or a prefix of one: the target path its values land in
	Skip     map[string]string // a source path, or a prefix of one, the target doesn't take: why
	Computed map[string]string // a target path no source field copies into, so it may hold anything: why
	// Enums holds the value each enum type takes, as a counted one could read as unknown; their fields are left to the enum's own tests.
	Enums map[reflect.Type]any
}

// Diff converts fills of S, each field with its own value, and lists what a conversion lost or holds under another path than its own or its rename, each path of the target no source value reached, and each declared path neither side has.
// The booleans, which have two values only, take a pattern per fill: all false, all true, then bit r of each one's number from 1 and its negation, so any two differ in some fill.
func Diff[S, T any](m Match, convert func(S) T) []string {
	var out []string
	run := func(pattern func(i int) bool) int {
		f := &filler{enums: m.Enums, pattern: pattern}
		var src S
		f.fill(reflect.ValueOf(&src).Elem())
		out = append(out, m.diff(src, convert(src))...)
		return f.bools
	}
	booleans := run(func(int) bool { return false })
	run(func(int) bool { return true })
	for r := range bits.Len(uint(booleans)) {
		run(func(i int) bool { return (i+1)>>r&1 == 1 })
		run(func(i int) bool { return (i+1)>>r&1 == 0 })
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// filler sets every exported field it reaches to a value no other field has, so a field a conversion drops or swaps shows; the i-th boolean it fills takes pattern(i).
type filler struct {
	enums   map[reflect.Type]any
	pattern func(i int) bool
	n       int64
	bools   int
}

func (f *filler) fill(v reflect.Value) {
	if e, ok := f.enums[v.Type()]; ok {
		v.Set(reflect.ValueOf(e))
		return
	}
	f.n++
	switch {
	case v.Type() == reflect.TypeFor[time.Time]():
		v.Set(reflect.ValueOf(time.Unix(f.n, 0)))
		return
	case v.Type() == reflect.TypeFor[time.Duration]():
		v.SetInt(int64(time.Duration(f.n) * time.Millisecond))
		return
	}
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(f.pattern(f.bools))
		f.bools++
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if v.OverflowInt(f.n) {
			panic(fmt.Sprintf("copytest: %d overflows a %s, so its values would repeat; pin it in Enums", f.n, v.Type()))
		}
		v.SetInt(f.n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if v.OverflowUint(uint64(f.n)) {
			panic(fmt.Sprintf("copytest: %d overflows a %s, so its values would repeat; pin it in Enums", f.n, v.Type()))
		}
		v.SetUint(uint64(f.n))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(f.n) + 0.5)
	case reflect.String:
		v.SetString(fmt.Sprintf("s%d", f.n))
	case reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 { // an ID: the count in its last bytes, so no two match
			for i := range min(v.Len(), 8) {
				v.Index(v.Len() - 1 - i).SetUint(uint64(byte(f.n >> (8 * i))))
			}
			return
		}
		for i := range v.Len() {
			f.fill(v.Index(i))
		}
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 2, 2))
		for i := range 2 {
			f.fill(v.Index(i))
		}
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
		for range 2 {
			k, e := reflect.New(v.Type().Key()).Elem(), reflect.New(v.Type().Elem()).Elem()
			f.fill(k)
			f.fill(e)
			v.SetMapIndex(k, e)
		}
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		f.fill(p.Elem())
		v.Set(p)
	case reflect.Interface: // left nil: no fill can choose its type, so the caller fills one and checks it apart
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				f.fill(v.Field(i))
			}
		}
	default:
		panic(fmt.Sprintf("copytest: no value for a %s", v.Type()))
	}
}

// diff finds each value of from in to by what it reads as; booleans, which repeat, are compared at their path.
func (m Match) diff(from, to any) []string {
	src, dst := leaves{enums: m.Enums}, leaves{enums: m.Enums}
	src.add("", reflect.ValueOf(from))
	dst.add("", reflect.ValueOf(to))
	out := stale("source", src, m.Renames, m.Skip)
	out = append(out, stale("target", dst, m.Computed)...)
	at := map[string][]string{} // a target value: the paths holding it
	for path, vs := range dst.values {
		for _, v := range vs {
			at[v] = append(at[v], path)
		}
	}
	reached := map[string]bool{}
	for _, path := range slices.Sorted(maps.Keys(src.values)) {
		if m.skipped(path) {
			continue
		}
		want := m.renamed(path)
		reached[want] = true
		for _, v := range src.values[path] {
			if v == "" || v == "0" {
				continue
			}
			if !slices.Contains(at[v], want) {
				if len(at[v]) == 0 {
					out = append(out, fmt.Sprintf("%s: %s is lost", path, v))
				} else {
					out = append(out, fmt.Sprintf("%s: %s landed in %v, not %s", path, v, at[v], want))
				}
			}
		}
	}
	for _, path := range slices.Sorted(maps.Keys(src.bools)) {
		if m.skipped(path) {
			continue
		}
		want := m.renamed(path)
		reached[want] = true
		if got, ok := dst.bools[want]; !ok {
			out = append(out, fmt.Sprintf("%s: the target has no %s", path, want))
		} else if !slices.Equal(got, src.bools[path]) {
			out = append(out, fmt.Sprintf("%s: %v became %v at %s", path, src.bools[path], got, want))
		}
	}
	unset := slices.Concat(slices.Collect(maps.Keys(dst.values)), slices.Collect(maps.Keys(dst.bools)))
	slices.Sort(unset)
	for _, path := range slices.Compact(unset) {
		if _, ok := m.Computed[path]; ok || reached[path] {
			continue
		}
		out = append(out, fmt.Sprintf("%s: no source field sets it", path))
	}
	return out
}

// skipped is whether the longest prefix of path that is skipped or renamed is skipped, so a rename inside a skipped struct still counts.
func (m Match) skipped(path string) bool {
	for p := path; p != ""; p = parent(p) {
		if _, ok := m.Skip[p]; ok {
			return true
		}
		if _, ok := m.Renames[p]; ok {
			return false
		}
	}
	return false
}

// renamed is the target path of a source path: its own, or its longest renamed prefix's rename with the rest after it; a prefix renamed to "" lands at the root.
func (m Match) renamed(path string) string {
	for p := path; p != ""; p = parent(p) {
		if r, ok := m.Renames[p]; ok {
			return strings.TrimPrefix(r+path[len(p):], ".")
		}
	}
	return path
}

func parent(path string) string {
	before, _, found := strings.CutLast(path, ".")
	if !found {
		return ""
	}
	return before
}

// leaves are a value's plain values by path, each as it reads; booleans apart, in order; and every path its type has, empty or not.
type leaves struct {
	enums  map[reflect.Type]any
	values map[string][]string
	bools  map[string][]bool
	paths  map[string]bool
}

// stale lists each declared path the side has no field at, which would otherwise hide a field added back under that name.
func stale(side string, l leaves, declared ...map[string]string) []string {
	var out []string
	for _, d := range declared {
		for path := range d {
			if !l.paths[path] {
				out = append(out, fmt.Sprintf("%s: the %s has no such path", path, side))
			}
		}
	}
	return out
}

func (l *leaves) add(path string, v reflect.Value) {
	if l.values == nil {
		l.values, l.bools, l.paths = map[string][]string{}, map[string][]bool{}, map[string]bool{}
	}
	l.paths[path] = true
	if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil() {
		l.addType(path, v.Type(), nil)
		return
	}
	if _, enum := l.enums[v.Type()]; enum {
		return
	}
	if text, ok := read(v); ok {
		l.values[path] = append(l.values[path], text)
		return
	}
	switch v.Kind() {
	case reflect.Bool:
		l.bools[path] = append(l.bools[path], v.Bool())
	case reflect.Pointer, reflect.Interface:
		l.add(path, v.Elem())
	case reflect.Slice, reflect.Array:
		if v.Len() == 0 {
			l.addType(path, v.Type(), nil)
		}
		for i := range v.Len() {
			l.add(path, v.Index(i))
		}
	case reflect.Map: // by key, so booleans keep an order
		if v.Len() == 0 {
			l.addType(path, v.Type(), nil)
		}
		keys := v.MapKeys()
		slices.SortFunc(keys, func(a, b reflect.Value) int { return strings.Compare(fmt.Sprint(a), fmt.Sprint(b)) })
		for _, k := range keys {
			l.add(join(path, "key"), k)
			l.add(join(path, "value"), v.MapIndex(k))
		}
	case reflect.Struct: // an embedded struct's fields by their promoted names, not under the struct's too
		for _, field := range reflect.VisibleFields(v.Type()) {
			if !field.IsExported() || field.Anonymous && embedsStruct(field.Type) {
				continue
			}
			if f, err := v.FieldByIndexErr(field.Index); err == nil {
				l.add(join(path, field.Name), f)
			} else { // promoted through a nil pointer
				l.addType(join(path, field.Name), field.Type, nil)
			}
		}
	default:
		panic(fmt.Sprintf("copytest: can't read a %s at %s", v.Type(), path))
	}
}

// addType adds the paths a value of type t at path would have, for one that is nil or empty; open holds the types it is inside, so a type that holds itself ends.
func (l *leaves) addType(path string, t reflect.Type, open map[reflect.Type]bool) {
	l.paths[path] = true
	if _, enum := l.enums[t]; enum || open[t] || leaf(t) {
		return
	}
	if open == nil {
		open = map[reflect.Type]bool{}
	}
	open[t] = true
	defer delete(open, t)
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		l.addType(path, t.Elem(), open)
	case reflect.Map:
		l.addType(join(path, "key"), t.Key(), open)
		l.addType(join(path, "value"), t.Elem(), open)
	case reflect.Struct:
		for _, field := range reflect.VisibleFields(t) {
			if field.IsExported() && (!field.Anonymous || !embedsStruct(field.Type)) {
				l.addType(join(path, field.Name), field.Type, open)
			}
		}
	}
}

// leaf is whether read reads a value of type t whole.
func leaf(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return true
	case reflect.Array:
		return t.Implements(reflect.TypeFor[fmt.Stringer]())
	}
	return t == reflect.TypeFor[time.Time]()
}

func embedsStruct(t reflect.Type) bool {
	return t.Kind() == reflect.Struct || t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct
}

// read is a plain value as text, the same for the same value in another type: a duration in milliseconds, a time in seconds, an ID as its text.
func read(v reflect.Value) (string, bool) {
	switch {
	case v.Type() == reflect.TypeFor[time.Duration]():
		return strconv.FormatInt(time.Duration(v.Int()).Milliseconds(), 10), true
	case v.Type() == reflect.TypeFor[time.Time]():
		if t := v.Interface().(time.Time); !t.IsZero() {
			return strconv.FormatInt(t.Unix(), 10), true
		}
		return "", true
	case v.Kind() == reflect.Array:
		if s, ok := reflect.TypeAssert[fmt.Stringer](v); ok {
			return s.String(), true
		}
		return "", false
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10), true
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64), true
	case reflect.String:
		return v.String(), true
	}
	return "", false
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}
