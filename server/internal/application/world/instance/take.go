package instance

import (
	"context"
	"errors"
	"slices"

	"finga/internal/application/world/loot"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/pkg/transient"
)

// takeMsg asks to take items from the player's pouches; done hears each pick once all settle.
type takeMsg struct {
	id    kernel.CharacterID
	conn  string
	picks []model.Pick
	done  chan takeAnswer
}

type takeAnswer struct {
	picks []model.PickAnswer
	err   error
}

func (takeMsg) message() {}

// TakeLoot takes picked items from the player's own pouches within reach, and returns what each pick came to once the item module answered.
func (i *Instance) TakeLoot(ctx context.Context, id kernel.CharacterID, conn string, picks []model.Pick) ([]model.PickAnswer, error) {
	m := takeMsg{id: id, conn: conn, picks: picks, done: make(chan takeAnswer, 1)}
	if err := i.post(m); err != nil {
		return nil, err
	}
	ctx, cancel := answering(ctx, "a take")
	defer cancel()
	select {
	case a := <-m.done:
		return a.picks, a.err
	case <-ctx.Done():
		if why := context.Cause(ctx); errors.Is(why, transient.ErrUnavailable) { // each pick may be asked again; the grant settles whenever it answers
			answers := make([]model.PickAnswer, len(picks))
			for k, pk := range picks {
				answers[k] = model.PickAnswer{Pick: pk, Err: why}
			}
			return answers, nil
		}
		return nil, ctx.Err()
	case <-i.stopped:
		return nil, model.ErrStopped
	}
}

// startTake refuses a dead player, then hands the picks to the ground from the player's middle.
func (i *Instance) startTake(m takeMsg) {
	p, err := i.alive(m.id, m.conn)
	if err != nil {
		m.done <- takeAnswer{err: err}
		return
	}
	at := loot.Point{X: p.state.X, Y: p.state.Y + i.params.Height/2, Z: p.state.Z}
	i.ground.Take(ownerOf(p), at, m.picks, func(a []model.PickAnswer) { m.done <- takeAnswer{picks: a} })
}

// collects reports whether an active quest of the player's collects the item.
func (i *Instance) collects(id kernel.CharacterID, item string) bool {
	p, ok := i.players[id]
	if !ok {
		return false
	}
	for qid, q := range p.Quests {
		if q.Status != model.QuestActive {
			continue
		}
		def, ok := i.data.Quest(qid)
		if ok && slices.ContainsFunc(def.Objectives, func(o gamedata.Objective) bool { return o.Kind == gamedata.Collect && o.Target == item }) {
			return true
		}
	}
	return false
}
