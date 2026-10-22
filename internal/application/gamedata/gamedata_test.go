package gamedata_test

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"finga/internal/application/gamedata"
	"finga/internal/application/world/sim"
	"finga/internal/model"
)

// fixture is a minimal valid data folder, as file name to content. Each
// rejection row breaks one thing in it.
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
		"classes.yaml": `
classes:
  - id: warrior
    name: Warrior
    attack: {kind: melee, width: 64, height: 56, cooldown: 600ms}
    base_stats: {str: 12, dex: 5, int: 4, agi: 5, vit: 10}
    level_up_points: {str: 3, vit: 2}
    starter_items: [{item: wooden_sword, quantity: 1}]
  - id: bowman
    name: Bowman
    attack: {kind: ranged, range: 420, cooldown: 800ms}
    base_stats: {str: 5, dex: 12, int: 4, agi: 10, vit: 5}
    level_up_points: {dex: 3, agi: 2}
`,
		"stats.yaml": `
warrior:
  max_hp:       {base: 60, per_level: 18, coef: {vit: 12, str: 2}}
  max_mp:       {base: 20, per_level: 4, coef: {int: 2}}
  attack_power: {coef: {str: 1.5, vit: 0.3}}
  accuracy:     {base: 10, coef: {dex: 1.0, str: 0.5}}
  evasion:      {base: 5, coef: {agi: 0.8}}
  crit_rate:    {base: 0.05, coef: {agi: 0.002}, cap: 0.6}
  crit_damage:  {base: 0.5, coef: {dex: 0.005}}
  defense:      {base: 5, coef: {vit: 1.0}}
  penetration:  {cap: 0.5}
bowman:
  max_hp:       {base: 50, per_level: 12, coef: {vit: 9, agi: 1}}
  max_mp:       {base: 25, per_level: 5, coef: {int: 2}}
  attack_power: {coef: {dex: 1.5, agi: 0.3}}
  accuracy:     {base: 15, coef: {dex: 1.2}}
  evasion:      {base: 8, coef: {agi: 1.0}}
  crit_rate:    {base: 0.05, coef: {agi: 0.003}, cap: 0.6}
  crit_damage:  {base: 0.5, coef: {dex: 0.008}}
  defense:      {base: 3, coef: {vit: 0.6}}
  penetration:  {cap: 0.5}
`,
		"exp_curve.yaml": `
level_cap: 4
exp_to_next:
  - {level: 1, exp: 20}
  - {level: 2, exp: 65}
  - {level: 3, exp: 129}
`,
		"items.yaml": `
items:
  - {id: wooden_sword, name: Wooden Sword, tab: equip, slot: weapon, class: warrior, min_level: 1, attack: 10}
  - {id: red_potion, name: Red Potion, tab: use, heal_hp: 50}
  - {id: slime_gel, name: Slime Gel, tab: etc}
`,
		"mobs.yaml": `
mobs:
  - id: green_slime
    name: Green Slime
    level: 1
    hp: 30
    attack: 6
    defense: 2
    accuracy: 8
    evasion: 2
    exp: 5
    speed: 40
    width: 32
    height: 28
    behavior: patrol
    drops:
      - {item: slime_gel, chance: 0.6, min: 1, max: 2}
      - {item: red_potion, chance: 0.15, min: 1, max: 1}
`,
		"maps/town.tmj": tiledMap("town", "", []object{
			{layer: "solids", name: "floor", x: 0, y: 608, w: 1280, h: 32},
			{layer: "platforms", name: "ledge", x: 96, y: 416, w: 192, h: 8},
			{layer: "ropes", name: "rope", x: 284, y: 416, w: 8, h: 192},
			{layer: "portals", name: "to_field", x: 1200, y: 544, w: 32, h: 64, props: `{"name":"target_map","type":"string","value":"field"},{"name":"target_portal","type":"string","value":"from_town"}`},
			{layer: "player_spawn", name: "start", x: 100, y: 576, w: 32, h: 32},
		}),
		"maps/field.tmj": tiledMap("hunting", "", []object{
			{layer: "solids", name: "floor", x: 0, y: 608, w: 1280, h: 32},
			{layer: "portals", name: "from_town", x: 40, y: 544, w: 32, h: 64, props: `{"name":"target_map","type":"string","value":"town"},{"name":"target_portal","type":"string","value":"to_field"}`},
			{layer: "spawns", name: "slimes", x: 300, y: 500, w: 400, h: 108, props: `{"name":"mob","type":"string","value":"green_slime"},{"name":"max","type":"int","value":4},{"name":"respawn_seconds","type":"int","value":10}`},
			{layer: "player_spawn", name: "start", x: 80, y: 576, w: 32, h: 32},
		}),
	}
}

type object struct {
	layer, name string
	x, y, w, h  float64
	props       string
}

// tiledMap renders a Tiled JSON map with the six object layers, in the
// shape Tiled 1.11 exports. extraLayers is spliced in as is; dropLayer
// omits one layer.
func tiledMap(kind, dropLayer string, objects []object) string {
	var b strings.Builder
	b.WriteString(`{"type":"map","version":"1.10","tiledversion":"1.11.0","orientation":"orthogonal","renderorder":"right-down",`)
	b.WriteString(`"width":40,"height":20,"tilewidth":32,"tileheight":32,"infinite":false,"nextlayerid":7,"nextobjectid":99,"tilesets":[],`)
	props := `{"name":"kind","type":"string","value":"` + kind + `"}`
	if kind == "hunting" {
		props += `,{"name":"town","type":"string","value":"town"}`
	}
	b.WriteString(`"properties":[` + props + `],"layers":[`)
	first := true
	id := 1
	for i, layer := range []string{"solids", "platforms", "ropes", "portals", "spawns", "player_spawn"} {
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
	d, err := gamedata.Load(write(t, fixture()))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("classes with every derived stat", func(t *testing.T) {
		for _, class := range []model.Class{model.Warrior, model.Bowman} {
			c, ok := d.Class(class)
			if !ok || c.ID != class {
				t.Fatalf("class %s missing", class)
			}
			f, ok := d.Formulas(class)
			if !ok {
				t.Fatalf("formulas for %s missing", class)
			}
			if f.MaxHP.Base == 0 || f.AttackPower.Coef.STR+f.AttackPower.Coef.DEX == 0 || f.CritRate.Cap != 0.6 || f.Penetration.Cap != 0.5 {
				t.Fatalf("formulas for %s not loaded: %+v", class, f)
			}
		}
		if _, ok := d.Class(model.Thief); ok {
			t.Fatal("thief is not in the fixture but was found")
		}
		w, _ := d.Class(model.Warrior)
		if w.Attack.Kind != gamedata.Melee || w.Attack.Width != 64 || w.Attack.Cooldown != 600*time.Millisecond {
			t.Fatalf("warrior attack = %+v", w.Attack)
		}
		if w.LevelUpPoints != (gamedata.Stats{STR: 3, VIT: 2}) || w.BaseStats.STR != 12 {
			t.Fatalf("warrior stats = %+v / %+v", w.BaseStats, w.LevelUpPoints)
		}
		b, _ := d.Class(model.Bowman)
		if b.Attack.Kind != gamedata.Ranged || b.Attack.Range != 420 {
			t.Fatalf("bowman attack = %+v", b.Attack)
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
		if !ok || it.Tab != gamedata.Equip || it.Slot != gamedata.Weapon || it.Attack != 10 || it.Class != model.Warrior || it.MaxStack != 1 {
			t.Fatalf("wooden_sword = %+v", it)
		}
		potion, _ := d.Item("red_potion")
		if potion.Tab != gamedata.Use || potion.MaxStack != 100 || potion.HealHP != 50 {
			t.Fatalf("red_potion = %+v", potion)
		}
		m, ok := d.Mob("green_slime")
		if !ok || m.Level != 1 || m.HP != 30 || len(m.Drops) != 2 || m.Drops[0].Item != "slime_gel" || m.Drops[0].Max != 2 {
			t.Fatalf("green_slime = %+v", m)
		}
		if _, ok := d.Item("nope"); ok {
			t.Fatal("unknown item found")
		}
		if _, ok := d.Mob("nope"); ok {
			t.Fatal("unknown mob found")
		}
	})
	t.Run("movement", func(t *testing.T) {
		if d.Movement() != sim.DefaultParams() {
			t.Fatalf("movement = %+v", d.Movement())
		}
	})
	t.Run("maps", func(t *testing.T) {
		town, ok := d.Map("town")
		if !ok || town.Kind != gamedata.Town || town.ID != "town" {
			t.Fatalf("town = %+v", town)
		}
		if d.Town().ID != "town" {
			t.Fatalf("Town() = %s", d.Town().ID)
		}
		hunting, _ := d.Map("field")
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
}

func TestTiledConversion(t *testing.T) {
	d, err := gamedata.Load(write(t, fixture()))
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
		{"duplicate mob IDs", "mobs.yaml", `mobs:`, "mobs:\n  - {id: green_slime, name: X, level: 1, hp: 1, attack: 1, defense: 1, accuracy: 1, evasion: 1, exp: 1, speed: 1, width: 1, height: 1, behavior: patrol}", []string{"mobs.yaml", "green_slime", "duplicate"}},
		{"duplicate class IDs", "classes.yaml", `id: bowman`, `id: warrior`, []string{"classes.yaml", "warrior", "duplicate"}},
		{"a map missing a required layer", "maps/town.tmj", `"name":"ropes"`, `"name":"strings"`, []string{"town.tmj", "ropes"}},
		{"a map without a kind", "maps/town.tmj", `"name":"kind"`, `"name":"mood"`, []string{"town.tmj", "kind"}},
		{"a map with an unknown kind", "maps/town.tmj", `"value":"town"`, `"value":"dungeon"`, []string{"town.tmj", "kind", "dungeon"}},
		{"a hunting map without a town", "maps/field.tmj", `"name":"town"`, `"name":"home"`, []string{"field.tmj", "town"}},
		{"a hunting map whose town is not a town", "maps/field.tmj", `"name":"town","type":"string","value":"town"`, `"name":"town","type":"string","value":"field"`, []string{"field.tmj", "town", "field"}},
		{"a town with a mob spawn", "maps/town.tmj", `"name":"spawns","type":"objectgroup","visible":true,"opacity":1,"x":0,"y":0,"draworder":"topdown","objects":[]`, `"name":"spawns","type":"objectgroup","visible":true,"opacity":1,"x":0,"y":0,"draworder":"topdown","objects":[{"id":50,"name":"s","type":"","rotation":0,"visible":true,"x":1,"y":1,"width":1,"height":1,"properties":[{"name":"mob","type":"string","value":"green_slime"},{"name":"max","type":"int","value":1},{"name":"respawn_seconds","type":"int","value":1}]}]`, []string{"town.tmj", "town", "spawn"}},
		{"no town at all", "maps/town.tmj", `"value":"town"`, `"value":"hunting"`, []string{"town"}},
		{"the EXP curve must rise strictly", "exp_curve.yaml", `{level: 3, exp: 129}`, `{level: 3, exp: 65}`, []string{"exp_curve.yaml", "level 3"}},
		{"the EXP curve must cover every level below the cap", "exp_curve.yaml", "  - {level: 3, exp: 129}\n", "", []string{"exp_curve.yaml", "level 3"}},
		{"the EXP curve must not go past the cap", "exp_curve.yaml", `level_cap: 4`, `level_cap: 3`, []string{"exp_curve.yaml", "level 3"}},
		{"a class without a formula for a derived stat", "stats.yaml", "  evasion:      {base: 8, coef: {agi: 1.0}}\n", "", []string{"stats.yaml", "bowman", "evasion"}},
		{"a formula with an unknown primary stat", "stats.yaml", `coef: {vit: 12, str: 2}`, `coef: {vit: 12, luk: 2}`, []string{"stats.yaml", "luk"}},
		{"a formula for a class that doesn't exist", "stats.yaml", "bowman:", "thief:\n  max_hp: {base: 1}\nbowman:", []string{"stats.yaml", "thief"}},
		{"a class with an unknown attack kind", "classes.yaml", `kind: melee`, `kind: magic`, []string{"classes.yaml", "warrior", "magic"}},
		{"a class whose level-up preset isn't five points", "classes.yaml", `level_up_points: {str: 3, vit: 2}`, `level_up_points: {str: 3, vit: 3}`, []string{"classes.yaml", "warrior", "5"}},
		{"a class id that isn't a class", "classes.yaml", `id: bowman`, `id: archer`, []string{"classes.yaml", "archer"}},
		{"a starter item that doesn't exist", "classes.yaml", `starter_items: [{item: wooden_sword, quantity: 1}]`, `starter_items: [{item: excalibur, quantity: 1}]`, []string{"classes.yaml", "warrior", "excalibur"}},
		{"a starter stack above the stack size", "classes.yaml", `starter_items: [{item: wooden_sword, quantity: 1}]`, `starter_items: [{item: wooden_sword, quantity: 2}]`, []string{"classes.yaml", "warrior", "wooden_sword"}},
		{"an item with an unknown tab", "items.yaml", `tab: etc`, `tab: misc`, []string{"items.yaml", "slime_gel", "misc"}},
		{"equipment without a slot", "items.yaml", `slot: weapon, `, ``, []string{"items.yaml", "wooden_sword", "slot"}},
		{"an item restricted to an unknown class", "items.yaml", `class: warrior`, `class: paladin`, []string{"items.yaml", "wooden_sword", "paladin"}},
		{"an unknown key is rejected", "items.yaml", `tab: etc`, `tab: etc, colour: green`, []string{"items.yaml", "colour"}},
		{"movement constants must be positive", "movement.yaml", `gravity: 1900`, `gravity: 0`, []string{"movement.yaml", "gravity"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := fixture()
			before := files[tt.file]
			files[tt.file] = strings.Replace(before, tt.old, tt.replace, 1)
			if files[tt.file] == before {
				t.Fatalf("replacement %q not found in %s", tt.old, tt.file)
			}
			_, err := gamedata.Load(write(t, files))
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
		_, err := gamedata.Load(write(t, files))
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
		_, err := gamedata.Load(write(t, files))
		if err == nil || !strings.Contains(err.Error(), "player_spawn") || !strings.Contains(err.Error(), "field.tmj") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a map file that isn't JSON", func(t *testing.T) {
		files := fixture()
		files["maps/field.tmj"] = "{not json"
		_, err := gamedata.Load(write(t, files))
		if err == nil || !strings.Contains(err.Error(), "field.tmj") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a missing data file", func(t *testing.T) {
		files := fixture()
		delete(files, "mobs.yaml")
		_, err := gamedata.Load(write(t, files))
		if err == nil || !strings.Contains(err.Error(), "mobs.yaml") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a missing maps folder", func(t *testing.T) {
		files := fixture()
		delete(files, "maps/town.tmj")
		delete(files, "maps/field.tmj")
		_, err := gamedata.Load(write(t, files))
		if err == nil || !strings.Contains(err.Error(), "maps") {
			t.Fatalf("err = %v", err)
		}
	})
}

// The real content is checked on every run, so a content change that breaks
// a reference fails CI.
func TestRealDataLoads(t *testing.T) {
	d, err := gamedata.Load("../../../data")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		ok   bool
	}{
		{"two Stage 1 classes", len(d.Classes()) == 2},
		{"level cap 30", d.LevelCap() == 30},
		{"three maps: one town and two hunting maps", len(d.Maps()) == 3 && d.Town() != nil},
		{"three to four mobs", len(d.Mobs()) >= 3 && len(d.Mobs()) <= 4},
		{"about ten items", len(d.Items()) >= 8 && len(d.Items()) <= 14},
		{"movement constants are page 5's", d.Movement() == sim.DefaultParams()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ok {
				t.Fatal("check failed")
			}
		})
	}
	t.Run("the EXP curve is round(20 × level^1.7)", func(t *testing.T) {
		for level := 1; level < d.LevelCap(); level++ {
			want := int64(math.Round(20 * math.Pow(float64(level), 1.7)))
			got, ok := d.ExpToNext(level)
			if !ok || got != want {
				t.Fatalf("level %d: %d, want %d", level, got, want)
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
