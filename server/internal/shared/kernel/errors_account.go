package kernel

// The account module's rules.
var (
	ErrAccountNotFound  = NewRule("account_not_found")
	ErrUsernameTaken    = NewRule("username_taken")
	ErrUnauthenticated  = NewRule("unauthenticated")
	ErrLoginRateLimited = NewRule("login_rate_limited")
	ErrBadUsername      = NewRule("bad_username")
	ErrBadPassword      = NewRule("bad_password")
	ErrBadControlPreset = NewRule("bad_control_preset")
	ErrSessionRace      = NewRule("session_race")
	ErrBusy             = NewRule("busy").Retryable() // password hashing is at capacity; the player tries again
)
