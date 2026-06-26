package singlenominatorpoolv1

import errorsmod "cosmossdk.io/errors"

var (
	ErrInvalidParams     = errorsmod.Register("single-nominator-pool", 2, "invalid single nominator pool params")
	ErrUnauthorized      = errorsmod.Register("single-nominator-pool", 3, "unauthorized single nominator pool operation")
	ErrNotFound          = errorsmod.Register("single-nominator-pool", 4, "single nominator pool resource not found")
	ErrPoolNotFound      = errorsmod.Register("single-nominator-pool", 5, "single nominator pool not found")
	ErrRewardsNotFound   = errorsmod.Register("single-nominator-pool", 6, "single nominator rewards not found")
)