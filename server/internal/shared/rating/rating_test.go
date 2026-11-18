package rating_test

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"finga/internal/shared/rating"
)

// The update matches vectors computed apart from this code, in Python, from the paper's formulas as openskill's BradleyTerryFull writes them, with tau and without.
func TestRateMatchesTheVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Cases []struct {
			Name  string
			Tau   float64
			Sides [2][]rating.Rating
			Score float64
			After [2][]rating.Rating
		}
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Cases) == 0 {
		t.Fatal("no vectors")
	}
	for _, c := range v.Cases {
		got := rating.Rate(rating.Rules{Beta: 4.167, Tau: c.Tau}, c.Sides, c.Score)
		for s := range 2 {
			for k, want := range c.After[s] {
				if g := got[s][k]; math.Abs(g.Mu-want.Mu) > 1e-9 || math.Abs(g.Sigma-want.Sigma) > 1e-9 {
					t.Errorf("%s (tau %v): side %d member %d is %+v, want %+v", c.Name, c.Tau, s+1, k+1, g, want)
				}
			}
		}
	}
}

// A win raises the winner and lowers the loser, an upset more than an expected win, a draw between equals moves no mean, and every sigma shrinks.
func TestRateMovesTheWayItShould(t *testing.T) {
	rules := rating.Rules{Beta: 4.167, Tau: 0}
	fresh := rating.Rating{Mu: 25, Sigma: 8.333}
	won := rating.Rate(rules, [2][]rating.Rating{{fresh}, {fresh}}, 1)
	if won[0][0].Mu <= 25 || won[1][0].Mu >= 25 || won[0][0].Sigma >= 8.333 || won[1][0].Sigma >= 8.333 {
		t.Fatalf("after a win between equals %+v", won)
	}
	if d := rating.Rate(rules, [2][]rating.Rating{{fresh}, {fresh}}, 0.5); math.Abs(d[0][0].Mu-25) > 1e-12 || math.Abs(d[1][0].Mu-25) > 1e-12 {
		t.Fatalf("a draw between equals moved the means: %+v", d)
	}
	strong, weak := rating.Rating{Mu: 30, Sigma: 4}, rating.Rating{Mu: 20, Sigma: 4}
	expected := rating.Rate(rules, [2][]rating.Rating{{strong}, {weak}}, 1)[0][0].Mu - 30
	upset := rating.Rate(rules, [2][]rating.Rating{{weak}, {strong}}, 1)[0][0].Mu - 20
	if !(upset > expected && expected > 0) {
		t.Fatalf("an upset gains %v, an expected win %v", upset, expected)
	}
}

// The shown rating is base + per point × (mu − 3 sigma), never below 0: a new player shows the base.
func TestShown(t *testing.T) {
	for _, c := range []struct {
		r    rating.Rating
		want int
	}{
		{rating.Rating{Mu: 25, Sigma: 25.0 / 3}, 1000},
		{rating.Rating{Mu: 30, Sigma: 2}, 1960},
		{rating.Rating{Mu: 0, Sigma: 9}, 0},
	} {
		if got := rating.Shown(c.r, 1000, 40); got != c.want {
			t.Errorf("Shown(%+v) = %d, want %d", c.r, got, c.want)
		}
	}
}
