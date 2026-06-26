package actorregistryv1

const (
	EventTypeRegisterActor  = "actor_registry_register"
	EventTypeUpdateActorCode = "actor_registry_update_code"
	EventTypeFreezeActor     = "actor_registry_freeze"
	EventTypeUnfreezeActor   = "actor_registry_unfreeze"
	EventTypeDeleteActor     = "actor_registry_delete"
	EventTypeMigrateActor   = "actor_registry_migrate"

	AttributeKeyAuthority = "authority"
	AttributeKeyActorID   = "actor_id"
	AttributeKeyHeight    = "height"
)