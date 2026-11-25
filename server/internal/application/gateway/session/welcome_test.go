package session_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	"finga/internal/application/gateway/session"
	model "finga/internal/model/gateway"
	"finga/internal/shared/kernel"
)

// missing is the pvp module answering a Welcome's missed matches with missed, or failing with err.
type missing struct {
	noPvp
	missed model.Missed
	err    error
}

func (m missing) Missed(context.Context, kernel.CharacterID) (model.Missed, error) {
	return m.missed, m.err
}

// A Welcome tells the matches that ended while the player was away; a pvp module that can't say fails no login, and its Welcome tells none.
func TestAWelcomeTellsTheMatchesMissedWhileAwayAndALostReadFailsNoLogin(t *testing.T) {
	missed := model.Missed{Matches: []model.MissedMatch{{Mode: "duel", Result: model.MatchLoss, Left: true, Ended: time.UnixMilli(1_764_000_000_000)}}, More: 2}
	for _, tt := range []struct {
		name string
		pvp  missing
		want model.Missed
	}{
		{"told", missing{missed: missed}, missed},
		{"unreadable", missing{missed: missed, err: errors.New("the store is down")}, model.Missed{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := session.NewRegistry(filled(session.Deps{World: world{}, Pvp: tt.pvp, Logger: slog.New(slog.DiscardHandler), Traces: noop.NewTracerProvider()}), session.Options{JoinTimeout: time.Second})
			w, err := r.Welcome(t.Context(), kernel.NewCharacterID(), model.Joined{MapID: "town"})
			if err != nil {
				t.Fatalf("Welcome failed: %v", err)
			}
			if w.MapID != "town" || w.Missed.More != tt.want.More || len(w.Missed.Matches) != len(tt.want.Matches) || (len(w.Missed.Matches) > 0 && w.Missed.Matches[0] != tt.want.Matches[0]) {
				t.Fatalf("Welcome on %q missed %+v; want %+v", w.MapID, w.Missed, tt.want)
			}
		})
	}
}
