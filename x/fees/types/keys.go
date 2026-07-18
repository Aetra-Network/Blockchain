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
	// ZoneGasConsumedPrefix keys the AEZ Phase 6 per-zone gas reserved so far
	// this block: 0x06 || zone_be4 -> [height_be8][gas_be8]. It self-resets by
	// height stamp exactly like BlockTxCountKey/SenderTxCountPrefix (a stored
	// height != the current height reads as 0), so it needs no EndBlock reset --
	// an unconditional reset write would touch the fees-store root every block
	// and break bit-identical AppHash in single-zone. It is written ONLY for
	// elastic zones, so it never appears on a single-zone chain.
	ZoneGasConsumedPrefix	= []byte{0x06}
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
	// 500_000_000 naet == 0.5 AET. Storage rent,
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
	//
	// ANS Phase B replaced this ADDITIVE premium/discount with a MULTIPLICATIVE
	// scaling of the base anchor for reputation-GATED senders (domain holders
	// and validators) -- see computeReputationMultiplierBps. The two caps are
	// retained for genesis/params compatibility but no longer feed the formula.
	DefaultLowReputationPremiumCap		= "250000000"	// 0.25 AET
	DefaultHighReputationDiscountCap	= "100000000"	// 0.10 AET

	// ANS Phase B multiplicative reputation-fee scaling. The base transfer
	// anchor of a reputation-GATED sender is multiplied by a basis-point factor
	// between the floor (best reputation, ~0.20x) and the ceiling (worst
	// reputation, ~0.80x), linear in the sender's score against the reference.
	//
	// The reference is the realistic score ceiling, NOT 10000: x/reputation's
	// ComputeIdentityScore saturates near ~850 in practice (its component caps
	// sum to stake 400 + tx 200 + contract 100 + domain 50 + uptime 100 = 850),
	// and a fresh record defaults to 100. Anchoring at 850 makes the excellent
	// (~0.20x) tier reachable; anchoring at 10000 would pin every real wallet at
	// the poor (~0.80x) end.
	DefaultReputationFeeFloorBps		= uint32(2_000)	// 0.20x, best reputation
	DefaultReputationFeeCeilBps		= uint32(8_000)	// 0.80x, worst reputation
	DefaultReputationFeeReferenceScore	= uint32(850)	// score at/above which the floor applies

	// Default storage rent side-effect budget per state-creating tx (naet).
	DefaultStorageRentSideEffectsNaet	= "50000000"	// 0.05 AET

	// Default gas/byte/message fee components (naet). Sized so a typical bank
	// transfer (~200k gas, ~330 bytes, 1 message) lands at the 0.5 AET target:
	// 0.4 AET anchor + 0.05 AET gas + ~0.033 AET bytes + 0.015 AET message.
	DefaultBaseGasFeePerGas	= "250"		// naet per gas unit
	DefaultByteFeeNaet	= "100000"	// naet per tx byte
	DefaultMessageFeeNaet	= "15000000"	// naet per message
)
