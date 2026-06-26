package singlenominatorpoolv1

const (
	EventTypeCreateSingleNominatorPool   = "single_nominator_pool_create"
	EventTypeDepositSingleNominator      = "single_nominator_pool_deposit"
	EventTypeWithdrawSingleNominator     = "single_nominator_pool_withdraw"
	EventTypeClaimSingleNominatorRewards = "single_nominator_pool_claim_rewards"
	EventTypeEmergencyLockSingleNominator = "single_nominator_pool_emergency_lock"
	EventTypeChangeSingleNominatorValidator = "single_nominator_pool_change_validator"

	AttributeKeyAuthority    = "authority"
	AttributeKeyPoolAddress = "pool_address"
	AttributeKeyHeight      = "height"
)