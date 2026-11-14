package kernel

// The world module's rules; a class the data no longer has is ErrClassUnavailable, and a template that is no potion ErrNotUsable.
var (
	ErrMapBusy                  = NewRule("map_busy").Retryable() // its inbox is full this tick
	ErrNoMap                    = NewRule("no_map")
	ErrWorldStopped             = NewRule("world_stopped").Retryable() // the map is restarting or shutting down
	ErrNotSubscribed            = NewRule("not_subscribed")
	ErrCharacterDead            = NewRule("character_dead")
	ErrNpcOutOfReach            = NewRule("npc_out_of_reach")
	ErrQuestUnavailable         = NewRule("quest_unavailable")
	ErrQuestConditionsUnmet     = NewRule("quest_conditions_unmet")
	ErrQuestAlreadyActive       = NewRule("quest_already_active")
	ErrQuestAlreadyFinished     = NewRule("quest_already_finished")
	ErrTooManyQuests            = NewRule("too_many_quests")
	ErrQuestNotReady            = NewRule("quest_not_ready")
	ErrHPFull                   = NewRule("hp_full")
	ErrPointsChanged            = NewRule("points_changed")
	ErrNotEnoughPoints          = NewRule("not_enough_points")
	ErrNoFreeReset              = NewRule("no_free_reset")
	ErrBadPoints                = NewRule("bad_points")
	ErrSkillPointsChanged       = NewRule("skill_points_changed")
	ErrNotEnoughSkillPoints     = NewRule("not_enough_skill_points")
	ErrSkillNotLearnable        = NewRule("skill_not_learnable")
	ErrBadSkillPoints           = NewRule("bad_skill_points")
	ErrSkillNotLearned          = NewRule("skill_not_learned")
	ErrSkillPassive             = NewRule("skill_passive")
	ErrNotInTown                = NewRule("not_in_town")
	ErrSkillCoolingDown         = NewRule("skill_cooling_down")
	ErrNotEnoughEnergy          = NewRule("not_enough_energy")
	ErrSkillWaiting             = NewRule("skill_waiting")
	ErrPromotionConditionsUnmet = NewRule("promotion_conditions_unmet")
	ErrNotYourNextClass         = NewRule("not_your_next_class")
)
