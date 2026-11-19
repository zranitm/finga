package e2e_test

import (
	"net/http"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/uuidv7"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// D8: a dropped piece goes into the account's wardrobe, once, and is worn, and everyone on the map sees it; the look outlives a restart and the account's other characters may wear it.
func TestAPieceIsCollectedWornAndSeen(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	token, ayla := s.CreateCharacter(t, "dresser", "Ayla", "warrior")
	cleo := s.Call("POST", "/api/characters", token, map[string]string{"name": "Cleo"})
	s.MustStatus(cleo, http.StatusCreated)
	plate := s.Give(t, ayla, "piece.knight_plate", nil)
	s.Give(t, ayla, "piece.knight_plate", nil)
	bram := s.Enter(t, "watcher", "Bram", "beginner")
	p, _ := s.Welcome(t, "dresser", ayla)
	// Five commands, within the inventory limit's burst of five.
	ask := func(req string, msg any) *clientv1.Response {
		t.Helper()
		p.Send(wsconn.Message{ReqID: req, Data: msg})
		r := p.Expect("Response")
		if r.ReqID != req {
			t.Fatalf("answer to %s, want %s", r.ReqID, req)
		}
		return r.Data.(*clientv1.Response)
	}
	knight := &clientv1.Look{Outfit: "knight_plate", HideHeadwear: true}

	if r := ask("l-1", &clientv1.SetLook{IdempotencyKey: "k-" + uuidv7.NewString(), Look: knight}); r.GetOk() || r.Code != code("PIECE_NOT_COLLECTED") {
		t.Fatalf("a look before its piece: %+v", r)
	}
	if r := ask("w-1", &clientv1.AddToWardrobe{IdempotencyKey: "k-" + uuidv7.NewString(), ItemId: plate}); !r.GetOk() {
		t.Fatalf("adding the piece: %+v", r)
	}
	if ic := p.Expect("InventoryChanged").Data.(*clientv1.InventoryChanged); len(ic.Slots) != 1 || ic.Slots[0].GetItem().GetQuantity() != 1 {
		t.Fatalf("adding took %+v, want one of the two", ic.Slots)
	}
	if r := ask("w-3", &clientv1.AddToWardrobe{IdempotencyKey: "k-" + uuidv7.NewString(), ItemId: plate}); r.GetOk() || r.Code != code("PIECE_COLLECTED") {
		t.Fatalf("a piece the wardrobe holds: %+v", r)
	}
	if rows, quantity, _ := s.ItemTotals(t, ayla, "piece.knight_plate"); rows != 1 || quantity != 1 {
		t.Fatalf("%d rows holding %d, want the one left of two", rows, quantity)
	}

	if r := ask("l-2", &clientv1.SetLook{IdempotencyKey: "k-" + uuidv7.NewString(), Look: &clientv1.Look{Outfit: "knight_plate", Weapon: "greatsword"}}); r.GetOk() || r.Code != code("PIECE_NOT_COLLECTED") {
		t.Fatalf("a look with a piece not collected: %+v", r)
	}
	if r := ask("l-3", &clientv1.SetLook{IdempotencyKey: "k-" + uuidv7.NewString(), Look: knight}); !r.GetOk() {
		t.Fatalf("wearing the plate: %+v", r)
	}
	expectSeen(t, bram, ayla, knight)

	wardrobe := s.Call("GET", "/api/wardrobe", p.Token, nil)
	s.MustStatus(wardrobe, http.StatusOK)
	pieces, _ := wardrobe.Body["pieces"].([]any)
	if len(pieces) != 1 || pieces[0].(map[string]any)["id"] != "knight_plate" || pieces[0].(map[string]any)["cause"] != "item" || pieces[0].(map[string]any)["item_id"] != plate {
		t.Fatalf("wardrobe %v, want the plate from its item", wardrobe.Body)
	}

	s.Restart()
	back := s.Connect(t, ayla)
	if _, snap := back.Hello(login(s, "dresser")); !seenIn(snap.Players, ayla, knight) {
		t.Fatalf("Ayla came back in %+v, want the plate", snap.Players)
	}
	other := s.Connect(t, cleo.Body["id"].(string))
	other.Hello(login(s, "dresser"))
	other.Send(wsconn.Message{ReqID: "l-4", Data: &clientv1.SetLook{IdempotencyKey: "k-" + uuidv7.NewString(), Look: knight}})
	if r := other.Expect("Response"); !r.Data.(*clientv1.Response).GetOk() {
		t.Fatalf("the account's other character wearing the plate: %+v", r)
	}
	s.CheckInvariants(t)
}

// login is a fresh session token for username.
func login(s *e2etest.Stack, username string) string {
	return s.Call("POST", "/api/sessions", "", map[string]string{"username": username, "password": "correct horse"}).Body["token"].(string)
}

// seenIn is whether views show who in look.
func seenIn(views []*clientv1.PlayerView, who string, look *clientv1.Look) bool {
	for _, v := range views {
		if v.GetId() == who && proto.Equal(v.GetLook(), look) {
			return true
		}
	}
	return false
}

// expectSeen waits for a frame of watcher's that shows who in look; 15 s only guards a hang, as Expect's does.
func expectSeen(t *testing.T, watcher *e2etest.Player, who string, look *clientv1.Look) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		m, ok := watcher.Read(time.Until(deadline))
		if !ok {
			break
		}
		if delta, isDelta := m.Data.(*clientv1.Delta); isDelta && seenIn(delta.Players, who, look) {
			return
		}
	}
	t.Fatalf("no frame showed %s in %+v within 15 s", who, look)
}
