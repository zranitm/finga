package mobs

// ChaseBounds is where mob id may go after a player, for the test that checks every shipped map's mobs against its portals.
func (a *Arena) ChaseBounds(id uint64) (lo, hi float64, ok bool) {
	for _, mb := range a.mobs {
		if mb.id == id {
			return mb.chaseX.lo, mb.chaseX.hi, true
		}
	}
	return 0, 0, false
}
