package e2e_test

import (
	"net/http"
	"reflect"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// I2/I4 (pvp.md sections 6 and 10): a character's profile carries their world bar's copy until they save an arena build, which then fights in their match: a skill off it is refused there, one on it goes. Another account's character is not found, a build not the class's or a Beginner's is refused, and a board lists the placed alone. Every wait is for a message, a minute its bound.
func TestAnArenaBuildIsSavedAndFoughtWith(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t,
		e2etest.DataEdit{File: "pvp.yaml", Old: "queue: {bots_after: 15s", New: "queue: {bots_after: 1s"},
		e2etest.DataEdit{File: "pvp.yaml", Old: "rounds: {prep: 10s, countdown: 3s, between: 5s}", New: "rounds: {prep: 500ms, countdown: 500ms, between: 500ms}"},
		e2etest.DataEdit{File: "pvp.yaml", Old: "result: 10s", New: "result: 500ms"},
		e2etest.DataEdit{File: "pvp.yaml", Old: "rounds: {best_of: 3, most: 5, length: 60s}", New: "rounds: {best_of: 1, most: 1, length: 20s}"},
	))
	token, id := s.CreateCharacter(t, "arena", "Arena", "warrior")
	world := []any{"strike", "taunt", "", "", "", "", "", ""}
	s.MustStatus(s.Call("PUT", "/api/characters/"+id+"/skill-bar", token, map[string]any{"slots": world}), http.StatusOK)
	profile := "/api/characters/" + id + "/pvp"
	p := s.Call("GET", profile, token, nil)
	s.MustStatus(p, http.StatusOK)
	duel := map[string]any{"mode": "duel", "shown": float64(1000), "rank": "bronze", "placement_left": float64(5), "wins": float64(0), "losses": float64(0), "draws": float64(0), "place": float64(0)}
	if got := p.Body["standings"].([]any); len(got) != 2 || !reflect.DeepEqual(got[0], duel) {
		t.Fatalf("a new fighter's standings %v; want %v first", got, duel)
	}
	if b := p.Body["build"].(map[string]any); !reflect.DeepEqual(b["skill_bar"], world) {
		t.Fatalf("the build before one's saved %v; want the world bar's copy %v", b, world)
	}

	build := map[string]any{"skill_bar": []any{"strike", "war_cry", "", "", "", "", "", ""}, "traits": map[string]any{"strike": "concussion"}}
	path := "/api/characters/" + id + "/pvp-build"
	s.MustStatus(s.Call("PUT", path, token, build), http.StatusOK)
	if b := s.Call("GET", profile, token, nil).Body["build"]; !reflect.DeepEqual(b, build) {
		t.Fatalf("the profile's build %v; want the saved %v", b, build)
	}
	other, _ := s.CreateCharacter(t, "other", "Other", "warrior")
	for _, r := range []struct {
		method, path string
		body         any
	}{{"PUT", path, build}, {"GET", profile, nil}} {
		if got := s.Call(r.method, r.path, other, r.body); got.Status != http.StatusNotFound || got.Body["code"] != float64(code("CHARACTER_NOT_FOUND")) {
			t.Fatalf("%s %s for another account answered %d %v", r.method, r.path, got.Status, got.Body)
		}
	}
	bad := map[string]any{"skill_bar": []any{"fire_arrow", "", "", "", "", "", "", ""}, "traits": map[string]any{}}
	if got := s.Call("PUT", path, token, bad); got.Status != http.StatusBadRequest || got.Body["code"] != float64(code("BAD_PVP_BUILD")) {
		t.Fatalf("a magician's skill on a warrior's bar answered %d %v", got.Status, got.Body)
	}
	novice, nid := s.CreateCharacter(t, "novice", "Novice", "beginner")
	if got := s.Call("PUT", "/api/characters/"+nid+"/pvp-build", novice, map[string]any{"skill_bar": []any{"strike", "", "", "", "", "", "", ""}, "traits": map[string]any{}}); got.Status != http.StatusConflict || got.Body["code"] != float64(code("PVP_NOT_PROMOTED")) {
		t.Fatalf("a Beginner's build answered %d %v", got.Status, got.Body)
	}
	board := s.Call("GET", "/api/pvp/leaderboard?mode=duel", token, nil)
	if board.Status != http.StatusOK || !reflect.DeepEqual(board.Body["rows"], []any{}) {
		t.Fatalf("the board with no one placed: %d %v", board.Status, board.Body)
	}
	if got := s.Call("GET", "/api/pvp/leaderboard?mode=ffa", token, nil); got.Status != http.StatusBadRequest || got.Body["code"] != float64(code("PVP_UNKNOWN_MODE")) {
		t.Fatalf("a board of no mode answered %d %v", got.Status, got.Body)
	}

	pl, snap := s.Join(t, token, id)
	h := &heard{l: e2etest.NewLooter(pl, snap)}
	ask := func(req string, msg any) (int, *clientv1.Response) {
		t.Helper()
		from := len(h.got)
		h.l.Send(wsconn.Message{ReqID: req, Seq: h.l.Seq, Data: msg})
		k := h.since(from, time.Minute, func(m wsconn.Message) bool { return m.Type == "Response" && m.ReqID == req })
		if k < 0 {
			t.Fatalf("%s: no Response", req)
		}
		return from, h.got[k].Data.(*clientv1.Response)
	}
	from, r := ask("queue", &clientv1.PvpQueue{Mode: "duel"})
	k := h.since(from, time.Minute, func(m wsconn.Message) bool { return m.Type == "PvpOffer" })
	if !r.GetOk() || k < 0 {
		t.Fatalf("queueing: %+v, then %v", r, h.types(from))
	}
	from, r = ask("accept", &clientv1.PvpAnswer{Offer: h.got[k].Data.(*clientv1.PvpOffer).GetOffer(), Accept: true})
	fight := h.since(from, time.Minute, func(m wsconn.Message) bool {
		st, ok := m.Data.(*clientv1.MatchState)
		return ok && st.GetPhase() == clientv1.MatchPhase_MATCH_PHASE_FIGHT
	})
	if !r.GetOk() || fight < 0 {
		t.Fatalf("accepting: %+v, then %v", r, h.types(from))
	}
	if _, r := ask("off", &clientv1.UseSkill{Skill: "taunt"}); r.GetOk() || r.GetCode() != code("NOT_ON_ARENA_BAR") {
		t.Fatalf("Taunt, on the world bar but off the arena bar, answered %+v", r)
	}
	if _, r := ask("on", &clientv1.UseSkill{Skill: "war_cry"}); !r.GetOk() {
		t.Fatalf("War Cry, on the arena bar, answered %+v", r)
	}
}
