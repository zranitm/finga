package instance

import model "finga/internal/model/world"

// tellSpots tells each player on a map cut by cells where their party's members on it stand that they don't see, so their party frames and minimap keep them; once all are in sight again, an empty list says so.
func (i *Instance) tellSpots() {
	n := i.interest
	if n.whole {
		return
	}
	for _, id := range i.order {
		p := i.players[id]
		var spots []model.PartySpot
		for _, m := range i.deps.Parties.Party(id).Members {
			q, on := i.players[m]
			if _, seen := n.watches[id].seen[playerOf(m)]; on && !seen {
				spots = append(spots, model.PartySpot{ID: m, X: q.state.X, Z: q.state.Z})
			}
		}
		if len(spots) > 0 || p.toldSpots {
			p.private = append(p.private, model.PartySpots{Tick: i.tick, Spots: spots})
		}
		p.toldSpots = len(spots) > 0
	}
}
