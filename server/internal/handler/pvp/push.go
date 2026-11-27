package pvp

import (
	"errors"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/pvp"
	"finga/internal/handler/transport"
	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
)

// Encoders registers the encoders of the pushes the module owns, and its part of the Welcome: the matches that ended while the player was away.
func Encoders(e *transport.Encoders) error {
	e.WelcomeSection(func(w model.Welcome, out *clientv1.Welcome) {
		m, ok := w.Missed.(pvp.Missed)
		if !ok {
			return
		}
		out.MissedMatches, out.MissedMore = missedMatches(m.Matches), int32(len(m.Older)) //nolint:gosec // a count of matches
	})
	return errors.Join(
		transport.Encode(e, func(p pvp.Ended) any { return matchEnded(p) }),
		transport.Encode(e, func(p pvp.Offered) any { return offered(p) }),
		transport.Encode(e, func(p pvp.OfferGone) any { return offerGone(p) }),
		transport.Encode(e, func(p pvp.PiecesAdded) any { return piecesAdded(p) }),
		transport.Encode(e, func(p pvp.Queued) any { return queued(p) }),
		transport.Encode(e, func(p pvp.Unqueued) any { return unqueued(p) }),
	)
}

func offered(p pvp.Offered) *clientv1.PvpOffer {
	return &clientv1.PvpOffer{Offer: p.Offer, Mode: p.Mode, Bots: int32(p.Bots), UntilMs: uint32(p.Until.Milliseconds())} //nolint:gosec // a few bots, seconds
}

func offerGone(p pvp.OfferGone) *clientv1.PvpOfferGone {
	return &clientv1.PvpOfferGone{Offer: p.Offer, Reason: goneReasons[p.Reason], Requeued: p.Requeued}
}

func queued(p pvp.Queued) *clientv1.PvpQueued {
	return &clientv1.PvpQueued{Mode: p.Mode, SinceMs: uint32(p.Since.Milliseconds()), Members: ids(p.Members), WaitingRoom: p.WaitingRoom} //nolint:gosec // a wait of minutes
}

func unqueued(p pvp.Unqueued) *clientv1.PvpUnqueued {
	return &clientv1.PvpUnqueued{Reason: unqueueReasons[p.Reason], By: ids(p.By)}
}

// piecesAdded tells of the pieces a match's end gave the account, each with what gave it: a rank, or a win feat.
func piecesAdded(p pvp.PiecesAdded) *clientv1.PiecesAdded {
	out := make([]*clientv1.AddedPiece, 0, len(p.Pieces)+len(p.Feats))
	for _, r := range p.Pieces {
		out = append(out, &clientv1.AddedPiece{Id: r.Piece, Cause: pieceFromRank})
	}
	for _, f := range p.Feats {
		out = append(out, &clientv1.AddedPiece{Id: f.Piece, Cause: pieceFromFeat})
	}
	return &clientv1.PiecesAdded{Pieces: out}
}

// The names of what gave a piece, as the item module has them; a test holds them to its.
const (
	pieceFromRank = "rank"
	pieceFromFeat = "feat"
)

// ids are character IDs as the wire names them.
func ids(cs []kernel.CharacterID) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.String())
	}
	return out
}

var unqueueReasons = map[pvp.UnqueueReason]clientv1.PvpUnqueueReason{
	pvp.UnqueueLeft:         clientv1.PvpUnqueueReason_PVP_UNQUEUE_REASON_LEFT,
	pvp.UnqueueDeclined:     clientv1.PvpUnqueueReason_PVP_UNQUEUE_REASON_DECLINED,
	pvp.UnqueueLapsed:       clientv1.PvpUnqueueReason_PVP_UNQUEUE_REASON_LAPSED,
	pvp.UnqueuePartyChanged: clientv1.PvpUnqueueReason_PVP_UNQUEUE_REASON_PARTY_CHANGED,
}

var goneReasons = map[pvp.GoneReason]clientv1.PvpOfferGoneReason{
	pvp.GoneDeclined: clientv1.PvpOfferGoneReason_PVP_OFFER_GONE_REASON_DECLINED,
	pvp.GoneLapsed:   clientv1.PvpOfferGoneReason_PVP_OFFER_GONE_REASON_LAPSED,
}

var matchOutcomes = map[pvp.Outcome]clientv1.MatchOutcome{
	pvp.Win:  clientv1.MatchOutcome_MATCH_OUTCOME_WIN,
	pvp.Loss: clientv1.MatchOutcome_MATCH_OUTCOME_LOSS,
	pvp.Draw: clientv1.MatchOutcome_MATCH_OUTCOME_DRAW,
}

// missedMatches is a Welcome's matches that ended while the player was away.
func missedMatches(ms []pvp.MissedMatch) []*clientv1.MissedMatch {
	out := make([]*clientv1.MissedMatch, 0, len(ms))
	for _, m := range ms {
		out = append(out, &clientv1.MissedMatch{Mode: m.Mode, Result: matchOutcomes[m.Outcome], Left: m.Left, EndedAt: m.Ended.UnixMilli()})
	}
	return out
}

func matchEnded(e pvp.Ended) *clientv1.MatchEnded {
	out := &clientv1.MatchEnded{Mode: e.Mode, Result: matchOutcomes[e.Outcome], Left: e.Left}
	if e.Battle {
		out.Score = []int32{int32(e.Score[0]), int32(e.Score[1])} //nolint:gosec // up to the goal
	} else {
		out.Wins = []int32{int32(e.Wins[0]), int32(e.Wins[1])} //nolint:gosec // a few rounds
	}
	if r := e.Rating; r != nil {
		out.Rating = &clientv1.PvpRatingChange{Before: int32(r.Before), After: int32(r.After), RankBefore: r.RankBefore, RankAfter: r.RankAfter, PlacementLeft: int32(r.PlacementLeft)} //nolint:gosec // a rating and a few placement matches
	}
	return out
}
