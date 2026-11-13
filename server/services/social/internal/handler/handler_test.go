package handler_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/gamedata"
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/observability/metrics"
	"finga/pkg/panics"
	"finga/pkg/uuidv7"
	"finga/services/social"
	"finga/services/social/internal/handler"
	"finga/services/social/internal/model"
)

type stack struct {
	c     socialv1.SocialServiceClient
	ctx   context.Context
	notes *notes
	joins atomic.Uint64 // the last join's sequence
}

func newStack(t *testing.T) *stack {
	t.Helper()
	return newStackOver(t, func(svc handler.Service) handler.Service { return svc })
}

// newStackOver serves the module's facade as wrap returns it.
func newStackOver(t *testing.T, wrap func(handler.Service) handler.Service) *stack {
	t.Helper()
	return newStackRecording(t, wrap, panics.NewRecorder(slog.New(slog.DiscardHandler), nil))
}

// newStackRecording serves the module's facade as wrap returns it, recording a sweep's panic in rec.
func newStackRecording(t *testing.T, wrap func(handler.Service) handler.Service, rec *panics.Recorder) *stack {
	t.Helper()
	streams := handler.NewStreams("boot-1")
	n := &notes{Streams: streams, online: map[kernel.CharacterID]bool{}}
	h, err := host.New(host.Options{Name: "social", Logger: slog.New(slog.DiscardHandler), Registry: metrics.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	m, err := h.Module("social")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc, err := social.New(ctx, social.Deps{Module: m, Data: gamedata.New(gamedata.Parts{ChatLineMax: 120}), Notifier: n})
	if err != nil {
		t.Fatal(err)
	}
	g := grpcx.NewServer(grpcx.ServerOptions{Errors: handler.Errors, Logger: slog.New(slog.DiscardHandler)})
	socialv1.RegisterSocialServiceServer(g, handler.New(wrap(svc), streams, rec))
	return &stack{c: socialv1.NewSocialServiceClient(grpctest.Conn(t, g)), ctx: ctx, notes: n}
}

// notes hands the module's notes on to the streams, keeping whether each member in a party was online in the last state their party heard.
type notes struct {
	*handler.Streams
	mu     sync.Mutex
	online map[kernel.CharacterID]bool
}

func (n *notes) PartyChanged(members []kernel.CharacterID, p *model.Party) {
	n.mu.Lock()
	if p == nil {
		for _, id := range members {
			delete(n.online, id)
		}
	} else {
		for _, m := range p.Members {
			n.online[m.ID] = m.Online
		}
	}
	n.mu.Unlock()
	n.Streams.PartyChanged(members, p)
}

func (s *stack) gateway(t *testing.T, id string) grpc.ServerStreamingClient[socialv1.DeliverResponse] {
	t.Helper()
	return s.gatewayUntil(t, s.ctx, id)
}

// gatewayUntil subscribes as the gateway named id until ctx ends.
func (s *stack) gatewayUntil(t *testing.T, ctx context.Context, id string) grpc.ServerStreamingClient[socialv1.DeliverResponse] {
	t.Helper()
	stream, err := s.c.Deliver(ctx, &socialv1.DeliverRequest{GatewayId: id})
	if err != nil {
		t.Fatal(err)
	}
	if m, err := stream.Recv(); err != nil || m.GetSubscribed().GetBootId() != "boot-1" {
		t.Fatalf("first %+v %v", m, err)
	}
	return stream
}

// online registers the character through the connection named conn-<name>.
func (s *stack) online(t *testing.T, gateway string, id kernel.CharacterID, name string) {
	t.Helper()
	s.onlineThrough(t, gateway, "conn-"+name, s.join(), id, name)
}

// onlineThrough registers the character through conn, which the world's join of sequence seq is behind.
func (s *stack) onlineThrough(t *testing.T, gateway, conn string, seq uint64, id kernel.CharacterID, name string) {
	t.Helper()
	member := &socialv1.Member{Id: id.String(), Name: name, Class: "beginner", Level: 3, Hp: 90, MaxHp: 100, MapId: "forest"}
	if _, err := s.c.Online(s.ctx, &socialv1.OnlineRequest{GatewayId: gateway, ConnectionId: conn, Member: member, JoinSeq: seq}); err != nil {
		t.Fatal(err)
	}
}

// join is the sequence of a new join, as the world hands them out.
func (s *stack) join() uint64 {
	return s.joins.Add(1)
}

// isOnline is whether the character's party shows them online.
func (s *stack) isOnline(t *testing.T, id kernel.CharacterID) bool {
	t.Helper()
	s.notes.mu.Lock()
	defer s.notes.mu.Unlock()
	online, ok := s.notes.online[id]
	if !ok {
		t.Fatalf("%s has no party", id)
	}
	return online
}

func (s *stack) party(t *testing.T, leader, member kernel.CharacterID) {
	t.Helper()
	inv, err := s.c.Invite(s.ctx, &socialv1.InviteRequest{CharacterId: leader.String(), TargetId: member.String()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.c.Respond(s.ctx, &socialv1.RespondRequest{CharacterId: member.String(), InviteId: inv.GetInviteId(), Accept: true}); err != nil {
		t.Fatal(err)
	}
}

func recv[T any](t *testing.T, stream grpc.ServerStreamingClient[T]) *T {
	t.Helper()
	m, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPartyTrafficReachesEachMembersGateway(t *testing.T) {
	s := newStack(t)
	gwA, gwB := s.gateway(t, "gw-a"), s.gateway(t, "gw-b")
	ann, bo := kernel.NewCharacterID(), kernel.NewCharacterID()
	s.online(t, "gw-a", ann, "Ann")
	s.online(t, "gw-b", bo, "Bo")
	for stream, who := range map[grpc.ServerStreamingClient[socialv1.DeliverResponse]]kernel.CharacterID{gwA: ann, gwB: bo} {
		if m := recv(t, stream); m.GetCharacterId() != who.String() || m.GetPartyState() == nil || m.GetPartyState().GetParty() != nil {
			t.Fatalf("at login %s heard %+v, want an empty party", who, m)
		}
	}

	inv, err := s.c.Invite(s.ctx, &socialv1.InviteRequest{CharacterId: ann.String(), TargetId: bo.String()})
	if err != nil {
		t.Fatal(err)
	}
	m := recv(t, gwB)
	if m.GetCharacterId() != bo.String() || m.GetPartyInvited().GetInviteId() != inv.GetInviteId() || m.GetPartyInvited().GetFromName() != "Ann" || m.GetPartyInvited().GetFromId() != ann.String() {
		t.Fatalf("invite %+v", m)
	}
	if _, err := s.c.Respond(s.ctx, &socialv1.RespondRequest{CharacterId: bo.String(), InviteId: inv.GetInviteId(), Accept: true}); err != nil {
		t.Fatal(err)
	}
	for stream, who := range map[grpc.ServerStreamingClient[socialv1.DeliverResponse]]kernel.CharacterID{gwA: ann, gwB: bo} {
		m := recv(t, stream)
		p := m.GetPartyState().GetParty()
		if m.GetCharacterId() != who.String() || p.GetLeaderId() != ann.String() || len(p.GetMembers()) != 2 || p.GetMembers()[1].GetName() != "Bo" || p.GetMembers()[1].GetClass() != "beginner" || p.GetMembers()[1].GetMapId() != "forest" {
			t.Fatalf("state for %s: %+v", who, m)
		}
	}

	t.Run("a member's update reaches the party", func(t *testing.T) {
		if _, err := s.c.UpdateMember(s.ctx, &socialv1.UpdateMemberRequest{CharacterId: bo.String(), MapId: "cave", Class: "warrior", Level: 4, Hp: 50, MaxHp: 120}); err != nil {
			t.Fatal(err)
		}
		mb := recv(t, gwA).GetPartyState().GetParty().GetMembers()[1]
		if mb.GetMapId() != "cave" || mb.GetClass() != "warrior" || mb.GetLevel() != 4 || mb.GetHp() != 50 || mb.GetMaxHp() != 120 {
			t.Fatalf("member %+v", mb)
		}
		recv(t, gwB)
	})
	t.Run("PartyOf is retired", func(t *testing.T) {
		if _, err := s.c.PartyOf(s.ctx, &socialv1.PartyOfRequest{CharacterId: bo.String()}); status.Code(err) != codes.Unimplemented {
			t.Fatalf("PartyOf: %v, want Unimplemented", err)
		}
	})
	t.Run("a party line reaches every member on their gateway", func(t *testing.T) {
		res, err := s.c.Chat(s.ctx, &socialv1.ChatRequest{SenderId: ann.String(), SenderName: "Ann", Channel: socialv1.ChatChannel_CHAT_CHANNEL_PARTY, Text: "  hi\tall "})
		if err != nil || res.GetText() != "hiall" || res.GetChannel() != socialv1.ChatChannel_CHAT_CHANNEL_PARTY {
			t.Fatalf("res %+v %v", res, err)
		}
		for _, stream := range []grpc.ServerStreamingClient[socialv1.DeliverResponse]{gwA, gwB} {
			line := recv(t, stream).GetChatLine()
			if line.GetText() != "hiall" || line.GetSenderId() != ann.String() || line.GetSenderName() != "Ann" {
				t.Fatalf("line %+v", line)
			}
		}
	})
	t.Run("a map line comes back to the caller and nothing is delivered", func(t *testing.T) {
		res, err := s.c.Chat(s.ctx, &socialv1.ChatRequest{SenderId: bo.String(), SenderName: "Bo", Channel: socialv1.ChatChannel_CHAT_CHANNEL_MAP, Text: "hello map"})
		if err != nil || res.GetChannel() != socialv1.ChatChannel_CHAT_CHANNEL_MAP || res.GetText() != "hello map" {
			t.Fatalf("res %+v %v", res, err)
		}
	})
	t.Run("going offline is told to the party, and is the next thing it hears", func(t *testing.T) {
		if _, err := s.c.Offline(s.ctx, &socialv1.OfflineRequest{CharacterId: bo.String(), ConnectionId: "conn-Bo"}); err != nil {
			t.Fatal(err)
		}
		// The map line above delivered nothing, so this is the PartyState.
		m := recv(t, gwA)
		if m.GetPartyState() == nil {
			t.Fatalf("next message %+v", m)
		}
		if mb := m.GetPartyState().GetParty().GetMembers()[1]; mb.GetOnline() {
			t.Fatalf("member %+v", mb)
		}
	})
}

func TestCommandsAnswerWithTheirReasons(t *testing.T) {
	s := newStack(t)
	ann, bo := kernel.NewCharacterID(), kernel.NewCharacterID()
	s.online(t, "gw", ann, "Ann")
	s.online(t, "gw", bo, "Bo")
	s.party(t, ann, bo)
	tests := []struct {
		name string
		call func() error
		want socialv1.Reason
	}{
		{"inviting yourself", func() error {
			_, err := s.c.Invite(s.ctx, &socialv1.InviteRequest{CharacterId: ann.String(), TargetId: ann.String()})
			return err
		}, socialv1.Reason_REASON_INVITE_SELF},
		{"an invite nobody made", func() error {
			_, err := s.c.Respond(s.ctx, &socialv1.RespondRequest{CharacterId: bo.String(), InviteId: uuidv7.NewString(), Accept: true})
			return err
		}, socialv1.Reason_REASON_INVITE_EXPIRED},
		{"a malformed invite ID", func() error {
			_, err := s.c.Respond(s.ctx, &socialv1.RespondRequest{CharacterId: bo.String(), InviteId: "nope", Accept: true})
			return err
		}, socialv1.Reason_REASON_BAD_ID},
		{"a member kicking", func() error {
			_, err := s.c.Kick(s.ctx, &socialv1.KickRequest{CharacterId: bo.String(), TargetId: ann.String()})
			return err
		}, socialv1.Reason_REASON_NOT_PARTY_LEADER},
		{"promoting a stranger", func() error {
			_, err := s.c.Promote(s.ctx, &socialv1.PromoteRequest{CharacterId: ann.String(), TargetId: kernel.NewCharacterID().String()})
			return err
		}, socialv1.Reason_REASON_NOT_PARTY_MEMBER},
		{"leaving with no party", func() error {
			_, err := s.c.LeaveParty(s.ctx, &socialv1.LeavePartyRequest{CharacterId: kernel.NewCharacterID().String()})
			return err
		}, socialv1.Reason_REASON_NO_PARTY},
		{"a malformed target", func() error {
			_, err := s.c.Invite(s.ctx, &socialv1.InviteRequest{CharacterId: ann.String(), TargetId: "x"})
			return err
		}, socialv1.Reason_REASON_BAD_ID},
		{"an empty line", func() error {
			_, err := s.c.Chat(s.ctx, &socialv1.ChatRequest{SenderId: ann.String(), Channel: socialv1.ChatChannel_CHAT_CHANNEL_MAP, Text: " \n "})
			return err
		}, socialv1.Reason_REASON_CHAT_EMPTY},
		{"no channel", func() error {
			_, err := s.c.Chat(s.ctx, &socialv1.ChatRequest{SenderId: ann.String(), Text: "hi"})
			return err
		}, socialv1.Reason_REASON_BAD_CHAT_CHANNEL},
		{"a party line with no party", func() error {
			_, err := s.c.Chat(s.ctx, &socialv1.ChatRequest{SenderId: kernel.NewCharacterID().String(), Channel: socialv1.ChatChannel_CHAT_CHANNEL_PARTY, Text: "hi"})
			return err
		}, socialv1.Reason_REASON_CHAT_NO_PARTY},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(tt.want) {
				t.Fatalf("got %v, want %s", err, tt.want)
			}
		})
	}
	t.Run("the leader promotes, kicks, and the kicked one can leave nothing", func(t *testing.T) {
		if _, err := s.c.Promote(s.ctx, &socialv1.PromoteRequest{CharacterId: ann.String(), TargetId: bo.String()}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.c.Kick(s.ctx, &socialv1.KickRequest{CharacterId: bo.String(), TargetId: ann.String()}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.c.LeaveParty(s.ctx, &socialv1.LeavePartyRequest{CharacterId: ann.String()}); grpcx.Key(socialv1.Reason_REASON_NO_PARTY) != reasonOf(err) {
			t.Fatalf("got %v", err)
		}
	})
}

func reasonOf(err error) grpcx.Reason {
	r, _ := grpcx.ReasonOf(err)
	return r
}

func TestWatchParties(t *testing.T) {
	s := newStack(t)
	ann, bo, cy, di := kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID()
	for _, id := range []kernel.CharacterID{ann, bo, cy, di} {
		s.online(t, "gw", id, "X")
	}
	s.party(t, ann, bo)
	stream, err := s.c.WatchParties(s.ctx, &socialv1.WatchPartiesRequest{WorldId: "world-1"})
	if err != nil {
		t.Fatal(err)
	}
	if m := recv(t, stream); m.GetSubscribed().GetBootId() != "boot-1" {
		t.Fatalf("first %+v", m)
	}
	var first, second string // the two parties' IDs
	t.Run("the current memberships follow Subscribed", func(t *testing.T) {
		ms := recv(t, stream).GetMembership()
		if len(ms.GetCharacterIds()) != 2 || len(ms.GetMembers()) != 2 || ms.GetPartyId() == "" {
			t.Fatalf("membership %+v", ms)
		}
		first = ms.GetPartyId()
	})
	t.Run("then each change", func(t *testing.T) {
		s.party(t, cy, di)
		ms := recv(t, stream).GetMembership()
		if len(ms.GetMembers()) != 2 || (ms.GetMembers()[0] != cy.String() && ms.GetMembers()[1] != cy.String()) || ms.GetPartyId() == "" || ms.GetPartyId() == first {
			t.Fatalf("membership %+v", ms)
		}
		second = ms.GetPartyId()
	})
	t.Run("a leaver's party is empty, the rest keep theirs", func(t *testing.T) {
		if _, err := s.c.LeaveParty(s.ctx, &socialv1.LeavePartyRequest{CharacterId: di.String()}); err != nil {
			t.Fatal(err)
		}
		var left, rest *socialv1.Membership
		for range 2 {
			ms := recv(t, stream).GetMembership()
			if len(ms.GetMembers()) == 0 {
				left = ms
			} else {
				rest = ms
			}
		}
		if left == nil || left.GetCharacterIds()[0] != di.String() || left.GetPartyId() != "" || rest == nil || len(rest.GetMembers()) != 1 || rest.GetMembers()[0] != cy.String() || rest.GetPartyId() != second {
			t.Fatalf("left %+v rest %+v; the party keeps its ID as members leave", left, rest)
		}
	})
}

// manyParties reports n two-member parties besides the real ones.
type manyParties struct {
	handler.Service
	n int
}

func (m manyParties) Memberships(ctx context.Context, visit func(party social.PartyID, members []kernel.CharacterID)) error {
	for range m.n {
		visit(model.NewPartyID(), []kernel.CharacterID{kernel.NewCharacterID(), kernel.NewCharacterID()})
	}
	return m.Service.Memberships(ctx, visit)
}

// A snapshot larger than the stream's queue still arrives whole, and the changes after it.
func TestWatchPartiesSendsASnapshotLargerThanItsQueue(t *testing.T) {
	const many = 5000 // the queue holds 4096
	s := newStackOver(t, func(svc handler.Service) handler.Service { return manyParties{Service: svc, n: many} })
	stream, err := s.c.WatchParties(s.ctx, &socialv1.WatchPartiesRequest{WorldId: "world-1"})
	if err != nil {
		t.Fatal(err)
	}
	if m := recv(t, stream); m.GetSubscribed().GetBootId() != "boot-1" {
		t.Fatalf("first %+v", m)
	}
	for i := range many {
		if m := recv(t, stream); len(m.GetMembership().GetMembers()) != 2 {
			t.Fatalf("snapshot message %d: %+v", i, m)
		}
	}
	ann, bo := kernel.NewCharacterID(), kernel.NewCharacterID()
	s.online(t, "gw", ann, "Ann")
	s.online(t, "gw", bo, "Bo")
	s.party(t, ann, bo)
	if ms := recv(t, stream).GetMembership(); len(ms.GetMembers()) != 2 {
		t.Fatalf("the change after the snapshot: %+v; a login in no party changes no membership", ms)
	}
}

func TestStreamsNeedTheirSubscriberID(t *testing.T) {
	s := newStack(t)
	d, err := s.c.Deliver(s.ctx, &socialv1.DeliverRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Recv(); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("deliver: %v", err)
	}
	w, err := s.c.WatchParties(s.ctx, &socialv1.WatchPartiesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Recv(); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("watch: %v", err)
	}
	if _, err := s.c.Online(s.ctx, &socialv1.OnlineRequest{ConnectionId: "c-1", JoinSeq: 1, Member: &socialv1.Member{Id: kernel.NewCharacterID().String()}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("online without a gateway: %v", err)
	}
	if _, err := s.c.Online(s.ctx, &socialv1.OnlineRequest{GatewayId: "gw", JoinSeq: 1, Member: &socialv1.Member{Id: kernel.NewCharacterID().String()}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("online without a connection: %v", err)
	}
	if _, err := s.c.Online(s.ctx, &socialv1.OnlineRequest{GatewayId: "gw", ConnectionId: "c-1", Member: &socialv1.Member{Id: kernel.NewCharacterID().String()}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("online without the join's sequence: %v", err)
	}
	if _, err := s.c.Offline(s.ctx, &socialv1.OfflineRequest{CharacterId: kernel.NewCharacterID().String()}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("offline without a connection: %v", err)
	}
}

// The old connection's Offline, arriving after the player reconnected, leaves them online and reachable.
func TestAStaleOfflineLeavesTheNewConnectionOnline(t *testing.T) {
	s := newStack(t)
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second) // a line that never comes fails, not hangs
	defer cancel()
	gwA, gwB := s.gateway(t, "gw-a"), s.gatewayUntil(t, ctx, "gw-b")
	ann, bo := kernel.NewCharacterID(), kernel.NewCharacterID()
	s.online(t, "gw-a", ann, "Ann")
	s.online(t, "gw-a", bo, "Bo")
	s.party(t, ann, bo)
	recv(t, gwA) // the invite
	recv(t, gwA)
	recv(t, gwA) // the PartyState, for each
	s.onlineThrough(t, "gw-b", "conn-Bo-2", s.join(), bo, "Bo")
	recv(t, gwA)
	recv(t, gwB)
	if _, err := s.c.Offline(s.ctx, &socialv1.OfflineRequest{CharacterId: bo.String(), ConnectionId: "conn-Bo"}); err != nil {
		t.Fatal(err)
	}
	if !s.isOnline(t, bo) {
		t.Fatal("the old connection's Offline took Bo offline")
	}
	if _, err := s.c.Chat(s.ctx, &socialv1.ChatRequest{SenderId: ann.String(), SenderName: "Ann", Channel: socialv1.ChatChannel_CHAT_CHANNEL_PARTY, Text: "still there?"}); err != nil {
		t.Fatal(err)
	}
	if line := recv(t, gwB).GetChatLine(); line.GetText() != "still there?" {
		t.Fatalf("Bo's new gateway got %+v", line)
	}
}

// An ended tab's registration that lands after the live tab's changes nothing: not the party, and not where the character's deliveries go.
func TestALateOnlineFromAnEndedTabLeavesTheLiveTabsDeliveries(t *testing.T) {
	s := newStack(t)
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second) // a line that never comes fails, not hangs
	defer cancel()
	gwAnn, gwA, gwB := s.gateway(t, "gw-ann"), s.gatewayUntil(t, ctx, "gw-a"), s.gatewayUntil(t, ctx, "gw-b")
	ann, bo := kernel.NewCharacterID(), kernel.NewCharacterID()
	tabA, tabB := uuidv7.NewString(), uuidv7.NewString() // A connected first, then B took the character
	s.online(t, "gw-ann", ann, "Ann")
	joinA := s.join()
	s.onlineThrough(t, "gw-a", tabA, joinA, bo, "Bo")
	s.party(t, ann, bo)
	recv(t, gwA) // the invite
	recv(t, gwAnn)
	recv(t, gwA) // the PartyState, for each
	s.onlineThrough(t, "gw-b", tabB, s.join(), bo, "Bo")
	recv(t, gwAnn)
	recv(t, gwB)
	s.onlineThrough(t, "gw-a", tabA, joinA, bo, "Bo") // A's registration, on its way before B's, lands last
	if _, err := s.c.Offline(s.ctx, &socialv1.OfflineRequest{CharacterId: bo.String(), ConnectionId: tabA}); err != nil {
		t.Fatal(err) // A, ended, closes
	}
	if !s.isOnline(t, bo) {
		t.Fatal("the ended tab took Bo offline")
	}
	if _, err := s.c.Chat(s.ctx, &socialv1.ChatRequest{SenderId: ann.String(), SenderName: "Ann", Channel: socialv1.ChatChannel_CHAT_CHANNEL_PARTY, Text: "still there?"}); err != nil {
		t.Fatal(err)
	}
	if line := recv(t, gwB).GetChatLine(); line.GetText() != "still there?" {
		t.Fatalf("Bo's live tab got %+v", line)
	}
}

// The gateway's newest login is the one whose join finished last, which need not be the tab that connected last; social follows the join.
func TestATabThatConnectedFirstButJoinedLastStaysOnline(t *testing.T) {
	s := newStack(t)
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second) // a line that never comes fails, not hangs
	defer cancel()
	gwAnn, gwA, gwB := s.gateway(t, "gw-ann"), s.gatewayUntil(t, ctx, "gw-a"), s.gatewayUntil(t, ctx, "gw-b")
	ann, bo := kernel.NewCharacterID(), kernel.NewCharacterID()
	tabA, tabB := uuidv7.NewString(), uuidv7.NewString() // A connected first
	s.online(t, "gw-ann", ann, "Ann")
	s.onlineThrough(t, "gw-b", tabB, s.join(), bo, "Bo") // B's Hello finished first
	s.party(t, ann, bo)
	recv(t, gwB) // the invite
	recv(t, gwAnn)
	recv(t, gwB)                                         // the PartyState, for each
	s.onlineThrough(t, "gw-a", tabA, s.join(), bo, "Bo") // then A's, which ended B
	recv(t, gwAnn)
	recv(t, gwA)
	if _, err := s.c.Offline(s.ctx, &socialv1.OfflineRequest{CharacterId: bo.String(), ConnectionId: tabB}); err != nil {
		t.Fatal(err) // B, ended, closes
	}
	if !s.isOnline(t, bo) {
		t.Fatal("Bo is offline while playing in tab A")
	}
	if _, err := s.c.Chat(s.ctx, &socialv1.ChatRequest{SenderId: ann.String(), SenderName: "Ann", Channel: socialv1.ChatChannel_CHAT_CHANNEL_PARTY, Text: "still there?"}); err != nil {
		t.Fatal(err)
	}
	if line := recv(t, gwA).GetChatLine(); line.GetText() != "still there?" {
		t.Fatalf("Bo's tab A got %+v", line)
	}
}

// A gateway whose stream stays gone past the grace takes its players offline; one back in time does not.
func TestAGatewayGoneForGoodTakesItsPlayersOffline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newStack(t)
		s.gateway(t, "gw-a")
		ann, bo, cy := kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID()
		s.online(t, "gw-a", ann, "Ann")
		s.online(t, "gw-a", cy, "Cy")
		ctxB, endB := context.WithCancel(s.ctx)
		s.gatewayUntil(t, ctxB, "gw-b")
		s.online(t, "gw-b", bo, "Bo")
		s.party(t, ann, bo)
		ctxC, endC := context.WithCancel(s.ctx)
		s.gatewayUntil(t, ctxC, "gw-c")
		s.onlineThrough(t, "gw-c", "conn-Cy-2", s.join(), cy, "Cy")
		s.party(t, ann, cy)

		endB()
		endC()
		synctest.Wait()
		time.Sleep(time.Second)
		s.gateway(t, "gw-c") // back within the grace
		time.Sleep(time.Minute)
		synctest.Wait()
		if s.isOnline(t, bo) {
			t.Fatal("Bo is online through a gateway gone for a minute")
		}
		if !s.isOnline(t, cy) || !s.isOnline(t, ann) {
			t.Fatal("a player of a gateway that came back, or of one that never left, went offline")
		}
	})
}

// gatedOffline holds each Offline until gate closes, and tells on reached when one waits.
type gatedOffline struct {
	handler.Service
	gate    chan struct{}
	reached chan struct{}
}

func (g gatedOffline) Offline(ctx context.Context, id kernel.CharacterID, conn string) error {
	select {
	case g.reached <- struct{}{}:
	default:
	}
	<-g.gate
	return g.Service.Offline(ctx, id, conn)
}

// failingOffline fails the first failures Offlines, as a store out of reach would.
type failingOffline struct {
	handler.Service
	failures *atomic.Int32
}

func (f failingOffline) Offline(ctx context.Context, id kernel.CharacterID, conn string) error {
	if f.failures.Add(-1) >= 0 {
		return errors.New("store unreachable")
	}
	return f.Service.Offline(ctx, id, conn)
}

// A sweep cut short by a failing store tries again, as a gateway gone for good never resubscribes to finish it.
func TestASweepCutShortTriesAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		failures := &atomic.Int32{}
		failures.Store(3)
		s := newStackOver(t, func(svc handler.Service) handler.Service { return failingOffline{svc, failures} })
		ctxA, endA := context.WithCancel(s.ctx)
		s.gatewayUntil(t, ctxA, "gw-a")
		ann, bo := kernel.NewCharacterID(), kernel.NewCharacterID()
		s.online(t, "gw-a", ann, "Ann")
		s.online(t, "gw-a", bo, "Bo")
		s.party(t, ann, bo)
		endA()
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if s.isOnline(t, ann) || s.isOnline(t, bo) {
			t.Fatalf("Ann online %v, Bo online %v, two minutes after their gateway went", s.isOnline(t, ann), s.isOnline(t, bo))
		}
	})
}

// panickyOffline panics taking anyone offline.
type panickyOffline struct{ handler.Service }

func (panickyOffline) Offline(context.Context, kernel.CharacterID, string) error {
	panic("store corrupt")
}

// countedPanics counts recorded panics by flow.
type countedPanics struct{ n atomic.Int32 }

func (c *countedPanics) Inc(flow string) {
	if flow == panics.FlowJob {
		c.n.Add(1)
	}
}

// A sweep runs on a timer, outside any call, so its panic is recorded there rather than ending the process.
func TestASweepsPanicIsRecorded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		count := &countedPanics{}
		s := newStackRecording(t, func(svc handler.Service) handler.Service { return panickyOffline{svc} }, panics.NewRecorder(slog.New(slog.DiscardHandler), count))
		ctxA, endA := context.WithCancel(s.ctx)
		s.gatewayUntil(t, ctxA, "gw-a")
		s.online(t, "gw-a", kernel.NewCharacterID(), "Ann")
		endA()
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if count.n.Load() != 1 {
			t.Fatalf("%d sweep panics recorded, want 1", count.n.Load())
		}
	})
}

// A gateway that comes back while its players are being taken offline keeps each one it registers again.
func TestAGatewayBackMidSweepKeepsThePlayersItRegistersAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate, reached := make(chan struct{}), make(chan struct{}, 1)
		s := newStackOver(t, func(svc handler.Service) handler.Service { return gatedOffline{svc, gate, reached} })
		ctxA, endA := context.WithCancel(s.ctx)
		s.gatewayUntil(t, ctxA, "gw-a")
		ann, bo := kernel.NewCharacterID(), kernel.NewCharacterID()
		s.online(t, "gw-a", ann, "Ann")
		s.online(t, "gw-a", bo, "Bo")
		s.party(t, ann, bo)
		endA()
		<-reached // the sweep is taking its first player offline
		s.gateway(t, "gw-a")
		again := make(chan error, 2)
		for id, name := range map[kernel.CharacterID]string{ann: "Ann", bo: "Bo"} {
			go func() {
				member := &socialv1.Member{Id: id.String(), Name: name, Level: 3, Hp: 90, MaxHp: 100, MapId: "forest"}
				_, err := s.c.Online(s.ctx, &socialv1.OnlineRequest{GatewayId: "gw-a", ConnectionId: "conn-" + name, Member: member, JoinSeq: s.join()})
				again <- err
			}()
		}
		synctest.Wait()
		close(gate)
		for range 2 {
			if err := <-again; err != nil {
				t.Fatal(err)
			}
		}
		synctest.Wait()
		if !s.isOnline(t, ann) || !s.isOnline(t, bo) {
			t.Fatalf("Ann online %v, Bo online %v, after their gateway registered them again", s.isOnline(t, ann), s.isOnline(t, bo))
		}
	})
}

// An announcement goes once to each gateway anyone is online through, however many are on it, and to none with nobody online; one of no kind is refused.
func TestAnAnnouncementGoesOnceToEachGatewayWithPlayers(t *testing.T) {
	s := newStack(t)
	gwA, gwB, gwC := s.gateway(t, "gw-a"), s.gateway(t, "gw-b"), s.gateway(t, "gw-c")
	ann, cy, bo, di := kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID()
	s.online(t, "gw-a", ann, "Ann")
	s.online(t, "gw-a", cy, "Cy")
	s.online(t, "gw-b", bo, "Bo")
	recv(t, gwA)
	recv(t, gwA)
	recv(t, gwB)
	news := &socialv1.Announcement{Kind: &socialv1.Announcement_BossAppeared{BossAppeared: &socialv1.BossAppeared{Boss: "king_slime", MapId: "forest"}}}
	if _, err := s.c.Announce(s.ctx, &socialv1.AnnounceRequest{Announcement: news}); err != nil {
		t.Fatal(err)
	}
	for name, stream := range map[string]grpc.ServerStreamingClient[socialv1.DeliverResponse]{"gw-a": gwA, "gw-b": gwB} {
		m := recv(t, stream)
		if b := m.GetAnnouncement().GetBossAppeared(); m.GetCharacterId() != "" || b.GetBoss() != "king_slime" || b.GetMapId() != "forest" {
			t.Fatalf("%s heard %+v; want the King Slime notice, for no one character", name, m)
		}
	}
	// Once only: the next thing on gw-a is an invite to Ann, and gw-c, with nobody online then, heard nothing before Di's party.
	if _, err := s.c.Invite(s.ctx, &socialv1.InviteRequest{CharacterId: bo.String(), TargetId: ann.String()}); err != nil {
		t.Fatal(err)
	}
	if m := recv(t, gwA); m.GetCharacterId() != ann.String() || m.GetPartyInvited() == nil {
		t.Fatalf("next on gw-a %+v; want Bo's invite to Ann, the notice sent once", m)
	}
	s.online(t, "gw-c", di, "Di")
	if m := recv(t, gwC); m.GetCharacterId() != di.String() || m.GetPartyState() == nil {
		t.Fatalf("first on gw-c %+v; want Di's party, no notice", m)
	}
	if _, err := s.c.Announce(s.ctx, &socialv1.AnnounceRequest{Announcement: &socialv1.Announcement{}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("an announcement of no kind: %v, want InvalidArgument", err)
	}
}
