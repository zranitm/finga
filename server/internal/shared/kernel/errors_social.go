package kernel

// The social module's rules.
var (
	ErrNotPartyLeader  = NewRule("not_party_leader")
	ErrPartyFull       = NewRule("party_full")
	ErrAlreadyInParty  = NewRule("already_in_party")
	ErrTargetInParty   = NewRule("target_in_party")
	ErrNoParty         = NewRule("no_party")
	ErrNotPartyMember  = NewRule("not_party_member")
	ErrTargetNotOnMap  = NewRule("target_not_on_map")
	ErrAlreadyInvited  = NewRule("already_invited")
	ErrInviteExpired   = NewRule("invite_expired")
	ErrInviteSelf      = NewRule("invite_self")
	ErrChatEmpty       = NewRule("chat_empty")
	ErrChatTooLong     = NewRule("chat_too_long")
	ErrBadChatChannel  = NewRule("bad_chat_channel")
	ErrChatNoParty     = NewRule("chat_no_party")
	ErrChatRateLimited = NewRule("chat_rate_limited")
)
