package clients_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/services/world/internal/clients"
	"finga/services/world/internal/model"
)

// watched is the social service's party stream: the test sends its messages and ends it.
type watched struct {
	socialv1.UnimplementedSocialServiceServer
	events chan *socialv1.WatchPartiesResponse
	end    chan struct{}

	mu      sync.Mutex
	worlds  []string
	updates []*socialv1.UpdateMemberRequest
}

func (w *watched) WatchParties(req *socialv1.WatchPartiesRequest, stream grpc.ServerStreamingServer[socialv1.WatchPartiesResponse]) error {
	w.mu.Lock()
	w.worlds = append(w.worlds, req.GetWorldId())
	end := w.end
	w.mu.Unlock()
	for {
		select {
		case m := <-w.events:
			if err := stream.Send(m); err != nil {
				return err
			}
		case <-end:
			return status.Error(codes.Unavailable, "restarting")
		case <-stream.Context().Done():
			return nil
		}
	}
}

func (w *watched) UpdateMember(_ context.Context, req *socialv1.UpdateMemberRequest) (*socialv1.UpdateMemberResponse, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.updates = append(w.updates, req)
	return &socialv1.UpdateMemberResponse{}, nil
}

func newSocial(t *testing.T, w *watched) clients.Social {
	t.Helper()
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	socialv1.RegisterSocialServiceServer(srv, w)
	return clients.NewSocial(socialv1.NewSocialServiceClient(grpctest.Conn(t, srv)), nil)
}

func next(t *testing.T, events <-chan model.PartyEvent) model.PartyEvent {
	t.Helper()
	select {
	case e := <-events:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no party event")
		return nil
	}
}

func TestFollowPartiesTranslatesTheStream(t *testing.T) {
	w := &watched{events: make(chan *socialv1.WatchPartiesResponse), end: make(chan struct{})}
	s := newSocial(t, w)
	events := make(chan model.PartyEvent, 8)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.FollowParties(ctx, "world-1", func(e model.PartyEvent) { events <- e }) }()
	t.Cleanup(func() { cancel(); <-done })

	w.events <- &socialv1.WatchPartiesResponse{Event: &socialv1.WatchPartiesResponse_Subscribed{Subscribed: &socialv1.Subscribed{BootId: "boot-1"}}}
	if e, ok := next(t, events).(model.PartiesSubscribed); !ok {
		t.Fatalf("first event %#v", e)
	}

	ann, bo := kernel.NewCharacterID(), kernel.NewCharacterID()
	w.events <- &socialv1.WatchPartiesResponse{Event: &socialv1.WatchPartiesResponse_Membership{Membership: &socialv1.Membership{
		CharacterIds: []string{ann.String(), "not-an-id", bo.String()},
		Members:      []string{bo.String(), "not-an-id", ann.String()},
		PartyId:      "party-1",
	}}}
	m, ok := next(t, events).(model.Membership)
	if !ok || len(m.Characters) != 2 || m.Characters[0] != ann || m.Characters[1] != bo || m.Party.ID != "party-1" || len(m.Party.Members) != 2 || m.Party.Members[0] != bo || m.Party.Members[1] != ann {
		t.Fatalf("membership %#v; an ID that does not parse is left out", m)
	}

	w.mu.Lock()
	old := w.end
	w.end = make(chan struct{})
	w.mu.Unlock()
	close(old)
	lost, ok := next(t, events).(model.PartiesLost)
	if !ok || status.Code(lost.Err) != codes.Unavailable {
		t.Fatalf("after the stream ended: %#v", lost)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		w.mu.Lock()
		worlds := append([]string(nil), w.worlds...)
		w.mu.Unlock()
		if len(worlds) >= 2 {
			if worlds[0] != "world-1" || worlds[1] != "world-1" {
				t.Fatalf("subscribed as %v", worlds)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no new subscription after the break")
		}
	}
}

func TestUpdateMemberOverTheSocialService(t *testing.T) {
	w := &watched{}
	s := newSocial(t, w)
	ann := kernel.NewCharacterID()
	if err := s.UpdateMember(context.Background(), model.MemberUpdate{ID: ann, MapID: "cave", Level: 5, HP: 40, MaxHP: 120}); err != nil {
		t.Fatal(err)
	}
	if u := w.updates[0]; u.GetCharacterId() != ann.String() || u.GetMapId() != "cave" || u.GetLevel() != 5 || u.GetHp() != 40 || u.GetMaxHp() != 120 {
		t.Fatalf("update %+v", u)
	}
	if err := clients.NewSocial(socialv1.NewSocialServiceClient(grpctest.Conn(t, grpcx.NewServer(grpcx.ServerOptions{}))), nil).UpdateMember(context.Background(), model.MemberUpdate{ID: ann}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("an update the service does not take: %v", err)
	}
}
