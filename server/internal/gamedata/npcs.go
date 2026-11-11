package gamedata

import (
	"fmt"
	"slices"

	"finga/internal/kernel"
)

const (
	npcsFile   = "npcs.yaml"
	questsFile = "quests.yaml"
)

// NPC is a character placed on one map that offers services: quests to give and take back, and a promotion for a trainer.
type NPC struct {
	ID      string
	Name    string
	Look    string       // the client's sprite key
	Trainer kernel.Class // the class it promotes to; empty for none
	Gives   []string     // quest IDs
	Takes   []string     // quest IDs handed in to it
	MapID   string
	At      Point // where it stands: its feet
}

// ObjectiveKind is what an objective asks for.
type ObjectiveKind string

// The four kinds of objective.
const (
	Kill    ObjectiveKind = "kill"
	Collect ObjectiveKind = "collect"
	TalkTo  ObjectiveKind = "talk"
	Reach   ObjectiveKind = "level"
)

// Objective is one task of a quest: Count of a mob (Kill) or an item (Collect), an NPC to talk to (Count 1), or a level to reach (Count is the level).
type Objective struct {
	Kind   ObjectiveKind
	Target string // the mob, item or NPC; empty for a level
	Count  int
}

// Rewards are what handing a quest in gives.
type Rewards struct {
	Exp   int64
	Items []Grant
}

// Quest is a task an NPC gives and another (often the same) takes back.
type Quest struct {
	ID         string
	Name       string
	Giver      string
	TurnIn     string
	Level      int          // the least level to accept it
	Class      kernel.Class // empty: any class
	Requires   []string     // quests finished before
	Objectives []Objective
	Rewards    Rewards
	Repeatable bool
}

type npcsYAML struct {
	TalkReach float64   `yaml:"talk_reach"`
	NPCs      []npcYAML `yaml:"npcs"`
}

type npcYAML struct {
	ID      string   `yaml:"id"`
	Name    string   `yaml:"name"`
	Look    string   `yaml:"look"`
	Trainer string   `yaml:"trainer"`
	Gives   []string `yaml:"gives"`
	Takes   []string `yaml:"takes"`
	Lines   []string `yaml:"lines"` // what the client shows it saying; the server only checks none is empty
}

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
		Exp   int64   `yaml:"exp"`
		Items []Grant `yaml:"items"`
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

// loadNPCs reads npcs.yaml and places each NPC where a map's npcs layer puts it, exactly once.
func (d *Data) loadNPCs(dir string) error {
	var f npcsYAML
	if err := readYAML(dir, npcsFile, &f); err != nil {
		return err
	}
	if f.TalkReach <= 0 {
		return fmt.Errorf("%s: talk_reach must be positive", npcsFile)
	}
	d.talkReach = f.TalkReach
	for _, n := range f.NPCs {
		if n.ID == "" || n.Name == "" || n.Look == "" {
			return fmt.Errorf("%s: npc %q: id, name and look are required", npcsFile, n.ID)
		}
		if _, dup := d.npcIdx[n.ID]; dup {
			return fmt.Errorf("%s: duplicate npc %q", npcsFile, n.ID)
		}
		if slices.Contains(n.Lines, "") {
			return fmt.Errorf("%s: npc %q: a line is empty", npcsFile, n.ID)
		}
		npc := NPC{ID: n.ID, Name: n.Name, Look: n.Look, Gives: n.Gives, Takes: n.Takes}
		// A trainer may teach a class the data doesn't offer yet; it gives its trial until promotions arrive.
		if n.Trainer != "" {
			c, err := kernel.ParseClass(n.Trainer)
			if err != nil {
				return fmt.Errorf("%s: npc %s: unknown trainer class %q", npcsFile, n.ID, n.Trainer)
			}
			npc.Trainer = c
		}
		d.npcIdx[n.ID] = len(d.npcs)
		d.npcs = append(d.npcs, npc)
	}
	for _, m := range d.maps {
		for _, p := range m.NPCs {
			i, ok := d.npcIdx[p.NPC]
			if !ok {
				return fmt.Errorf("%s%s: layer %q: npc %q is not in %s", m.ID, mapExt, layerNPCs, p.NPC, npcsFile)
			}
			if d.npcs[i].MapID != "" {
				return fmt.Errorf("%s%s: layer %q: npc %q already stands on %s", m.ID, mapExt, layerNPCs, p.NPC, d.npcs[i].MapID)
			}
			d.npcs[i].MapID, d.npcs[i].At = m.ID, p.At
		}
	}
	for _, n := range d.npcs {
		if n.MapID == "" {
			return fmt.Errorf("%s: npc %s stands on no map: place it in a map's %q layer", npcsFile, n.ID, layerNPCs)
		}
	}
	return nil
}

// loadQuests reads quests.yaml, then checks every reference: NPCs, mobs, items, classes, required quests and their cycles.
func (d *Data) loadQuests(dir string) error {
	var f questsYAML
	if err := readYAML(dir, questsFile, &f); err != nil {
		return err
	}
	if f.MaxActive <= 0 {
		return fmt.Errorf("%s: max_active must be positive", questsFile)
	}
	d.maxActive = f.MaxActive
	for _, y := range f.Quests {
		q, err := d.quest(y)
		if err != nil {
			return fmt.Errorf("%s: quest %q: %w", questsFile, y.ID, err)
		}
		if _, dup := d.questIdx[q.ID]; dup {
			return fmt.Errorf("%s: duplicate quest %q", questsFile, q.ID)
		}
		d.questIdx[q.ID] = len(d.quests)
		d.quests = append(d.quests, q)
	}
	for _, q := range d.quests {
		for _, r := range q.Requires {
			if _, ok := d.Quest(r); !ok {
				return fmt.Errorf("%s: quest %s: requires unknown quest %q", questsFile, q.ID, r)
			}
		}
	}
	if cycle := d.requiredCycle(); cycle != nil {
		return fmt.Errorf("%s: quests require each other in a cycle: %v", questsFile, cycle)
	}
	return d.checkQuestNPCs()
}

func (d *Data) quest(y questYAML) (Quest, error) {
	q := Quest{ID: y.ID, Name: y.Name, Giver: y.Giver, TurnIn: y.TurnIn, Level: y.Level, Requires: y.Quests, Repeatable: y.Repeatable,
		Rewards: Rewards{Exp: y.Rewards.Exp, Items: y.Rewards.Items}}
	switch {
	case q.ID == "" || q.Name == "":
		return q, fmt.Errorf("id and name are required")
	case q.Level < 0 || q.Level > d.levelCap:
		return q, fmt.Errorf("level must be 0 to %d", d.levelCap)
	case q.Rewards.Exp < 0:
		return q, fmt.Errorf("rewards: exp must not be negative")
	case len(y.Objectives) == 0:
		return q, fmt.Errorf("needs at least one objective")
	}
	if y.Class != "" {
		q.Class = kernel.Class(y.Class)
		if _, ok := d.Class(q.Class); !ok {
			return q, fmt.Errorf("unknown class %q", y.Class)
		}
	}
	for _, npc := range []string{q.Giver, q.TurnIn} {
		if _, ok := d.NPC(npc); !ok {
			return q, fmt.Errorf("unknown npc %q", npc)
		}
	}
	for k, oy := range y.Objectives {
		o, err := d.objective(oy)
		if err != nil {
			return q, fmt.Errorf("objective %d: %w", k+1, err)
		}
		q.Objectives = append(q.Objectives, o)
	}
	for _, g := range q.Rewards.Items {
		it, ok := d.Item(g.Item)
		switch {
		case !ok:
			return q, fmt.Errorf("rewards: unknown item %q", g.Item)
		case it.Tab == Equip && g.Quantity != 1:
			return q, fmt.Errorf("rewards: equipment %s must have quantity 1, as each copy rolls its own stats", g.Item)
		case g.Quantity < 1 || g.Quantity > it.MaxStack:
			return q, fmt.Errorf("rewards: %s: quantity must be 1 to %d", g.Item, it.MaxStack)
		}
	}
	return q, nil
}

func (d *Data) objective(y objectiveYAML) (Objective, error) {
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
		return Objective{}, fmt.Errorf("must be exactly one of kill, collect, talk and level")
	}
	switch {
	case y.Kill != "":
		if _, ok := d.Mob(y.Kill); !ok {
			return Objective{}, fmt.Errorf("unknown mob %q", y.Kill)
		}
		return counted(Kill, y.Kill, y.Count)
	case y.Collect != "":
		if _, ok := d.Item(y.Collect); !ok {
			return Objective{}, fmt.Errorf("unknown item %q", y.Collect)
		}
		return counted(Collect, y.Collect, y.Count)
	case y.Talk != "":
		if _, ok := d.NPC(y.Talk); !ok {
			return Objective{}, fmt.Errorf("unknown npc %q", y.Talk)
		}
		if y.Count != 0 {
			return Objective{}, fmt.Errorf("talk takes no count")
		}
		return Objective{Kind: TalkTo, Target: y.Talk, Count: 1}, nil
	default:
		if y.Count != 0 {
			return Objective{}, fmt.Errorf("level takes no count")
		}
		if y.Level < 2 || y.Level > d.levelCap {
			return Objective{}, fmt.Errorf("level must be 2 to %d", d.levelCap)
		}
		return Objective{Kind: Reach, Count: y.Level}, nil
	}
}

func counted(kind ObjectiveKind, target string, count int) (Objective, error) {
	if count < 1 {
		return Objective{}, fmt.Errorf("%s %s: count must be at least 1", kind, target)
	}
	return Objective{Kind: kind, Target: target, Count: count}, nil
}

// requiredCycle is a chain of quests that ends where it starts, if any requires itself through others.
func (d *Data) requiredCycle() []string {
	const (
		unseen = iota
		open
		done
	)
	state := make([]int, len(d.quests))
	var path []string
	var visit func(i int) []string
	visit = func(i int) []string {
		state[i] = open
		path = append(path, d.quests[i].ID)
		for _, r := range d.quests[i].Requires {
			j := d.questIdx[r]
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
	for i := range d.quests {
		if state[i] == unseen {
			if c := visit(i); c != nil {
				return c
			}
		}
	}
	return nil
}

// checkQuestNPCs holds both sides to each other: a quest's giver gives it and its turn-in NPC takes it, and an NPC lists only quests that name it.
func (d *Data) checkQuestNPCs() error {
	for _, q := range d.quests {
		if g, _ := d.NPC(q.Giver); !slices.Contains(g.Gives, q.ID) {
			return fmt.Errorf("%s: quest %s: its giver %s doesn't list it under gives", questsFile, q.ID, q.Giver)
		}
		if t, _ := d.NPC(q.TurnIn); !slices.Contains(t.Takes, q.ID) {
			return fmt.Errorf("%s: quest %s: its turn-in npc %s doesn't list it under takes", questsFile, q.ID, q.TurnIn)
		}
	}
	for _, n := range d.npcs {
		for _, id := range n.Gives {
			if q, ok := d.Quest(id); !ok || q.Giver != n.ID {
				return fmt.Errorf("%s: npc %s gives %q, which isn't a quest it gives", npcsFile, n.ID, id)
			}
		}
		for _, id := range n.Takes {
			if q, ok := d.Quest(id); !ok || q.TurnIn != n.ID {
				return fmt.Errorf("%s: npc %s takes %q, which isn't a quest handed in to it", npcsFile, n.ID, id)
			}
		}
	}
	return nil
}

// TalkReach is how close a character's centre must be to an NPC's feet to talk to it.
func (d *Data) TalkReach() float64 { return d.talkReach }

// NPCs lists every NPC in file order.
func (d *Data) NPCs() []NPC { return d.npcs }

// NPC finds an NPC.
func (d *Data) NPC(id string) (NPC, bool) {
	i, ok := d.npcIdx[id]
	if !ok {
		return NPC{}, false
	}
	return d.npcs[i], true
}

// Quests lists every quest in file order.
func (d *Data) Quests() []Quest { return d.quests }

// MaxActiveQuests is the most quests a character has active at once.
func (d *Data) MaxActiveQuests() int { return d.maxActive }

// Quest finds a quest.
func (d *Data) Quest(id string) (Quest, bool) {
	i, ok := d.questIdx[id]
	if !ok {
		return Quest{}, false
	}
	return d.quests[i], true
}
