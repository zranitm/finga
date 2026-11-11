package load_test

import (
	"cmp"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"finga/internal/gamedata"
	"finga/internal/gamedata/load"
	"finga/internal/kernel"
	"finga/internal/sim"
)

// wantMovement are the movement constants data/movement.yaml and the fixture both hold.
var wantMovement = sim.Params{
	Dt:           1.0 / 60,
	WalkSpeed:    200,
	JumpVelocity: 620,
	Gravity:      1900,
	MaxFallSpeed: 900,
	ClimbSpeed:   140,
	DropThrough:  0.25,
	Width:        24,
	Height:       56,
}

// fixture is a minimal valid data folder; each rejection row breaks one thing in it.
func fixture() map[string]string {
	return map[string]string{
		"movement.yaml": `
dt: 0.016666666666666666
walk_speed: 200
jump_velocity: 620
gravity: 1900
max_fall_speed: 900
climb_speed: 140
drop_through: 0.25
width: 24
height: 56
`,
		"loot.yaml": `
collect_delay: 1s
drop_life: 60s
room_check_every: 2s
`,
		"chat.yaml": `
line_max: 120
`,
		"classes.yaml": `
classes:
  - id: beginner
    name: Beginner
    look: {sprite: alien_blue, tint: 0xd8c8a8}
    attack: {kind: melee, damage: physical, width: 48, height: 48, cooldown: 700ms}
    base_stats: {str: 12, dex: 5, int: 4, agi: 5, vit: 10}
    starter_items: [{item: wooden_sword, quantity: 1}]
  - id: warrior
    name: Warrior
    parent: beginner
    look: {sprite: alien_blue}
    attack: {kind: melee, damage: physical, width: 64, height: 56, cooldown: 600ms}
    auto_assign: {str: 3, vit: 2}
  - id: bowman
    name: Bowman
    parent: beginner
    look: {sprite: alien_green}
    attack: {kind: ranged, damage: magic, range: 420, cooldown: 800ms}
    auto_assign: {dex: 3, agi: 2}
`,
		"stats.yaml": `
points_per_level: 5
per_point:
  str: {physical_attack: 1.5, max_hp: 2}
  dex: {physical_attack: 0.5, magic_attack: 0.5, accuracy: 1, crit_damage: 0.005}
  int: {magic_attack: 1.5, max_mp: 3, mp_regen: 0.05, magic_defense: 0.5}
  agi: {evasion: 1}
  vit: {max_hp: 10, physical_defense: 1, magic_defense: 0.5, hp_regen: 0.05}
diminishing:
  crit_rate:      {stat: dex, base: 0.05, limit: 0.60, k: 200}
  attack_speed:   {stat: agi, limit: 0.50, k: 150}
  cooldown_speed: {stat: int, limit: 0.40, k: 200}
  move_speed:     {stat: agi, limit: 0.30, k: 200}
limits: {penetration: 0.50, hit_chance: [0.20, 0.99]}
classes:
  beginner: {max_hp: {base: 55, per_level: 15}, accuracy: {base: 12}}
  warrior: {max_hp: {base: 60, per_level: 18}, max_mp: {base: 20, per_level: 4}, accuracy: {base: 10}, crit_damage: {base: 0.5}}
  bowman:  {max_hp: {base: 50, per_level: 12}, accuracy: {base: 15}}
`,
		"exp_curve.yaml": `
level_cap: 4
exp_to_next:
  - {level: 1, exp: 20}
  - {level: 2, exp: 65}
  - {level: 3, exp: 129}
`,
		"items.yaml": `
equip_slots:
  - {id: weapon, name: Weapon, kind: weapon}
  - {id: hat, name: Hat, kind: hat}
  - {id: ring_1, name: Ring, kind: ring}
  - {id: ring_2, name: Ring, kind: ring}
items:
  - {id: wooden_sword, name: Wooden Sword, tab: equip, slot: weapon, class: warrior, min_level: 1, stars: 1, description: "A practice blade.", stats: {physical_attack: [10, 12], str: [0, 2]}}
  - {id: leather_cap, name: Leather Cap, tab: equip, slot: hat, stars: 1, description: "A cap.", requires: {str: 8, vit: 6}, stats: {physical_defense: [3, 4], move_speed: [5, 10], hp_regen: [1, 2]}}
  - {id: red_potion, name: Red Potion, tab: use, heal_hp: 50, stars: 1, description: "A red tonic."}
  - {id: slime_gel, name: Slime Gel, tab: etc, stars: 2, description: "Sticky."}
`,
		"mobs.yaml": `
mobs:
  - id: green_slime
    name: Green Slime
    level: 1
    hp: 30
    attack: 6
    damage: physical
    physical_defense: 2
    magic_defense: 1
    accuracy: 8
    evasion: 2
    exp: 5
    speed: 40
    width: 32
    height: 28
    behavior: patrol
    look: {sprite: slime}
    drops:
      - {item: slime_gel, chance: 0.6, min: 1, max: 2}
      - {item: red_potion, chance: 0.15, min: 1, max: 1}
`,
		"npcs.yaml": `
talk_reach: 60
npcs:
  - id: guide
    name: Guide
    look: guide
    gives: [first_hunt, gel_for_me]
    takes: [first_hunt]
  - id: hunter
    name: Hunter
    look: hunter
    trainer: bowman
    gives: [warrior_drill]
    takes: [gel_for_me, warrior_drill]
`,
		"quests.yaml": `
max_active: 20
quests:
  - id: first_hunt
    name: First Hunt
    giver: guide
    turn_in: guide
    objectives:
      - {kill: green_slime, count: 3}
      - {level: 2}
    rewards: {exp: 10, items: [{item: red_potion, quantity: 5}]}
  - id: gel_for_me
    name: Gel for Me
    giver: guide
    turn_in: hunter
    level: 2
    quests: [first_hunt]
    objectives:
      - {collect: slime_gel, count: 4}
      - {talk: hunter}
    rewards: {exp: 20, items: [{item: leather_cap, quantity: 1}]}
    repeatable: true
  - id: warrior_drill
    name: Warrior Drill
    giver: hunter
    turn_in: hunter
    class: warrior
    objectives:
      - {kill: green_slime, count: 1}
`,
		"maps/town.tmj": tiledMap("town", "", []object{
			{layer: "solids", name: "floor", x: 0, y: 608, w: 1280, h: 32},
			{layer: "platforms", name: "ledge", x: 96, y: 416, w: 192, h: 8},
			{layer: "ropes", name: "rope", x: 284, y: 416, w: 8, h: 192},
			{layer: "portals", name: "to_field", x: 1200, y: 544, w: 32, h: 64, props: `{"name":"target_map","type":"string","value":"field"},{"name":"target_portal","type":"string","value":"from_town"}`},
			{layer: "player_spawn", name: "start", x: 100, y: 576, w: 32, h: 32},
			{layer: "npcs", name: "guide", x: 200, y: 608},
		}),
		"maps/field.tmj": tiledMap("hunting", "", []object{
			{layer: "solids", name: "floor", x: 0, y: 608, w: 1280, h: 32},
			{layer: "portals", name: "from_town", x: 40, y: 544, w: 32, h: 64, props: `{"name":"target_map","type":"string","value":"town"},{"name":"target_portal","type":"string","value":"to_field"}`},
			{layer: "spawns", name: "slimes", x: 300, y: 500, w: 400, h: 108, props: `{"name":"mob","type":"string","value":"green_slime"},{"name":"max","type":"int","value":4},{"name":"respawn_seconds","type":"int","value":10}`},
			{layer: "player_spawn", name: "start", x: 80, y: 576, w: 32, h: 32},
			{layer: "npcs", name: "hunter", x: 600, y: 608},
		}),
	}
}

type object struct {
	layer, name string
	x, y, w, h  float64
	props       string
}

// tiledMap renders a Tiled 1.11 JSON map; extraLayers is spliced in as is and dropLayer omitted.
func tiledMap(kind, dropLayer string, objects []object) string {
	var b strings.Builder
	b.WriteString(`{"type":"map","version":"1.10","tiledversion":"1.11.0","orientation":"orthogonal","renderorder":"right-down",`)
	b.WriteString(`"width":40,"height":20,"tilewidth":32,"tileheight":32,"infinite":false,"nextlayerid":8,"nextobjectid":99,"tilesets":[],`)
	props := `{"name":"kind","type":"string","value":"` + kind + `"},{"name":"name","type":"string","value":"Somewhere"}`
	if kind == "town" {
		props += `,{"name":"start","type":"bool","value":true}`
	}
	if kind == "hunting" {
		props += `,{"name":"town","type":"string","value":"town"},{"name":"min_level","type":"int","value":1},{"name":"max_level","type":"int","value":5}`
	}
	b.WriteString(`"properties":[` + props + `],"layers":[`)
	first := true
	id := 1
	for i, layer := range []string{"solids", "platforms", "ropes", "portals", "spawns", "player_spawn", "npcs"} {
		if layer == dropLayer {
			continue
		}
		if !first {
			b.WriteString(",")
		}
		first = false
		b.WriteString(`{"id":` + itoa(i+1) + `,"name":"` + layer + `","type":"objectgroup","visible":true,"opacity":1,"x":0,"y":0,"draworder":"topdown","objects":[`)
		firstObj := true
		for _, o := range objects {
			if o.layer != layer {
				continue
			}
			if !firstObj {
				b.WriteString(",")
			}
			firstObj = false
			b.WriteString(`{"id":` + itoa(id) + `,"name":"` + o.name + `","type":"","rotation":0,"visible":true,`)
			b.WriteString(`"x":` + ftoa(o.x) + `,"y":` + ftoa(o.y) + `,"width":` + ftoa(o.w) + `,"height":` + ftoa(o.h))
			if o.props != "" {
				b.WriteString(`,"properties":[` + o.props + `]`)
			}
			b.WriteString("}")
			id++
		}
		b.WriteString("]}")
	}
	b.WriteString("]}")
	return b.String()
}

func itoa(i int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + fmtInt(i)) }
func fmtInt(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var d []byte
	for i > 0 {
		d = append([]byte{byte('0' + i%10)}, d...)
		i /= 10
	}
	if neg {
		d = append([]byte{'-'}, d...)
	}
	return string(d)
}
func ftoa(f float64) string { return fmtInt(int(f)) }

func write(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestValidDataLoads(t *testing.T) {
	d, err := load.Dir(write(t, fixture()))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("the most active quests and the longest chat line", func(t *testing.T) {
		if d.MaxActiveQuests() != 20 || d.ChatLineMax() != 120 {
			t.Fatalf("max active %d, chat line %d", d.MaxActiveQuests(), d.ChatLineMax())
		}
	})
	t.Run("the class tree, with its attacks and auto-assign splits", func(t *testing.T) {
		root := d.StartClass()
		if root.ID != "beginner" || root.Parent != "" || !slices.Equal(root.Line, []kernel.Class{"beginner"}) {
			t.Fatalf("root = %+v, want the beginner", root)
		}
		if root.BaseStats.STR != 12 || len(root.StarterItems) != 1 || root.AutoAssign != (gamedata.Stats{}) {
			t.Fatalf("root stats %+v, starters %+v, split %+v", root.BaseStats, root.StarterItems, root.AutoAssign)
		}
		if _, ok := d.Class("thief"); ok {
			t.Fatal("thief is not in the fixture but was found")
		}
		w, ok := d.Class("warrior")
		if !ok || w.Parent != "beginner" || !slices.Equal(w.Line, []kernel.Class{"warrior", "beginner"}) {
			t.Fatalf("warrior = %+v, want a child of the beginner", w)
		}
		if w.Attack != (gamedata.Attack{Kind: gamedata.Melee, Damage: gamedata.Physical, Width: 64, Height: 56, Cooldown: 600 * time.Millisecond}) {
			t.Fatalf("warrior attack = %+v", w.Attack)
		}
		if w.AutoAssign != (gamedata.Stats{STR: 3, VIT: 2}) || w.BaseStats != (gamedata.Stats{}) {
			t.Fatalf("warrior stats = %+v / %+v", w.BaseStats, w.AutoAssign)
		}
		b, _ := d.Class("bowman")
		if b.Attack.Kind != gamedata.Ranged || b.Attack.Range != 420 || b.Attack.Damage != gamedata.Magic {
			t.Fatalf("bowman attack = %+v", b.Attack)
		}
	})
	t.Run("a class fits its own items, its ancestors' and classless ones", func(t *testing.T) {
		w, _ := d.Class("warrior")
		root := d.StartClass()
		for _, tt := range []struct {
			c    gamedata.Class
			item kernel.Class
			want bool
		}{
			{w, "", true}, {w, "warrior", true}, {w, "beginner", true}, {w, "bowman", false},
			{root, "", true}, {root, "beginner", true}, {root, "warrior", false},
		} {
			if got := tt.c.Fits(tt.item); got != tt.want {
				t.Errorf("%s fits a %q item = %v, want %v", tt.c.ID, tt.item, got, tt.want)
			}
		}
	})
	t.Run("the stat rules", func(t *testing.T) {
		r := d.StatRules()
		if r.PointsPerLevel != 5 {
			t.Fatalf("points per level %d", r.PointsPerLevel)
		}
		if want := (gamedata.Flats[float64]{PhysicalAttack: 0.5, MagicAttack: 0.5, Accuracy: 1, CritDamage: 0.005}); r.PerPoint.DEX != want {
			t.Fatalf("a point of DEX gives %+v, want %+v", r.PerPoint.DEX, want)
		}
		if want := (gamedata.Flats[float64]{MaxHP: 10, PhysicalDefense: 1, MagicDefense: 0.5, HPRegen: 0.05}); r.PerPoint.VIT != want {
			t.Fatalf("a point of VIT gives %+v, want %+v", r.PerPoint.VIT, want)
		}
		want := gamedata.Rates[gamedata.Curve]{
			CritRate:      gamedata.Curve{Stat: gamedata.DEX, Base: 0.05, Limit: 0.60, K: 200},
			AttackSpeed:   gamedata.Curve{Stat: gamedata.AGI, Limit: 0.50, K: 150},
			CooldownSpeed: gamedata.Curve{Stat: gamedata.INT, Limit: 0.40, K: 200},
			MoveSpeed:     gamedata.Curve{Stat: gamedata.AGI, Limit: 0.30, K: 200},
		}
		if r.Curves != want {
			t.Fatalf("curves %+v, want %+v", r.Curves, want)
		}
		if r.Penetration != 0.5 || r.MinHit != 0.2 || r.MaxHit != 0.99 {
			t.Fatalf("limits: penetration %v, hit %v to %v", r.Penetration, r.MinHit, r.MaxHit)
		}
		w := r.Classes[kernel.Class("warrior")]
		if w.MaxHP != (gamedata.Grows{Base: 60, PerLevel: 18}) || w.CritDamage.Base != 0.5 || w.MaxHP.At(3) != 96 {
			t.Fatalf("warrior bases %+v", w)
		}
		if b := r.Classes[kernel.Class("bowman")]; b.Accuracy.Base != 15 || b.MaxMP != (gamedata.Grows{}) {
			t.Fatalf("bowman bases %+v: a stat left out has no base", b)
		}
	})
	t.Run("the loot timings", func(t *testing.T) {
		if got, want := d.Loot(), (gamedata.Loot{CollectDelay: time.Second, DropLife: time.Minute, RoomCheckEvery: 2 * time.Second}); got != want {
			t.Fatalf("loot %+v, want %+v", got, want)
		}
	})
	t.Run("the EXP curve", func(t *testing.T) {
		if d.LevelCap() != 4 {
			t.Fatalf("cap %d", d.LevelCap())
		}
		for level, want := range map[int]int64{1: 20, 2: 65, 3: 129} {
			got, ok := d.ExpToNext(level)
			if !ok || got != want {
				t.Fatalf("ExpToNext(%d) = %d, %v; want %d", level, got, ok, want)
			}
		}
		if _, ok := d.ExpToNext(4); ok {
			t.Fatal("ExpToNext at the cap should be false")
		}
		if _, ok := d.ExpToNext(0); ok {
			t.Fatal("ExpToNext(0) should be false")
		}
	})
	t.Run("items and mobs", func(t *testing.T) {
		it, ok := d.Item("wooden_sword")
		if !ok || it.Tab != gamedata.Equip || it.Slot != "weapon" || it.Class != kernel.Class("warrior") || it.MaxStack != 1 || it.Stars != 1 || it.Description != "A practice blade." {
			t.Fatalf("wooden_sword = %+v", it)
		}
		if want := (gamedata.Rolls{PhysicalAttack: gamedata.Range{Min: 10, Max: 12}, STR: gamedata.Range{Max: 2}}); it.Rolls != want || it.Requires != (gamedata.Stats{}) {
			t.Fatalf("wooden_sword rolls %+v, want %+v, and no requirements", it.Rolls, want)
		}
		cap, _ := d.Item("leather_cap")
		if want := (gamedata.Rolls{PhysicalDefense: gamedata.Range{Min: 3, Max: 4}, MoveSpeed: gamedata.Range{Min: 5, Max: 10}, HPRegen: gamedata.Range{Min: 1, Max: 2}}); cap.Rolls != want || cap.Requires != (gamedata.Stats{STR: 8, VIT: 6}) || cap.Class != "" {
			t.Fatalf("leather_cap = %+v", cap)
		}
		if gel, _ := d.Item("slime_gel"); gel.Stars != 2 || gel.Rolls != (gamedata.Rolls{}) {
			t.Fatalf("slime_gel = %+v", gel)
		}
		potion, _ := d.Item("red_potion")
		if potion.Tab != gamedata.Use || potion.MaxStack != 100 || potion.HealHP != 50 {
			t.Fatalf("red_potion = %+v", potion)
		}
		m, ok := d.Mob("green_slime")
		if !ok || m.Level != 1 || m.HP != 30 || m.Damage != gamedata.Physical || m.PhysicalDefense != 2 || m.MagicDefense != 1 || len(m.Drops) != 2 || m.Drops[0].Item != "slime_gel" || m.Drops[0].Max != 2 {
			t.Fatalf("green_slime = %+v", m)
		}
		if _, ok := d.Item("nope"); ok {
			t.Fatal("unknown item found")
		}
		if _, ok := d.Mob("nope"); ok {
			t.Fatal("unknown mob found")
		}
	})
	t.Run("equip slots, in file order and by ID", func(t *testing.T) {
		var ids []string
		for _, s := range d.EquipSlots() {
			ids = append(ids, s.ID)
		}
		if !slices.Equal(ids, []string{"weapon", "hat", "ring_1", "ring_2"}) {
			t.Fatalf("slots %v", ids)
		}
		if s, ok := d.EquipSlot("ring_2"); !ok || s.Kind != "ring" || s.Name != "Ring" {
			t.Fatalf("ring_2: %+v, %v", s, ok)
		}
		if _, ok := d.EquipSlot("boots"); ok {
			t.Fatal("found a slot the data doesn't have")
		}
	})
	t.Run("movement", func(t *testing.T) {
		if d.Movement() != wantMovement {
			t.Fatalf("movement = %+v", d.Movement())
		}
	})
	t.Run("maps", func(t *testing.T) {
		town, ok := d.Map("town")
		if !ok || town.Kind != gamedata.Town || town.ID != "town" {
			t.Fatalf("town = %+v", town)
		}
		if d.StartTown().ID != "town" {
			t.Fatalf("StartTown() = %s", d.StartTown().ID)
		}
		hunting, _ := d.Map("field")
		if !town.Start || hunting.Start || town.Name != "Somewhere" || hunting.MinLevel != 1 || hunting.MaxLevel != 5 || town.MinLevel != 0 {
			t.Fatalf("town %+v, hunting %+v: start, name and levels", town, hunting)
		}
		if town.Town != "town" || hunting.Town != "town" {
			t.Fatalf("linked towns: town %q, hunting %q", town.Town, hunting.Town)
		}
		if town.Width != 1280 || town.Height != 640 {
			t.Fatalf("town size = %v x %v, want 1280 x 640 (40 x 20 tiles of 32)", town.Width, town.Height)
		}
		field, _ := d.Map("field")
		if field.Kind != gamedata.Hunting || len(field.Spawns) != 1 {
			t.Fatalf("field = %+v", field)
		}
		sp := field.Spawns[0]
		if sp.Mob != "green_slime" || sp.Max != 4 || sp.Respawn != 10*time.Second || sp.Area != (sim.Rect{X: 300, Y: 500, W: 400, H: 108}) {
			t.Fatalf("spawn = %+v", sp)
		}
		if len(d.Maps()) != 2 {
			t.Fatalf("%d maps", len(d.Maps()))
		}
	})
	t.Run("NPCs, placed by their maps", func(t *testing.T) {
		if d.TalkReach() != 60 {
			t.Fatalf("talk reach %v", d.TalkReach())
		}
		guide, ok := d.NPC("guide")
		if !ok || guide.Name != "Guide" || guide.Look != "guide" || guide.MapID != "town" || guide.At != (gamedata.Point{X: 200, Y: 608}) || guide.Trainer != "" {
			t.Fatalf("guide = %+v", guide)
		}
		if !slices.Equal(guide.Gives, []string{"first_hunt", "gel_for_me"}) || !slices.Equal(guide.Takes, []string{"first_hunt"}) {
			t.Fatalf("guide offers %v, takes %v", guide.Gives, guide.Takes)
		}
		hunter, _ := d.NPC("hunter")
		if hunter.MapID != "field" || hunter.Trainer != "bowman" {
			t.Fatalf("hunter = %+v, want the bowman's trainer", hunter)
		}
		if len(d.NPCs()) != 2 || d.NPCs()[0].ID != "guide" {
			t.Fatalf("NPCs() = %+v", d.NPCs())
		}
		if _, ok := d.NPC("nobody"); ok {
			t.Fatal("unknown NPC found")
		}
	})
	t.Run("quests, with each objective in one shape", func(t *testing.T) {
		first, ok := d.Quest("first_hunt")
		want := []gamedata.Objective{{Kind: gamedata.Kill, Target: "green_slime", Count: 3}, {Kind: gamedata.Reach, Count: 2}}
		if !ok || first.Name != "First Hunt" || first.Giver != "guide" || first.TurnIn != "guide" || !slices.Equal(first.Objectives, want) || first.Repeatable {
			t.Fatalf("first_hunt = %+v", first)
		}
		if first.Rewards.Exp != 10 || !slices.Equal(first.Rewards.Items, []gamedata.Grant{{Item: "red_potion", Quantity: 5}}) {
			t.Fatalf("first_hunt rewards %+v", first.Rewards)
		}
		gel, _ := d.Quest("gel_for_me")
		want = []gamedata.Objective{{Kind: gamedata.Collect, Target: "slime_gel", Count: 4}, {Kind: gamedata.TalkTo, Target: "hunter", Count: 1}}
		if gel.Level != 2 || !slices.Equal(gel.Requires, []string{"first_hunt"}) || !slices.Equal(gel.Objectives, want) || !gel.Repeatable || gel.TurnIn != "hunter" {
			t.Fatalf("gel_for_me = %+v", gel)
		}
		if drill, _ := d.Quest("warrior_drill"); drill.Class != kernel.Class("warrior") || drill.Rewards.Exp != 0 || drill.Rewards.Items != nil {
			t.Fatalf("warrior_drill = %+v", drill)
		}
		if len(d.Quests()) != 3 || d.Quests()[2].ID != "warrior_drill" {
			t.Fatalf("Quests() = %+v", d.Quests())
		}
		if _, ok := d.Quest("nope"); ok {
			t.Fatal("unknown quest found")
		}
	})
}

func TestTiledConversion(t *testing.T) {
	d, err := load.Dir(write(t, fixture()))
	if err != nil {
		t.Fatal(err)
	}
	town, _ := d.Map("town")
	tests := []struct {
		name string
		got  any
		want any
	}{
		{"a solid keeps Tiled's top-left origin and size", town.Geometry.Solids[0], sim.Rect{X: 0, Y: 608, W: 1280, H: 32}},
		{"a one-way platform lands where sim expects it", town.Geometry.Platforms[0], sim.Rect{X: 96, Y: 416, W: 192, H: 8}},
		{"a rope", town.Geometry.Ropes[0], sim.Rect{X: 284, Y: 416, W: 8, H: 192}},
		{"the player spawn is the bottom centre of its object", town.PlayerSpawn, gamedata.Point{X: 116, Y: 608}},
		{"a portal keeps its rectangle and typed targets", town.Portals[0], gamedata.Portal{Name: "to_field", Area: sim.Rect{X: 1200, Y: 544, W: 32, H: 64}, TargetMap: "field", TargetPortal: "from_town"}},
		{"a portal's arrival point is its bottom centre", town.Portals[0].Arrival(), gamedata.Point{X: 1216, Y: 608}},
		{"an NPC stands at its point object", len(town.NPCs) == 1 && town.NPCs[0] == gamedata.Placement{NPC: "guide", At: gamedata.Point{X: 200, Y: 608}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("got %+v, want %+v", tt.got, tt.want)
			}
		})
	}
	t.Run("a character standing on the converted platform stays on it", func(t *testing.T) {
		s := sim.State{X: 192, Y: 416, Facing: 1, OnGround: true, Ground: 0, DropPlatform: sim.NoPlatform}
		s = sim.Step(s, sim.Input{}, town.Geometry, d.Movement())
		if !s.OnGround || s.Y != 416 || s.Ground != 0 {
			t.Fatalf("state %+v", s)
		}
	})
}

func TestRejections(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		old     string
		replace string
		wantAll []string // substrings the error must contain
	}{
		{"a spawn naming an unknown mob names the map and object", "maps/field.tmj", `"value":"green_slime"`, `"value":"dragon"`, []string{"field.tmj", "slimes", "dragon"}},
		{"a portal whose target map doesn't exist", "maps/town.tmj", `"value":"field"`, `"value":"swamp"`, []string{"town.tmj", "to_field", "swamp"}},
		{"a portal whose target portal doesn't exist", "maps/town.tmj", `"value":"from_town"`, `"value":"from_nowhere"`, []string{"town.tmj", "to_field", "from_nowhere"}},
		{"a portal without a target_map property", "maps/town.tmj", `{"name":"target_map","type":"string","value":"field"},`, ``, []string{"town.tmj", "to_field", "target_map"}},
		{"a drop table naming an unknown item", "mobs.yaml", `item: slime_gel`, `item: dragon_scale`, []string{"mobs.yaml", "green_slime", "dragon_scale"}},
		{"an equipment drop with a quantity above one", "mobs.yaml", `{item: red_potion, chance: 0.15, min: 1, max: 1}`, `{item: wooden_sword, chance: 0.15, min: 1, max: 2}`, []string{"mobs.yaml", "green_slime", "wooden_sword"}},
		{"a drop chance above one", "mobs.yaml", `chance: 0.6`, `chance: 1.5`, []string{"mobs.yaml", "green_slime", "chance"}},
		{"duplicate item IDs", "items.yaml", `id: slime_gel`, `id: red_potion`, []string{"items.yaml", "red_potion", "duplicate"}},
		{"duplicate mob IDs", "mobs.yaml", `mobs:`, "mobs:\n  - {id: green_slime, name: X, level: 1, hp: 1, attack: 1, damage: physical, physical_defense: 1, magic_defense: 1, accuracy: 1, evasion: 1, exp: 1, speed: 1, width: 1, height: 1, behavior: patrol, look: {sprite: slime}}", []string{"mobs.yaml", "green_slime", "duplicate"}},
		{"a mob with an unknown damage type", "mobs.yaml", `damage: physical`, `damage: fire`, []string{"mobs.yaml", "green_slime", "fire"}},
		{"a mob with a negative defense", "mobs.yaml", `magic_defense: 1`, `magic_defense: -1`, []string{"mobs.yaml", "green_slime", "negative"}},
		{"duplicate class IDs", "classes.yaml", `id: bowman`, `id: warrior`, []string{"classes.yaml", "warrior", "duplicate"}},
		{"a map missing a required layer", "maps/town.tmj", `"name":"ropes"`, `"name":"strings"`, []string{"town.tmj", "ropes"}},
		{"a map without a kind", "maps/town.tmj", `"name":"kind"`, `"name":"mood"`, []string{"town.tmj", "kind"}},
		{"a map with an unknown kind", "maps/town.tmj", `"value":"town"`, `"value":"dungeon"`, []string{"town.tmj", "kind", "dungeon"}},
		{"a hunting map without a town", "maps/field.tmj", `"name":"town"`, `"name":"home"`, []string{"field.tmj", "town"}},
		{"a hunting map whose town is not a town", "maps/field.tmj", `"name":"town","type":"string","value":"town"`, `"name":"town","type":"string","value":"field"`, []string{"field.tmj", "town", "field"}},
		{"a town with a mob spawn", "maps/town.tmj", `"name":"spawns","type":"objectgroup","visible":true,"opacity":1,"x":0,"y":0,"draworder":"topdown","objects":[]`, `"name":"spawns","type":"objectgroup","visible":true,"opacity":1,"x":0,"y":0,"draworder":"topdown","objects":[{"id":50,"name":"s","type":"","rotation":0,"visible":true,"x":1,"y":1,"width":1,"height":1,"properties":[{"name":"mob","type":"string","value":"green_slime"},{"name":"max","type":"int","value":1},{"name":"respawn_seconds","type":"int","value":1}]}]`, []string{"town.tmj", "town", "spawn"}},
		{"no town at all", "maps/town.tmj", `"value":"town"`, `"value":"hunting"`, []string{"town"}},
		{"a map without a name", "maps/field.tmj", `"name":"name"`, `"name":"title"`, []string{"field.tmj", "name"}},
		{"no start town", "maps/town.tmj", `"name":"start","type":"bool","value":true`, `"name":"start","type":"bool","value":false`, []string{"town", "start"}},
		{"a hunting map as the start", "maps/field.tmj", `{"name":"kind","type":"string","value":"hunting"}`, `{"name":"kind","type":"string","value":"hunting"},{"name":"start","type":"bool","value":true}`, []string{"field.tmj", "start", "town"}},
		{"a hunting map without its levels", "maps/field.tmj", `"name":"min_level"`, `"name":"low_level"`, []string{"field.tmj", "min_level", "max_level"}},
		{"a hunting map whose levels run backwards", "maps/field.tmj", `{"name":"max_level","type":"int","value":5}`, `{"name":"max_level","type":"int","value":0}`, []string{"field.tmj", "min_level", "max_level"}},
		{"a spawn whose mob is outside the map's levels", "maps/field.tmj", `{"name":"min_level","type":"int","value":1}`, `{"name":"min_level","type":"int","value":2}`, []string{"field.tmj", "slimes", "green_slime", "level 1", "2 to 5"}},
		{"a mob without a look", "mobs.yaml", "    look: {sprite: slime}\n", "", []string{"mobs.yaml", "green_slime", "look"}},
		{"a mob tinted past RGB", "mobs.yaml", `look: {sprite: slime}`, `look: {sprite: slime, tint: 0x1000000}`, []string{"mobs.yaml", "green_slime", "tint"}},
		{"a mob scaled to nothing", "mobs.yaml", `look: {sprite: slime}`, `look: {sprite: slime, scale: 0}`, []string{"mobs.yaml", "green_slime", "scale"}},
		{"the EXP curve must rise strictly", "exp_curve.yaml", `{level: 3, exp: 129}`, `{level: 3, exp: 65}`, []string{"exp_curve.yaml", "level 3"}},
		{"the EXP curve must cover every level below the cap", "exp_curve.yaml", "  - {level: 3, exp: 129}\n", "", []string{"exp_curve.yaml", "level 3"}},
		{"the EXP curve must not go past the cap", "exp_curve.yaml", `level_cap: 4`, `level_cap: 3`, []string{"exp_curve.yaml", "level 3"}},
		{"points per level below one", "stats.yaml", `points_per_level: 5`, `points_per_level: 0`, []string{"stats.yaml", "points_per_level"}},
		{"a point of a stat that isn't primary", "stats.yaml", `  agi: {evasion: 1}`, `  luk: {evasion: 1}`, []string{"stats.yaml", "luk"}},
		{"a point giving a rate, which has its curve", "stats.yaml", `  agi: {evasion: 1}`, `  agi: {evasion: 1, crit_rate: 0.01}`, []string{"stats.yaml", "agi", "crit_rate"}},
		{"a point giving a negative amount", "stats.yaml", `  agi: {evasion: 1}`, `  agi: {evasion: -1}`, []string{"stats.yaml", "agi", "evasion", "negative"}},
		{"a curve over a stat that isn't primary", "stats.yaml", `{stat: dex, base: 0.05`, `{stat: luk, base: 0.05`, []string{"stats.yaml", "crit_rate", "luk"}},
		{"a limit at its base", "stats.yaml", `base: 0.05, limit: 0.60`, `base: 0.05, limit: 0.05`, []string{"stats.yaml", "crit_rate", "limit"}},
		{"a limit below its base", "stats.yaml", `base: 0.05, limit: 0.60`, `base: 0.05, limit: 0.01`, []string{"stats.yaml", "crit_rate", "limit"}},
		{"a k of zero", "stats.yaml", `limit: 0.50, k: 150`, `limit: 0.50, k: 0`, []string{"stats.yaml", "attack_speed", "k"}},
		{"a k below zero", "stats.yaml", `limit: 0.50, k: 150`, `limit: 0.50, k: -5`, []string{"stats.yaml", "attack_speed", "k"}},
		{"a rate without a curve", "stats.yaml", "  move_speed:     {stat: agi, limit: 0.30, k: 200}\n", "", []string{"stats.yaml", "move_speed"}},
		{"a curve for a rate that doesn't exist", "stats.yaml", "diminishing:\n", "diminishing:\n  luck_rate: {stat: agi, limit: 0.3, k: 1}\n", []string{"stats.yaml", "luck_rate"}},
		{"penetration above one", "stats.yaml", `penetration: 0.50`, `penetration: 1.5`, []string{"stats.yaml", "penetration"}},
		{"a hit chance whose least is above its most", "stats.yaml", `hit_chance: [0.20, 0.99]`, `hit_chance: [0.99, 0.20]`, []string{"stats.yaml", "hit_chance"}},
		{"class bases for a class that doesn't exist", "stats.yaml", "  bowman:", "  thief: {max_hp: {base: 1}}\n  bowman:", []string{"stats.yaml", "thief"}},
		{"a class base for a rate", "stats.yaml", `accuracy: {base: 15}`, `accuracy: {base: 15}, crit_rate: {base: 0.1}`, []string{"stats.yaml", "bowman", "crit_rate"}},
		{"a class without bases", "stats.yaml", "  bowman:  {max_hp: {base: 50, per_level: 12}, accuracy: {base: 15}}\n", "", []string{"stats.yaml", "bowman"}},
		{"a class with an unknown attack kind", "classes.yaml", `kind: melee`, `kind: magic`, []string{"classes.yaml", "beginner", "magic"}},
		{"an attack with an unknown damage type", "classes.yaml", `damage: physical`, `damage: fire`, []string{"classes.yaml", "beginner", "fire"}},
		{"an auto-assign split that doesn't add up to the points a level gives", "classes.yaml", `auto_assign: {str: 3, vit: 2}`, `auto_assign: {str: 3, vit: 3}`, []string{"classes.yaml", "warrior", "auto_assign", "5"}},
		{"an auto-assign split with a negative stat", "classes.yaml", `auto_assign: {str: 3, vit: 2}`, `auto_assign: {str: 6, vit: -1}`, []string{"classes.yaml", "warrior", "auto_assign"}},
		{"a class renamed away from its bases", "classes.yaml", `id: bowman`, `id: archer`, []string{"stats.yaml", "bowman", "classes.yaml"}},
		{"a first class with a parent, so no root", "classes.yaml", "    name: Beginner\n", "    name: Beginner\n    parent: warrior\n", []string{"classes.yaml", "beginner", "root"}},
		{"a second root", "classes.yaml", "    parent: beginner\n    look: {sprite: alien_green}", "    look: {sprite: alien_green}", []string{"classes.yaml", "bowman", "root"}},
		{"a parent that isn't above it in the file", "classes.yaml", "    name: Warrior\n    parent: beginner\n", "    name: Warrior\n    parent: bowman\n", []string{"classes.yaml", "warrior", "bowman"}},
		{"a parent that doesn't exist", "classes.yaml", "    name: Warrior\n    parent: beginner\n", "    name: Warrior\n    parent: paladin\n", []string{"classes.yaml", "warrior", "paladin"}},
		{"starting stats on a class a character is promoted to", "classes.yaml", "    auto_assign: {str: 3, vit: 2}\n", "    auto_assign: {str: 3, vit: 2}\n    base_stats: {str: 9}\n", []string{"classes.yaml", "warrior", "base_stats"}},
		{"starter items on a class a character is promoted to", "classes.yaml", "    auto_assign: {str: 3, vit: 2}\n", "    auto_assign: {str: 3, vit: 2}\n    starter_items: [{item: red_potion, quantity: 1}]\n", []string{"classes.yaml", "warrior", "starter_items"}},
		{"no starting stats on the root", "classes.yaml", "    base_stats: {str: 12, dex: 5, int: 4, agi: 5, vit: 10}\n", "", []string{"classes.yaml", "beginner", "base_stats"}},
		{"an auto-assign split on the root", "classes.yaml", "    base_stats: {str: 12", "    auto_assign: {str: 5}\n    base_stats: {str: 12", []string{"classes.yaml", "beginner", "auto_assign"}},
		{"a class a character is promoted to, with no split", "classes.yaml", "    auto_assign: {str: 3, vit: 2}\n", "", []string{"classes.yaml", "warrior", "auto_assign"}},
		{"a class without a look", "classes.yaml", "    look: {sprite: alien_blue}\n", "", []string{"classes.yaml", "warrior", "look"}},
		{"a class tinted past RGB", "classes.yaml", "look: {sprite: alien_blue}", "look: {sprite: alien_blue, tint: 0x1000000}", []string{"classes.yaml", "warrior", "tint"}},
		{"a starter item that doesn't exist", "classes.yaml", `starter_items: [{item: wooden_sword, quantity: 1}]`, `starter_items: [{item: excalibur, quantity: 1}]`, []string{"classes.yaml", "beginner", "excalibur"}},
		{"a starter stack above the stack size", "classes.yaml", `starter_items: [{item: wooden_sword, quantity: 1}]`, `starter_items: [{item: wooden_sword, quantity: 2}]`, []string{"classes.yaml", "beginner", "wooden_sword"}},
		{"an item with an unknown tab", "items.yaml", `tab: etc`, `tab: misc`, []string{"items.yaml", "slime_gel", "misc"}},
		{"equipment without a slot", "items.yaml", `slot: weapon, `, ``, []string{"items.yaml", "wooden_sword", "slot"}},
		{"equipment naming a slot kind no equip slot has", "items.yaml", `slot: weapon, `, `slot: cape, `, []string{"items.yaml", "wooden_sword", "cape"}},
		{"no stars", "items.yaml", `tab: etc, stars: 2, `, `tab: etc, `, []string{"items.yaml", "slime_gel", "stars"}},
		{"stars above seven", "items.yaml", `stars: 2`, `stars: 8`, []string{"items.yaml", "slime_gel", "stars"}},
		{"no description", "items.yaml", `, description: "Sticky."`, ``, []string{"items.yaml", "slime_gel", "description"}},
		{"equipment without stats", "items.yaml", `, stats: {physical_attack: [10, 12], str: [0, 2]}`, ``, []string{"items.yaml", "wooden_sword", "stats"}},
		{"an unknown stat", "items.yaml", `str: [0, 2]`, `luck: [0, 2]`, []string{"items.yaml", "wooden_sword", "luck"}},
		{"a range whose minimum is above its maximum", "items.yaml", `physical_attack: [10, 12]`, `physical_attack: [12, 10]`, []string{"items.yaml", "wooden_sword", "physical_attack"}},
		{"a range below zero", "items.yaml", `str: [0, 2]`, `str: [-1, 2]`, []string{"items.yaml", "wooden_sword", "str"}},
		{"a range that only rolls zero", "items.yaml", `str: [0, 2]`, `str: [0, 0]`, []string{"items.yaml", "wooden_sword", "str"}},
		{"a range that isn't two numbers", "items.yaml", `physical_attack: [10, 12]`, `physical_attack: [10]`, []string{"items.yaml", "wooden_sword", "physical_attack"}},
		{"the old name of physical attack", "items.yaml", `physical_attack: [10, 12]`, `attack: [10, 12]`, []string{"items.yaml", "wooden_sword", "attack"}},
		{"a class on armor: only weapons keep one", "items.yaml", `slot: hat, `, `slot: hat, class: warrior, `, []string{"items.yaml", "leather_cap", "weapon"}},
		{"a requirement on a stat that isn't primary", "items.yaml", `requires: {str: 8, vit: 6}`, `requires: {str: 8, luk: 6}`, []string{"items.yaml", "leather_cap", "luk"}},
		{"a requirement on a derived stat", "items.yaml", `requires: {str: 8, vit: 6}`, `requires: {str: 8, max_hp: 6}`, []string{"items.yaml", "leather_cap", "max_hp"}},
		{"a requirement below one", "items.yaml", `requires: {str: 8, vit: 6}`, `requires: {str: 8, vit: 0}`, []string{"items.yaml", "leather_cap", "vit"}},
		{"requirements on a use item", "items.yaml", `heal_hp: 50, `, `heal_hp: 50, requires: {str: 1}, `, []string{"items.yaml", "red_potion", "requirements"}},
		{"stats on a use item", "items.yaml", `heal_hp: 50, `, `heal_hp: 50, stats: {max_hp: [1, 2]}, `, []string{"items.yaml", "red_potion", "stats"}},
		{"duplicate equip slots", "items.yaml", `id: ring_2`, `id: ring_1`, []string{"items.yaml", "ring_1", "duplicate"}},
		{"an equip slot without a kind", "items.yaml", `{id: hat, name: Hat, kind: hat}`, `{id: hat, name: Hat}`, []string{"items.yaml", "hat", "kind"}},
		{"an item restricted to an unknown class", "items.yaml", `class: warrior`, `class: paladin`, []string{"items.yaml", "wooden_sword", "paladin"}},
		{"an unknown key is rejected", "items.yaml", `tab: etc`, `tab: etc, colour: green`, []string{"items.yaml", "colour"}},
		{"movement constants must be positive", "movement.yaml", `gravity: 1900`, `gravity: 0`, []string{"movement.yaml", "gravity"}},
		{"no active quest allowed", "quests.yaml", `max_active: 20`, `max_active: 0`, []string{"quests.yaml", "max_active"}},
		{"no chat line allowed", "chat.yaml", `line_max: 120`, `line_max: 0`, []string{"chat.yaml", "line_max"}},
		{"loot timings must be positive", "loot.yaml", `collect_delay: 1s`, `collect_delay: 0s`, []string{"loot.yaml", "collect_delay"}},
		{"a drop must fly before it is lost", "loot.yaml", `collect_delay: 1s`, `collect_delay: 60s`, []string{"loot.yaml", "drop_life"}},
		{"a full inventory must be asked again before the drop is lost", "loot.yaml", `room_check_every: 2s`, `room_check_every: 90s`, []string{"loot.yaml", "drop_life"}},
		{"a loot timing that isn't a duration", "loot.yaml", `drop_life: 60s`, `drop_life: a minute`, []string{"loot.yaml"}},
		{"a map without an npcs layer", "maps/field.tmj", `"name":"npcs"`, `"name":"people"`, []string{"field.tmj", "npcs"}},
		{"an NPC on a map but not in npcs.yaml", "maps/town.tmj", `"name":"guide"`, `"name":"stranger"`, []string{"town.tmj", "stranger", "npcs.yaml"}},
		{"an NPC placed twice", "maps/field.tmj", `"name":"hunter"`, `"name":"guide"`, []string{"town.tmj", "guide", "field"}},
		{"an NPC placed nowhere", "npcs.yaml", "npcs:\n", "npcs:\n  - {id: ghost, name: Ghost, look: ghost}\n", []string{"npcs.yaml", "ghost", "no map"}},
		{"duplicate NPC IDs", "npcs.yaml", `id: hunter`, `id: guide`, []string{"npcs.yaml", "guide", "duplicate"}},
		{"an NPC without a look", "npcs.yaml", "    look: hunter\n", "", []string{"npcs.yaml", "hunter", "look"}},
		{"an NPC with an empty line", "npcs.yaml", "    look: hunter\n", "    look: hunter\n    lines: [\"\"]\n", []string{"npcs.yaml", "hunter", "line"}},
		{"an NPC saying a line twice", "npcs.yaml", "    look: hunter\n", "    look: hunter\n    lines: [Hi., Hi.]\n", []string{"npcs.yaml", "hunter", "twice"}},
		{"a trainer of a class that doesn't exist", "npcs.yaml", `trainer: bowman`, `trainer: paladin`, []string{"npcs.yaml", "hunter", "paladin"}},
		{"a trainer of the root, which nobody is promoted to", "npcs.yaml", `trainer: bowman`, `trainer: beginner`, []string{"npcs.yaml", "hunter", "beginner"}},
		{"a talk reach of zero", "npcs.yaml", `talk_reach: 60`, `talk_reach: 0`, []string{"npcs.yaml", "talk_reach"}},
		{"a quest its giver doesn't list under gives", "npcs.yaml", `gives: [first_hunt, gel_for_me]`, `gives: [first_hunt]`, []string{"quests.yaml", "gel_for_me", "guide", "gives"}},
		{"a quest its turn-in NPC doesn't list under takes", "npcs.yaml", `takes: [gel_for_me, warrior_drill]`, `takes: [warrior_drill]`, []string{"quests.yaml", "gel_for_me", "hunter", "takes"}},
		{"an NPC giving a quest another NPC gives", "npcs.yaml", `gives: [warrior_drill]`, `gives: [warrior_drill, first_hunt]`, []string{"npcs.yaml", "hunter", "first_hunt"}},
		{"an NPC taking a quest handed in elsewhere", "npcs.yaml", `takes: [first_hunt]`, `takes: [first_hunt, warrior_drill]`, []string{"npcs.yaml", "guide", "warrior_drill"}},
		{"an NPC giving a quest that doesn't exist", "npcs.yaml", `gives: [warrior_drill]`, `gives: [warrior_drill, dragon_hunt]`, []string{"npcs.yaml", "hunter", "dragon_hunt"}},
		{"a quest given by an unknown NPC", "quests.yaml", `giver: hunter`, `giver: mayor`, []string{"quests.yaml", "warrior_drill", "mayor"}},
		{"a quest handed in to an unknown NPC", "quests.yaml", `turn_in: hunter`, `turn_in: mayor`, []string{"quests.yaml", "gel_for_me", "mayor"}},
		{"a kill of an unknown mob", "quests.yaml", `{kill: green_slime, count: 3}`, `{kill: dragon, count: 3}`, []string{"quests.yaml", "first_hunt", "dragon"}},
		{"a collect of an unknown item", "quests.yaml", `{collect: slime_gel, count: 4}`, `{collect: dragon_scale, count: 4}`, []string{"quests.yaml", "gel_for_me", "dragon_scale"}},
		{"a talk to an unknown NPC", "quests.yaml", `{talk: hunter}`, `{talk: mayor}`, []string{"quests.yaml", "gel_for_me", "mayor"}},
		{"a reward of an unknown item", "quests.yaml", `{item: red_potion, quantity: 5}`, `{item: elixir, quantity: 5}`, []string{"quests.yaml", "first_hunt", "elixir"}},
		{"a quest for an unknown class", "quests.yaml", `class: warrior`, `class: paladin`, []string{"quests.yaml", "warrior_drill", "paladin"}},
		{"a quest requiring an unknown quest", "quests.yaml", `quests: [first_hunt]`, `quests: [lost_quest]`, []string{"quests.yaml", "gel_for_me", "lost_quest"}},
		{"quests requiring each other", "quests.yaml", "    turn_in: guide\n    objectives:", "    turn_in: guide\n    quests: [gel_for_me]\n    objectives:", []string{"quests.yaml", "cycle", "first_hunt", "gel_for_me"}},
		{"a quest requiring itself", "quests.yaml", `class: warrior`, "class: warrior\n    quests: [warrior_drill]", []string{"quests.yaml", "cycle", "warrior_drill"}},
		{"a kill count below one", "quests.yaml", `{kill: green_slime, count: 3}`, `{kill: green_slime, count: 0}`, []string{"quests.yaml", "first_hunt", "count"}},
		{"a collect without a count", "quests.yaml", `{collect: slime_gel, count: 4}`, `{collect: slime_gel}`, []string{"quests.yaml", "gel_for_me", "count"}},
		{"a talk with a count", "quests.yaml", `{talk: hunter}`, `{talk: hunter, count: 2}`, []string{"quests.yaml", "gel_for_me", "count"}},
		{"an objective of two kinds", "quests.yaml", `{kill: green_slime, count: 3}`, `{kill: green_slime, collect: slime_gel, count: 3}`, []string{"quests.yaml", "first_hunt", "exactly one"}},
		{"an objective of no kind", "quests.yaml", `{kill: green_slime, count: 3}`, `{count: 3}`, []string{"quests.yaml", "first_hunt", "exactly one"}},
		{"a level objective past the cap", "quests.yaml", `{level: 2}`, `{level: 5}`, []string{"quests.yaml", "first_hunt", "level"}},
		{"a quest without objectives", "quests.yaml", "    objectives:\n      - {kill: green_slime, count: 1}\n", "", []string{"quests.yaml", "warrior_drill", "objective"}},
		{"a quest level past the cap", "quests.yaml", "level: 2\n    quests:", "level: 5\n    quests:", []string{"quests.yaml", "gel_for_me", "level"}},
		{"an equipment reward with a quantity above one", "quests.yaml", `{item: leather_cap, quantity: 1}`, `{item: leather_cap, quantity: 2}`, []string{"quests.yaml", "gel_for_me", "leather_cap", "quantity 1"}},
		{"a reward above its stack size", "quests.yaml", `{item: red_potion, quantity: 5}`, `{item: red_potion, quantity: 101}`, []string{"quests.yaml", "first_hunt", "red_potion"}},
		{"negative reward EXP", "quests.yaml", `exp: 10`, `exp: -10`, []string{"quests.yaml", "first_hunt", "exp"}},
		{"duplicate quest IDs", "quests.yaml", `id: warrior_drill`, `id: first_hunt`, []string{"quests.yaml", "first_hunt", "duplicate"}},
		{"an unknown key in a quest", "quests.yaml", `repeatable: true`, `repeat: true`, []string{"quests.yaml", "repeat"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := fixture()
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

func TestRejectionsNeedingTheirOwnMaps(t *testing.T) {
	t.Run("duplicate portal names in one map", func(t *testing.T) {
		files := fixture()
		files["maps/field.tmj"] = tiledMap("hunting", "", []object{
			{layer: "solids", name: "floor", x: 0, y: 608, w: 1280, h: 32},
			{layer: "portals", name: "from_town", x: 40, y: 544, w: 32, h: 64, props: `{"name":"target_map","type":"string","value":"town"},{"name":"target_portal","type":"string","value":"to_field"}`},
			{layer: "portals", name: "from_town", x: 80, y: 544, w: 32, h: 64, props: `{"name":"target_map","type":"string","value":"town"},{"name":"target_portal","type":"string","value":"to_field"}`},
			{layer: "player_spawn", name: "start", x: 80, y: 576, w: 32, h: 32},
		})
		_, err := load.Dir(write(t, files))
		if err == nil || !strings.Contains(err.Error(), "from_town") || !strings.Contains(err.Error(), "field.tmj") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a map without a player spawn", func(t *testing.T) {
		files := fixture()
		files["maps/field.tmj"] = tiledMap("hunting", "", []object{
			{layer: "solids", name: "floor", x: 0, y: 608, w: 1280, h: 32},
			{layer: "portals", name: "from_town", x: 40, y: 544, w: 32, h: 64, props: `{"name":"target_map","type":"string","value":"town"},{"name":"target_portal","type":"string","value":"to_field"}`},
		})
		_, err := load.Dir(write(t, files))
		if err == nil || !strings.Contains(err.Error(), "player_spawn") || !strings.Contains(err.Error(), "field.tmj") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("two start towns", func(t *testing.T) {
		files := fixture()
		files["maps/village.tmj"] = tiledMap("town", "", []object{
			{layer: "solids", name: "floor", x: 0, y: 608, w: 1280, h: 32},
			{layer: "player_spawn", name: "start", x: 80, y: 576, w: 32, h: 32},
		})
		_, err := load.Dir(write(t, files))
		if err == nil || !strings.Contains(err.Error(), "town.tmj") || !strings.Contains(err.Error(), "village.tmj") || !strings.Contains(err.Error(), "start") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a map file that isn't JSON", func(t *testing.T) {
		files := fixture()
		files["maps/field.tmj"] = "{not json"
		_, err := load.Dir(write(t, files))
		if err == nil || !strings.Contains(err.Error(), "field.tmj") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a missing data file", func(t *testing.T) {
		files := fixture()
		delete(files, "mobs.yaml")
		_, err := load.Dir(write(t, files))
		if err == nil || !strings.Contains(err.Error(), "mobs.yaml") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a missing maps folder", func(t *testing.T) {
		files := fixture()
		delete(files, "maps/town.tmj")
		delete(files, "maps/field.tmj")
		_, err := load.Dir(write(t, files))
		if err == nil || !strings.Contains(err.Error(), "maps") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestEliteMobs(t *testing.T) {
	king := "\n  - {id: king_slime, name: King Slime, level: 2, elite: true, hp: 90, attack: 8, damage: physical, physical_defense: 2, magic_defense: 1, accuracy: 8, evasion: 2, exp: 12, speed: 30, width: 48, height: 40, behavior: patrol, look: {sprite: slime}}\n"
	kingSpawn := `,{"id":60,"name":"king","type":"","rotation":0,"visible":true,"x":300,"y":500,"width":400,"height":108,"properties":[{"name":"mob","type":"string","value":"king_slime"},{"name":"max","type":"int","value":1},{"name":"respawn_seconds","type":"int","value":30}]}`
	slimes := `{"name":"respawn_seconds","type":"int","value":10}]}`
	t.Run("an elite beside its map's normal mob loads, marked elite", func(t *testing.T) {
		files := fixture()
		files["mobs.yaml"] = strings.TrimRight(files["mobs.yaml"], "\n") + king
		files["maps/field.tmj"] = strings.Replace(files["maps/field.tmj"], slimes, slimes+kingSpawn, 1)
		d, err := load.Dir(write(t, files))
		if err != nil {
			t.Fatal(err)
		}
		if k, _ := d.Mob("king_slime"); !k.Elite {
			t.Fatalf("king_slime = %+v, want elite", k)
		}
		if s, _ := d.Mob("green_slime"); s.Elite {
			t.Fatal("green_slime is elite")
		}
	})
	t.Run("an elite alone on its map", func(t *testing.T) {
		files := fixture()
		files["mobs.yaml"] = strings.Replace(files["mobs.yaml"], "    level: 1\n", "    level: 1\n    elite: true\n", 1)
		_, err := load.Dir(write(t, files))
		if err == nil || !strings.Contains(err.Error(), "field.tmj") || !strings.Contains(err.Error(), "green_slime") || !strings.Contains(err.Error(), "normal") {
			t.Fatalf("err = %v", err)
		}
	})
}

// realItems are the item IDs data/items.yaml lists.
func realItems(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("../../../data/items.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Items []struct {
			ID string `yaml:"id"`
		} `yaml:"items"`
	}
	if err := yaml.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(f.Items))
	for i, it := range f.Items {
		ids[i] = it.ID
	}
	return ids
}

// reaches reports whether the portals lead from map from to map to, through any maps between.
func reaches(d *gamedata.Data, from, to string) bool {
	seen := map[string]bool{from: true}
	next := []string{from}
	for len(next) > 0 {
		id := next[0]
		next = next[1:]
		if id == to {
			return true
		}
		m, _ := d.Map(id)
		for _, p := range m.Portals {
			if !seen[p.TargetMap] {
				seen[p.TargetMap] = true
				next = append(next, p.TargetMap)
			}
		}
	}
	return false
}

func allLoaded(d *gamedata.Data, ids []string) bool {
	for _, id := range ids {
		if _, ok := d.Item(id); !ok {
			return false
		}
	}
	return true
}

func mobDamage(d *gamedata.Data, id string) gamedata.DamageType {
	m, _ := d.Mob(id)
	return m.Damage
}

// The real content is checked on every run, so a broken reference fails CI.
func TestRealDataLoads(t *testing.T) {
	d, err := load.Dir("../../../data")
	if err != nil {
		t.Fatal(err)
	}
	items := realItems(t)
	tests := []struct {
		name string
		ok   bool
	}{
		{"level cap 30", d.LevelCap() == 30},
		{"new characters start in a town", d.StartTown() != nil && d.StartTown().Kind == gamedata.Town},
		{"every listed item loaded", len(items) > 0 && allLoaded(d, items)},
		{"the expected movement constants", d.Movement() == wantMovement},
		{"cave bats deal magic damage, the forest's mobs physical", mobDamage(d, "cave_bat") == gamedata.Magic && mobDamage(d, "green_slime") == gamedata.Physical},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ok {
				t.Fatal("check failed")
			}
		})
	}
	t.Run("every character starts a Beginner, promoted to one of four classes", func(t *testing.T) {
		var children []kernel.Class
		for _, c := range d.Classes()[1:] {
			if c.Parent != d.StartClass().ID {
				t.Errorf("%s: parent %q, want the beginner", c.ID, c.Parent)
			}
			children = append(children, c.ID)
		}
		if d.StartClass().ID != "beginner" || !slices.Equal(children, []kernel.Class{"warrior", "thief", "bowman", "magician"}) {
			t.Fatalf("root %s, children %v", d.StartClass().ID, children)
		}
	})
	t.Run("the EXP curve is round(20 × level^1.7)", func(t *testing.T) {
		for level := 1; level < d.LevelCap(); level++ {
			want := int64(math.Round(20 * math.Pow(float64(level), 1.7)))
			got, ok := d.ExpToNext(level)
			if !ok || got != want {
				t.Fatalf("level %d: %d, want %d", level, got, want)
			}
		}
	})
	// Systems → Items, content: in each level band, gear for every armor and accessory kind and a weapon per class.
	t.Run("each level band has gear for every slot kind and a weapon per class", func(t *testing.T) {
		in := func(lo, hi int, ok func(gamedata.Item) bool) bool {
			for _, id := range items {
				if it, _ := d.Item(id); it.Tab == gamedata.Equip && it.MinLevel >= lo && it.MinLevel <= hi && ok(it) {
					return true
				}
			}
			return false
		}
		for _, band := range [][2]int{{1, 9}, {10, 19}, {20, d.LevelCap()}} {
			for _, s := range d.EquipSlots() {
				if s.Kind != "weapon" && !in(band[0], band[1], func(it gamedata.Item) bool { return it.Slot == s.Kind }) {
					t.Errorf("levels %d to %d: nothing for the %s slot", band[0], band[1], s.ID)
				}
			}
			for _, c := range d.Classes()[1:] {
				if !in(band[0], band[1], func(it gamedata.Item) bool { return it.Slot == "weapon" && it.Class == c.ID }) {
					t.Errorf("levels %d to %d: no weapon for the %s", band[0], band[1], c.ID)
				}
			}
		}
	})
	// A class's level 1 weapon is its trainer's gift at promotion.
	t.Run("every piece of equipment drops from a mob, or is a starter or a class's first weapon", func(t *testing.T) {
		got := map[string]bool{}
		for _, m := range d.Mobs() {
			for _, dr := range m.Drops {
				got[dr.Item] = true
			}
		}
		for _, c := range d.Classes() {
			for _, g := range c.StarterItems {
				got[g.Item] = true
			}
		}
		for _, id := range items {
			it, _ := d.Item(id)
			first := it.Slot == "weapon" && it.Class != "" && it.MinLevel == 1
			if it.Tab == gamedata.Equip && !got[id] && !first {
				t.Errorf("%s: nobody can get it", id)
			}
		}
	})
	t.Run("every NPC stands on a floor or platform of its map", func(t *testing.T) {
		if len(d.NPCs()) == 0 {
			t.Fatal("no NPCs")
		}
		for _, n := range d.NPCs() {
			m, _ := d.Map(n.MapID)
			if !slices.ContainsFunc(slices.Concat(m.Geometry.Solids, m.Geometry.Platforms), func(r sim.Rect) bool {
				return r.Y == n.At.Y && n.At.X >= r.X && n.At.X <= r.X+r.W
			}) {
				t.Errorf("%s floats at %+v on %s", n.ID, n.At, m.ID)
			}
		}
	})
	// A mob spawns on its area's bottom edge and patrols the floor under it; with no floor there it would float.
	t.Run("every spawn area stands on one floor or platform, clear of solids", func(t *testing.T) {
		for _, m := range d.Maps() {
			for _, s := range m.Spawns {
				a := s.Area
				if !slices.ContainsFunc(slices.Concat(m.Geometry.Solids, m.Geometry.Platforms), func(r sim.Rect) bool {
					return r.Y == a.Y+a.H && a.X >= r.X && a.X+a.W <= r.X+r.W
				}) {
					t.Errorf("%s: spawn of %s at %+v has no one floor under all of it", m.ID, s.Mob, a)
				}
				if slices.ContainsFunc(m.Geometry.Solids, a.Overlaps) {
					t.Errorf("%s: spawn of %s at %+v overlaps a solid its mobs would walk through", m.ID, s.Mob, a)
				}
			}
		}
	})
	t.Run("every map can be walked to from the start town, and every hunting map back to its town", func(t *testing.T) {
		for _, m := range d.Maps() {
			if !reaches(d, d.StartTown().ID, m.ID) {
				t.Errorf("%s: no portals lead there from %s", m.ID, d.StartTown().ID)
			}
			if !reaches(d, m.ID, m.Town) {
				t.Errorf("%s: no portals lead back to its town %s", m.ID, m.Town)
			}
		}
	})
	t.Run("a mob's EXP never drops as its level rises, normal mobs and elites each among their own", func(t *testing.T) {
		for _, elite := range []bool{false, true} {
			var mobs []gamedata.Mob
			for _, m := range d.Mobs() {
				if m.Elite == elite {
					mobs = append(mobs, m)
				}
			}
			slices.SortFunc(mobs, func(a, b gamedata.Mob) int { return cmp.Compare(a.Level, b.Level) })
			for k := 1; k < len(mobs); k++ {
				if lo, hi := mobs[k-1], mobs[k]; lo.Level < hi.Level && lo.Exp > hi.Exp {
					t.Errorf("%s (level %d) gives %d EXP, more than %s (level %d) at %d", lo.ID, lo.Level, lo.Exp, hi.ID, hi.Level, hi.Exp)
				}
			}
		}
	})
	t.Run("every hunting map's portals lead somewhere and back", func(t *testing.T) {
		for _, m := range d.Maps() {
			if len(m.Portals) == 0 {
				t.Fatalf("map %s has no portals", m.ID)
			}
			for _, p := range m.Portals {
				target, ok := d.Map(p.TargetMap)
				if !ok {
					t.Fatalf("%s: %s -> %s missing", m.ID, p.Name, p.TargetMap)
				}
				back := false
				for _, q := range target.Portals {
					if q.TargetMap == m.ID {
						back = true
					}
				}
				if !back {
					t.Fatalf("%s -> %s has no way back", m.ID, target.ID)
				}
			}
		}
	})
}
