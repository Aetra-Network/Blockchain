package contractsv1

import errorsmod "cosmossdk.io/errors"

var (
	ErrInvalidParams      = errorsmod.Register("contracts", 2, "invalid contracts params")
	ErrUnauthorized       = errorsmod.Register("contracts", 3, "unauthorized contracts operation")
	ErrNotFound           = errorsmod.Register("contracts", 4, "code, contract, or receipt not found")
	ErrDuplicate          = errorsmod.Register("contracts", 5, "code or contract already exists")
	ErrInsufficientFunds  = errorsmod.Register("contracts", 6, "insufficient funds")
	ErrGasLimitExceeded   = errorsmod.Register("contracts", 7, "gas limit exceeded")
	ErrExecutionFailed    = errorsmod.Register("contracts", 8, "contract execution failed")
	ErrInvalidBytecode    = errorsmod.Register("contracts", 9, "invalid bytecode")
	ErrContractDisabled   = errorsmod.Register("contracts", 10, "contract is disabled")
	ErrStorageRentDebt    = errorsmod.Register("contracts", 11, "storage rent debt")
)