package validatorregistryv1

import errorsmod "cosmossdk.io/errors"

var (
	ErrInvalidParams     = errorsmod.Register("validator-registry", 2, "invalid validator registry params")
	ErrUnauthorized      = errorsmod.Register("validator-registry", 3, "unauthorized validator registry operation")
	ErrNotFound          = errorsmod.Register("validator-registry", 4, "validator registry resource not found")
	ErrValidatorNotFound = errorsmod.Register("validator-registry", 5, "validator not found")
	ErrDuplicateValidator = errorsmod.Register("validator-registry", 6, "validator already registered")
	ErrInvalidStatus     = errorsmod.Register("validator-registry", 7, "invalid validator status")
)