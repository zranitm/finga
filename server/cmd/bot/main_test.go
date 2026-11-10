package main

import (
	"maps"
	"math"
	"testing"

	clientv1 "finga/api/gen/client/v1"
)

func TestKillsAreTheBotsOwnKillingBlows(t *testing.T) {
	type event struct {
		tick    uint64
		removed []uint32         // a Delta's removed_mobs
		blow    *clientv1.Damage // else a Damage
	}
	mine := func(mob string) *clientv1.Damage { return &clientv1.Damage{Source: "p:c-bot", Target: mob, Amount: 9} }
	other := func(mob string) *clientv1.Damage {
		return &clientv1.Damage{Source: "p:c-other", Target: mob, Amount: 9}
	}
	next := event{tick: 99} // a later tick settles the one before
	cases := []struct {
		name   string
		events []event
		want   int
	}{
		{"its blow and the mob's removal in one tick, the Delta first as the world sends them", []event{{tick: 7, removed: []uint32{3}}, {tick: 7, blow: mine("m:3")}, next}, 1},
		{"the same, the Damage first", []event{{tick: 7, blow: mine("m:3")}, {tick: 7, removed: []uint32{3}}, next}, 1},
		{"its blow, then a party member's killing blow, in one tick", []event{{tick: 7, removed: []uint32{3}}, {tick: 7, blow: mine("m:3")}, {tick: 7, blow: other("m:3")}, next}, 0},
		{"a party member's blow, then its killing blow, in one tick", []event{{tick: 7, removed: []uint32{3}}, {tick: 7, blow: other("m:3")}, {tick: 7, blow: mine("m:3")}, next}, 1},
		{"its miss after a party member's killing blow", []event{{tick: 7, removed: []uint32{3}}, {tick: 7, blow: other("m:3")}, {tick: 7, blow: &clientv1.Damage{Source: "p:c-bot", Target: "m:3", Miss: true}}, next}, 0},
		{"its blow a tick before another's killing blow", []event{{tick: 6, blow: mine("m:3")}, {tick: 7, removed: []uint32{3}}, {tick: 7, blow: other("m:3")}, next}, 0},
		{"a mob hitting the bot as it dies", []event{{tick: 7, removed: []uint32{3}}, {tick: 7, blow: &clientv1.Damage{Source: "m:3", Target: "p:c-bot", Amount: 4}}, next}, 0},
		{"two mobs in one tick, one its own, and one the next", []event{{tick: 7, removed: []uint32{3, 4}}, {tick: 7, blow: other("m:3")}, {tick: 7, blow: mine("m:4")}, {tick: 8, removed: []uint32{5}}, {tick: 8, blow: mine("m:5")}, next}, 2},
		{"a tick not yet over", []event{{tick: 7, removed: []uint32{3}}, {tick: 7, blow: mine("m:3")}}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k, got := newKills(), 0
			for _, e := range c.events {
				if e.blow != nil {
					got += k.blow(e.tick, e.blow, "c-bot")
				} else {
					got += k.removedMobs(e.tick, e.removed, "c-bot")
				}
			}
			if got != c.want {
				t.Fatalf("kills %d, want %d", got, c.want)
			}
		})
	}
}

func TestTheReportPrintsBucketBoundsAsTheyAre(t *testing.T) {
	ticks := newHist()
	maps.Copy(ticks.buckets, map[float64]float64{0.0005: 90, 0.001: 99, 0.0025: 100, math.Inf(1): 100})
	ticks.count = 100
	if got, want := percentile(ticks, 0.5), "0.28 ms (bucket ≤ 0.5 ms)"; got != want {
		t.Errorf("p50 %q, want %q", got, want)
	}
	if got, want := percentile(ticks, 0.99), "1.00 ms (bucket ≤ 1 ms)"; got != want {
		t.Errorf("p99 %q, want %q", got, want)
	}

	batches := newHist()
	maps.Copy(batches.buckets, map[float64]float64{1: 649, 2: 649, 32: 649, math.Inf(1): 649})
	batches.count = 649
	if got, want := drops(batches.quantile(0.5)), "≤ 1"; got != want {
		t.Errorf("drops p50 %q, want %q: a call carries at least one drop", got, want)
	}
	batches.buckets[math.Inf(1)] = 650
	batches.count = 650
	if got, want := drops(batches.quantile(1)), "> 32"; got != want {
		t.Errorf("drops max %q, want %q", got, want)
	}
}
