package constitutionv1

import errorsmod "cosmossdk.io/errors"

var (
	ErrInvalidParams        = errorsmod.Register("constitution", 2, "invalid constitution params")
	ErrUnauthorized         = errorsmod.Register("constitution", 3, "unauthorized constitution operation")
	ErrNotFound             = errorsmod.Register("constitution", 4, "constitution or amendment not found")
	ErrDuplicate            = errorsmod.Register("constitution", 5, "amendment already exists")
	ErrVotingPeriodEnded    = errorsmod.Register("constitution", 6, "voting period has ended")
	ErrInsufficientVotingPower = errorsmod.Register("constitution", 7, "insufficient voting power")
	ErrAmendmentExecuted    = errorsmod.Register("constitution", 8, "amendment already executed")
	ErrAmendmentCancelled   = errorsmod.Register("constitution", 9, "amendment cancelled")
	ErrInvalidAmendmentState = errorsmod.Register("constitution", 10, "invalid amendment state")
	ErrProtectedLimitExceeded = errorsmod.Register("constitution", 11, "protected limit exceeded")
)