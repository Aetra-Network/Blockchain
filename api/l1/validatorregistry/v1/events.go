package validatorregistryv1

const (
	EventTypeRegisterValidator       = "validator_registry_register"
	EventTypeUpdateValidatorMetadata  = "validator_registry_update_metadata"
	EventTypeRotateConsensusKey      = "validator_registry_rotate_consensus_key"
	EventTypeUpdateWithdrawalAddress = "validator_registry_update_withdrawal_address"
	EventTypeUpdateTreasuryAddress   = "validator_registry_update_treasury_address"
	EventTypeRetireValidator         = "validator_registry_retire"
	EventTypeSetValidatorCapabilities = "validator_registry_set_capabilities"

	AttributeKeyAuthority        = "authority"
	AttributeKeyOperatorAddress = "operator_address"
	AttributeKeyHeight           = "height"
)