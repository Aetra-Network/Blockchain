package types

const (
	NativeDenom                   = "naet"
	CanonicalEncodingVersion      = byte(1)
	CurrentAppVersion             = uint32(1)
	CurrentStateVersion           = uint32(1)
	SignatureSchemeEd25519        = "ed25519-aetra-v1"
	SignatureObjectState          = "channel_state"
	SignatureObjectClaim          = "unidirectional_claim"
	SignatureObjectDelta          = "async_delta"
	SignatureObjectPromise        = "conditional_promise"
	SignatureObjectGossip         = "payment_gossip"
	SignatureObjectLiquidity      = "liquidity_reservation"
	SignatureObjectRoutingFee     = "routing_fee_policy"
	SignatureObjectVirtual        = "virtual_channel"
	SignatureObjectVirtualReserve = "virtual_reservation"
	SignatureObjectVirtualClose   = "virtual_close"
	DefaultDisputePeriod          = uint64(16)
	DefaultOpeningFee             = "1"
	MaxDisputeExtensions          = uint32(2)
	MinCloseDelay                 = uint64(1)
	MaxCloseDelay                 = uint64(10_000)
	MinChallengePeriod            = uint64(1)
	MaxChallengePeriod            = uint64(20_000)
	MaxParticipants               = 8
	MaxConditionsPerState         = 128
	MaxParentChannels             = 16
	MaxSettlementBatchOps         = 256
	MaxRoutingHops                = 16
	MaxTokenLength                = 128
	MaxSettlementFeeFraction      = int64(10_000)
	MaxPenaltyRouteBps            = uint32(10_000)
	DefaultGossipTTL              = uint64(512)
	InvalidGossipPenalty          = int64(25)
	DefaultTimeoutMargin          = uint64(16)
	DefaultReplayHorizon          = uint64(100_000)
	SignerIsolationProcess        = "process"
	SignerIsolationHardware       = "hardware"
)
