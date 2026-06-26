package systemregistryv1

const (
	EventTypeRegisterSystemEntity = "system_registry_register_entity"
	EventTypeUpdateSystemEntity   = "system_registry_update_entity"
	EventTypePauseSystemEntity    = "system_registry_pause_entity"
	EventTypeResumeSystemEntity   = "system_registry_resume_entity"
	EventTypeDeprecateSystemEntity = "system_registry_deprecate_entity"

	AttributeKeyAuthority        = "authority"
	AttributeKeyModuleName       = "module_name"
	AttributeKeyHeight           = "height"
	AttributeKeyAllowPrivileged  = "allow_privileged_calls"
)