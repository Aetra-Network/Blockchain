package keeper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	corestore "cosmossdk.io/core/store"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	coretypes "github.com/sovereign-l1/l1/x/aetracore/types"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/chunk"
	"github.com/sovereign-l1/l1/x/contracts/types"
)

const storageRentReserveModule = "feecollector_storage_rent_reserve"

var storageRentBaseDenom = "naet"

// BankKeeper defines the subset of bank functionality needed by the contracts keeper.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
}

type Keeper struct {
	genesis                 types.GenesisState
	storeService            corestore.KVStoreService
	accountStatusReader     AccountStatusReader
	bankKeeper              BankKeeper
	runtimeCtx              context.Context
	storageRentRateProvider StorageRentRateProvider
}

const (
	accountStatusActive   = "active"
	accountStatusInactive = "inactive"
	accountStatusFrozen   = "frozen"
)

var genesisKey = []byte{0x01}

type AccountStatusReader interface {
	AccountStatus(context.Context, string) (string, bool, error)
}

// StorageRentRateProvider queries the active storage rent rate from the storage-rent module.
type StorageRentRateProvider interface {
	StorageRentRatePerByteBlock() uint64
}

func NewKeeper() Keeper {
	return Keeper{genesis: types.DefaultGenesis()}
}

func NewPersistentKeeper(storeService corestore.KVStoreService) Keeper {
	return Keeper{genesis: types.DefaultGenesis(), storeService: storeService}
}

func NewKeeperWithAccountStatus(reader AccountStatusReader) Keeper {
	k := NewKeeper()
	k.accountStatusReader = reader
	return k
}

func (k Keeper) WithAccountStatusReader(reader AccountStatusReader) Keeper {
	k.accountStatusReader = reader
	return k
}

func (k Keeper) WithBankKeeper(bk BankKeeper) Keeper {
	k.bankKeeper = bk
	return k
}

func (k Keeper) WithStorageRentRateProvider(provider StorageRentRateProvider) Keeper {
	k.storageRentRateProvider = provider
	return k
}

func DefaultGenesis() types.GenesisState {
	return types.DefaultGenesis()
}

func (k *Keeper) InitGenesis(gs types.GenesisState) error {
	gs = types.RefreshStateRoot(gs)
	if err := gs.Validate(); err != nil {
		return err
	}
	k.genesis = gs
	return nil
}

func (k *Keeper) InitGenesisState(ctx context.Context, gs types.GenesisState) error {
	if err := k.InitGenesis(gs); err != nil {
		return err
	}
	k.runtimeCtx = ctx
	return k.writeGenesis(ctx)
}

func (k Keeper) ExportGenesis() types.GenesisState {
	return types.RefreshStateRoot(k.genesis)
}

func (k Keeper) ExportGenesisState(ctx context.Context) (types.GenesisState, error) {
	if k.storeService == nil {
		return k.ExportGenesis(), nil
	}
	if !reflect.DeepEqual(k.genesis, types.DefaultGenesis()) {
		return k.ExportGenesis(), nil
	}
	bz, err := k.storeService.OpenKVStore(ctx).Get(genesisKey)
	if err != nil {
		return types.GenesisState{}, err
	}
	if len(bz) == 0 {
		return types.DefaultGenesis(), nil
	}
	var gs types.GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		return types.GenesisState{}, err
	}
	gs = types.RefreshStateRoot(gs)
	if err := gs.Validate(); err != nil {
		return types.GenesisState{}, err
	}
	return gs, nil
}

func (k Keeper) Params() types.Params {
	return k.genesis.Params
}

func (k Keeper) Code(req types.QueryCodeRequest) (types.CodeRecord, bool, error) {
	if req.CodeID == "" {
		return types.CodeRecord{}, false, errors.New("contract code id is required")
	}
	code, found := findCode(k.genesis.State.Codes, req.CodeID)
	return code, found, nil
}

func (k Keeper) Codes(req types.QueryCodesRequest) ([]types.CodeRecord, error) {
	if err := types.ValidateQueryPagination(req.Pagination); err != nil {
		return nil, err
	}
	codes := k.genesis.State.Normalize().Codes
	if uint32(len(codes)) > req.Pagination.Limit {
		codes = codes[:req.Pagination.Limit]
	}
	return append([]types.CodeRecord(nil), codes...), nil
}

func (k Keeper) ValidateInvariants() error {
	return k.genesis.Validate()
}

func (k Keeper) RootContribution() (coretypes.RootContribution, error) {
	return types.RootContribution(k.genesis)
}

func (k Keeper) Migrate1to2State(ctx context.Context) error {
	_, err := k.ExportGenesisState(ctx)
	return err
}

func (k *Keeper) StoreCode(msg types.MsgStoreCode) (types.StoreCodeResponse, error) {
	if !k.genesis.Params.Enabled {
		return types.StoreCodeResponse{}, errors.New(types.ErrExecutionFailed + ": module disabled")
	}
	if err := types.ValidateUserFacingAEAddress("contract code authority", msg.Authority); err != nil {
		return types.StoreCodeResponse{}, err
	}
	if err := k.ensureActiveWallet(k.runtimeCtx, msg.Authority, "contract code store"); err != nil {
		return types.StoreCodeResponse{}, err
	}
	return k.storeCodeUnchecked(msg)
}

func (k *Keeper) storeCodeUnchecked(msg types.MsgStoreCode) (types.StoreCodeResponse, error) {
	if len(msg.Bytecode) > 0 {
		if err := types.ValidateAVMBytecode(k.genesis.Params, msg.Bytecode); err != nil {
			return types.StoreCodeResponse{}, err
		}
		codeHash := types.CanonicalCodeHash(msg.Bytecode)
		if msg.CodeHash != "" && msg.CodeHash != codeHash {
			return types.StoreCodeResponse{}, errors.New(types.ErrInvalidBytecode + ": code hash must match canonical bytecode hash")
		}
		msg.CodeHash = codeHash
		msg.CodeBytes = uint64(len(msg.Bytecode))
	}
	if msg.CodeBytes == 0 || msg.CodeBytes > k.genesis.Params.MaxCodeBytes {
		return types.StoreCodeResponse{}, errors.New(types.ErrInvalidBytecode + ": code size out of bounds")
	}
	if err := coretypes.ValidateHash("contracts code hash", msg.CodeHash); err != nil {
		return types.StoreCodeResponse{}, err
	}
	next := k.genesis
	next.State.Codes = upsertCode(next.State.Codes, types.CodeRecord{
		CodeID:    msg.CodeHash,
		CodeHash:  msg.CodeHash,
		CodeBytes: msg.CodeBytes,
		Bytecode:  append([]byte(nil), msg.Bytecode...),
		Owner:     msg.Authority,
	})
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return types.StoreCodeResponse{}, err
	}
	k.genesis = next
	return types.StoreCodeResponse{CodeID: msg.CodeHash, StateRoot: k.genesis.StateRoot}, nil
}

func (k *Keeper) StoreCodeState(ctx context.Context, msg types.MsgStoreCode) (types.StoreCodeResponse, error) {
	if !k.genesis.Params.Enabled {
		return types.StoreCodeResponse{}, errors.New(types.ErrExecutionFailed + ": module disabled")
	}
	if err := types.ValidateUserFacingAEAddress("contract code authority", msg.Authority); err != nil {
		return types.StoreCodeResponse{}, err
	}
	if err := k.ensureActiveWallet(ctx, msg.Authority, "contract code store"); err != nil {
		return types.StoreCodeResponse{}, err
	}
	res, err := k.storeCodeUnchecked(msg)
	if err != nil {
		return types.StoreCodeResponse{}, err
	}
	return res, k.writeGenesis(ctx)
}

func (k *Keeper) DeployContract(msg types.MsgDeployContract) (types.InstantiateContractResponse, error) {
	return k.deployContract(k.runtimeCtx, msg)
}

func (k *Keeper) DeployContractState(ctx context.Context, msg types.MsgDeployContract) (types.InstantiateContractResponse, error) {
	res, err := k.deployContract(ctx, msg)
	if err != nil {
		return types.InstantiateContractResponse{}, err
	}
	return res, k.writeGenesis(ctx)
}

func (k *Keeper) deployContract(ctx context.Context, msg types.MsgDeployContract) (types.InstantiateContractResponse, error) {
	if err := msg.ValidateBasic(k.genesis.Params); err != nil {
		return types.InstantiateContractResponse{}, err
	}
	return k.instantiateContract(ctx, types.MsgInstantiateContract{
		Creator:       msg.Creator,
		CodeID:        msg.CodeID,
		ChainID:       msg.ChainID,
		Namespace:     msg.Namespace,
		StateInit:     msg.StateInit,
		InitMsg:       append([]byte(nil), msg.InitPayload...),
		Funds:         msg.InitialBalance,
		Admin:         msg.Admin,
		Salt:          msg.Salt,
		Upgradeable:   msg.Upgradeable,
		SystemOwned:   msg.SystemOwned,
		SchemaVersion: msg.SchemaVersion,
		Height:        msg.Height,
	})
}

func (k *Keeper) ExecuteExternal(msg types.MsgExecuteExternal) (types.ExecuteContractResponse, error) {
	return k.executeExternal(k.runtimeCtx, msg)
}

func (k *Keeper) ExecuteExternalState(ctx context.Context, msg types.MsgExecuteExternal) (types.ExecuteContractResponse, error) {
	res, err := k.executeExternal(ctx, msg)
	if err != nil {
		return types.ExecuteContractResponse{}, err
	}
	return res, k.writeGenesis(ctx)
}

func (k *Keeper) executeExternal(ctx context.Context, msg types.MsgExecuteExternal) (types.ExecuteContractResponse, error) {
	if err := msg.ValidateBasic(k.genesis.Params); err != nil {
		return types.ExecuteContractResponse{}, err
	}
	if _, found := findContract(k.genesis.State.Contracts, msg.ContractAddress); !found && msg.StateInit != nil {
		user, _, err := types.DeriveContractAddressFromStateInit(msg.ChainID, msg.Namespace, msg.Sender, *msg.StateInit, k.genesis.Params)
		if err != nil {
			return types.ExecuteContractResponse{}, err
		}
		if user != msg.ContractAddress {
			return types.ExecuteContractResponse{}, errors.New(types.ErrContractNotFound + ": state init address does not match external execute target")
		}
		_, err = k.instantiateContract(ctx, types.MsgInstantiateContract{
			Creator:   msg.Sender,
			CodeID:    msg.StateInit.Normalize().CodeID,
			ChainID:   msg.ChainID,
			Namespace: msg.Namespace,
			StateInit: msg.StateInit,
			Height:    msg.Height,
		})
		if err != nil {
			return types.ExecuteContractResponse{}, err
		}
	}
	return k.executeContract(ctx, types.MsgExecuteContract{
		Sender:          msg.Sender,
		ContractAddress: msg.ContractAddress,
		Msg:             append([]byte(nil), msg.Payload...),
		Funds:           msg.Funds,
		Height:          msg.Height,
	})
}

func (k *Keeper) ExecuteInternal(msg types.MsgExecuteInternal) (types.InternalMessage, error) {
	if err := msg.ValidateBasic(k.genesis.Params); err != nil {
		return types.InternalMessage{}, err
	}
	return k.ReceiveInternalMessage(types.MsgReceiveInternalMessage{
		SourceContractUser: msg.Message.SourceContractUser,
		DestinationAccount: msg.Message.DestinationAccount,
		Funds:              msg.Message.Funds,
		Opcode:             msg.Message.Opcode,
		QueryID:            msg.Message.QueryID,
		Body:               append([]byte(nil), msg.Message.Body...),
		StateInit:          msg.Message.StateInit,
		Bounce:             msg.Message.Bounce,
		Deadline:           msg.Message.Deadline,
		GasLimit:           msg.Message.GasLimit,
		LogicalTime:        msg.Message.LogicalTime,
		MessageID:          msg.Message.MessageID,
		Height:             msg.Height,
	})
}

func (k *Keeper) SendInternalMessage(msg types.MsgSendInternalMessage) (types.InternalMessage, error) {
	if err := msg.ValidateBasic(k.genesis.Params); err != nil {
		return types.InternalMessage{}, err
	}
	return k.ReceiveInternalMessage(types.MsgReceiveInternalMessage{
		SourceContractUser: msg.Message.SourceContractUser,
		DestinationAccount: msg.Message.DestinationAccount,
		Funds:              msg.Message.Funds,
		Opcode:             msg.Message.Opcode,
		QueryID:            msg.Message.QueryID,
		Body:               append([]byte(nil), msg.Message.Body...),
		StateInit:          msg.Message.StateInit,
		Bounce:             msg.Message.Bounce,
		Deadline:           msg.Message.Deadline,
		GasLimit:           msg.Message.GasLimit,
		LogicalTime:        msg.Message.LogicalTime,
		MessageID:          msg.Message.MessageID,
		Height:             msg.Height,
	})
}

func (k *Keeper) UpdateContractParams(msg types.MsgUpdateContractParams) error {
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	next := k.genesis
	next.Params = msg.Params
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return err
	}
	k.genesis = next
	return nil
}

func (k *Keeper) SubmitSecurityAttestation(msg types.MsgSubmitSecurityAttestation) (types.MsgSubmitSecurityAttestationResponse, error) {
	if err := msg.ValidateBasic(k.genesis.Params); err != nil {
		return types.MsgSubmitSecurityAttestationResponse{}, err
	}
	attestation := msg.Attestation.Normalize()
	if attestation.AttestationID == "" {
		attestation.AttestationID = types.ComputeSecurityAttestationID(attestation)
	}
	if err := attestation.Validate(); err != nil {
		return types.MsgSubmitSecurityAttestationResponse{}, err
	}
	next := k.genesis
	next.State = next.State.UpsertSecurityAttestation(attestation)
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return types.MsgSubmitSecurityAttestationResponse{}, err
	}
	k.genesis = next
	return types.MsgSubmitSecurityAttestationResponse{Attestation: attestation, StateRoot: next.StateRoot}, nil
}

func (k *Keeper) RevokeSecurityAttestation(msg types.MsgRevokeSecurityAttestation) (types.MsgRevokeSecurityAttestationResponse, error) {
	if err := msg.ValidateBasic(k.genesis.Params); err != nil {
		return types.MsgRevokeSecurityAttestationResponse{}, err
	}
	next := k.genesis
	updated, found := next.State.RevokeSecurityAttestation(msg.AttestationID, msg.RevokedReason, msg.Height)
	if !found {
		return types.MsgRevokeSecurityAttestationResponse{}, errors.New("security attestation not found")
	}
	next.State = updated
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return types.MsgRevokeSecurityAttestationResponse{}, err
	}
	k.genesis = next
	attestation, ok := findSecurityAttestation(next.State.SecurityAttestations, msg.AttestationID)
	if !ok {
		return types.MsgRevokeSecurityAttestationResponse{}, errors.New("security attestation not found after revoke")
	}
	return types.MsgRevokeSecurityAttestationResponse{Attestation: attestation, StateRoot: next.StateRoot}, nil
}

func (k Keeper) Contract(req types.QueryContractRequest) (types.QueryContractResponse, error) {
	if strings.TrimSpace(req.ContractAddress) == "" && req.StateInit != nil {
		user, _, err := types.DeriveContractAddressFromStateInit(req.ChainID, req.Namespace, req.Deployer, *req.StateInit, k.genesis.Params)
		if err != nil {
			return types.QueryContractResponse{}, err
		}
		req.ContractAddress = user
	}
	if err := types.ValidateContractAddress(req.ContractAddress); err != nil {
		return types.QueryContractResponse{}, err
	}
	contract, found := findContract(k.genesis.State.Contracts, req.ContractAddress)
	if !found && req.StateInit != nil {
		user, _, err := types.DeriveContractAddressFromStateInit(req.ChainID, req.Namespace, req.Deployer, *req.StateInit, k.genesis.Params)
		if err != nil {
			return types.QueryContractResponse{}, err
		}
		if user != req.ContractAddress {
			return types.QueryContractResponse{}, errors.New(types.ErrContractNotFound + ": state init address does not match query address")
		}
		return types.QueryContractResponse{ContractAddress: req.ContractAddress, StateRoot: k.genesis.StateRoot, Found: false, Virtual: true}, nil
	}
	if found {
		if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionQuery); err != nil {
			return types.QueryContractResponse{}, err
		}
	}
	return types.QueryContractResponse{ContractAddress: req.ContractAddress, StateRoot: k.genesis.StateRoot, Found: found, Contract: contract}, nil
}

func (k Keeper) Contracts(req types.QueryContractsRequest) ([]types.Contract, error) {
	if err := types.ValidateQueryPagination(req.Pagination); err != nil {
		return nil, err
	}
	contracts := k.genesis.State.Normalize().Contracts
	if uint32(len(contracts)) > req.Pagination.Limit {
		contracts = contracts[:req.Pagination.Limit]
	}
	return append([]types.Contract(nil), contracts...), nil
}

func (k Keeper) ContractStorage(req types.QueryContractStorageRequest) ([]types.ContractStorageEntry, error) {
	if err := types.ValidateContractAddress(req.ContractAddress); err != nil {
		return nil, err
	}
	if err := types.ValidateQueryPagination(req.Pagination); err != nil {
		return nil, err
	}
	contract, found := findContract(k.genesis.State.Contracts, req.ContractAddress)
	if !found {
		return nil, errors.New(types.ErrContractNotFound + ": contract not found")
	}
	if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionProofQuery); err != nil {
		return nil, err
	}
	entries := []types.ContractStorageEntry{{
		ContractAddress: contract.AddressUser,
		Key:             []byte("data"),
		Value:           append([]byte(nil), contract.Data...),
	}}
	if snapshot, decodable, err := decodeContractSnapshot(contract.Data); err == nil && decodable && len(snapshot) > 0 {
		entries = make([]types.ContractStorageEntry, 0, len(snapshot))
		for _, item := range avm.Snapshot(snapshot) {
			entries = append(entries, types.ContractStorageEntry{
				ContractAddress: contract.AddressUser,
				Key:             []byte(item.Key),
				Value:           append([]byte(nil), item.Value...),
			})
		}
	}
	out := make([]types.ContractStorageEntry, 0, len(entries))
	for _, entry := range entries {
		if len(req.KeyPrefix) != 0 && !bytes.HasPrefix(entry.Key, req.KeyPrefix) {
			continue
		}
		out = append(out, entry)
		if uint32(len(out)) == req.Pagination.Limit {
			break
		}
	}
	return out, nil
}

func (k Keeper) ContractReceipts(req types.QueryContractReceiptsRequest) ([]types.ContractReceipt, error) {
	if err := types.ValidateContractAddress(req.ContractAddress); err != nil {
		return nil, err
	}
	if err := types.ValidateQueryPagination(req.Pagination); err != nil {
		return nil, err
	}
	receipts := k.genesis.State.Normalize().Receipts
	out := make([]types.ContractReceipt, 0)
	contract, found := findContract(k.genesis.State.Contracts, req.ContractAddress)
	if !found {
		return nil, errors.New(types.ErrContractNotFound + ": contract not found")
	}
	if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionQuery); err != nil {
		return nil, err
	}
	for _, receipt := range receipts {
		if receipt.ContractAddress != req.ContractAddress {
			continue
		}
		out = append(out, receipt)
		if uint32(len(out)) == req.Pagination.Limit {
			break
		}
	}
	return out, nil
}

func (k Keeper) ContractQueue(req types.QueryContractQueueRequest) ([]types.InternalMessage, error) {
	if err := types.ValidateContractAddress(req.ContractAddress); err != nil {
		return nil, err
	}
	if err := types.ValidateQueryPagination(req.Pagination); err != nil {
		return nil, err
	}
	queue := make([]types.InternalMessage, 0)
	contract, found := findContract(k.genesis.State.Contracts, req.ContractAddress)
	if !found {
		return nil, errors.New(types.ErrContractNotFound + ": contract not found")
	}
	if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionQuery); err != nil {
		return nil, err
	}
	for _, msg := range k.genesis.State.Normalize().InternalMessages {
		if msg.SourceContractUser == req.ContractAddress || msg.DestinationAccount == req.ContractAddress {
			queue = append(queue, msg)
			if uint32(len(queue)) == req.Pagination.Limit {
				break
			}
		}
	}
	return queue, nil
}

func (k Keeper) ContractEvents(req types.QueryContractEventsRequest) error {
	if err := types.ValidateContractAddress(req.ContractAddress); err != nil {
		return err
	}
	return types.ValidateQueryPagination(req.Pagination)
}

func (k Keeper) ContractStateRoot(req types.QueryContractStateRootRequest) (string, error) {
	if err := types.ValidateContractAddress(req.ContractAddress); err != nil {
		return "", err
	}
	contract, found := findContract(k.genesis.State.Contracts, req.ContractAddress)
	if !found {
		return "", errors.New(types.ErrContractNotFound + ": contract not found")
	}
	if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionProofQuery); err != nil {
		return "", err
	}
	return contract.StateRoot, nil
}

func (k Keeper) SecurityAttestations(req types.QuerySecurityAttestationsRequest) ([]types.ContractSecurityAttestation, error) {
	if err := types.ValidateContractAddress(req.ContractAddress); err != nil {
		return nil, err
	}
	if err := types.ValidateQueryPagination(req.Pagination); err != nil {
		return nil, err
	}
	out := k.genesis.State.SecurityAttestationsFor(req.ContractAddress, req.IncludeRevoked)
	if uint32(len(out)) > req.Pagination.Limit {
		out = out[:req.Pagination.Limit]
	}
	return append([]types.ContractSecurityAttestation(nil), out...), nil
}

func (k Keeper) SecurityBadge(req types.QuerySecurityBadgeRequest) (types.ContractSecurityBadge, bool, error) {
	if err := types.ValidateContractAddress(req.ContractAddress); err != nil {
		return types.ContractSecurityBadge{}, false, err
	}
	badge := k.genesis.State.SecurityBadge(req.ContractAddress)
	return badge, badge.AttestationCount > 0, nil
}

func (k *Keeper) InstantiateContract(msg types.MsgInstantiateContract) (types.InstantiateContractResponse, error) {
	return k.instantiateContract(k.runtimeCtx, msg)
}

func (k *Keeper) instantiateContract(ctx context.Context, msg types.MsgInstantiateContract) (types.InstantiateContractResponse, error) {
	if !k.genesis.Params.Enabled {
		return types.InstantiateContractResponse{}, errors.New(types.ErrExecutionFailed + ": module disabled")
	}
	if err := types.ValidateUserFacingAEAddress("contract creator", msg.Creator); err != nil {
		return types.InstantiateContractResponse{}, err
	}
	// Contract-initiated instantiation — the authenticated internal-message
	// auto-deploy path, where Creator is itself an on-chain contract — is
	// exempt from the native-wallet activation gate and the code-ownership
	// rule. A contract address can never be an activated native wallet, and
	// the StateInit address derivation already binds the exact code hash the
	// child must run, so ownership adds nothing. The only flow that reaches
	// here with a contract creator is pending-queue delivery, whose source
	// is authenticated by content hash (SEC-HIGH #5); external tx paths
	// always carry a human creator and keep both checks.
	_, _, creatorIsContract := findContractWithIndex(k.genesis.State.Contracts, msg.Creator)
	if !creatorIsContract {
		if err := k.ensureActiveWallet(ctx, msg.Creator, "contract instantiate"); err != nil {
			return types.InstantiateContractResponse{}, err
		}
	}
	if msg.Height == 0 {
		return types.InstantiateContractResponse{}, errors.New("contract instantiate height must be positive")
	}
	code, found := findCode(k.genesis.State.Codes, msg.CodeID)
	if !found && creatorIsContract {
		// The AVM's autoDeployAddress/counterfactualAddress builtins identify
		// code by the plain module hash sha256(bytecode) (see avm.go
		// runtimeStateInitFromValue), while stored code records are keyed by
		// the domain-separated canonical hash. Both are content addresses of
		// the same bytes, so resolving the module hash against stored
		// bytecode is exact, deterministic, and unforgeable.
		code, found = findCodeByModuleHash(k.genesis.State.Codes, msg.CodeID)
	}
	if !found {
		return types.InstantiateContractResponse{}, errors.New(types.ErrContractNotFound + ": contract code not found")
	}
	if !creatorIsContract && code.Owner != msg.Creator {
		return types.InstantiateContractResponse{}, errors.New(types.ErrUnauthorized + ": contract instantiate requires code owner")
	}
	stateInit, data, funds, err := k.stateInitForInstantiate(msg, code)
	if err != nil {
		return types.InstantiateContractResponse{}, err
	}
	admin := msg.Admin
	if admin == "" {
		admin = stateInit.Owner
	}
	if err := types.ValidateUserFacingAEAddress("contract admin", admin); err != nil {
		return types.InstantiateContractResponse{}, err
	}
	user, raw, err := types.DeriveContractAddressFromStateInit(msg.ChainID, msg.Namespace, msg.Creator, stateInit, k.genesis.Params)
	if err != nil {
		return types.InstantiateContractResponse{}, err
	}
	if _, found := findContract(k.genesis.State.Contracts, user); found {
		return types.InstantiateContractResponse{}, errors.New(types.ErrContractNotFound + ": contract address already exists")
	}
	storageBytes, err := contractStorageBytesForCode(code, data)
	if err != nil {
		return types.InstantiateContractResponse{}, err
	}
	if msg.StorageBytes != 0 && msg.StorageBytes != storageBytes {
		return types.InstantiateContractResponse{}, errors.New(types.ErrStorageRent + ": contract storage must equal code bytes plus data bytes")
	}
	if storageBytes > k.genesis.Params.MaxContractStorageBytes {
		return types.InstantiateContractResponse{}, errors.New(types.ErrStorageRent + ": contract storage exceeds configured limit")
	}
	stateInitHash, err := types.HashStateInit(stateInit)
	if err != nil {
		return types.InstantiateContractResponse{}, err
	}
	schemaVersion := msg.SchemaVersion
	if schemaVersion == 0 {
		schemaVersion = 1
	}
	contract := types.Contract{
		AddressUser: user,
		AddressRaw:  raw,
		// The resolved record's canonical ID, not msg.CodeID: contract-built
		// auto-deploys reference code by plain module hash, which is not a
		// stored code key.
		CodeID:                  code.CodeID,
		CodeHash:                code.CodeHash,
		StateInitHash:           stateInitHash,
		StateInit:               stateInit,
		Creator:                 msg.Creator,
		Owner:                   stateInit.Owner,
		Admin:                   admin,
		Upgradeable:             msg.Upgradeable,
		SystemOwned:             msg.SystemOwned,
		StorageSchemaVersion:    schemaVersion,
		InitMsg:                 append([]byte(nil), data...),
		Data:                    append([]byte(nil), data...),
		Balance:                 funds,
		Status:                  types.ContractStatusActive,
		StorageBytes:            storageBytes,
		LastStorageChargeHeight: msg.Height,
		LogicalTime:             1,
		CreatedHeight:           msg.Height,
		UpdatedHeight:           msg.Height,
	}
	contract.StateRoot = types.ComputeContractStateRoot(contract)
	next := k.genesis
	next.State.Contracts = append(next.State.Contracts, contract)
	next.State.Receipts = append(next.State.Receipts, newContractReceipt(contract.AddressUser, msg.Creator, "deploy", types.ExitCodeOK, funds, 0, contract.LogicalTime, msg.Height))
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return types.InstantiateContractResponse{}, err
	}
	initialStorageFee, err := checkedMul(storageBytes, k.storageRentPerByteBlock(), "contract storage fee overflow")
	if err != nil {
		return types.InstantiateContractResponse{}, err
	}
	if err := k.collectRentPayment(ctx, msg.Creator, initialStorageFee); err != nil {
		return types.InstantiateContractResponse{}, err
	}
	k.genesis = next
	return types.InstantiateContractResponse{
		ContractAddressUser: user,
		ContractAddressRaw:  raw,
		Owner:               contract.Owner,
		Admin:               contract.Admin,
		Balance:             contract.Balance,
		Events: []types.ContractEvent{{
			Type:        types.EventTypeContractInstantiated,
			Actor:       msg.Creator,
			Contract:    user,
			Amount:      funds,
			InternalRaw: raw,
		}},
	}, nil
}

func (k *Keeper) InstantiateContractState(ctx context.Context, msg types.MsgInstantiateContract) (types.InstantiateContractResponse, error) {
	res, err := k.instantiateContract(ctx, msg)
	if err != nil {
		return types.InstantiateContractResponse{}, err
	}
	return res, k.writeGenesis(ctx)
}

func (k *Keeper) UpgradeContractCode(msg types.MsgUpgradeContractCode) (types.ContractReceipt, error) {
	if err := msg.ValidateBasic(k.genesis.Params); err != nil {
		return types.ContractReceipt{}, err
	}
	idx, contract, found := findContractWithIndex(k.genesis.State.Contracts, msg.ContractAddress)
	if !found {
		return types.ContractReceipt{}, errors.New(types.ErrContractNotFound + ": contract not found")
	}
	if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionUpgradeMigrate); err != nil {
		return types.ContractReceipt{}, err
	}
	if err := k.authorizeContractUpgradeActor(contract, msg.Actor); err != nil {
		return types.ContractReceipt{}, err
	}
	if !contract.Upgradeable || contract.UpgradesDisabled {
		return types.ContractReceipt{}, errors.New(types.ErrUnauthorized + ": contract is immutable")
	}
	code, found := findCode(k.genesis.State.Codes, msg.NewCodeID)
	if !found {
		return types.ContractReceipt{}, errors.New(types.ErrContractNotFound + ": upgrade code not found")
	}
	if code.CodeHash != contract.CodeHash && strings.TrimSpace(msg.MigrationHandler) == "" {
		return types.ContractReceipt{}, errors.New(types.ErrExecutionFailed + ": code hash change requires migration handler")
	}
	nextContract := contract
	nextContract.CodeID = code.CodeID
	nextContract.CodeHash = code.CodeHash
	storageBytes, err := contractStorageBytesForCode(code, nextContract.Data)
	if err != nil {
		return types.ContractReceipt{}, err
	}
	if storageBytes > k.genesis.Params.MaxContractStorageBytes {
		return types.ContractReceipt{}, errors.New(types.ErrStorageRent + ": contract storage exceeds configured limit")
	}
	if storageBytes > contract.StorageBytes {
		diff := storageBytes - contract.StorageBytes
		extraFee, err := checkedMul(diff, k.storageRentPerByteBlock(), "contract storage fee overflow")
		if err != nil {
			return types.ContractReceipt{}, err
		}
		if err := k.collectRentPayment(k.runtimeCtx, msg.Actor, extraFee); err != nil {
			return types.ContractReceipt{}, err
		}
	}
	nextContract.StorageBytes = storageBytes
	nextContract.LogicalTime++
	nextContract.UpdatedHeight = msg.Height
	nextContract.StateRoot = types.ComputeContractStateRoot(nextContract)
	receipt := newContractReceipt(nextContract.AddressUser, receiptActor(contract, msg.Actor), "upgrade_code", types.ExitCodeOK, 0, 0, nextContract.LogicalTime, msg.Height)
	next := k.genesis
	next.State.Contracts[idx] = nextContract
	next.State.Receipts = append(next.State.Receipts, receipt)
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return types.ContractReceipt{}, err
	}
	k.genesis = next
	return receipt, nil
}

func (k *Keeper) MigrateContractState(msg types.MsgMigrateContractState) (types.ContractReceipt, error) {
	if err := msg.ValidateBasic(k.genesis.Params); err != nil {
		return types.ContractReceipt{}, err
	}
	idx, contract, found := findContractWithIndex(k.genesis.State.Contracts, msg.ContractAddress)
	if !found {
		return types.ContractReceipt{}, errors.New(types.ErrContractNotFound + ": contract not found")
	}
	if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionUpgradeMigrate); err != nil {
		return types.ContractReceipt{}, err
	}
	if err := k.authorizeContractUpgradeActor(contract, msg.Actor); err != nil {
		return types.ContractReceipt{}, err
	}
	if !contract.Upgradeable || contract.UpgradesDisabled {
		return types.ContractReceipt{}, errors.New(types.ErrUnauthorized + ": contract is immutable")
	}
	if contract.StorageSchemaVersion != msg.FromSchemaVersion {
		return types.ContractReceipt{}, errors.New(types.ErrExecutionFailed + ": contract migration schema version mismatch")
	}
	nextContract := contract
	data, err := applyContractMigration(nextContract.Data, msg.MigrationHandler, msg.Payload)
	if err != nil {
		return types.ContractReceipt{}, err
	}
	nextContract.Data = data
	nextContract.StorageSchemaVersion = msg.ToSchemaVersion
	storageBytes, err := k.contractStorageBytes(nextContract)
	if err != nil {
		return types.ContractReceipt{}, err
	}
	if storageBytes > k.genesis.Params.MaxContractStorageBytes {
		return types.ContractReceipt{}, errors.New(types.ErrStorageRent + ": migrated contract storage exceeds configured limit")
	}
	if storageBytes > contract.StorageBytes {
		diff := storageBytes - contract.StorageBytes
		extraFee, err := checkedMul(diff, k.storageRentPerByteBlock(), "contract storage fee overflow")
		if err != nil {
			return types.ContractReceipt{}, err
		}
		if err := k.collectRentPayment(k.runtimeCtx, msg.Actor, extraFee); err != nil {
			return types.ContractReceipt{}, err
		}
	}
	nextContract.StorageBytes = storageBytes
	nextContract.LogicalTime++
	nextContract.UpdatedHeight = msg.Height
	nextContract.StateRoot = types.ComputeContractStateRoot(nextContract)
	receipt := newContractReceipt(nextContract.AddressUser, receiptActor(contract, msg.Actor), "migrate_state", types.ExitCodeOK, msg.ToSchemaVersion, 0, nextContract.LogicalTime, msg.Height)
	next := k.genesis
	next.State.Contracts[idx] = nextContract
	next.State.Receipts = append(next.State.Receipts, receipt)
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return types.ContractReceipt{}, err
	}
	k.genesis = next
	return receipt, nil
}

func (k *Keeper) SetContractAdmin(msg types.MsgSetContractAdmin) (types.ContractReceipt, error) {
	if err := msg.ValidateBasic(k.genesis.Params); err != nil {
		return types.ContractReceipt{}, err
	}
	idx, contract, found := findContractWithIndex(k.genesis.State.Contracts, msg.ContractAddress)
	if !found {
		return types.ContractReceipt{}, errors.New(types.ErrContractNotFound + ": contract not found")
	}
	if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionUpgradeMigrate); err != nil {
		return types.ContractReceipt{}, err
	}
	if err := k.authorizeContractUpgradeActor(contract, msg.Actor); err != nil {
		return types.ContractReceipt{}, err
	}
	contract.Admin = msg.NewAdmin
	contract.LogicalTime++
	contract.UpdatedHeight = msg.Height
	contract.StateRoot = types.ComputeContractStateRoot(contract)
	receipt := newContractReceipt(contract.AddressUser, receiptActor(contract, msg.Actor), "set_admin", types.ExitCodeOK, 0, 0, contract.LogicalTime, msg.Height)
	next := k.genesis
	next.State.Contracts[idx] = contract
	next.State.Receipts = append(next.State.Receipts, receipt)
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return types.ContractReceipt{}, err
	}
	k.genesis = next
	return receipt, nil
}

func (k *Keeper) DisableContractUpgrades(msg types.MsgDisableContractUpgrades) (types.ContractReceipt, error) {
	if err := msg.ValidateBasic(k.genesis.Params); err != nil {
		return types.ContractReceipt{}, err
	}
	idx, contract, found := findContractWithIndex(k.genesis.State.Contracts, msg.ContractAddress)
	if !found {
		return types.ContractReceipt{}, errors.New(types.ErrContractNotFound + ": contract not found")
	}
	if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionUpgradeMigrate); err != nil {
		return types.ContractReceipt{}, err
	}
	if err := k.authorizeContractUpgradeActor(contract, msg.Actor); err != nil {
		return types.ContractReceipt{}, err
	}
	contract.Upgradeable = false
	contract.UpgradesDisabled = true
	contract.LogicalTime++
	contract.UpdatedHeight = msg.Height
	contract.StateRoot = types.ComputeContractStateRoot(contract)
	receipt := newContractReceipt(contract.AddressUser, receiptActor(contract, msg.Actor), "disable_upgrades", types.ExitCodeOK, 0, 0, contract.LogicalTime, msg.Height)
	next := k.genesis
	next.State.Contracts[idx] = contract
	next.State.Receipts = append(next.State.Receipts, receipt)
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return types.ContractReceipt{}, err
	}
	k.genesis = next
	return receipt, nil
}

func (k Keeper) stateInitForInstantiate(msg types.MsgInstantiateContract, code types.CodeRecord) (types.StateInit, []byte, uint64, error) {
	if msg.StateInit == nil {
		stateInit := types.NewStateInit(msg.Creator, code.CodeHash, msg.InitMsg, msg.Salt, msg.Funds).Normalize()
		if err := stateInit.Validate(k.genesis.Params); err != nil {
			return types.StateInit{}, nil, 0, err
		}
		return stateInit, append([]byte(nil), stateInit.InitData...), stateInit.InitialBalanceNAET, nil
	}
	stateInit := msg.StateInit.Normalize()
	if err := stateInit.Validate(k.genesis.Params); err != nil {
		return types.StateInit{}, nil, 0, err
	}
	if stateInit.CodeID != msg.CodeID {
		return types.StateInit{}, nil, 0, errors.New("state init code id must match instantiate code id")
	}
	// Contract-built StateInits (AVM autoDeployAddress/counterfactualAddress)
	// identify code by the plain module hash sha256(bytecode); tx-built ones
	// use the stored record's domain-separated canonical hash. Both are
	// content addresses of the same bytecode, so either binds the child to
	// exactly one module.
	if stateInit.CodeHash != code.CodeHash && !codeHashMatchesModule(stateInit.CodeHash, code.Bytecode) {
		return types.StateInit{}, nil, 0, errors.New("state init code hash must match stored code")
	}
	if len(msg.InitMsg) != 0 && !bytes.Equal(msg.InitMsg, stateInit.InitData) {
		return types.StateInit{}, nil, 0, errors.New("state init data must match instantiate init message")
	}
	// A StateInit with a pinned non-zero balance must agree with the
	// instantiate funds. A zero StateInit balance (the AVM never pins one
	// for message-value-funded auto-deploys) means the attached message
	// value funds the child instead — the caller debits the source by the
	// same amount, so value is conserved either way.
	if msg.Funds != 0 && stateInit.InitialBalanceNAET != 0 && msg.Funds != stateInit.InitialBalanceNAET {
		return types.StateInit{}, nil, 0, errors.New("state init initial balance must match instantiate funds")
	}
	if msg.Salt != "" && msg.Salt != stateInit.Salt && !bytes.Equal([]byte(msg.Salt), stateInit.SaltBytesForAddress()) {
		return types.StateInit{}, nil, 0, errors.New("state init salt must match instantiate salt")
	}
	effectiveFunds := stateInit.InitialBalanceNAET
	if effectiveFunds == 0 {
		effectiveFunds = msg.Funds
	}
	return stateInit, append([]byte(nil), stateInit.InitData...), effectiveFunds, nil
}

func findCodeRecord(codes []types.CodeRecord, codeID string) (types.CodeRecord, bool) {
	for _, code := range codes {
		if code.CodeID == codeID {
			return code, true
		}
	}
	return types.CodeRecord{}, false
}

func decodeContractSnapshot(data []byte) (avm.Storage, bool, error) {
	if len(data) == 0 {
		return avm.Storage{}, true, nil
	}
	if looksLikeAVMSnapshot(data) {
		snapshot, err := avm.DecodeSnapshot(data)
		if err != nil {
			return nil, false, err
		}
		return snapshot, true, nil
	}
	var fields []struct {
		Name  string          `json:"name"`
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, false, nil
	}
	storage := make(avm.Storage, len(fields))
	for _, field := range fields {
		if strings.TrimSpace(field.Name) == "" {
			continue
		}
		encoded, err := encodeJSONStorageValue(field.Type, field.Value)
		if err != nil {
			return nil, false, err
		}
		storage[field.Name] = encoded
	}
	return storage, true, nil
}

func looksLikeAVMSnapshot(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	count := binary.BigEndian.Uint32(data[:4])
	if count > uint32(len(data)) {
		return false
	}
	remaining := len(data) - 4
	if count == 0 {
		return remaining == 0
	}
	minBytesPerEntry := 6
	return int(count) <= remaining/minBytesPerEntry
}

func encodeJSONStorageValue(typeName string, raw json.RawMessage) ([]byte, error) {
	kind := strings.ToLower(strings.TrimSpace(typeName))
	kind = strings.TrimSuffix(kind, "?")
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	switch kind {
	case "uint8", "uint16", "uint32", "uint64", "int64", "coins", "ticket", "countervalue", "packedstate":
		value, err := parseJSONUint64(raw)
		if err != nil {
			return nil, err
		}
		return avm.EncodeU64(value), nil
	case "bool":
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		if value {
			return avm.EncodeU64(1), nil
		}
		return avm.EncodeU64(0), nil
	case "address":
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		encoded, err := avm.CanonicalEncode(avm.ValueAddress(text))
		if err != nil {
			return nil, err
		}
		return encoded, nil
	case "string":
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		encoded, err := avm.CanonicalEncode(avm.ValueString(text))
		if err != nil {
			return nil, err
		}
		return encoded, nil
	case "code", "chunk":
		// The storage codec renders chunk-typed fields as the canonical
		// snapshot map {hex, base64, hash, chunks}. Rebuild the real runtime
		// chunk value from the payload bytes: without this, the raw JSON map
		// leaks into runtime storage and every code-identity derivation the
		// contract performs (autoDeployAddress, counterfactualAddress)
		// hashes JSON text instead of the module bytes.
		var snapshot struct {
			Hex    string `json:"hex"`
			Base64 string `json:"base64"`
		}
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return nil, fmt.Errorf("decode %s storage value: %w", kind, err)
		}
		var data []byte
		var err error
		switch {
		case strings.TrimSpace(snapshot.Hex) != "":
			data, err = hex.DecodeString(strings.TrimSpace(snapshot.Hex))
		case strings.TrimSpace(snapshot.Base64) != "":
			data, err = base64.StdEncoding.DecodeString(strings.TrimSpace(snapshot.Base64))
		}
		if err != nil {
			return nil, fmt.Errorf("decode %s storage payload: %w", kind, err)
		}
		if len(data) == 0 {
			return nil, nil
		}
		root, err := avm.ToChunkPayload(data, chunk.TypeNormal)
		if err != nil {
			return nil, err
		}
		encoded, err := avm.CanonicalEncode(avm.ValueChunkRef(root))
		if err != nil {
			return nil, err
		}
		return encoded, nil
	default:
		if numericValue, ok := parseJSONNumericValue(raw); ok {
			return avm.EncodeU64(numericValue), nil
		}
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			return []byte(text), nil
		}
		return append([]byte(nil), raw...), nil
	}
}

func parseJSONNumericValue(raw json.RawMessage) (uint64, bool) {
	var num uint64
	if err := json.Unmarshal(raw, &num); err == nil {
		return num, true
	}
	var signed int64
	if err := json.Unmarshal(raw, &signed); err == nil {
		if signed < 0 {
			return 0, false
		}
		return uint64(signed), true
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = strings.TrimSpace(text)
		if strings.HasPrefix(strings.ToLower(text), "0x") {
			value, err := strconv.ParseUint(text[2:], 16, 64)
			if err == nil {
				return value, true
			}
			return 0, false
		}
		value, err := strconv.ParseUint(text, 10, 64)
		if err == nil {
			return value, true
		}
	}
	return 0, false
}

func parseJSONUint64(raw json.RawMessage) (uint64, error) {
	var num uint64
	if err := json.Unmarshal(raw, &num); err == nil {
		return num, nil
	}
	var signed int64
	if err := json.Unmarshal(raw, &signed); err == nil {
		if signed < 0 {
			return 0, fmt.Errorf("negative value %d cannot be encoded as uint64", signed)
		}
		return uint64(signed), nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = strings.TrimSpace(text)
		if strings.HasPrefix(strings.ToLower(text), "0x") {
			return strconv.ParseUint(text[2:], 16, 64)
		}
		return strconv.ParseUint(text, 10, 64)
	}
	return 0, fmt.Errorf("unsupported numeric storage value %s", string(raw))
}

func encodeSnapshotStorage(storage avm.Storage) []byte {
	if len(storage) == 0 {
		return nil
	}
	return avm.EncodeSnapshot(storage)
}

// blockBeaconInputs returns the consensus entropy feeding contract randomness:
// the previous block's committed app hash (previousStateRoot) and the current
// block hash (block entropy). Both come only from the live consensus header, so
// every validator derives identical random() values, and the current block hash
// is not known when a transaction is submitted. Returns nil inputs when no SDK
// header is bound (unit tests, genesis, off-chain tooling); randomness then
// still resolves deterministically from the message discriminator alone.
func blockBeaconInputs(ctx context.Context) (prevStateRoot, blockEntropy []byte) {
	defer func() { _ = recover() }()
	info := sdk.UnwrapSDKContext(ctx).HeaderInfo()
	return info.AppHash, info.Hash
}

func (k Keeper) buildAVMContext(entry avm.Entrypoint, contract types.Contract, sender string, payload []byte, funds, gasLimit, height, logicalTime uint64, opcode uint32, queryID uint64, bounced bool) (avm.RuntimeContext, error) {
	contractAddress, err := aetraaddress.ParseAccAddress(contract.AddressUser)
	if err != nil {
		return avm.RuntimeContext{}, err
	}
	sourceAddress, err := aetraaddress.ParseAccAddress(sender)
	if err != nil {
		return avm.RuntimeContext{}, err
	}
	if gasLimit == 0 {
		gasLimit = 100_000
	}
	prevStateRoot, blockEntropy := blockBeaconInputs(k.runtimeCtx)
	return avm.RuntimeContext{
		Entry:           entry,
		ContractAddress: contractAddress,
		Message: async.MessageEnvelope{
			Source:             sourceAddress,
			Destination:        contractAddress,
			Opcode:             opcode,
			QueryID:            queryID,
			Body:               append([]byte(nil), payload...),
			Bounce:             true,
			Bounced:            bounced,
			GasLimit:           gasLimit,
			Value:              sdk.NewCoin(storageRentBaseDenom, sdkmath.NewIntFromUint64(funds)),
			ForwardFee:         sdk.NewCoin(storageRentBaseDenom, sdkmath.ZeroInt()),
			CreatedLogicalTime: logicalTime,
		},
		BlockHeight:             height,
		BlockTimestamp:          height,
		LogicalTime:             logicalTime,
		CurrentBlockLogicalTime: height,
		OriginalBalance:         sdkmath.NewIntFromUint64(contract.Balance),
		AttachedValue:           sdkmath.NewIntFromUint64(funds),
		GasLimit:                gasLimit,
		PrevStateRoot:           prevStateRoot,
		BlockEntropy:            blockEntropy,
	}, nil
}

func loadAVMModule(code types.CodeRecord) (avm.Module, bool, error) {
	if len(code.Bytecode) == 0 {
		return avm.Module{}, false, nil
	}
	module, err := avm.DecodeModule(code.Bytecode)
	if err != nil {
		return avm.Module{}, false, nil
	}
	return module, true, nil
}

func (k *Keeper) ExecuteContract(msg types.MsgExecuteContract) (types.ExecuteContractResponse, error) {
	return k.executeContract(k.runtimeCtx, msg)
}

func (k *Keeper) executeContract(ctx context.Context, msg types.MsgExecuteContract) (types.ExecuteContractResponse, error) {
	if !k.genesis.Params.Enabled {
		return types.ExecuteContractResponse{}, errors.New(types.ErrExecutionFailed + ": module disabled")
	}
	if err := types.ValidateUserFacingAEAddress("contract execute sender", msg.Sender); err != nil {
		return types.ExecuteContractResponse{}, err
	}
	if err := k.ensureActiveWallet(ctx, msg.Sender, "contract execute"); err != nil {
		return types.ExecuteContractResponse{}, err
	}
	if msg.Height == 0 {
		return types.ExecuteContractResponse{}, errors.New("contract execute height must be positive")
	}
	idx, contract, found := findContractWithIndex(k.genesis.State.Contracts, msg.ContractAddress)
	if !found {
		return types.ExecuteContractResponse{}, errors.New(types.ErrContractNotFound + ": contract not found")
	}
	if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionExecuteExternal); err != nil {
		return types.ExecuteContractResponse{}, err
	}
	contract, rentCharged, err := k.chargeContractRentAt(ctx, idx, contract, msg.Height)
	if err != nil {
		return types.ExecuteContractResponse{}, errors.New(types.ErrStorageRent + ": contract has storage rent debt")
	}
	balance, err := checkedAdd(contract.Balance, msg.Funds, "contract balance overflow")
	if err != nil {
		return types.ExecuteContractResponse{}, err
	}
	contract.Balance = balance
	code, found := findCodeRecord(k.genesis.State.Codes, contract.CodeID)
	if !found {
		return types.ExecuteContractResponse{}, errors.New(types.ErrInvalidBytecode + ": stored code not found for contract")
	}
	module, executable, err := loadAVMModule(code)
	if err != nil {
		return types.ExecuteContractResponse{}, err
	}
	var outgoing []async.MessageEnvelope
	if executable {
		storage, decodable, err := decodeContractSnapshot(contract.Data)
		if err != nil {
			return types.ExecuteContractResponse{}, err
		}
		if !decodable {
			return types.ExecuteContractResponse{}, errors.New(types.ErrExecutionFailed + ": contract state is not AVM snapshot compatible")
		}
		runner, err := avm.NewRunner(avm.DefaultParams())
		if err != nil {
			return types.ExecuteContractResponse{}, err
		}
		runtimeCtx, err := k.buildAVMContext(avm.EntryReceiveExternal, contract, msg.Sender, msg.Msg, msg.Funds, k.genesis.Params.MaxGasPerExecution, msg.Height, contract.LogicalTime, 0, 0, false)
		if err != nil {
			return types.ExecuteContractResponse{}, err
		}
		exec, err := runner.Run(module, storage, runtimeCtx)
		if err != nil {
			return types.ExecuteContractResponse{}, err
		}
		if exec.ResultCode != async.ResultOK {
			return types.ExecuteContractResponse{}, fmt.Errorf("%s: avm external execution failed with code %d", types.ErrExecutionFailed, exec.ResultCode)
		}
		contract.Data = encodeSnapshotStorage(exec.State)
		outgoing = append([]async.MessageEnvelope(nil), exec.Outgoing...)
	} else {
		contract.Data = append([]byte(nil), msg.Msg...)
	}
	contract.LogicalTime++
	storageBytes, err := k.contractStorageBytes(contract)
	if err != nil {
		return types.ExecuteContractResponse{}, err
	}
	if storageBytes > k.genesis.Params.MaxContractStorageBytes {
		return types.ExecuteContractResponse{}, errors.New(types.ErrStorageRent + ": contract storage exceeds configured limit")
	}
	contract.StorageBytes = storageBytes
	contract.UpdatedHeight = msg.Height
	contract.StateRoot = types.ComputeContractStateRoot(contract)
	next := k.genesis
	next.State.Contracts[idx] = contract
	if err := k.appendAVMOutgoingMessages(&next, contract, outgoing, msg.Height); err != nil {
		return types.ExecuteContractResponse{}, err
	}
	next.State.Receipts = append(next.State.Receipts, newContractReceipt(contract.AddressUser, msg.Sender, "execute", types.ExitCodeOK, msg.Funds, 0, contract.LogicalTime, msg.Height))
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return types.ExecuteContractResponse{}, err
	}
	if rentCharged > 0 {
		if err := k.chargeRentToReserve(ctx, contract, rentCharged); err != nil {
			return types.ExecuteContractResponse{}, err
		}
	}
	k.genesis = next
	return types.ExecuteContractResponse{
		ContractAddressUser: contract.AddressUser,
		Owner:               contract.Owner,
		Balance:             contract.Balance,
		Events: []types.ContractEvent{{
			Type:        types.EventTypeContractExecuted,
			Actor:       msg.Sender,
			Contract:    contract.AddressUser,
			Amount:      msg.Funds,
			InternalRaw: contract.AddressRaw,
		}},
	}, nil
}

func (k *Keeper) ExecuteContractState(ctx context.Context, msg types.MsgExecuteContract) (types.ExecuteContractResponse, error) {
	res, err := k.executeContract(ctx, msg)
	if err != nil {
		return types.ExecuteContractResponse{}, err
	}
	return res, k.writeGenesis(ctx)
}

func (k *Keeper) TopUpContract(msg types.MsgTopUpContract) (types.Contract, error) {
	if err := types.ValidateUserFacingAEAddress("contract top-up sender", msg.Sender); err != nil {
		return types.Contract{}, err
	}
	if msg.Amount == 0 || msg.Height == 0 {
		return types.Contract{}, errors.New("contract top-up amount and height must be positive")
	}
	idx, contract, found := findContractWithIndex(k.genesis.State.Contracts, msg.ContractAddress)
	if !found {
		return types.Contract{}, errors.New(types.ErrContractNotFound + ": contract not found")
	}
	if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionReceiveTopUp); err != nil {
		return types.Contract{}, err
	}
	balance, err := checkedAdd(contract.Balance, msg.Amount, "contract top-up balance overflow")
	if err != nil {
		return types.Contract{}, err
	}
	contract.Balance = balance
	contract.UpdatedHeight = msg.Height
	next := k.genesis
	next.State.Contracts[idx] = contract
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return types.Contract{}, err
	}
	k.genesis = next
	return contract, nil
}

func (k *Keeper) TopUpContractState(ctx context.Context, msg types.MsgTopUpContract) (types.Contract, error) {
	contract, err := k.TopUpContract(msg)
	if err != nil {
		return types.Contract{}, err
	}
	return contract, k.writeGenesis(ctx)
}

func (k *Keeper) PayContractStorageDebt(msg types.MsgPayContractStorageDebt) (types.Contract, error) {
	if err := types.ValidateUserFacingAEAddress("contract rent payer", msg.Sender); err != nil {
		return types.Contract{}, err
	}
	if msg.Amount == 0 || msg.Height == 0 {
		return types.Contract{}, errors.New("contract storage debt payment amount and height must be positive")
	}
	idx, contract, found := findContractWithIndex(k.genesis.State.Contracts, msg.ContractAddress)
	if !found {
		return types.Contract{}, errors.New(types.ErrContractNotFound + ": contract not found")
	}
	if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionPayRentDebt); err != nil {
		return types.Contract{}, err
	}
	// Only the portion that actually reduces the debt is collected; any
	// overpayment beyond the outstanding debt is ignored (not charged).
	applied := msg.Amount
	if applied > contract.StorageRentDebt {
		applied = contract.StorageRentDebt
	}
	// Move the coins into the storage-rent reserve BEFORE reducing the debt.
	// If the bank transfer fails, return the error and leave the debt intact
	// so the freeze cannot be cleared for free.
	if err := k.collectRentPayment(k.runtimeCtx, msg.Sender, applied); err != nil {
		return types.Contract{}, err
	}
	contract.StorageRentDebt -= applied
	contract.UpdatedHeight = msg.Height
	next := k.genesis
	next.State.Contracts[idx] = contract
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return types.Contract{}, err
	}
	k.genesis = next
	return contract, nil
}

func (k *Keeper) PayContractStorageDebtState(ctx context.Context, msg types.MsgPayContractStorageDebt) (types.Contract, error) {
	contract, err := k.PayContractStorageDebt(msg)
	if err != nil {
		return types.Contract{}, err
	}
	return contract, k.writeGenesis(ctx)
}

func (k *Keeper) UnfreezeContract(msg types.MsgUnfreezeContract) (types.Contract, error) {
	return k.unfreezeContract(k.runtimeCtx, msg)
}

func (k *Keeper) UnfreezeContractState(ctx context.Context, msg types.MsgUnfreezeContract) (types.Contract, error) {
	contract, err := k.unfreezeContract(ctx, msg)
	if err != nil {
		return types.Contract{}, err
	}
	return contract, k.writeGenesis(ctx)
}

func (k *Keeper) unfreezeContract(ctx context.Context, msg types.MsgUnfreezeContract) (types.Contract, error) {
	if err := types.ValidateUserFacingAEAddress("contract unfreeze sender", msg.Sender); err != nil {
		return types.Contract{}, err
	}
	if err := k.ensureActiveWallet(ctx, msg.Sender, "contract unfreeze"); err != nil {
		return types.Contract{}, err
	}
	if msg.Height == 0 {
		return types.Contract{}, errors.New("contract unfreeze height must be positive")
	}
	idx, contract, found := findContractWithIndex(k.genesis.State.Contracts, msg.ContractAddress)
	if !found {
		return types.Contract{}, errors.New(types.ErrContractNotFound + ": contract not found")
	}
	if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionUnfreeze); err != nil {
		return types.Contract{}, err
	}
	if contract.StorageRentDebt > 0 {
		return types.Contract{}, errors.New(types.ErrStorageRent + ": contract storage rent debt must be paid before unfreeze")
	}
	contract.Status = types.ContractStatusActive
	contract.LastStorageChargeHeight = msg.Height
	contract.UpdatedHeight = msg.Height
	next := k.genesis
	next.State.Contracts[idx] = contract
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return types.Contract{}, err
	}
	k.genesis = next
	return contract, nil
}

func (k *Keeper) GrantNativeStakingCapability(msg types.MsgGrantNativeStakingCapability) (types.ContractCapability, error) {
	if err := k.genesis.Params.Authorize(msg.Authority); err != nil {
		return types.ContractCapability{}, err
	}
	if msg.Height == 0 {
		return types.ContractCapability{}, errors.New("contract capability height must be positive")
	}
	if _, found := findContract(k.genesis.State.Contracts, msg.ContractAddressUser); !found {
		return types.ContractCapability{}, errors.New(types.ErrContractNotFound + ": contract not found")
	}
	capability := types.ContractCapability{
		ContractAddressUser: msg.ContractAddressUser,
		ContractAddressRaw:  msg.ContractAddressRaw,
		Capability:          types.NativeStakingCapability,
		PoolID:              msg.PoolID,
		GrantedHeight:       msg.Height,
	}
	if err := capability.Validate(); err != nil {
		return types.ContractCapability{}, err
	}
	next := k.genesis
	next.State.StakingCapabilities = upsertCapability(next.State.StakingCapabilities, capability)
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return types.ContractCapability{}, err
	}
	k.genesis = next
	return capability, nil
}

func (k *Keeper) InjectNativeStaking(msg types.MsgInjectNativeStaking) (types.NativeStakingInjectionRecord, error) {
	if msg.Amount == 0 || msg.Height == 0 {
		return types.NativeStakingInjectionRecord{}, errors.New("native staking injection amount and height must be positive")
	}
	if err := types.ValidateAddressPair("native staking caller contract", msg.CallerContractUser, msg.CallerContractRaw); err != nil {
		return types.NativeStakingInjectionRecord{}, err
	}
	idx, contract, found := findContractWithIndex(k.genesis.State.Contracts, msg.CallerContractUser)
	if !found {
		return types.NativeStakingInjectionRecord{}, errors.New(types.ErrContractNotFound + ": contract not found")
	}
	if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionExecuteExternal); err != nil {
		return types.NativeStakingInjectionRecord{}, err
	}
	if !hasCapability(k.genesis.State.StakingCapabilities, msg.CallerContractUser, msg.PoolID) {
		return types.NativeStakingInjectionRecord{}, errors.New(types.ErrUnauthorized + ": contract lacks native staking capability")
	}
	contract, rentCharged, err := k.chargeContractRentAt(k.runtimeCtx, idx, contract, msg.Height)
	if err != nil {
		return types.NativeStakingInjectionRecord{}, errors.New(types.ErrStorageRent + ": contract has storage rent debt")
	}
	record := types.NativeStakingInjectionRecord{
		ContractAddressUser: msg.CallerContractUser,
		ContractAddressRaw:  msg.CallerContractRaw,
		PoolID:              msg.PoolID,
		Amount:              msg.Amount,
		Height:              msg.Height,
	}
	next := k.genesis
	next.State.Contracts[idx] = contract
	next.State.NativeStakingInjects = append(next.State.NativeStakingInjects, record)
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return types.NativeStakingInjectionRecord{}, err
	}
	if rentCharged > 0 {
		if err := k.chargeRentToReserve(k.runtimeCtx, contract, rentCharged); err != nil {
			return types.NativeStakingInjectionRecord{}, err
		}
	}
	k.genesis = next
	return record, nil
}

func (k *Keeper) ReceiveInternalMessage(msg types.MsgReceiveInternalMessage) (types.InternalMessage, error) {
	// Honor the module kill switch on the internal-message path too. StoreCode,
	// instantiate and external-execute already gate on Params.Enabled; without
	// this guard the publicly-routable ExecuteInternal/SendInternalMessage
	// handlers keep executing contract code after governance disables the module
	// for an incident. See SEC-LOW: kill-switch not enforced on internal handlers.
	if !k.genesis.Params.Enabled {
		return types.InternalMessage{}, errors.New(types.ErrExecutionFailed + ": module disabled")
	}
	// Internal messages are DELIVERED, never authored from the caller record.
	// The only writer of the pending queue is appendAVMOutgoingMessages, which
	// stamps the verified AddressUser of a genuinely-executing contract as the
	// source. Recompute the message ID over the caller fields and require an
	// exact queued match: a forged source (or any other field bound into the
	// ID) yields an ID absent from the queue, so delivery is rejected. This is
	// what authenticates the source without a tx signer. See SEC-HIGH:
	// authenticate internal contract messages.
	lookup := types.InternalMessage{
		SourceContractUser: msg.SourceContractUser,
		DestinationAccount: msg.DestinationAccount,
		Funds:              msg.Funds,
		Opcode:             msg.Opcode,
		QueryID:            msg.QueryID,
		Body:               append([]byte(nil), msg.Body...),
		StateInit:          msg.StateInit,
		Bounce:             msg.Bounce,
		Deadline:           msg.Deadline,
		GasLimit:           msg.GasLimit,
		LogicalTime:        msg.LogicalTime,
		MessageID:          msg.MessageID,
		Height:             msg.Height,
	}
	if lookup.LogicalTime == 0 {
		lookup.LogicalTime = msg.Height
	}
	wantID := lookup.MessageID
	if wantID == "" {
		wantID = types.ComputeInternalMessageID(lookup)
	}
	queuedIdx := -1
	for i := range k.genesis.State.InternalMessages {
		queued := k.genesis.State.InternalMessages[i]
		id := queued.MessageID
		if id == "" {
			id = types.ComputeInternalMessageID(queued)
		}
		if id == wantID {
			queuedIdx = i
			break
		}
	}
	if queuedIdx == -1 {
		return types.InternalMessage{}, errors.New(types.ErrUnauthorized + ": internal message is not present in the pending queue")
	}
	record := k.genesis.State.InternalMessages[queuedIdx]
	record.Body = append([]byte(nil), record.Body...)
	if record.MessageID == "" {
		record.MessageID = wantID
	}
	if record.LogicalTime == 0 {
		record.LogicalTime = record.Height
	}
	// Clamp the queued gas to the module maximum before it reaches the
	// interpreter. See SEC-CRIT: uncapped AVM gas on internal messages.
	if maxGas := k.genesis.Params.MaxGasPerExecution; maxGas > 0 && record.GasLimit > maxGas {
		record.GasLimit = maxGas
	}
	if err := record.Validate(); err != nil {
		return types.InternalMessage{}, err
	}
	idx, contract, found := findContractWithIndex(k.genesis.State.Contracts, record.SourceContractUser)
	if !found {
		return types.InternalMessage{}, errors.New(types.ErrContractNotFound + ": source contract not found")
	}
	if err := types.EnsureContractLifecycleAction(contract, types.ContractLifecycleActionEmitInternalMessage); err != nil {
		return types.InternalMessage{}, err
	}
	destinationIdx := -1
	var destination types.Contract
	if didx, foundContract, ok := findContractWithIndex(k.genesis.State.Contracts, record.DestinationAccount); ok {
		destinationIdx = didx
		destination = foundContract
		if err := types.EnsureContractLifecycleAction(destination, types.ContractLifecycleActionReceiveInternal); err != nil {
			return types.InternalMessage{}, err
		}
	}
	contract, rentCharged, err := k.chargeContractRentAt(k.runtimeCtx, idx, contract, record.Height)
	if err != nil {
		return types.InternalMessage{}, errors.New(types.ErrStorageRent + ": contract has storage rent debt")
	}
	next := k.genesis
	// Work on a private copy of the contract set so balances mutated below never
	// corrupt the live genesis on the error paths that follow. The per-branch
	// fund debit/credit is applied inside the destination branches.
	next.State.Contracts = append([]types.Contract(nil), k.genesis.State.Contracts...)
	next.State.Contracts[idx] = contract
	receiptOp := "internal_message_delivered"
	var code types.CodeRecord
	var module avm.Module
	var executable bool
	if destinationIdx >= 0 {
		// Conserve funds: debit the verified source, then credit the destination.
		// Writing the debit first and re-reading the destination makes a self-send
		// (source == destination) net to zero, and rejects underfunded messages
		// rather than minting value. See SEC-HIGH: fund debit on internal messages.
		if record.Funds > 0 {
			src := next.State.Contracts[idx]
			if src.Balance < record.Funds {
				return types.InternalMessage{}, errors.New(types.ErrExecutionFailed + ": source contract has insufficient balance")
			}
			src.Balance -= record.Funds
			next.State.Contracts[idx] = src
			destination = next.State.Contracts[destinationIdx]
			creditedBalance, err := checkedAdd(destination.Balance, record.Funds, "destination contract balance overflow")
			if err != nil {
				return types.InternalMessage{}, err
			}
			destination.Balance = creditedBalance
			next.State.Contracts[destinationIdx] = destination
		}
		code, found := findCodeRecord(k.genesis.State.Codes, destination.CodeID)
		if !found {
			return types.InternalMessage{}, errors.New(types.ErrInvalidBytecode + ": stored code not found for internal destination")
		}
		module, executable, err = loadAVMModule(code)
		if err != nil {
			return types.InternalMessage{}, err
		}
		if executable {
			storage, decodable, err := decodeContractSnapshot(destination.Data)
			if err != nil {
				return types.InternalMessage{}, err
			}
			if !decodable {
				return types.InternalMessage{}, errors.New(types.ErrExecutionFailed + ": destination contract state is not AVM snapshot compatible")
			}
			runner, err := avm.NewRunner(avm.DefaultParams())
			if err != nil {
				return types.InternalMessage{}, err
			}
			runtimeCtx, err := k.buildAVMContext(avm.EntryReceiveInternal, destination, record.SourceContractUser, record.Body, record.Funds, record.GasLimit, record.Height, record.LogicalTime, record.Opcode, record.QueryID, false)
			if err != nil {
				return types.InternalMessage{}, err
			}
			exec, err := runner.Run(module, storage, runtimeCtx)
			if err != nil {
				return types.InternalMessage{}, err
			}
			if exec.ResultCode != async.ResultOK {
				return types.InternalMessage{}, fmt.Errorf("%s: avm internal execution failed with code %d", types.ErrExecutionFailed, exec.ResultCode)
			}
			destination.Data = encodeSnapshotStorage(exec.State)
			destination.LogicalTime++
			destination.UpdatedHeight = record.Height
			destination.StateRoot = types.ComputeContractStateRoot(destination)
			destinationStorageBytes, err := k.contractStorageBytes(destination)
			if err != nil {
				return types.InternalMessage{}, err
			}
			if destinationStorageBytes > k.genesis.Params.MaxContractStorageBytes {
				return types.InternalMessage{}, errors.New(types.ErrStorageRent + ": destination contract storage exceeds configured limit")
			}
			destination.StorageBytes = destinationStorageBytes
			next.State.Contracts[destinationIdx] = destination
			if err := k.appendAVMOutgoingMessages(&next, destination, exec.Outgoing, record.Height); err != nil {
				return types.InternalMessage{}, err
			}
			receiptOp = "internal_message_executed"
		}
	} else if record.StateInit != nil {
		if record.SourceContractUser == "" {
			return types.InternalMessage{}, errors.New(types.ErrContractNotFound + ": internal auto-deploy requires source contract user")
		}
		expectedUser, _, err := types.DeriveContractAddressFromStateInit(types.DefaultContractChainID, types.DefaultContractNamespace, record.SourceContractUser, *record.StateInit, k.genesis.Params)
		if err != nil {
			return types.InternalMessage{}, err
		}
		if expectedUser != record.DestinationAccount {
			return types.InternalMessage{}, errors.New(types.ErrContractNotFound + ": internal state init address does not match destination")
		}
		// Verify the source can cover the attached funds BEFORE instantiate, which
		// credits the auto-deployed contract with record.Funds and self-commits to
		// k.genesis. Without this pre-check an insufficient-balance error after
		// instantiate would leave the new contract holding fabricated funds with no
		// matching source debit — minting value. See SEC-HIGH: fund debit.
		if record.Funds > 0 && next.State.Contracts[idx].Balance < record.Funds {
			return types.InternalMessage{}, errors.New(types.ErrExecutionFailed + ": source contract has insufficient balance")
		}
		_, err = k.instantiateContract(k.runtimeCtx, types.MsgInstantiateContract{
			Creator:       record.SourceContractUser,
			CodeID:        record.StateInit.Normalize().CodeID,
			ChainID:       types.DefaultContractChainID,
			Namespace:     types.DefaultContractNamespace,
			StateInit:     record.StateInit,
			InitMsg:       append([]byte(nil), record.StateInit.InitData...),
			Funds:         record.Funds,
			Height:        record.Height,
			SchemaVersion: 1,
		})
		if err != nil {
			return types.InternalMessage{}, err
		}
		next = k.genesis
		next.State.Contracts = append([]types.Contract(nil), k.genesis.State.Contracts...)
		// The auto-deployed contract already received record.Funds as its initial
		// balance; apply the matching source debit now that instantiate re-synced
		// next (value conserved).
		if record.Funds > 0 {
			srcNowIdx, srcNow, srcFound := findContractWithIndex(next.State.Contracts, record.SourceContractUser)
			if !srcFound {
				return types.InternalMessage{}, errors.New(types.ErrContractNotFound + ": source contract not found")
			}
			if srcNow.Balance < record.Funds {
				return types.InternalMessage{}, errors.New(types.ErrExecutionFailed + ": source contract has insufficient balance")
			}
			srcNow.Balance -= record.Funds
			next.State.Contracts[srcNowIdx] = srcNow
		}
		destinationIdx, destination, found = findContractWithIndex(k.genesis.State.Contracts, record.DestinationAccount)
		if !found {
			return types.InternalMessage{}, errors.New(types.ErrContractNotFound + ": auto-deployed contract not found")
		}
		code, found = findCodeRecord(k.genesis.State.Codes, destination.CodeID)
		if !found {
			return types.InternalMessage{}, errors.New(types.ErrInvalidBytecode + ": stored code not found for auto-deployed destination")
		}
		module, executable, err = loadAVMModule(code)
		if err != nil {
			return types.InternalMessage{}, err
		}
		if executable {
			storage, decodable, err := decodeContractSnapshot(destination.Data)
			if err != nil {
				return types.InternalMessage{}, err
			}
			if !decodable {
				return types.InternalMessage{}, errors.New(types.ErrExecutionFailed + ": destination contract state is not AVM snapshot compatible")
			}
			runner, err := avm.NewRunner(avm.DefaultParams())
			if err != nil {
				return types.InternalMessage{}, err
			}
			runtimeCtx, err := k.buildAVMContext(avm.EntryReceiveInternal, destination, record.SourceContractUser, record.Body, record.Funds, record.GasLimit, record.Height, record.LogicalTime, record.Opcode, record.QueryID, false)
			if err != nil {
				return types.InternalMessage{}, err
			}
			exec, err := runner.Run(module, storage, runtimeCtx)
			if err != nil {
				return types.InternalMessage{}, err
			}
			if exec.ResultCode != async.ResultOK {
				return types.InternalMessage{}, fmt.Errorf("%s: avm internal execution failed with code %d", types.ErrExecutionFailed, exec.ResultCode)
			}
			destination.Data = encodeSnapshotStorage(exec.State)
			destination.LogicalTime++
			destination.UpdatedHeight = record.Height
			destination.StateRoot = types.ComputeContractStateRoot(destination)
			destinationStorageBytes, err := k.contractStorageBytes(destination)
			if err != nil {
				return types.InternalMessage{}, err
			}
			if destinationStorageBytes > k.genesis.Params.MaxContractStorageBytes {
				return types.InternalMessage{}, errors.New(types.ErrStorageRent + ": destination contract storage exceeds configured limit")
			}
			destination.StorageBytes = destinationStorageBytes
			next.State.Contracts[destinationIdx] = destination
			if err := k.appendAVMOutgoingMessages(&next, destination, exec.Outgoing, record.Height); err != nil {
				return types.InternalMessage{}, err
			}
			receiptOp = "internal_message_executed"
		}
	} else {
		return types.InternalMessage{}, errors.New(types.ErrContractNotFound + ": internal message destination not found")
	}
	// Dequeue the delivered message. Messages the destination emitted during
	// execution carry a different source (hence a different ID) and remain
	// queued; only the single entry matching the delivered ID is drained.
	dequeued := false
	remaining := make([]types.InternalMessage, 0, len(next.State.InternalMessages))
	for i := range next.State.InternalMessages {
		if !dequeued {
			qid := next.State.InternalMessages[i].MessageID
			if qid == "" {
				qid = types.ComputeInternalMessageID(next.State.InternalMessages[i])
			}
			if qid == wantID {
				dequeued = true
				continue
			}
		}
		remaining = append(remaining, next.State.InternalMessages[i])
	}
	if !dequeued {
		return types.InternalMessage{}, errors.New(types.ErrExecutionFailed + ": delivered internal message missing from queue")
	}
	next.State.InternalMessages = remaining
	next.State.Receipts = append(next.State.Receipts, newContractReceipt(record.SourceContractUser, record.SourceContractUser, receiptOp, types.ExitCodeOK, record.Funds, record.GasLimit, record.LogicalTime, record.Height))
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return types.InternalMessage{}, err
	}
	if rentCharged > 0 {
		if err := k.chargeRentToReserve(k.runtimeCtx, contract, rentCharged); err != nil {
			return types.InternalMessage{}, err
		}
	}
	k.genesis = next
	return record, nil
}

// EndBlocker autonomously drains the pending internal-message queue up to
// Params.MaxInternalMessageGasPerBlock AVM gas per block, so a contract's
// outgoing message (queued by appendAVMOutgoingMessages) is delivered without
// a separate signed MsgReceiveInternalMessage transaction. Zero budget (the
// default in DefaultParams, so genesis behavior is unchanged until governance
// explicitly raises it) disables autonomous delivery entirely, preserving
// tx-only-delivery behavior.
// Delivery reuses ReceiveInternalMessage verbatim: the same content-hash
// match that authenticates a manually-submitted delivery also authenticates
// this one, since the record being delivered IS the queued record. A message
// that fails to deliver (e.g. destination has a storage-rent debt) is left
// queued and retried on a later block; it is never dropped or double-charged,
// since ReceiveInternalMessage only mutates k.genesis on its success path.
//
// The budget check runs on the in-memory k.genesis.Params BEFORE
// loadForBlock, so a disabled drain (the default) never touches the store
// and never overwrites k.genesis with the persisted snapshot. That reload
// would otherwise clobber any change a caller applied directly to the
// in-memory keeper without going through the persist-on-write msg-server
// wrapper (writeGenesis) -- exactly what every EndBlock invocation used to
// never do before this hook existed. The narrow cost is that a governance
// param update raising the budget from a genesis restored from disk (not
// yet reflected in a freshly-constructed keeper's in-memory default) only
// takes effect once some other tx first refreshes k.genesis; that is a
// one-tx delay, not a correctness or consensus-safety gap.
func (k *Keeper) EndBlocker(ctx sdk.Context) error {
	if k.genesis.Params.MaxInternalMessageGasPerBlock == 0 {
		return nil
	}
	if err := k.loadForBlock(ctx); err != nil {
		return err
	}
	budget := k.genesis.Params.MaxInternalMessageGasPerBlock
	if budget == 0 || len(k.genesis.State.InternalMessages) == 0 {
		return nil
	}
	maxGas := k.genesis.Params.MaxGasPerExecution
	// Snapshot the queue up front: ReceiveInternalMessage dequeues by
	// mutating k.genesis.State.InternalMessages, so iterating the live slice
	// while it shrinks would skip or repeat entries. Attempts stay in the
	// queue's FIFO order.
	queued := append([]types.InternalMessage(nil), k.genesis.State.InternalMessages...)
	delivered := 0
	for _, msg := range queued {
		gasCost := msg.GasLimit
		if gasCost == 0 || gasCost > maxGas {
			gasCost = maxGas
		}
		if gasCost > budget {
			break
		}
		budget -= gasCost
		if k.deliverQueuedInternalMessage(msg) {
			delivered++
		}
	}
	if delivered == 0 {
		return nil
	}
	return k.writeGenesis(ctx)
}

// deliverQueuedInternalMessage attempts one autonomous delivery of a message
// already sitting in the pending queue, recovering from any panic during AVM
// execution so a bug in one contract cannot halt block production for the
// whole chain. It reports whether delivery succeeded; on failure the message
// stays queued for a later block attempt.
func (k *Keeper) deliverQueuedInternalMessage(msg types.InternalMessage) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()
	_, err := k.ReceiveInternalMessage(types.MsgReceiveInternalMessage{
		SourceContractUser: msg.SourceContractUser,
		DestinationAccount: msg.DestinationAccount,
		Funds:              msg.Funds,
		Opcode:             msg.Opcode,
		QueryID:            msg.QueryID,
		Body:               append([]byte(nil), msg.Body...),
		StateInit:          msg.StateInit,
		Bounce:             msg.Bounce,
		Deadline:           msg.Deadline,
		GasLimit:           msg.GasLimit,
		LogicalTime:        msg.LogicalTime,
		MessageID:          msg.MessageID,
		Height:             msg.Height,
	})
	return err == nil
}

func (k *Keeper) appendAVMOutgoingMessages(next *types.GenesisState, source types.Contract, outgoing []async.MessageEnvelope, height uint64) error {
	if len(outgoing) == 0 {
		return nil
	}
	for i, envelope := range outgoing {
		if envelope.Value.Denom != "" && envelope.Value.Denom != storageRentBaseDenom {
			return fmt.Errorf("%s: emitted message denom must be %q", types.ErrExecutionFailed, storageRentBaseDenom)
		}
		if !envelope.Value.Amount.IsUint64() {
			return fmt.Errorf("%s: emitted message value exceeds uint64", types.ErrExecutionFailed)
		}
		msgHeight := height
		if envelope.DeliverAtBlock != 0 {
			msgHeight = envelope.DeliverAtBlock
		}
		msgLogicalTime := source.LogicalTime + uint64(i) + 1
		if envelope.CreatedLogicalTime != 0 {
			msgLogicalTime = envelope.CreatedLogicalTime
		}
		internal := types.InternalMessage{
			SourceContractUser: source.AddressUser,
			DestinationAccount: aetraaddress.FormatAccAddress(envelope.Destination),
			Funds:              envelope.Value.Amount.Uint64(),
			Opcode:             envelope.Opcode,
			QueryID:            envelope.QueryID,
			Body:               append([]byte(nil), envelope.Body...),
			StateInit:          envelope.StateInit,
			Bounce:             envelope.Bounce,
			Deadline:           envelope.DeadlineBlock,
			GasLimit:           envelope.GasLimit,
			LogicalTime:        msgLogicalTime,
			Height:             msgHeight,
		}
		if internal.MessageID == "" {
			internal.MessageID = types.ComputeInternalMessageID(internal)
		}
		if err := internal.Validate(); err != nil {
			return err
		}
		if len(next.State.InternalMessages) >= types.MaxInternalMessageQueueDepth {
			return fmt.Errorf("%s: internal message queue at capacity (%d)", types.ErrExecutionFailed, types.MaxInternalMessageQueueDepth)
		}
		next.State.InternalMessages = append(next.State.InternalMessages, internal)
	}
	return nil
}

func newContractReceipt(contractAddress, actor, operation string, exitCode uint32, amount, gasUsed, logicalTime, height uint64) types.ContractReceipt {
	receipt := types.ContractReceipt{
		ContractAddress: contractAddress,
		Actor:           actor,
		Operation:       operation,
		ExitCode:        exitCode,
		Amount:          amount,
		GasUsed:         gasUsed,
		LogicalTime:     logicalTime,
		Height:          height,
	}
	receipt.ReceiptID = types.ComputeContractReceiptID(receipt)
	return receipt
}

func (k Keeper) AssetOwner(req types.QueryAssetOwnerRequest) (types.QueryAssetOwnerResponse, error) {
	if req.AssetType == "" {
		return types.QueryAssetOwnerResponse{}, fmt.Errorf("contract asset type must not be empty")
	}
	if err := types.ValidateUserFacingAEAddress("asset contract address", req.ContractAddressUser); err != nil {
		return types.QueryAssetOwnerResponse{}, err
	}
	if req.AssetID == "" {
		return types.QueryAssetOwnerResponse{}, errors.New("asset id is required")
	}
	for _, asset := range k.genesis.State.AssetOwnership {
		if asset.AssetType == req.AssetType && asset.ContractAddressUser == req.ContractAddressUser && asset.AssetID == req.AssetID {
			return types.QueryAssetOwnerResponse{Owner: asset.Owner, Found: true}, nil
		}
	}
	return types.QueryAssetOwnerResponse{}, nil
}

func (k *Keeper) SetAssetOwner(record types.AssetOwnershipRecord) error {
	if err := record.Validate(); err != nil {
		return err
	}
	next := k.genesis
	next.State.AssetOwnership = upsertAsset(next.State.AssetOwnership, record)
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return err
	}
	k.genesis = next
	return nil
}

func (k *Keeper) ensureActiveWallet(ctx context.Context, address string, operation string) error {
	if k.accountStatusReader == nil {
		return nil
	}
	status, found, err := k.accountStatusReader.AccountStatus(ctx, address)
	if err != nil {
		return err
	}
	if !found || status == accountStatusInactive {
		return fmt.Errorf("%s: %s", operation, types.ErrAccountInactive)
	}
	if status == accountStatusFrozen {
		return fmt.Errorf("%s: %s", operation, types.ErrAccountFrozen)
	}
	if status != accountStatusActive {
		return fmt.Errorf("%s: unsupported account status %q", operation, status)
	}
	return nil
}

func (k *Keeper) chargeContractRentAt(ctx context.Context, idx int, contract types.Contract, height uint64) (types.Contract, uint64, error) {
	prevBalance := contract.Balance
	contract, changed, err := k.chargeRent(contract, height)
	if err != nil {
		return types.Contract{}, 0, err
	}
	if contract.StorageRentDebt > 0 {
		contract.Status = k.storageRentFrozenStatus(contract)
		if err := k.persistContractAt(idx, contract); err != nil {
			return types.Contract{}, 0, err
		}
		return contract, 0, errors.New(types.ErrStorageRent + ": contract has storage rent debt")
	}
	if changed {
		return contract, prevBalance - contract.Balance, nil
	}
	return contract, 0, nil
}

func (k Keeper) storageRentFrozenStatus(contract types.Contract) string {
	if contract.Status == types.ContractStatusFrozenLimited || hasAnyNativeStakingCapability(k.genesis.State.StakingCapabilities, contract.AddressUser) {
		return types.ContractStatusFrozenLimited
	}
	return types.ContractStatusFrozen
}

func (k *Keeper) persistContractAt(idx int, contract types.Contract) error {
	if idx < 0 || idx >= len(k.genesis.State.Contracts) {
		return errors.New(types.ErrContractNotFound + ": contract index out of bounds")
	}
	next := k.genesis
	next.State.Contracts[idx] = contract
	next = types.RefreshStateRoot(next)
	if err := next.Validate(); err != nil {
		return err
	}
	k.genesis = next
	return nil
}

func (k Keeper) storageRentPerByteBlock() uint64 {
	if k.storageRentRateProvider != nil {
		return k.storageRentRateProvider.StorageRentRatePerByteBlock()
	}
	return k.genesis.Params.StorageRentPerByteBlock
}

func (k Keeper) chargeRent(contract types.Contract, height uint64) (types.Contract, bool, error) {
	if height < contract.LastStorageChargeHeight {
		return types.Contract{}, false, errors.New(types.ErrStorageRent + ": contract storage rent height must be monotonic")
	}
	if height <= contract.LastStorageChargeHeight || contract.StorageBytes == 0 || k.storageRentPerByteBlock() == 0 {
		return contract, false, nil
	}
	blocks := height - contract.LastStorageChargeHeight
	charge, err := checkedMul(blocks, contract.StorageBytes, "contract storage rent overflow")
	if err != nil {
		return types.Contract{}, false, err
	}
	charge, err = checkedMul(charge, k.storageRentPerByteBlock(), "contract storage rent overflow")
	if err != nil {
		return types.Contract{}, false, err
	}
	if contract.Balance >= charge {
		contract.Balance -= charge
	} else {
		unpaid := charge - contract.Balance
		debt, err := checkedAdd(contract.StorageRentDebt, unpaid, "contract storage rent debt overflow")
		if err != nil {
			return types.Contract{}, false, err
		}
		contract.StorageRentDebt = debt
		contract.Balance = 0
	}
	contract.LastStorageChargeHeight = height
	return contract, true, nil
}

func (k Keeper) contractStorageBytes(contract types.Contract) (uint64, error) {
	code, found := findCode(k.genesis.State.Codes, contract.CodeID)
	if !found {
		return 0, errors.New(types.ErrContractNotFound + ": contract code not found")
	}
	return contractStorageBytesForCode(code, contract.Data)
}

func (k *Keeper) collectRentPayment(ctx context.Context, payer string, amount uint64) error {
	if k.bankKeeper == nil || ctx == nil {
		return nil
	}
	payerAddr, err := aetraaddress.ParseAccAddress(payer)
	if err != nil {
		return err
	}
	coin := sdk.NewCoins(sdk.NewCoin(storageRentBaseDenom, sdkmath.NewIntFromUint64(amount)))
	return k.bankKeeper.SendCoinsFromAccountToModule(ctx, payerAddr, storageRentReserveModule, coin)
}

func (k *Keeper) chargeRentToReserve(ctx context.Context, contract types.Contract, amount uint64) error {
	if k.bankKeeper == nil || amount == 0 {
		return nil
	}
	return k.collectRentPayment(ctx, contract.Creator, amount)
}

func (k Keeper) authorizeContractUpgradeActor(contract types.Contract, actor string) error {
	actor = strings.TrimSpace(actor)
	if contract.SystemOwned {
		if actor != k.genesis.Params.Authority {
			return errors.New(types.ErrUnauthorized + ": system contract upgrade requires governance authority")
		}
		return nil
	}
	if err := types.ValidateUserFacingAEAddress("contract upgrade actor", actor); err != nil {
		return err
	}
	if actor != contract.Admin {
		return errors.New(types.ErrUnauthorized + ": contract upgrade requires admin")
	}
	return nil
}

func receiptActor(contract types.Contract, actor string) string {
	if contract.SystemOwned {
		return ""
	}
	return actor
}

func applyContractMigration(current []byte, handler string, payload []byte) ([]byte, error) {
	switch strings.TrimSpace(handler) {
	case "schema_only":
		return append([]byte(nil), current...), nil
	case "replace":
		return append([]byte(nil), payload...), nil
	case "append":
		out := append([]byte(nil), current...)
		out = append(out, payload...)
		return out, nil
	case "fail":
		return nil, errors.New(types.ErrExecutionFailed + ": contract migration handler failed")
	default:
		return nil, errors.New(types.ErrExecutionFailed + ": unsupported contract migration handler")
	}
}

func contractStorageBytesForCode(code types.CodeRecord, data []byte) (uint64, error) {
	dataBytes := uint64(len(data))
	return checkedAdd(code.CodeBytes, dataBytes, "contract storage size overflow")
}

func checkedAdd(left, right uint64, message string) (uint64, error) {
	if left > math.MaxUint64-right {
		return 0, errors.New(message)
	}
	return left + right, nil
}

func checkedMul(left, right uint64, message string) (uint64, error) {
	if left != 0 && right > math.MaxUint64/left {
		return 0, errors.New(message)
	}
	return left * right, nil
}

func upsertCode(codes []types.CodeRecord, code types.CodeRecord) []types.CodeRecord {
	out := append([]types.CodeRecord(nil), codes...)
	for i := range out {
		if out[i].CodeID == code.CodeID {
			out[i] = code
			return out
		}
	}
	return append(out, code)
}

func upsertCapability(caps []types.ContractCapability, cap types.ContractCapability) []types.ContractCapability {
	out := append([]types.ContractCapability(nil), caps...)
	for i := range out {
		if out[i].ContractAddressUser == cap.ContractAddressUser && out[i].PoolID == cap.PoolID && out[i].Capability == cap.Capability {
			out[i] = cap
			return out
		}
	}
	return append(out, cap)
}

func upsertAsset(assets []types.AssetOwnershipRecord, record types.AssetOwnershipRecord) []types.AssetOwnershipRecord {
	out := append([]types.AssetOwnershipRecord(nil), assets...)
	for i := range out {
		if out[i].AssetType == record.AssetType && out[i].ContractAddressUser == record.ContractAddressUser && out[i].AssetID == record.AssetID {
			out[i] = record
			return out
		}
	}
	return append(out, record)
}

func findCode(codes []types.CodeRecord, codeID string) (types.CodeRecord, bool) {
	for _, code := range codes {
		if code.CodeID == codeID {
			return code, true
		}
	}
	return types.CodeRecord{}, false
}

// findCodeByModuleHash resolves a stored code record by the plain module
// hash hex(sha256(bytecode)) — the code identity the AVM runtime embeds in
// contract-built StateInits.
func findCodeByModuleHash(codes []types.CodeRecord, moduleHashHex string) (types.CodeRecord, bool) {
	for _, code := range codes {
		if codeHashMatchesModule(moduleHashHex, code.Bytecode) {
			return code, true
		}
	}
	return types.CodeRecord{}, false
}

// codeHashMatchesModule reports whether hashHex is the plain module hash
// hex(sha256(bytecode)) of the given bytecode.
func codeHashMatchesModule(hashHex string, bytecode []byte) bool {
	want := strings.ToLower(strings.TrimSpace(hashHex))
	if want == "" || len(bytecode) == 0 {
		return false
	}
	sum := sha256.Sum256(bytecode)
	return hex.EncodeToString(sum[:]) == want
}

func findContract(contracts []types.Contract, address string) (types.Contract, bool) {
	_, contract, found := findContractWithIndex(contracts, address)
	return contract, found
}

func findContractWithIndex(contracts []types.Contract, address string) (int, types.Contract, bool) {
	for idx, contract := range contracts {
		if contract.AddressUser == address {
			return idx, contract, true
		}
	}
	return -1, types.Contract{}, false
}

func findSecurityAttestation(attestations []types.ContractSecurityAttestation, attestationID string) (types.ContractSecurityAttestation, bool) {
	for _, attestation := range attestations {
		if attestation.AttestationID == attestationID {
			return attestation, true
		}
	}
	return types.ContractSecurityAttestation{}, false
}

func hasCapability(caps []types.ContractCapability, contract string, poolID string) bool {
	for _, cap := range caps {
		if cap.ContractAddressUser == contract && cap.PoolID == poolID && cap.Capability == types.NativeStakingCapability {
			return true
		}
	}
	return false
}

func hasAnyNativeStakingCapability(caps []types.ContractCapability, contract string) bool {
	for _, cap := range caps {
		if cap.ContractAddressUser == contract && cap.Capability == types.NativeStakingCapability {
			return true
		}
	}
	return false
}

// loadForBlock hydrates the in-memory genesis from the committed store using the
// live block context, and points runtimeCtx at it. It MUST run at the start of
// every state-changing handler so a restarted or state-synced node — where
// InitGenesis is not re-run — operates on the same committed state as a
// continuously-running node instead of the empty default. Reading through the
// block context observes writes made earlier in the same block, so sequential
// handlers within a block stay consistent. See SEC-HIGH: contracts keeper drives
// state off in-memory genesis never restored on restart/state-sync.
func (k *Keeper) loadForBlock(ctx context.Context) error {
	k.runtimeCtx = ctx
	if k.storeService == nil {
		return nil
	}
	bz, err := k.storeService.OpenKVStore(ctx).Get(genesisKey)
	if err != nil {
		return err
	}
	if len(bz) == 0 {
		return nil
	}
	var gs types.GenesisState
	if err := json.Unmarshal(bz, &gs); err != nil {
		return err
	}
	gs = types.RefreshStateRoot(gs)
	if err := gs.Validate(); err != nil {
		return err
	}
	k.genesis = gs
	return nil
}

func (k Keeper) writeGenesis(ctx context.Context) error {
	if k.storeService == nil {
		return nil
	}
	gs := types.RefreshStateRoot(k.genesis)
	if err := gs.Validate(); err != nil {
		return err
	}
	bz, err := json.Marshal(gs)
	if err != nil {
		return err
	}
	return k.storeService.OpenKVStore(ctx).Set(genesisKey, bz)
}
