package configv1

const (
	EventTypeSubmitConfigChange = "config_submit_change"
	EventTypeApproveConfigChange = "config_approve_change"
	EventTypeRejectConfigChange  = "config_reject_change"
	EventTypeExecuteConfigChange = "config_execute_change"
	EventTypeCancelConfigChange  = "config_cancel_change"

	AttributeKeyAuthority   = "authority"
	AttributeKeyChangeID    = "change_id"
	AttributeKeyReason      = "reason"
)