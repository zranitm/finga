package instance

import (
	"context"
	"math"
	"slices"
	"time"

	"finga/internal/application/world/mobs"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
)

// bossSlot is one of a region's field bosses, each hunting area's (large-regions.md section 6), up or beaten.
type bossSlot struct {
	spot   gamedata.BossSpot
	area   int        // its hunting area's index in the region's Areas
	up     *fieldBoss // nil while beaten
	fallen uint64     // the beaten boss's mob
	back   uint64     // the tick a beaten boss is back
	due    bool       // a player came into its area on foot this tick, who gets its bar in the frame whether it changed or not
}

// fieldBoss is a boss while it is up: its mob and the HP each character took from it.
type fieldBoss struct {
	id    uint64
	def   gamedata.Mob
	dealt map[kernel.CharacterID]int
}

// bossSlots are m's bosses: each hunting area's in a region.
func bossSlots(m *gamedata.Map) []*bossSlot {
	var out []*bossSlot
	for k, a := range m.Areas {
		if a.Boss != nil {
			out = append(out, &bossSlot{spot: *a.Boss, area: k})
		}
	}
	return out
}

// raise stands s's boss at its spot.
func (i *Instance) raise(s *bossSlot) {
	def, _ := i.data.Mob(s.spot.Mob)
	s.up = &fieldBoss{id: i.arena.SpawnBoss(def, s.spot), def: def, dealt: map[kernel.CharacterID]int{}}
}

// stepBosses raises each beaten boss again once its respawn is up, which those who hear it are told; a boss never leaves on its own.
func (i *Instance) stepBosses() {
	for _, s := range i.bosses {
		if s.up == nil && i.tick >= s.back {
			i.raise(s)
			i.announce(s, i.bossUp(s))
		}
	}
}

// announce sends s's news to those who hear it: those in its area.
func (i *Instance) announce(s *bossSlot, e model.Event) {
	for _, id := range i.order {
		if p := i.players[id]; p.area == s.area {
			p.private = append(p.private, e)
		}
	}
}

// bossOf is the index in bosses of the boss p hears of: their area's; -1 for none.
func (i *Instance) bossOf(p *player) int {
	return slices.IndexFunc(i.bosses, func(s *bossSlot) bool { return s.area == p.area })
}

// bossState is the boss p hears of as they're told on coming to it: up, or beaten with when it's back; neither where there's none.
func (i *Instance) bossState(p *player) (*model.FieldBoss, *model.FieldBossGone) {
	k := i.bossOf(p)
	if k < 0 {
		return nil, nil
	}
	switch s := i.bosses[k]; {
	case s.up != nil:
		up := i.bossUp(s)
		return &up, nil
	default:
		down := i.bossDown(s)
		return nil, &down
	}
}

// bossUp is s's boss as those who hear it are told.
func (i *Instance) bossUp(s *bossSlot) model.FieldBoss {
	return model.FieldBoss{Tick: i.tick, Mob: s.up.id, Type: s.up.def.ID}
}

// bossDown is s's beaten boss as those who hear it are told, with the time until it's back from this tick.
func (i *Instance) bossDown(s *bossSlot) model.FieldBossGone {
	return model.FieldBossGone{Tick: i.tick, Mob: s.fallen, Type: s.spot.Mob, BackIn: time.Duration(s.back-i.tick) * i.tuning.TickInterval}
}

// Bosses is each hunting area's field boss on a region's channel, in its areas' order, as the tick last left it: none on a map, whose one boss its players hear of as it comes and goes.
func (i *Instance) Bosses(ctx context.Context) ([]model.AreaBoss, error) {
	m := bossesMsg{done: make(chan []model.AreaBoss, 1)}
	if err := i.post(m); err != nil {
		return nil, err
	}
	select {
	case b := <-m.done:
		return b, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-i.stopped:
		return nil, model.ErrStopped
	}
}

// areaBosses is Bosses on the tick.
func (i *Instance) areaBosses() []model.AreaBoss {
	var out []model.AreaBoss
	for _, s := range i.bosses {
		b := model.AreaBoss{Area: i.areaID(s.area), Up: s.up != nil}
		if s.up == nil {
			b.BackIn = i.bossDown(s).BackIn
		}
		out = append(out, b)
	}
	return out
}

// bossUpAs is the slot whose boss is up as mob, nil for none.
func (i *Instance) bossUpAs(mob uint64) *bossSlot {
	for _, s := range i.bosses {
		if s.up != nil && s.up.id == mob {
			return s
		}
	}
	return nil
}

// bossGroup is who a beaten boss pays together, a party's members there when it fell or a player alone, and whose tallies make their damage: the whole party's, wherever its members are.
type bossGroup struct {
	present []*player
	tallied []kernel.CharacterID
}

// bossGroups are those alive there when s's boss fell, grouped by party in join order: those in its area or within its leash of its spot, where its fight may have gone over the area's line.
func (i *Instance) bossGroups(s *bossSlot) []bossGroup {
	reach := s.spot.Radius + i.data.MobRules().Leash
	var groups []bossGroup
	byParty := map[string]int{}
	for _, id := range i.order {
		p := i.players[id]
		if p.dead || p.area != s.area && math.Hypot(p.state.X-s.spot.At.X, p.state.Z-s.spot.At.Z) > reach {
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

// bossFell pays out s's beaten boss: each group's EXP by its share of the boss's HP, and its own drop roll and the boss's feats for each player whose group dealt the drop share. Damage of those not there to collect goes to no one, and the killer's kill counts for quests as any does.
func (i *Instance) bossFell(s *bossSlot, killer *player, k *mobs.Kill) {
	b := s.up
	s.up, s.fallen, s.back = nil, b.id, i.tick+i.ticksFor(i.data.FieldBoss().Respawn)
	i.announce(s, i.bossDown(s)) // its fall, then what it pays
	for _, g := range i.bossGroups(s) {
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
			i.grantFeats(m, func(w gamedata.FeatWhen) bool { return w.Boss == gamedata.AnyBoss || w.Boss == b.def.ID })
		}
	}
	i.countKill(killer, b.def.ID)
}
