package kernel

// The pvp module's rules, and the world's refusals in a match.
var (
	ErrPvpNotPromoted   = NewRule("pvp_not_promoted")
	ErrPvpAlreadyQueued = NewRule("pvp_already_queued")
	ErrPvpNotQueued     = NewRule("pvp_not_queued")
	ErrPvpPartyTooBig   = NewRule("pvp_party_too_big")
	ErrPvpPartyNotReady = NewRule("pvp_party_not_ready")
	ErrPvpInMatch       = NewRule("pvp_in_match")
	ErrPvpOfferGone     = NewRule("pvp_offer_gone")
	ErrPvpNotInMatch    = NewRule("pvp_not_in_match")
	ErrMatchNotStarted  = NewRule("match_not_started")
	ErrBadPvpBuild      = NewRule("bad_pvp_build")
	ErrPvpUnknownMode   = NewRule("pvp_unknown_mode")
)
