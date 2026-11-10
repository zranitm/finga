package instance

import (
	"finga/internal/gamedata"
	"finga/internal/progression"
	"finga/services/world/internal/application/loot"
	"finga/services/world/internal/model"
)

// present is the killer and their party's other members alive on this map, who share a kill.
func (i *Instance) present(killer *player, party model.Party) []*player {
	out := []*player{killer}
	for _, id := range party.Members {
		if m, ok := i.players[id]; ok && id != killer.ID && !m.dead {
			out = append(out, m)
		}
	}
	return out
}

// claim is who may own a kill's drops: the killer, in turn with their party's members alive on this map.
func (i *Instance) claim(killer *player, mob gamedata.Mob, x, y float64) loot.Kill {
	party := i.deps.Parties.Party(killer.ID)
	k := loot.Kill{Mob: mob, X: x, Y: y, Killer: ownerOf(killer), Party: party}
	for _, m := range i.present(killer, party) {
		k.Eligible = append(k.Eligible, ownerOf(m))
	}
	return k
}

func ownerOf(p *player) loot.Owner {
	return loot.Owner{ID: p.ID, Account: p.Account, Name: p.Name, Sink: p.sink}
}

// shareExp splits a kill's EXP, plus 10% per extra member, among the live party on this map.
func (i *Instance) shareExp(killer *player, base int64) {
	sharers := i.present(killer, i.deps.Parties.Party(killer.ID))
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

// award gives a player EXP; a level gained restores HP and MP, tells the party, and is saved at once.
func (i *Instance) award(p *player, amount int64) {
	if amount <= 0 {
		return
	}
	a := progression.AwardExp(i.data, progression.Progress{Level: p.Level, Exp: p.Exp}, amount, p.Stats, progression.PresetPlan(p.class))
	p.Level, p.Exp, p.Stats = a.Progress.Level, a.Progress.Exp, a.Stats
	p.private = append(p.private, model.ExpGained{Tick: i.tick, Amount: amount, Exp: p.Exp})
	if a.LevelsGained == 0 {
		return
	}
	formulas, _ := i.data.Formulas(p.Class)
	p.derived = progression.DeriveStats(formulas, p.Stats, p.Level, p.Worn.Stats)
	p.HP, p.MP = p.derived.MaxHP, p.derived.MaxMP
	p.changed = true
	p.private = append(p.private, model.LevelUp{
		Tick: i.tick, ID: p.ID, Name: p.Name, Class: p.Class, Level: p.Level, Exp: p.Exp, Stats: p.Stats,
		HP: p.HP, MaxHP: p.derived.MaxHP, MP: p.MP, MaxMP: p.derived.MaxMP, Sheet: i.sheet(p),
	})
	i.tellParty(p, i.m.ID)
	i.checkpoint(p)
}

// retellParties tells every player's state again once the party stream subscribed again: social may have restarted, keeping only its logins' values.
func (i *Instance) retellParties() {
	epoch := i.deps.Parties.Epoch()
	if epoch == i.epoch {
		return
	}
	i.epoch = epoch
	for _, id := range i.order {
		i.tellParty(i.players[id], i.m.ID)
	}
}

func (i *Instance) tellParty(p *player, mapID string) {
	i.deps.Parties.Update(p.ID, mapID, p.Level, p.HP, p.derived.MaxHP)
}
