package validatorelectionv1

const (
	EventTypeApplyForValidatorSet  = "validator_election_apply"
	EventTypeWithdrawApplication   = "validator_election_withdraw"
	EventTypeCommitElection        = "validator_election_commit"
	EventTypeFinalizeElection      = "validator_election_finalize"
	EventTypeRequestValidatorExit  = "validator_election_request_exit"
	EventTypeCancelValidatorExit   = "validator_election_cancel_exit"

	AttributeKeyAuthority    = "authority"
	AttributeKeyEpoch        = "epoch"
	AttributeKeyHeight       = "height"
	AttributeKeyOperator     = "operator_address"
)
