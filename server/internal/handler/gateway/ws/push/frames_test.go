package push

import (
	"runtime"
	"testing"
	"time"
	"weak"

	model "finga/internal/model/gateway"
)

// A frame's encodings go once the frame does, and only its own: a gateway keeps those of the frames still being sent.
func TestAFramesEncodingsGoWithIt(t *testing.T) {
	f := NewFrames()
	frame := func() *model.Frame { return &model.Frame{Parts: []model.Part{{RemovedMobs: []uint64{1}}}} }
	kept := frame()
	first := f.shared(kept, []int{0})[0]
	for range 3 {
		f.shared(frame(), []int{0})
	}
	f.shared(kept, []int{0})
	held := func() (int, *encoding) {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.frames), f.frames[weak.Make(kept)]
	}
	left, e := held()
	for deadline := time.Now().Add(10 * time.Second); left > 1 && time.Now().Before(deadline); left, e = held() {
		runtime.GC() // the cleanups run on a goroutine of their own after it
		time.Sleep(time.Millisecond)
	}
	if left != 1 {
		t.Fatalf("%d frames' encodings kept; want the one frame still sent", left)
	}
	if e == nil || e.parts[0] != first {
		t.Fatal("another frame's going took the encoding of one still sent")
	}
	runtime.KeepAlive(kept)
}
