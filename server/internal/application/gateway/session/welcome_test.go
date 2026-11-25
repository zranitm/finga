package session_test

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	"finga/internal/application/gateway/session"
	model "finga/internal/model/gateway"
	"finga/internal/shared/kernel"
	"finga/pkg/panics"
)

// missing is the pvp module answering a Welcome's missed matches with missed, or failing with err, or hanging till its ctx ends, and keeping what it's told.
type missing struct {
	noPvp
	missed model.Missed
	err    error
	hang   bool
	mu     sync.Mutex
	told   [][]kernel.MatchID
}

func (m *missing) Missed(ctx context.Context, _ kernel.CharacterID) (model.Missed, error) {
	if m.hang {
		<-ctx.Done()
		return model.Missed{}, ctx.Err()
	}
	return m.missed, m.err
}

func (m *missing) Told(_ context.Context, _ kernel.CharacterID, matches []kernel.MatchID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.told = append(m.told, matches)
	return nil
}

func (m *missing) heard() [][]kernel.MatchID {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.told)
}

// A Welcome tells the matches that ended while the player was away, and marks them told only once it's written, every one it lists or counts, so a Welcome that never goes out leaves them for the next; with none, or a pvp module that can't say or hangs, it marks nothing, and the login goes on within 2 s.
func TestAWelcomeTellsTheMatchesMissedWhileAwayOnceWritten(t *testing.T) {
	shown, older1, older2 := kernel.NewMatchID(), kernel.NewMatchID(), kernel.NewMatchID()
	missed := model.Missed{Matches: []model.MissedMatch{{Match: shown, Mode: "duel", Result: model.MatchLoss, Left: true, Ended: time.UnixMilli(1_764_000_000_000)}}, Older: []kernel.MatchID{older1, older2}}
	for _, tt := range []struct {
		name string
		pvp  *missing
		want model.Missed
		told []kernel.MatchID
	}{
		{"missed", &missing{missed: missed}, missed, []kernel.MatchID{shown, older1, older2}},
		{"none", &missing{}, model.Missed{}, nil},
		{"unreadable", &missing{missed: missed, err: errors.New("the store is down")}, model.Missed{}, nil},
		{"hung", &missing{missed: missed, hang: true}, model.Missed{}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				log := slog.New(slog.DiscardHandler)
				r := session.NewRegistry(filled(session.Deps{World: world{}, Pvp: tt.pvp, Logger: log, Panics: panics.NewRecorder(log, nil), Traces: noop.NewTracerProvider()}), session.Options{JoinTimeout: time.Second})
				start := time.Now()
				w, err := r.Welcome(t.Context(), kernel.NewCharacterID(), model.Joined{MapID: "town"})
				if err != nil {
					t.Fatalf("Welcome failed: %v", err)
				}
				if took := time.Since(start); took > 2*time.Second {
					t.Fatalf("Welcome took %v; want 2 s at most", took)
				}
				if w.MapID != "town" || len(w.Missed.Matches) != len(tt.want.Matches) || (len(w.Missed.Matches) > 0 && w.Missed.Matches[0] != tt.want.Matches[0]) || !slices.Equal(w.Missed.Older, tt.want.Older) {
					t.Fatalf("Welcome on %q missed %+v; want %+v", w.MapID, w.Missed, tt.want)
				}
				synctest.Wait()
				if got := tt.pvp.heard(); len(got) != 0 {
					t.Fatalf("told %v before the Welcome was written; want nothing", got)
				}
				if tt.told == nil {
					if w.Written != nil {
						t.Fatal("a Welcome with nothing to mark has a Written")
					}
					return
				}
				w.Written()
				synctest.Wait()
				if got := tt.pvp.heard(); len(got) != 1 || !slices.Equal(got[0], tt.told) {
					t.Fatalf("told %v once written; want %v", got, tt.told)
				}
			})
		})
	}
}
