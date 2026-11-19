package instance

import model "finga/internal/model/world"

// tellSpots tells each player in a party, on a map cut by cells, where the members on it stand that they don't see, so their party frames and minimap keep them; an empty list too, so a member who left the map, its channel or the game drops off within a second.
func (i *Instance) tellSpots() {
	n := i.interest
	if n.whole {
		return
	}
	for _, id := range i.order {
		members := i.deps.Parties.Party(id).Members
		if len(members) < 2 {
			continue
		}
		var spots []model.PartySpot
		for _, m := range members {
			q, on := i.players[m]
			if _, seen := n.watches[id].seen[playerOf(m)]; on && !seen {
				spots = append(spots, model.PartySpot{ID: m, X: q.state.X, Z: q.state.Z})
			}
		}
		p := i.players[id]
		p.private = append(p.private, model.PartySpots{Tick: i.tick, Spots: spots})
	}
}
