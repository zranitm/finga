package e2etest

import (
	"slices"

	clientv1 "finga/api/gen/client/v1"
)

// Beaten reports whether the looter has heard the boss fall.
func (l *Looter) Beaten(boss uint32) bool {
	return slices.ContainsFunc(l.BossesGone, func(g *clientv1.FieldBossGone) bool { return g.GetMobId() == boss && g.GetBeaten() })
}
