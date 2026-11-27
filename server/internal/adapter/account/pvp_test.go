package account

import (
	"reflect"
	"testing"
	"time"

	"finga/internal/application/pvp"
	"finga/internal/shared/kernel"
)

// Each pvp push reaches its player as the module sent it, and a match that ended while the player was away is the one marked told once it is handed on.
func TestEachPvpDeliveryIsHandedOnAsTheModuleSentIt(t *testing.T) {
	ann, match := kernel.NewCharacterID(), kernel.NewMatchID()
	for _, in := range []pvp.Delivery{
		pvp.Queued{Mode: "team", Since: 2 * time.Second, Members: []kernel.CharacterID{ann}},
		pvp.Unqueued{Reason: pvp.UnqueueDeclined, By: []kernel.CharacterID{ann}},
		pvp.Offered{Offer: "of-1", Mode: "team", Bots: 2, Until: 10 * time.Second},
		pvp.OfferGone{Offer: "of-1", Reason: pvp.GoneLapsed, Requeued: true},
		pvp.Ended{Mode: "team", Outcome: pvp.Loss, Wins: [2]int{2, 1}, Left: true},
		pvp.PiecesAdded{Feats: []pvp.FeatPiece{{Piece: "victors_horns"}}},
	} {
		got := pvpDelivery(pvp.Push{To: ann, Delivery: in})
		if got.Character != ann || got.Untold != (kernel.MatchID{}) || got.Delivery.Tick != 0 || got.Delivery.Starts || got.Delivery.MapID != "" {
			t.Errorf("%T became %+v", in, got)
			continue
		}
		if !reflect.DeepEqual(got.Delivery.Event, in) {
			t.Errorf("%T became %+v", in, got.Delivery.Event)
		}
	}
	if got := pvpDelivery(pvp.Push{To: ann, Delivery: pvp.Ended{Match: match, Away: true}}); got.Untold != match {
		t.Errorf("an away player's match %v went out untold as %v", match, got.Untold)
	}
	if got := pvpDelivery(pvp.Push{To: ann, Delivery: pvp.Ended{Match: match}}); got.Untold != (kernel.MatchID{}) {
		t.Errorf("a match the player was in is marked told too: %v", got.Untold)
	}
}
