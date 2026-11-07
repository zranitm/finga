package logger_test

import (
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"finga/pkg/logger"
)

// A key writes its first line and leaves out the rest within the gap, counting them on its next; each key has a gap of its own.
func TestASamplerLetsOneLinePerKeyThroughPerGap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := logger.NewSampler(time.Minute)
		type line struct {
			key        string
			try        int
			suppressed int
		}
		var got []line
		write := func(key string, try int) {
			s.Do(key, func(suppressed int) { got = append(got, line{key, try, suppressed}) })
		}
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
