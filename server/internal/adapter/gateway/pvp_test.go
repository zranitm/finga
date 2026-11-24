package gateway

import (
	"reflect"
	"testing"
	"time"

	"finga/internal/application/pvp"
	model "finga/internal/model/gateway"
	"finga/internal/shared/kernel"
)

func TestEachPvpDeliveryKeepsItsValues(t *testing.T) {
	ann, bo := kernel.NewCharacterID(), kernel.NewCharacterID()
	tests := []struct {
		in   pvp.Delivery
		want model.Push
	}{
		{pvp.Queued{Mode: "team", Since: 2 * time.Second, Members: []kernel.CharacterID{ann, bo}}, model.PvpQueued{Mode: "team", Since: 2 * time.Second, Members: []string{ann.String(), bo.String()}}},
		{pvp.Queued{Mode: "duel", Since: 20 * time.Second, Members: []kernel.CharacterID{ann}, WaitingRoom: true}, model.PvpQueued{Mode: "duel", Since: 20 * time.Second, Members: []string{ann.String()}, WaitingRoom: true}},
		{pvp.Unqueued{Reason: pvp.UnqueueDeclined, By: []kernel.CharacterID{bo}}, model.PvpUnqueued{Reason: model.PvpUnqueueDeclined, By: []string{bo.String()}}},
		{pvp.Offered{Offer: "of-1", Mode: "team", Bots: 2, Until: 10 * time.Second}, model.PvpOffer{Offer: "of-1", Mode: "team", Bots: 2, Until: 10 * time.Second}},
		{pvp.OfferGone{Offer: "of-1", Reason: pvp.GoneLapsed, Requeued: true}, model.PvpOfferCancelled{Offer: "of-1", Reason: model.PvpGoneLapsed, Requeued: true}},
		{
			pvp.Ended{Mode: "team", Outcome: pvp.Loss, Wins: [2]int{2, 1}, Rating: &pvp.RatingChange{Before: 1000, After: 968, RankBefore: "bronze", RankAfter: "iron", PlacementLeft: 4}, Left: true},
			model.MatchEnded{Mode: "team", Result: model.MatchLoss, Wins: [2]int{2, 1}, Rating: &model.PvpRatingChange{Before: 1000, After: 968, RankBefore: "bronze", RankAfter: "iron", PlacementLeft: 4}, Left: true},
		},
		{pvp.Ended{Mode: "battle", Outcome: pvp.Win, Battle: true, Score: [2]int{1000, 640}}, model.MatchEnded{Mode: "battle", Result: model.MatchWin, Battle: true, Score: [2]int{1000, 640}}},
	}
	for _, tt := range tests {
		if got, ok := pvpPush(tt.in); !ok || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%T became %+v, want %+v", tt.in, got, tt.want)
		}
	}
}
