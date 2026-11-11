package logger

import (
	"maps"
	"slices"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Sampler lets one line per key through every gap, each counting the lines the key left out before it, so a failure repeated in a burst writes a few.
type Sampler struct {
	every rate.Limit

	mu   sync.Mutex
	keys map[string]*sampled // kept for good: keys come from a small set, such as method names
}

type sampled struct {
	limiter    *rate.Limiter
	suppressed int
	last       func(suppressed int) // the newest line left out, which Flush writes
}

// NewSampler lets one line per key through every gap.
func NewSampler(gap time.Duration) *Sampler {
	return &Sampler{every: rate.Every(gap), keys: map[string]*sampled{}}
}

// Do calls write with the count of lines key left out since its last, unless key wrote one within the gap, when it counts this one instead.
func (s *Sampler) Do(key string, write func(suppressed int)) {
	s.mu.Lock()
	k, ok := s.keys[key]
	if !ok {
		k = &sampled{limiter: rate.NewLimiter(s.every, 1)}
		s.keys[key] = k
	}
	if !k.limiter.Allow() {
		k.suppressed++
		k.last = write
		s.mu.Unlock()
		return
	}
	suppressed := k.suppressed
	k.suppressed, k.last = 0, nil
	s.mu.Unlock()
	write(suppressed)
}

// Flush writes each key's newest line left out, counting the others it left out, so a run that ends within a gap still shows.
func (s *Sampler) Flush() {
	s.mu.Lock()
	var pending []func()
	for _, key := range slices.Sorted(maps.Keys(s.keys)) {
		k := s.keys[key]
		if k.last == nil {
			continue
		}
		write, before := k.last, k.suppressed-1
		pending = append(pending, func() { write(before) })
		k.suppressed, k.last = 0, nil
	}
	s.mu.Unlock()
	for _, write := range pending {
		write()
	}
}
