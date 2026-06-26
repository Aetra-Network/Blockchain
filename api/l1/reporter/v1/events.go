package reporterv1

const (
	EventTypeRegisterReporter    = "reporter_register"
	EventTypeBondReporter        = "reporter_bond"
	EventTypeUnbondReporter      = "reporter_unbond"
	EventTypeSubmitReport        = "reporter_submit_report"
	EventTypeClaimReporterReward = "reporter_claim_reward"

	AttributeKeyAuthority       = "authority"
	AttributeKeyReporterAddress = "reporter_address"
	AttributeKeyReportID        = "report_id"
	AttributeKeyHeight          = "height"
)