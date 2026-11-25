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

// FeatPieces is the pieces of each feat met gives, in file order.
func FeatPieces(data *gamedata.Data, met func(gamedata.FeatWhen) bool) []model.FeatPiece {
	var pieces []model.FeatPiece
	for _, f := range data.Feats() {
		if met(f.When) {
			for _, piece := range data.FeatPieces(f.ID) {
				pieces = append(pieces, model.FeatPiece{Piece: piece, Feat: f.ID})
			}
		}
	}
	return pieces
}

// grantFeats has a worker ask the item module for the pieces of each feat met gives (outfits.md section 4); the player hears of those new to the account. A feat asks each time it's met, so a grant lost to a failure, or to a pool too busy to take it, comes the next time; a bot does no feat.
func (i *Instance) grantFeats(p *player, met func(gamedata.FeatWhen) bool) {
	if p.bot != nil {
		return
	}
	pieces := FeatPieces(i.data, met)
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
		i.log.Warn("a feat's grant dropped, every item-call worker busy; the next time it's met grants it", "character_id", id, "pieces", len(pieces))
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
