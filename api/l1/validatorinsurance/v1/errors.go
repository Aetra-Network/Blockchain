package validatorinsurancev1

import errorsmod "cosmossdk.io/errors"

var (
	ErrInvalidParams       = errorsmod.Register("validator-insurance", 2, "invalid validator insurance params")
	ErrUnauthorized        = errorsmod.Register("validator-insurance", 3, "unauthorized validator insurance operation")
	ErrNotFound            = errorsmod.Register("validator-insurance", 4, "validator insurance resource not found")
	ErrInsuranceNotFound   = errorsmod.Register("validator-insurance", 5, "validator insurance not found")
	ErrClaimNotFound       = errorsmod.Register("validator-insurance", 6, "validator insurance claim not found")
)