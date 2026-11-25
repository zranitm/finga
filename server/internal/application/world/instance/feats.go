package instance

import (
	"context"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
)

// featsMsg brings a feat grant's answer to the tick: the pieces new to the account.
type featsMsg struct {
	id    kernel.CharacterID
	added []model.FeatPiece
}

func (featsMsg) message() {}

// metFeatsMsg asks for the feats a joined character's save shows met.
type metFeatsMsg struct{ id kernel.CharacterID }

func (metFeatsMsg) message() {}

// AskMetFeats has the map ask, without blocking, for the pieces of each feat the character's save shows met: a level reached, a promotion taken, a quest finished (outfits.md section 4). The world asks once a login or a reconnect has joined, so a grant lost when a one-shot feat was met still comes; ErrBusy when the inbox is full. A boss feat has no record to see, and comes again with the next boss.
func (i *Instance) AskMetFeats(id kernel.CharacterID) error {
	return i.post(metFeatsMsg{id: id})
}

// askMetFeats handles AskMetFeats.
func (i *Instance) askMetFeats(m metFeatsMsg) {
	p, ok := i.players[m.id]
	if !ok {
		return // gone already: the next join asks
	}
	class, _ := i.data.Class(p.Class)
	i.grantFeats(p, func(w gamedata.FeatWhen) bool {
		return w.Level > 0 && w.Level <= p.Level || w.Promoted && class.Parent != "" || w.Quest != "" && p.Quests[w.Quest].TimesFinished > 0
	})
}

// grantFeats has a worker ask the item module for the pieces of each feat met gives (outfits.md section 4); the player hears of those new to the account. A feat asks each time it's met, so a grant lost to a failure, or to a pool too busy to take it, comes the next time; a bot does no feat.
func (i *Instance) grantFeats(p *player, met func(gamedata.FeatWhen) bool) {
	if p.bot != nil {
		return
	}
	var pieces []model.FeatPiece
	for _, f := range i.data.Feats() {
		if met(f.When) {
			for _, piece := range i.data.FeatPieces(f.ID) {
				pieces = append(pieces, model.FeatPiece{Piece: piece, Feat: f.ID})
			}
		}
	}
	if len(pieces) == 0 {
		return
	}
	account, id := p.Account, p.ID
	submitted := i.deps.ItemCalls.Submit(func(ctx context.Context) {
		added, err := i.callFeats(ctx, account, id, pieces)
		if err != nil || len(added) == 0 {
			return
		}
		select {
		case i.inbox <- featsMsg{id: id, added: added}:
		case <-ctx.Done(): // the map stopped first; its inbox may never drain
		}
	})
	if !submitted {
		i.log.Warn("a feat's grant dropped, every item-call worker busy; the next time it's met, or the next join, grants it", "character_id", id, "pieces", len(pieces))
	}
}

// callFeats tries the grant as retried does; the log hears a grant given up.
func (i *Instance) callFeats(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, pieces []model.FeatPiece) ([]model.FeatPiece, error) {
	who := []any{"character_id", id}
	added, tries, err := retried(ctx, i.log, "feat's grant", who, func(ctx context.Context) ([]model.FeatPiece, error) {
		return i.deps.Feats.GrantFeatPieces(ctx, account, pieces)
	})
	if err != nil {
		i.log.ErrorContext(ctx, "a feat's grant given up; the next time it's met, or the next join, grants it", append(who, "tries", tries, "err", err)...)
	}
	return added, err
}

// settleFeats tells the character on this map, whatever its connection, of the pieces its feats added; one gone from the map finds them in the wardrobe.
func (i *Instance) settleFeats(m featsMsg) {
	p, ok := i.players[m.id]
	if !ok {
		return
	}
	pieces := make([]string, 0, len(m.added))
	for _, a := range m.added {
		pieces = append(pieces, a.Piece)
	}
	p.private = append(p.private, model.PiecesAdded{Tick: i.tick, Pieces: pieces, Cause: model.PieceFromFeat})
}
