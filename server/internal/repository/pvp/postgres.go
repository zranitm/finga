// Package pvp is the pvp module's Postgres store behind service.Store: ratings, match history and arena builds.
package pvp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	model "finga/internal/model/pvp"
	"finga/internal/repository/pvp/internal/db"
	"finga/internal/shared/kernel"
	"finga/pkg/dbx"
)

// Postgres is the module's store; every error it returns is classified.
type Postgres struct {
	pool *pgxpool.Pool
	obs  dbx.Observer // hears each transaction; nil hears none
}

// NewPostgres builds the store over a pool, telling obs about each transaction.
func NewPostgres(pool *pgxpool.Pool, obs dbx.Observer) *Postgres {
	return &Postgres{pool: pool, obs: obs}
}

// failures has no row or constraint a caller can break: timeouts and outages are transient.ErrUnavailable.
var failures = dbx.Errors{}

// Rating is the character's rating in mode, false when they have none yet.
func (p *Postgres) Rating(ctx context.Context, id kernel.CharacterID, mode string) (model.Rating, bool, error) {
	var out model.Rating
	found := false
	err := dbx.InTx(ctx, p.pool, func(tx pgx.Tx) error {
		r, err := db.New(tx).Rating(ctx, db.RatingParams{CharacterID: uuid.UUID(id), Mode: mode})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		out = model.Rating{Mu: r.Mu, Sigma: r.Sigma, Matches: int(r.Matches), Wins: int(r.Wins), Losses: int(r.Losses), Draws: int(r.Draws)}
		return nil
	}, dbx.Observed(p.obs))
	return out, found, failures.Classify(err)
}

// Record stores match once, in one transaction: the match row, whose id makes a repeat do nothing, the characters' ratings locked and rated, and each fighter's row. It gives back each fighter's rating before and after; a repeat gives the characters' as the first stored them.
func (p *Postgres) Record(ctx context.Context, match model.Match, characters []kernel.CharacterID, rate func(before map[kernel.CharacterID]model.Rating) model.Rated) ([]model.Fighter, error) {
	var fighters []model.Fighter
	err := dbx.InTx(ctx, p.pool, func(tx pgx.Tx) error {
		q, now := db.New(tx), dbx.Now()
		fighters = nil
		n, err := q.RecordMatch(ctx, db.RecordMatchParams{ID: uuid.UUID(match.ID), Mode: match.Mode, StartedAt: match.Started, EndedAt: match.Ended, Winner: int16(match.Winner), Now: now}) //nolint:gosec // 0 to 2
		if err != nil {
			return err
		}
		if n == 0 {
			fighters, err = stored(ctx, q, match)
			return err
		}
		ids := make([]uuid.UUID, 0, len(characters))
		for _, c := range characters {
			ids = append(ids, uuid.UUID(c))
		}
		rows, err := q.LockRatings(ctx, db.LockRatingsParams{Mode: match.Mode, Characters: ids})
		if err != nil {
			return err
		}
		before := make(map[kernel.CharacterID]model.Rating, len(rows))
		for _, r := range rows {
			before[kernel.CharacterID(r.CharacterID)] = model.Rating{Mu: r.Mu, Sigma: r.Sigma, Matches: int(r.Matches), Wins: int(r.Wins), Losses: int(r.Losses), Draws: int(r.Draws)}
		}
		rated := rate(before)
		for c, r := range rated.After {
			if err := q.PutRating(ctx, db.PutRatingParams{
				CharacterID: uuid.UUID(c), Mode: match.Mode, Mu: r.Mu, Sigma: r.Sigma, Shown: int32(r.Shown), Placed: r.Placed, //nolint:gosec // a rating fits
				Matches: int32(r.Matches), Wins: int32(r.Wins), Losses: int32(r.Losses), Draws: int32(r.Draws), Now: now, //nolint:gosec // counts fit
			}); err != nil {
				return err
			}
		}
		for _, f := range rated.Fighters {
			row := db.RecordFighterParams{
				MatchID: uuid.UUID(match.ID), Side: int16(f.Side), Slot: int16(f.Slot), LeftMatch: f.Left, Downs: int32(f.Downs), Untold: f.Untold, //nolint:gosec // small
				MuBefore: f.Before.Mu, SigmaBefore: f.Before.Sigma, MuAfter: f.After.Mu, SigmaAfter: f.After.Sigma, Now: now,
			}
			if f.Character.IsZero() {
				row.BotTier = &f.Tier
			} else {
				id := uuid.UUID(f.Character)
				row.CharacterID = &id
			}
			if err := q.RecordFighter(ctx, row); err != nil {
				return err
			}
		}
		fighters = rated.Fighters
		return nil
	}, dbx.Observed(p.obs))
	return fighters, failures.Classify(err)
}

// Untold is each stored match the character was away from at its end and hasn't been told of.
func (p *Postgres) Untold(ctx context.Context, id kernel.CharacterID) ([]model.Untold, error) {
	var out []model.Untold
	err := dbx.InTx(ctx, p.pool, func(tx pgx.Tx) error {
		c := uuid.UUID(id)
		rows, err := db.New(tx).Untold(ctx, &c)
		if err != nil {
			return err
		}
		out = make([]model.Untold, 0, len(rows))
		for _, r := range rows {
			out = append(out, model.Untold{Match: kernel.MatchID(r.ID), Mode: r.Mode, Ended: r.EndedAt, Winner: int(r.Winner), Side: int(r.Side), Left: r.LeftMatch})
		}
		return nil
	}, dbx.Observed(p.obs))
	return out, failures.Classify(err)
}

// MarkTold marks the character told of matches, as a gateway handed them their outcomes.
func (p *Postgres) MarkTold(ctx context.Context, id kernel.CharacterID, matches []kernel.MatchID) error {
	ids := make([]uuid.UUID, 0, len(matches))
	for _, m := range matches {
		ids = append(ids, uuid.UUID(m))
	}
	err := dbx.InTx(ctx, p.pool, func(tx pgx.Tx) error {
		c := uuid.UUID(id)
		return db.New(tx).MarkTold(ctx, db.MarkToldParams{Now: dbx.Now(), CharacterID: &c, Matches: ids})
	}, dbx.Observed(p.obs))
	return failures.Classify(err)
}

// stored is match's characters as it stored them: their ratings before and after, with their record as it is now.
func stored(ctx context.Context, q *db.Queries, match model.Match) ([]model.Fighter, error) {
	rows, err := q.StoredFighters(ctx, db.StoredFightersParams{MatchID: uuid.UUID(match.ID), Mode: match.Mode})
	if err != nil {
		return nil, err
	}
	out := make([]model.Fighter, 0, len(rows))
	for _, r := range rows {
		out = append(out, model.Fighter{
			Side: int(r.Side), Slot: int(r.Slot), Character: kernel.CharacterID(*r.CharacterID),
			Before: model.Rating{Mu: r.MuBefore, Sigma: r.SigmaBefore},
			After:  model.Rating{Mu: r.MuAfter, Sigma: r.SigmaAfter, Matches: int(r.Matches), Wins: int(r.Wins), Losses: int(r.Losses), Draws: int(r.Draws)},
		})
	}
	return out, nil
}

// Ratings are the character's ratings by mode, those they have.
func (p *Postgres) Ratings(ctx context.Context, id kernel.CharacterID) (map[string]model.Rating, error) {
	out := map[string]model.Rating{}
	err := dbx.InTx(ctx, p.pool, func(tx pgx.Tx) error {
		rows, err := db.New(tx).Ratings(ctx, uuid.UUID(id))
		for _, r := range rows {
			out[r.Mode] = model.Rating{Mu: r.Mu, Sigma: r.Sigma, Matches: int(r.Matches), Wins: int(r.Wins), Losses: int(r.Losses), Draws: int(r.Draws)}
		}
		return err
	}, dbx.Observed(p.obs))
	return out, failures.Classify(err)
}

// Board is the most placed ratings of mode, highest first, a tie in character order.
func (p *Postgres) Board(ctx context.Context, mode string, most int) ([]model.Ranked, error) {
	var out []model.Ranked
	err := dbx.InTx(ctx, p.pool, func(tx pgx.Tx) error {
		rows, err := db.New(tx).Board(ctx, db.BoardParams{Mode: mode, Most: int32(most)}) //nolint:gosec // a board's length
		out = make([]model.Ranked, 0, len(rows))
		for _, r := range rows {
			out = append(out, model.Ranked{Character: kernel.CharacterID(r.CharacterID), Rating: model.Rating{
				Shown: int(r.Shown), Placed: true, Matches: int(r.Matches), Wins: int(r.Wins), Losses: int(r.Losses), Draws: int(r.Draws),
			}})
		}
		return err
	}, dbx.Observed(p.obs))
	return out, failures.Classify(err)
}

// Above counts the placed ratings of mode higher than shown.
func (p *Postgres) Above(ctx context.Context, mode string, shown int) (int, error) {
	var n int64
	err := dbx.InTx(ctx, p.pool, func(tx pgx.Tx) error {
		var err error
		n, err = db.New(tx).Above(ctx, db.AboveParams{Mode: mode, Shown: int32(shown)}) //nolint:gosec // a rating fits
		return err
	}, dbx.Observed(p.obs))
	return int(n), failures.Classify(err)
}

// Build is the character's saved arena build, false when they've saved none.
func (p *Postgres) Build(ctx context.Context, id kernel.CharacterID) (model.Build, bool, error) {
	var out model.Build
	found := false
	err := dbx.InTx(ctx, p.pool, func(tx pgx.Tx) error {
		r, err := db.New(tx).Build(ctx, uuid.UUID(id))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		out = model.Build{Bar: r.SkillBar, Revision: int(r.Revision)}
		return json.Unmarshal(r.Traits, &out.Traits)
	}, dbx.Observed(p.obs))
	return out, found, failures.Classify(err)
}

// PutBuild saves the character's arena build, giving back its revision: one more than the last save's.
func (p *Postgres) PutBuild(ctx context.Context, id kernel.CharacterID, b model.Build) (int, error) {
	traits, _ := json.Marshal(b.Traits) // a map of strings always marshals
	var revision int32
	err := dbx.InTx(ctx, p.pool, func(tx pgx.Tx) error {
		var err error
		revision, err = db.New(tx).PutBuild(ctx, db.PutBuildParams{CharacterID: uuid.UUID(id), SkillBar: b.Bar, Traits: traits, Now: dbx.Now()})
		return err
	}, dbx.Observed(p.obs))
	return int(revision), failures.Classify(err)
}
