package pvp_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"finga/internal/application/pvp"
	model "finga/internal/model/pvp"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/gamedata/load"
	"finga/internal/shared/kernel"
)

var firstWin = []model.FeatPiece{{Piece: "victors_horns", Feat: "first_pvp_win"}}

// winsTold is every win feat's piece the character was told of, and in how many pushes.
func winsTold(f fixture, id kernel.CharacterID) ([]model.FeatPiece, int) {
	var out []model.FeatPiece
	pushes := 0
	for _, d := range f.notes.of(id) {
		if p, ok := d.(pvp.CorePiecesAdded); ok && len(p.Feats) > 0 {
			out = append(out, p.Feats...)
			pushes++
		}
	}
	return out, pushes
}

// fighter is a fixture's new character, of an account, with no record.
func fighter(f fixture) (kernel.CharacterID, kernel.AccountID) {
	id, account := kernel.NewCharacterID(), kernel.NewAccountID()
	f.chars.of[id] = model.Character{Account: account, Class: "warrior"}
	return id, account
}

// record stores a match of mode won by winner (0 a draw), with ayla on side 1, leaving if left, against a novice bot.
func record(f fixture, mode string, ayla kernel.CharacterID, winner int, left bool) {
	f.svc.Record(context.Background(), model.Result{Match: kernel.NewMatchID(), Mode: mode, Winner: winner, Score: [2]int{1000, 640}, Sides: [2][]model.Outcome{{{Character: ayla, Left: left}}, {{Tier: "novice"}}}})
}

// outfits.md section 4: a win, rated or not, asks for the pieces of each win feat the character's wins over every mode reach; the winner hears of those new to the account, once. A loss, a draw or a win the character left asks for none.
func TestAWinAsksForTheWinFeats(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		ayla, account := fighter(f)
		record(f, "duel", ayla, 2, false)
		record(f, "duel", ayla, 0, false)
		record(f, "duel", ayla, 1, true)
		if f.items.featAsks != 0 {
			t.Fatalf("a loss, a draw and a win left asked %d times: %+v", f.items.featAsks, f.items.featCalls)
		}
		record(f, "battle", ayla, 1, false) // unrated
		if got, pushes := winsTold(f, ayla); !slices.Equal(got, firstWin) || pushes != 1 || !f.items.feats[account]["victors_horns"] {
			t.Fatalf("the first win told of %+v in %d pushes", got, pushes)
		}
		if _, ok := f.notes.of(ayla)[len(f.notes.of(ayla))-2].(pvp.CoreEnded); !ok {
			t.Fatalf("Ayla heard %+v, the outcome not before the piece", f.notes.of(ayla))
		}
		record(f, "duel", ayla, 2, false)
		record(f, "duel", ayla, 0, false)
		record(f, "duel", ayla, 1, true)
		if f.items.featAsks != 1 {
			t.Fatalf("a loss, a draw and a win left after a win asked again: %+v", f.items.featCalls)
		}
		record(f, "duel", ayla, 1, false)
		if got, pushes := winsTold(f, ayla); len(got) != 1 || pushes != 1 || f.items.featAsks != 2 {
			t.Fatalf("a second win told of %+v in %d pushes after %d asks, want the first win's ask again and nothing new", got, pushes, f.items.featAsks)
		}
	})
}

// A grant the wardrobe can't take is tried again; one that never lands tells of nothing, and the next win grants it.
func TestALostWinFeatComesWithTheNextWin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		ayla, _ := fighter(f)
		f.items.featFail = 2
		record(f, "duel", ayla, 1, false)
		if got, _ := winsTold(f, ayla); !slices.Equal(got, firstWin) || f.items.featAsks != 3 {
			t.Fatalf("after two failures told of %+v in %d asks", got, f.items.featAsks)
		}
		bea, _ := fighter(f)
		f.items.featFail = 1 << 20
		record(f, "duel", bea, 1, false)
		if got, _ := winsTold(f, bea); len(got) != 0 {
			t.Fatalf("a grant that never landed told of %+v", got)
		}
		f.items.featFail = 0
		record(f, "team", bea, 1, false)
		if got, pushes := winsTold(f, bea); !slices.Equal(got, firstWin) || pushes != 1 {
			t.Fatalf("the next win told of %+v in %d pushes", got, pushes)
		}
	})
}

// Wins count over every mode: six duels and three 3v3s won before, and a 15v15 won now, reach ten wins' feat.
func TestWinsCountOverEveryMode(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := seededWith(t, 1, tenWinsData(t))
		ayla, _ := fighter(f)
		f.store.modes["duel"] = map[kernel.CharacterID]model.Rating{ayla: {Matches: 8, Wins: 6, Losses: 2}}
		f.store.modes["team"] = map[kernel.CharacterID]model.Rating{ayla: {Matches: 3, Wins: 3}}
		record(f, "battle", ayla, 1, false)
		got, _ := winsTold(f, ayla)
		if want := append(slices.Clone(firstWin), model.FeatPiece{Piece: "ten_win_cape", Feat: "ten_wins"}); !slices.Equal(got, want) {
			t.Fatalf("the tenth win told of %+v, want %+v", got, want)
		}
	})
}

// tenWinsData is the shipped data with a feat of ten wins, giving a cape.
func tenWinsData(t *testing.T) *gamedata.Data {
	t.Helper()
	dir := gamedatatest.ShippedDir(t)
	raw, err := os.ReadFile(filepath.Join(dir, "outfits.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(raw), "\nfeats:\n", "\n  - {id: ten_win_cape, slot: outfit, name: Ten-CoreWin Cape, stars: 3, look: knight_body, from: [{feat: ten_wins}]}\nfeats:\n", 1)
	if err := os.WriteFile(filepath.Join(dir, "outfits.yaml"), []byte(text+"  - {id: ten_wins, name: CoreWin ten PvP matches, when: {pvp_wins: 10}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := load.Dir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
