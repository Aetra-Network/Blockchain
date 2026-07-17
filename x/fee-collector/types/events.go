package types

const (
	EventTypeCollectFees		= "fee_collector_collect_fees"
	EventTypeDistributeFees		= "fee_collector_distribute_fees"
	EventTypeUpdateDistribution	= "fee_collector_update_distribution"
	// EventTypeBurnCapped is emitted when the supply-aware fee burn cap spares
	// coins from the burn and routes them to validators instead. Under the
	// calibrated economy this should only ever fire under congestion or attack,
	// so it doubles as an alert that throughput is past the design point.
	EventTypeBurnCapped		= "fee_collector_burn_capped"

	AttributeKeyAuthority	= "authority"
	AttributeKeyEpoch	= "epoch"
	AttributeKeyFeeType	= "fee_type"
	AttributeKeyAmount	= "amount"
	AttributeKeyBurn	= "burn"
	AttributeKeyTotalBurn	= "total_burn"
)
