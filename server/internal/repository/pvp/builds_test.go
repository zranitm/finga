package pvp

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

// An arena build is saved and read back whole, each save a revision past the last; a character who saved none has none.
func TestABuildIsSavedWithARisingRevision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewPostgres(cluster.DB(t), nil)
	ayla := kernel.NewCharacterID()
	if _, ok, err := store.Build(ctx, ayla); err != nil || ok {
		t.Fatalf("a build never saved: found %v, %v", ok, err)
	}
	builds := []model.Build{
		{Bar: []string{"strike", "war_cry", "", "", "", "", "", ""}, Traits: map[string]string{"strike": "concussion"}},
		{Bar: []string{"", "", "", "", "", "", "", "taunt"}, Traits: map[string]string{}},
	}
	for k, b := range builds {
		revision, err := store.PutBuild(ctx, ayla, b)
		if err != nil || revision != k+1 {
			t.Fatalf("save %d: revision %d, %v", k+1, revision, err)
		}
		got, ok, err := store.Build(ctx, ayla)
		if err != nil || !ok || !slices.Equal(got.Bar, b.Bar) || !maps.Equal(got.Traits, b.Traits) || got.Traits == nil || got.Revision != k+1 {
			t.Fatalf("save %d read back as %+v, %v, %v", k+1, got, ok, err)
		}
	}
}

// rate stores ratings as an outcome does, one match each, so a test sets the board.
func rate(t *testing.T, store *Postgres, mode string, after map[kernel.CharacterID]model.Rating) {
	t.Helper()
	match := model.Match{ID: kernel.NewMatchID(), Mode: mode, Started: t0, Ended: t0, Winner: 1}
	ids := slices.Collect(maps.Keys(after))
	if _, err := store.Record(context.Background(), match, ids, func(map[kernel.CharacterID]model.Rating) model.Rated { return model.Rated{After: after} }); err != nil {
		t.Fatal(err)
	}
}

// pvp.md section 10: the board is a mode's placed ratings, highest first and a tie in character order, at most most; Above counts the placed above a rating; Ratings is a character's in each mode.
func TestTheBoardIsTheModesPlacedHighestFirst(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewPostgres(cluster.DB(t), nil)
	ids := []kernel.CharacterID{kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID()}
	r := func(shown int, placed bool) model.Rating {
		m := 3
		if placed {
			m = 6
		}
		return model.Rating{Mu: 25, Sigma: 4, Shown: shown, Placed: placed, Matches: m, Wins: m}
	}
	rate(t, store, "duel", map[kernel.CharacterID]model.Rating{ids[0]: r(1400, true), ids[1]: r(1650, true), ids[2]: r(1400, true), ids[3]: r(2000, false)})
	rate(t, store, "team", map[kernel.CharacterID]model.Rating{ids[0]: r(1900, true), ids[4]: r(1700, true)})
	board, err := store.Board(ctx, "duel", 100)
	if err != nil {
		t.Fatal(err)
	}
	tied := []kernel.CharacterID{ids[0], ids[2]}
	slices.SortFunc(tied, func(a, b kernel.CharacterID) int {
		return strings.Compare(uuid.UUID(a).String(), uuid.UUID(b).String())
	})
	var got []kernel.CharacterID
	for _, b := range board {
		got = append(got, b.Character)
	}
	if !slices.Equal(got, []kernel.CharacterID{ids[1], tied[0], tied[1]}) || board[0].Rating.Shown != 1650 || board[0].Rating.Wins != 6 || !board[0].Rating.Placed {
		t.Fatalf("duel's board %+v; want the 1650, then the tied 1400s in character order, the unplaced 2000 left off", board)
	}
	if top, _ := store.Board(ctx, "duel", 1); len(top) != 1 || top[0].Character != ids[1] {
		t.Fatalf("the board of one: %+v", top)
	}
	for _, c := range []struct {
		shown, above int
	}{{1650, 0}, {1400, 1}, {1399, 3}, {9999, 0}} {
		if n, err := store.Above(ctx, "duel", c.shown); err != nil || n != c.above {
			t.Fatalf("above %d: %d, %v; want %d", c.shown, n, err, c.above)
		}
	}
	all, err := store.Ratings(ctx, ids[0])
	if err != nil || len(all) != 2 || all["duel"].Matches != 6 || all["team"].Mu != 25 {
		t.Fatalf("Ayla's ratings %+v, %v", all, err)
	}
}

// The board and a place are read from pvp_ratings_board, the partial index of the placed: with 20,000 ratings, most unplaced, neither scans the table. The plans print with -v.
func TestTheBoardIsReadByItsIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := cluster.DB(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO pvp_ratings (character_id, mode, mu, sigma, shown, placed, matches, wins, losses, draws, created_at, updated_at)
		SELECT gen_random_uuid(), CASE WHEN n % 2 = 0 THEN 'duel' ELSE 'team' END, 25, 3, 1000 + (n * 7919) % 1500, m >= 5, m, m, 0, 0, now(), now()
		FROM generate_series(1, 20000) AS n, LATERAL (SELECT CASE WHEN n % 10 < 3 THEN 8 ELSE 2 END AS m) AS played;
		ANALYZE pvp_ratings;`); err != nil {
		t.Fatal(err)
	}
	for name, q := range map[string]string{
		"board": "EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, SUMMARY OFF) SELECT character_id, shown, matches, wins, losses, draws FROM pvp_ratings WHERE mode = 'duel' AND placed ORDER BY shown DESC, character_id LIMIT 100",
		"above": "EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, SUMMARY OFF) SELECT count(*) FROM pvp_ratings WHERE mode = 'duel' AND placed AND shown > 2300",
	} {
		rows, err := pool.Query(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, line)
		}
		rows.Close()
		text := strings.Join(plan, "\n")
		t.Logf("%s:\n%s", name, text)
		if !strings.Contains(text, "pvp_ratings_board") || strings.Contains(text, "Seq Scan") {
			t.Fatalf("the %s's plan doesn't read the index alone:\n%s", name, text)
		}
	}
}
