package instance

import (
	"context"
	"fmt"
	"math"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/modules/world/internal/application/promotions"
	"finga/modules/world/internal/application/quests"
	"finga/modules/world/internal/model"
	"finga/pkg/transient"
)

// talkMsg asks for an NPC's dialog; done hears whether it was sent.
type talkMsg struct {
	id   kernel.CharacterID
	conn string
	npc  string
	done chan error
}

func (talkMsg) message() {}

// Talk sends the player the dialog of an NPC within reach on this map, and returns once it is queued.
func (i *Instance) Talk(ctx context.Context, id kernel.CharacterID, conn, npc string) error {
	m := talkMsg{id: id, conn: conn, npc: npc, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

func (i *Instance) talk(m talkMsg) {
	p, err := i.alive(m.id, m.conn)
	if err != nil {
		m.done <- err
		return
	}
	npc, ok := i.reachable(p, m.npc)
	if !ok {
		m.done <- model.ErrNpcOutOfReach
		return
	}
	i.progress(p, gamedata.TalkTo, npc.ID)
	d := quests.Dialog(i.data, npc, i.questsOf(p))
	if offer, ok := promotions.Offer(i.data, npc, i.promotionOf(p)); ok {
		d.Promotion = &offer
	}
	d.Tick = i.tick
	p.private = append(p.private, d)
	m.done <- nil
}

// alive is a living player on this map through conn.
func (i *Instance) alive(id kernel.CharacterID, conn string) (*player, error) {
	p, ok := i.players[id]
	switch {
	case !ok || p.Connection != conn:
		return nil, fmt.Errorf("%w: the character is not on this map through that connection", transient.ErrUnavailable)
	case p.dead:
		return nil, model.ErrCharacterDead
	}
	return p, nil
}

// reachable is the NPC named id, if it stands on this map within talk reach of p's centre.
func (i *Instance) reachable(p *player, id string) (gamedata.NPC, bool) {
	npc, ok := i.data.NPC(id)
	if !ok || npc.MapID != i.m.ID {
		return gamedata.NPC{}, false
	}
	return npc, math.Hypot(npc.At.X-p.state.X, npc.At.Y-(p.state.Y-i.params.Height/2)) <= i.data.TalkReach()
}

// questsOf is what the quest rules read of p.
func (i *Instance) questsOf(p *player) quests.Character {
	return quests.Character{Level: p.Level, Class: p.Class, Log: p.Quests}
}
