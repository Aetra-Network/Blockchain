package aetracore

import (
	"slices"

	actorregistrytypes "github.com/sovereign-l1/l1/x/actor-registry/types"
	aetraeconomicstypes "github.com/sovereign-l1/l1/x/aetra-economics/types"
	aetrastakingpolicytypes "github.com/sovereign-l1/l1/x/aetra-staking-policy/types"
	aetravalidatorscoretypes "github.com/sovereign-l1/l1/x/aetra-validator-score/types"
	aetracoretypes "github.com/sovereign-l1/l1/x/aetracore/types"
	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
	avmschedulertypes "github.com/sovereign-l1/l1/x/avm-scheduler/types"
	bridgehubtypes "github.com/sovereign-l1/l1/x/bridge-hub/types"
	configvotingtypes "github.com/sovereign-l1/l1/x/config-voting/types"
	configtypes "github.com/sovereign-l1/l1/x/config/types"
	constitutiontypes "github.com/sovereign-l1/l1/x/constitution/types"
	contractstypes "github.com/sovereign-l1/l1/x/contracts/types"
	crosschainregistrytypes "github.com/sovereign-l1/l1/x/cross-chain-registry/types"
	nativeevidencetypes "github.com/sovereign-l1/l1/x/evidence/types"
	identityroottypes "github.com/sovereign-l1/l1/x/identity-root/types"
	loadtypes "github.com/sovereign-l1/l1/x/load/types"
	meshtypes "github.com/sovereign-l1/l1/x/mesh/types"
	nativeaccounttypes "github.com/sovereign-l1/l1/x/native-account/types"
	networkingtypes "github.com/sovereign-l1/l1/x/networking/types"
	nominatorpooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
	paymentstypes "github.com/sovereign-l1/l1/x/payments/types"
	reportertypes "github.com/sovereign-l1/l1/x/reporter/types"
	routingtypes "github.com/sovereign-l1/l1/x/routing/types"
	schedulertypes "github.com/sovereign-l1/l1/x/scheduler/types"
	shardingcoordinatortypes "github.com/sovereign-l1/l1/x/sharding-coordinator/types"
	singlenominatorpooltypes "github.com/sovereign-l1/l1/x/single-nominator-pool/types"
	storagerenttypes "github.com/sovereign-l1/l1/x/storage-rent/types"
	systemregistrytypes "github.com/sovereign-l1/l1/x/system-registry/types"
	validatorelectiontypes "github.com/sovereign-l1/l1/x/validator-election/types"
	validatorinsurancetypes "github.com/sovereign-l1/l1/x/validator-insurance/types"
	validatorregistrytypes "github.com/sovereign-l1/l1/x/validator-registry/types"
)

type RoutingExecutionPoint string

const (
	// Routing remains an admission/ante-level executable spec until a coordinated
	// upgrade adds public Msg services and production persistence semantics.
	RoutingExecutionPointAnteAdmissionOnly RoutingExecutionPoint = "ANTE_ADMISSION_ONLY"
)

var prototypeModules = []string{
	aetracoretypes.ModuleName,
	loadtypes.ModuleName,
	routingtypes.ModuleName,
	meshtypes.ModuleName,
	networkingtypes.ModuleName,
	paymentstypes.ModuleName,
	configvotingtypes.ModuleName,
	schedulertypes.ModuleName,
	avmschedulertypes.ModuleName,
	actorregistrytypes.ModuleName,
	storagerenttypes.ModuleName,
	bridgehubtypes.ModuleName,
	crosschainregistrytypes.ModuleName,
	shardingcoordinatortypes.ModuleName,
}

var systemModules = []string{
	constitutiontypes.ModuleName,
	systemregistrytypes.ModuleName,
	nativeevidencetypes.ModuleName,
	reportertypes.ModuleName,
	nominatorpooltypes.ModuleName,
	singlenominatorpooltypes.ModuleName,
	validatorelectiontypes.ModuleName,
	validatorinsurancetypes.ModuleName,
	validatorregistrytypes.ModuleName,
	aetrastakingpolicytypes.ModuleName,
	aetraeconomicstypes.ModuleName,
	aetravalidatorscoretypes.ModuleName,
	configtypes.ModuleName,
	nativeaccounttypes.ModuleName,
	// x/contracts graduated out of prototypeModules: unlike the still-dormant
	// prototype set (see docs/security/phase9-aether-core-wiring-gate.md), it
	// has live Msg/Query services and, as of the EndBlock internal-message
	// drain, an audited (if default-off) EndBlocker. Genesis still ships with
	// Params.MaxInternalMessageGasPerBlock = 0, so autonomous delivery stays
	// inert until governance explicitly raises the budget.
	contractstypes.ModuleName,
	// AEZ Phase 2: x/aez graduated out of prototypeModules for the same
	// reason x/contracts did -- it now has a live Msg service
	// (MsgUpdateRoutingTable, gov-only) and an audited BeginBlocker that
	// swaps a pending routing table in at its activation height. The
	// prototype family is defined by having neither, so it no longer
	// qualifies; app/aetra_core_wiring_test.go asserts that absence for
	// every prototype module and would fail if x/aez stayed.
	//
	// Both this list and SystemStoreKeys() below are paired POSITIONALLY by
	// app/aetra_core_wiring.go, and their lengths are compared directly --
	// a mismatch is a startup panic, not a test failure. x/aez must occupy
	// the same index in both.
	//
	// It carries no module account and holds no other module's store handle
	// (I-10/I-11), and genesis still maps all 256 buckets to the core zone,
	// so nothing routes anywhere.
	aeztypes.ModuleName,
	// ANS Phase A: x/identity-root graduated out of prototypeModules exactly as
	// x/contracts and x/aez did. It now has a live Msg service (the .aet
	// collection: SendToNameCollection/PlaceBid/StartAuction/UpdatePriceTable),
	// a Query service, an EndBlocker (auction close + treasury sweep) and a real
	// bank module account. The prototype family is defined by having none of
	// these, so it no longer qualifies; app/aetra_core_wiring_test.go asserts
	// that absence for every prototype module and would fail if it stayed.
	//
	// Paired POSITIONALLY with SystemStoreKeys() below (lengths are compared
	// directly by app/aetra_core_wiring.go -- a mismatch is a startup panic), so
	// it must occupy the same index in both.
	identityroottypes.ModuleName,
}

func RoutingExecution() RoutingExecutionPoint {
	return RoutingExecutionPointAnteAdmissionOnly
}

func PrototypeModuleNames() []string {
	return slices.Clone(prototypeModules)
}

func PrototypeStoreKeys() []string {
	return []string{
		aetracoretypes.StoreKey,
		loadtypes.StoreKey,
		routingtypes.StoreKey,
		meshtypes.StoreKey,
		networkingtypes.StoreKey,
		paymentstypes.StoreKey,
		configvotingtypes.StoreKey,
		schedulertypes.StoreKey,
		avmschedulertypes.StoreKey,
		actorregistrytypes.StoreKey,
		storagerenttypes.StoreKey,
		bridgehubtypes.StoreKey,
		crosschainregistrytypes.StoreKey,
		shardingcoordinatortypes.StoreKey,
	}
}

func SystemModuleNames() []string {
	return slices.Clone(systemModules)
}

func SystemStoreKeys() []string {
	return []string{
		constitutiontypes.StoreKey,
		systemregistrytypes.StoreKey,
		nativeevidencetypes.StoreKey,
		reportertypes.StoreKey,
		nominatorpooltypes.StoreKey,
		singlenominatorpooltypes.StoreKey,
		validatorelectiontypes.StoreKey,
		validatorinsurancetypes.StoreKey,
		validatorregistrytypes.StoreKey,
		aetrastakingpolicytypes.StoreKey,
		aetraeconomicstypes.StoreKey,
		aetravalidatorscoretypes.StoreKey,
		configtypes.StoreKey,
		nativeaccounttypes.StoreKey,
		contractstypes.StoreKey,
		// Paired positionally with aeztypes.ModuleName's index in
		// systemModules above. Keep the two in lockstep.
		aeztypes.StoreKey,
		// Paired positionally with identityroottypes.ModuleName's index in
		// systemModules above. Keep the two in lockstep.
		identityroottypes.StoreKey,
	}
}
