package contractsv1

const (
	EventTypeStoreCode           = "contracts_store_code"
	EventTypeDeployContract      = "contracts_deploy_contract"
	EventTypeExecuteExternal     = "contracts_execute_external"
	EventTypeExecuteInternal     = "contracts_execute_internal"
	EventTypeSendInternalMessage = "contracts_send_internal_message"
	EventTypeUpdateParams        = "contracts_update_params"

	AttributeKeyAuthority       = "authority"
	AttributeKeyCreator         = "creator"
	AttributeKeySender          = "sender"
	AttributeKeyContractAddress = "contract_address"
	AttributeKeyCodeID          = "code_id"
	AttributeKeyCodeHash        = "code_hash"
	AttributeKeyStateRoot       = "state_root"
	AttributeKeyExitCode        = "exit_code"
	AttributeKeyGasUsed         = "gas_used"
)