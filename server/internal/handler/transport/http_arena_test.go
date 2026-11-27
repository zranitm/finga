package transport_test

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"finga/internal/application/pvp"
	model "finga/internal/model/gateway"
	"finga/internal/shared/kernel"
)

// pvp.md section 11: a character's profile, their arena build saved, and a mode's board, each for a signed-in caller, the module's refusals with their codes.
func TestTheArenaEndpoints(t *testing.T) {
	h := newRestHarness()
	id := sampleChar.ID.String()
	t.Run("profile", func(t *testing.T) {
		var asked []any
		h.arena.profile = func(account kernel.AccountID, c kernel.CharacterID) (pvp.Profile, error) {
			asked = []any{account, c}
			return pvp.Profile{
				Standings: []pvp.Standing{{Mode: "duel", Shown: 1240, Rank: "silver", Wins: 6, Losses: 2, Draws: 1, Place: 12}, {Mode: "team", Shown: 1000, Rank: "bronze", PlacementLeft: 5}},
				Build:     pvp.Build{Bar: []string{"strike", "", "", "", "", "", "", ""}},
			}, nil
		}
		rec := h.do("GET", "/api/characters/"+id+"/pvp", "", authed())
		want := `{"standings":[{"mode":"duel","shown":1240,"rank":"silver","placement_left":0,"wins":6,"losses":2,"draws":1,"place":12},{"mode":"team","shown":1000,"rank":"bronze","placement_left":5,"wins":0,"losses":0,"draws":0,"place":0}],"build":{"skill_bar":["strike","","","","","","",""],"traits":{}}}`
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != want || !slices.Equal(asked, []any{accountID, sampleChar.ID}) {
			t.Fatalf("status %d: %s, asked %v", rec.Code, rec.Body.String(), asked)
		}
		h.arena.profile = func(kernel.AccountID, kernel.CharacterID) (pvp.Profile, error) {
			return pvp.Profile{}, fmt.Errorf("%w: another account's", kernel.ErrCharacterNotFound)
		}
		assertError(t, h.do("GET", "/api/characters/"+id+"/pvp", "", authed()), http.StatusNotFound, model.CharacterNotFound)
		assertError(t, h.do("GET", "/api/characters/nope/pvp", "", authed()), http.StatusBadRequest, model.BadID)
	})
	t.Run("build", func(t *testing.T) {
		var got []string
		var gotTraits map[string]string
		h.arena.setBuild = func(account kernel.AccountID, c kernel.CharacterID, bar []string, traits map[string]string) error {
			if account != accountID || c != sampleChar.ID {
				t.Fatalf("saved for %s of %s", c, account)
			}
			got, gotTraits = bar, traits
			return nil
		}
		path := "/api/characters/" + id + "/pvp-build"
		body := `{"skill_bar":["strike","war_cry","","","","","",""],"traits":{"strike":"concussion"}}`
		rec := h.do("PUT", path, body, authed())
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != body || !slices.Equal(got, []string{"strike", "war_cry", "", "", "", "", "", ""}) || !maps.Equal(gotTraits, map[string]string{"strike": "concussion"}) {
			t.Fatalf("status %d: %s; the module got %v with %v", rec.Code, rec.Body.String(), got, gotTraits)
		}
		assertError(t, h.do("PUT", path, `{"skill_bar":"strike"}`, authed()), http.StatusBadRequest, model.MalformedBody)
		assertError(t, h.do("PUT", path, `{"skill_bar":[],"bar":[]}`, authed()), http.StatusBadRequest, model.MalformedBody)
		h.arena.setBuild = func(kernel.AccountID, kernel.CharacterID, []string, map[string]string) error {
			return fmt.Errorf("%w: seven keys", kernel.ErrBadPvpBuild)
		}
		assertError(t, h.do("PUT", path, body, authed()), http.StatusBadRequest, model.BadPvpBuild)
	})
	t.Run("leaderboard", func(t *testing.T) {
		ayla := kernel.NewCharacterID()
		h.arena.board = func(mode string) ([]pvp.Row, error) {
			if mode != "team" {
				return nil, fmt.Errorf("%w: %s", kernel.ErrPvpUnknownMode, mode)
			}
			return []pvp.Row{{Place: 1, Character: ayla, Name: "Ayla", Class: "warrior", Shown: 1650, Rank: "platinum", Wins: 9, Losses: 1}}, nil
		}
		rec := h.do("GET", "/api/pvp/leaderboard?mode=team", "", authed())
		want := fmt.Sprintf(`{"mode":"team","rows":[{"place":1,"character_id":"%s","name":"Ayla","class":"warrior","shown":1650,"rank":"platinum","wins":9,"losses":1,"draws":0}]}`, ayla)
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != want {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		assertError(t, h.do("GET", "/api/pvp/leaderboard", "", authed()), http.StatusBadRequest, model.MissingField)
		assertError(t, h.do("GET", "/api/pvp/leaderboard?mode=ffa", "", authed()), http.StatusBadRequest, model.PvpUnknownMode)
	})
	for _, r := range [][2]string{{"GET", "/api/characters/" + id + "/pvp"}, {"PUT", "/api/characters/" + id + "/pvp-build"}, {"GET", "/api/pvp/leaderboard?mode=duel"}} {
		assertError(t, h.do(r[0], r[1], "{}", nil), http.StatusUnauthorized, model.Unauthenticated)
	}
}
