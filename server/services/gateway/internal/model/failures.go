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
	PickupsUnavailable   = Public{"pickups_unavailable", "pickups are not available"}
	PickupBusy           = Public{"pickup_busy", "try again in a moment"}
)
