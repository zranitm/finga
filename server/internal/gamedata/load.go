package gamedata

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"finga/internal/kernel"
	"finga/internal/sim"
)

const (
	movementFile = "movement.yaml"
	lootFile     = "loot.yaml"
	classesFile  = "classes.yaml"
	statsFile    = "stats.yaml"
	expCurveFile = "exp_curve.yaml"
	itemsFile    = "items.yaml"
	mobsFile     = "mobs.yaml"
	chatFile     = "chat.yaml"
	mapsDir      = "maps"
	mapExt       = ".tmj"

	defaultMaxStack = 100
	maxStars        = 7
)

// Load reads and validates every file under dir.
func Load(dir string) (*Data, error) {
	d := &Data{
		classIdx: map[kernel.Class]int{},
		itemIdx:  map[string]int{},
		mobIdx:   map[string]int{},
		mapIdx:   map[string]int{},
		npcIdx:   map[string]int{},
		questIdx: map[string]int{},
	}
	steps := []func(string) error{d.loadMovement, d.loadLoot, d.loadClasses, d.loadStats, d.loadExpCurve, d.loadItems, d.validateStarters, d.loadMobs, d.loadMaps, d.loadNPCs, d.loadQuests, d.loadChat}
	for _, step := range steps {
		if err := step(dir); err != nil {
			return nil, fmt.Errorf("gamedata: %w", err)
		}
	}
	return d, nil
}

// readYAML decodes one file strictly: an unknown key is an error.
func readYAML(dir, name string, into any) error {
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

type movementYAML struct {
	Dt           float64 `yaml:"dt"`
	WalkSpeed    float64 `yaml:"walk_speed"`
	JumpVelocity float64 `yaml:"jump_velocity"`
	Gravity      float64 `yaml:"gravity"`
	MaxFallSpeed float64 `yaml:"max_fall_speed"`
	ClimbSpeed   float64 `yaml:"climb_speed"`
	DropThrough  float64 `yaml:"drop_through"`
	Width        float64 `yaml:"width"`
	Height       float64 `yaml:"height"`
}

func (d *Data) loadMovement(dir string) error {
	var m movementYAML
	if err := readYAML(dir, movementFile, &m); err != nil {
		return err
	}
	for _, v := range []struct {
		name  string
		value float64
	}{
		{"dt", m.Dt}, {"walk_speed", m.WalkSpeed}, {"jump_velocity", m.JumpVelocity}, {"gravity", m.Gravity},
		{"max_fall_speed", m.MaxFallSpeed}, {"climb_speed", m.ClimbSpeed}, {"drop_through", m.DropThrough},
		{"width", m.Width}, {"height", m.Height},
	} {
		if v.value <= 0 {
			return fmt.Errorf("%s: %s must be positive", movementFile, v.name)
		}
	}
	d.movement = sim.Params{
		Dt: m.Dt, WalkSpeed: m.WalkSpeed, JumpVelocity: m.JumpVelocity, Gravity: m.Gravity, MaxFallSpeed: m.MaxFallSpeed,
		ClimbSpeed: m.ClimbSpeed, DropThrough: m.DropThrough, Width: m.Width, Height: m.Height,
	}
	return nil
}

type lootYAML struct {
	CollectDelay   time.Duration `yaml:"collect_delay"`
	DropLife       time.Duration `yaml:"drop_life"`
	RoomCheckEvery time.Duration `yaml:"room_check_every"`
}

type chatYAML struct {
	LineMax int `yaml:"line_max"`
}

func (d *Data) loadChat(dir string) error {
	var c chatYAML
	if err := readYAML(dir, chatFile, &c); err != nil {
		return err
	}
	if c.LineMax <= 0 {
		return fmt.Errorf("%s: line_max must be positive", chatFile)
	}
	d.chatLineMax = c.LineMax
	return nil
}

func (d *Data) loadLoot(dir string) error {
	var l lootYAML
	if err := readYAML(dir, lootFile, &l); err != nil {
		return err
	}
	for _, v := range []struct {
		name  string
		value time.Duration
	}{{"collect_delay", l.CollectDelay}, {"drop_life", l.DropLife}, {"room_check_every", l.RoomCheckEvery}} {
		if v.value <= 0 {
			return fmt.Errorf("%s: %s must be positive", lootFile, v.name)
		}
	}
	// A drop must have time to fly, and to be asked about again, before it is lost.
	if l.CollectDelay >= l.DropLife || l.RoomCheckEvery >= l.DropLife {
		return fmt.Errorf("%s: collect_delay and room_check_every must be shorter than drop_life", lootFile)
	}
	d.loot = Loot(l)
	return nil
}

type classesYAML struct {
	Classes []classYAML `yaml:"classes"`
}

type classYAML struct {
	ID           string     `yaml:"id"`
	Name         string     `yaml:"name"`
	Attack       attackYAML `yaml:"attack"`
	BaseStats    Stats      `yaml:"base_stats"`
	AutoAssign   Stats      `yaml:"auto_assign"`
	StarterItems []Grant    `yaml:"starter_items"`
}

type attackYAML struct {
	Kind     string        `yaml:"kind"`
	Damage   string        `yaml:"damage"`
	Width    float64       `yaml:"width"`
	Height   float64       `yaml:"height"`
	Range    float64       `yaml:"range"`
	Cooldown time.Duration `yaml:"cooldown"`
}

func (d *Data) loadClasses(dir string) error {
	var file classesYAML
	if err := readYAML(dir, classesFile, &file); err != nil {
		return err
	}
	if len(file.Classes) == 0 {
		return fmt.Errorf("%s: no classes", classesFile)
	}
	for _, c := range file.Classes {
		id, err := kernel.ParseClass(c.ID)
		if err != nil {
			return fmt.Errorf("%s: class %q: not a class", classesFile, c.ID)
		}
		if _, dup := d.classIdx[id]; dup {
			return fmt.Errorf("%s: duplicate class %s", classesFile, id)
		}
		kind := AttackKind(c.Attack.Kind)
		switch kind {
		case Melee:
			if c.Attack.Width <= 0 || c.Attack.Height <= 0 {
				return fmt.Errorf("%s: class %s: a melee attack needs width and height", classesFile, id)
			}
		case Ranged:
			if c.Attack.Range <= 0 {
				return fmt.Errorf("%s: class %s: a ranged attack needs a range", classesFile, id)
			}
		default:
			return fmt.Errorf("%s: class %s: unknown attack kind %q", classesFile, id, c.Attack.Kind)
		}
		if c.Attack.Cooldown <= 0 {
			return fmt.Errorf("%s: class %s: cooldown must be positive", classesFile, id)
		}
		damage, err := parseDamage(c.Attack.Damage)
		if err != nil {
			return fmt.Errorf("%s: class %s: attack: %w", classesFile, id, err)
		}
		if c.AutoAssign.STR < 0 || c.AutoAssign.DEX < 0 || c.AutoAssign.INT < 0 || c.AutoAssign.AGI < 0 || c.AutoAssign.VIT < 0 {
			return fmt.Errorf("%s: class %s: auto_assign must not be negative", classesFile, id)
		}
		d.classIdx[id] = len(d.classes)
		d.classes = append(d.classes, Class{
			ID: id, Name: c.Name,
			Attack:       Attack{Kind: kind, Damage: damage, Width: c.Attack.Width, Height: c.Attack.Height, Range: c.Attack.Range, Cooldown: c.Attack.Cooldown},
			BaseStats:    c.BaseStats,
			AutoAssign:   c.AutoAssign,
			StarterItems: c.StarterItems,
		})
	}
	return nil
}

type expCurveYAML struct {
	LevelCap  int `yaml:"level_cap"`
	ExpToNext []struct {
		Level int   `yaml:"level"`
		Exp   int64 `yaml:"exp"`
	} `yaml:"exp_to_next"`
}

func (d *Data) loadExpCurve(dir string) error {
	var file expCurveYAML
	if err := readYAML(dir, expCurveFile, &file); err != nil {
		return err
	}
	if file.LevelCap < 2 {
		return fmt.Errorf("%s: level_cap must be at least 2", expCurveFile)
	}
	d.levelCap = file.LevelCap
	d.expToNext = make([]int64, file.LevelCap-1)
	seen := make([]bool, file.LevelCap-1)
	for _, e := range file.ExpToNext {
		if e.Level < 1 || e.Level >= file.LevelCap {
			return fmt.Errorf("%s: level %d is at or past the cap %d", expCurveFile, e.Level, file.LevelCap)
		}
		if seen[e.Level-1] {
			return fmt.Errorf("%s: duplicate level %d", expCurveFile, e.Level)
		}
		if e.Exp < 1 {
			return fmt.Errorf("%s: level %d must need positive EXP", expCurveFile, e.Level)
		}
		seen[e.Level-1] = true
		d.expToNext[e.Level-1] = e.Exp
	}
	for level := 1; level < file.LevelCap; level++ {
		if !seen[level-1] {
			return fmt.Errorf("%s: no entry for level %d", expCurveFile, level)
		}
		if level > 1 && d.expToNext[level-1] <= d.expToNext[level-2] {
			return fmt.Errorf("%s: level %d must need more EXP than level %d", expCurveFile, level, level-1)
		}
	}
	return nil
}

type itemsYAML struct {
	EquipSlots []equipSlotYAML `yaml:"equip_slots"`
	Items      []itemYAML      `yaml:"items"`
}

type equipSlotYAML struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	Kind string `yaml:"kind"`
}

type itemYAML struct {
	ID          string           `yaml:"id"`
	Name        string           `yaml:"name"`
	Tab         string           `yaml:"tab"`
	Stars       int              `yaml:"stars"`
	Description string           `yaml:"description"`
	Slot        string           `yaml:"slot"`
	Class       string           `yaml:"class"`
	MinLevel    int              `yaml:"min_level"`
	Requires    map[string]int   `yaml:"requires"`
	Stats       map[string][]int `yaml:"stats"`
	HealHP      int              `yaml:"heal_hp"`
	HealMP      int              `yaml:"heal_mp"`
	MaxStack    int              `yaml:"max_stack"`
}

func (d *Data) loadItems(dir string) error {
	var file itemsYAML
	if err := readYAML(dir, itemsFile, &file); err != nil {
		return err
	}
	kinds, err := d.loadEquipSlots(file.EquipSlots)
	if err != nil {
		return err
	}
	for _, raw := range file.Items {
		if raw.ID == "" {
			return fmt.Errorf("%s: an item has no id", itemsFile)
		}
		if _, dup := d.itemIdx[raw.ID]; dup {
			return fmt.Errorf("%s: duplicate item %s", itemsFile, raw.ID)
		}
		it := Item{ID: raw.ID, Name: raw.Name, Tab: Tab(raw.Tab), Stars: raw.Stars, Description: raw.Description, MinLevel: raw.MinLevel, HealHP: raw.HealHP, HealMP: raw.HealMP, MaxStack: raw.MaxStack}
		if it.Stars < 1 || it.Stars > maxStars {
			return fmt.Errorf("%s: item %s: stars must be 1 to %d, got %d", itemsFile, it.ID, maxStars, it.Stars)
		}
		if it.Description == "" || strings.ContainsRune(it.Description, '\n') {
			return fmt.Errorf("%s: item %s: needs a description of one line", itemsFile, it.ID)
		}
		switch it.Tab {
		case Equip:
			it.Slot = SlotKind(raw.Slot)
			if !slices.Contains(kinds, it.Slot) {
				return fmt.Errorf("%s: item %s: equipment needs a slot kind from equip_slots, got %q", itemsFile, it.ID, raw.Slot)
			}
			rolls, err := loadRolls(raw.Stats)
			if err != nil {
				return fmt.Errorf("%s: item %s: %w", itemsFile, it.ID, err)
			}
			it.Rolls = rolls
			if it.Requires, err = loadRequires(raw.Requires); err != nil {
				return fmt.Errorf("%s: item %s: %w", itemsFile, it.ID, err)
			}
			if raw.Class != "" && it.Slot != Weapon {
				return fmt.Errorf("%s: item %s: only a weapon keeps a class", itemsFile, it.ID)
			}
			if raw.Class != "" {
				class, err := kernel.ParseClass(raw.Class)
				if err != nil {
					return fmt.Errorf("%s: item %s: unknown class %q", itemsFile, it.ID, raw.Class)
				}
				if _, ok := d.classIdx[class]; !ok {
					return fmt.Errorf("%s: item %s: class %s is not in %s", itemsFile, it.ID, class, classesFile)
				}
				it.Class = class
			}
			it.MaxStack = 1 // equipment never stacks
		case Use, Etc:
			if raw.Slot != "" || raw.Class != "" || raw.Stats != nil || raw.Requires != nil {
				return fmt.Errorf("%s: item %s: only equipment has a slot, a class, requirements or stats", itemsFile, it.ID)
			}
			if it.MaxStack == 0 {
				it.MaxStack = defaultMaxStack
			}
			if it.MaxStack < 1 {
				return fmt.Errorf("%s: item %s: max_stack must be positive", itemsFile, it.ID)
			}
		default:
			return fmt.Errorf("%s: item %s: unknown tab %q", itemsFile, it.ID, raw.Tab)
		}
		d.itemIdx[it.ID] = len(d.items)
		d.items = append(d.items, it)
	}
	return nil
}

// loadEquipSlots keeps the slots a character wears and returns their kinds, in order.
func (d *Data) loadEquipSlots(raw []equipSlotYAML) ([]SlotKind, error) {
	var kinds []SlotKind
	for _, s := range raw {
		if s.ID == "" || s.Name == "" || s.Kind == "" {
			return nil, fmt.Errorf("%s: equip slot %q needs an id, a name and a kind", itemsFile, s.ID)
		}
		if slices.ContainsFunc(d.slots, func(e EquipSlot) bool { return e.ID == s.ID }) {
			return nil, fmt.Errorf("%s: duplicate equip slot %s", itemsFile, s.ID)
		}
		d.slots = append(d.slots, EquipSlot{ID: s.ID, Name: s.Name, Kind: SlotKind(s.Kind)})
		if !slices.Contains(kinds, SlotKind(s.Kind)) {
			kinds = append(kinds, SlotKind(s.Kind))
		}
	}
	if len(d.slots) == 0 {
		return nil, fmt.Errorf("%s: no equip_slots", itemsFile)
	}
	return kinds, nil
}

type mobsYAML struct {
	Mobs []mobYAML `yaml:"mobs"`
}

type mobYAML struct {
	ID              string     `yaml:"id"`
	Name            string     `yaml:"name"`
	Level           int        `yaml:"level"`
	Elite           bool       `yaml:"elite"`
	HP              int        `yaml:"hp"`
	Attack          int        `yaml:"attack"`
	Damage          string     `yaml:"damage"`
	PhysicalDefense int        `yaml:"physical_defense"`
	MagicDefense    int        `yaml:"magic_defense"`
	Accuracy        int        `yaml:"accuracy"`
	Evasion         int        `yaml:"evasion"`
	Exp             int64      `yaml:"exp"`
	Speed           float64    `yaml:"speed"`
	Width           float64    `yaml:"width"`
	Height          float64    `yaml:"height"`
	Behavior        string     `yaml:"behavior"`
	Look            lookYAML   `yaml:"look"`
	Drops           []dropYAML `yaml:"drops"`
}

// lookYAML is the client's art for a mob: a sprite it draws, tinted and scaled; the server only checks it is there.
type lookYAML struct {
	Sprite string   `yaml:"sprite"`
	Tint   *int     `yaml:"tint"`
	Scale  *float64 `yaml:"scale"`
}

type dropYAML struct {
	Item   string  `yaml:"item"`
	Chance float64 `yaml:"chance"`
	Min    int     `yaml:"min"`
	Max    int     `yaml:"max"`
}

func (d *Data) loadMobs(dir string) error {
	var file mobsYAML
	if err := readYAML(dir, mobsFile, &file); err != nil {
		return err
	}
	for _, raw := range file.Mobs {
		if raw.ID == "" {
			return fmt.Errorf("%s: a mob has no id", mobsFile)
		}
		if _, dup := d.mobIdx[raw.ID]; dup {
			return fmt.Errorf("%s: duplicate mob %s", mobsFile, raw.ID)
		}
		if raw.Level < 1 || raw.HP < 1 || raw.Width <= 0 || raw.Height <= 0 || raw.Speed < 0 || raw.Exp < 0 {
			return fmt.Errorf("%s: mob %s: level and hp must be at least 1, width and height positive, speed and exp not negative", mobsFile, raw.ID)
		}
		if raw.Look.Sprite == "" || raw.Look.Tint != nil && (*raw.Look.Tint < 0 || *raw.Look.Tint > 0xffffff) || raw.Look.Scale != nil && *raw.Look.Scale <= 0 {
			return fmt.Errorf("%s: mob %s: look needs a sprite, a tint is an RGB color and a scale is positive", mobsFile, raw.ID)
		}
		if raw.Behavior != "patrol" {
			return fmt.Errorf("%s: mob %s: unknown behavior %q (Stage 1 has patrol)", mobsFile, raw.ID, raw.Behavior)
		}
		damage, err := parseDamage(raw.Damage)
		if err != nil {
			return fmt.Errorf("%s: mob %s: %w", mobsFile, raw.ID, err)
		}
		if raw.Attack < 0 || raw.PhysicalDefense < 0 || raw.MagicDefense < 0 || raw.Accuracy < 0 || raw.Evasion < 0 {
			return fmt.Errorf("%s: mob %s: attack, defenses, accuracy and evasion must not be negative", mobsFile, raw.ID)
		}
		mob := Mob{
			ID: raw.ID, Name: raw.Name, Level: raw.Level, Elite: raw.Elite, HP: raw.HP, Attack: raw.Attack, Damage: damage,
			PhysicalDefense: raw.PhysicalDefense, MagicDefense: raw.MagicDefense,
			Accuracy: raw.Accuracy, Evasion: raw.Evasion, Exp: raw.Exp, Speed: raw.Speed, Width: raw.Width, Height: raw.Height,
			Behavior: raw.Behavior,
		}
		for _, dr := range raw.Drops {
			item, ok := d.Item(dr.Item)
			if !ok {
				return fmt.Errorf("%s: mob %s: drop %s: unknown item", mobsFile, raw.ID, dr.Item)
			}
			if dr.Chance <= 0 || dr.Chance > 1 {
				return fmt.Errorf("%s: mob %s: drop %s: chance must be within (0, 1]", mobsFile, raw.ID, dr.Item)
			}
			if dr.Min < 1 || dr.Max < dr.Min {
				return fmt.Errorf("%s: mob %s: drop %s: need 1 <= min <= max", mobsFile, raw.ID, dr.Item)
			}
			if item.Tab == Equip && dr.Max != 1 {
				return fmt.Errorf("%s: mob %s: drop %s: equipment never stacks, max must be 1", mobsFile, raw.ID, dr.Item)
			}
			mob.Drops = append(mob.Drops, Drop(dr))
		}
		d.mobIdx[mob.ID] = len(d.mobs)
		d.mobs = append(d.mobs, mob)
	}
	return nil
}

func (d *Data) loadMaps(dir string) error {
	entries, err := os.ReadDir(filepath.Join(dir, mapsDir))
	if err != nil {
		return fmt.Errorf("%s: %w", mapsDir, err)
	}
	for _, e := range entries { // ReadDir sorts by name
		if e.IsDir() || filepath.Ext(e.Name()) != mapExt {
			continue
		}
		m, err := d.loadMap(filepath.Join(dir, mapsDir, e.Name()))
		if err != nil {
			return err
		}
		d.mapIdx[m.ID] = len(d.maps)
		d.maps = append(d.maps, m)
	}
	if len(d.maps) == 0 {
		return fmt.Errorf("%s: no %s maps", mapsDir, mapExt)
	}
	// Portals can only be checked once every map is loaded.
	for _, m := range d.maps {
		file := m.ID + mapExt
		for _, p := range m.Portals {
			target, ok := d.Map(p.TargetMap)
			if !ok {
				return fmt.Errorf("%s: layer %q: object %q: target map %q does not exist", file, layerPortals, p.Name, p.TargetMap)
			}
			if !slices.ContainsFunc(target.Portals, func(q Portal) bool { return q.Name == p.TargetPortal }) {
				return fmt.Errorf("%s: layer %q: object %q: target portal %q not in map %q", file, layerPortals, p.Name, p.TargetPortal, p.TargetMap)
			}
		}
	}
	for _, m := range d.maps {
		town, ok := d.Map(m.Town)
		if !ok || town.Kind != Town {
			return fmt.Errorf("%s%s: map property %q names %q, which is not a town", m.ID, mapExt, propTown, m.Town)
		}
	}
	for _, m := range d.maps {
		if !m.Start {
			continue
		}
		if d.start != nil {
			return fmt.Errorf("maps: %s and %s both have the property %q; new characters start in one town", d.start.ID+mapExt, m.ID+mapExt, propStart)
		}
		if m.Kind != Town {
			return fmt.Errorf("%s%s: the map property %q is for a town, where new characters start", m.ID, mapExt, propStart)
		}
		d.start = m
	}
	if d.start == nil {
		return fmt.Errorf("maps: no town has the property %q, where new characters start", propStart)
	}
	return nil
}

// parseDamage reads a damage type: physical or magic.
func parseDamage(raw string) (DamageType, error) {
	switch d := DamageType(raw); d {
	case Physical, Magic:
		return d, nil
	}
	return "", fmt.Errorf("damage must be physical or magic, got %q", raw)
}

// validateStarters checks starter items: known templates, positive quantities, one of each equipment piece.
func (d *Data) validateStarters(string) error {
	for _, c := range d.classes {
		for _, g := range c.StarterItems {
			it, ok := d.Item(g.Item)
			if !ok {
				return fmt.Errorf("%s: class %s: starter item %s: unknown item", classesFile, c.ID, g.Item)
			}
			if g.Quantity < 1 || g.Quantity > it.MaxStack {
				return fmt.Errorf("%s: class %s: starter item %s: quantity must be 1 to %d", classesFile, c.ID, g.Item, it.MaxStack)
			}
		}
	}
	return nil
}
