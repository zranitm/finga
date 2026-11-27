package pvp

import (
	"context"
	"slices"
	"time"

	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

const missedShown = 5 // the most missed matches a Welcome tells of; it counts the rest

// Missed is what a character missed while away: the newest matches that ended without them, newest first, and the older ones a Welcome only counts.
type Missed struct {
	Matches []MissedMatch
	Older   []kernel.MatchID
}

// MissedMatch is a match that ended while the character was away: its mode, how it went for them, whether they'd left it, and when it ended.
type MissedMatch struct {
	Match   kernel.MatchID
	Mode    string
	Outcome Outcome
	Left    bool
	Ended   time.Time
}

// Missed is the matches the character was away from at their end and hasn't been told of, for their Welcome: the newest missedShown, and the rest to count. They stay untold till Told.
func (s *cases) Missed(ctx context.Context, id kernel.CharacterID) (Missed, error) {
	untold, err := s.d.Store.Untold(ctx, id)
	if err != nil {
		return Missed{}, err
	}
	slices.SortFunc(untold, func(a, b model.Untold) int { return b.Ended.Compare(a.Ended) })
	var out Missed
	for k, u := range untold {
		if k >= missedShown {
			out.Older = append(out.Older, u.Match)
			continue
		}
		out.Matches = append(out.Matches, MissedMatch{Match: u.Match, Mode: u.Mode, Outcome: outcomeFor(model.Outcome{Left: u.Left}, u.Winner, u.Side), Left: u.Left, Ended: u.Ended})
	}
	return out, nil
}

// Told marks the character told of matches' outcomes, as a gateway handed them on, live or in a Welcome.
func (s *cases) Told(ctx context.Context, id kernel.CharacterID, matches []kernel.MatchID) error {
	return s.d.Store.MarkTold(ctx, id, matches)
}
