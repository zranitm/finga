package model

// Every failure a client can meet, by a six-digit code (area, then case) never reused; Message is for developers.

// 10: requests, and what any service may answer.
var (
	MalformedBody    = Public{100001, "MALFORMED_BODY", "malformed request body"}
	MissingField     = Public{100002, "MISSING_FIELD", "missing a required field"}
	BodyTooLarge     = Public{100003, "BODY_TOO_LARGE", "request body too large"}
	BodyTimeout      = Public{100004, "BODY_TIMEOUT", "request body not received in time"}
	NoSuchEndpoint   = Public{100005, "NO_SUCH_ENDPOINT", "no such endpoint"}
	MethodNotAllowed = Public{100006, "METHOD_NOT_ALLOWED", "method not allowed"}
	RateLimited      = Public{100007, "RATE_LIMITED", "slow down"}
	NotImplemented   = Public{100008, "NOT_IMPLEMENTED", "not available yet"}
	BadID            = Public{100009, "BAD_ID", "malformed ID"}
	Internal         = Public{100010, "INTERNAL", "something went wrong"}
	Unavailable      = Public{100011, "UNAVAILABLE", "the server is busy; try again"}
)

// 11: accounts and sessions.
var (
	Unauthenticated  = Public{110001, "UNAUTHENTICATED", "invalid credentials or session"}
	AccountNotFound  = Public{110002, "ACCOUNT_NOT_FOUND", "account not found"}
	UsernameTaken    = Public{110003, "USERNAME_TAKEN", "that username is taken"}
	LoginRateLimited = Public{110004, "LOGIN_RATE_LIMITED", "too many login attempts, try again in a minute"}
	BadUsername      = Public{110005, "BAD_USERNAME", "username must be 3 to 16 letters, digits or underscores"}
	BadPassword      = Public{110006, "BAD_PASSWORD", "password must be 8 to 128 characters"}
	BadControlPreset = Public{110007, "BAD_CONTROL_PRESET", "control preset must be classic or wasd"}
	SessionRace      = Public{110008, "SESSION_RACE", "another login is in progress, try again"}
)

// 12: characters.
var (
	CharacterNotFound  = Public{120001, "CHARACTER_NOT_FOUND", "character not found"}
	NameTaken          = Public{120002, "NAME_TAKEN", "that name is taken"}
	BadCharacterName   = Public{120003, "BAD_CHARACTER_NAME", "name must be 3 to 12 letters and digits, starting with a letter"}
	ClassUnavailable   = Public{120004, "CLASS_UNAVAILABLE", "that class is not available"}
	CharacterSlotsFull = Public{120005, "CHARACTER_SLOTS_FULL", "this account already has the most characters allowed"}
	UnknownClass       = Public{120006, "UNKNOWN_CLASS", "unknown class"}
)

// 22: items.
var (
	ItemNotFound         = Public{220001, "ITEM_NOT_FOUND", "item not found"}
	SlotTaken            = Public{220002, "SLOT_TAKEN", "that slot is taken"}
	SlotHoldsOtherItem   = Public{220003, "SLOT_HOLDS_OTHER_ITEM", "that slot holds a different item"}
	StackFull            = Public{220004, "STACK_FULL", "that stack is full"}
	EquipTabFull         = Public{220005, "EQUIP_TAB_FULL", "the Equip tab has no free slot"}
	UseTabFull           = Public{220006, "USE_TAB_FULL", "the Use tab has no free slot"}
	EtcTabFull           = Public{220007, "ETC_TAB_FULL", "the Etc tab has no free slot"}
	IdempotencyKeyReused = Public{220008, "IDEMPOTENCY_KEY_REUSED", "that request was already made with other arguments"}
	BadQuantity          = Public{220009, "BAD_QUANTITY", "quantity must be at least 1 and no more than the item holds"}
	BadSlot              = Public{220010, "BAD_SLOT", "slot must be within the tab"}
	WrongTab             = Public{220011, "WRONG_TAB", "an item stays in its own tab"}
	UnknownItem          = Public{220012, "UNKNOWN_ITEM", "unknown item"}
	ItemWorn             = Public{220013, "ITEM_WORN", "unequip it first"}
	LevelTooLow          = Public{220014, "LEVEL_TOO_LOW", "your level is too low for that item"}
	STRTooLow            = Public{220015, "STR_TOO_LOW", "the item requires more STR than the character's own"}
	DEXTooLow            = Public{220016, "DEX_TOO_LOW", "the item requires more DEX than the character's own"}
	INTTooLow            = Public{220017, "INT_TOO_LOW", "the item requires more INT than the character's own"}
	AGITooLow            = Public{220018, "AGI_TOO_LOW", "the item requires more AGI than the character's own"}
	VITTooLow            = Public{220019, "VIT_TOO_LOW", "the item requires more VIT than the character's own"}
	WrongClass           = Public{220020, "WRONG_CLASS", "your class can't use that item"}
	WrongEquipSlot       = Public{220021, "WRONG_EQUIP_SLOT", "that item doesn't go there"}
	NotEquipment         = Public{220022, "NOT_EQUIPMENT", "only equipment can be worn"}
	NothingWorn          = Public{220023, "NOTHING_WORN", "nothing is worn there"}
	NotEnoughItems       = Public{220024, "NOT_ENOUGH_ITEMS", "you don't have everything it asks for"}
)

// 33: the world and quests.
var (
	CharacterDead        = Public{330001, "CHARACTER_DEAD", "you can't do that while dead"}
	NotEnoughPoints      = Public{330002, "NOT_ENOUGH_POINTS", "you don't have that many points"}
	PointsChanged        = Public{330003, "POINTS_CHANGED", "your points changed; try again"}
	NoFreeReset          = Public{330004, "NO_FREE_RESET", "your free reset is used"}
	BadPoints            = Public{330005, "BAD_POINTS", "spend at least one point, and none below zero"}
	NpcOutOfReach        = Public{330006, "NPC_OUT_OF_REACH", "move closer"}
	QuestUnavailable     = Public{330007, "QUEST_UNAVAILABLE", "that quest isn't offered here"}
	QuestConditionsUnmet = Public{330008, "QUEST_CONDITIONS_UNMET", "you don't meet that quest's conditions yet"}
	QuestAlreadyActive   = Public{330009, "QUEST_ALREADY_ACTIVE", "you have that quest already"}
	QuestAlreadyFinished = Public{330010, "QUEST_ALREADY_FINISHED", "you've finished that quest"}
	TooManyQuests        = Public{330011, "TOO_MANY_QUESTS", "you have the most active quests already"}
	QuestNotReady        = Public{330012, "QUEST_NOT_READY", "that quest isn't finished yet"}
)

// 44: parties and chat.
var (
	NotPartyLeader       = Public{440001, "NOT_PARTY_LEADER", "only the party leader can do that"}
	PartyFull            = Public{440002, "PARTY_FULL", "the party is full"}
	AlreadyInParty       = Public{440003, "ALREADY_IN_PARTY", "the player accepting an invite is in a party already"}
	TargetInParty        = Public{440004, "TARGET_IN_PARTY", "the character invited is in a party already"}
	NoParty              = Public{440005, "NO_PARTY", "not in a party"}
	NotPartyMember       = Public{440006, "NOT_PARTY_MEMBER", "not a member of your party"}
	TargetNotOnMap       = Public{440007, "TARGET_NOT_ON_MAP", "that character is not on your map"}
	AlreadyInvited       = Public{440008, "ALREADY_INVITED", "already invited"}
	InviteExpired        = Public{440009, "INVITE_EXPIRED", "the invite has expired"}
	InviteSelf           = Public{440010, "INVITE_SELF", "you cannot invite yourself"}
	ChatEmpty            = Public{440011, "CHAT_EMPTY", "nothing to say"}
	ChatTooLong          = Public{440012, "CHAT_TOO_LONG", "that chat line is too long"}
	BadChatChannel       = Public{440013, "BAD_CHAT_CHANNEL", "unknown chat channel"}
	ChatNoParty          = Public{440014, "CHAT_NO_PARTY", "you have no party"}
	ChatRateLimited      = Public{440015, "CHAT_RATE_LIMITED", "too many chat lines; slow down"}
	PartyChatUnavailable = Public{440016, "PARTY_CHAT_UNAVAILABLE", "party chat is not available"}
)
