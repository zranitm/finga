package world_test

import (
	"errors"
	"log/slog"
	"testing"

	"finga/internal/host"
	"finga/internal/kernel"
	"finga/modules/world"
	"finga/modules/world/internal/model"
	"finga/pkg/observability/metrics"
	"finga/pkg/transient"
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
		{model.ErrBusy, kernel.ErrMapBusy, "map_busy", true},
		{model.ErrNoMap, kernel.ErrNoMap, "no_map", false},
		{model.ErrStopped, kernel.ErrWorldStopped, "world_stopped", true},
		{model.ErrClassUnavailable, kernel.ErrClassUnavailable, "class_unavailable", false},
		{model.ErrCharacterDead, kernel.ErrCharacterDead, "character_dead", false},
		{model.ErrPointsChanged, kernel.ErrPointsChanged, "points_changed", false},
		{model.ErrNotEnoughPoints, kernel.ErrNotEnoughPoints, "not_enough_points", false},
		{model.ErrNoFreeReset, kernel.ErrNoFreeReset, "no_free_reset", false},
		{model.ErrBadPoints, kernel.ErrBadPoints, "bad_points", false},
		{model.ErrSkillPointsChanged, kernel.ErrSkillPointsChanged, "skill_points_changed", false},
		{model.ErrNotEnoughSkillPoints, kernel.ErrNotEnoughSkillPoints, "not_enough_skill_points", false},
		{model.ErrSkillNotLearnable, kernel.ErrSkillNotLearnable, "skill_not_learnable", false},
		{model.ErrBadSkillPoints, kernel.ErrBadSkillPoints, "bad_skill_points", false},
		{model.ErrSkillNotLearned, kernel.ErrSkillNotLearned, "skill_not_learned", false},
		{model.ErrSkillPassive, kernel.ErrSkillPassive, "skill_passive", false},
		{model.ErrNotInTown, kernel.ErrNotInTown, "not_in_town", false},
		{model.ErrSkillCoolingDown, kernel.ErrSkillCoolingDown, "skill_cooling_down", false},
		{model.ErrNotEnoughEnergy, kernel.ErrNotEnoughEnergy, "not_enough_energy", false},
		{model.ErrSkillWaiting, kernel.ErrSkillWaiting, "skill_waiting", false},
		{model.ErrPromotionUnmet, kernel.ErrPromotionConditionsUnmet, "promotion_conditions_unmet", false},
		{model.ErrNotYourNextClass, kernel.ErrNotYourNextClass, "not_your_next_class", false},
		{model.ErrNpcOutOfReach, kernel.ErrNpcOutOfReach, "npc_out_of_reach", false},
		{model.ErrQuestUnavailable, kernel.ErrQuestUnavailable, "quest_unavailable", false},
		{model.ErrQuestUnmet, kernel.ErrQuestConditionsUnmet, "quest_conditions_unmet", false},
		{model.ErrQuestActive, kernel.ErrQuestAlreadyActive, "quest_already_active", false},
		{model.ErrQuestFinished, kernel.ErrQuestAlreadyFinished, "quest_already_finished", false},
		{model.ErrTooManyQuests, kernel.ErrTooManyQuests, "too_many_quests", false},
		{model.ErrQuestNotReady, kernel.ErrQuestNotReady, "quest_not_ready", false},
		{model.ErrHPFull, kernel.ErrHPFull, "hp_full", false},
		{model.ErrNotUsable, kernel.ErrNotUsable, "not_usable", false},
		{kernel.ErrNotSubscribed, kernel.ErrNotSubscribed, "not_subscribed", false}, // the facade's own: a join without its gateway's stream
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
	mod, hostErr := h.Module("world")
	if hostErr != nil {
		t.Fatal(hostErr)
	}
	calls, hostErr := mod.Calls(world.ErrorTable)
	if hostErr != nil {
		t.Fatal(hostErr)
	}
	_, c := calls.Start(t.Context(), "Call")
	defer c.End(&err)
	return do()
}
