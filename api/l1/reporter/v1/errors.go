package reporterv1

import errorsmod "cosmossdk.io/errors"

var (
	ErrInvalidParams = errorsmod.Register("reporter", 2, "invalid reporter params")
	ErrUnauthorized  = errorsmod.Register("reporter", 3, "unauthorized reporter operation")
	ErrNotFound       = errorsmod.Register("reporter", 4, "reporter not found")
	ErrDuplicate      = errorsmod.Register("reporter", 5, "reporter already registered")
	ErrInvalidStatus  = errorsmod.Register("reporter", 6, "invalid reporter status")
	ErrReportNotFound = errorsmod.Register("reporter", 7, "report not found")
	ErrRewardClaimed  = errorsmod.Register("reporter", 8, "reporter reward already claimed")
)