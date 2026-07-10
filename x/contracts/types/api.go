package types

import (
	"errors"
	"fmt"
	"strings"
)

const (
	MaxContractMetadataBytes = 1024
	MaxContractPayloadBytes  = 64 * 1024
	MaxContractQueryLimit    = 100

	// MaxCommentBytes bounds the textComment memo carried on internal
	// messages. Matches async.MaxCommentBytes (kept in sync here to avoid a
	// contracts->async import cycle). Comment bytes are charged through the
	// normal per-byte fee, so a longer memo simply costs more.
	MaxCommentBytes = 512

	// MaxInternalMessageQueueDepth bounds the pending internal-message queue.
	// The queue is currently append-only (no autonomous drain), so without a
	// ceiling contract activity grows module state without bound until block
	// production stalls. Enqueue is rejected once the queue is at this depth,
	// turning an unbounded-growth halt into a bounded, deterministic rejection.
	// See SEC-HIGH: whole-module state re-serialized with unbounded queue growth.
	MaxInternalMessageQueueDepth = 65536

	// MaxRetainedReceipts bounds the per-module contract receipt log. Receipts
	// are appended by every handler and the whole module state is re-serialized
	// each block, so without a ceiling the store grows without bound. Pruning
	// keeps only the most recent MaxRetainedReceipts entries and is applied
	// inside RefreshStateRoot so the in-memory genesis and the persisted store
	// prune to the identical set on every node. See SEC-HIGH: bound receipt log.
	MaxRetainedReceipts = 8192
)

type MsgDeployContract struct {
	Creator        string `protobuf:"bytes,1,opt,name=creator,proto3" json:"creator,omitempty"`
	CodeID         string `protobuf:"bytes,2,opt,name=code_id,json=codeId,proto3" json:"code_id,omitempty"`
	Salt           string `protobuf:"bytes,3,opt,name=salt,proto3" json:"salt,omitempty"`
	InitPayload    []byte `protobuf:"bytes,4,opt,name=init_payload,json=initPayload,proto3" json:"init_payload,omitempty"`
	InitialBalance uint64 `protobuf:"varint,5,opt,name=initial_balance,json=initialBalance,proto3" json:"initial_balance,omitempty"`
	Admin          string `protobuf:"bytes,6,opt,name=admin,proto3" json:"admin,omitempty"`
	Metadata       []byte `protobuf:"bytes,7,opt,name=metadata,proto3" json:"metadata,omitempty"`
	// Proto field/JSON names are avm_chain_id/avm_namespace (not chain_id/
	// namespace) so autocli's Msg-field-derived flags don't collide with the
	// standard --chain-id tx flag every autocli tx command already gets from
	// flags.AddTxFlagsToCmd; see cosmossdk.io/client/v2/autocli.
	ChainID       string     `protobuf:"bytes,8,opt,name=avm_chain_id,json=avmChainId,proto3" json:"avm_chain_id,omitempty"`
	Namespace     string     `protobuf:"bytes,9,opt,name=avm_namespace,json=avmNamespace,proto3" json:"avm_namespace,omitempty"`
	StateInit     *StateInit `protobuf:"bytes,10,opt,name=state_init,json=stateInit,proto3" json:"state_init,omitempty"`
	Upgradeable   bool       `protobuf:"varint,11,opt,name=upgradeable,proto3" json:"upgradeable,omitempty"`
	SystemOwned   bool       `protobuf:"varint,12,opt,name=system_owned,json=systemOwned,proto3" json:"system_owned,omitempty"`
	SchemaVersion uint64     `protobuf:"varint,13,opt,name=schema_version,json=schemaVersion,proto3" json:"schema_version,omitempty"`
	Height        uint64     `protobuf:"varint,14,opt,name=height,proto3" json:"height,omitempty"`
}

type MsgExecuteExternal struct {
	Sender          string `protobuf:"bytes,1,opt,name=sender,proto3" json:"sender,omitempty"`
	ContractAddress string `protobuf:"bytes,2,opt,name=contract_address,json=contractAddress,proto3" json:"contract_address,omitempty"`
	Payload         []byte `protobuf:"bytes,3,opt,name=payload,proto3" json:"payload,omitempty"`
	Funds           uint64 `protobuf:"varint,4,opt,name=funds,proto3" json:"funds,omitempty"`
	GasLimit        uint64 `protobuf:"varint,5,opt,name=gas_limit,json=gasLimit,proto3" json:"gas_limit,omitempty"`
	Metadata        []byte `protobuf:"bytes,6,opt,name=metadata,proto3" json:"metadata,omitempty"`
	// See the matching comment on MsgDeployContract for why these use
	// avm_chain_id/avm_namespace instead of chain_id/namespace.
	ChainID   string     `protobuf:"bytes,7,opt,name=avm_chain_id,json=avmChainId,proto3" json:"avm_chain_id,omitempty"`
	Namespace string     `protobuf:"bytes,8,opt,name=avm_namespace,json=avmNamespace,proto3" json:"avm_namespace,omitempty"`
	StateInit *StateInit `protobuf:"bytes,9,opt,name=state_init,json=stateInit,proto3" json:"state_init,omitempty"`
	Height    uint64     `protobuf:"varint,10,opt,name=height,proto3" json:"height,omitempty"`
	// Opcode is the @message discriminator of the external message body. The
	// AVM routes a union-typed incomingExternal via OpReadMsgOpcode, which
	// reads this value from the runtime context; without it a multi-variant
	// ExternalMsg cannot be matched and every external call silently falls to
	// the `else` arm. A single-variant union with opcode 0 keeps working.
	Opcode uint32 `protobuf:"varint,11,opt,name=opcode,proto3" json:"opcode,omitempty"`
}

type MsgExecuteInternal struct {
	Message InternalMessage `protobuf:"bytes,1,opt,name=message,proto3" json:"message"`
	Height  uint64          `protobuf:"varint,2,opt,name=height,proto3" json:"height,omitempty"`
}

type MsgSendInternalMessage struct {
	Message InternalMessage `protobuf:"bytes,1,opt,name=message,proto3" json:"message"`
	Height  uint64          `protobuf:"varint,2,opt,name=height,proto3" json:"height,omitempty"`
}

type MsgUpdateContractParams struct {
	Authority string `protobuf:"bytes,1,opt,name=authority,proto3" json:"authority,omitempty"`
	Params    Params `protobuf:"bytes,2,opt,name=params,proto3" json:"params"`
}

type MsgUpdateContractParamsResponse struct {
	StateRoot string `protobuf:"bytes,1,opt,name=state_root,json=stateRoot,proto3" json:"state_root,omitempty"`
}

type PageRequest struct {
	Limit uint32 `protobuf:"varint,1,opt,name=limit,proto3" json:"limit,omitempty"`
}

type QueryParamsRequest struct{}

type QueryParamsResponse struct {
	Params Params `protobuf:"bytes,1,opt,name=params,proto3" json:"params"`
}

type QueryCodeRequest struct {
	CodeID string `protobuf:"bytes,1,opt,name=code_id,json=codeId,proto3" json:"code_id,omitempty"`
}

type QueryCodeResponse struct {
	Code  CodeRecord `protobuf:"bytes,1,opt,name=code,proto3" json:"code"`
	Found bool       `protobuf:"varint,2,opt,name=found,proto3" json:"found,omitempty"`
}

type QueryCodesRequest struct {
	Pagination PageRequest `protobuf:"bytes,1,opt,name=pagination,proto3" json:"pagination"`
}

type QueryCodesResponse struct {
	Codes []CodeRecord `protobuf:"bytes,1,rep,name=codes,proto3" json:"codes"`
}

type QueryContractsRequest struct {
	Pagination PageRequest `protobuf:"bytes,1,opt,name=pagination,proto3" json:"pagination"`
}

type QueryContractsResponse struct {
	Contracts []Contract `protobuf:"bytes,1,rep,name=contracts,proto3" json:"contracts"`
}

type QueryContractStorageRequest struct {
	ContractAddress string      `protobuf:"bytes,1,opt,name=contract_address,json=contractAddress,proto3" json:"contract_address,omitempty"`
	KeyPrefix       []byte      `protobuf:"bytes,2,opt,name=key_prefix,json=keyPrefix,proto3" json:"key_prefix,omitempty"`
	Pagination      PageRequest `protobuf:"bytes,3,opt,name=pagination,proto3" json:"pagination"`
}

type QueryContractStorageResponse struct {
	Entries []ContractStorageEntry `protobuf:"bytes,1,rep,name=entries,proto3" json:"entries"`
}

type QueryContractReceiptsRequest struct {
	ContractAddress string      `protobuf:"bytes,1,opt,name=contract_address,json=contractAddress,proto3" json:"contract_address,omitempty"`
	Pagination      PageRequest `protobuf:"bytes,2,opt,name=pagination,proto3" json:"pagination"`
}

type QueryContractReceiptsResponse struct {
	Receipts []ContractReceipt `protobuf:"bytes,1,rep,name=receipts,proto3" json:"receipts"`
}

type QueryContractQueueRequest struct {
	ContractAddress string      `protobuf:"bytes,1,opt,name=contract_address,json=contractAddress,proto3" json:"contract_address,omitempty"`
	Pagination      PageRequest `protobuf:"bytes,2,opt,name=pagination,proto3" json:"pagination"`
}

type QueryContractQueueResponse struct {
	Messages []InternalMessage `protobuf:"bytes,1,rep,name=messages,proto3" json:"messages"`
}

type QueryContractEventsRequest struct {
	ContractAddress string      `protobuf:"bytes,1,opt,name=contract_address,json=contractAddress,proto3" json:"contract_address,omitempty"`
	Pagination      PageRequest `protobuf:"bytes,2,opt,name=pagination,proto3" json:"pagination"`
}

type QueryContractEventsResponse struct{}

type QueryContractStateRootRequest struct {
	ContractAddress string `protobuf:"bytes,1,opt,name=contract_address,json=contractAddress,proto3" json:"contract_address,omitempty"`
}

type QueryContractStateRootResponse struct {
	StateRoot string `protobuf:"bytes,1,opt,name=state_root,json=stateRoot,proto3" json:"state_root,omitempty"`
}

func (m MsgStoreCode) ValidateBasic(params Params) error {
	if err := ValidateUserFacingAEAddress("store code authority", m.Authority); err != nil {
		return err
	}
	if len(m.Bytecode) > 0 {
		return ValidateAVMBytecode(params, m.Bytecode)
	}
	if m.CodeBytes == 0 || m.CodeBytes > params.MaxCodeBytes {
		return errors.New(ErrInvalidBytecode + ": code size out of bounds")
	}
	return validateHashText("store code hash", m.CodeHash)
}

func (m MsgDeployContract) ValidateBasic(params Params) error {
	if err := ValidateUserFacingAEAddress("deploy creator", m.Creator); err != nil {
		return err
	}
	if m.CodeID == "" {
		return errors.New("deploy code id is required")
	}
	if m.StateInit != nil {
		if err := m.StateInit.Validate(params); err != nil {
			return err
		}
	}
	if len(m.InitPayload) > MaxContractPayloadBytes {
		return errors.New("deploy payload exceeds maximum size")
	}
	if len(m.Metadata) > MaxContractMetadataBytes {
		return errors.New("deploy metadata exceeds maximum size")
	}
	if m.Admin != "" {
		if err := ValidateUserFacingAEAddress("deploy admin", m.Admin); err != nil {
			return err
		}
	}
	if m.Height == 0 {
		return errors.New("deploy height must be positive")
	}
	return nil
}

func (m MsgExecuteExternal) ValidateBasic(params Params) error {
	if err := ValidateUserFacingAEAddress("external execute sender", m.Sender); err != nil {
		return err
	}
	if err := ValidateContractAddress(m.ContractAddress); err != nil {
		return err
	}
	if m.StateInit != nil {
		if err := m.StateInit.Validate(params); err != nil {
			return err
		}
	}
	if len(m.Payload) > MaxContractPayloadBytes {
		return errors.New("external execute payload exceeds maximum size")
	}
	if len(m.Metadata) > MaxContractMetadataBytes {
		return errors.New("external execute metadata exceeds maximum size")
	}
	if m.GasLimit == 0 || m.GasLimit > params.MaxGasPerExecution {
		return errors.New("external execute gas limit out of bounds")
	}
	if m.Height == 0 {
		return errors.New("external execute height must be positive")
	}
	return nil
}

func (m MsgExecuteInternal) ValidateBasic(params Params) error {
	if m.Height == 0 {
		return errors.New("internal execute height must be positive")
	}
	msg := m.Message
	if msg.Height == 0 {
		msg.Height = m.Height
	}
	if err := msg.Validate(); err != nil {
		return err
	}
	// A zero gas limit means "use the module default"; any explicit limit must
	// stay within the per-execution ceiling so a permissionless internal
	// message cannot run the AVM effectively forever and halt the chain.
	// See SEC-CRIT: uncapped AVM gas on internal messages.
	if msg.GasLimit > params.MaxGasPerExecution {
		return errors.New("internal execute gas limit exceeds maximum")
	}
	return nil
}

func (m MsgSendInternalMessage) ValidateBasic(params Params) error {
	if m.Height == 0 {
		return errors.New("send internal height must be positive")
	}
	msg := m.Message
	if msg.Height == 0 {
		msg.Height = m.Height
	}
	if err := msg.Validate(); err != nil {
		return err
	}
	if msg.GasLimit > params.MaxGasPerExecution {
		return errors.New("send internal gas limit exceeds maximum")
	}
	return nil
}

func (m MsgUpgradeContractCode) ValidateBasic(params Params) error {
	if strings.TrimSpace(m.Actor) == "" {
		return errors.New("contract upgrade actor is required")
	}
	if err := ValidateContractAddress(m.ContractAddress); err != nil {
		return err
	}
	if strings.TrimSpace(m.NewCodeID) == "" {
		return errors.New("contract upgrade code id is required")
	}
	if len(m.MigrationHandler) > MaxContractMetadataBytes {
		return errors.New("contract migration handler exceeds maximum size")
	}
	if m.Height == 0 {
		return errors.New("contract upgrade height must be positive")
	}
	_ = params
	return nil
}

func (m MsgMigrateContractState) ValidateBasic(_ Params) error {
	if strings.TrimSpace(m.Actor) == "" {
		return errors.New("contract migration actor is required")
	}
	if err := ValidateContractAddress(m.ContractAddress); err != nil {
		return err
	}
	if m.FromSchemaVersion == 0 || m.ToSchemaVersion == 0 || m.ToSchemaVersion <= m.FromSchemaVersion {
		return errors.New("contract migration schema versions are invalid")
	}
	if strings.TrimSpace(m.MigrationHandler) == "" {
		return errors.New("contract migration handler is required")
	}
	if len(m.MigrationHandler) > MaxContractMetadataBytes {
		return errors.New("contract migration handler exceeds maximum size")
	}
	if len(m.Payload) > MaxContractPayloadBytes {
		return errors.New("contract migration payload exceeds maximum size")
	}
	if m.Height == 0 {
		return errors.New("contract migration height must be positive")
	}
	return nil
}

func (m MsgSetContractAdmin) ValidateBasic(_ Params) error {
	if strings.TrimSpace(m.Actor) == "" {
		return errors.New("contract admin actor is required")
	}
	if err := ValidateContractAddress(m.ContractAddress); err != nil {
		return err
	}
	if err := ValidateUserFacingAEAddress("new contract admin", m.NewAdmin); err != nil {
		return err
	}
	if m.Height == 0 {
		return errors.New("contract admin height must be positive")
	}
	return nil
}

func (m MsgDisableContractUpgrades) ValidateBasic(_ Params) error {
	if strings.TrimSpace(m.Actor) == "" {
		return errors.New("contract upgrade disable actor is required")
	}
	if err := ValidateContractAddress(m.ContractAddress); err != nil {
		return err
	}
	if m.Height == 0 {
		return errors.New("contract upgrade disable height must be positive")
	}
	return nil
}

func (m MsgUpdateContractParams) ValidateBasic() error {
	if err := m.Params.Authorize(m.Authority); err != nil {
		return err
	}
	return m.Params.Validate()
}

func ValidateQueryPagination(req PageRequest) error {
	if req.Limit == 0 || req.Limit > MaxContractQueryLimit {
		return fmt.Errorf("query limit must be within 1..%d", MaxContractQueryLimit)
	}
	return nil
}
