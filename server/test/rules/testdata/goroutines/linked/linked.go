// Package linked starts goroutines every way the check knows, and one way it doesn't count.
package linked

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// Start starts one goroutine with the go keyword and one with a WaitGroup.
func Start() {
	go func() {}()
	var wg sync.WaitGroup
	wg.Go(func() {})
	wg.Wait()
}

type pump struct{}

// Run starts two with errgroup.
func (*pump) Run() error {
	var g errgroup.Group
	g.Go(func() error { return nil })
	g.TryGo(func() error { return nil })
	return g.Wait()
}

type timer struct{}

// arm starts two on timers.
func (timer) arm(ctx context.Context) {
	context.AfterFunc(ctx, func() {})
	time.AfterFunc(time.Second, func() {})
}

type recorder struct{}

// Go is a method named Go that starts nothing.
func (recorder) Go(fn func()) { fn() }

func init() {
	recorder{}.Go(func() {})
	(&pump{}).Run()
	timer{}.arm(context.Background())
}
