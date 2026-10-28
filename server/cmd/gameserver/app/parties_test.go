package app

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/model"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
)

// watched is the social service's WatchParties and UpdateMember, faked.
type watched struct {
	socialv1.UnimplementedSocialServiceServer
	mu      sync.Mutex
	events  chan *socialv1.WatchPartiesResponse
	end     chan struct{}
	updates []*socialv1.UpdateMemberRequest
	worlds  []string
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

func subscribed(boot string) *socialv1.WatchPartiesResponse {
	return &socialv1.WatchPartiesResponse{Event: &socialv1.WatchPartiesResponse_Subscribed{Subscribed: &socialv1.Subscribed{BootId: boot}}}
}

func membership(of []model.CharacterID, members ...model.CharacterID) *socialv1.WatchPartiesResponse {
	str := func(ids []model.CharacterID) []string {
		out := []string{}
		for _, id := range ids {
			out = append(out, id.String())
		}
		return out
	}
	return &socialv1.WatchPartiesResponse{Event: &socialv1.WatchPartiesResponse_Membership{Membership: &socialv1.Membership{CharacterIds: str(of), Members: str(members)}}}
}

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
	}
}

func TestThePartyCopy(t *testing.T) {
	w := &watched{events: make(chan *socialv1.WatchPartiesResponse, 16), end: make(chan struct{})}
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	socialv1.RegisterSocialServiceServer(srv, w)
	copy := newPartyCopy(socialv1.NewSocialServiceClient(grpctest.Conn(t, srv)), "world-1", slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- copy.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})

	ann, bo, cy := model.NewCharacterID(), model.NewCharacterID(), model.NewCharacterID()
	w.events <- subscribed("boot-1")
	w.events <- membership([]model.CharacterID{ann, bo}, ann, bo)

	t.Run("memberships are read from the copy", func(t *testing.T) {
		eventually(t, func() bool { return copy.SameParty(ann, bo) })
		if got := copy.Members(bo); len(got) != 2 || got[0] != ann {
			t.Fatalf("members %v", got)
		}
		if copy.SameParty(ann, cy) || copy.Members(cy) != nil {
			t.Fatal("cy has a party")
		}
	})
	t.Run("a leaver has none, the others keep theirs", func(t *testing.T) {
		w.events <- membership([]model.CharacterID{bo})
		w.events <- membership([]model.CharacterID{ann}, ann)
		eventually(t, func() bool { return !copy.SameParty(ann, bo) && copy.Members(bo) == nil })
		if got := copy.Members(ann); len(got) != 1 {
			t.Fatalf("members %v", got)
		}
	})
	t.Run("a broken stream clears the copy, and the next subscription rebuilds it", func(t *testing.T) {
		w.events <- membership([]model.CharacterID{ann, cy}, ann, cy)
		eventually(t, func() bool { return copy.SameParty(ann, cy) })
		w.mu.Lock()
		close(w.end)
		w.end = make(chan struct{})
		w.mu.Unlock()
		eventually(t, func() bool { return copy.Members(ann) == nil })
		eventually(t, func() bool { w.mu.Lock(); defer w.mu.Unlock(); return len(w.worlds) == 2 })
		w.events <- subscribed("boot-2")
		w.events <- membership([]model.CharacterID{bo, cy}, bo, cy)
		eventually(t, func() bool { return copy.SameParty(bo, cy) })
		if w.worlds[0] != "world-1" || w.worlds[1] != "world-1" {
			t.Fatalf("worlds %v", w.worlds)
		}
	})
	t.Run("a member update reaches the social service from the worker", func(t *testing.T) {
		copy.Update(ann, "cave", 5, 40, 120)
		eventually(t, func() bool { w.mu.Lock(); defer w.mu.Unlock(); return len(w.updates) == 1 })
		u := w.updates[0]
		if u.GetCharacterId() != ann.String() || u.GetMapId() != "cave" || u.GetLevel() != 5 || u.GetHp() != 40 || u.GetMaxHp() != 120 {
			t.Fatalf("update %+v", u)
		}
	})
}

func TestAFullUpdateQueueDropsWithoutBlocking(t *testing.T) {
	copy := newPartyCopy(nil, "world-1", slog.New(slog.DiscardHandler)) // never Run: nothing drains
	start := time.Now()
	for range partyUpdateQueue + 10 {
		copy.Update(model.NewCharacterID(), "town", 1, 1, 1)
	}
	if time.Since(start) > time.Second {
		t.Fatal("Update blocked")
	}
}
