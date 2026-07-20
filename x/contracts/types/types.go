package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	coretypes "github.com/sovereign-l1/l1/x/aetracore/types"
	"github.com/sovereign-l1/l1/x/internal/prototype"
)

const (
	EventTypeCodeStored           = "contracts.code_stored"
	EventTypeContractInstantiated = "contracts.instantiated"
	EventTypeContractExecuted     = "contracts.executed"

	ErrInvalidParams    = "contracts_invalid_params"
	ErrInvalidGenesis   = "contracts_invalid_genesis"
	ErrContractNotFound = "contracts_not_found"
	ErrInvalidBytecode  = "contracts_invalid_bytecode"
	ErrExecutionFailed  = "contracts_execution_failed"
)

// MaxStorageRentPerByteBlock caps the governance-settable per-byte-per-block
// storage rent rate. StorageRentPerByteBlock is multiplied by contract storage
// (bounded by MaxContractStorageBytes) and, in chargeRent, additionally by the
// elapsed block span. Without an upper bound a large governance rate could wrap
// these uint64 multiplications (silent overflow -> rent underpayment); the
// keeper now guards every multiply with checkedMul, and this cap keeps valid
// governance parameters far from the overflow boundary. 1<<30 (~1.07e9 base
// units per stored byte per block) is astronomically larger than any realistic
// rent, yet with the default MaxContractStorageBytes (64 MiB = 2^26) the
// storage*rate product stays at ~2^56, leaving 2^8 of headroom for the
// block-span factor.
const MaxStorageRentPerByteBlock uint64 = 1 << 30

type Params struct {
	Authority                string `protobuf:"bytes,1,opt,name=authority,proto3" json:"authority,omitempty"`
	Enabled                  bool   `protobuf:"varint,2,opt,name=enabled,proto3" json:"enabled,omitempty"`
	MaxCodeBytes             uint64 `protobuf:"varint,3,opt,name=max_code_bytes,json=maxCodeBytes,proto3" json:"max_code_bytes,omitempty"`
	MaxContractStorageBytes  uint64 `protobuf:"varint,4,opt,name=max_contract_storage_bytes,json=maxContractStorageBytes,proto3" json:"max_contract_storage_bytes,omitempty"`
	MaxGasPerExecution       uint64 `protobuf:"varint,5,opt,name=max_gas_per_execution,json=maxGasPerExecution,proto3" json:"max_gas_per_execution,omitempty"`
	StorageRentPerByteBlock  uint64 `protobuf:"varint,6,opt,name=storage_rent_per_byte_block,json=storageRentPerByteBlock,proto3" json:"storage_rent_per_byte_block,omitempty"`
	MaxInitDataBytes         uint64 `protobuf:"varint,7,opt,name=max_init_data_bytes,json=maxInitDataBytes,proto3" json:"max_init_data_bytes,omitempty"`
	MaxStateInitSaltBytes    uint64 `protobuf:"varint,8,opt,name=max_state_init_salt_bytes,json=maxStateInitSaltBytes,proto3" json:"max_state_init_salt_bytes,omitempty"`
	MaxStateInitDependencies uint32 `protobuf:"varint,9,opt,name=max_state_init_dependencies,json=maxStateInitDependencies,proto3" json:"max_state_init_dependencies,omitempty"`
	// MaxInternalMessageGasPerBlock bounds the AVM gas the EndBlock drain may
	// spend autonomously delivering queued internal messages in a single
	// block. Zero disables autonomous delivery (messages then only deliver via
	// an explicit MsgReceiveInternalMessage tx), which keeps genesis fixtures
	// built before this field existed behaving exactly as before.
	MaxInternalMessageGasPerBlock uint64 `protobuf:"varint,10,opt,name=max_internal_message_gas_per_block,json=maxInternalMessageGasPerBlock,proto3" json:"max_internal_message_gas_per_block,omitempty"`
	// MinUpgradeDelay is the minimum number of blocks that must elapse between
	// ScheduleContractUpgrade recording a pending code upgrade and
	// ApplyScheduledUpgrade being allowed to apply it (see
	// ContractLifecycleActionUpgradeMigrate / keeper.ScheduleContractUpgrade /
	// keeper.ApplyScheduledContractUpgrade). Zero means a scheduled upgrade may
	// be applied in the same block it was scheduled in (no enforced delay);
	// this keeps genesis fixtures built before this field existed behaving
	// exactly as before (DefaultParams sets a positive default explicitly).
	// This has no effect on the pre-existing immediate UpgradeContractCode
	// Msg route, which is unchanged and does not go through a timelock.
	MinUpgradeDelay uint64 `protobuf:"varint,11,opt,name=min_upgrade_delay,json=minUpgradeDelay,proto3" json:"min_upgrade_delay,omitempty"`
	// StorageRentRetentionBlocks is the minimum number of blocks a contract
	// must remain frozen (Contract.Status Frozen/FrozenLimited) before it
	// becomes eligible for archival deletion via MsgDeleteExpiredContract.
	// Stamped onto Contract.DeletionEligibilityHeight the first time a
	// contract transitions into a frozen status (see chargeContractRentAt /
	// storageRentFrozenStatus in x/contracts/keeper/keeper.go), mirroring
	// x/storage-rent's RentParams.RetentionBlocks precedent. Zero would make
	// a contract deletable in the very same block it freezes, giving the
	// owner no real window to pay off the debt and unfreeze -- so
	// DefaultParams sets a positive default, matching MinUpgradeDelay's own
	// reasoning above.
	StorageRentRetentionBlocks uint64 `protobuf:"varint,12,opt,name=storage_rent_retention_blocks,json=storageRentRetentionBlocks,proto3" json:"storage_rent_retention_blocks,omitempty"`
}

// MaxStorageRentRetentionBlocks caps the governance-settable retention
// window, for the same reason MaxStorageRentPerByteBlock caps the rent rate
// above: DeletionEligibilityHeight is computed as
// height + StorageRentRetentionBlocks with an overflow-checked add (see
// chargeContractRentAt), and an astronomically large but still-bounded cap
// keeps governance parameters far from the uint64 overflow boundary while
// leaving room for any realistic retention policy (even 1<<40 blocks is
// centuries at any plausible block time).
const MaxStorageRentRetentionBlocks uint64 = 1 << 40

type GenesisState struct {
	Params    Params
	State     State
	StateRoot string
}

type MsgStoreCode struct {
	Authority string `protobuf:"bytes,1,opt,name=authority,proto3" json:"authority,omitempty"`
	Bytecode  []byte `protobuf:"bytes,2,opt,name=bytecode,proto3" json:"bytecode,omitempty"`
	CodeHash  string `protobuf:"bytes,3,opt,name=code_hash,json=codeHash,proto3" json:"code_hash,omitempty"`
	CodeBytes uint64 `protobuf:"varint,4,opt,name=code_bytes,json=codeBytes,proto3" json:"code_bytes,omitempty"`
	// ManifestBytes is an OPTIONAL JSON-encoded avm.InterfaceManifest
	// describing the deployed module's callable surface (methods, async
	// handlers, get-methods, CLI/SDK/wallet bindings). When present, StoreCode
	// re-derives avm.InterfaceHash(manifest) and verifies it against the
	// decoded module's own MetadataHash (avm.VerifyInterface) before
	// accepting the code record -- rejecting a manifest that doesn't actually
	// describe the deployed bytecode. Empty means no manifest is published for
	// this code (ContractManifest query then reports not-found), which keeps
	// every StoreCode call built before this field existed behaving exactly
	// as before.
	ManifestBytes []byte `protobuf:"bytes,5,opt,name=manifest_bytes,json=manifestBytes,proto3" json:"manifest_bytes,omitempty"`
}

type StoreCodeResponse struct {
	CodeID    string `protobuf:"bytes,1,opt,name=code_id,json=codeId,proto3" json:"code_id,omitempty"`
	StateRoot string `protobuf:"bytes,2,opt,name=state_root,json=stateRoot,proto3" json:"state_root,omitempty"`
}

type QueryContractRequest struct {
	ContractAddress string     `protobuf:"bytes,1,opt,name=contract_address,json=contractAddress,proto3" json:"contract_address,omitempty"`
	ChainID         string     `protobuf:"bytes,2,opt,name=chain_id,json=chainId,proto3" json:"chain_id,omitempty"`
	Namespace       string     `protobuf:"bytes,3,opt,name=namespace,proto3" json:"namespace,omitempty"`
	Deployer        string     `protobuf:"bytes,4,opt,name=deployer,proto3" json:"deployer,omitempty"`
	StateInit       *StateInit `protobuf:"bytes,5,opt,name=state_init,json=stateInit,proto3" json:"state_init,omitempty"`
}

type QueryContractResponse struct {
	ContractAddress string   `protobuf:"bytes,1,opt,name=contract_address,json=contractAddress,proto3" json:"contract_address,omitempty"`
	StateRoot       string   `protobuf:"bytes,2,opt,name=state_root,json=stateRoot,proto3" json:"state_root,omitempty"`
	Found           bool     `protobuf:"varint,3,opt,name=found,proto3" json:"found,omitempty"`
	Virtual         bool     `protobuf:"varint,4,opt,name=virtual,proto3" json:"virtual,omitempty"`
	Contract        Contract `protobuf:"bytes,5,opt,name=contract,proto3" json:"contract"`
	// Status is the canonical lifecycle status of the queried address,
	// defined for every address: for a live contract it mirrors
	// Contract.Status (active/frozen/frozen_limited/archived/deleted); for a
	// derivable-but-undeployed address it is "uninit"; otherwise
	// "nonexistent". Explorers and wallets can render it without inspecting
	// Found/Virtual.
	Status string `protobuf:"bytes,6,opt,name=status,proto3" json:"status,omitempty"`
}

// QueryContractManifestRequest looks up the OPTIONAL AVM interface manifest
// published for a stored code (see MsgStoreCode.ManifestBytes / CodeRecord.
// ManifestBytes). Keyed by CodeID rather than a live contract address since
// the manifest describes the CODE's callable surface, shared by every
// contract instantiated from it.
type QueryContractManifestRequest struct {
	CodeID string `protobuf:"bytes,1,opt,name=code_id,json=codeId,proto3" json:"code_id,omitempty"`
}

// QueryContractManifestResponse carries the raw JSON-encoded
// avm.InterfaceManifest bytes exactly as stored (already verified against
// the code's MetadataHash at StoreCode time -- see avm.VerifyInterface), or
// Found=false if the code exists but was stored without a manifest, or does
// not exist at all.
type QueryContractManifestResponse struct {
	Found         bool   `protobuf:"varint,1,opt,name=found,proto3" json:"found,omitempty"`
	ManifestBytes []byte `protobuf:"bytes,2,opt,name=manifest_bytes,json=manifestBytes,proto3" json:"manifest_bytes,omitempty"`
}

type MsgServer interface {
	StoreCode(MsgStoreCode) (StoreCodeResponse, error)
	DeployContract(MsgDeployContract) (InstantiateContractResponse, error)
	ExecuteExternal(MsgExecuteExternal) (ExecuteContractResponse, error)
	ExecuteInternal(MsgExecuteInternal) (InternalMessage, error)
	SendInternalMessage(MsgSendInternalMessage) (InternalMessage, error)
	UpgradeContractCode(MsgUpgradeContractCode) (ContractReceipt, error)
	MigrateContractState(MsgMigrateContractState) (ContractReceipt, error)
	SetContractAdmin(MsgSetContractAdmin) (ContractReceipt, error)
	DisableContractUpgrades(MsgDisableContractUpgrades) (ContractReceipt, error)
	ScheduleContractUpgrade(MsgScheduleContractUpgrade) (MsgScheduleContractUpgradeResponse, error)
	ApplyScheduledUpgrade(MsgApplyScheduledUpgrade) (ContractReceipt, error)
	UpdateContractParams(MsgUpdateContractParams) error
	SubmitSecurityAttestation(MsgSubmitSecurityAttestation) (MsgSubmitSecurityAttestationResponse, error)
	RevokeSecurityAttestation(MsgRevokeSecurityAttestation) (MsgRevokeSecurityAttestationResponse, error)
	DeleteExpiredContract(MsgDeleteExpiredContract) (ContractReceipt, error)
}

// MsgDeleteExpiredContract archives a long-frozen contract into a permanent
// tombstone (Contract.Status Deleted), force-writing off any remaining
// StorageRentDebt as documented bad debt and zeroing Balance/Data/
// StorageBytes -- satisfying ValidateDeletedContractTombstone. Only callable
// on a contract already Frozen/FrozenLimited (see
// ContractLifecycleActionArchiveDelete in lifecycle.go) whose
// DeletionEligibilityHeight (stamped at freeze time, Params.
// StorageRentRetentionBlocks after the freeze height) has passed. Mirrors
// x/storage-rent's MsgDeleteExpiredContract precedent: authority-gated
// (Params.Authorize), not permissionless -- pruning module state is a
// governance action, not something any address can trigger unilaterally.
//
// A frozen contract can still carry a nonzero Balance at deletion time --
// TopUpContract is reachable from Frozen/FrozenLimited without requiring debt
// payment or unfreezing first -- and those naet are real coins already
// resident in the storage-rent reserve module account (TopUpContract routes
// every top-up through collectRentPayment before crediting the ledger). The
// keeper does not silently zero a nonzero Balance: it sweeps it out of the
// reserve to the protocol treasury (the same SendCoinsFromModuleToModule
// convention x/identity-root's treasury sweep already established) and
// stamps the swept amount on the returned receipt's SweptBalance field, so
// the write-off has a real, auditable bank-side trail rather than an
// unexplained shortfall in the reserve account.
type MsgDeleteExpiredContract struct {
	Authority       string `protobuf:"bytes,1,opt,name=authority,proto3" json:"authority,omitempty"`
	ContractAddress string `protobuf:"bytes,2,opt,name=contract_address,json=contractAddress,proto3" json:"contract_address,omitempty"`
	Height          uint64 `protobuf:"varint,3,opt,name=height,proto3" json:"height,omitempty"`
}

// MsgDeleteExpiredContractResponse: see MsgUpgradeContractCodeResponse's doc
// comment (contract_state.go) for why this wraps ContractReceipt (defined in
// query.proto) as registry metadata only.
type MsgDeleteExpiredContractResponse struct {
	Receipt ContractReceipt `protobuf:"bytes,1,opt,name=receipt,proto3" json:"receipt"`
}

type QueryServer interface {
	Params() Params
	Code(QueryCodeRequest) (CodeRecord, bool, error)
	Codes(QueryCodesRequest) ([]CodeRecord, error)
	ContractManifest(QueryContractManifestRequest) (QueryContractManifestResponse, error)
	Contract(QueryContractRequest) (QueryContractResponse, error)
	ContractGet(QueryContractGetRequest) (QueryContractGetResponse, error)
	Contracts(QueryContractsRequest) ([]Contract, error)
	ContractStorage(QueryContractStorageRequest) ([]ContractStorageEntry, error)
	ContractReceipts(QueryContractReceiptsRequest) ([]ContractReceipt, error)
	ContractQueue(QueryContractQueueRequest) ([]InternalMessage, error)
	ContractEvents(QueryContractEventsRequest) error
	ContractStateRoot(QueryContractStateRootRequest) (string, error)
	SecurityAttestations(QuerySecurityAttestationsRequest) ([]ContractSecurityAttestation, error)
	SecurityBadge(QuerySecurityBadgeRequest) (ContractSecurityBadge, bool, error)
	RootContribution() (coretypes.RootContribution, error)
}

func DefaultParams() Params {
	return Params{
		Authority:                prototype.DefaultAuthority,
		Enabled:                  true,
		MaxCodeBytes:             4 * 1024 * 1024,
		MaxContractStorageBytes:  64 * 1024 * 1024,
		MaxGasPerExecution:       100_000_000,
		StorageRentPerByteBlock:  1,
		MaxInitDataBytes:         MaxContractPayloadBytes,
		MaxStateInitSaltBytes:    MaxContractSaltBytes,
		MaxStateInitDependencies: MaxContractDependencies,
		// Off by default: autonomous delivery is a new capability and must not
		// change behavior for any genesis built before it existed. Governance
		// raises this explicitly via MsgUpdateContractParams to turn it on.
		MaxInternalMessageGasPerBlock: 0,
		// 100 blocks gives affected parties (users, integrators) a real window
		// to observe a scheduled upgrade and react before it can take effect.
		MinUpgradeDelay: 100,
		// 10,000 blocks gives a frozen contract's owner a real window to pay
		// off storage rent debt and unfreeze before archival deletion becomes
		// possible, mirroring MinUpgradeDelay's reasoning above.
		StorageRentRetentionBlocks: 10_000,
	}
}

func DefaultGenesis() GenesisState {
	gs := GenesisState{Params: DefaultParams()}
	gs.State = gs.State.Normalize()
	gs.StateRoot = ComputeContractsStateRoot(gs)
	return gs
}

func (p Params) Validate() error {
	if strings.TrimSpace(p.Authority) == "" {
		return errors.New(ErrInvalidParams + ": authority is required")
	}
	if p.MaxCodeBytes == 0 {
		return errors.New(ErrInvalidParams + ": max code bytes must be positive")
	}
	if p.MaxContractStorageBytes == 0 {
		return errors.New(ErrInvalidParams + ": max contract storage bytes must be positive")
	}
	if p.StorageRentPerByteBlock > MaxStorageRentPerByteBlock {
		return errors.New(ErrInvalidParams + ": storage rent per byte block exceeds maximum")
	}
	if p.StorageRentRetentionBlocks > MaxStorageRentRetentionBlocks {
		return errors.New(ErrInvalidParams + ": storage rent retention blocks exceeds maximum")
	}
	if p.MaxGasPerExecution == 0 {
		return errors.New(ErrInvalidParams + ": max gas per execution must be positive")
	}
	if p.MaxInitDataBytes == 0 {
		return errors.New(ErrInvalidParams + ": max init data bytes must be positive")
	}
	if p.MaxStateInitSaltBytes == 0 {
		return errors.New(ErrInvalidParams + ": max state init salt bytes must be positive")
	}
	if p.MaxStateInitDependencies == 0 {
		return errors.New(ErrInvalidParams + ": max state init dependencies must be positive")
	}
	return nil
}

func (p Params) Authorize(authority string) error {
	if strings.TrimSpace(authority) != p.Authority {
		return errors.New(ErrUnauthorized + ": authority mismatch")
	}
	return nil
}

func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	if err := gs.State.Validate(gs.Params); err != nil {
		return err
	}
	if err := coretypes.ValidateHash("contracts genesis state root", gs.StateRoot); err != nil {
		return err
	}
	if gs.StateRoot != ComputeContractsStateRoot(gs) {
		return errors.New(ErrInvalidGenesis + ": state root mismatch")
	}
	return nil
}

func RootContribution(gs GenesisState) (coretypes.RootContribution, error) {
	if err := gs.Validate(); err != nil {
		return coretypes.RootContribution{}, err
	}
	return coretypes.NewRootContribution(coretypes.RootType(ModuleName), ModuleName, gs.StateRoot)
}

// ComputeContractsStateRoot computes the genesis state root, normalizing
// gs.State first. Kept as the public entry point precisely because not
// every caller has already normalized (DefaultGenesis and Validate may both
// run against a gs.State that hasn't been -- Validate in particular is
// called on a freshly-unmarshaled genesis before any RefreshStateRoot call).
// A caller that HAS already normalized (RefreshStateRoot, the hottest path
// in the module, immediately above its own explicit Normalize() call)
// should use computeContractsStateRootNormalized directly instead, to avoid
// paying for a second, redundant deep-clone-and-sort of the entire state on
// every single mutating call (design doc §8.4). Behavior for every existing
// caller is unchanged -- this function still normalizes, exactly as before.
func ComputeContractsStateRoot(gs GenesisState) string {
	gs.State = gs.State.Normalize()
	return computeContractsStateRootNormalized(gs)
}

// computeContractsStateRootNormalized is ComputeContractsStateRoot's core,
// skipping the Normalize() call: the caller MUST already have a normalized
// gs.State (RefreshStateRoot is the only intended caller). See
// ComputeContractsStateRoot's doc comment for why the public function keeps
// normalizing for its own (not-necessarily-pre-normalized) callers.
func computeContractsStateRootNormalized(gs GenesisState) string {
	stateJSON, err := json.Marshal(gs.State)
	if err != nil {
		panic(err)
	}
	return coretypes.DeterministicEmptyRootCommitment(coretypes.RootType(ModuleName), fmt.Sprintf(
		"authority=%s/enabled=%t/code=%020d/storage=%020d/gas=%020d/rent=%020d/init=%020d/salt=%020d/deps=%010d/upgradedelay=%020d/retention=%020d/state=%s",
		gs.Params.Authority,
		gs.Params.Enabled,
		gs.Params.MaxCodeBytes,
		gs.Params.MaxContractStorageBytes,
		gs.Params.MaxGasPerExecution,
		gs.Params.StorageRentPerByteBlock,
		gs.Params.MaxInitDataBytes,
		gs.Params.MaxStateInitSaltBytes,
		gs.Params.MaxStateInitDependencies,
		gs.Params.MinUpgradeDelay,
		gs.Params.StorageRentRetentionBlocks,
		string(stateJSON),
	))
}

func ValidateContractAddress(address string) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return errors.New(ErrContractNotFound + ": contract address is required")
	}
	return ValidateUserFacingAEAddress("contract address", address)
}
