// Package e2e runs the whole game, built as production builds it, against a real Postgres.
package e2e_test

import (
	"net/http"
	"os"
	"testing"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/dependencies"
	"finga/internal/shared/protocol"
	"finga/pkg/runner/runnertest"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

func TestMain(m *testing.M) {
	var err error
	if binDir, err = os.MkdirTemp("", "finga-e2e-bin"); err != nil {
		panic(err)
	}
	code := e2etest.Main(m)
	_ = os.RemoveAll(binDir)
	os.Exit(code)
}

func newStack(t *testing.T) *e2etest.Stack {
	t.Helper()
	return newStackOver(t, "../data")
}

// newStackOver runs cmd/finga's app over the game data in data.
func newStackOver(t *testing.T, data string) *e2etest.Stack {
	t.Helper()
	return e2etest.NewStackBuilt(t, data, dependencies.Build)
}

func TestAcceptance(t *testing.T) {
	t.Parallel()
	t.Run("A1/sign-up-and-log-in", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		s.MustStatus(s.Call("POST", "/api/accounts", "", map[string]string{"username": "alice", "password": "correct horse"}), http.StatusCreated)
		s.MustStatus(s.Call("POST", "/api/accounts", "", map[string]string{"username": "ALICE", "password": "correct horse"}), http.StatusConflict)
		login := s.Call("POST", "/api/sessions", "", map[string]string{"username": "alice", "password": "correct horse"})
		s.MustStatus(login, http.StatusCreated)
		token, _ := login.Body["token"].(string)
		if token == "" {
			t.Fatalf("no token: %v", login.Body)
		}
		s.MustStatus(s.Call("GET", "/api/characters", token, nil), http.StatusOK)
		s.MustStatus(s.Call("POST", "/api/sessions", "", map[string]string{"username": "alice", "password": "wrong"}), http.StatusUnauthorized)
		s.MustStatus(s.Call("DELETE", "/api/sessions/current", token, nil), http.StatusNoContent)
		s.MustStatus(s.Call("GET", "/api/characters", token, nil), http.StatusUnauthorized)
	})

	t.Run("A1/hello-rotates-the-token-reuse-ends-the-session-and-logout-everywhere-kicks", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		p := s.Enter(t, "erin", "Erin", "warrior")
		login := s.Call("POST", "/api/sessions", "", map[string]string{"username": "erin", "password": "correct horse"})
		// The second login kicked the first connection and revoked its session.
		p.Expect("Kicked")
		token := login.Body["token"].(string)
		q := s.Connect(t, p.ID)
		q.Send(wsconn.Message{Data: &clientv1.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterId: p.ID}})
		w := q.Expect("Welcome").Data.(*clientv1.Welcome)
		q.Expect("Snapshot")
		if w.Token == "" || w.Token == token {
			t.Fatalf("welcome token %q", w.Token)
		}
		// The rotated token works; the one sent in Hello works once more.
		s.MustStatus(s.Call("GET", "/api/characters", w.Token, nil), http.StatusOK)
		s.MustStatus(s.Call("GET", "/api/characters", token, nil), http.StatusOK)
		// Reusing the retired token ends the session and kills the rotated token too.
		s.MustStatus(s.Call("GET", "/api/characters", token, nil), http.StatusUnauthorized)
		if k := q.Expect("Kicked").Data.(*clientv1.Kicked); k.Reason != clientv1.KickReason_KICK_REASON_SESSION_EXPIRED {
			t.Fatalf("kicked for %s", k.Reason)
		}
		s.MustStatus(s.Call("GET", "/api/characters", w.Token, nil), http.StatusUnauthorized)

		r := s.Enter(t, "fay", "Fay", "bowman")
		s.MustStatus(s.Call("DELETE", "/api/sessions", r.Token, nil), http.StatusNoContent)
		if k := r.Expect("Kicked").Data.(*clientv1.Kicked); k.Reason != clientv1.KickReason_KICK_REASON_SESSION_EXPIRED {
			t.Fatalf("kicked for %s", k.Reason)
		}
		s.MustStatus(s.Call("GET", "/api/characters", r.Token, nil), http.StatusUnauthorized)
	})

	t.Run("A1/throttled-after-repeated-failures", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		s.MustStatus(s.Call("POST", "/api/accounts", "", map[string]string{"username": "bob", "password": "correct horse"}), http.StatusCreated)
		for range 5 {
			s.MustStatus(s.Call("POST", "/api/sessions", "", map[string]string{"username": "bob", "password": "wrong"}), http.StatusUnauthorized)
		}
		r := s.Call("POST", "/api/sessions", "", map[string]string{"username": "bob", "password": "correct horse"})
		s.MustStatus(r, http.StatusTooManyRequests)
		if r.Body["code"] != float64(code("LOGIN_RATE_LIMITED")) {
			t.Fatalf("body %v", r.Body)
		}
	})

	t.Run("A2/create-a-character", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		s.MustStatus(s.Call("POST", "/api/accounts", "", map[string]string{"username": "carol", "password": "correct horse"}), http.StatusCreated)
		login := s.Call("POST", "/api/sessions", "", map[string]string{"username": "carol", "password": "correct horse"})
		token := login.Body["token"].(string)
		created := s.Call("POST", "/api/characters", token, map[string]string{"name": "Ayla"})
		s.MustStatus(created, http.StatusCreated)
		if created.Body["level"] != float64(1) || created.Body["class"] != "beginner" || created.Body["map_id"] != "town" || created.Body["hp"] != float64(124) {
			t.Fatalf("character %v; want a level 1 Beginner in town at full HP", created.Body)
		}
		s.MustStatus(s.Call("POST", "/api/characters", token, map[string]string{"name": "ayla"}), http.StatusConflict)
		// Every character starts a Beginner: a class in the body is refused.
		s.MustStatus(s.Call("POST", "/api/characters", token, map[string]string{"name": "Merlin", "class": "magician"}), http.StatusBadRequest)
		list := s.Call("GET", "/api/characters", token, nil)
		s.MustStatus(list, http.StatusOK)
		chars, _ := list.Body["characters"].([]any)
		if len(chars) != 1 {
			t.Fatalf("characters %v", list.Body)
		}
	})

	t.Run("settings-saved-and-returned", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		s.MustStatus(s.Call("POST", "/api/accounts", "", map[string]string{"username": "dave", "password": "correct horse"}), http.StatusCreated)
		token := s.Call("POST", "/api/sessions", "", map[string]string{"username": "dave", "password": "correct horse"}).Body["token"].(string)
		initial := s.Call("GET", "/api/settings", token, nil)
		s.MustStatus(initial, http.StatusOK)
		if initial.Body["control_preset"] != "classic" {
			t.Fatalf("default settings %v", initial.Body)
		}
		s.MustStatus(s.Call("PUT", "/api/settings", token, map[string]string{"control_preset": "wasd"}), http.StatusOK)
		s.MustStatus(s.Call("PUT", "/api/settings", token, map[string]string{"control_preset": "vim"}), http.StatusBadRequest)
		after := s.Call("GET", "/api/settings", token, nil)
		if after.Body["control_preset"] != "wasd" {
			t.Fatalf("settings after save %v", after.Body)
		}
		token2 := s.Call("POST", "/api/sessions", "", map[string]string{"username": "dave", "password": "correct horse"}).Body["token"].(string)
		if s.Call("GET", "/api/settings", token2, nil).Body["control_preset"] != "wasd" {
			t.Fatal("settings did not survive a new session")
		}
	})

	t.Run("probes/on-the-ops-listener-only", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
			if status, _ := runnertest.Get(t, s.Ops+path); status != http.StatusOK {
				t.Errorf("ops %s: status %d", path, status)
			}
			s.MustStatus(s.Call("GET", path, "", nil), http.StatusNotFound)
		}
	})
}
