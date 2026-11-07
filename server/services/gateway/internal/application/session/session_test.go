package session_test

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

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

// Pushes and logs read who a session plays while its join sets it, so the identity is replaced whole, never in place.
func TestTheIdentityCanBeReadWhileAJoinSetsIt(t *testing.T) {
	r := session.NewRegistry(session.Deps{World: world{}, Logger: slog.New(slog.DiscardHandler), Traces: noop.NewTracerProvider()}, session.Options{JoinTimeout: time.Second})
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

// recordingWorld joins after a pause, recording each join's connection and how many ran at once; held ones wait.
type recordingWorld struct {
	mu            sync.Mutex
	held          map[string]chan struct{}
	conns         []string
	running, most int
}

func (w *recordingWorld) Join(ctx context.Context, _, conn string, _ kernel.AccountID, _ kernel.CharacterID) (model.Joined, error) {
	w.mu.Lock()
	gate := w.held[conn]
	w.running++
	w.most = max(w.most, w.running)
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.running--
		w.conns = append(w.conns, conn)
		w.mu.Unlock()
	}()
	time.Sleep(10 * time.Millisecond)
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return model.Joined{}, ctx.Err()
		}
	}
	return model.Joined{MapID: "town"}, nil
}

func (*recordingWorld) Leave(context.Context, model.Presence) error { return nil }

func (w *recordingWorld) hold(conn string) chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.held == nil {
		w.held = map[string]chan struct{}{}
	}
	w.held[conn] = make(chan struct{})
	return w.held[conn]
}

func (w *recordingWorld) joined() (conns []string, most int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.conns...), w.most
}

func play(t *testing.T, r *session.Registry, conn string, character kernel.CharacterID) *session.Session {
	t.Helper()
	s := r.Open(conn, outbox{})
	j, err := r.Join(t.Context(), s, kernel.NewAccountID(), kernel.NewSessionID(), character)
	if err != nil {
		t.Fatal(err)
	}
	j.Finish(model.Player{ID: character}, model.Welcome{})
	return s
}

// social records the registrations that reach the social service.
type social struct {
	mu    sync.Mutex
	calls []string
}

func (s *social) Online(_ context.Context, _, conn string, _ model.Player, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "online "+conn)
	return nil
}

func (s *social) Offline(_ context.Context, _ kernel.CharacterID, conn string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "offline "+conn)
	return nil
}

func (s *social) list() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

// A registration again that comes after the connection closed sends nothing: the close's Offline has the last word.
func TestAClosedConnectionIsNotRegisteredAgain(t *testing.T) {
	soc := &social{}
	r := session.NewRegistry(session.Deps{World: world{}, Social: soc, Logger: slog.New(slog.DiscardHandler), Traces: noop.NewTracerProvider()}, session.Options{JoinTimeout: time.Second})
	s := play(t, r, "c-1", kernel.NewCharacterID())
	r.Close(s)
	r.Online(t.Context(), s)
	if calls := soc.list(); !slices.Equal(calls, []string{"offline c-1"}) {
		t.Fatalf("calls %v, want only the close's Offline", calls)
	}
}

// A registration again of a tab whose character a newer login took sends nothing, so it can't pull the player back to the old tab.
func TestATabThatLostItsCharacterIsNotRegisteredAgain(t *testing.T) {
	soc := &social{}
	r := session.NewRegistry(session.Deps{World: world{}, Social: soc, Logger: slog.New(slog.DiscardHandler), Traces: noop.NewTracerProvider()}, session.Options{JoinTimeout: time.Second})
	character := kernel.NewCharacterID()
	older := play(t, r, "c-1", character)
	play(t, r, "c-2", character)
	r.Online(t.Context(), older)
	if calls := soc.list(); len(calls) != 0 {
		t.Fatalf("calls %v; want none for the tab that lost its character", calls)
	}
}

// After the world's stream breaks every player rejoins, a few at a time, so a restarted world is not hit all at once.
func TestRejoinsRunAFewAtATime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := &recordingWorld{}
		r := session.NewRegistry(session.Deps{World: w, Logger: slog.New(slog.DiscardHandler), Traces: noop.NewTracerProvider()}, session.Options{JoinTimeout: time.Minute})
		r.WorldSubscribed()
		for i := range 100 {
			play(t, r, fmt.Sprint("c-", i), kernel.NewCharacterID())
		}
		_, active := r.WorldSubscribed()
		for _, s := range active {
			r.Rejoin(s)
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if conns, most := w.joined(); len(conns) != 200 || most > 32 {
			t.Fatalf("%d joins, %d at once; want 100 rejoins, at most 32 at once", len(conns)-100, most)
		}
	})
}

// A rejoin in flight when the player reconnects cannot undo the newer join, and one that starts after it does not join.
func TestANewerJoinOutlastsAStaleRejoin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := &recordingWorld{}
		r := session.NewRegistry(session.Deps{World: w, Logger: slog.New(slog.DiscardHandler), Traces: noop.NewTracerProvider()}, session.Options{JoinTimeout: time.Minute})
		r.WorldSubscribed()
		ayla := kernel.NewCharacterID()
		old := play(t, r, "c-old", ayla)
		release := w.hold("c-old")
		_, active := r.WorldSubscribed()
		r.Rejoin(active[0])
		time.Sleep(time.Second) // the rejoin is in the world, held there
		joined := make(chan struct{})
		go func() { defer close(joined); play(t, r, "c-new", ayla) }()
		synctest.Wait()
		close(release)
		<-joined
		r.Rejoin(old) // a rejoin the old connection's last break asked for
		time.Sleep(time.Second)
		synctest.Wait()
		if conns, _ := w.joined(); !slices.Equal(conns, []string{"c-old", "c-old", "c-new"}) {
			t.Fatalf("joins %v; the newer connection's must be the last", conns)
		}
	})
}

// A login that fails after its join gives the character back to the older tab, but not to one that closed meanwhile.
func TestAFailedLoginGivesTheCharacterBackOnlyToAnOpenTab(t *testing.T) {
	for _, closed := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			r := session.NewRegistry(session.Deps{World: world{}, Social: &social{}, Logger: slog.New(slog.DiscardHandler), Traces: noop.NewTracerProvider()}, session.Options{JoinTimeout: time.Second})
			character := kernel.NewCharacterID()
			older := play(t, r, "c-1", character)
			j, err := r.Join(t.Context(), r.Open("c-2", outbox{}), kernel.NewAccountID(), kernel.NewSessionID(), character)
			if err != nil {
				t.Fatal(err)
			}
			if closed {
				r.Close(older)
			}
			j.Release()
			want := older
			if closed {
				want = nil
			}
			if got := r.ByCharacter(character); got != want {
				t.Fatalf("older closed %v: the character's session is %v, want %v", closed, got, want)
			}
			time.Sleep(time.Second) // the open tab's rejoin
		})
	}
}

// Rejoins asked for together, as a timed-out Hello's and the world's Detached are, join once.
func TestRejoinsAskedForTogetherJoinOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := &recordingWorld{}
		r := session.NewRegistry(session.Deps{World: w, Logger: slog.New(slog.DiscardHandler), Traces: noop.NewTracerProvider()}, session.Options{JoinTimeout: time.Minute})
		r.WorldSubscribed()
		s := play(t, r, "c-1", kernel.NewCharacterID())
		r.Rejoin(s)
		r.Rejoin(s)
		time.Sleep(time.Second)
		synctest.Wait()
		r.Rejoin(s) // a later break
		time.Sleep(time.Second)
		synctest.Wait()
		if conns, _ := w.joined(); len(conns) != 3 {
			t.Fatalf("joins %v; want the login's, one for the two rejoins asked together, and one for the later", conns)
		}
	})
}
