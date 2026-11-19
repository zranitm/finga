package instance

import (
	"math"
	"slices"
	"time"

	"finga/internal/application/world/mobs"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
)

// fieldBoss is the map's boss while it is up: its mob and the HP each character took from it.
type fieldBoss struct {
	id    uint64
	def   gamedata.Mob
	dealt map[kernel.CharacterID]int
}

// raiseBoss stands the map's boss at its spot.
func (i *Instance) raiseBoss() {
	def, _ := i.data.Mob(i.m.Boss.Mob)
	i.boss = &fieldBoss{id: i.arena.SpawnBoss(def, *i.m.Boss), def: def, dealt: map[kernel.CharacterID]int{}}
}

// stepBoss raises a beaten boss again once its respawn is up, which the map hears; a boss never leaves on its own.
func (i *Instance) stepBoss() {
	if i.m.Boss == nil || i.boss != nil || i.tick < i.bossBack {
		return
	}
	i.raiseBoss()
	i.events = append(i.events, i.bossUp())
}

// bossUp is the boss as the map hears of it.
func (i *Instance) bossUp() model.FieldBoss {
	return model.FieldBoss{Tick: i.tick, Mob: i.boss.id, Type: i.boss.def.ID}
}

// bossDown is the beaten boss as the map hears of it, with the time until it's back from this tick.
func (i *Instance) bossDown() model.FieldBossGone {
	return model.FieldBossGone{Tick: i.tick, Mob: i.fallen, Type: i.m.Boss.Mob, BackIn: time.Duration(i.bossBack-i.tick) * i.tuning.TickInterval}
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
			i.ground.Box(b.def, k.X, k.Y, k.Z, ownerOf(m))
		}
	}
	i.countKill(killer, b.def.ID)
	i.fallen, i.bossBack = b.id, i.tick+i.ticksFor(i.data.FieldBoss().Respawn)
	i.events = append(i.events, i.bossDown())
}
