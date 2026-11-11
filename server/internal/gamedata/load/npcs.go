package load

import (
	"fmt"

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
}

// loadNPCs reads npcs.yaml and places each NPC where a map's npcs layer puts it, exactly once.
func (l *loader) loadNPCs(dir string) error {
	var f npcsYAML
	if err := readYAML(dir, npcsFile, &f); err != nil {
		return err
	}
	if f.TalkReach <= 0 {
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
		npc := gamedata.NPC{ID: n.ID, Name: n.Name, Look: n.Look, Gives: n.Gives, Takes: n.Takes}
		// A trainer may teach a class the data doesn't offer yet; it gives its trial until promotions arrive.
		if n.Trainer != "" {
			c, err := kernel.ParseClass(n.Trainer)
			if err != nil {
				return fmt.Errorf("%s: npc %s: unknown trainer class %q", npcsFile, n.ID, n.Trainer)
			}
			npc.Trainer = c
		}
		l.npcs.add(n.ID, npc)
	}
	for _, m := range l.maps.list {
		for _, p := range m.NPCs {
			i, ok := l.npcs.idx[p.NPC]
			if !ok {
				return fmt.Errorf("%s%s: layer %q: npc %q is not in %s", m.ID, mapExt, layerNPCs, p.NPC, npcsFile)
			}
			if l.npcs.list[i].MapID != "" {
				return fmt.Errorf("%s%s: layer %q: npc %q already stands on %s", m.ID, mapExt, layerNPCs, p.NPC, l.npcs.list[i].MapID)
			}
			l.npcs.list[i].MapID, l.npcs.list[i].At = m.ID, p.At
		}
	}
	for _, n := range l.npcs.list {
		if n.MapID == "" {
			return fmt.Errorf("%s: npc %s stands on no map: place it in a map's %q layer", npcsFile, n.ID, layerNPCs)
		}
	}
	return nil
}
