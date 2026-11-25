package world

import (
	"context"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
)

// resultsQueue bounds the outcomes waiting for the pvp module: far more than end at once.
const resultsQueue = 1024

// MatchSpec is a match to open: its id and mode, and its two sides.
type MatchSpec struct {
	ID      kernel.MatchID
	Mode    string
	Sides   [2][]Fighter
	StandIn string // the tier of a bot that takes a team mode's leaver's place
}

// Fighter is a place on a side: a character, or a bot when Bot is set.
type Fighter struct {
	Character kernel.CharacterID
	Bot       *Bot
}

// Bot is a made-up fighter: its id, name, class, tier and pvp build.
type Bot struct {
	ID    kernel.CharacterID
	Name  string
	Class kernel.Class
	Tier  string
	Bar   []string
}

// Build is a player's arena build: eight keys, "" for an empty one, the trait chosen for skills on it, and its revision, which counts its saves.
type Build struct {
	Bar      []string
	Traits   map[string]string
	Revision int
}

// MatchResult is a match's outcome: Winner is 1 or 2, 0 for a draw.
type MatchResult struct {
	Match          kernel.MatchID
	Mode           string
	Started, Ended time.Time
	Sides          [2][]FighterResult
	Winner         int
	Wins           [2]int
	Score          [2]int // a battle's
}

// FighterResult is how a fighter did: a character, or a bot of Tier.
type FighterResult struct {
	Character kernel.CharacterID // zero for a bot
	Tier      string
	Left      bool
	Downs     int
	Away      bool // not in the match at its end, so told of it at their next Welcome
}

// results holds the outcomes the matches report until the pvp module takes them; one past the queue is dropped, as a crash drops a match's.
type results struct {
	ch   chan model.MatchResult
	drop func(model.MatchResult)
}

func newResults(drop func(model.MatchResult)) *results {
	return &results{ch: make(chan model.MatchResult, resultsQueue), drop: drop}
}

// Ended queues r without blocking.
func (r *results) Ended(res model.MatchResult) {
	select {
	case r.ch <- res:
	default:
		r.drop(res)
	}
}

func specOf(s MatchSpec) model.MatchSpec {
	out := model.MatchSpec{ID: s.ID, Mode: s.Mode, StandIn: s.StandIn}
	for k, side := range s.Sides {
		for _, f := range side {
			mf := model.Fighter{Character: f.Character}
			if b := f.Bot; b != nil {
				mf.Bot = &model.Bot{ID: b.ID, Name: b.Name, Class: b.Class, Tier: b.Tier, Build: model.Build{Bar: b.Bar}}
			}
			out.Sides[k] = append(out.Sides[k], mf)
		}
	}
	return out
}

func resultOf(r model.MatchResult) MatchResult {
	out := MatchResult{Match: r.Match, Mode: r.Mode, Started: r.Started, Ended: r.Ended, Winner: r.Winner, Wins: r.Wins, Score: r.Score}
	for k, side := range r.Sides {
		for _, f := range side {
			out.Sides[k] = append(out.Sides[k], FighterResult(f))
		}
	}
	return out
}

// OpenMatch opens a match and hands its characters to it from where they are; one who can't go counts as gone once its prep ends.
func (m *Module) OpenMatch(ctx context.Context, spec MatchSpec) (err error) {
	ctx, call := m.calls.Start(ctx, "OpenMatch")
	defer call.End(&err)
	return m.svc.OpenMatch(ctx, specOf(spec))
}

// Bots is how many bots the matches running have, as their specs placed them; a leaver's stand-in isn't counted.
func (m *Module) Bots() int { return m.svc.Bots() }

// CanFight is nil when the character could go into a match now: on a map, alive, promoted and in no match.
func (m *Module) CanFight(ctx context.Context, id kernel.CharacterID) (err error) {
	ctx, call := m.calls.Start(ctx, "CanFight")
	defer call.End(&err)
	return m.svc.CanFight(ctx, id)
}

// SetBuild hands the character's match their arena build, which it fights with from its next round's countdown; nothing when they're in no match, and an older revision than one it has changes nothing.
func (m *Module) SetBuild(ctx context.Context, id kernel.CharacterID, b Build) (err error) {
	ctx, call := m.calls.Start(ctx, "SetBuild")
	defer call.End(&err)
	return m.svc.SetBuild(ctx, id, model.Build(b))
}

// Forfeit takes the character out of their match, back where they came from.
func (m *Module) Forfeit(ctx context.Context, p Presence) (err error) {
	ctx, call := m.calls.Start(ctx, "Forfeit")
	defer call.End(&err)
	return m.svc.Forfeit(ctx, p.Character, p.Conn)
}

// FollowResults hands deliver each match's outcome, in order, until ctx ends; one module follows them.
func (m *Module) FollowResults(ctx context.Context, deliver func(MatchResult)) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case r := <-m.results.ch:
			deliver(resultOf(r))
		}
	}
}
