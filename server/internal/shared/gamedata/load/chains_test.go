package load_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
)

// chained is the roster with C18's chains, and Power Shot the Bowman's alone:
//   - the Beginner's Strike offers Lunge at a far lock, then Meteor after its hit, then Taunt at a stunned or frozen lock;
//   - its Roll Back offers Disengage after a dodge;
//   - the Warrior's own Strike offers Shadow Step at a far lock;
//   - its War Cry offers Shadow Step after its hit too.
func chained() map[string]string {
	files := roster()
	files["classes.yaml"] = strings.Replace(files["classes.yaml"], "    skills: [strike, second_wind, lunge,", `    chains:
      - root: strike
        follow:
          - {skill: lunge, when: {far: 3}}
          - {skill: meteor, when: {after: 2s}}
          - {skill: taunt, when: {controlled: [stun, freeze]}}
      - root: roll_back
        follow:
          - {skill: disengage, when: {dodged: 1500ms}}
    skills: [strike, second_wind, lunge,`, 1)
	files["classes.yaml"] = strings.Replace(files["classes.yaml"], "    auto_assign: {str: 3, vit: 2}\n", `    auto_assign: {str: 3, vit: 2}
    chains:
      - root: strike
        follow:
          - {skill: shadow_step, when: {far: 3}}
      - root: war_cry
        follow:
          - {skill: shadow_step, when: {after: 3s}}
`, 1)
	files["classes.yaml"] = strings.Replace(files["classes.yaml"], "    auto_assign: {dex: 3, agi: 2}\n", "    auto_assign: {dex: 3, agi: 2}\n    skills: [power_shot]\n", 1)
	files["skills.yaml"] += `  - id: power_shot
    icon: lorc/high-shot
    kind: active
    effect: damage
    damage: physical
    shape: {kind: shot, range: 12, width: 0.6}
    energy: 2
    cooldown: 6s
    length: 1s
    power: {base: 1.5}
    stages:
      - {at: 0, name: Power Shot, description: "A heavy arrow."}
`
	return files
}

// C18: each class keeps its own chains in order, a follow-up may sit in two of them, and a skill on its own key is a root or in no chain.
func TestChainsLoadPerClass(t *testing.T) {
	d, err := load.Dir(write(t, chained()))
	if err != nil {
		t.Fatal(err)
	}
	beginner, _ := d.Class("beginner")
	warrior, _ := d.Class("warrior")
	if got := beginner.Chains[0].Follow; len(got) != 3 || !reflect.DeepEqual(got[0], gamedata.FollowUp{Skill: "lunge", When: gamedata.ChainCondition{Far: 3}}) || got[1].When.After != 2*time.Second || !reflect.DeepEqual(got[2].When.Controlled, []string{"stun", "freeze"}) {
		t.Fatalf("the Beginner's Strike chain %+v, want Lunge at 3 m, Meteor 2 s after, then Taunt on a stun or a freeze", got)
	}
	if f, ok := beginner.FollowUp("roll_back", "disengage"); !ok || f.When.Dodged != 1500*time.Millisecond {
		t.Fatalf("Roll Back's Disengage %+v %v, want 1.5 s after a dodge", f, ok)
	}
	for _, c := range []struct {
		name       string
		class      gamedata.Class
		root, step string
		follows    bool
	}{
		{"the Warrior's Strike to Shadow Step", warrior, "strike", "shadow_step", true},
		{"the Warrior's War Cry to Shadow Step too", warrior, "war_cry", "shadow_step", true},
		{"not the Beginner's Strike to Lunge, for a Warrior", warrior, "strike", "lunge", false},
		{"not the Warrior's Strike to Shadow Step, for a Beginner", beginner, "strike", "shadow_step", false},
	} {
		if _, ok := c.class.FollowUp(c.root, c.step); ok != c.follows {
			t.Errorf("%s: follows %v, want %v", c.name, ok, c.follows)
		}
	}
	for _, c := range []struct {
		skill   string
		keyable bool
	}{{"strike", true}, {"roll_back", true}, {"lunge", false}, {"disengage", false}, {"barrier", true}} {
		if got := beginner.Keyable(c.skill); got != c.keyable {
			t.Errorf("a Beginner's %s on its own key %v, want %v", c.skill, got, c.keyable)
		}
	}
	if !warrior.Keyable("lunge") {
		t.Error("a Warrior's Lunge, in none of its chains, can't sit on a key")
	}
}

func TestChainRejections(t *testing.T) {
	const strikeChain = "          - {skill: lunge, when: {far: 3}}\n          - {skill: meteor, when: {after: 2s}}\n          - {skill: taunt, when: {controlled: [stun, freeze]}}\n"
	tests := []struct {
		name, old, replace string
		wantAll            []string
	}{
		{"a root the class can't learn", "      - root: roll_back\n", "      - root: power_shot\n", []string{"classes.yaml", "beginner", "power_shot", "can learn"}},
		{"a follow-up the class can't learn", "{skill: lunge, when: {far: 3}}", "{skill: power_shot, when: {far: 3}}", []string{"beginner", "chain strike", "power_shot", "can learn"}},
		{"a follow-up that's no skill", "{skill: lunge, when: {far: 3}}", "{skill: lungee, when: {far: 3}}", []string{"beginner", "lungee", "can learn"}},
		{"a passive", "{skill: lunge, when: {far: 3}}", "{skill: second_wind, when: {far: 3}}", []string{"beginner", "second_wind", "passive"}},
		{"a root starting two chains", "      - root: roll_back\n", "      - root: strike\n", []string{"beginner", "strike roots two chains"}},
		{"a follow-up that's its own root", "{skill: lunge, when: {far: 3}}", "{skill: strike, when: {far: 3}}", []string{"beginner", "chain strike", "strike is its root"}},
		{"a follow-up listed twice", strikeChain, strikeChain + "          - {skill: lunge, when: {after: 2s}}\n", []string{"beginner", "lunge", "listed twice"}},
		{"a chain with no follow-up", "      - root: roll_back\n        follow:\n          - {skill: disengage, when: {dodged: 1500ms}}\n", "      - root: roll_back\n        follow: []\n", []string{"beginner", "at least one follow-up"}},
		{"a when with no key", "{skill: lunge, when: {far: 3}}", "{skill: lunge, when: {}}", []string{"beginner", "lunge", "exactly one"}},
		{"a when with two keys", "{skill: lunge, when: {far: 3}}", "{skill: lunge, when: {far: 3, after: 2s}}", []string{"beginner", "lunge", "exactly one"}},
		{"an after too short", "when: {after: 2s}", "when: {after: 100ms}", []string{"meteor", "200ms to 5s"}},
		{"an after too long", "when: {after: 2s}", "when: {after: 6s}", []string{"meteor", "200ms to 5s"}},
		{"a dodged too long", "when: {dodged: 1500ms}", "when: {dodged: 6s}", []string{"disengage", "200ms to 5s"}},
		{"a far of 0", "when: {far: 3}}\n          - {skill: meteor", "when: {far: 0}}\n          - {skill: meteor", []string{"lunge", "far is more than 0"}},
		{"controlled on a soft status", "controlled: [stun, freeze]", "controlled: [stun, slow]", []string{"taunt", "slow isn't a hard status", "statuses.yaml"}},
		{"controlled on no status", "controlled: [stun, freeze]", "controlled: [stun, stunned]", []string{"taunt", "stunned isn't a hard status"}},
		{"controlled naming none", "controlled: [stun, freeze]", "controlled: []", []string{"taunt", "at least one status"}},
		{"controlled beside another key", "{controlled: [stun, freeze]}", "{controlled: [stun, freeze], far: 3}", []string{"taunt", "exactly one"}},
		{"an unknown condition", "when: {far: 3}}\n          - {skill: meteor", "when: {near: 3}}\n          - {skill: meteor", []string{"classes.yaml", "near"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := chained()
			if !strings.Contains(files["classes.yaml"], tt.old) {
				t.Fatalf("the fixture has no %q", tt.old)
			}
			files["classes.yaml"] = strings.Replace(files["classes.yaml"], tt.old, tt.replace, 1)
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
