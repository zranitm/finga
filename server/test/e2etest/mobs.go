package e2etest

import (
	"strconv"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// Walk holds right or left for d of the world's ticks, an input a frame as a client sends them, then lets go; the frames it paces itself by are left for the next reads.
func (p *Player) Walk(right bool, d time.Duration) {
	p.t.Helper()
	in := &clientv1.Input{Right: right, Left: !right}
	at := p.keepFrame(0)
	for start := at; at-start < p.ticksIn(d); at = p.keepFrame(at) {
		p.Send(wsconn.Message{Seq: p.NextSeq(), Data: in})
	}
	p.Send(wsconn.Message{Seq: p.NextSeq(), Data: &clientv1.Input{}})
}

// keepFrame waits for a frame newer than tick after, then takes what else has come, holding all of it for the next reads; it's the newest frame's tick.
func (p *Player) keepFrame(after uint64) uint64 {
	p.t.Helper()
	newest := after
	keep := func(m wsconn.Message) {
		p.held = append(p.held, m)
		if m.Type == "Delta" && m.Tick > newest {
			newest = m.Tick
		}
	}
	for newest == after {
		m, ok := p.receive(Stalled)
		if !ok {
			p.t.Fatalf("%s: no frame for %s while walking", p.ID, Stalled)
		}
		keep(m)
	}
	for m, ok := p.receive(0); ok; m, ok = p.receive(0) {
		keep(m)
	}
	return newest
}

// WindupOf is the tick of mob's next windup, waiting up to d of the world's ticks.
func (p *Player) WindupOf(t *testing.T, mob uint32, d time.Duration) uint64 {
	t.Helper()
	var at uint64
	if !p.Await(d, func(m wsconn.Message) bool {
		a, is := m.Data.(*clientv1.MobAttacked)
		at = m.Tick
		return is && a.GetMob() == mob
	}) {
		t.Fatalf("no windup from mob %d in %s", mob, d)
	}
	return at
}

// MobRef is the lock on mob id as a client sends it, empty for 0.
func MobRef(id uint32) string {
	if id == 0 {
		return ""
	}
	return "m:" + strconv.FormatUint(uint64(id), 10)
}
