package constitutionv1

const (
	EventTypeProposeAmendment = "constitution_propose_amendment"
	EventTypeVoteAmendment    = "constitution_vote_amendment"
	EventTypeExecuteAmendment = "constitution_execute_amendment"
	EventTypeCancelAmendment  = "constitution_cancel_amendment"

	AttributeKeyAuthority      = "authority"
	AttributeKeyAmendmentID    = "amendment_id"
	AttributeKeySupport        = "support"
	AttributeKeyVotingPowerBps = "voting_power_bps"
)