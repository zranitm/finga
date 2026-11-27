package account_test

import (
	"context"
	"errors"
	"finga/internal/application/account"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
	"finga/pkg/panics"
)

// missing is the pvp module answering a Welcome's missed matches with missed, or failing with err, or hanging till its ctx ends, and keeping what it's told.
type missing struct {
	noPvp
	missed model.MissedMatches
	err    error
	hang   bool
	mu     sync.Mutex
	told   [][]kernel.MatchID
}

func (m *missing) Missed(ctx context.Context, _ kernel.CharacterID) (model.MissedMatches, error) {
	if m.hang {
		<-ctx.Done()
		return model.MissedMatches{}, ctx.Err()
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
	shownDetails := "the pvp module's account of the matches"
	missed := model.MissedMatches{Told: []kernel.MatchID{shown, older1, older2}, Details: shownDetails}
	for _, tt := range []struct {
		name string
		pvp  *missing
		want any
		told []kernel.MatchID
	}{
		{"missed", &missing{missed: missed}, shownDetails, []kernel.MatchID{shown, older1, older2}},
		{"none", &missing{}, nil, nil},
		{"unreadable", &missing{missed: missed, err: errors.New("the store is down")}, nil, nil},
		{"hung", &missing{missed: missed, hang: true}, nil, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				log := slog.New(slog.DiscardHandler)
				r := account.NewConnections(filled(account.ConnectionsDeps{World: world{}, Pvp: tt.pvp, Logger: log, Panics: panics.NewRecorder(log, nil), Traces: noop.NewTracerProvider()}), account.ConnectionsOptions{JoinTimeout: time.Second})
				start := time.Now()
				w, err := r.Welcome(t.Context(), kernel.NewCharacterID(), model.Joined{MapID: "town"})
				if err != nil {
					t.Fatalf("Welcome failed: %v", err)
				}
				if took := time.Since(start); took > 2*time.Second {
					t.Fatalf("Welcome took %v; want 2 s at most", took)
				}
				if w.MapID != "town" || w.Missed != tt.want {
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
