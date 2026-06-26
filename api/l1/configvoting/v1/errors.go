package configvotingv1

import errorsmod "cosmossdk.io/errors"

var (
	ErrInvalidParams    = errorsmod.Register("config-voting", 2, "invalid config voting params")
	ErrUnauthorized      = errorsmod.Register("config-voting", 3, "unauthorized config voting operation")
	ErrNotFound          = errorsmod.Register("config-voting", 4, "config voting resource not found")
	ErrProposalNotFound = errorsmod.Register("config-voting", 5, "config proposal not found")
	ErrEmptyRequest      = errorsmod.Register("config-voting", 6, "empty request")
)