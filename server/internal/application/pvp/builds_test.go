package pvp_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/pvp"
	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

// warriorBar is a warrior's arena bar, Strike and War Cry with traits.
var warriorBar = []string{"strike", "war_cry", "bastion", "roll_back", "", "", "", ""}

// warrior is a warrior of account with world bar and traits.
func (f fixture) warrior(account kernel.AccountID, bar []string, traits map[string]string) kernel.CharacterID {
	id := kernel.NewCharacterID()
	f.chars.mu.Lock()
	defer f.chars.mu.Unlock()
	f.chars.of[id] = model.Character{Account: account, Class: "warrior", Bar: bar, Traits: traits}
	return id
}

// of is a character of account and class, their world bar Strike alone.
func (f fixture) of(account kernel.AccountID, class kernel.Class) kernel.CharacterID {
	id := f.warrior(account, arenaKeys("strike"), nil)
	f.chars.mu.Lock()
	defer f.chars.mu.Unlock()
	ch := f.chars.of[id]
	ch.Class = class
	f.chars.of[id] = ch
	return id
}

// arenaKeys are eight keys holding skills, the rest empty.
func arenaKeys(skills ...string) []string {
	return append(skills, make([]string, 8-len(skills))...)
}

// pvp.md section 6: a build is saved once it's the class's, each save a revision past the last, and sent to the world for the player's match; one that isn't the class's, or a character not the account's, is refused, nothing saved or sent.
func TestABuildIsSavedOnceItsTheClasssAndSentToTheMatch(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	account := kernel.NewAccountID()
	ayla := f.warrior(account, warriorBar, nil)
	traits := map[string]string{"strike": "concussion", "war_cry": "fury", "iron_body": "iron_wall"} // Iron Body, War Cry's follow-up, off the bar
	for k := range 2 {
		if err := f.svc.SetBuild(ctx, account, ayla, warriorBar, traits); err != nil {
			t.Fatal(err)
		}
		want := model.Build{Bar: warriorBar, Traits: traits, Revision: k + 1}
		if got := f.world.sent(ayla); len(got) != k+1 || !same(got[k], want) || !same(f.store.builds[ayla], want) {
			t.Fatalf("save %d: sent %+v, stored %+v; want %+v", k+1, got, f.store.builds[ayla], want)
		}
	}
	tests := []struct {
		name   string
		id     kernel.CharacterID
		bar    []string
		traits map[string]string
		want   error
	}{
		{"another class's skill", ayla, []string{"fire_arrow", "", "", "", "", "", "", ""}, nil, model.ErrBadBuild},
		{"seven keys", ayla, warriorBar[:7], nil, model.ErrBadBuild},
		{"a trait not its skill's", ayla, warriorBar, map[string]string{"strike": "fury"}, model.ErrBadBuild},
		{"a trait for another class's skill", ayla, warriorBar, map[string]string{"fire_arrow": "flame"}, model.ErrBadBuild},
		{"another account's character", f.warrior(kernel.NewAccountID(), warriorBar, nil), warriorBar, nil, model.ErrNoCharacter},
		{"a Beginner's", f.of(account, "beginner"), arenaKeys("strike"), nil, model.ErrNotPromoted},
		{"an unknown class's", f.of(account, "jester"), arenaKeys(), nil, model.ErrNotPromoted},
		{"no such character", kernel.NewCharacterID(), warriorBar, nil, model.ErrNoCharacter},
	}
	for _, tt := range tests {
		if err := f.svc.SetBuild(ctx, account, tt.id, tt.bar, tt.traits); !errors.Is(err, tt.want) {
			t.Fatalf("%s: %v, want %v", tt.name, err, tt.want)
		}
	}
	if len(f.world.sent(ayla)) != 2 || f.store.builds[ayla].Revision != 2 {
		t.Fatalf("a refused build was sent or stored: %+v, %+v", f.world.sent(ayla), f.store.builds[ayla])
	}
}

// A build saved while the world can't take it is still saved: the next match it opens reads it.
func TestABuildTheWorldCantTakeIsStillSaved(t *testing.T) {
	f := newFixture(t)
	account := kernel.NewAccountID()
	ayla := f.warrior(account, warriorBar, nil)
	f.world.buildErr = errors.New("the world's inbox is full")
	if err := f.svc.SetBuild(context.Background(), account, ayla, warriorBar, nil); err != nil {
		t.Fatalf("saving with the world failing: %v", err)
	}
	if f.store.builds[ayla].Revision != 1 {
		t.Fatalf("stored %+v", f.store.builds[ayla])
	}
}

// pvp.md section 10: a profile gives each rated mode's shown rating, rank, placement matches left, record and place, the place once placed and shared by a tie, and an unrated mode's record alone; with no build saved, the build is the world bar's copy.
func TestAProfileGivesEachModeAndTheBuild(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	account := kernel.NewAccountID()
	ayla := f.warrior(account, []string{"guard", "strike", "", "", "", "", "", ""}, map[string]string{"strike": "swift", "taunt": "goad", "fire_arrow": "flame"})
	p, err := f.svc.Profile(ctx, account, ayla)
	if err != nil {
		t.Fatal(err)
	}
	fresh := []pvp.CoreStanding{
		{Mode: "duel", Shown: 1000, Rank: "bronze", PlacementLeft: 5},
		{Mode: "team", Shown: 1000, Rank: "bronze", PlacementLeft: 5},
		{Mode: "battle"},
	}
	copied := model.Build{Bar: []string{"bastion", "strike", "", "", "", "", "", ""}, Traits: map[string]string{"strike": "swift", "taunt": "goad"}} // another class's trait left off
	if !slices.Equal(p.Standings, fresh) || !same(p.Build, copied) {
		t.Fatalf("a new fighter's profile %+v; want %+v, built %+v", p, fresh, copied)
	}
	high, tie := model.Rating{Mu: 40, Sigma: 2, Matches: 9, Wins: 7, Losses: 2}, model.Rating{Mu: 22, Sigma: 2, Matches: 6, Wins: 4, Losses: 1, Draws: 1}
	shown := func(r model.Rating) int { return 1000 + int(40*(r.Mu-3*r.Sigma)) }
	f.store.modes["duel"] = map[kernel.CharacterID]model.Rating{
		ayla:                    tie,
		kernel.NewCharacterID(): {Shown: shown(high), Placed: true},
		kernel.NewCharacterID(): {Shown: shown(tie), Placed: true},
		kernel.NewCharacterID(): {Shown: shown(high) + 100, Matches: 3}, // not placed: off the board
	}
	f.store.modes["duel"][ayla] = model.Rating{Mu: tie.Mu, Sigma: tie.Sigma, Shown: shown(tie), Placed: true, Matches: tie.Matches, Wins: tie.Wins, Losses: tie.Losses, Draws: tie.Draws}
	f.store.modes["team"] = map[kernel.CharacterID]model.Rating{ayla: {Mu: high.Mu, Sigma: high.Sigma, Shown: shown(high), Placed: true, Matches: 9, Wins: 7, Losses: 2}}
	f.store.modes["battle"] = map[kernel.CharacterID]model.Rating{ayla: {Mu: 25, Sigma: 8.333, Shown: 0, Matches: 6, Wins: 3, Losses: 2, Draws: 1}}
	f.store.builds[ayla] = model.Build{Bar: warriorBar, Traits: map[string]string{}, Revision: 3}
	p, err = f.svc.Profile(ctx, account, ayla)
	if err != nil {
		t.Fatal(err)
	}
	if want := (pvp.CoreStanding{Mode: "duel", Shown: shown(tie), Rank: "platinum", Wins: 4, Losses: 1, Draws: 1, Place: 2}); p.Standings[0] != want || !same(p.Build, f.store.builds[ayla]) {
		t.Fatalf("a placed fighter's duel %+v, build %+v; want %+v, second with the one tied, and the saved build", p.Standings[0], p.Build, want)
	}
	if want := (pvp.CoreStanding{Mode: "battle", Wins: 3, Losses: 2, Draws: 1}); p.Standings[2] != want {
		t.Fatalf("the battle's standing %+v; want %+v, its own record alone", p.Standings[2], want)
	}
	if _, err := f.svc.Profile(ctx, kernel.NewAccountID(), ayla); !errors.Is(err, model.ErrNoCharacter) {
		t.Fatalf("another account's profile: %v", err)
	}
	if p, err := f.svc.Profile(ctx, account, f.of(account, "jester")); err != nil || !same(p.Build, model.Build{Bar: arenaKeys(), Traits: map[string]string{}}) {
		t.Fatalf("an unknown class's profile %+v, %v; want an empty copy, no skill being its", p, err)
	}
}

// A board's read is bounded: one that hangs ends after 5 s, every caller sharing it hearing so, and the next asks again; the module stopping ends one at once.
func TestABoardsReadIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		ctx, stop := context.WithCancel(context.Background())
		var running sync.WaitGroup
		running.Go(func() { _ = f.svc.Run(ctx) })
		synctest.Wait()
		f.store.hang = true
		var errs [2]error
		var ended [2]time.Duration
		start := time.Now()
		var asking sync.WaitGroup
		for k := range 2 {
			asking.Go(func() {
				_, errs[k] = f.svc.Leaderboard(context.Background(), "duel")
				ended[k] = time.Since(start)
			})
		}
		asking.Wait()
		if !errors.Is(errs[0], context.DeadlineExceeded) || !errors.Is(errs[1], context.DeadlineExceeded) || ended[0] != 5*time.Second || ended[1] != 5*time.Second || f.store.boards != 1 {
			t.Fatalf("two callers of a hung read: %v and %v, at %v, %d reads; want both out of time at 5 s, one read", errs[0], errs[1], ended, f.store.boards)
		}
		start = time.Now()
		go func() { time.Sleep(time.Second); stop() }()
		if _, err := f.svc.Leaderboard(context.Background(), "duel"); !errors.Is(err, context.Canceled) || time.Since(start) != time.Second || f.store.boards != 2 {
			t.Fatalf("the next read, the module stopping a second in: %v at %v, %d reads", err, time.Since(start), f.store.boards)
		}
		running.Wait()
	})
}

// pvp.md section 10: the board lists a rated mode's placed characters highest first, a tie sharing its place, with their names; one read serves 30 s.
func TestTheBoardListsThePlacedHighestFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		ctx := context.Background()
		ids := []kernel.CharacterID{kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID(), kernel.NewCharacterID()}
		shown := []int{1650, 1420, 1420, 1100, 2000}
		f.store.modes["team"] = map[kernel.CharacterID]model.Rating{}
		for k, id := range ids {
			f.store.modes["team"][id] = model.Rating{Shown: shown[k], Placed: k < 4, Matches: 5 + k, Wins: k}
			f.chars.as[id] = model.Named{Name: []string{"Ayla", "Bram", "Cora", "Eli", "Lone"}[k], Class: "warrior"}
		}
		slices.SortFunc(ids[1:3], func(a, b kernel.CharacterID) int { return slices.Compare(a[:], b[:]) })
		rows, err := f.svc.Leaderboard(ctx, "team")
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, r := range rows {
			got = append(got, f.chars.as[r.Character].Name)
			if r.Name != f.chars.as[r.Character].Name || r.Rank != map[int]string{1650: "platinum", 1420: "gold", 1100: "bronze"}[r.Shown] {
				t.Fatalf("row %+v", r)
			}
		}
		if places := []int{rows[0].Place, rows[1].Place, rows[2].Place, rows[3].Place}; len(rows) != 4 || got[0] != "Ayla" || got[3] != "Eli" || !slices.Equal(places, []int{1, 2, 2, 4}) {
			t.Fatalf("board %v at places %+v; want Ayla, the tied pair at 2, then Eli at 4, the unplaced Lone left off", got, rows)
		}
		f.store.modes["team"][ids[3]] = model.Rating{Shown: 3000, Placed: true}
		time.Sleep(29 * time.Second)
		if again, _ := f.svc.Leaderboard(ctx, "team"); again[0].Shown != 1650 || f.store.boards != 1 {
			t.Fatalf("29 s on, read %d times, top %+v; want the first read", f.store.boards, again[0])
		}
		time.Sleep(time.Second)
		if again, _ := f.svc.Leaderboard(ctx, "team"); again[0].Shown != 3000 || f.store.boards != 2 {
			t.Fatalf("30 s on, read %d times, top %+v; want it read again", f.store.boards, again[0])
		}
		for _, mode := range []string{"ffa", "", "battle"} { // an unrated mode has no board
			if _, err := f.svc.Leaderboard(ctx, mode); !errors.Is(err, model.ErrUnknownMode) {
				t.Fatalf("a board of %q: %v", mode, err)
			}
		}
	})
}

// A rating is placed once its placement matches are played, the board's to list.
func TestARatingIsPlacedAfterItsPlacementMatches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFixture(t)
		ayla := kernel.NewCharacterID()
		for k := range f.data.Pvp().Rating.Placement {
			f.svc.Record(context.Background(), model.Result{Match: kernel.NewMatchID(), Mode: "duel", Winner: 1, Sides: [2][]model.Outcome{{{Character: ayla}}, {{Tier: "novice"}}}})
			if rt := f.store.ratings[ayla]; rt.Placed != (k+1 == f.data.Pvp().Rating.Placement) {
				t.Fatalf("after %d matches placed %v", k+1, rt.Placed)
			}
		}
	})
}

// pvp.md section 6: once a match opens, each player's build is read and sent to it, the saved one or the world bar's copy; one that can't be read is left out, the match open all the same.
func TestAnOpenedMatchIsSentEachPlayersBuild(t *testing.T) {
	for _, failing := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			f := newFixture(t)
			f.run(t)
			ctx := context.Background()
			account := kernel.NewAccountID()
			ayla, robin := f.warrior(account, warriorBar, nil), f.warrior(account, []string{"guard", "", "", "", "", "", "", ""}, nil)
			saved := model.Build{Bar: warriorBar, Traits: map[string]string{"strike": "swift"}, Revision: 4}
			f.store.builds[ayla] = saved
			if failing {
				f.store.buildErr = errors.New("the database is down")
			}
			for _, id := range []kernel.CharacterID{ayla, robin} {
				if err := f.svc.Queue(ctx, model.Player{ID: id, Class: "warrior"}, "duel"); err != nil {
					t.Fatal(err)
				}
			}
			wait(time.Second)
			opened := f.open(t, model.Player{ID: ayla}, model.Player{ID: robin})
			synctest.Wait()
			if len(opened) != 1 {
				t.Fatalf("opened %d matches", len(opened))
			}
			a, r := f.world.sent(ayla), f.world.sent(robin)
			if failing {
				if len(a)+len(r) != 0 {
					t.Fatalf("with the store failing, sent %+v and %+v", a, r)
				}
				return
			}
			copied := model.Build{Bar: []string{"bastion", "", "", "", "", "", "", ""}, Traits: map[string]string{}}
			if len(a) != 1 || !same(a[0], saved) || len(r) != 1 || !same(r[0], copied) {
				t.Fatalf("sent Ayla %+v, Robin %+v; want her saved build, and his world bar's copy", a, r)
			}
		})
	}
}

func same(a, b model.Build) bool {
	return slices.Equal(a.Bar, b.Bar) && maps.Equal(a.Traits, b.Traits) && a.Revision == b.Revision
}

// Each player's build is read on its own, apart from the call that opened the match: one whose read hangs leaves the other's sent at once, and gives up after 5 s, or as the module stops.
func TestEachPlayersBuildIsReadOnItsOwn(t *testing.T) {
	for _, c := range []struct {
		name   string
		stopAt time.Duration // when the module stops, 0 for not
		lasted time.Duration
	}{
		{"its own bound", 0, 5 * time.Second},
		{"the module stopping", time.Second, time.Second},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newFixture(t)
				lifeCtx, stop := context.WithCancel(context.Background())
				var running sync.WaitGroup
				running.Go(func() { _ = f.svc.Run(lifeCtx) })
				defer func() { stop(); running.Wait() }()
				ctx := context.Background()
				account := kernel.NewAccountID()
				ayla, robin := f.warrior(account, warriorBar, nil), f.warrior(account, warriorBar, nil)
				f.store.stall[ayla] = true
				for _, id := range []kernel.CharacterID{ayla, robin} {
					if err := f.svc.Queue(ctx, model.Player{ID: id, Class: "warrior"}, "duel"); err != nil {
						t.Fatal(err)
					}
				}
				wait(time.Second)
				f.open(t, model.Player{ID: ayla}, model.Player{ID: robin})
				synctest.Wait()
				if len(f.world.sent(robin)) != 1 || len(f.world.sent(ayla)) != 0 {
					t.Fatalf("with Ayla's read hung, sent Robin %+v and Ayla %+v; want Robin's", f.world.sent(robin), f.world.sent(ayla))
				}
				if c.stopAt > 0 {
					wait(c.stopAt)
					stop()
				}
				wait(5 * time.Second)
				f.store.mu.Lock()
				defer f.store.mu.Unlock()
				if got, ok := f.store.stalled[ayla]; !ok || got != c.lasted {
					t.Fatalf("Ayla's hung read lasted %v (ended %v); want %v", got, ok, c.lasted)
				}
			})
		})
	}
}
