package e2e_test

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/protocol"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// TestSmoke signs up, enters the world and moves against FINGA_SMOKE_URL; skipped without it.
func TestSmoke(t *testing.T) {
	t.Parallel()
	url := os.Getenv("FINGA_SMOKE_URL")
	if url == "" {
		t.Skip("FINGA_SMOKE_URL not set")
	}
	s := &e2etest.Stack{T: t, URL: url, DB: (*pgxpool.Pool)(nil)}
	name := "smoke" + time.Now().Format("150405")
	s.MustStatus(s.Call("POST", "/api/accounts", "", map[string]string{"username": name, "password": "correct horse"}), http.StatusCreated)
	token := s.Call("POST", "/api/sessions", "", map[string]string{"username": name, "password": "correct horse"}).Body["token"].(string)
	created := s.Call("POST", "/api/characters", token, map[string]string{"name": "S" + name[len(name)-6:]})
	s.MustStatus(created, http.StatusCreated)
	id := created.Body["id"].(string)
	p := s.Connect(t, id)
	p.Send(wsconn.Message{Data: &clientv1.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterId: id}})
	w := p.Expect("Welcome").Data.(*clientv1.Welcome)
	if w.GetMapId() != "willowmere" || w.Token == "" {
		t.Fatalf("welcome %+v", w)
	}
	snap := p.Expect("Snapshot").Data.(*clientv1.Snapshot)
	var start float64
	for _, v := range snap.Players {
		if v.GetId() == id {
			start = v.X
		}
	}
	p.Send(wsconn.Message{Seq: p.NextSeq(), Data: &clientv1.Input{Right: true}})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		v, ok := p.Position(id, time.Until(deadline))
		if !ok {
			break
		}
		if v.X > start+1.5 {
			p.Send(wsconn.Message{Seq: p.NextSeq(), Data: &clientv1.Input{}})
			return
		}
	}
	t.Fatal("the character did not move 1.5 m east within 5 s")
}
