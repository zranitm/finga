package party_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/kernel"
	"finga/pkg/panics"
	"finga/services/world/internal/application/party"
	"finga/services/world/internal/model"
)

var errUnavailable = errors.New("unavailable")

// fakeSocial is the social service's party stream, fed by the test, and where member updates go.
type fakeSocial struct {
	events chan model.PartyEvent

	mu       sync.Mutex
	worlds   []string
	updates  []model.MemberUpdate
	failures int           // the first failures updates fail
	refused  error         // when set, every update fails with it
	panics   bool          // an update panics
	gate     chan struct{} // when set, every update waits for it to close
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
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refused != nil {
		f.updates = append(f.updates, u)
		return f.refused
	}
	if f.panics {
		panic("bad update")
	}
	f.updates = append(f.updates, u)
	if len(f.updates) <= f.failures {
		return errUnavailable
	}
	return nil
}

func (f *fakeSocial) Retryable(err error) bool { return errors.Is(err, errUnavailable) }

func (f *fakeSocial) sent() []model.MemberUpdate {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]model.MemberUpdate(nil), f.updates...)
}

// runCopy runs a party copy over social in the bubble until the test ends.
func runCopy(t *testing.T, social *fakeSocial, log *slog.Logger) *party.Copy {
	t.Helper()
	p := party.NewCopy(party.Deps{Social: social, Logger: log}, party.Options{World: "world-1"})
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
		social.events <- model.Membership{Characters: []kernel.CharacterID{ann, bo}, Party: model.Party{ID: "party-1", Members: []kernel.CharacterID{ann, bo}}}
		synctest.Wait() // applied
		if p.Epoch() != 1 {
			t.Fatalf("epoch %d after the first subscription", p.Epoch())
		}

		{ // memberships are read from the copy, as this world subscribed
			if !slices.Contains(p.Party(ann).Members, bo) {
				t.Fatal("ann and bo are not in one party")
			}
			if got := p.Party(bo); got.ID != "party-1" || len(got.Members) != 2 || got.Members[0] != ann {
				t.Fatalf("members %v", got)
			}
			if slices.Contains(p.Party(ann).Members, cy) || p.Party(cy).Members != nil {
				t.Fatal("cy has a party")
			}
			if social.worlds[0] != "world-1" {
				t.Fatalf("followed as %v", social.worlds)
			}
		}
		{ // a leaver has none, the others keep theirs
			social.events <- model.Membership{Characters: []kernel.CharacterID{bo}}
			social.events <- model.Membership{Characters: []kernel.CharacterID{ann}, Party: model.Party{ID: "party-2", Members: []kernel.CharacterID{ann}}}
			synctest.Wait()
			if slices.Contains(p.Party(ann).Members, bo) || p.Party(bo).Members != nil || len(p.Party(ann).Members) != 1 {
				t.Fatalf("after bo left: ann %v, bo %v", p.Party(ann).Members, p.Party(bo).Members)
			}
		}
		{ // a lost stream clears the copy, and is logged; the next subscription rebuilds it
			social.events <- model.Membership{Characters: []kernel.CharacterID{ann, cy}, Party: model.Party{ID: "party-3", Members: []kernel.CharacterID{ann, cy}}}
			social.events <- model.PartiesLost{Err: errUnavailable}
			synctest.Wait()
			if p.Party(ann).Members != nil || p.Party(cy).Members != nil {
				t.Fatal("a party outlived the lost stream")
			}
			if !strings.Contains(logs.String(), `"msg":"party stream broke; nobody has a party until it is back"`) {
				t.Fatalf("log %q", logs.String())
			}
			social.events <- model.Membership{Characters: []kernel.CharacterID{ann}, Party: model.Party{ID: "party-4", Members: []kernel.CharacterID{ann, bo}}}
			social.events <- model.PartiesSubscribed{}
			social.events <- model.Membership{Characters: []kernel.CharacterID{bo, cy}, Party: model.Party{ID: "party-5", Members: []kernel.CharacterID{bo, cy}}}
			synctest.Wait()
			if !slices.Contains(p.Party(bo).Members, cy) || p.Party(ann).Members != nil {
				t.Fatalf("after the new subscription: ann %v, bo %v", p.Party(ann).Members, p.Party(bo).Members)
			}
			if p.Epoch() != 2 {
				t.Fatalf("epoch %d after the second subscription; the maps tell social their players again on each", p.Epoch())
			}
		}
		{ // a copy handed out is the caller's
			got := p.Party(bo).Members
			got[0] = kernel.NewCharacterID()
			if p.Party(bo).Members[0] != bo {
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
	t.Run("one that may pass is retried behind the others, and logged", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			social := newFakeSocial()
			social.failures = 1
			social.gate = make(chan struct{})
			var logs bytes.Buffer
			p := runCopy(t, social, slog.New(slog.NewJSONHandler(&logs, nil)))
			ann, bo := kernel.NewCharacterID(), kernel.NewCharacterID()
			// Ann's is taken and held at the gate before Bo's is put, so Bo's is waiting when Ann's fails:
			// put back to back, the sender could take and fail Ann's first, retrying it before Bo's existed.
			p.Update(ann, "cave", 5, 40, 120)
			synctest.Wait()
			p.Update(bo, "town", 3, 60, 60)
			close(social.gate)
			time.Sleep(time.Second)
			synctest.Wait()
			got := social.sent()
			if len(got) != 3 || got[1].ID != bo || got[2] != (model.MemberUpdate{ID: ann, MapID: "cave", Level: 5, HP: 40, MaxHP: 120}) {
				t.Fatalf("updates %+v, want Ann's again after Bo's", got)
			}
			if !strings.Contains(logs.String(), `"msg":"member update failed; retrying"`) || !strings.Contains(logs.String(), ann.String()) {
				t.Fatalf("log %q", logs.String())
			}
		})
	})
	t.Run("one refused for good is logged and not retried", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			social := newFakeSocial()
			social.refused = errors.New("bad member")
			var logs bytes.Buffer
			p := runCopy(t, social, slog.New(slog.NewJSONHandler(&logs, nil)))
			p.Update(kernel.NewCharacterID(), "cave", 5, 40, 120)
			time.Sleep(time.Minute)
			synctest.Wait()
			if len(social.sent()) != 1 || !strings.Contains(logs.String(), `"msg":"member update failed"`) {
				t.Fatalf("updates %+v, log %q", social.sent(), logs.String())
			}
		})
	})
	// A level-up's update must not be lost to a busy social service: nothing else corrects an idle member's frame.
	t.Run("none is dropped, and a member's newest goes out", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			social := newFakeSocial()
			social.gate = make(chan struct{})
			p := runCopy(t, social, slog.New(slog.DiscardHandler))
			ann := kernel.NewCharacterID()
			p.Update(ann, "cave", 5, 40, 120) // taken, and held at the gate
			synctest.Wait()
			others := map[kernel.CharacterID]bool{}
			for range 2000 {
				id := kernel.NewCharacterID()
				others[id] = true
				p.Update(id, "town", 1, 1, 1)
			}
			p.Update(ann, "cave", 6, 30, 130)
			p.Update(ann, "cave", 6, 20, 130)
			start := time.Now()
			close(social.gate)
			synctest.Wait()
			if time.Since(start) != 0 {
				t.Fatal("sending waited on the clock")
			}
			var anns []model.MemberUpdate
			for _, u := range social.sent() {
				if u.ID == ann {
					anns = append(anns, u)
				}
				delete(others, u.ID)
			}
			if len(others) != 0 || len(anns) != 2 || anns[1] != (model.MemberUpdate{ID: ann, MapID: "cave", Level: 6, HP: 20, MaxHP: 130}) {
				t.Fatalf("%d members never sent; Ann's %+v", len(others), anns)
			}
		})
	})
	t.Run("Update never blocks", func(t *testing.T) {
		p := party.NewCopy(party.Deps{Social: newFakeSocial(), Logger: slog.New(slog.DiscardHandler)}, party.Options{World: "world-1"}) // never Run: nothing drains
		start := time.Now()
		for range 4096 {
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
			p := party.NewCopy(party.Deps{Social: social, Logger: slog.New(slog.DiscardHandler)}, party.Options{World: "world-1"})
			p.Update(kernel.NewCharacterID(), "town", 1, 10, 10)
			if e, ok := errors.AsType[*panics.Error](p.Run(context.Background())); !ok || e.Value != "bad update" {
				t.Fatal("Run kept going after its update loop panicked")
			}
		})
	})
}
