package session_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"finga/internal/kernel"
	"finga/services/gateway/internal/application/session"
	"finga/services/gateway/internal/model"
)

type world struct{}

func (world) Join(context.Context, string, string, kernel.AccountID, kernel.CharacterID) (model.Joined, error) {
	return model.Joined{MapID: "town"}, nil
}

func (world) Leave(context.Context, model.Presence) error { return nil }

type outbox struct{}

func (outbox) Welcome(model.Welcome)  {}
func (outbox) Deliver(model.Delivery) {}
func (outbox) End(model.EndReason)    {}

// Pushes and logs read who a session plays while its join sets it: the identity is replaced whole,
// never written in place, so the race detector has nothing to find.
func TestTheIdentityCanBeReadWhileAJoinSetsIt(t *testing.T) {
	r := session.NewRegistry(session.Deps{World: world{}, Logger: slog.New(slog.DiscardHandler), JoinTimeout: time.Second})
	s := r.Open("conn-1", outbox{})
	account, login, character := kernel.NewAccountID(), kernel.NewSessionID(), kernel.NewCharacterID()
	stop, reading := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for first := true; ; first = false {
			_, _, _, _ = s.Character(), s.Player(), s.LoginSession(), s.Presence()
			if first {
				close(reading)
			}
			select {
			case <-stop:
				return
			default:
			}
		}
	})
	<-reading
	j, err := r.Join(context.Background(), s, account, login, character)
	if err != nil {
		t.Fatal(err)
	}
	j.Finish(model.Player{ID: character, Name: "Ayla"}, model.Welcome{})
	close(stop)
	wg.Wait()
	if s.Character() != character || s.Player().Name != "Ayla" || s.LoginSession() != login || s.Presence().Connection != "conn-1" {
		t.Fatalf("after the join: %v %+v %v %+v", s.Character(), s.Player(), s.LoginSession(), s.Presence())
	}
}
