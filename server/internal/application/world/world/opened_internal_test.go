package world

import (
	"context"
	"testing"

	"finga/internal/application/world/instance"
)

// An instance asked to open before the World runs, and shut before it does, never runs: start has nothing to launch, and the World ends without a failure.
func TestAnInstanceShutBeforeStartNeverRuns(t *testing.T) {
	var o opened
	var never *instance.Instance // running it would panic, which would fail the World
	if err := o.run("gone", never, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := o.get("gone"); !ok {
		t.Fatal("an instance asked to open before start isn't there to join")
	}
	o.shut("gone")
	if _, ok := o.get("gone"); ok {
		t.Fatal("a shut instance is still there")
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	o.start(ctx, cancel)
	if err := o.stop(); err != nil {
		t.Fatalf("the World ended with %v; want nothing run", err)
	}
	if err := o.run("late", never, nil); err == nil {
		t.Fatal("an instance opened after the World stopped")
	}
}
