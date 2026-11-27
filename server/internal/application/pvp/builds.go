package pvp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	model "finga/internal/model/pvp"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/pkg/panics"
)

// The board's length, how long one read of it serves and how long a read may take, and how long each player's build is tried as their match opens.
const (
	boardLength = 100
	boardFor    = 30 * time.Second
	boardRead   = 5 * time.Second
	buildsFor   = 5 * time.Second
)

// Standing is a character in a mode: in a rated one their shown rating and its rank, the placement matches they have left, their record, and their place on the board once placed, 0 before; in an unrated one their record alone.
type Standing struct {
	Mode                string
	Shown               int
	Rank                string
	PlacementLeft       int
	Wins, Losses, Draws int
	Place               int
}

// Profile is a character's standing in each mode, in pvp.yaml's order, and their arena build.
type Profile struct {
	Standings []Standing
	Build     model.Build
}

// Row is a character on a mode's board: their place, shared by a tie, and their shown rating, rank and record.
type Row struct {
	Place               int
	Character           kernel.CharacterID
	Name                string
	Class               kernel.Class
	Shown               int
	Rank                string
	Wins, Losses, Draws int
}

// boards are the boards read lately, by rated mode; one read at a time per mode, which the others asking wait for.
type boards struct {
	mu      sync.Mutex
	read    map[string]board
	reading singleflight.Group
}

type board struct {
	at   time.Time
	rows []Row
}

// SetBuild saves the arena build of one of the account's characters, checked against their class, and hands it to their match if they're in one, for its next round; a match that can't take it now gets it at their next.
func (s *cases) SetBuild(ctx context.Context, account kernel.AccountID, id kernel.CharacterID, bar []string, traits map[string]string) error {
	ch, err := s.owned(ctx, account, id)
	if err != nil {
		return err
	}
	class, _ := s.d.Data.Class(ch.Class)
	if class.Parent == "" {
		return model.ErrNotPromoted // a build fights in a match, where only a promoted class goes; an unknown class is none
	}
	if err := gamedata.PvpBuildFault(class, s.d.Data.Skill, bar, traits); err != nil {
		return fmt.Errorf("%w: %w", model.ErrBadBuild, err)
	}
	b := model.Build{Bar: bar, Traits: traits}
	if b.Revision, err = s.d.Store.PutBuild(ctx, id, b); err != nil {
		return err
	}
	if err := s.d.World.SetBuild(ctx, id, b); err != nil {
		s.d.Logger.WarnContext(ctx, "a saved arena build didn't reach its match", "character_id", id, "err", err)
	}
	return nil
}

// Profile is one of the account's characters' standing in each rated mode, and their arena build: the one they saved, or their world bar's copy.
func (s *cases) Profile(ctx context.Context, account kernel.AccountID, id kernel.CharacterID) (Profile, error) {
	ch, err := s.owned(ctx, account, id)
	if err != nil {
		return Profile{}, err
	}
	ratings, err := s.d.Store.Ratings(ctx, id)
	if err != nil {
		return Profile{}, err
	}
	var out Profile
	for _, mode := range s.rules.Modes {
		rt, ok := ratings[mode.ID]
		if !mode.Rated {
			out.Standings = append(out.Standings, Standing{Mode: mode.ID, Wins: rt.Wins, Losses: rt.Losses, Draws: rt.Draws})
			continue
		}
		if !ok {
			rt = s.fresh()
		}
		shown := s.shown(rt)
		st := Standing{Mode: mode.ID, Shown: shown, Rank: s.rankOf(shown), PlacementLeft: max(0, s.rules.Rating.Placement-rt.Matches), Wins: rt.Wins, Losses: rt.Losses, Draws: rt.Draws}
		if st.PlacementLeft == 0 {
			above, err := s.d.Store.Above(ctx, mode.ID, shown)
			if err != nil {
				return Profile{}, err
			}
			st.Place = above + 1
		}
		out.Standings = append(out.Standings, st)
	}
	b, saved, err := s.d.Store.Build(ctx, id)
	if err != nil {
		return Profile{}, err
	}
	if !saved {
		b = s.copied(ch)
	}
	out.Build = b
	return out, nil
}

// owned is one of the account's characters; another's is ErrNoCharacter, as none is.
func (s *cases) owned(ctx context.Context, account kernel.AccountID, id kernel.CharacterID) (model.Character, error) {
	ch, err := s.d.Characters.Character(ctx, id)
	if err == nil && ch.Account != account {
		return model.Character{}, model.ErrNoCharacter
	}
	return ch, err
}

// copied is the build of a character who saved none: their world bar's copy, as their class can use it; an unknown class has no skill, so its copy is empty.
func (s *cases) copied(ch model.Character) model.Build {
	class, _ := s.d.Data.Class(ch.Class)
	bar, traits := gamedata.PvpCopy(class, s.d.Data.Skill, ch.Bar, ch.Traits)
	return model.Build{Bar: bar, Traits: traits}
}

// Leaderboard is a rated mode's top placed characters, highest first, a tie sharing its place; one read serves everyone for boardFor.
func (s *cases) Leaderboard(ctx context.Context, modeID string) ([]Row, error) {
	mode, ok := s.d.Data.PvpMode(modeID)
	if !ok || !mode.Rated {
		return nil, model.ErrUnknownMode // an unrated mode has no board
	}
	s.boards.mu.Lock()
	b, ok := s.boards.read[mode.ID]
	s.boards.mu.Unlock()
	if ok && time.Since(b.at) < boardFor {
		return b.rows, nil
	}
	rows, err, _ := s.boards.reading.Do(mode.ID, func() (any, error) {
		ctx, cancel := s.detached(ctx, boardRead) // its callers share it: none's leaving ends it
		defer cancel()
		rows, err := s.readBoard(ctx, mode)
		if err == nil {
			s.boards.mu.Lock()
			s.boards.read[mode.ID] = board{at: time.Now(), rows: rows}
			s.boards.mu.Unlock()
		}
		return rows, err
	})
	if err != nil {
		return nil, err
	}
	return rows.([]Row), nil
}

// readBoard reads mode's board from the store, with the characters' names.
func (s *cases) readBoard(ctx context.Context, mode gamedata.PvpMode) ([]Row, error) {
	ranked, err := s.d.Store.Board(ctx, mode.ID, boardLength)
	if err != nil {
		return nil, err
	}
	ids := make([]kernel.CharacterID, 0, len(ranked))
	for _, r := range ranked {
		ids = append(ids, r.Character)
	}
	names, err := s.d.Characters.Names(ctx, ids)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(ranked))
	for k, r := range ranked {
		place := k + 1
		if k > 0 && r.Rating.Shown == rows[k-1].Shown {
			place = rows[k-1].Place
		}
		n := names[r.Character]
		rows = append(rows, Row{Place: place, Character: r.Character, Name: n.Name, Class: n.Class, Shown: r.Rating.Shown, Rank: s.rankOf(r.Rating.Shown), Wins: r.Rating.Wins, Losses: r.Rating.Losses, Draws: r.Rating.Draws})
	}
	return rows, nil
}

// detached is ctx's values, ended after d or as the module stops, but not by ctx.
func (s *cases) detached(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	s.mu.Lock()
	lifeCtx := s.lifeCtx
	s.mu.Unlock()
	detachedCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), d)
	stop := context.AfterFunc(lifeCtx, cancel)
	return detachedCtx, func() { stop(); cancel() }
}

// sendBuilds hands an opened match each player's build, the saved one or their world bar's copy, read once the match knows its players, so a save from then on reaches it too. Each player's is read at once and on its own, apart from ctx, the one who accepted last, and one that can't be within buildsFor, or as the module stops, leaves its player fighting with no bar but their client's, on their world traits.
func (s *cases) sendBuilds(ctx context.Context, spec model.Spec) {
	for _, side := range spec.Sides {
		for _, p := range side {
			if p.Bot != nil {
				continue
			}
			ctx, cancel := s.detached(ctx, buildsFor)
			s.d.Panics.Go(ctx, panics.FlowJob, func() {
				defer cancel()
				b, err := s.buildOf(ctx, p.Character)
				if err == nil {
					err = s.d.World.SetBuild(ctx, p.Character, b)
				}
				if err != nil {
					s.d.Logger.WarnContext(ctx, "an arena build didn't reach its match; its player fights with no bar but their client's", "character_id", p.Character, "match_id", spec.ID, "err", err)
				}
			}, "job", "send_build", "character_id", p.Character, "match_id", spec.ID)
		}
	}
}

// buildOf is the character's build: the one they saved, or their world bar's copy.
func (s *cases) buildOf(ctx context.Context, id kernel.CharacterID) (model.Build, error) {
	b, saved, err := s.d.Store.Build(ctx, id)
	if err != nil || saved {
		return b, err
	}
	ch, err := s.d.Characters.Character(ctx, id)
	if err != nil {
		return model.Build{}, err
	}
	return s.copied(ch), nil
}
