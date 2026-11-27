package pvp

import (
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/item"
	"finga/internal/application/pvp"
	"finga/pkg/copytest"
)

// A queue's push, an offer, its falling through and an unqueue reach the client with every field the wire has for them.
func TestEachQueuePushKeepsItsFields(t *testing.T) {
	const reason = "the enum's own map: TestEachPvpEnumKeepsItsName"
	if got := copytest.Diff[pvp.Queued, *clientv1.PvpQueued](copytest.Match{Renames: map[string]string{"Since": "SinceMs"}}, queued); len(got) > 0 {
		t.Errorf("Queued: %v", got)
	}
	if got := copytest.Diff[pvp.Unqueued, *clientv1.PvpUnqueued](copytest.Match{Skip: map[string]string{"Reason": reason}, Computed: map[string]string{"Reason": reason}}, unqueued); len(got) > 0 {
		t.Errorf("Unqueued: %v", got)
	}
	if got := copytest.Diff[pvp.Offered, *clientv1.PvpOffer](copytest.Match{Renames: map[string]string{"Until": "UntilMs"}}, offered); len(got) > 0 {
		t.Errorf("Offered: %v", got)
	}
	if got := copytest.Diff[pvp.OfferGone, *clientv1.PvpOfferGone](copytest.Match{Skip: map[string]string{"Reason": reason}, Computed: map[string]string{"Reason": reason}}, offerGone); len(got) > 0 {
		t.Errorf("OfferGone: %v", got)
	}
}

// A match's end reaches the client with the fields it has: a battle's score or a round match's wins, and a rated mode's rating.
func TestAMatchEndKeepsItsFields(t *testing.T) {
	const kind = "a battle's score, or a round match's wins: TestAMatchEndNamesAScoreOrWinsByItsKind"
	m := copytest.Match{
		Skip:     map[string]string{"Outcome": "the enum's own map: TestEachPvpEnumKeepsItsName", "Battle": kind, "Score": kind, "Wins": kind, "Match": "the match is the delivery's, to mark told", "Away": "the delivery's, to mark told"},
		Computed: map[string]string{"Result": "the outcome's own map", "Score": kind, "Wins": kind},
		Renames:  map[string]string{"Outcome": "Result"},
	}
	if got := copytest.Diff[pvp.Ended, *clientv1.MatchEnded](m, matchEnded); len(got) > 0 {
		t.Error(got)
	}
}

// A battle counts a score, any other match its rounds' wins.
func TestAMatchEndNamesAScoreOrWinsByItsKind(t *testing.T) {
	e := pvp.Ended{Wins: [2]int{2, 1}, Score: [2]int{30, 28}}
	if got := matchEnded(e); len(got.Wins) != 2 || got.Wins[0] != 2 || got.Score != nil {
		t.Fatalf("a round match went out as %+v", got)
	}
	e.Battle = true
	if got := matchEnded(e); len(got.Score) != 2 || got.Score[0] != 30 || got.Wins != nil {
		t.Fatalf("a battle went out as %+v", got)
	}
}

// A queue's reasons, an offer's, and a match's outcomes reach the client as their own; one the wire doesn't know reads as unspecified.
func TestEachPvpEnumKeepsItsName(t *testing.T) {
	for reason := range unqueueReasons {
		if got := unqueued(pvp.Unqueued{Reason: reason}).Reason; got == 0 && reason != pvp.UnqueueLeft {
			t.Errorf("unqueue reason %d went out unspecified", reason)
		}
	}
	if got := unqueued(pvp.Unqueued{Reason: pvp.UnqueueReason(9)}).Reason; got != 0 {
		t.Errorf("an unknown reason went out as %v", got)
	}
	if offerGone(pvp.OfferGone{Reason: pvp.GoneLapsed}).Reason == offerGone(pvp.OfferGone{Reason: pvp.GoneDeclined}).Reason {
		t.Error("an offer's two reasons went out alike")
	}
	if matchEnded(pvp.Ended{Outcome: pvp.Win}).Result == matchEnded(pvp.Ended{Outcome: pvp.Loss}).Result {
		t.Error("a win and a loss went out alike")
	}
}

// The pieces a match gives are told by rank or feat, the item module's names for what gave a piece, which this handler may not import: the test holds them to its.
func TestThePieceCausesAreTheItemModulesNames(t *testing.T) {
	got := piecesAdded(pvp.PiecesAdded{Pieces: []pvp.RankPiece{{Piece: "helm_silver"}}, Feats: []pvp.FeatPiece{{Piece: "victors_horns"}}})
	if len(got.Pieces) != 2 || got.Pieces[0].Cause != item.PieceFromRank || got.Pieces[1].Cause != item.PieceFromFeat || got.Pieces[0].Id != "helm_silver" || got.Pieces[1].Id != "victors_horns" {
		t.Fatalf("pieces %+v, the item module's causes %q and %q", got.Pieces, item.PieceFromRank, item.PieceFromFeat)
	}
}

// The missed matches of a Welcome keep their mode, result, whether the player left, and when they ended.
func TestAMissedMatchKeepsItsFields(t *testing.T) {
	m := copytest.Match{
		Skip:     map[string]string{"Match": "the match is marked told, not shown", "Outcome": "the outcome's own map: TestEachPvpEnumKeepsItsName", "Ended": "a time, in milliseconds: below"},
		Computed: map[string]string{"Result": "the outcome's own map: TestEachPvpEnumKeepsItsName", "EndedAt": "a time, in milliseconds: below"},
	}
	at := time.UnixMilli(1_764_000_000_000)
	if got := missedMatches([]pvp.MissedMatch{{Ended: at}})[0].EndedAt; got != at.UnixMilli() {
		t.Errorf("a match ended at %v went out at %d", at, got)
	}
	if got := copytest.Diff(m, missedMatches); len(got) > 0 {
		t.Error(got)
	}
}
