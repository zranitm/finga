package application_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/kernel"
	"finga/pkg/panics"
	"finga/services/world/internal/application"
	"finga/services/world/internal/model"
)

// fakeSocial is the social service's party stream, fed by the test, and where member updates go.
type fakeSocial struct {
	events chan model.PartyEvent

	mu       sync.Mutex
	worlds   []string
	updates  []model.MemberUpdate
	failures int  // the first failures updates fail
	panics   bool // an update panics
}

func newFakeSocial() *fakeSocial { return &fakeSocial{events: make(chan model.PartyEvent)} }

func (f *fakeSocial) FollowParties(ctx context.Context, world string, on func(model.PartyEvent)) {
	f.mu.Lock()
	f.worlds = append(f.worlds, world)
	f.mu.Unlock()
	for {
		select {
		case e := <-f.events:
			on(e)
		case <-ctx.Done():
			return
		}
	}
}

func (f *fakeSocial) UpdateMember(_ context.Context, u model.MemberUpdate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.panics {
		panic("bad update")
	}
	f.updates = append(f.updates, u)
	if len(f.updates) <= f.failures {
		return errUnavailable
	}
	return nil
}

func (f *fakeSocial) sent() []model.MemberUpdate {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]model.MemberUpdate(nil), f.updates...)
}

// runCopy runs a party copy over social in the bubble until the test ends.
func runCopy(t *testing.T, social *fakeSocial, log *slog.Logger) *application.PartyCopy {
	t.Helper()
	p := application.NewPartyCopy(social, "world-1", log)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return p
}

func TestThePartyCopy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		social := newFakeSocial()
		var logs bytes.Buffer
		p := runCopy(t, social, slog.New(slog.NewJSONHandler(&logs, nil)))
		ann, bo, cy := kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID()
		social.events <- model.PartiesSubscribed{}
		social.events <- model.Membership{Characters: []kernel.CharacterID{ann, bo}, Members: []kernel.CharacterID{ann, bo}}
		synctest.Wait() // applied

		{ // memberships are read from the copy, as this world subscribed
			if !p.SameParty(ann, bo) {
				t.Fatal("ann and bo are not in one party")
			}
			if got := p.Members(bo); len(got) != 2 || got[0] != ann {
				t.Fatalf("members %v", got)
			}
			if p.SameParty(ann, cy) || p.Members(cy) != nil {
				t.Fatal("cy has a party")
			}
			if social.worlds[0] != "world-1" {
				t.Fatalf("followed as %v", social.worlds)
			}
		}
		{ // a leaver has none, the others keep theirs
			social.events <- model.Membership{Characters: []kernel.CharacterID{bo}}
			social.events <- model.Membership{Characters: []kernel.CharacterID{ann}, Members: []kernel.CharacterID{ann}}
			synctest.Wait()
			if p.SameParty(ann, bo) || p.Members(bo) != nil || len(p.Members(ann)) != 1 {
				t.Fatalf("after bo left: ann %v, bo %v", p.Members(ann), p.Members(bo))
			}
		}
		{ // a lost stream clears the copy, and is logged; the next subscription rebuilds it
			social.events <- model.Membership{Characters: []kernel.CharacterID{ann, cy}, Members: []kernel.CharacterID{ann, cy}}
			social.events <- model.PartiesLost{Err: errUnavailable}
			synctest.Wait()
			if p.Members(ann) != nil || p.Members(cy) != nil {
				t.Fatal("a party outlived the lost stream")
			}
			if !strings.Contains(logs.String(), `"msg":"party stream broke; nobody has a party until it is back"`) {
				t.Fatalf("log %q", logs.String())
			}
			social.events <- model.Membership{Characters: []kernel.CharacterID{ann}, Members: []kernel.CharacterID{ann, bo}}
			social.events <- model.PartiesSubscribed{}
			social.events <- model.Membership{Characters: []kernel.CharacterID{bo, cy}, Members: []kernel.CharacterID{bo, cy}}
			synctest.Wait()
			if !p.SameParty(bo, cy) || p.Members(ann) != nil {
				t.Fatalf("after the new subscription: ann %v, bo %v", p.Members(ann), p.Members(bo))
			}
		}
		{ // a copy handed out is the caller's
			got := p.Members(bo)
			got[0] = kernel.NewCharacterID()
			if p.Members(bo)[0] != bo {
				t.Fatal("changing a returned list changed the copy")
			}
		}
	})
}

func TestMemberUpdates(t *testing.T) {
	t.Run("reach the social service from the copy's own goroutine", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			social := newFakeSocial()
			p := runCopy(t, social, slog.New(slog.DiscardHandler))
			ann := kernel.NewCharacterID()
			p.Update(ann, "cave", 5, 40, 120)
			synctest.Wait()
			if got := social.sent(); len(got) != 1 || got[0] != (model.MemberUpdate{ID: ann, MapID: "cave", Level: 5, HP: 40, MaxHP: 120}) {
				t.Fatalf("updates %+v", got)
			}
		})
	})
	t.Run("a failed one is logged, and the next still goes", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			social := newFakeSocial()
			social.failures = 1
			var logs bytes.Buffer
			p := runCopy(t, social, slog.New(slog.NewJSONHandler(&logs, nil)))
			ann := kernel.NewCharacterID()
			p.Update(ann, "cave", 5, 40, 120)
			p.Update(ann, "cave", 5, 30, 120)
			synctest.Wait()
			if len(social.sent()) != 2 || !strings.Contains(logs.String(), `"msg":"member update failed"`) || !strings.Contains(logs.String(), ann.String()) {
				t.Fatalf("updates %+v, log %q", social.sent(), logs.String())
			}
		})
	})
	t.Run("a full queue drops one without blocking", func(t *testing.T) {
		p := application.NewPartyCopy(newFakeSocial(), "world-1", slog.New(slog.DiscardHandler)) // never Run: nothing drains
		start := time.Now()
		for range 1024 + 10 {
			p.Update(kernel.NewCharacterID(), "town", 1, 1, 1)
		}
		if time.Since(start) > time.Second {
			t.Fatal("Update blocked")
		}
	})
	t.Run("a panic sending one stops the copy", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			social := newFakeSocial()
			social.panics = true
			p := application.NewPartyCopy(social, "world-1", slog.New(slog.DiscardHandler))
			p.Update(kernel.NewCharacterID(), "town", 1, 10, 10)
			if e, ok := errors.AsType[*panics.Error](p.Run(context.Background())); !ok || e.Value != "bad update" {
				t.Fatal("Run kept going after its update loop panicked")
			}
		})
	})
}
