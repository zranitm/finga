package service

import (
	"context"
	"slices"
	"time"

	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

const missedShown = 5 // the most missed matches a Welcome tells of; it counts the rest

// Missed is what a character missed while away: the newest matches that ended without them, newest first, and how many older ones there were besides.
type Missed struct {
	Matches []MissedMatch
	More    int
}

// MissedMatch is a match that ended while the character was away: its mode, how it went for them, whether they'd left it, and when it ended.
type MissedMatch struct {
	Mode    string
	Outcome Outcome
	Left    bool
	Ended   time.Time
}

// Missed takes the matches the character was away from at their end and hasn't been told of, for their Welcome: each told once, the newest missedShown and a count of the rest.
func (s *Service) Missed(ctx context.Context, id kernel.CharacterID) (Missed, error) {
	untold, err := s.d.Store.TakeUntold(ctx, id)
	if err != nil {
		return Missed{}, err
	}
	slices.SortFunc(untold, func(a, b model.Untold) int { return b.Ended.Compare(a.Ended) })
	var out Missed
	for k, u := range untold {
		if k == missedShown {
			out.More = len(untold) - k
			break
		}
		out.Matches = append(out.Matches, MissedMatch{Mode: u.Mode, Outcome: outcomeFor(model.Outcome{Left: u.Left}, u.Winner, u.Side), Left: u.Left, Ended: u.Ended})
	}
	return out, nil
}

// Told marks the character told of match's outcome, as a gateway handed it to them live.
func (s *Service) Told(ctx context.Context, id kernel.CharacterID, match kernel.MatchID) error {
	return s.d.Store.MarkTold(ctx, id, match)
}
