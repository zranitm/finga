package handler_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/sentinels"
	"finga/services/social/internal/application"
	"finga/services/social/internal/handler"
)

type stack struct {
	c   socialv1.SocialServiceClient
	ctx context.Context
}

func newStack(t *testing.T) *stack {
	t.Helper()
	now := time.Unix(1_700_000_000, 0)
	parties := application.NewPartyService(application.PartyOptions{Clock: func() time.Time { return now }})
	srv := handler.New(parties, application.NewChatService(parties, application.ChatOptions{Clock: func() time.Time { return now }}), "boot-1")
	parties.Notify(srv)
	g := grpcx.NewServer(grpcx.ServerOptions{Errors: handler.Errors, Logger: slog.New(slog.DiscardHandler)})
	socialv1.RegisterSocialServiceServer(g, srv)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &stack{c: socialv1.NewSocialServiceClient(grpctest.Conn(t, g)), ctx: ctx}
}

// gateway subscribes to Deliver as one gateway node.
func (s *stack) gateway(t *testing.T, id string) grpc.ServerStreamingClient[socialv1.DeliverResponse] {
	t.Helper()
	stream, err := s.c.Deliver(s.ctx, &socialv1.DeliverRequest{GatewayId: id})
	if err != nil {
		t.Fatal(err)
	}
	if m, err := stream.Recv(); err != nil || m.GetSubscribed().GetBootId() != "boot-1" {
		t.Fatalf("first %+v %v", m, err)
	}
	return stream
}

func (s *stack) online(t *testing.T, gateway string, id kernel.CharacterID, name string) {
	t.Helper()
	if _, err := s.c.Online(s.ctx, &socialv1.OnlineRequest{GatewayId: gateway, Member: &socialv1.Member{Id: id.String(), Name: name, Level: 3, Hp: 90, MaxHp: 100, MapId: "forest"}}); err != nil {
		t.Fatal(err)
	}
}

// party makes leader and member a party through an invite and its acceptance.
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
		if m.GetCharacterId() != who.String() || p.GetLeaderId() != ann.String() || len(p.GetMembers()) != 2 || p.GetMembers()[1].GetName() != "Bo" || p.GetMembers()[1].GetMapId() != "forest" {
			t.Fatalf("state for %s: %+v", who, m)
		}
	}

	t.Run("a member's update reaches the party", func(t *testing.T) {
		if _, err := s.c.UpdateMember(s.ctx, &socialv1.UpdateMemberRequest{CharacterId: bo.String(), MapId: "cave", Level: 4, Hp: 50, MaxHp: 120}); err != nil {
			t.Fatal(err)
		}
		mb := recv(t, gwA).GetPartyState().GetParty().GetMembers()[1]
		if mb.GetMapId() != "cave" || mb.GetLevel() != 4 || mb.GetHp() != 50 || mb.GetMaxHp() != 120 {
			t.Fatalf("member %+v", mb)
		}
		recv(t, gwB)
	})
	t.Run("PartyOf", func(t *testing.T) {
		res, err := s.c.PartyOf(s.ctx, &socialv1.PartyOfRequest{CharacterId: bo.String()})
		if err != nil || res.GetParty().GetLeaderId() != ann.String() {
			t.Fatalf("party %+v %v", res, err)
		}
		alone := kernel.NewCharacterID()
		if res, err := s.c.PartyOf(s.ctx, &socialv1.PartyOfRequest{CharacterId: alone.String()}); err != nil || res.GetParty() != nil {
			t.Fatalf("no party: %+v %v", res, err)
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
		if _, err := s.c.Offline(s.ctx, &socialv1.OfflineRequest{CharacterId: bo.String()}); err != nil {
			t.Fatal(err)
		}
		// The next message is this PartyState, not a chat line: the map line
		// above delivered nothing.
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
			_, err := s.c.Respond(s.ctx, &socialv1.RespondRequest{CharacterId: bo.String(), InviteId: "nope", Accept: true})
			return err
		}, socialv1.Reason_REASON_INVITE_EXPIRED},
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
	t.Run("the current memberships follow Subscribed", func(t *testing.T) {
		ms := recv(t, stream).GetMembership()
		if len(ms.GetCharacterIds()) != 2 || len(ms.GetMembers()) != 2 {
			t.Fatalf("membership %+v", ms)
		}
	})
	t.Run("then each change", func(t *testing.T) {
		s.party(t, cy, di)
		ms := recv(t, stream).GetMembership()
		if len(ms.GetMembers()) != 2 || (ms.GetMembers()[0] != cy.String() && ms.GetMembers()[1] != cy.String()) {
			t.Fatalf("membership %+v", ms)
		}
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
		if left == nil || left.GetCharacterIds()[0] != di.String() || rest == nil || len(rest.GetMembers()) != 1 || rest.GetMembers()[0] != cy.String() {
			t.Fatalf("left %+v rest %+v", left, rest)
		}
	})
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
	if _, err := s.c.Online(s.ctx, &socialv1.OnlineRequest{Member: &socialv1.Member{Id: kernel.NewCharacterID().String()}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("online without a gateway: %v", err)
	}
}

func TestTheTableCoversTheModulesAndTheContract(t *testing.T) {
	declared, err := sentinels.Declared(filepath.Join("..", "model"))
	if err != nil || len(declared) < 2 {
		t.Fatalf("declared %v, err %v", declared, err)
	}
	mapped, err := sentinels.Referenced("errors.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range declared {
		if !mapped[name] {
			t.Errorf("%s has no row in Errors", name)
		}
	}
	used := map[grpcx.Reason]bool{}
	for _, row := range handler.Errors {
		used[grpcx.Key(row.Reason)] = true
	}
	for _, r := range grpcx.Reasons(socialv1.Reason(0).Descriptor()) {
		if !used[r] {
			t.Errorf("%s has no row in Errors", r)
		}
	}
}
