// Package load reads the content in data/, checks it, and builds the read-only gamedata.Data.
package load

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/sim"
)

// Dir reads and validates every file under dir.
func Dir(dir string) (*gamedata.Data, error) {
	l := &loader{}
	steps := []func(string) error{l.loadMovement, l.loadLoot, l.loadActions, l.loadClasses, l.loadSkills, l.loadStats, l.loadExpCurve, l.loadItems, l.validateStarters, l.loadMobs, l.loadMaps, l.loadNPCs, l.loadQuests, l.checkPromotions, l.loadChat}
	for _, step := range steps {
		if err := step(dir); err != nil {
			return nil, fmt.Errorf("gamedata: %w", err)
		}
	}
	return gamedata.New(gamedata.Parts{
		Movement:        l.movement,
		Loot:            l.loot,
		Energy:          l.energy,
		Classes:         l.classes.list,
		Skills:          l.skills.list,
		StatRules:       l.stats,
		LevelCap:        l.levelCap,
		ExpToNext:       l.expToNext,
		Items:           l.items.list,
		EquipSlots:      l.slots,
		Mobs:            l.mobs.list,
		MobRules:        l.mobRules,
		FieldBoss:       l.fieldBoss,
		Maps:            l.maps.list,
		StartTown:       l.start,
		NPCs:            l.npcs.list,
		TalkReach:       l.talkReach,
		Quests:          l.quests.list,
		MaxActiveQuests: l.maxActive,
		ChatLineMax:     l.chatLineMax,
	}), nil
}

// loader holds what the files read so far give, so each later file is checked against it.
type loader struct {
	movement    sim.Params
	loot        gamedata.Loot
	cancel      float64 // actions.yaml's cancel point, for actions that don't name their own
	energy      gamedata.Energy
	mobRules    gamedata.MobRules
	fieldBoss   gamedata.FieldBossRules
	classes     byID[kernel.Class, gamedata.Class]
	skills      byID[string, gamedata.Skill]
	stats       gamedata.StatRules
	levelCap    int
	expToNext   []int64 // index level-1, for levels 1..cap-1
	items       byID[string, gamedata.Item]
	slots       []gamedata.EquipSlot
	mobs        byID[string, gamedata.Mob]
	maps        byID[string, *gamedata.Map]
	start       *gamedata.Map
	npcs        byID[string, gamedata.NPC]
	talkReach   float64
	quests      byID[string, gamedata.Quest]
	maxActive   int
	chatLineMax int
}

// byID keeps one file's entries in file order, indexed by ID.
type byID[K comparable, T any] struct {
	list []T
	idx  map[K]int
}

func (b *byID[K, T]) add(id K, v T) {
	if b.idx == nil {
		b.idx = map[K]int{}
	}
	b.idx[id] = len(b.list)
	b.list = append(b.list, v)
}

func (b *byID[K, T]) has(id K) bool {
	_, ok := b.idx[id]
	return ok
}

func (b *byID[K, T]) get(id K) (T, bool) {
	i, ok := b.idx[id]
	if !ok {
		var zero T
		return zero, false
	}
	return b.list[i], true
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

// YAML can spell NaN and infinity (.nan, .inf), which pass a check like x <= 0; these checks refuse them.

// positive is whether x is a finite number above 0.
func positive(x float64) bool { return x > 0 && !math.IsInf(x, 1) }

// nonNegative is whether x is a finite number not below 0.
func nonNegative(x float64) bool { return x >= 0 && !math.IsInf(x, 1) }

// finite is whether x is a number and not infinite.
func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

// parseDamage reads a damage type: physical or magic.
func parseDamage(raw string) (gamedata.DamageType, error) {
	switch d := gamedata.DamageType(raw); d {
	case gamedata.Physical, gamedata.Magic:
		return d, nil
	}
	return "", fmt.Errorf("damage must be physical or magic, got %q", raw)
}
