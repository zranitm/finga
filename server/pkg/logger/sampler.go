package logger

import (
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
		s.mu.Unlock()
		return
	}
	suppressed := k.suppressed
	k.suppressed = 0
	s.mu.Unlock()
	write(suppressed)
}
