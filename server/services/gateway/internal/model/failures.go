package model

// The failures the gateway raises itself.
var (
	MalformedBody    = Public{"malformed_body", "malformed request body"}
	MissingField     = Public{"missing_field", "missing a required field"}
	BodyTooLarge     = Public{"body_too_large", "request body too large"}
	BodyTimeout      = Public{"body_timeout", "request body not received in time"}
	NoSuchEndpoint   = Public{"no_such_endpoint", "no such endpoint"}
	MethodNotAllowed = Public{"method_not_allowed", "method not allowed"}
	RateLimited      = Public{"rate_limited", "slow down"}
	NotImplemented   = Public{"not_implemented", "not available yet"}
	Unauthenticated  = Public{"unauthenticated", "invalid credentials or session"}
	BadID            = Public{"bad_id", "malformed ID"}
	UnknownClass     = Public{"unknown_class", "unknown class"}
	Internal         = Public{"internal", "something went wrong"}
	Unavailable      = Public{"unavailable", "the server is busy; try again"}
)

// The services' failures, as the gateway words them; clients/errmap maps each service reason to one.
var (
	CharacterNotFound    = Public{"character_not_found", "character not found"}
	NameTaken            = Public{"name_taken", "that name is taken"}
	BadCharacterName     = Public{"bad_character_name", "name must be 3 to 12 letters and digits, starting with a letter"}
	ClassUnavailable     = Public{"class_unavailable", "that class is not available"}
	CharacterSlotsFull   = Public{"character_slots_full", "this account already has the most characters allowed"}
	AccountNotFound      = Public{"account_not_found", "account not found"}
	UsernameTaken        = Public{"username_taken", "that username is taken"}
	LoginRateLimited     = Public{"login_rate_limited", "too many login attempts, try again in a minute"}
	BadUsername          = Public{"bad_username", "username must be 3 to 16 letters, digits or underscores"}
	BadPassword          = Public{"bad_password", "password must be 8 to 128 characters"}
	BadControlPreset     = Public{"bad_control_preset", "control preset must be classic or wasd"}
	SessionRace          = Public{"session_race", "another login is in progress, try again"}
	ItemNotFound         = Public{"item_not_found", "item not found"}
	SlotTaken            = Public{"slot_taken", "that slot is taken"}
	SlotHoldsOtherItem   = Public{"slot_holds_other_item", "that slot holds a different item"}
	StackFull            = Public{"stack_full", "that stack is full"}
	InventoryFull        = Public{"inventory_full", "your inventory is full"}
	IdempotencyKeyReused = Public{"idempotency_key_reused", "that request was already made with other arguments"}
	BadQuantity          = Public{"bad_quantity", "quantity must be at least 1 and no more than the item holds"}
	BadSlot              = Public{"bad_slot", "slot must be within the tab"}
	WrongTab             = Public{"wrong_tab", "an item stays in its own tab"}
	UnknownItem          = Public{"unknown_item", "unknown item"}
	ItemWorn             = Public{"item_worn", "unequip it first"}
	LevelTooLow          = Public{"level_too_low", "your level is too low for that item"}
	StatsTooLow          = Public{"stats_too_low", "your stats are too low for that item"}
	WrongClass           = Public{"wrong_class", "your class can't use that item"}
	WrongEquipSlot       = Public{"wrong_equip_slot", "that item doesn't go there"}
	NotEquipment         = Public{"not_equipment", "only equipment can be worn"}
	NothingWorn          = Public{"nothing_worn", "nothing is worn there"}
	NotEnoughItems       = Public{"not_enough_items", "you don't have everything it asks for"}
	CharacterDead        = Public{"character_dead", "you can't do that while dead"}
	NotEnoughPoints      = Public{"not_enough_points", "you don't have that many points"}
	PointsChanged        = Public{"points_changed", "your points changed; try again"}
	NoFreeReset          = Public{"no_free_reset", "your free reset is used"}
	BadPoints            = Public{"bad_points", "spend at least one point, and none below zero"}
	NpcOutOfReach        = Public{"npc_out_of_reach", "move closer"}
	QuestUnavailable     = Public{"quest_unavailable", "that quest isn't offered here"}
	QuestConditionsUnmet = Public{"quest_conditions_unmet", "you don't meet that quest's conditions yet"}
	QuestAlreadyActive   = Public{"quest_already_active", "you have that quest already"}
	QuestAlreadyFinished = Public{"quest_already_finished", "you've finished that quest"}
	TooManyQuests        = Public{"too_many_quests", "you have 20 quests already"}
	QuestNotReady        = Public{"quest_not_ready", "that quest isn't finished yet"}
	NotPartyLeader       = Public{"not_party_leader", "only the party leader can do that"}
	PartyFull            = Public{"party_full", "the party is full"}
	AlreadyInParty       = Public{"already_in_party", "already in a party"}
	NoParty              = Public{"no_party", "not in a party"}
	NotPartyMember       = Public{"not_party_member", "not a member of your party"}
	TargetNotOnMap       = Public{"target_not_on_map", "that character is not on your map"}
	AlreadyInvited       = Public{"already_invited", "already invited"}
	InviteExpired        = Public{"invite_expired", "the invite has expired"}
	InviteSelf           = Public{"invite_self", "you cannot invite yourself"}
	ChatEmpty            = Public{"chat_empty", "nothing to say"}
	ChatTooLong          = Public{"chat_too_long", "a chat line is at most 120 characters"}
	BadChatChannel       = Public{"bad_chat_channel", "unknown chat channel"}
	ChatNoParty          = Public{"chat_no_party", "you have no party"}
	ChatRateLimited      = Public{"chat_rate_limited", "slow down"}
	PartyChatUnavailable = Public{"party_chat_unavailable", "party chat is not available"}
)
