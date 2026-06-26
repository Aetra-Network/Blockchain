package evidencev1

import errorsmod "cosmossdk.io/errors"

var (
	ErrInvalidParams  = errorsmod.Register("native-evidence", 2, "invalid evidence params")
	ErrUnauthorized   = errorsmod.Register("native-evidence", 3, "unauthorized evidence operation")
	ErrNotFound       = errorsmod.Register("native-evidence", 4, "evidence not found")
	ErrDuplicate      = errorsmod.Register("native-evidence", 5, "duplicate evidence")
	ErrInvalidStatus  = errorsmod.Register("native-evidence", 6, "invalid evidence status")
	ErrVoteRejected   = errorsmod.Register("native-evidence", 7, "evidence vote rejected")
	ErrExpired        = errorsmod.Register("native-evidence", 8, "evidence expired")
)