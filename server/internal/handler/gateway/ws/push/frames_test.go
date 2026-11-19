package push

import (
	"runtime"
	"testing"
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
	for range 100 {
		runtime.GC()
		f.mu.Lock()
		left := len(f.frames)
		f.mu.Unlock()
		if left == 1 {
			break
		}
		runtime.Gosched()
	}
	if len(f.frames) != 1 {
		t.Fatalf("%d frames' encodings kept; want the one frame still sent", len(f.frames))
	}
	if e := f.frames[weak.Make(kept)]; e == nil || e.parts[0] != first {
		t.Fatal("another frame's going took the encoding of one still sent")
	}
	runtime.KeepAlive(kept)
}
