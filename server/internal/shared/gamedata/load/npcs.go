package load

import (
	"fmt"
	"slices"

	"finga/internal/gamedata"
	"finga/internal/kernel"
)

const npcsFile = "npcs.yaml"

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
	Lines   []string `yaml:"lines"` // what the client shows it saying; the server checks each is there once, as the client keys them by text
}

// loadNPCs reads npcs.yaml; the maps place each NPC, exactly once.
func (l *loader) loadNPCs(dir string) error {
	var f npcsYAML
	if err := readYAML(dir, npcsFile, &f); err != nil {
		return err
	}
	if !positive(f.TalkReach) {
		return fmt.Errorf("%s: talk_reach must be positive", npcsFile)
	}
	l.talkReach = f.TalkReach
	for _, n := range f.NPCs {
		if n.ID == "" || n.Name == "" || n.Look == "" {
			return fmt.Errorf("%s: npc %q: id, name and look are required", npcsFile, n.ID)
		}
		if l.npcs.has(n.ID) {
			return fmt.Errorf("%s: duplicate npc %q", npcsFile, n.ID)
		}
		if slices.Contains(n.Lines, "") {
			return fmt.Errorf("%s: npc %q: a line is empty", npcsFile, n.ID)
		}
		if len(slices.Compact(slices.Sorted(slices.Values(n.Lines)))) != len(n.Lines) {
			return fmt.Errorf("%s: npc %q: a line is said twice", npcsFile, n.ID)
		}
		npc := gamedata.NPC{ID: n.ID, Name: n.Name, Look: n.Look, Gives: n.Gives, Takes: n.Takes}
		// A trainer teaches a class a character is promoted to, so never the root.
		if n.Trainer != "" {
			c, ok := l.classes.get(kernel.Class(n.Trainer))
			if !ok || c.Parent == "" {
				return fmt.Errorf("%s: npc %s: trainer %q is not a class a character is promoted to", npcsFile, n.ID, n.Trainer)
			}
			npc.Trainer = c.ID
		}
		l.npcs.add(n.ID, npc)
	}
	return nil
}
