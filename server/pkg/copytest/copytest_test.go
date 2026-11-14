package copytest_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"finga/pkg/copytest"
)

type id [16]byte

func (i id) String() string { return string(i[:]) }

type (
	kind int
	part struct{ Low, High int }
	from struct {
		ID       id
		Name     string
		Count    int32
		Lasts    time.Duration
		Parts    []part
		Best     *part
		Scores   map[string]int
		At       time.Time
		Open     bool
		Shut     bool
		Kind     kind
		Internal string
		Err      error // left nil
	}
	to struct {
		ID       string
		Title    string
		Count    int64
		LastsMs  int64
		Parts    []part
		Best     *part
		Scores   map[string]int
		At       time.Time
		Open     bool
		Shut     bool
		Kind     kind
		Computed int
	}
)

var match = copytest.Match{
	Renames:  map[string]string{"Name": "Title", "Lasts": "LastsMs"},
	Skip:     map[string]string{"Internal": "kept inside"},
	Computed: map[string]string{"Computed": "worked out, not copied"},
	Enums:    map[reflect.Type]any{reflect.TypeFor[kind](): kind(2)},
}

// copied is the conversion under test, with what a bug does to it.
func copied(bug func(*to)) func(from) to {
	return func(f from) to {
		t := to{
			ID: f.ID.String(), Title: f.Name, Count: int64(f.Count), LastsMs: f.Lasts.Milliseconds(), Parts: slices.Clone(f.Parts), Best: f.Best,
			Scores: f.Scores, At: f.At, Open: f.Open, Shut: f.Shut, Kind: f.Kind, Computed: 1,
		}
		if bug != nil {
			bug(&t)
		}
		return t
	}
}

func TestDiffFindsWhatAConversionLosesOrSwaps(t *testing.T) {
	if got := copytest.Diff(match, copied(nil)); len(got) != 0 {
		t.Fatalf("a faithful copy: %v", got)
	}
	for name, bug := range map[string]func(*to){
		"a dropped field":       func(t *to) { t.Title = "" },
		"two fields swapped":    func(t *to) { t.Parts[0].Low, t.Parts[0].High = t.Parts[0].High, t.Parts[0].Low },
		"a lost element":        func(t *to) { t.Parts = t.Parts[:1] },
		"a pointer left unset":  func(t *to) { t.Best = nil },
		"another time":          func(t *to) { t.At = t.At.Add(time.Second) },
		"a duration in seconds": func(t *to) { t.LastsMs /= 1000 },
		"an ID's other text":    func(t *to) { t.ID = "zz" },
		"a map entry lost":      func(t *to) { t.Scores = map[string]int{} },
		"a dropped boolean":     func(t *to) { t.Open = false },
		"two booleans swapped":  func(t *to) { t.Open, t.Shut = t.Shut, t.Open },
		"a boolean negated":     func(t *to) { t.Shut = !t.Shut },
		"a computed field kept": func(t *to) { t.Computed = 0 }, // allowed: it may hold anything
	} {
		got := copytest.Diff(match, copied(bug))
		if allowed := name == "a computed field kept"; allowed != (len(got) == 0) {
			t.Errorf("%s: found %v", name, got)
		}
	}
	if got := copytest.Diff(copytest.Match{Enums: match.Enums}, copied(nil)); len(got) < 3 {
		t.Errorf("without its renames, skips and computed fields, a match finds only %v", got)
	}
}

func TestEveryFieldGetsItsOwnValue(t *testing.T) {
	var got from
	copytest.Diff(match, func(f from) to { got = f; return copied(nil)(f) })
	seen := map[int64]bool{}
	for _, n := range []int64{int64(got.Count), int64(got.Lasts / time.Millisecond), int64(got.Parts[0].Low), int64(got.Parts[0].High), int64(got.Parts[1].Low), int64(got.Best.Low), got.At.Unix()} {
		if n == 0 || seen[n] {
			t.Fatalf("a value given twice or zero: %d in %+v", n, got)
		}
		seen[n] = true
	}
	if got.Name == "" || got.ID == (id{}) || len(got.Scores) != 2 || got.Kind != 2 || got.Open == got.Shut {
		t.Fatalf("left empty, or booleans alike: %+v", got)
	}
}

type (
	nested struct {
		Inner  struct{ Name string }
		Points struct{ Spent, Unspent int }
		Skills map[string]int
		On     bool
	}
	flat struct {
		Name                 string
		Spent, UnspentPoints int
		Skills               []skill
		On                   bool
	}
	skill struct {
		ID     string
		Points int
	}
)

func flatten(swap bool) func(nested) flat {
	return func(n nested) flat {
		out := flat{Name: n.Inner.Name, Spent: n.Points.Spent, UnspentPoints: n.Points.Unspent, On: n.On}
		if swap {
			out.Spent, out.UnspentPoints = out.UnspentPoints, out.Spent
		}
		for id, p := range n.Skills {
			out.Skills = append(out.Skills, skill{ID: id, Points: p})
		}
		return out
	}
}

// A rename names where a value lands however the shape changes: a nested field flattened, or a whole struct's, a map's keys and values in a list.
func TestRenamesFollowAChangeOfShape(t *testing.T) {
	m := copytest.Match{Renames: map[string]string{
		"Inner": "", "Points.Spent": "Spent", "Points.Unspent": "UnspentPoints", "Skills.key": "Skills.ID", "Skills.value": "Skills.Points",
	}}
	if got := copytest.Diff(m, flatten(false)); len(got) != 0 {
		t.Fatalf("a faithful copy: %v", got)
	}
	if got := copytest.Diff(copytest.Match{Renames: m.Renames, Skip: map[string]string{"Points": "a rename inside wins"}}, flatten(false)); len(got) != 0 {
		t.Fatalf("renamed fields inside a skipped struct: %v", got)
	}
	if got := copytest.Diff(m, flatten(true)); len(got) != 2 {
		t.Fatalf("two flattened fields swapped: %v", got)
	}
}

type (
	Range   struct{ Low, High int }
	wrapped struct {
		*Range
		Name string
	}
	flatPart struct {
		Low, High int
		Name      string
	}
)

// An embedded struct's fields count once, by their promoted names, through a pointer too; a nil one holds none.
func TestAnEmbeddedStructsFieldsArePromoted(t *testing.T) {
	if got := copytest.Diff(copytest.Match{}, func(w wrapped) flatPart { return flatPart{w.Low, w.High, w.Name} }); len(got) != 0 {
		t.Fatalf("a faithful copy: %v", got)
	}
	if got := copytest.Diff(copytest.Match{}, func(w wrapped) wrapped { return wrapped{Name: w.Name} }); len(got) != 2 {
		t.Fatalf("a nil embedded struct: %v; want its two fields lost", got)
	}
}

type flags struct{ A, B, C bool }

// Any two booleans differ in some fill, so swapping the first and the third shows though they sit two apart.
func TestEveryTwoBooleansDifferInSomeFill(t *testing.T) {
	if got := copytest.Diff(copytest.Match{}, func(f flags) flags { return flags{A: f.C, B: f.B, C: f.A} }); len(got) == 0 {
		t.Fatal("the first and third booleans swapped: no difference found")
	}
	for name, convert := range map[string]func(flags) flags{
		"one dropped": func(f flags) flags { return flags{B: f.B, C: f.C} },
		"one forced":  func(f flags) flags { return flags{A: true, B: f.B, C: f.C} },
		"one negated": func(f flags) flags { return flags{A: f.A, B: !f.B, C: f.C} },
	} {
		if got := copytest.Diff(copytest.Match{}, convert); len(got) == 0 {
			t.Errorf("%s: no difference found", name)
		}
	}
}

// A value past what a small integer holds would wrap and repeat, so the fill refuses it.
func TestAnIntegerTooSmallForItsValueIsRefused(t *testing.T) {
	type wide struct {
		Pad   [300]int
		Small uint8
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a uint8 took the 301st value")
		}
	}()
	copytest.Diff(copytest.Match{}, func(w wide) wide { return w })
}

// A declared path neither side has fails, so a stale one can't hide a field added back under its name; one inside an empty list or a nil pointer is still a path.
func TestADeclaredPathMustExist(t *testing.T) {
	for name, m := range map[string]copytest.Match{
		"a skip":     {Skip: map[string]string{"Gone": "removed"}},
		"a rename":   {Renames: map[string]string{"Gone": "Name"}},
		"a computed": {Computed: map[string]string{"Gone": "removed"}},
	} {
		if got := copytest.Diff(m, func(f flatPart) flatPart { return f }); len(got) == 0 {
			t.Errorf("%s of a path no side has: no difference found", name)
		}
	}
	empty := copytest.Match{
		Skip:     map[string]string{"Parts.Low": "dropped", "Parts.High": "dropped", "Best": "dropped"},
		Computed: map[string]string{"Parts.Low": "none", "Best.High": "none"},
	}
	type holder struct {
		Parts []part
		Best  *part
	}
	if got := copytest.Diff(empty, func(holder) holder { return holder{} }); len(got) != 0 {
		t.Fatalf("paths inside an empty list or a nil pointer: %v", got)
	}
}
