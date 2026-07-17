package types

import appparams "github.com/sovereign-l1/l1/app/params"

const (
	ModuleName	= "feecollector"
	StoreKey	= ModuleName
	RouterKey	= ModuleName

	CollectorModuleName	= ModuleName
	TreasuryModuleName	= "feecollector_treasury"
	ProtectionModuleName	= "feecollector_protection"

	ValidatorInsuranceModuleName	= "feecollector_validator_insurance"
	EcosystemGrantsModuleName	= "feecollector_ecosystem_grants"
	StorageRentReserveModuleName	= "feecollector_storage_rent_reserve"
	BurnModuleName			= "feecollector_burn"
	ReporterRewardsModuleName	= "feecollector_reporter_rewards"
)

var (
	ParamsKey		= []byte{0x01}
	FeeBalancesKey		= []byte{0x02}
	PendingDistributionKey	= []byte{0x03}
	FeeHistoryPrefix	= []byte{0x04}
	ProtocolIncomePolicyKey	= []byte{0x05}
	// LastBurnCapTimeKey stores the consensus block time at which the fee burn
	// cap was last accounted, as a big-endian int64 of Unix seconds. It is what
	// gives the cap a time base: the cap is a RATE (fraction of supply per year),
	// so it can only be enforced against an elapsed interval.
	LastBurnCapTimeKey	= []byte{0x06}
)

const (
	BaseDenom		= appparams.BaseDenom
	BasisPoints	uint32	= 10_000

	FeeTypeGas		= "gas"
	FeeTypeForwarding	= "forwarding"
	FeeTypeProtocol		= "protocol"
)
