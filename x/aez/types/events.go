package types

// Routing-table events.
//
// The routing table pins the chain's zone layout, and Phase 2 makes it
// governable. Two moments must be observable off-chain without replaying state:
// when a table is STAGED (a proposal executed and a swap is now scheduled) and
// when it is ACTIVATED (the swap actually happened, in a BeginBlocker, with no
// transaction to attribute it to).
//
// The activation event matters more than the staging one. Staging is a tx: it
// has a hash, a proposal id, and a result an indexer can already see. Activation
// is not -- it happens between blocks, triggered by height alone, potentially
// thousands of blocks after the tx that scheduled it. Without an event the only
// evidence that the table moved is a diff of two queries, which nothing emits
// and no indexer takes.
const (
	// EventTypeStageRoutingTable is emitted when MsgUpdateRoutingTable
	// accepts a table and schedules it for a future epoch boundary.
	EventTypeStageRoutingTable	= "aez_stage_routing_table"

	// EventTypeActivateRoutingTable is emitted from the BeginBlocker at the
	// exact height a pending table becomes current.
	EventTypeActivateRoutingTable	= "aez_activate_routing_table"
)

const (
	// AttributeKeyVersion is the routing table version.
	AttributeKeyVersion	= "version"

	// AttributeKeyEpoch is the routing epoch the table belongs to.
	AttributeKeyEpoch	= "epoch"

	// AttributeKeyActivationHeight is the height the table becomes current.
	AttributeKeyActivationHeight	= "activation_height"

	// AttributeKeyTableHash is the hex-encoded canonical table hash. It is
	// what lets an observer confirm the table that activated is byte-for-byte
	// the table that was staged.
	AttributeKeyTableHash	= "table_hash"

	// AttributeKeyPreviousVersion is the version the activation replaced.
	AttributeKeyPreviousVersion	= "previous_version"

	// AttributeKeyAuthority is the authority that staged the table.
	AttributeKeyAuthority	= "authority"
)
