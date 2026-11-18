// Package rating is Weng and Lin's Bradley-Terry rating with full pairing, for two sides ("A Bayesian Approximation Method for Online Ranking", JMLR 2011), as openskill's BradleyTerryFull computes it.
package rating

import "math"

// kappa is the least a variance may shrink to, as a share of itself.
const kappa = 0.0001

// Rating is a player's skill: its mean and its uncertainty.
type Rating struct {
	Mu, Sigma float64
}

// Rules are the model's: Beta, the gap in skill that makes a win likely, and Tau, added to each sigma before an update so a rating never stops moving.
type Rules struct {
	Beta, Tau float64
}

// Rate is each side's members after a match, side 1's score being 1 for a win, ½ for a draw and 0 for a loss.
func Rate(r Rules, sides [2][]Rating, score float64) [2][]Rating {
	var mu, sigma2 [2]float64
	var in [2][]Rating
	for s, side := range sides {
		for _, p := range side {
			p.Sigma = math.Sqrt(p.Sigma*p.Sigma + r.Tau*r.Tau)
			in[s] = append(in[s], p)
			mu[s] += p.Mu
			sigma2[s] += p.Sigma * p.Sigma
		}
	}
	c := math.Sqrt(sigma2[0] + sigma2[1] + 2*r.Beta*r.Beta)
	var out [2][]Rating
	for s := range 2 {
		q := 1 - s
		p := 1 / (1 + math.Exp((mu[q]-mu[s])/c))
		got := score
		if s == 1 {
			got = 1 - score
		}
		omega := sigma2[s] / c * (got - p)
		gamma := math.Sqrt(sigma2[s]) / c
		delta := gamma * sigma2[s] / (c * c) * p * (1 - p)
		for _, m := range in[s] {
			share := m.Sigma * m.Sigma / sigma2[s]
			out[s] = append(out[s], Rating{Mu: m.Mu + share*omega, Sigma: m.Sigma * math.Sqrt(math.Max(1-share*delta, kappa))})
		}
	}
	return out
}

// Shown is a rating as one number: base + perPoint × (mu − 3 sigma), at least 0.
func Shown(rt Rating, base, perPoint float64) int {
	return max(0, int(math.Round(base+perPoint*(rt.Mu-3*rt.Sigma))))
}
