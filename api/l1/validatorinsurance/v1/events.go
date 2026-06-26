package validatorinsurancev1

const (
	EventTypeFundValidatorInsurance    = "validator_insurance_fund"
	EventTypeWithdrawValidatorInsurance = "validator_insurance_withdraw"
	EventTypeSubmitInsuranceClaim      = "validator_insurance_submit_claim"
	EventTypeResolveInsuranceClaim     = "validator_insurance_resolve_claim"

	AttributeKeyAuthority        = "authority"
	AttributeKeyValidatorAddress = "validator_address"
	AttributeKeyClaimID          = "claim_id"
	AttributeKeyHeight           = "height"
)