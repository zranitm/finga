package gamedata

import "finga/internal/kernel"

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

// TalkReach is how close a character's centre must be to an NPC's feet to talk to it.
func (d *Data) TalkReach() float64 { return d.p.TalkReach }

// NPCs lists every NPC in file order.
func (d *Data) NPCs() []NPC { return d.p.NPCs }

// NPC finds an NPC.
func (d *Data) NPC(id string) (NPC, bool) { return find(d.p.NPCs, d.npcIdx, id) }
