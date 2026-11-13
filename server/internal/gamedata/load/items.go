package load

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"finga/internal/gamedata"
	"finga/internal/kernel"
)

const (
	itemsFile = "items.yaml"

	defaultMaxStack = 100
	maxStars        = 7
)

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

func (l *loader) loadItems(dir string) error {
	var file itemsYAML
	if err := readYAML(dir, itemsFile, &file); err != nil {
		return err
	}
	kinds, err := l.loadEquipSlots(file.EquipSlots)
	if err != nil {
		return err
	}
	for _, raw := range file.Items {
		if raw.ID == "" {
			return fmt.Errorf("%s: an item has no id", itemsFile)
		}
		if l.items.has(raw.ID) {
			return fmt.Errorf("%s: duplicate item %s", itemsFile, raw.ID)
		}
		it := gamedata.Item{ID: raw.ID, Name: raw.Name, Tab: gamedata.Tab(raw.Tab), Stars: raw.Stars, Description: raw.Description, MinLevel: raw.MinLevel, HealHP: raw.HealHP, HealMP: raw.HealMP, MaxStack: raw.MaxStack}
		if it.Stars < 1 || it.Stars > maxStars {
			return fmt.Errorf("%s: item %s: stars must be 1 to %d, got %d", itemsFile, it.ID, maxStars, it.Stars)
		}
		if it.Description == "" || strings.ContainsRune(it.Description, '\n') {
			return fmt.Errorf("%s: item %s: needs a description of one line", itemsFile, it.ID)
		}
		switch it.Tab {
		case gamedata.Equip:
			it.Slot = gamedata.SlotKind(raw.Slot)
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
			if raw.Class != "" && it.Slot != gamedata.Weapon {
				return fmt.Errorf("%s: item %s: only a weapon keeps a class", itemsFile, it.ID)
			}
			if raw.Class != "" {
				class := kernel.Class(raw.Class)
				if !l.classes.has(class) {
					return fmt.Errorf("%s: item %s: class %s is not in %s", itemsFile, it.ID, class, classesFile)
				}
				it.Class = class
			}
			it.MaxStack = 1 // equipment never stacks
		case gamedata.Use, gamedata.Etc:
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
		l.items.add(it.ID, it)
	}
	return nil
}

// loadEquipSlots keeps the slots a character wears and returns their kinds, in order.
func (l *loader) loadEquipSlots(raw []equipSlotYAML) ([]gamedata.SlotKind, error) {
	var kinds []gamedata.SlotKind
	for _, s := range raw {
		if s.ID == "" || s.Name == "" || s.Kind == "" {
			return nil, fmt.Errorf("%s: equip slot %q needs an id, a name and a kind", itemsFile, s.ID)
		}
		if slices.ContainsFunc(l.slots, func(e gamedata.EquipSlot) bool { return e.ID == s.ID }) {
			return nil, fmt.Errorf("%s: duplicate equip slot %s", itemsFile, s.ID)
		}
		l.slots = append(l.slots, gamedata.EquipSlot{ID: s.ID, Name: s.Name, Kind: gamedata.SlotKind(s.Kind)})
		if !slices.Contains(kinds, gamedata.SlotKind(s.Kind)) {
			kinds = append(kinds, gamedata.SlotKind(s.Kind))
		}
	}
	if len(l.slots) == 0 {
		return nil, fmt.Errorf("%s: no equip_slots", itemsFile)
	}
	return kinds, nil
}

// loadRequires reads an item's stat minimums: primary stats only, each at least 1.
func loadRequires(raw map[string]int) (gamedata.Stats, error) {
	var s gamedata.Stats
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		f := statField(&s, name)
		if f == nil {
			return s, fmt.Errorf("requires: unknown primary stat %q", name)
		}
		if raw[name] < 1 {
			return s, fmt.Errorf("requires: %s must be at least 1", name)
		}
		*f = raw[name]
	}
	return s, nil
}

// loadRolls reads an equipment item's stat ranges: at least one, each [min, max] with 0 <= min <= max and max > 0.
func loadRolls(stats map[string][]int) (gamedata.Rolls, error) {
	var r gamedata.Rolls
	if len(stats) == 0 {
		return r, errors.New("equipment needs stats")
	}
	for _, name := range slices.Sorted(maps.Keys(stats)) {
		field := setField(&r, name)
		if field == nil {
			return r, fmt.Errorf("unknown stat %s", name)
		}
		v := stats[name]
		if len(v) != 2 || v[0] < 0 || v[0] > v[1] || v[1] == 0 {
			return r, fmt.Errorf("stat %s needs a range [min, max] with 0 <= min <= max and max above 0, got %v", name, v)
		}
		*field = gamedata.Range{Min: v[0], Max: v[1]}
	}
	return r, nil
}

// setField is one stat of a StatSet, by the name the data files give it: any of the five primary and the fifteen derived.
func setField[T any](r *gamedata.StatSet[T], name string) *T {
	if name == "penetration" {
		return &r.Penetration
	}
	primary := gamedata.PerPrimary[*T]{STR: &r.STR, DEX: &r.DEX, INT: &r.INT, AGI: &r.AGI, VIT: &r.VIT}
	if p := primaryField(&primary, name); p != nil {
		return *p
	}
	flat := gamedata.Flats[*T]{
		PhysicalAttack: &r.PhysicalAttack, MagicAttack: &r.MagicAttack, PhysicalDefense: &r.PhysicalDefense, MagicDefense: &r.MagicDefense,
		MaxHP: &r.MaxHP, MaxMP: &r.MaxMP, Accuracy: &r.Accuracy, Evasion: &r.Evasion, CritDamage: &r.CritDamage, HPRegen: &r.HPRegen, MPRegen: &r.MPRegen,
	}
	if f := flatField(&flat, name); f != nil {
		return *f
	}
	rates := gamedata.Rates[*T]{CritRate: &r.CritRate, CooldownSpeed: &r.CooldownSpeed, MoveSpeed: &r.MoveSpeed}
	if f := rateField(&rates, name); f != nil {
		return *f
	}
	return nil
}

// validateStarters checks starter items: known templates, positive quantities, one of each equipment piece.
func (l *loader) validateStarters(string) error {
	for _, c := range l.classes.list {
		for _, g := range c.StarterItems {
			it, ok := l.items.get(g.Item)
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
