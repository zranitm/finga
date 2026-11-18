package load_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
	"finga/internal/shared/kernel"
)

// guarded is chained's data with C21's block: the Beginner's Guard, granted, its counter Strike right after a block.
func guarded() map[string]string {
	files := chained()
	files["classes.yaml"] = strings.Replace(files["classes.yaml"], "    skills: [strike, second_wind, lunge,", "    skills: [guard, strike, second_wind, lunge,", 1)
	files["classes.yaml"] = strings.Replace(files["classes.yaml"], "      - root: roll_back\n", "      - root: guard\n        follow:\n          - {skill: strike, when: {blocked: 1500ms}}\n      - root: roll_back\n", 1)
	files["skills.yaml"] += `  - id: guard
    icon: lorc/checked-shield
    kind: active
    effect: block
    granted: true
    energy: 0
    cooldown: 6s
    length: 600ms
    block: 600ms
    stages:
      - {at: 0, name: Guard, description: "Blocks."}
`
	return files
}

// C21 §3: a block is granted, free, as long as its action, and may carry a move; its counter follows on blocked; actions.yaml gives its arc, as the cosine of its half, and its energy.
func TestABlockLoads(t *testing.T) {
	files := guarded()
	files["skills.yaml"] = strings.Replace(files["skills.yaml"], "    block: 600ms\n", "    block: 600ms\n    move: {kind: leap, distance: 2, time: 300ms, toward: back}\n", 1)
	d, err := load.Dir(write(t, files))
	if err != nil {
		t.Fatal(err)
	}
	s, _ := d.Skill("guard")
	st := s.Stages[0]
	if s.Effect != gamedata.BlockEffect || !s.Granted || st.Block != 600*time.Millisecond || st.Energy != 0 || st.Cancelable || st.Move.Distance != 2 {
		t.Fatalf("guard %+v", s)
	}
	beginner, _ := d.Class("beginner")
	if f, ok := beginner.FollowUp("guard", "strike"); !ok || f.When.Blocked != 1500*time.Millisecond {
		t.Fatalf("Guard's chain %+v", beginner.Chains)
	}
	if b, e := d.Block(), d.Energy(); b.Arc != 180 || math.Abs(b.Front) > 1e-12 || e.PerBlock != 2 {
		t.Fatalf("block %+v, energy %+v; want a 180° arc, the front from a dot product of 0, and 2 energy", b, e)
	}
}

func TestBlockRejections(t *testing.T) {
	tests := []struct {
		name, file, old, replace string
		wantAll                  []string
	}{
		{"not granted", "skills.yaml", "    effect: block\n    granted: true\n", "    effect: block\n", []string{"guard", "granted with its class"}},
		{"costing energy", "skills.yaml", "    effect: block\n    granted: true\n    energy: 0\n", "    effect: block\n    granted: true\n    energy: 1\n", []string{"guard", "costs no energy"}},
		{"a block skill without a block", "skills.yaml", "    block: 600ms\n", "", []string{"guard", "only one, has a block"}},
		{"a block below 0", "skills.yaml", "    block: 600ms\n", "    block: -100ms\n", []string{"guard", "above 0"}},
		{"a block past the skill's length", "skills.yaml", "    block: 600ms\n", "    block: 700ms\n", []string{"guard", "at most the skill's length"}},
		{"a block skill cancelable", "skills.yaml", "    block: 600ms\n", "    block: 600ms\n    cancelable: true\n", []string{"guard", "isn't cancelable"}},
		{"a block skill with a shape", "skills.yaml", "    block: 600ms\n", "    block: 600ms\n    shape: {kind: area, radius: 2}\n", []string{"guard", "no shape"}},
		{"a block on another skill", "skills.yaml", "    cooldown: 6s\n    length: 1s\n    power: {base: 1.5}\n", "    cooldown: 6s\n    length: 1s\n    block: 500ms\n    power: {base: 1.5}\n", []string{"power_shot", "only one, has a block"}},
		{"blocked under 0.2 s", "classes.yaml", "{blocked: 1500ms}", "{blocked: 100ms}", []string{"guard", "after, dodged and blocked run from"}},
		{"blocked over 5 s", "classes.yaml", "{blocked: 1500ms}", "{blocked: 6s}", []string{"guard", "after, dodged and blocked run from"}},
		{"blocked beside another key", "classes.yaml", "{blocked: 1500ms}", "{blocked: 1500ms, far: 3}", []string{"guard", "exactly one"}},
		{"actions.yaml without block", "actions.yaml", "block: {arc: 180}\n", "", []string{"actions.yaml", "block"}},
		{"an arc of 0", "actions.yaml", "block: {arc: 180}", "block: {arc: 0}", []string{"actions.yaml", "arc"}},
		{"an arc past 360", "actions.yaml", "block: {arc: 180}", "block: {arc: 361}", []string{"actions.yaml", "arc"}},
		{"a block's energy below 0", "actions.yaml", "per_block: 2", "per_block: -1", []string{"actions.yaml", "per_block"}},
		{"a block's energy past the max", "actions.yaml", "per_block: 2", "per_block: 11", []string{"actions.yaml", "per_block"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := guarded()
			before := files[tt.file]
			files[tt.file] = strings.Replace(before, tt.old, tt.replace, 1)
			if files[tt.file] == before {
				t.Fatalf("replacement %q not found in %s", tt.old, tt.file)
			}
			_, err := load.Dir(write(t, files))
			if err == nil {
				t.Fatal("no error")
			}
			for _, want := range tt.wantAll {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// blocks is C21's section 3 table: each class's block, its guard's time and the action's length, its cooldown, a leap back, and its counter.
var blocks = map[string]struct {
	class            kernel.Class
	length, cooldown time.Duration
	leap             float64
	counter          string
}{
	"guard":   {"beginner", 600 * time.Millisecond, 6 * time.Second, 0, "strike"},
	"bastion": {"warrior", 600 * time.Millisecond, 2 * time.Second, 0, "ground_slam"},
	"parry":   {"thief", 400 * time.Millisecond, 4 * time.Second, 0, "flurry"},
	"deflect": {"bowman", 600 * time.Millisecond, 6 * time.Second, 2, "power_shot"},
	"ward":    {"magician", 800 * time.Millisecond, 7 * time.Second, 0, "frost_nova"},
}
