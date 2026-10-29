package application

import "finga/internal/kernel"

// Parties answers party questions from inside a tick and hears what the
// world learns about members; the party service implements it. nil means
// nobody has a party.
type Parties interface {
	Members(id kernel.CharacterID) []kernel.CharacterID
	SameParty(a, b kernel.CharacterID) bool
	Update(id kernel.CharacterID, mapID string, level, hp, maxHP int)
}

type chatMsg struct {
	from kernel.CharacterID
	name string
	text string
}

// Chat says an already cleaned line to everyone on the map, the sender
// included. A sender who is not on the map is ignored.
func (i *Instance) Chat(from kernel.CharacterID, name, text string) bool {
	return i.post(message{chat: &chatMsg{from: from, name: name, text: text}})
}

// shareExp splits a kill's EXP with the killer's party members who are
// alive on this map (page 5, OD-04 default): total = base × (1 + 0.1 ×
// (members − 1)) rounded down, split equally rounded down, the remainder
// to the killer. Alone, the killer gets the base.
func (i *Instance) shareExp(killer *player, base int64) {
	sharers := []*player{killer}
	if i.opts.Parties != nil {
		for _, id := range i.opts.Parties.Members(killer.ID) {
			if id == killer.ID {
				continue
			}
			if m, ok := i.players[id]; ok && !m.dead {
				sharers = append(sharers, m)
			}
		}
	}
	n := int64(len(sharers))
	if n == 1 {
		i.award(killer, base)
		return
	}
	total := base * (10 + n - 1) / 10
	share := total / n
	rem := total - share*n
	for _, m := range sharers {
		amt := share
		if m == killer {
			amt += rem
		}
		i.award(m, amt)
	}
}

// tellParty reports a member's map, level and HP after a change worth a
// PartyState: a level-up, a respawn, a map change.
func (i *Instance) tellParty(p *player, mapID string) {
	if i.opts.Parties != nil {
		i.opts.Parties.Update(p.ID, mapID, p.Level, p.HP, p.derived.MaxHP)
	}
}
