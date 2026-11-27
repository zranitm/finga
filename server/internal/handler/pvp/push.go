package pvp

import (
	"errors"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/handler/transport"
	model "finga/internal/model/gateway"
)

// Encoders registers the encoders of the pushes the module owns.
func Encoders(e *transport.Encoders) error {
	e.WelcomeSection(func(w model.Welcome, out *clientv1.Welcome) {
		out.MissedMatches, out.MissedMore = missedMatches(w.Missed.Matches), int32(len(w.Missed.Older)) //nolint:gosec // a count of matches
	})
	return errors.Join(
		transport.Encode(e, func(p model.ArenaBar) any {
			return &clientv1.ArenaBar{SkillBar: p.Bar}
		}),
		transport.Encode(e, func(p model.MatchEnded) any {
			return matchEnded(p)
		}),
		transport.Encode(e, func(p model.MatchState) any {
			return matchState(p)
		}),
		transport.Encode(e, func(p model.PvpOffer) any {
			return &clientv1.PvpOffer{Offer: p.Offer, Mode: p.Mode, Bots: int32(p.Bots), UntilMs: uint32(p.Until.Milliseconds())} //nolint:gosec // a few bots, seconds
		}),
		transport.Encode(e, func(p model.PvpOfferCancelled) any {
			return &clientv1.PvpOfferGone{Offer: p.Offer, Reason: goneReasons[p.Reason], Requeued: p.Requeued}
		}),
		transport.Encode(e, func(p model.PvpQueued) any {
			return &clientv1.PvpQueued{Mode: p.Mode, SinceMs: uint32(p.Since.Milliseconds()), Members: p.Members, WaitingRoom: p.WaitingRoom} //nolint:gosec // a wait of minutes
		}),
		transport.Encode(e, func(p model.PvpUnqueued) any {
			return &clientv1.PvpUnqueued{Reason: unqueueReasons[p.Reason], By: p.By}
		}),
	)
}

var unqueueReasons = map[model.PvpUnqueueReason]clientv1.PvpUnqueueReason{
	model.PvpUnqueueLeft:         clientv1.PvpUnqueueReason_PVP_UNQUEUE_REASON_LEFT,
	model.PvpUnqueueDeclined:     clientv1.PvpUnqueueReason_PVP_UNQUEUE_REASON_DECLINED,
	model.PvpUnqueueLapsed:       clientv1.PvpUnqueueReason_PVP_UNQUEUE_REASON_LAPSED,
	model.PvpUnqueuePartyChanged: clientv1.PvpUnqueueReason_PVP_UNQUEUE_REASON_PARTY_CHANGED,
}

var goneReasons = map[model.PvpGoneReason]clientv1.PvpOfferGoneReason{
	model.PvpGoneDeclined: clientv1.PvpOfferGoneReason_PVP_OFFER_GONE_REASON_DECLINED,
	model.PvpGoneLapsed:   clientv1.PvpOfferGoneReason_PVP_OFFER_GONE_REASON_LAPSED,
}

var matchOutcomes = map[model.MatchOutcome]clientv1.MatchOutcome{
	model.MatchWin:  clientv1.MatchOutcome_MATCH_OUTCOME_WIN,
	model.MatchLoss: clientv1.MatchOutcome_MATCH_OUTCOME_LOSS,
	model.MatchDraw: clientv1.MatchOutcome_MATCH_OUTCOME_DRAW,
}

// missedMatches is a Welcome's matches that ended while the player was away.
func missedMatches(ms []model.MissedMatch) []*clientv1.MissedMatch {
	out := make([]*clientv1.MissedMatch, 0, len(ms))
	for _, m := range ms {
		out = append(out, &clientv1.MissedMatch{Mode: m.Mode, Result: matchOutcomes[m.Result], Left: m.Left, EndedAt: m.Ended.UnixMilli()})
	}
	return out
}

func matchEnded(e model.MatchEnded) *clientv1.MatchEnded {
	out := &clientv1.MatchEnded{Mode: e.Mode, Result: matchOutcomes[e.Result], Left: e.Left}
	if e.Battle {
		out.Score = []int32{int32(e.Score[0]), int32(e.Score[1])} //nolint:gosec // up to the goal
	} else {
		out.Wins = []int32{int32(e.Wins[0]), int32(e.Wins[1])} //nolint:gosec // a few rounds
	}
	if r := e.Rating; r != nil {
		out.Rating = &clientv1.PvpRatingChange{Before: int32(r.Before), After: int32(r.After), RankBefore: r.RankBefore, RankAfter: r.RankAfter, PlacementLeft: int32(r.PlacementLeft)} //nolint:gosec // ratings fit
	}
	return out
}

var matchPhases = map[model.MatchPhase]clientv1.MatchPhase{
	model.PhasePrep:      clientv1.MatchPhase_MATCH_PHASE_PREP,
	model.PhaseCountdown: clientv1.MatchPhase_MATCH_PHASE_COUNTDOWN,
	model.PhaseFight:     clientv1.MatchPhase_MATCH_PHASE_FIGHT,
	model.PhaseBetween:   clientv1.MatchPhase_MATCH_PHASE_BETWEEN,
	model.PhaseResult:    clientv1.MatchPhase_MATCH_PHASE_RESULT,
}

func matchState(st model.MatchState) *clientv1.MatchState {
	out := &clientv1.MatchState{
		Match: st.Match, Mode: st.Mode, Phase: matchPhases[st.Phase], Round: int32(st.Round), //nolint:gosec // a few rounds
		EndsInMs: uint32(st.EndsIn.Milliseconds()), //nolint:gosec // minutes at most
	}
	if st.Battle {
		out.Score = []int32{int32(st.Score[0]), int32(st.Score[1])} //nolint:gosec // up to the goal
		for _, p := range st.Points {
			out.Points = append(out.Points, &clientv1.CapturePoint{Id: p.ID, Holder: int32(p.Holder), Progress: p.Progress, Contested: p.Contested}) //nolint:gosec // 0 to 2
		}
	} else {
		out.Wins = []int32{int32(st.Wins[0]), int32(st.Wins[1])} //nolint:gosec // a few rounds
	}
	for _, side := range st.Sides {
		s := &clientv1.MatchSide{}
		for _, f := range side {
			s.Fighters = append(s.Fighters, &clientv1.MatchFighter{Id: f.ID, Name: f.Name, Class: f.Class, Bot: f.Bot, Away: f.Away})
		}
		out.Sides = append(out.Sides, s)
	}
	return out
}
