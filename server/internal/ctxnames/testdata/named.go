// Package named holds contexts named well and badly, for the ctxnames check.
package named

import "context"

var background = context.Background

func one() {
	hurried, stop := context.WithCancel(background())
	defer stop()
	_ = func(parent context.Context, ctx context.Context) {}
	_, _ = hurried, background
}

func two(ctx context.Context) {
	retrying, stopRetrying := context.WithCancel(ctx)
	defer stopRetrying()
	retryCtx, cancel := context.WithCancel(retrying)
	defer cancel()
	_ = struct {
		jobs    context.Context
		jobsCtx context.Context
	}{retryCtx, retryCtx}
}
