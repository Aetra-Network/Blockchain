package schedulerv1

const (
	EventTypeUpdateParams        = "scheduler_update_params"
	EventTypeRegisterScheduledJob = "scheduler_register_job"
	EventTypePauseScheduledJob  = "scheduler_pause_job"
	EventTypeResumeScheduledJob = "scheduler_resume_job"
	EventTypeCancelScheduledJob = "scheduler_cancel_job"
	EventTypeExecuteDueJobs     = "scheduler_execute_due_jobs"

	AttributeKeyAuthority  = "authority"
	AttributeKeyOwnerModule = "owner_module"
	AttributeKeyJobID      = "job_id"
	AttributeKeyHeight     = "height"
)