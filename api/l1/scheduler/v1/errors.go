package schedulerv1

import errorsmod "cosmossdk.io/errors"

var (
	ErrInvalidParams   = errorsmod.Register("scheduler", 2, "invalid scheduler params")
	ErrUnauthorized    = errorsmod.Register("scheduler", 3, "unauthorized scheduler operation")
	ErrNotFound        = errorsmod.Register("scheduler", 4, "scheduler job not found")
	ErrDuplicate       = errorsmod.Register("scheduler", 5, "scheduler job already registered")
	ErrCancelled       = errorsmod.Register("scheduler", 6, "scheduler job is cancelled")
	ErrDisabled       = errorsmod.Register("scheduler", 7, "scheduler is disabled")
	ErrExecutionFailed = errorsmod.Register("scheduler", 8, "scheduler job execution failed")
)