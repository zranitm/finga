package e2etest

import (
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// Walk holds right or left for d, an input a tick as a client sends them, then lets go.
func (p *Player) Walk(right bool, d time.Duration) {
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		p.Send(wsconn.Message{Seq: p.NextSeq(), Data: &clientv1.Input{Right: right, Left: !right}})
	}
	p.Send(wsconn.Message{Seq: p.NextSeq(), Data: &clientv1.Input{}})
}

// WindupOf is the tick of mob's next windup, waiting up to d.
func (p *Player) WindupOf(t *testing.T, mob uint32, d time.Duration) uint64 {
	t.Helper()
	for deadline := time.Now().Add(d); time.Now().Before(deadline); {
		m, ok := p.Read(time.Until(deadline))
		if a, is := m.Data.(*clientv1.MobAttacked); ok && is && a.GetMob() == mob {
			return m.Tick
		}
	}
	t.Fatalf("no windup from mob %d in %s", mob, d)
	return 0
}
