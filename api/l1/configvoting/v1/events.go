package configvotingv1

const (
	EventTypeConfigProposalSubmitted = "config_proposal_submitted"
	EventTypeConfigProposalVoted     = "config_proposal_voted"
	EventTypeConfigProposalExecuted  = "config_proposal_executed"
	EventTypeConfigProposalVetoed    = "config_proposal_vetoed"

	AttributeKeyProposalID = "proposal_id"
	AttributeKeyAuthority  = "authority"
	AttributeKeyVoter      = "voter"
	AttributeKeyOption     = "option"
	AttributeKeyHeight     = "height"
)