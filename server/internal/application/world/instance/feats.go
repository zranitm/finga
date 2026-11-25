package instance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v5"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/pkg/transient"
)

// featsMsg brings a feat grant's answer to the tick: the pieces new to the account.
type featsMsg struct {
	id    kernel.CharacterID
	added []model.FeatPiece
}

func (featsMsg) message() {}

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
		i.log.Warn("a feat's grant dropped, every item-call worker busy; the next time it's met grants it", "character_id", id, "pieces", len(pieces))
	}
}

// callFeats tries the grant until the item module answers or refuses, or callFor passes, as a hand-in does; the log hears a grant given up.
func (i *Instance) callFeats(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, pieces []model.FeatPiece) ([]model.FeatPiece, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, callFor, fmt.Errorf("%w: the item module didn't answer a feat's grant in time", transient.ErrUnavailable))
	defer cancel()
	tries := 0
	wait := &backoff.ExponentialBackOff{InitialInterval: handInRetryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: handInRetryMax}
	added, err := backoff.Retry(ctx, func() ([]model.FeatPiece, error) {
		tries++
		tryCtx, cancelTry := context.WithTimeout(ctx, handInTimeout)
		defer cancelTry()
		added, err := i.deps.Feats.GrantFeatPieces(tryCtx, account, pieces)
		if err != nil && !errors.Is(err, transient.ErrUnavailable) {
			return added, backoff.Permanent(err)
		}
		return added, err
	}, backoff.WithBackOff(wait), backoff.WithMaxElapsedTime(0), backoff.WithNotify(func(err error, next time.Duration) {
		i.log.WarnContext(ctx, "a feat's grant failed; trying again", "character_id", id, "try", tries, "next_in", next, "err", err)
	}))
	if err != nil {
		i.log.ErrorContext(ctx, "a feat's grant given up; the next time it's met grants it", "character_id", id, "tries", tries, "err", err)
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
