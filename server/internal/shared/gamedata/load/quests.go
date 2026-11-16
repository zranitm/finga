package load

import (
	"fmt"
	"slices"

	"finga/internal/gamedata"
	"finga/internal/kernel"
)

const questsFile = "quests.yaml"

type questsYAML struct {
	MaxActive int         `yaml:"max_active"`
	Quests    []questYAML `yaml:"quests"`
}

type questYAML struct {
	ID         string          `yaml:"id"`
	Name       string          `yaml:"name"`
	Giver      string          `yaml:"giver"`
	TurnIn     string          `yaml:"turn_in"`
	Level      int             `yaml:"level"`
	Class      string          `yaml:"class"`
	Quests     []string        `yaml:"quests"`
	Objectives []objectiveYAML `yaml:"objectives"`
	Rewards    struct {
		Exp   int64       `yaml:"exp"`
		Items []grantYAML `yaml:"items"`
	} `yaml:"rewards"`
	Repeatable bool `yaml:"repeatable"`
}

// objectiveYAML is one of {kill: mob, count}, {collect: item, count}, {talk: npc} or {level: n}.
type objectiveYAML struct {
	Kill    string `yaml:"kill"`
	Collect string `yaml:"collect"`
	Talk    string `yaml:"talk"`
	Level   int    `yaml:"level"`
	Count   int    `yaml:"count"`
}

// loadQuests reads quests.yaml, then checks every reference: NPCs, mobs, items, classes, required quests and their cycles.
func (l *loader) loadQuests(dir string) error {
	var f questsYAML
	if err := readYAML(dir, questsFile, &f); err != nil {
		return err
	}
	if f.MaxActive <= 0 {
		return fmt.Errorf("%s: max_active must be positive", questsFile)
	}
	l.maxActive = f.MaxActive
	for _, y := range f.Quests {
		q, err := l.quest(y)
		if err != nil {
			return fmt.Errorf("%s: quest %q: %w", questsFile, y.ID, err)
		}
		if l.quests.has(q.ID) {
			return fmt.Errorf("%s: duplicate quest %q", questsFile, q.ID)
		}
		l.quests.add(q.ID, q)
	}
	for _, q := range l.quests.list {
		for _, r := range q.Requires {
			if !l.quests.has(r) {
				return fmt.Errorf("%s: quest %s: requires unknown quest %q", questsFile, q.ID, r)
			}
		}
	}
	if cycle := l.requiredCycle(); cycle != nil {
		return fmt.Errorf("%s: quests require each other in a cycle: %v", questsFile, cycle)
	}
	return l.checkQuestNPCs()
}

func (l *loader) quest(y questYAML) (gamedata.Quest, error) {
	q := gamedata.Quest{ID: y.ID, Name: y.Name, Giver: y.Giver, TurnIn: y.TurnIn, Level: y.Level, Requires: y.Quests, Repeatable: y.Repeatable,
		Rewards: gamedata.Rewards{Exp: y.Rewards.Exp, Items: grants(y.Rewards.Items)}}
	switch {
	case q.ID == "" || q.Name == "":
		return q, fmt.Errorf("id and name are required")
	case q.Level < 0 || q.Level > l.levelCap:
		return q, fmt.Errorf("level must be 0 to %d", l.levelCap)
	case q.Rewards.Exp < 0:
		return q, fmt.Errorf("rewards: exp must not be negative")
	case len(y.Objectives) == 0:
		return q, fmt.Errorf("needs at least one objective")
	}
	if y.Class != "" {
		q.Class = kernel.Class(y.Class)
		if !l.classes.has(q.Class) {
			return q, fmt.Errorf("unknown class %q", y.Class)
		}
	}
	for _, npc := range []string{q.Giver, q.TurnIn} {
		if !l.npcs.has(npc) {
			return q, fmt.Errorf("unknown npc %q", npc)
		}
	}
	for k, oy := range y.Objectives {
		o, err := l.objective(oy)
		if err != nil {
			return q, fmt.Errorf("objective %d: %w", k+1, err)
		}
		q.Objectives = append(q.Objectives, o)
	}
	for _, g := range q.Rewards.Items {
		it, ok := l.items.get(g.Item)
		switch {
		case !ok:
			return q, fmt.Errorf("rewards: unknown item %q", g.Item)
		case it.Tab == gamedata.Equip && g.Quantity != 1:
			return q, fmt.Errorf("rewards: equipment %s must have quantity 1, as each copy rolls its own stats", g.Item)
		case g.Quantity < 1 || g.Quantity > it.MaxStack:
			return q, fmt.Errorf("rewards: %s: quantity must be 1 to %d", g.Item, it.MaxStack)
		}
	}
	return q, nil
}

func (l *loader) objective(y objectiveYAML) (gamedata.Objective, error) {
	set := 0
	for _, s := range []string{y.Kill, y.Collect, y.Talk} {
		if s != "" {
			set++
		}
	}
	if y.Level != 0 {
		set++
	}
	if set != 1 {
		return gamedata.Objective{}, fmt.Errorf("must be exactly one of kill, collect, talk and level")
	}
	switch {
	case y.Kill != "":
		if !l.mobs.has(y.Kill) {
			return gamedata.Objective{}, fmt.Errorf("unknown mob %q", y.Kill)
		}
		return counted(gamedata.Kill, y.Kill, y.Count)
	case y.Collect != "":
		if !l.items.has(y.Collect) {
			return gamedata.Objective{}, fmt.Errorf("unknown item %q", y.Collect)
		}
		return counted(gamedata.Collect, y.Collect, y.Count)
	case y.Talk != "":
		if !l.npcs.has(y.Talk) {
			return gamedata.Objective{}, fmt.Errorf("unknown npc %q", y.Talk)
		}
		if y.Count != 0 {
			return gamedata.Objective{}, fmt.Errorf("talk takes no count")
		}
		return gamedata.Objective{Kind: gamedata.TalkTo, Target: y.Talk, Count: 1}, nil
	default:
		if y.Count != 0 {
			return gamedata.Objective{}, fmt.Errorf("level takes no count")
		}
		if y.Level < 2 || y.Level > l.levelCap {
			return gamedata.Objective{}, fmt.Errorf("level must be 2 to %d", l.levelCap)
		}
		return gamedata.Objective{Kind: gamedata.Reach, Count: y.Level}, nil
	}
}

func counted(kind gamedata.ObjectiveKind, target string, count int) (gamedata.Objective, error) {
	if count < 1 {
		return gamedata.Objective{}, fmt.Errorf("%s %s: count must be at least 1", kind, target)
	}
	return gamedata.Objective{Kind: kind, Target: target, Count: count}, nil
}

// requiredCycle is a chain of quests that ends where it starts, if any requires itself through others.
func (l *loader) requiredCycle() []string {
	const (
		unseen = iota
		open
		done
	)
	quests := l.quests.list
	state := make([]int, len(quests))
	var path []string
	var visit func(i int) []string
	visit = func(i int) []string {
		state[i] = open
		path = append(path, quests[i].ID)
		for _, r := range quests[i].Requires {
			j := l.quests.idx[r]
			switch state[j] {
			case open:
				return append(path[slices.Index(path, r):], r)
			case unseen:
				if c := visit(j); c != nil {
					return c
				}
			}
		}
		path = path[:len(path)-1]
		state[i] = done
		return nil
	}
	for i := range quests {
		if state[i] == unseen {
			if c := visit(i); c != nil {
				return c
			}
		}
	}
	return nil
}

// checkQuestNPCs holds both sides to each other: a quest's giver gives it and its turn-in NPC takes it, and an NPC lists only quests that name it.
func (l *loader) checkQuestNPCs() error {
	for _, q := range l.quests.list {
		if g, _ := l.npcs.get(q.Giver); !slices.Contains(g.Gives, q.ID) {
			return fmt.Errorf("%s: quest %s: its giver %s doesn't list it under gives", questsFile, q.ID, q.Giver)
		}
		if t, _ := l.npcs.get(q.TurnIn); !slices.Contains(t.Takes, q.ID) {
			return fmt.Errorf("%s: quest %s: its turn-in npc %s doesn't list it under takes", questsFile, q.ID, q.TurnIn)
		}
	}
	for _, n := range l.npcs.list {
		for _, id := range n.Gives {
			if q, ok := l.quests.get(id); !ok || q.Giver != n.ID {
				return fmt.Errorf("%s: npc %s gives %q, which isn't a quest it gives", npcsFile, n.ID, id)
			}
		}
		for _, id := range n.Takes {
			if q, ok := l.quests.get(id); !ok || q.TurnIn != n.ID {
				return fmt.Errorf("%s: npc %s takes %q, which isn't a quest handed in to it", npcsFile, n.ID, id)
			}
		}
	}
	return nil
}
