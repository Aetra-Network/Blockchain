package configv1

import errorsmod "cosmossdk.io/errors"

var (
	ErrInvalidParams      = errorsmod.Register("config", 2, "invalid config params")
	ErrUnauthorized       = errorsmod.Register("config", 3, "unauthorized config operation")
	ErrNotFound           = errorsmod.Register("config", 4, "config entry or change not found")
	ErrDuplicate          = errorsmod.Register("config", 5, "config change already exists")
	ErrChangeNotPending   = errorsmod.Register("config", 6, "config change is not pending")
	ErrChangeAlreadyExecuted = errorsmod.Register("config", 7, "config change already executed")
	ErrInvalidAuthorityPath = errorsmod.Register("config", 8, "invalid authority path")
	ErrConfigChangeFailed = errorsmod.Register("config", 9, "config change execution failed")
)