package actorregistryv1

import errorsmod "cosmossdk.io/errors"

var (
	ErrInvalidParams = errorsmod.Register("actor-registry", 2, "invalid actor registry params")
	ErrUnauthorized  = errorsmod.Register("actor-registry", 3, "unauthorized actor registry operation")
	ErrNotFound      = errorsmod.Register("actor-registry", 4, "actor registry resource not found")
)