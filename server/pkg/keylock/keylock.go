// Package keylock runs one holder at a time per key, such as each character's joins and leaves.
package keylock

import (
	"context"
	"sync"
)

// Locks are a lock per key, made on first use and forgotten once no one holds or waits for it.
type Locks[K comparable] struct {
	mu sync.Mutex
	by map[K]*lock
}

type lock struct {
	held  chan struct{} // full while held
	users int           // holding or waiting, under Locks.mu
}

// New is a set of locks, all free.
func New[K comparable]() *Locks[K] {
	return &Locks[K]{by: map[K]*lock{}}
}

// Lock waits for key's holder, if any, until ctx ends; a free lock is taken even with ctx done; unlock lets the next go.
func (l *Locks[K]) Lock(ctx context.Context, key K) (unlock func(), err error) {
	l.mu.Lock()
	k := l.by[key]
	if k == nil {
		k = &lock{held: make(chan struct{}, 1)}
		l.by[key] = k
	}
	k.users++
	l.mu.Unlock()
	release := func() {
		l.mu.Lock()
		if k.users--; k.users == 0 {
			delete(l.by, key)
		}
		l.mu.Unlock()
	}
	unlock = sync.OnceFunc(func() { <-k.held; release() })
	select {
	case k.held <- struct{}{}:
		return unlock, nil
	default:
	}
	select {
	case k.held <- struct{}{}:
		return unlock, nil
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	}
}
