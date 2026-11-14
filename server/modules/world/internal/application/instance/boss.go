package instance

import (
	"math"
	"slices"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/modules/world/internal/application/loot"
	"finga/modules/world/internal/application/mobs"
	"finga/modules/world/internal/model"
)

// fieldBoss is the map's boss while it is up: its mob, the tick it leaves, and the HP each character took from it.
type fieldBoss struct {
	id     uint64
	def    gamedata.Mob
	leaves uint64
	dealt  map[kernel.CharacterID]int
}

// countTowardBoss counts a kill toward the map's boss; none count while it is up.
func (i *Instance) countTowardBoss() {
	if i.m.Boss != nil && i.boss == nil {
		i.bossKills++
	}
}

// stepBoss ends the tick's boss business: an unbeaten boss whose time is up leaves, and a count that reached the map's number raises one, once, which every player online hears of.
func (i *Instance) stepBoss() {
	if b := i.boss; b != nil && i.tick >= b.leaves {
		i.arena.Remove(b.id)
		i.boss = nil
		i.events = append(i.events, model.FieldBossGone{Tick: i.tick, Mob: b.id})
	}
	spot := i.m.Boss
	if spot == nil || i.boss != nil || i.bossKills < spot.Kills {
		return
	}
	def, _ := i.data.Mob(spot.Mob)
	i.boss = &fieldBoss{id: i.arena.SpawnBoss(def, spot.Area), def: def, leaves: i.tick + i.ticksFor(i.data.FieldBoss().Life), dealt: map[kernel.CharacterID]int{}}
	i.bossKills = 0
	i.events = append(i.events, i.bossUp())
	i.deps.Notices.Announce(model.BossAppeared{Boss: def.ID, Map: i.m.ID})
}

// bossUp is the boss as the map hears of it, with the time it has left from this tick.
func (i *Instance) bossUp() model.FieldBoss {
	b := i.boss
	return model.FieldBoss{Tick: i.tick, Mob: b.id, Type: b.def.ID, LeavesIn: time.Duration(b.leaves-i.tick) * i.tuning.TickInterval}
}

// bossGroup is who a beaten boss pays together, a party's members alive on the map or a player alone, and whose tallies make their damage: the whole party's, wherever its members are.
type bossGroup struct {
	present []*player
	tallied []kernel.CharacterID
}

// bossGroups are the players alive on the map, grouped by party in join order.
func (i *Instance) bossGroups() []bossGroup {
	var groups []bossGroup
	byParty := map[string]int{}
	for _, id := range i.order {
		p := i.players[id]
		if p.dead {
			continue
		}
		party := i.deps.Parties.Party(id)
		k, ok := byParty[party.ID]
		if party.ID == "" || !ok {
			k = len(groups)
			groups = append(groups, bossGroup{tallied: slices.Clone(party.Members)})
			if party.ID != "" {
				byParty[party.ID] = k
			}
		}
		g := &groups[k]
		g.present = append(g.present, p)
		if !slices.Contains(g.tallied, id) {
			g.tallied = append(g.tallied, id)
		}
	}
	return groups
}

// bossFell pays out a beaten boss: each group's EXP by its share of the boss's HP, and its own drop roll for each player whose group dealt the drop share. Damage of those not here to collect goes to no one, and the killer's kill counts for quests as any does.
func (i *Instance) bossFell(killer *player, k *mobs.Kill) {
	b := i.boss
	i.boss = nil
	for _, g := range i.bossGroups() {
		dealt := 0
		for _, id := range g.tallied {
			dealt += b.dealt[id]
		}
		share := float64(dealt) / float64(b.def.HP)
		i.splitExp(g.present, int64(math.Round(float64(b.def.Exp)*share)))
		if share < i.data.FieldBoss().DropShare {
			continue
		}
		for _, m := range g.present {
			i.ground.Roll(loot.Kill{Mob: b.def, X: k.X, Y: k.Y, Killer: ownerOf(m)})
		}
	}
	i.countKill(killer, b.def.ID)
	i.events = append(i.events, model.FieldBossGone{Tick: i.tick, Mob: b.id, Beaten: true})
}
