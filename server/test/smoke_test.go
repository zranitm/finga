package e2e_test

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/protocol"
	"finga/pkg/wsconn"
)

// TestSmoke signs up, enters the world and moves against FINGA_SMOKE_URL; skipped without it.
func TestSmoke(t *testing.T) {
	t.Parallel()
	url := os.Getenv("FINGA_SMOKE_URL")
	if url == "" {
		t.Skip("FINGA_SMOKE_URL not set")
	}
	s := &stack{t: t, url: url, db: (*pgxpool.Pool)(nil)}
	name := "smoke" + time.Now().Format("150405")
	s.mustStatus(s.call("POST", "/api/accounts", "", map[string]string{"username": name, "password": "correct horse"}), http.StatusCreated)
	token := s.call("POST", "/api/sessions", "", map[string]string{"username": name, "password": "correct horse"}).body["token"].(string)
	created := s.call("POST", "/api/characters", token, map[string]string{"name": "S" + name[len(name)-6:]})
	s.mustStatus(created, http.StatusCreated)
	id := created.body["id"].(string)
	p := s.connect(t, id)
	p.send(wsconn.Message{Data: &clientv1.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterId: id}})
	w := p.expect("Welcome").Data.(*clientv1.Welcome)
	if w.GetMapId() != "town" || w.Token == "" {
		t.Fatalf("welcome %+v", w)
	}
	snap := p.expect("Snapshot").Data.(*clientv1.Snapshot)
	var start float64
	for _, v := range snap.Players {
		if v.GetId() == id {
			start = v.X
		}
	}
	p.send(wsconn.Message{Seq: 1, Data: &clientv1.Input{Right: true}})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		v, ok := p.position(id, time.Until(deadline))
		if !ok {
			break
		}
		if v.X > start+50 {
			p.send(wsconn.Message{Seq: 2, Data: &clientv1.Input{}})
			return
		}
	}
	t.Fatal("the character did not move 50 px to the right within 5 s")
}
