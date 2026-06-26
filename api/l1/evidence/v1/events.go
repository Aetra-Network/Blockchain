package evidencev1

const (
	EventTypeSubmitEvidence       = "evidence_submit"
	EventTypeVoteEvidence         = "evidence_vote"
	EventTypeFinalizeEvidence     = "evidence_finalize"
	EventTypeCancelExpiredEvidence = "evidence_cancel_expired"

	AttributeKeyAuthority        = "authority"
	AttributeKeyEvidenceID       = "evidence_id"
	AttributeKeyAccusedValidator = "accused_validator"
	AttributeKeyReporter         = "reporter"
	AttributeKeyHeight           = "height"
)