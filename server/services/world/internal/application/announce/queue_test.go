package announce_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/services/world/internal/application/announce"
	"finga/services/world/internal/model"
)

// social records what it was asked to announce, failing while down and holding each call until let through.
type social struct {
	mu    sync.Mutex
	sent  []model.Announcement
	down  bool
	held  chan struct{}
	calls int
}

func (s *social) Announce(ctx context.Context, a model.Announcement) error {
	s.mu.Lock()
	s.calls++
	held, down := s.held, s.down
	s.mu.Unlock()
	if held != nil {
		select {
		case <-held:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if down {
		return errors.New("social is down")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, a)
	return nil
}

func (s *social) seen() ([]model.Announcement, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.Announcement(nil), s.sent...), s.calls
}

func running(t *testing.T, s *social) (*announce.Queue, func()) {
	t.Helper()
	q := announce.New(announce.Deps{Social: s, Logger: slog.New(slog.DiscardHandler)})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = q.Run(ctx) }()
	return q, func() { cancel(); <-done }
}

// Announcements go out in turn; while one waits, a newer one of the same news replaces it, and other news keeps its own place.
func TestAnnouncementsGoOutInTurnTheNewestOfEachNews(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &social{held: make(chan struct{})}
		q, stop := running(t, s)
		defer stop()
		forest, cave := model.BossAppeared{Boss: "king_slime", Map: "forest"}, model.BossAppeared{Boss: "vampire_bat", Map: "cave"}
		q.Announce(forest)
		synctest.Wait() // the forest's is in flight, held
		q.Announce(cave)
		q.Announce(model.BossAppeared{Boss: "old_bat", Map: "cave"})
		q.Announce(cave)
		close(s.held)
		synctest.Wait()
		if sent, _ := s.seen(); len(sent) != 2 || sent[0] != forest || sent[1] != cave {
			t.Fatalf("sent %+v; want the forest's, then the cave's newest", sent)
		}
	})
}

// One that fails is dropped, not tried again, and the next still goes; one the social service never answers gives up at its timeout.
func TestAFailedAnnouncementIsDropped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &social{down: true}
		q, stop := running(t, s)
		defer stop()
		q.Announce(model.BossAppeared{Boss: "king_slime", Map: "forest"})
		synctest.Wait()
		if sent, calls := s.seen(); len(sent) != 0 || calls != 1 {
			t.Fatalf("sent %+v in %d calls; want one failed call", sent, calls)
		}
		s.mu.Lock()
		s.down, s.held = false, make(chan struct{}) // never let through: the call must time out
		s.mu.Unlock()
		q.Announce(model.BossAppeared{Boss: "vampire_bat", Map: "cave"})
		time.Sleep(3 * time.Second) // past the call's timeout
		synctest.Wait()
		s.mu.Lock()
		s.held = nil
		s.mu.Unlock()
		q.Announce(model.BossAppeared{Boss: "spore_mother", Map: "shroom_grove"})
		synctest.Wait()
		if sent, calls := s.seen(); len(sent) != 1 || sent[0] != (model.BossAppeared{Boss: "spore_mother", Map: "shroom_grove"}) || calls != 3 {
			t.Fatalf("sent %+v in %d calls; want the grove's after the cave's timed out", sent, calls)
		}
	})
}
