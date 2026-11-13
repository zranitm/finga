package kernel

// The character module's rules.
var (
	ErrCharacterNotFound  = NewRule("character_not_found")
	ErrNameTaken          = NewRule("name_taken")
	ErrBadCharacterName   = NewRule("bad_character_name")
	ErrClassUnavailable   = NewRule("class_unavailable")
	ErrCharacterSlotsFull = NewRule("character_slots_full")
	ErrBadQuest           = NewRule("bad_quest")
	ErrBadSkill           = NewRule("bad_skill")
	ErrBadSkillTimer      = NewRule("bad_skill_timer")
	ErrBadSkillBar        = NewRule("bad_skill_bar")
)
