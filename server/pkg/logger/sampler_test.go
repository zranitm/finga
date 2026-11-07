package logger_test

import (
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"finga/pkg/logger"
)

type line struct {
	key        string
	try        int
	suppressed int
}

// lineWriter writes the line of key's try through s into got.
func lineWriter(s *logger.Sampler, got *[]line) func(key string, try int) {
	return func(key string, try int) {
		s.Do(key, func(suppressed int) { *got = append(*got, line{key, try, suppressed}) })
	}
}

// A key writes its first line and leaves out the rest within the gap, counting them on its next; each key has a gap of its own.
func TestASamplerLetsOneLinePerKeyThroughPerGap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := logger.NewSampler(time.Minute)
		var got []line
		write := lineWriter(s, &got)
		for try := range 3 {
			write("export", try)
		}
		write("save", 0)
		if want := []line{{"export", 0, 0}, {"save", 0, 0}}; !slices.Equal(got, want) {
			t.Fatalf("wrote %v, want %v: the first of each key, none left out before it", got, want)
		}
		time.Sleep(time.Minute - time.Millisecond)
		write("export", 3)
		time.Sleep(time.Millisecond)
		write("export", 4)
		if want := []line{{"export", 0, 0}, {"save", 0, 0}, {"export", 4, 3}}; !slices.Equal(got, want) {
			t.Fatalf("wrote %v, want %v: a line a minute on, counting the 3 left out", got, want)
		}
	})
}

// Flush writes each key's newest line left out, counting the others, and nothing for a key that left none out.
func TestFlushWritesTheNewestLineLeftOut(t *testing.T) {
	s := logger.NewSampler(time.Minute)
	var got []line
	write := lineWriter(s, &got)
	for try := range 3 {
		write("export", try)
	}
	write("save", 0)
	write("cache", 0)
	write("cache", 1)
	got = nil
	s.Flush()
	if want := []line{{"cache", 1, 0}, {"export", 2, 1}}; !slices.Equal(got, want) {
		t.Fatalf("flushed %v, want %v", got, want)
	}
	s.Flush()
	if len(got) != 2 {
		t.Fatalf("a second flush wrote %v", got[2:])
	}
}
