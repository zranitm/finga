package push_test

import (
	"fmt"
	"sync"
	"testing"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/handler/ws/push"
	"finga/services/gateway/internal/model"
)

func TestEndTellsTheClientWhyWhenTheProtocolHasAReason(t *testing.T) {
	tests := []struct {
		reason model.EndReason
		kicked clientv1.KickReason // unspecified: no Kicked, the client reconnects
		code   int
		why    string
	}{
		{model.EndLoggedInElsewhere, clientv1.KickReason_KICK_REASON_LOGGED_IN_ELSEWHERE, wsconn.CloseKicked, "logged_in_elsewhere"},
		{model.EndSessionExpired, clientv1.KickReason_KICK_REASON_SESSION_EXPIRED, wsconn.CloseKicked, "session_expired"},
		{model.EndServerRestart, clientv1.KickReason_KICK_REASON_SERVER_RESTART, wsconn.CloseKicked, "server_restart"},
		{model.EndDeliveryBacklog, clientv1.KickReason_KICK_REASON_UNSPECIFIED, wsconn.CloseInternal, "delivery_backlog"},
		{model.EndRejoinFailed, clientv1.KickReason_KICK_REASON_UNSPECIFIED, wsconn.CloseInternal, "rejoin_failed"},
	}
	for _, tt := range tests {
		t.Run(string(tt.reason), func(t *testing.T) {
			s := &socket{}
			push.NewOutbox(s, s).End(tt.reason)
			s.mu.Lock()
			defer s.mu.Unlock()
			switch {
			case tt.kicked == clientv1.KickReason_KICK_REASON_UNSPECIFIED && len(s.sent) != 0:
				t.Fatalf("sent %+v before closing", s.sent)
			case tt.kicked != clientv1.KickReason_KICK_REASON_UNSPECIFIED && (len(s.sent) != 1 || s.sent[0].Data.(*clientv1.Kicked).GetReason() != tt.kicked):
				t.Fatalf("sent %+v, want Kicked %s", s.sent, tt.kicked)
			}
			if s.code != tt.code || s.why != tt.why {
				t.Fatalf("closed %d %q, want %d %q", s.code, s.why, tt.code, tt.why)
			}
		})
	}
}

func TestEachPushReachesTheClientAsItsMessage(t *testing.T) {
	players := []*clientv1.PlayerView{{Id: "p-1", Name: "Robin", Vx: 200, Facing: -1, OnGround: true}}
	mobs := []*clientv1.MobView{{Id: 7, Type: "green_slime", Hp: 9, MaxHp: 30}}
	drops := []*clientv1.DropView{{Id: 3, Template: "slime_gel", Quantity: 2}}
	party := &clientv1.PartyView{Leader: "p-1", Members: []*clientv1.PartyMember{{CharacterId: "p-1", Name: "Robin", Online: true}}}
	tests := []struct {
		push model.Push
		want proto.Message
	}{
		{model.Snapshot{Players: players, Mobs: mobs, Drops: drops}, &clientv1.Snapshot{Players: players, Mobs: mobs, Drops: drops}},
		{
			model.Delta{AckSeq: 3, Players: players, Mobs: mobs, Drops: drops, RemovedPlayers: []string{"p-2"}, RemovedMobs: []uint32{6}, RemovedDrops: []uint32{2}},
			&clientv1.Delta{AckSeq: 3, Players: players, Mobs: mobs, Drops: drops, RemovedPlayers: []string{"p-2"}, RemovedMobs: []uint32{6}, RemovedDrops: []uint32{2}},
		},
		{model.MapChange{MapID: "forest", X: 64, Y: 608}, &clientv1.MapChange{MapId: "forest", X: 64, Y: 608}},
		{model.Damage{Source: "p:p-1", Target: "m:7", Amount: 21, Crit: true, Miss: true}, &clientv1.Damage{Source: "p:p-1", Target: "m:7", Amount: 21, Crit: true, Miss: true}},
		{model.Died{CharacterID: "p-1", X: 1, Y: 2}, &clientv1.Died{CharacterId: "p-1", X: 1, Y: 2}},
		{model.Respawned{CharacterID: "p-1", MapID: "town", X: 416, Y: 608, HP: 102}, &clientv1.Respawned{CharacterId: "p-1", MapId: "town", X: 416, Y: 608, Hp: 102}},
		{model.ExpGained{Amount: 5, Exp: 15}, &clientv1.ExpGained{Amount: 5, Exp: 15}},
		{model.LevelUp{Character: &clientv1.CharacterView{Id: "p-1", Level: 2}}, &clientv1.LevelUp{Character: &clientv1.CharacterView{Id: "p-1", Level: 2}}},
		{
			model.ChatMessage{Channel: clientv1.ChatChannel_CHAT_CHANNEL_PARTY, Sender: "p-1", SenderName: "Robin", Text: "psst"},
			&clientv1.ChatMessage{Channel: clientv1.ChatChannel_CHAT_CHANNEL_PARTY, Sender: "p-1", SenderName: "Robin", Text: "psst"},
		},
		{model.InventoryChanged{Slots: []*clientv1.InventorySlot{{Tab: "etc", Slot: 1}}}, &clientv1.InventoryChanged{Slots: []*clientv1.InventorySlot{{Tab: "etc", Slot: 1}}}},
		{model.PickupFailed{Why: model.Public{Code: "inventory_full", Message: "your inventory is full"}}, &clientv1.Response{Code: "inventory_full", Message: "your inventory is full"}},
		{model.PartyState{Party: party}, &clientv1.PartyState{Party: party}},
		{model.PartyState{}, &clientv1.PartyState{}},
		{model.PartyInvited{InviteID: "inv-2", From: "p-1", FromName: "Robin"}, &clientv1.PartyInvited{InviteId: "inv-2", From: "p-1", FromName: "Robin"}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%T", tt.push), func(t *testing.T) {
			s := &socket{}
			push.NewOutbox(s, s).Deliver(model.Delivery{Tick: 9, Push: tt.push})
			s.mu.Lock()
			defer s.mu.Unlock()
			if len(s.sent) != 1 {
				t.Fatalf("sent %d messages", len(s.sent))
			}
			m := s.sent[0]
			if name := string(tt.want.ProtoReflect().Descriptor().Name()); m.Type != name || m.Tick != 9 || !proto.Equal(m.Data.(proto.Message), tt.want) || s.counted[0] != name {
				t.Fatalf("sent %s at tick %d: %v, want %s: %v", m.Type, m.Tick, m.Data, name, tt.want)
			}
		})
	}
}

// socket records what a connection sends, how it first closes, and the types the outbox counted.
type socket struct {
	mu      sync.Mutex
	sent    []wsconn.Message
	counted []string
	code    int
	why     string
}

func (s *socket) MessageOut(msgType string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counted = append(s.counted, msgType)
}

func (s *socket) ID() string { return "c-1" }

func (s *socket) Send(m wsconn.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, m)
	return nil
}

func (s *socket) Close(code int, why string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.code == 0 {
		s.code, s.why = code, why
	}
}
