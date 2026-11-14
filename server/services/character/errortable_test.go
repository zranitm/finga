package character_test

import (
	"errors"
	"log/slog"
	"testing"

	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/observability/metrics"
	"finga/pkg/transient"
	"finga/services/character"
	"finga/services/character/internal/model"
)

// Each error a call meets reaches callers as its kernel error, with its reason.
// The rows are literals, so a row changed in the facade's table or a reason changed in the kernel fails here.
func TestEachErrorReachesCallersAsItsKernelError(t *testing.T) {
	for _, tt := range []struct {
		err    error
		kernel error  // nil for none
		reason string // "" for none
		retry  bool   // a retry keyed on transient.ErrUnavailable retries it
	}{
		{model.ErrNotFound, kernel.ErrCharacterNotFound, "character_not_found", false},
		{model.ErrNameTaken, kernel.ErrNameTaken, "name_taken", false},
		{model.ErrBadName, kernel.ErrBadCharacterName, "bad_character_name", false},
		{model.ErrClassUnavailable, kernel.ErrClassUnavailable, "class_unavailable", false},
		{model.ErrSlotsFull, kernel.ErrCharacterSlotsFull, "character_slots_full", false},
		{model.ErrBadQuest, kernel.ErrBadQuest, "bad_quest", false},
		{model.ErrBadSkill, kernel.ErrBadSkill, "bad_skill", false},
		{model.ErrBadSkillTimer, kernel.ErrBadSkillTimer, "bad_skill_timer", false},
		{model.ErrBadCheckpoint, kernel.ErrBadCheckpoint, "bad_checkpoint", false},
		{model.ErrBadSkillBar, kernel.ErrBadSkillBar, "bad_skill_bar", false},
		{kernel.ErrBadID, kernel.ErrBadID, "bad_id", false},
		{transient.ErrUnavailable, nil, "", true},
		{errors.New("disk on fire"), nil, "", false}, // passes as it is, so a caller reads it as internal
	} {
		t.Run(tt.err.Error(), func(t *testing.T) {
			err := call(t, func() error { return tt.err })
			if tt.kernel != nil && !errors.Is(err, tt.kernel) || !errors.Is(err, tt.err) {
				t.Fatalf("reached callers as %v; want %v wrapping %v", err, tt.kernel, tt.err)
			}
			if reason, ok := kernel.ReasonOf(err); reason != tt.reason || ok != (tt.reason != "") {
				t.Fatalf("reason %q (%v), want %q", reason, ok, tt.reason)
			}
			if retry := errors.Is(err, transient.ErrUnavailable); retry != tt.retry {
				t.Fatalf("a caller retrying on transient.ErrUnavailable retries it: %v, want %v", retry, tt.retry)
			}
		})
	}
}

// A panic in a call reaches callers as kernel.ErrInternal, with no reason.
func TestAPanicIsInternal(t *testing.T) {
	err := call(t, func() error { panic("till jammed") })
	if _, has := kernel.ReasonOf(err); !errors.Is(err, kernel.ErrInternal) || has {
		t.Fatalf("reached callers as %v", err)
	}
}

// call is the error a call that met do's returns, through the module's facade as a caller in the process gets it.
func call(t *testing.T, do func() error) (err error) {
	t.Helper()
	h, hostErr := host.New(host.Options{Name: "finga", Logger: slog.New(slog.DiscardHandler), Registry: metrics.NewRegistry()})
	if hostErr != nil {
		t.Fatal(hostErr)
	}
	t.Cleanup(h.Close)
	mod, hostErr := h.Module("character")
	if hostErr != nil {
		t.Fatal(hostErr)
	}
	calls, hostErr := mod.Calls(character.ErrorTable)
	if hostErr != nil {
		t.Fatal(hostErr)
	}
	_, c := calls.Start(t.Context(), "Call")
	defer c.End(&err)
	return do()
}
