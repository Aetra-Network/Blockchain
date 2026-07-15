package types

import (
	"fmt"
	"math/bits"

	gogoproto "github.com/cosmos/gogoproto/proto"
)

// This file (together with query_marshal_size.go and
// query_marshal_unmarshal.go) hand-implements the gogoproto binary
// wire-format methods -- Marshal/MarshalTo/MarshalToSizedBuffer/Size/
// Unmarshal -- for the hand-written message structs in api.go, types.go,
// contract_state.go, and state_init.go that back the 7 x/contracts query
// RPCs already wired in queryclient.go and query.pb.gw.go: Params, Code,
// Codes, Contract, Contracts, ContractStorage, and ContractReceipts.
//
// Those structs already carry correct protobuf field-number tags (used
// below as the source of truth for field numbers -- nothing here invents a
// new one). The top-level Query*Request/Query*Response wrapper types also
// already implement Reset()/String()/ProtoMessage() (see service.go), but
// the leaf value types nested inside those responses -- Params, CodeRecord,
// PageRequest, CodeDependency, StateInit, Contract, ContractStorageEntry,
// and ContractReceipt -- never did; each is a plain data struct defined in
// its own file (types.go, api.go, state_init.go, contract_state.go) with no
// gogoproto marker methods at all. That was invisible from this file's own
// wire-format round-trip tests (Marshal/Unmarshal call each other directly,
// never through the proto.Message interface), and invisible to
// codec.ProtoCodec.GRPCCodec()'s binary path too (the parent
// Query*Response's own MarshalToSizedBuffer inlines each leaf field's bytes
// directly without going through the leaf's Marshal via an interface
// assertion). It only surfaces in clientCtx.PrintProto's JSON path (used by
// every `l1d query avm ...` CLI command), which walks the full nested
// object graph and requires every level -- leaf types included -- to
// satisfy proto.Message: without these methods, e.g. `query avm contract`
// fails with "types.Contract does not implement proto.Message" despite the
// underlying query succeeding. The three methods below are the same
// trivial, no-op-bodied triple already used for every other hand-written
// message in this package (see service.go): Reset zeroes the receiver,
// String defers to gogoproto's reflection-based text formatter, and
// ProtoMessage is the empty marker method the interface requires.
//
// cosmos-sdk's real production gRPC codec (codec.ProtoCodec.GRPCCodec(),
// forced via grpc.ForceServerCodec in server/grpc/server.go) does not
// tolerate it: it requires genuine Marshal/Unmarshal methods, and its
// protobuf-go-v2 legacy-compatibility shim does not fall back to reflection
// for types that are missing them. Without this file, any real
// out-of-process gRPC client -- e.g. the `l1d query avm code <id>` CLI,
// which talks to a node over the network via client.Context.Invoke -- fails
// with "rpc error: code = Internal desc = grpc: error while marshaling:
// proto: string does not implement Marshal".
//
// The code below mirrors the exact shape protoc-gen-gogo/protoc-gen-gocosmos
// emits -- see x/fees/types/query.pb.go or
// x/reputation/types/reputationpb/query.pb.go for real generated examples of
// the same conventions used elsewhere in this codebase: fields are written
// in descending field-number order into a buffer pre-sized by Size(),
// backpatching length prefixes for nested/repeated messages as it goes.
//
// The helper names below (sovContractsQuery/encodeVarintContractsQuery/
// skipContractsQuery/Err*ContractsQuery) follow the same per-proto-file
// namespacing convention protoc-gen-gogo uses (e.g. the *Query suffix in
// x/fees/types/query.pb.go, generated from l1/fees/v1/query.proto): they are
// namespaced for l1/contracts/v1/query.proto, so they cannot collide with a
// genuinely protoc-generated query.pb.go if one is ever introduced into this
// package.
func (m *Params) Reset()         { *m = Params{} }
func (m *Params) String() string { return gogoproto.CompactTextString(m) }
func (*Params) ProtoMessage()    {}

func (m *CodeRecord) Reset()         { *m = CodeRecord{} }
func (m *CodeRecord) String() string { return gogoproto.CompactTextString(m) }
func (*CodeRecord) ProtoMessage()    {}

func (m *PageRequest) Reset()         { *m = PageRequest{} }
func (m *PageRequest) String() string { return gogoproto.CompactTextString(m) }
func (*PageRequest) ProtoMessage()    {}

func (m *CodeDependency) Reset()         { *m = CodeDependency{} }
func (m *CodeDependency) String() string { return gogoproto.CompactTextString(m) }
func (*CodeDependency) ProtoMessage()    {}

func (m *StateInit) Reset()         { *m = StateInit{} }
func (m *StateInit) String() string { return gogoproto.CompactTextString(m) }
func (*StateInit) ProtoMessage()    {}

func (m *Contract) Reset()         { *m = Contract{} }
func (m *Contract) String() string { return gogoproto.CompactTextString(m) }
func (*Contract) ProtoMessage()    {}

func (m *ContractStorageEntry) Reset()         { *m = ContractStorageEntry{} }
func (m *ContractStorageEntry) String() string { return gogoproto.CompactTextString(m) }
func (*ContractStorageEntry) ProtoMessage()    {}

func (m *ContractReceipt) Reset()         { *m = ContractReceipt{} }
func (m *ContractReceipt) String() string { return gogoproto.CompactTextString(m) }
func (*ContractReceipt) ProtoMessage()    {}

var (
	ErrInvalidLengthContractsQuery        = fmt.Errorf("proto: negative length found during unmarshaling")
	ErrIntOverflowContractsQuery          = fmt.Errorf("proto: integer overflow")
	ErrUnexpectedEndOfGroupContractsQuery = fmt.Errorf("proto: unexpected end of group")
)

func encodeVarintContractsQuery(dAtA []byte, offset int, v uint64) int {
	offset -= sovContractsQuery(v)
	base := offset
	for v >= 1<<7 {
		dAtA[offset] = uint8(v&0x7f | 0x80)
		v >>= 7
		offset++
	}
	dAtA[offset] = uint8(v)
	return base
}

func sovContractsQuery(x uint64) (n int) {
	return (bits.Len64(x|1) + 6) / 7
}

// ---- QueryParamsRequest ----

func (m *QueryParamsRequest) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *QueryParamsRequest) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *QueryParamsRequest) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	return len(dAtA) - i, nil
}

// ---- QueryParamsResponse ----

func (m *QueryParamsResponse) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *QueryParamsResponse) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *QueryParamsResponse) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	{
		size, err := m.Params.MarshalToSizedBuffer(dAtA[:i])
		if err != nil {
			return 0, err
		}
		i -= size
		i = encodeVarintContractsQuery(dAtA, i, uint64(size))
	}
	i--
	dAtA[i] = 0xa
	return len(dAtA) - i, nil
}

// ---- Params ----

func (m *Params) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *Params) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *Params) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if m.MaxInternalMessageGasPerBlock != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.MaxInternalMessageGasPerBlock))
		i--
		dAtA[i] = 0x50
	}
	if m.MaxStateInitDependencies != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.MaxStateInitDependencies))
		i--
		dAtA[i] = 0x48
	}
	if m.MaxStateInitSaltBytes != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.MaxStateInitSaltBytes))
		i--
		dAtA[i] = 0x40
	}
	if m.MaxInitDataBytes != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.MaxInitDataBytes))
		i--
		dAtA[i] = 0x38
	}
	if m.StorageRentPerByteBlock != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.StorageRentPerByteBlock))
		i--
		dAtA[i] = 0x30
	}
	if m.MaxGasPerExecution != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.MaxGasPerExecution))
		i--
		dAtA[i] = 0x28
	}
	if m.MaxContractStorageBytes != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.MaxContractStorageBytes))
		i--
		dAtA[i] = 0x20
	}
	if m.MaxCodeBytes != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.MaxCodeBytes))
		i--
		dAtA[i] = 0x18
	}
	if m.Enabled {
		i--
		if m.Enabled {
			dAtA[i] = 1
		} else {
			dAtA[i] = 0
		}
		i--
		dAtA[i] = 0x10
	}
	if len(m.Authority) > 0 {
		i -= len(m.Authority)
		copy(dAtA[i:], m.Authority)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Authority)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

// ---- QueryCodeRequest ----

func (m *QueryCodeRequest) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *QueryCodeRequest) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *QueryCodeRequest) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if len(m.CodeID) > 0 {
		i -= len(m.CodeID)
		copy(dAtA[i:], m.CodeID)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.CodeID)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

// ---- QueryCodeResponse ----

func (m *QueryCodeResponse) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *QueryCodeResponse) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *QueryCodeResponse) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if m.Found {
		i--
		if m.Found {
			dAtA[i] = 1
		} else {
			dAtA[i] = 0
		}
		i--
		dAtA[i] = 0x10
	}
	{
		size, err := m.Code.MarshalToSizedBuffer(dAtA[:i])
		if err != nil {
			return 0, err
		}
		i -= size
		i = encodeVarintContractsQuery(dAtA, i, uint64(size))
	}
	i--
	dAtA[i] = 0xa
	return len(dAtA) - i, nil
}

// ---- CodeRecord ----

func (m *CodeRecord) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *CodeRecord) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *CodeRecord) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if len(m.Owner) > 0 {
		i -= len(m.Owner)
		copy(dAtA[i:], m.Owner)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Owner)))
		i--
		dAtA[i] = 0x2a
	}
	if len(m.Bytecode) > 0 {
		i -= len(m.Bytecode)
		copy(dAtA[i:], m.Bytecode)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Bytecode)))
		i--
		dAtA[i] = 0x22
	}
	if m.CodeBytes != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.CodeBytes))
		i--
		dAtA[i] = 0x18
	}
	if len(m.CodeHash) > 0 {
		i -= len(m.CodeHash)
		copy(dAtA[i:], m.CodeHash)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.CodeHash)))
		i--
		dAtA[i] = 0x12
	}
	if len(m.CodeID) > 0 {
		i -= len(m.CodeID)
		copy(dAtA[i:], m.CodeID)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.CodeID)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

// ---- QueryCodesRequest ----

func (m *QueryCodesRequest) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *QueryCodesRequest) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *QueryCodesRequest) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	{
		size, err := m.Pagination.MarshalToSizedBuffer(dAtA[:i])
		if err != nil {
			return 0, err
		}
		i -= size
		i = encodeVarintContractsQuery(dAtA, i, uint64(size))
	}
	i--
	dAtA[i] = 0xa
	return len(dAtA) - i, nil
}

// ---- QueryCodesResponse ----

func (m *QueryCodesResponse) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *QueryCodesResponse) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *QueryCodesResponse) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if len(m.Codes) > 0 {
		for iNdEx := len(m.Codes) - 1; iNdEx >= 0; iNdEx-- {
			{
				size, err := m.Codes[iNdEx].MarshalToSizedBuffer(dAtA[:i])
				if err != nil {
					return 0, err
				}
				i -= size
				i = encodeVarintContractsQuery(dAtA, i, uint64(size))
			}
			i--
			dAtA[i] = 0xa
		}
	}
	return len(dAtA) - i, nil
}

// ---- PageRequest ----

func (m *PageRequest) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *PageRequest) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *PageRequest) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if m.Limit != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.Limit))
		i--
		dAtA[i] = 0x8
	}
	return len(dAtA) - i, nil
}

// ---- QueryContractRequest ----

func (m *QueryContractRequest) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *QueryContractRequest) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *QueryContractRequest) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if m.StateInit != nil {
		{
			size, err := m.StateInit.MarshalToSizedBuffer(dAtA[:i])
			if err != nil {
				return 0, err
			}
			i -= size
			i = encodeVarintContractsQuery(dAtA, i, uint64(size))
		}
		i--
		dAtA[i] = 0x2a
	}
	if len(m.Deployer) > 0 {
		i -= len(m.Deployer)
		copy(dAtA[i:], m.Deployer)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Deployer)))
		i--
		dAtA[i] = 0x22
	}
	if len(m.Namespace) > 0 {
		i -= len(m.Namespace)
		copy(dAtA[i:], m.Namespace)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Namespace)))
		i--
		dAtA[i] = 0x1a
	}
	if len(m.ChainID) > 0 {
		i -= len(m.ChainID)
		copy(dAtA[i:], m.ChainID)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.ChainID)))
		i--
		dAtA[i] = 0x12
	}
	if len(m.ContractAddress) > 0 {
		i -= len(m.ContractAddress)
		copy(dAtA[i:], m.ContractAddress)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.ContractAddress)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

// ---- CodeDependency ----

func (m *CodeDependency) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *CodeDependency) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *CodeDependency) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if len(m.CodeHash) > 0 {
		i -= len(m.CodeHash)
		copy(dAtA[i:], m.CodeHash)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.CodeHash)))
		i--
		dAtA[i] = 0x12
	}
	if len(m.CodeID) > 0 {
		i -= len(m.CodeID)
		copy(dAtA[i:], m.CodeID)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.CodeID)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

// ---- StateInit ----

func (m *StateInit) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *StateInit) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *StateInit) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if len(m.Capabilities) > 0 {
		for iNdEx := len(m.Capabilities) - 1; iNdEx >= 0; iNdEx-- {
			i -= len(m.Capabilities[iNdEx])
			copy(dAtA[i:], m.Capabilities[iNdEx])
			i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Capabilities[iNdEx])))
			i--
			dAtA[i] = 0x5a
		}
	}
	if m.InitialBalanceNAET != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.InitialBalanceNAET))
		i--
		dAtA[i] = 0x50
	}
	if len(m.InitialStorageRoot) > 0 {
		i -= len(m.InitialStorageRoot)
		copy(dAtA[i:], m.InitialStorageRoot)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.InitialStorageRoot)))
		i--
		dAtA[i] = 0x4a
	}
	if len(m.Libraries) > 0 {
		for iNdEx := len(m.Libraries) - 1; iNdEx >= 0; iNdEx-- {
			{
				size, err := m.Libraries[iNdEx].MarshalToSizedBuffer(dAtA[:i])
				if err != nil {
					return 0, err
				}
				i -= size
				i = encodeVarintContractsQuery(dAtA, i, uint64(size))
			}
			i--
			dAtA[i] = 0x42
		}
	}
	if len(m.Owner) > 0 {
		i -= len(m.Owner)
		copy(dAtA[i:], m.Owner)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Owner)))
		i--
		dAtA[i] = 0x3a
	}
	if len(m.SaltBytes) > 0 {
		i -= len(m.SaltBytes)
		copy(dAtA[i:], m.SaltBytes)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.SaltBytes)))
		i--
		dAtA[i] = 0x32
	}
	if len(m.Salt) > 0 {
		i -= len(m.Salt)
		copy(dAtA[i:], m.Salt)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Salt)))
		i--
		dAtA[i] = 0x2a
	}
	if len(m.InitData) > 0 {
		i -= len(m.InitData)
		copy(dAtA[i:], m.InitData)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.InitData)))
		i--
		dAtA[i] = 0x22
	}
	if len(m.CodeHash) > 0 {
		i -= len(m.CodeHash)
		copy(dAtA[i:], m.CodeHash)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.CodeHash)))
		i--
		dAtA[i] = 0x1a
	}
	if len(m.CodeID) > 0 {
		i -= len(m.CodeID)
		copy(dAtA[i:], m.CodeID)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.CodeID)))
		i--
		dAtA[i] = 0x12
	}
	if m.ABIVersion != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.ABIVersion))
		i--
		dAtA[i] = 0x8
	}
	return len(dAtA) - i, nil
}

// ---- Contract ----

func (m *Contract) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *Contract) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *Contract) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if m.UpdatedHeight != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.UpdatedHeight))
		i--
		dAtA[i] = 0x1
		i--
		dAtA[i] = 0xc0
	}
	if m.CreatedHeight != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.CreatedHeight))
		i--
		dAtA[i] = 0x1
		i--
		dAtA[i] = 0xb8
	}
	if m.LogicalTime != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.LogicalTime))
		i--
		dAtA[i] = 0x1
		i--
		dAtA[i] = 0xb0
	}
	if m.StorageRentDebt != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.StorageRentDebt))
		i--
		dAtA[i] = 0x1
		i--
		dAtA[i] = 0xa8
	}
	if m.LastStorageChargeHeight != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.LastStorageChargeHeight))
		i--
		dAtA[i] = 0x1
		i--
		dAtA[i] = 0xa0
	}
	if m.StorageBytes != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.StorageBytes))
		i--
		dAtA[i] = 0x1
		i--
		dAtA[i] = 0x98
	}
	if len(m.Status) > 0 {
		i -= len(m.Status)
		copy(dAtA[i:], m.Status)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Status)))
		i--
		dAtA[i] = 0x1
		i--
		dAtA[i] = 0x92
	}
	if len(m.StateRoot) > 0 {
		i -= len(m.StateRoot)
		copy(dAtA[i:], m.StateRoot)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.StateRoot)))
		i--
		dAtA[i] = 0x1
		i--
		dAtA[i] = 0x8a
	}
	if m.Balance != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.Balance))
		i--
		dAtA[i] = 0x1
		i--
		dAtA[i] = 0x80
	}
	if len(m.Data) > 0 {
		i -= len(m.Data)
		copy(dAtA[i:], m.Data)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Data)))
		i--
		dAtA[i] = 0x7a
	}
	if len(m.InitMsg) > 0 {
		i -= len(m.InitMsg)
		copy(dAtA[i:], m.InitMsg)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.InitMsg)))
		i--
		dAtA[i] = 0x72
	}
	if m.StorageSchemaVersion != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.StorageSchemaVersion))
		i--
		dAtA[i] = 0x68
	}
	if m.SystemOwned {
		i--
		if m.SystemOwned {
			dAtA[i] = 1
		} else {
			dAtA[i] = 0
		}
		i--
		dAtA[i] = 0x60
	}
	if m.UpgradesDisabled {
		i--
		if m.UpgradesDisabled {
			dAtA[i] = 1
		} else {
			dAtA[i] = 0
		}
		i--
		dAtA[i] = 0x58
	}
	if m.Upgradeable {
		i--
		if m.Upgradeable {
			dAtA[i] = 1
		} else {
			dAtA[i] = 0
		}
		i--
		dAtA[i] = 0x50
	}
	if len(m.Admin) > 0 {
		i -= len(m.Admin)
		copy(dAtA[i:], m.Admin)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Admin)))
		i--
		dAtA[i] = 0x4a
	}
	if len(m.Owner) > 0 {
		i -= len(m.Owner)
		copy(dAtA[i:], m.Owner)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Owner)))
		i--
		dAtA[i] = 0x42
	}
	if len(m.Creator) > 0 {
		i -= len(m.Creator)
		copy(dAtA[i:], m.Creator)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Creator)))
		i--
		dAtA[i] = 0x3a
	}
	{
		size, err := m.StateInit.MarshalToSizedBuffer(dAtA[:i])
		if err != nil {
			return 0, err
		}
		i -= size
		i = encodeVarintContractsQuery(dAtA, i, uint64(size))
	}
	i--
	dAtA[i] = 0x32
	if len(m.StateInitHash) > 0 {
		i -= len(m.StateInitHash)
		copy(dAtA[i:], m.StateInitHash)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.StateInitHash)))
		i--
		dAtA[i] = 0x2a
	}
	if len(m.CodeHash) > 0 {
		i -= len(m.CodeHash)
		copy(dAtA[i:], m.CodeHash)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.CodeHash)))
		i--
		dAtA[i] = 0x22
	}
	if len(m.CodeID) > 0 {
		i -= len(m.CodeID)
		copy(dAtA[i:], m.CodeID)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.CodeID)))
		i--
		dAtA[i] = 0x1a
	}
	if len(m.AddressRaw) > 0 {
		i -= len(m.AddressRaw)
		copy(dAtA[i:], m.AddressRaw)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.AddressRaw)))
		i--
		dAtA[i] = 0x12
	}
	if len(m.AddressUser) > 0 {
		i -= len(m.AddressUser)
		copy(dAtA[i:], m.AddressUser)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.AddressUser)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

// ---- QueryContractResponse ----

func (m *QueryContractResponse) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *QueryContractResponse) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *QueryContractResponse) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if len(m.Status) > 0 {
		i -= len(m.Status)
		copy(dAtA[i:], m.Status)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Status)))
		i--
		dAtA[i] = 0x32
	}
	{
		size, err := m.Contract.MarshalToSizedBuffer(dAtA[:i])
		if err != nil {
			return 0, err
		}
		i -= size
		i = encodeVarintContractsQuery(dAtA, i, uint64(size))
	}
	i--
	dAtA[i] = 0x2a
	if m.Virtual {
		i--
		if m.Virtual {
			dAtA[i] = 1
		} else {
			dAtA[i] = 0
		}
		i--
		dAtA[i] = 0x20
	}
	if m.Found {
		i--
		if m.Found {
			dAtA[i] = 1
		} else {
			dAtA[i] = 0
		}
		i--
		dAtA[i] = 0x18
	}
	if len(m.StateRoot) > 0 {
		i -= len(m.StateRoot)
		copy(dAtA[i:], m.StateRoot)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.StateRoot)))
		i--
		dAtA[i] = 0x12
	}
	if len(m.ContractAddress) > 0 {
		i -= len(m.ContractAddress)
		copy(dAtA[i:], m.ContractAddress)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.ContractAddress)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

// ---- QueryContractsRequest ----

func (m *QueryContractsRequest) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *QueryContractsRequest) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *QueryContractsRequest) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	{
		size, err := m.Pagination.MarshalToSizedBuffer(dAtA[:i])
		if err != nil {
			return 0, err
		}
		i -= size
		i = encodeVarintContractsQuery(dAtA, i, uint64(size))
	}
	i--
	dAtA[i] = 0xa
	return len(dAtA) - i, nil
}

// ---- QueryContractsResponse ----

func (m *QueryContractsResponse) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *QueryContractsResponse) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *QueryContractsResponse) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if len(m.Contracts) > 0 {
		for iNdEx := len(m.Contracts) - 1; iNdEx >= 0; iNdEx-- {
			{
				size, err := m.Contracts[iNdEx].MarshalToSizedBuffer(dAtA[:i])
				if err != nil {
					return 0, err
				}
				i -= size
				i = encodeVarintContractsQuery(dAtA, i, uint64(size))
			}
			i--
			dAtA[i] = 0xa
		}
	}
	return len(dAtA) - i, nil
}

// ---- QueryContractStorageRequest ----

func (m *QueryContractStorageRequest) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *QueryContractStorageRequest) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *QueryContractStorageRequest) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	{
		size, err := m.Pagination.MarshalToSizedBuffer(dAtA[:i])
		if err != nil {
			return 0, err
		}
		i -= size
		i = encodeVarintContractsQuery(dAtA, i, uint64(size))
	}
	i--
	dAtA[i] = 0x1a
	if len(m.KeyPrefix) > 0 {
		i -= len(m.KeyPrefix)
		copy(dAtA[i:], m.KeyPrefix)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.KeyPrefix)))
		i--
		dAtA[i] = 0x12
	}
	if len(m.ContractAddress) > 0 {
		i -= len(m.ContractAddress)
		copy(dAtA[i:], m.ContractAddress)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.ContractAddress)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

// ---- ContractStorageEntry ----

func (m *ContractStorageEntry) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *ContractStorageEntry) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *ContractStorageEntry) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if len(m.Value) > 0 {
		i -= len(m.Value)
		copy(dAtA[i:], m.Value)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Value)))
		i--
		dAtA[i] = 0x1a
	}
	if len(m.Key) > 0 {
		i -= len(m.Key)
		copy(dAtA[i:], m.Key)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Key)))
		i--
		dAtA[i] = 0x12
	}
	if len(m.ContractAddress) > 0 {
		i -= len(m.ContractAddress)
		copy(dAtA[i:], m.ContractAddress)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.ContractAddress)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

// ---- QueryContractStorageResponse ----

func (m *QueryContractStorageResponse) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *QueryContractStorageResponse) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *QueryContractStorageResponse) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if len(m.Entries) > 0 {
		for iNdEx := len(m.Entries) - 1; iNdEx >= 0; iNdEx-- {
			{
				size, err := m.Entries[iNdEx].MarshalToSizedBuffer(dAtA[:i])
				if err != nil {
					return 0, err
				}
				i -= size
				i = encodeVarintContractsQuery(dAtA, i, uint64(size))
			}
			i--
			dAtA[i] = 0xa
		}
	}
	return len(dAtA) - i, nil
}

// ---- QueryContractReceiptsRequest ----

func (m *QueryContractReceiptsRequest) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *QueryContractReceiptsRequest) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *QueryContractReceiptsRequest) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	{
		size, err := m.Pagination.MarshalToSizedBuffer(dAtA[:i])
		if err != nil {
			return 0, err
		}
		i -= size
		i = encodeVarintContractsQuery(dAtA, i, uint64(size))
	}
	i--
	dAtA[i] = 0x12
	if len(m.ContractAddress) > 0 {
		i -= len(m.ContractAddress)
		copy(dAtA[i:], m.ContractAddress)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.ContractAddress)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

// ---- ContractReceipt ----

func (m *ContractReceipt) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *ContractReceipt) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *ContractReceipt) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if m.Height != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.Height))
		i--
		dAtA[i] = 0x48
	}
	if m.LogicalTime != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.LogicalTime))
		i--
		dAtA[i] = 0x40
	}
	if m.GasUsed != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.GasUsed))
		i--
		dAtA[i] = 0x38
	}
	if m.Amount != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.Amount))
		i--
		dAtA[i] = 0x30
	}
	if m.ExitCode != 0 {
		i = encodeVarintContractsQuery(dAtA, i, uint64(m.ExitCode))
		i--
		dAtA[i] = 0x28
	}
	if len(m.Operation) > 0 {
		i -= len(m.Operation)
		copy(dAtA[i:], m.Operation)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Operation)))
		i--
		dAtA[i] = 0x22
	}
	if len(m.Actor) > 0 {
		i -= len(m.Actor)
		copy(dAtA[i:], m.Actor)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.Actor)))
		i--
		dAtA[i] = 0x1a
	}
	if len(m.ContractAddress) > 0 {
		i -= len(m.ContractAddress)
		copy(dAtA[i:], m.ContractAddress)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.ContractAddress)))
		i--
		dAtA[i] = 0x12
	}
	if len(m.ReceiptID) > 0 {
		i -= len(m.ReceiptID)
		copy(dAtA[i:], m.ReceiptID)
		i = encodeVarintContractsQuery(dAtA, i, uint64(len(m.ReceiptID)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

// ---- QueryContractReceiptsResponse ----

func (m *QueryContractReceiptsResponse) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *QueryContractReceiptsResponse) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *QueryContractReceiptsResponse) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if len(m.Receipts) > 0 {
		for iNdEx := len(m.Receipts) - 1; iNdEx >= 0; iNdEx-- {
			{
				size, err := m.Receipts[iNdEx].MarshalToSizedBuffer(dAtA[:i])
				if err != nil {
					return 0, err
				}
				i -= size
				i = encodeVarintContractsQuery(dAtA, i, uint64(size))
			}
			i--
			dAtA[i] = 0xa
		}
	}
	return len(dAtA) - i, nil
}
