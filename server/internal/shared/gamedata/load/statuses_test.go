package load_test

import (
	"strings"
	"testing"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
)

// C19: each status loads with its group, blocks and factors, a factor it doesn't name being 1, and the rules every status shares.
func TestStatusesLoad(t *testing.T) {
	d, err := load.Dir(write(t, fixture()))
	if err != nil {
		t.Fatal(err)
	}
	if r := d.StatusRules(); r != (gamedata.StatusRules{SuperArmor: 0.5}) {
		t.Errorf("rules %+v", r)
	}
	one := gamedata.Status{MoveSpeed: 1, Accuracy: 1, Attack: 1, PhysicalDefense: 1, MagicDefense: 1, Moving: 1}
	with := func(f func(*gamedata.Status)) gamedata.Status {
		s := one
		f(&s)
		return s
	}
	for _, want := range []gamedata.Status{
		with(func(s *gamedata.Status) {
			s.ID, s.Group, s.Blocks = "stun", gamedata.Hard, gamedata.BlocksMove|gamedata.BlocksAct
		}),
		with(func(s *gamedata.Status) {
			s.ID, s.Group, s.Blocks, s.Shatter = "freeze", gamedata.Hard, gamedata.BlocksMove|gamedata.BlocksAct, 1.5
		}),
		with(func(s *gamedata.Status) {
			s.ID, s.Group, s.Blocks, s.Wakes = "sleep", gamedata.Hard, gamedata.BlocksMove|gamedata.BlocksAct, true
		}),
		with(func(s *gamedata.Status) { s.ID, s.Group, s.Blocks = "pin", gamedata.Hard, gamedata.BlocksMove }),
		with(func(s *gamedata.Status) { s.ID, s.Group, s.MoveSpeed = "slow", gamedata.Soft, 0.6 }),
		with(func(s *gamedata.Status) { s.ID, s.Group, s.Blocks = "silence", gamedata.Soft, gamedata.BlocksSkills }),
		with(func(s *gamedata.Status) { s.ID, s.Group, s.Accuracy = "blind", gamedata.Soft, 0.5 }),
		with(func(s *gamedata.Status) { s.ID, s.Group, s.Every = "burn", gamedata.Dot, 500*time.Millisecond }),
		with(func(s *gamedata.Status) { s.ID, s.Group, s.Every, s.Moving = "bleed", gamedata.Dot, time.Second, 2 }),
		with(func(s *gamedata.Status) {
			s.ID, s.Group, s.PhysicalDefense, s.MagicDefense = "armor_break", gamedata.Weaken, 0.7, 0.7
		}),
		with(func(s *gamedata.Status) { s.ID, s.Group, s.Attack = "weaken", gamedata.Weaken, 0.75 }),
		with(func(s *gamedata.Status) { s.ID, s.Group = gamedata.LoseSight, gamedata.Aggro }),
	} {
		if got, ok := d.Status(want.ID); !ok || got != want {
			t.Errorf("%s: %+v, want %+v", want.ID, got, want)
		}
	}
	if _, ok := d.Status("confuse"); ok {
		t.Error("an unknown status found")
	}
}

// C19: statuses.yaml refuses what a status or the rules can't be.
func TestStatusesRejections(t *testing.T) {
	tests := []struct {
		name, old, replace string
		wantAll            []string
	}{
		{"super armor taking hard statuses", "{hard: immune, others: 0.5}", "{hard: halved, others: 0.5}", []string{"super_armor hard", "halved"}},
		{"super armor lengthening the rest", "{hard: immune, others: 0.5}", "{hard: immune, others: 1.5}", []string{"super_armor others"}},
		{"super armor ending the rest", "{hard: immune, others: 0.5}", "{hard: immune, others: 0}", []string{"super_armor others"}},
		{"a status without an id", "{id: pin, group: hard", "{group: hard", []string{"status 5", "id"}},
		{"a duplicate status", "{id: pin, group: hard", "{id: stun, group: hard", []string{"duplicate status stun"}},
		{"an unknown group", "{id: pin, group: hard", "{id: pin, group: odd", []string{"pin", "odd"}},
		{"an unknown block", "{id: pin, group: hard, blocks: [move]}", "{id: pin, group: hard, blocks: [jump]}", []string{"pin", "jump"}},
		{"a block twice", "{id: pin, group: hard, blocks: [move]}", "{id: pin, group: hard, blocks: [move, move]}", []string{"pin", "once"}},
		{"a factor of 0", "move_speed: 0.6", "move_speed: 0", []string{"slow", "move_speed"}},
		{"a factor below 0", "attack: 0.75", "attack: -0.75", []string{"weaken", "attack"}},
		{"a shatter below 0", "shatter: 1.5", "shatter: -1.5", []string{"freeze", "shatter"}},
		{"damage over time that never ticks", "{id: poison, group: dot, every: 1s}", "{id: poison, group: dot}", []string{"poison", "ticks every"}},
		{"a tick on a status not over time", "{id: slow, group: soft,", "{id: slow, group: soft, every: 1s,", []string{"slow", "ticks every"}},
		{"moving on a status not over time", "{id: slow, group: soft,", "{id: slow, group: soft, moving: 2,", []string{"slow", "moving"}},
		{"an aggro status the mobs don't know", "{id: taunt, group: aggro}", "{id: charm, group: aggro}", []string{"charm", "taunt or lose_sight"}},
		{"no knockdown", "  - {id: knockdown, group: hard, blocks: [move, act]}\n", "", []string{"knockdown"}},
		{"a knockdown that doesn't hold", "{id: knockdown, group: hard, blocks: [move, act]}", "{id: knockdown, group: hard, blocks: [move]}", []string{"knockdown", "move and act"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := fixture()
			before := files["statuses.yaml"]
			files["statuses.yaml"] = strings.Replace(before, tt.old, tt.replace, 1)
			if files["statuses.yaml"] == before {
				t.Fatalf("replacement %q not found", tt.old)
			}
			_, err := load.Dir(write(t, files))
			if err == nil {
				t.Fatal("no error")
			}
			for _, want := range append([]string{"statuses.yaml"}, tt.wantAll...) {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// slamStuns has the slam stun in the table, and the slime's hop poison as its own use.
func slamStuns() map[string]string {
	files := fixture()
	files["mobs.yaml"] = strings.Replace(files["mobs.yaml"], "cooldown: 5s, power: 1.5}", "cooldown: 5s, power: 1.5, status: {id: stun, time: 1s}}", 1)
	files["mobs.yaml"] = strings.Replace(files["mobs.yaml"], "attacks: [{use: hop}]", "attacks: [{use: hop, status: {id: poison, time: 4s, power: 0.15}}, {use: slam, status: {id: slow, time: 2s}}]", 1)
	return files
}

// C19: a mob's attack takes a status from the table, and a use's replaces it whole.
func TestMobAttackStatusesLoad(t *testing.T) {
	d, err := load.Dir(write(t, slamStuns()))
	if err != nil {
		t.Fatal(err)
	}
	slime, _ := d.Mob("green_slime")
	if got := slime.Attacks[0].Status; got != (gamedata.Inflict{Status: "poison", Time: 4 * time.Second, Power: 0.15}) {
		t.Errorf("hop's status %+v, want the use's poison", got)
	}
	if got := slime.Attacks[1].Status; got != (gamedata.Inflict{Status: "slow", Time: 2 * time.Second}) {
		t.Errorf("slam's status %+v, want the use's slow in place of the table's stun", got)
	}
	lord, _ := d.Mob("slime_lord")
	for _, a := range lord.Attacks {
		want := gamedata.Inflict{}
		if a.Name == "slam" {
			want = gamedata.Inflict{Status: "stun", Time: time.Second}
		}
		if a.Status != want {
			t.Errorf("the lord's %s: status %+v, want %+v", a.Name, a.Status, want)
		}
	}
}

// A mob's hard status holds a player shorter than that attack's cooldown, a knockdown for the down pose, so one mob alone can't hold them for as long as it stays in reach; soft ones may outlast it.
func TestAMobsHoldIsShorterThanItsAttacksCooldown(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edits [][2]string
		ok    bool
	}{
		{"a stun just short of the cooldown", [][2]string{{"status: {id: stun, time: 1s}", "status: {id: stun, time: 4999ms}"}}, true},
		{"a knockdown lying 1 s of a 5 s cooldown", [][2]string{{"status: {id: stun, time: 1s}", "status: {id: knockdown}"}}, true},
		{"a knockdown lying as long as the cooldown", [][2]string{{"status: {id: stun, time: 1s}", "status: {id: knockdown}"}, {"down: 1s,", "down: 5s,"}}, false},
		{"a slow outlasting it", [][2]string{{"status: {id: stun, time: 1s}", "status: {id: slow, time: 9s}"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files := slamStuns()
			for _, e := range tc.edits {
				before := files["mobs.yaml"]
				if files["mobs.yaml"] = strings.Replace(before, e[0], e[1], 1); files["mobs.yaml"] == before {
					t.Fatalf("replacement %q not found", e[0])
				}
			}
			_, err := load.Dir(write(t, files))
			if tc.ok && err != nil {
				t.Fatal(err)
			}
			if !tc.ok && (err == nil || !strings.Contains(err.Error(), "knockdown holds a player 5s")) {
				t.Fatalf("error %v; want the knockdown's 5 s down pose refused against the slam's 5 s cooldown", err)
			}
		})
	}
}

// C19: a mob's attack refuses an unknown status, a bad time or power, and aggro, which is the mobs' own.
func TestMobAttackStatusRejections(t *testing.T) {
	tests := []struct {
		name, old, replace string
		wantAll            []string
	}{
		{"an unknown status", "status: {id: stun, time: 1s}", "status: {id: daze, time: 1s}", []string{"slam", "daze"}},
		{"a stun with no time", "status: {id: stun, time: 1s}", "status: {id: stun}", []string{"slam", "stun lasts a time"}},
		{"a stun with power", "status: {id: stun, time: 1s}", "status: {id: stun, time: 1s, power: 1}", []string{"slam", "no power"}},
		{"a poison with no power", "status: {id: poison, time: 4s, power: 0.15}", "status: {id: poison, time: 4s}", []string{"green_slime", "hop", "poison has power"}},
		{"a taunt", "status: {id: stun, time: 1s}", "status: {id: taunt, time: 1s}", []string{"slam", "aggro"}},
		{"a lose_sight in a use", "status: {id: slow, time: 2s}", "status: {id: lose_sight, time: 2s}", []string{"green_slime", "slam", "aggro"}},
		{"a knockdown with a time", "status: {id: stun, time: 1s}", "status: {id: knockdown, time: 1s}", []string{"slam", "knockdown names no time"}},
		{"a stun as long as its attack's cooldown", "status: {id: stun, time: 1s}", "status: {id: stun, time: 5s}", []string{"slam", "stun holds a player 5s", "cooldown, 5s"}},
		{"a freeze past a use's cooldown", "status: {id: slow, time: 2s}", "status: {id: freeze, time: 6s}", []string{"green_slime", "slam", "freeze holds a player 6s"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := slamStuns()
			before := files["mobs.yaml"]
			files["mobs.yaml"] = strings.Replace(before, tt.old, tt.replace, 1)
			if files["mobs.yaml"] == before {
				t.Fatalf("replacement %q not found", tt.old)
			}
			_, err := load.Dir(write(t, files))
			if err == nil {
				t.Fatal("no error")
			}
			for _, want := range append([]string{"mobs.yaml"}, tt.wantAll...) {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}
