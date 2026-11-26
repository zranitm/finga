package instance_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
)

// fakeFeats is the wardrobe as feats grant to it: each call's account and pieces, a piece the account has changing nothing; fail refuses that many calls.
type fakeFeats struct {
	mu       sync.Mutex
	accounts []kernel.AccountID
	calls    [][]model.FeatPiece
	has      map[string]bool
	fail     int
}

func (f *fakeFeats) GrantFeatPieces(_ context.Context, account kernel.AccountID, pieces []model.FeatPiece) ([]model.FeatPiece, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accounts = append(f.accounts, account)
	f.calls = append(f.calls, slices.Clone(pieces))
	if f.fail > 0 {
		f.fail--
		return nil, errors.New("the wardrobe refused")
	}
	if f.has == nil {
		f.has = map[string]bool{}
	}
	var added []model.FeatPiece
	for _, p := range pieces {
		if !f.has[p.Piece] {
			f.has[p.Piece] = true
			added = append(added, p)
		}
	}
	return added, nil
}

// asked is every piece asked for, in the calls' order, and from how many calls.
func (f *fakeFeats) asked() ([]model.FeatPiece, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Concat(f.calls...), len(f.calls)
}

// featData is the shipped data with the town guide's repeatable errand (talk to the guide; 7 EXP), and a feat of handing it in that gives the Errand Badge.
func featData(t *testing.T) *gamedata.Data {
	t.Helper()
	return worldtest.LoadDataWith(t, map[string]func(string) string{
		"npcs.yaml": func(s string) string {
			return strings.ReplaceAll(s, "ready_for_the_wilds]", "ready_for_the_wilds, errand]")
		},
		"quests.yaml": func(s string) string {
			return s + `
  - id: errand
    name: Errand
    giver: town_guide
    turn_in: town_guide
    repeatable: true
    objectives:
      - {talk: town_guide}
    rewards: {exp: 7}
`
		},
		"outfits.yaml": func(s string) string {
			s = strings.Replace(s, "\nfeats:\n", "\n  - {id: errand_badge, slot: headwear, name: Errand Badge, stars: 1, look: knight_helmet, from: [{feat: errand_done}]}\nfeats:\n", 1)
			return s + "  - {id: errand_done, name: Run an errand, when: {quest: errand}}\n"
		},
	})
}

// featTown runs the town with feats and a running item-call pool, and joins p, its errand ready to hand in.
func featTown(t *testing.T, data *gamedata.Data, p model.Player, feats *fakeFeats) (*instance.Instance, *worldtest.Sink) {
	t.Helper()
	inst, stop := worldtest.RunMap(t, data, worldtest.StartTown(data), &worldtest.Checkpoints{}, worldtest.Options{Feats: feats, CheckpointInterval: time.Hour, ItemCalls: runningPool(t)})
	t.Cleanup(stop)
	p.Connection, p.Account = "c1", kernel.NewAccountID()
	p.Quests = model.Quests{"errand": {Status: model.QuestActive, Progress: []int{1}}}
	return inst, join(t, inst, p)
}

// handInErrand hands the errand in and lets the grants come back.
func handInErrand(t *testing.T, inst *instance.Instance, id kernel.CharacterID) {
	t.Helper()
	if err := inst.HandInQuest(context.Background(), id, "c1", "town_guide", "errand"); err != nil {
		t.Fatal(err)
	}
	tick(2)
}

// piecesTold is every piece s heard was added by a feat, sorted, and in how many notices.
func piecesTold(t *testing.T, s *worldtest.Sink) ([]string, int) {
	t.Helper()
	var out []string
	added := eventsOf[model.PiecesAdded](s)
	for _, a := range added {
		if a.Cause != model.PieceFromFeat {
			t.Fatalf("a notice of cause %d", a.Cause)
		}
		out = append(out, a.Pieces...)
	}
	slices.Sort(out)
	return out, len(added)
}

// outfits.md section 4: a level-up to 10 or past it asks for Reach level 10's pieces, and a hand-in for the feats of its quest; the player hears of those new to the account, and of none it had.
func TestAFeatAsksForItsPiecesWhenMet(t *testing.T) {
	t.Run("a level-up to 10 and the quest’s feat, each new", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := featData(t)
			p := besideNPC(t, data, "Ayla", "town_guide")
			toTen, _ := data.ExpToNext(9)
			p.Level, p.Exp = 9, toTen-1
			feats := &fakeFeats{}
			inst, s := featTown(t, data, p, feats)
			handInErrand(t, inst, p.ID)
			if lus := eventsOf[model.LevelUp](s); len(lus) != 1 || lus[0].Level != 10 {
				t.Fatalf("level-ups %+v, want one to 10", lus)
			}
			asked, calls := feats.asked()
			slices.SortFunc(asked, func(a, b model.FeatPiece) int { return strings.Compare(a.Piece, b.Piece) })
			if want := []model.FeatPiece{{Piece: "errand_badge", Feat: "errand_done"}, {Piece: "wanderer_hat", Feat: "level_10"}}; !slices.Equal(asked, want) || calls != 2 {
				t.Fatalf("asked %+v in %d calls, want %+v in two", asked, calls, want)
			}
			if told, notices := piecesTold(t, s); !slices.Equal(told, []string{"errand_badge", "wanderer_hat"}) || notices != 2 {
				t.Fatalf("told of %v in %d notices", told, notices)
			}
		})
	})
	t.Run("pieces the account has tell of nothing; a level-up short of 10 asks for none", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := featData(t)
			p := besideNPC(t, data, "Ayla", "town_guide")
			toNine, _ := data.ExpToNext(8)
			p.Level, p.Exp = 8, toNine-1
			feats := &fakeFeats{has: map[string]bool{"errand_badge": true}}
			inst, s := featTown(t, data, p, feats)
			handInErrand(t, inst, p.ID)
			if lus := eventsOf[model.LevelUp](s); len(lus) != 1 || lus[0].Level != 9 {
				t.Fatalf("level-ups %+v, want one to 9", lus)
			}
			if asked, _ := feats.asked(); !slices.Equal(asked, []model.FeatPiece{{Piece: "errand_badge", Feat: "errand_done"}}) {
				t.Fatalf("asked %+v, want the errand's alone", asked)
			}
			if told, notices := piecesTold(t, s); len(told) != 0 || notices != 0 {
				t.Fatalf("told of %v in %d notices", told, notices)
			}
		})
	})
	t.Run("a level-up past 10 asks for it again, so a grant lost at 10 comes later", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			data := featData(t)
			p := besideNPC(t, data, "Ayla", "town_guide")
			toTwelve, _ := data.ExpToNext(11)
			p.Level, p.Exp = 11, toTwelve-1
			feats := &fakeFeats{}
			inst, s := featTown(t, data, p, feats)
			handInErrand(t, inst, p.ID)
			if asked, _ := feats.asked(); !slices.Contains(asked, model.FeatPiece{Piece: "wanderer_hat", Feat: "level_10"}) {
				t.Fatalf("asked %+v at level 12, want Reach level 10's again", asked)
			}
			if told, _ := piecesTold(t, s); !slices.Contains(told, "wanderer_hat") {
				t.Fatalf("told of %v", told)
			}
		})
	})
}

// A grant the item module refuses tells of nothing, and the next time the feat is met asks again and grants it.
func TestALostFeatGrantComesTheNextTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := featData(t)
		p := besideNPC(t, data, "Ayla", "town_guide")
		feats := &fakeFeats{fail: 1}
		inst, s := featTown(t, data, p, feats)
		handInErrand(t, inst, p.ID)
		if _, calls := feats.asked(); calls != 1 {
			t.Fatalf("%d calls, want the refused one", calls)
		}
		if told, _ := piecesTold(t, s); len(told) != 0 {
			t.Fatalf("a refused grant told of %v", told)
		}
		if err := inst.AcceptQuest(context.Background(), p.ID, "c1", "town_guide", "errand"); err != nil {
			t.Fatal(err)
		}
		if err := inst.Talk(context.Background(), p.ID, "c1", "town_guide"); err != nil {
			t.Fatal(err)
		}
		tick(1)
		handInErrand(t, inst, p.ID)
		if asked, calls := feats.asked(); calls != 2 || !slices.Equal(asked[1:], []model.FeatPiece{{Piece: "errand_badge", Feat: "errand_done"}}) {
			t.Fatalf("asked %+v in %d calls", asked, calls)
		}
		if told, notices := piecesTold(t, s); !slices.Equal(told, []string{"errand_badge"}) || notices != 1 {
			t.Fatalf("told of %v in %d notices, want the badge once", told, notices)
		}
	})
}

// A promotion asks for Take a second class's pieces once the class has changed.
func TestAPromotionAsksForThePromotedFeat(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := trainee(t)
		p.Account = kernel.NewAccountID()
		feats := &fakeFeats{}
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), worldtest.StartTown(worldtest.LoadData(t)), &worldtest.Checkpoints{}, worldtest.Options{Gifts: &fakeGifts{}, Feats: feats, CheckpointInterval: time.Hour, ItemCalls: runningPool(t)})
		defer stop()
		s := join(t, inst, p)
		if asked, calls := feats.asked(); calls != 0 {
			t.Fatalf("asked %+v before the promotion", asked)
		}
		done := promoteAsync(inst, p.ID, "warrior_trainer", "warrior")
		tick(2)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		tick(2)
		feats.mu.Lock()
		account := feats.accounts
		feats.mu.Unlock()
		if asked, _ := feats.asked(); !slices.Equal(asked, []model.FeatPiece{{Piece: "veteran_plate", Feat: "promoted"}}) || account[0] != p.Account {
			t.Fatalf("asked %+v for %v", asked, account)
		}
		if told, _ := piecesTold(t, s); !slices.Equal(told, []string{"veteran_plate"}) {
			t.Fatalf("told of %v", told)
		}
	})
}

// AskMetFeats asks for each feat the save shows met, a level reached, a promotion taken, a quest finished, and for none a save short of them all.
func TestAskMetFeatsAsksForWhatTheSaveShows(t *testing.T) {
	for _, c := range []struct {
		name   string
		class  kernel.Class
		level  int
		quests model.Quests
		want   []model.FeatPiece
	}{
		{"a promoted level 12 who ran the errand", "warrior", 12, model.Quests{"errand": {Status: model.QuestFinished, TimesFinished: 1}}, []model.FeatPiece{{Piece: "wanderer_hat", Feat: "level_10"}, {Piece: "veteran_plate", Feat: "promoted"}, {Piece: "errand_badge", Feat: "errand_done"}}},
		{"a beginner of 9 on the errand", "beginner", 9, model.Quests{"errand": {Status: model.QuestActive, Progress: []int{1}}}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := featData(t)
				p := besideNPC(t, data, "Ayla", "town_guide")
				p.Class, p.Level, p.Quests, p.Connection, p.Account = c.class, c.level, c.quests, "c1", kernel.NewAccountID()
				feats := &fakeFeats{}
				inst, stop := worldtest.RunMap(t, data, worldtest.StartTown(data), &worldtest.Checkpoints{}, worldtest.Options{Feats: feats, CheckpointInterval: time.Hour, ItemCalls: runningPool(t)})
				defer stop()
				s := join(t, inst, p)
				if err := inst.AskMetFeats(p.ID); err != nil {
					t.Fatal(err)
				}
				tick(2)
				asked, _ := feats.asked()
				slices.SortFunc(asked, func(a, b model.FeatPiece) int { return strings.Compare(a.Piece, b.Piece) })
				want := slices.Clone(c.want)
				slices.SortFunc(want, func(a, b model.FeatPiece) int { return strings.Compare(a.Piece, b.Piece) })
				if !slices.Equal(asked, want) {
					t.Fatalf("asked %+v, want %+v", asked, want)
				}
				if told, _ := piecesTold(t, s); len(told) != len(want) {
					t.Fatalf("told of %v", told)
				}
			})
		})
	}
}
