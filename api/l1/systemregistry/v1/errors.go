package systemregistryv1

import errorsmod "cosmossdk.io/errors"

var (
	ErrInvalidParams          = errorsmod.Register("systemregistry", 2, "invalid system registry params")
	ErrUnauthorized           = errorsmod.Register("systemregistry", 3, "unauthorized system registry operation")
	ErrNotFound               = errorsmod.Register("systemregistry", 4, "system entity not found")
	ErrDuplicate              = errorsmod.Register("systemregistry", 5, "system entity already registered")
	ErrModulePaused           = errorsmod.Register("systemregistry", 6, "module is paused")
	ErrModuleNotPaused        = errorsmod.Register("systemregistry", 7, "module is not paused")
	ErrInvalidModuleName      = errorsmod.Register("systemregistry", 8, "invalid module name")
	ErrInvalidCapability      = errorsmod.Register("systemregistry", 9, "invalid capability")
	ErrDeprecated             = errorsmod.Register("systemregistry", 10, "system entity is deprecated")
	ErrInvalidStateTransition = errorsmod.Register("systemregistry", 11, "invalid state transition")
)