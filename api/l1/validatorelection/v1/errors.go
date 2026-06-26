package validatorelectionv1

import errorsmod "cosmossdk.io/errors"

var (
	ErrInvalidParams    = errorsmod.Register("validator-election", 2, "invalid validator election params")
	ErrInvalidWindow    = errorsmod.Register("validator-election", 3, "invalid election window")
	ErrUnauthorized     = errorsmod.Register("validator-election", 4, "unauthorized validator election operation")
	ErrApplicationNotFound = errorsmod.Register("validator-election", 5, "candidate application not found")
	ErrExitNotFound     = errorsmod.Register("validator-election", 6, "pending exit not found")
	ErrFrozenStakeNotFound = errorsmod.Register("validator-election", 7, "frozen stake not found")
	ErrDuplicateEpoch   = errorsmod.Register("validator-election", 8, "election already committed for this epoch")
	ErrNotFound         = errorsmod.Register("validator-election", 9, "validator election resource not found")
)
