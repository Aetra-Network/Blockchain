package types

import appparams "github.com/sovereign-l1/l1/app/params"

const (
	ModuleName	= "fees"
	StoreKey	= ModuleName
	RouterKey	= ModuleName
)

var (
	ParamsKey		= []byte{0x01}
	ProtocolFeeStateKey	= []byte{0x02}
	BlockTxCountKey		= []byte{0x03}
	SenderTxCountPrefix	= []byte{0x04}
	// CongestionStateKey stores the last-finalized block_utilization_bps (KV-backed, deterministic).
	CongestionStateKey	= []byte{0x05}
)

const (
	BondDenom		= appparams.BaseDenom
	MaxAllowedFeeDenomsV1	= 1
	MaxMinFeeAmountV1	= "1000000000000000000"
	MaxFeeAmountV1		= "1000000000000000000"
	PrototypeBaseFeeAmount	= "1000000"
	PrototypeBaseFeeCoin	= PrototypeBaseFeeAmount + BondDenom
	PrototypeMinGasPriceV1	= "0" + BondDenom

	// TargetTransferFeeNaet is the governance anchor for a normal transfer fee (Requirement 1.2).
	// 500_000_000 naet == 0.5 AET (~$0.005 at the reference peg). Storage rent,
	// reputation adjustments, and congestion surcharges are separate additive
	// components on top of this average, never folded into it.
	TargetTransferFeeNaet	= int64(500_000_000)

	// DefaultTargetTransferFeeAmount is the string form used in genesis params.
	DefaultTargetTransferFeeAmount	= "500000000"

	// Reputation score boundaries.
	// Neutral reputation is 5000 bps (out of 10000).
	ReputationNeutralScore	= uint32(5_000)
	// Default caps as governance params (in naet).
	// Worst reputation adds at most 0.25 AET; best reputation subtracts at
	// most 0.1 AET, which can never zero the fee because the flat transfer
	// anchor alone is 0.4 AET.
	DefaultLowReputationPremiumCap		= "250000000"	// 0.25 AET
	DefaultHighReputationDiscountCap	= "100000000"	// 0.10 AET

	// Default storage rent side-effect budget per state-creating tx (naet).
	DefaultStorageRentSideEffectsNaet	= "50000000"	// 0.05 AET

	// Default gas/byte/message fee components (naet). Sized so a typical bank
	// transfer (~200k gas, ~330 bytes, 1 message) lands at the 0.5 AET target:
	// 0.4 AET anchor + 0.05 AET gas + ~0.033 AET bytes + 0.015 AET message.
	DefaultBaseGasFeePerGas	= "250"		// naet per gas unit
	DefaultByteFeeNaet	= "100000"	// naet per tx byte
	DefaultMessageFeeNaet	= "15000000"	// naet per message
)
