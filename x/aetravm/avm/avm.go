package avm

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strconv"
	"strings"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/app/addressing"
	appparams "github.com/sovereign-l1/l1/app/params"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/chunk"
	contracttypes "github.com/sovereign-l1/l1/x/contracts/types"
)

const (
	Magic          = "AVM1"
	Version uint16 = 1

	MetadataHashLength = 32
	MaxKeySize         = 128

	EntryDeploy          Entrypoint = 1
	EntryReceiveExternal Entrypoint = 2
	EntryReceiveInternal Entrypoint = 3
	EntryReceiveBounced  Entrypoint = 4
	EntryQuery           Entrypoint = 5
	EntryMigrate         Entrypoint = 6

	HostReadStorage  HostFunction = 1
	HostWriteStorage HostFunction = 2
	HostEmitInternal HostFunction = 3
	HostInspectMsg   HostFunction = 4
	HostBlockContext HostFunction = 5
	HostChargeGas    HostFunction = 6
	HostReturn       HostFunction = 7
	HostScheduleSelf HostFunction = 8

	OpNop                         Opcode = 0x00
	OpPushU64                     Opcode = 0x01
	OpReadStorage                 Opcode = 0x02
	OpWriteStorage                Opcode = 0x03
	OpDeleteStorage               Opcode = 0x42
	OpAdd                         Opcode = 0x04
	OpSub                         Opcode = 0x05
	OpEmitInternal                Opcode = 0x06
	OpReturn                      Opcode = 0x07
	OpReadMsgOpcode               Opcode = 0x08
	OpReadMsgQueryID              Opcode = 0x09
	OpReadBlock                   Opcode = 0x0a
	OpChargeGas                   Opcode = 0x0b
	OpScheduleSelf                Opcode = 0x0c
	OpEq                          Opcode = 0x0d
	OpNe                          Opcode = 0x0e
	OpLt                          Opcode = 0x0f
	OpLe                          Opcode = 0x10
	OpGt                          Opcode = 0x11
	OpGe                          Opcode = 0x12
	OpCmp                         Opcode = 0x43
	OpAnd                         Opcode = 0x13
	OpOr                          Opcode = 0x14
	OpNot                         Opcode = 0x15
	OpJump                        Opcode = 0x16
	OpJumpIfZero                  Opcode = 0x17
	OpAbort                       Opcode = 0x18
	OpDup                         Opcode = 0x19
	OpDrop                        Opcode = 0x1a
	OpReadMsgSender               Opcode = 0x1b
	OpReadMsgValue                Opcode = 0x1c
	OpReadMsgBody                 Opcode = 0x1d
	OpIsEmpty                     Opcode = 0x1e
	OpReadMsgField                Opcode = 0x1f
	OpMul                         Opcode = 0x20
	OpDiv                         Opcode = 0x21
	OpMod                         Opcode = 0x22
	OpShl                         Opcode = 0x23
	OpShr                         Opcode = 0x24
	OpBitAnd                      Opcode = 0x25
	OpBitOr                       Opcode = 0x26
	OpBitXor                      Opcode = 0x27
	OpNeg                         Opcode = 0x28
	OpBitNot                      Opcode = 0x29
	OpPushNull                    Opcode = 0x2a
	OpLoadLocal                   Opcode = 0x2b
	OpStoreLocal                  Opcode = 0x2c
	OpReadField                   Opcode = 0x2d
	OpLen                         Opcode = 0x2e
	OpMapEmpty                    Opcode = 0x2f
	OpMapGet                      Opcode = 0x30
	OpMapSet                      Opcode = 0x31
	OpMapHas                      Opcode = 0x32
	OpMapDelete                   Opcode = 0x33
	OpMapKeys                     Opcode = 0x34
	OpMapEntries                  Opcode = 0x35
	OpPushString                  Opcode = 0x36
	OpPushAddress                 Opcode = 0x44
	OpPushBytes                   Opcode = 0x37
	OpHash                        Opcode = 0x38
	OpReadContractAddress         Opcode = 0x39
	OpReadOriginalBalance         Opcode = 0x3a
	OpReadAttachedValue           Opcode = 0x3b
	OpReadLogicalTime             Opcode = 0x3c
	OpReadBlockTimestamp          Opcode = 0x3d
	OpReadCurrentBlockLogicalTime Opcode = 0x3e
	OpCounterfactualAddress       Opcode = 0x3f
	OpAutoDeployAddress           Opcode = 0x40
	OpVerifySignature             Opcode = 0x41
	// OpCastCoins retags the top-of-stack integer as TagCoins, preserving its
	// numeric value. Needed because a bare numeric literal always lowers to
	// TagUint64 (OpPushU64); a "coins"-typed struct field initialized with a
	// literal (e.g. `balance: 0`) must still canonically-encode as TagCoins,
	// or its encoding won't match an equivalent value sourced from a coins
	// field elsewhere (message decode, storage read) — which breaks anything
	// hashing the struct, such as counterfactualAddress/autoDeployAddress.
	OpCastCoins Opcode = 0x45
	// OpReadRandom pushes a deterministic uint64 from the block randomness
	// beacon (SHA256 over previous state root, block hash, message hash, and a
	// per-call domain). It is the language-level random() source and is fully
	// deterministic across validators — distinct from the forbidden 0xf1
	// OpRandom, which drew from process entropy.
	OpReadRandom Opcode = 0x46

	OpWallClock Opcode = 0xf0
	OpRandom    Opcode = 0xf1
	OpFileRead  Opcode = 0xf2
	OpFloatAdd  Opcode = 0xf3
	OpIterMap   Opcode = 0xf4
)

type Entrypoint uint8
type HostFunction uint16
type Opcode uint8

type Params struct {
	MaxCodeBytes    uint32
	MaxInstructions uint32
	MaxImports      uint16
	MaxStackDepth   uint32
	MaxMemoryBytes  uint32
	GasSchedule     map[Opcode]uint64
}

type Module struct {
	Version      uint16
	Imports      []HostFunction
	Exports      map[Entrypoint]uint32
	MetadataHash [MetadataHashLength]byte
	Code         []Instruction
}

type Instruction struct {
	Op   Opcode
	Arg  uint64
	Data []byte
}

type RuntimeContext struct {
	Entry                   Entrypoint
	ContractAddress         sdk.AccAddress
	Message                 async.MessageEnvelope
	BlockHeight             uint64
	BlockTimestamp          uint64
	LogicalTime             uint64
	CurrentBlockLogicalTime uint64
	OriginalBalance         sdkmath.Int
	AttachedValue           sdkmath.Int
	GasLimit                uint64
	EmitDestination         sdk.AccAddress
	// PrevStateRoot and BlockEntropy feed the deterministic randomness beacon
	// (OpReadRandom). PrevStateRoot is the previous block's committed app hash;
	// BlockEntropy is the current block hash. Both are consensus state, so all
	// validators derive identical random() results. Empty inputs are valid and
	// stay deterministic (lower entropy) for unit tests and off-chain tooling.
	PrevStateRoot []byte
	BlockEntropy  []byte
}

type Execution struct {
	State          Storage
	Outgoing       []async.MessageEnvelope
	GasUsed        uint64
	ResultCode     uint32
	StorageWrites  uint32
	ReturnValue    RuntimeValue
	ExecutedOpcode []Opcode
}

type Storage map[string][]byte

type SnapshotEntry struct {
	Key   string
	Value []byte
}

type ExecutionProof struct {
	ModuleHash    [32]byte
	BeforeRoot    [32]byte
	AfterRoot     [32]byte
	ContextHash   [32]byte
	OutgoingRoot  [32]byte
	TraceHash     [32]byte
	GasUsed       uint64
	ResultCode    uint32
	StorageWrites uint32
	ReturnValue   uint64
}

type Verifier struct {
	params Params
}

type Runner struct {
	params Params
}

// DefaultMaxStackDepth is the canonical AVM stack-depth limit shared by the
// interpreter params and the standalone determinism gates.
const DefaultMaxStackDepth = 1024

// MaxRuntimeGasLimit is the absolute ceiling on the gas budget any single AVM
// execution may be granted, enforced by the interpreter itself independent of
// the caller-supplied gas limit. It bounds both compute time and executed
// instructions. Callers (e.g. x/contracts) impose their own, lower
// per-execution gas caps below this value.
const MaxRuntimeGasLimit = 1_000_000_000

func DefaultParams() Params {
	return Params{
		MaxCodeBytes:    64 * 1024,
		MaxInstructions: 4096,
		MaxImports:      32,
		MaxStackDepth:   DefaultMaxStackDepth,
		MaxMemoryBytes:  1024 * 1024,
		GasSchedule: map[Opcode]uint64{
			OpNop:                         1,
			OpPushU64:                     2,
			OpPushNull:                    1,
			OpReadStorage:                 20,
			OpWriteStorage:                50,
			OpDeleteStorage:               30,
			OpAdd:                         3,
			OpSub:                         3,
			OpMul:                         4,
			OpDiv:                         5,
			OpMod:                         5,
			OpShl:                         3,
			OpShr:                         3,
			OpBitAnd:                      2,
			OpBitOr:                       2,
			OpBitXor:                      2,
			OpNeg:                         2,
			OpBitNot:                      2,
			OpEmitInternal:                100,
			OpReturn:                      1,
			OpReadMsgOpcode:               5,
			OpReadMsgQueryID:              5,
			OpReadBlock:                   5,
			OpChargeGas:                   1,
			OpScheduleSelf:                100,
			OpEq:                          3,
			OpNe:                          3,
			OpLt:                          3,
			OpLe:                          3,
			OpGt:                          3,
			OpGe:                          3,
			OpCmp:                         3,
			OpAnd:                         2,
			OpOr:                          2,
			OpNot:                         2,
			OpJump:                        1,
			OpJumpIfZero:                  2,
			OpAbort:                       1,
			OpDup:                         1,
			OpDrop:                        1,
			OpLoadLocal:                   1,
			OpStoreLocal:                  1,
			OpReadField:                   10,
			OpLen:                         2,
			OpMapEmpty:                    1,
			OpMapGet:                      12,
			OpMapSet:                      16,
			OpMapHas:                      10,
			OpMapDelete:                   12,
			OpMapKeys:                     16,
			OpMapEntries:                  18,
			OpPushString:                  2,
			OpPushAddress:                 2,
			OpPushBytes:                   2,
			OpHash:                        25,
			OpReadContractAddress:         5,
			OpReadOriginalBalance:         5,
			OpReadAttachedValue:           5,
			OpReadLogicalTime:             5,
			OpReadBlockTimestamp:          5,
			OpReadCurrentBlockLogicalTime: 5,
			OpCounterfactualAddress:       40,
			OpAutoDeployAddress:           40,
			OpVerifySignature:             5_000,
			OpReadMsgSender:               5,
			OpReadMsgValue:                5,
			OpReadMsgBody:                 5,
			OpIsEmpty:                     2,
			OpReadMsgField:                10,
			OpCastCoins:                   2,
			OpReadRandom:                  25,
		},
	}
}

func NewVerifier(params Params) (*Verifier, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}
	return &Verifier{params: params}, nil
}

func NewRunner(params Params) (*Runner, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}
	return &Runner{params: params}, nil
}

func (p Params) Validate() error {
	if p.MaxCodeBytes == 0 {
		return errors.New("max code bytes must be positive")
	}
	if p.MaxInstructions == 0 {
		return errors.New("max instructions must be positive")
	}
	if p.MaxImports == 0 {
		return errors.New("max imports must be positive")
	}
	if p.MaxStackDepth == 0 {
		return errors.New("max stack depth must be positive")
	}
	if p.MaxMemoryBytes == 0 {
		return errors.New("max memory bytes must be positive")
	}
	for _, op := range []Opcode{
		OpNop,
		OpPushU64,
		OpPushNull,
		OpReadStorage,
		OpWriteStorage,
		OpDeleteStorage,
		OpAdd,
		OpSub,
		OpMul,
		OpDiv,
		OpMod,
		OpShl,
		OpShr,
		OpBitAnd,
		OpBitOr,
		OpBitXor,
		OpNeg,
		OpBitNot,
		OpEmitInternal,
		OpReturn,
		OpReadMsgOpcode,
		OpReadMsgQueryID,
		OpReadBlock,
		OpChargeGas,
		OpScheduleSelf,
		OpEq,
		OpNe,
		OpLt,
		OpLe,
		OpGt,
		OpGe,
		OpCmp,
		OpAnd,
		OpOr,
		OpNot,
		OpJump,
		OpJumpIfZero,
		OpAbort,
		OpDup,
		OpDrop,
		OpLoadLocal,
		OpStoreLocal,
		OpReadField,
		OpLen,
		OpMapEmpty,
		OpMapGet,
		OpMapSet,
		OpMapHas,
		OpMapDelete,
		OpMapKeys,
		OpMapEntries,
		OpPushString,
		OpPushAddress,
		OpPushBytes,
		OpHash,
		OpReadContractAddress,
		OpReadOriginalBalance,
		OpReadAttachedValue,
		OpReadLogicalTime,
		OpReadBlockTimestamp,
		OpReadCurrentBlockLogicalTime,
		OpCounterfactualAddress,
		OpAutoDeployAddress,
		OpVerifySignature,
		OpReadMsgSender,
		OpReadMsgValue,
		OpReadMsgBody,
		OpIsEmpty,
		OpReadRandom,
	} {
		if p.GasSchedule[op] == 0 {
			return fmt.Errorf("gas schedule missing opcode 0x%02x", byte(op))
		}
	}
	return nil
}

func (v *Verifier) Verify(module Module) error {
	if module.Version != Version {
		return fmt.Errorf("unsupported AVM version %d", module.Version)
	}
	if len(module.Code) == 0 {
		return errors.New("AVM module code must not be empty")
	}
	if len(module.Code) > int(v.params.MaxInstructions) {
		return fmt.Errorf("AVM instruction count must be <= %d", v.params.MaxInstructions)
	}
	encoded, err := EncodeModule(module)
	if err != nil {
		return err
	}
	if len(encoded) > int(v.params.MaxCodeBytes) {
		return fmt.Errorf("AVM code bytes must be <= %d", v.params.MaxCodeBytes)
	}
	if len(module.Imports) > int(v.params.MaxImports) {
		return fmt.Errorf("AVM import count must be <= %d", v.params.MaxImports)
	}
	if len(module.Exports) == 0 {
		return errors.New("AVM module must export at least one entrypoint")
	}
	seenImports := make(map[HostFunction]struct{}, len(module.Imports))
	for _, host := range module.Imports {
		if !IsAllowedHostFunction(host) {
			return fmt.Errorf("AVM host function %d is not allowed", host)
		}
		if _, found := seenImports[host]; found {
			return fmt.Errorf("AVM module import %d is duplicated", host)
		}
		seenImports[host] = struct{}{}
	}
	imports := hostImportSet(module.Imports)
	for entry, offset := range module.Exports {
		if !IsValidEntrypoint(entry) {
			return fmt.Errorf("AVM entrypoint %d is invalid", entry)
		}
		if offset >= uint32(len(module.Code)) {
			return fmt.Errorf("AVM entrypoint %d offset out of range", entry)
		}
	}
	for _, ins := range module.Code {
		if IsForbiddenOpcode(ins.Op) {
			return fmt.Errorf("AVM opcode 0x%02x is nondeterministic or forbidden", byte(ins.Op))
		}
		if !IsAllowedOpcode(ins.Op) {
			return fmt.Errorf("AVM opcode 0x%02x is unknown", byte(ins.Op))
		}
		if required, ok := RequiredHostFunction(ins.Op); ok {
			if _, imported := imports[required]; !imported {
				return fmt.Errorf("AVM opcode 0x%02x requires host function %d", byte(ins.Op), required)
			}
		}
		if len(ins.Data) > MaxKeySize {
			return fmt.Errorf("AVM instruction data must be <= %d bytes", MaxKeySize)
		}
		if err := validateInstructionArg(ins); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) Run(module Module, storage Storage, ctx RuntimeContext) (Execution, error) {
	verifier, err := NewVerifier(r.params)
	if err != nil {
		return Execution{}, err
	}
	if err := verifier.Verify(module); err != nil {
		return Execution{}, err
	}
	if err := ValidateRuntimeContext(ctx); err != nil {
		return Execution{}, err
	}
	pc, ok := module.Exports[ctx.Entry]
	if !ok {
		return Execution{}, fmt.Errorf("AVM entrypoint %d is not exported", ctx.Entry)
	}
	originalState := CloneStorage(storage)
	state := CloneStorage(storage)
	stack := make([]RuntimeValue, 0)
	locals := make([]RuntimeValue, 0)
	// randomNonce domain-separates successive OpReadRandom reads within this
	// execution so each random() call yields an independent, deterministic value.
	var randomNonce uint64
	exec := Execution{State: state}
	readOnly := IsReadOnlyEntrypoint(ctx.Entry)
	gasLimit := ctx.GasLimit
	if gasLimit == 0 {
		gasLimit = ctx.Message.GasLimit
	}
	if gasLimit == 0 {
		return Execution{}, errors.New("AVM gas limit must be positive")
	}
	// Absolute, caller-independent ceiling on the gas budget. Because every
	// executable opcode costs at least 1 gas (enforced by Params.Validate and
	// the gas schedule), this also hard-bounds the number of executed
	// instructions regardless of the caller-supplied gas limit, so a hostile
	// contract cannot pin the interpreter in an effectively unbounded loop.
	// See SEC-CRIT: uncapped AVM gas on internal messages.
	if gasLimit > MaxRuntimeGasLimit {
		return Execution{}, fmt.Errorf("AVM gas limit %d exceeds maximum %d", gasLimit, MaxRuntimeGasLimit)
	}
	rollback := func(resultCode uint32, runErr error) (Execution, error) {
		exec.ResultCode = resultCode
		exec.State = originalState
		exec.Outgoing = nil
		return exec, runErr
	}

	for int(pc) < len(module.Code) {
		ins := module.Code[pc]
		gas := r.params.GasSchedule[ins.Op]
		nextGas, overflow := safeAddU64(exec.GasUsed, gas)
		if overflow {
			return rollback(async.ResultLimitExceeded, nil)
		}
		exec.GasUsed = nextGas
		if exec.GasUsed > gasLimit {
			return rollback(async.ResultLimitExceeded, nil)
		}
		exec.ExecutedOpcode = append(exec.ExecutedOpcode, ins.Op)
		switch ins.Op {
		case OpNop:
		case OpPushU64:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, ValueUint64(ins.Arg))
		case OpPushNull:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, ValueNull())
		case OpReadStorage:
			var value RuntimeValue
			if len(ins.Data) == 0 {
				value = runtimeStorageSnapshotValue(state)
			} else {
				value = runtimeValueFromStorage(state[string(ins.Data)])
			}
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, value)
		case OpWriteStorage:
			if readOnly {
				return rollback(async.ResultExecutionFailed, errors.New("AVM getter entrypoint cannot write storage"))
			}
			value, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on write storage"))
			}
			if len(ins.Data) == 0 {
				nextState, err := runtimeStorageFromValue(value)
				if err != nil {
					return rollback(async.ResultExecutionFailed, err)
				}
				state = nextState
			} else {
				encoded, err := CanonicalEncode(value)
				if err != nil {
					return rollback(async.ResultExecutionFailed, err)
				}
				state[string(ins.Data)] = encoded
			}
			exec.StorageWrites++
			if StorageMemoryBytes(state) > uint64(r.params.MaxMemoryBytes) {
				return rollback(async.ResultLimitExceeded, nil)
			}
		case OpDeleteStorage:
			if readOnly {
				return rollback(async.ResultExecutionFailed, errors.New("AVM getter entrypoint cannot delete storage"))
			}
			if len(ins.Data) == 0 {
				state = Storage{}
			} else {
				delete(state, string(ins.Data))
			}
			exec.StorageWrites++
			if StorageMemoryBytes(state) > uint64(r.params.MaxMemoryBytes) {
				return rollback(async.ResultLimitExceeded, nil)
			}
		case OpAdd:
			right, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on add"))
			}
			left, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on add"))
			}
			sum, err := runtimeAdd(left, right)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, sum)
		case OpSub:
			right, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on sub"))
			}
			left, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on sub"))
			}
			diff, err := runtimeSub(left, right)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, diff)
		case OpMul, OpDiv, OpMod, OpShl, OpShr, OpBitAnd, OpBitOr, OpBitXor:
			right, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on arithmetic"))
			}
			left, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on arithmetic"))
			}
			value, err := runtimeBinaryArithmetic(ins.Op, left, right)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, value)
		case OpNeg, OpBitNot:
			value, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on unary arithmetic"))
			}
			out, err := runtimeUnaryArithmetic(ins.Op, value)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, out)
		case OpEq, OpNe, OpLt, OpLe, OpGt, OpGe, OpCmp:
			right, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on compare"))
			}
			left, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on compare"))
			}
			cmp, err := runtimeCompare(left, right)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			isCmp := false
			var out bool
			switch ins.Op {
			case OpEq:
				out = cmp == 0
			case OpNe:
				out = cmp != 0
			case OpLt:
				out = cmp < 0
			case OpLe:
				out = cmp <= 0
			case OpGt:
				out = cmp > 0
			case OpGe:
				out = cmp >= 0
			case OpCmp:
				isCmp = true
			}
			if isCmp {
				stack = append(stack, ValueInt64(int64(cmp)))
				break
			}
			stack = append(stack, ValueBool(out))
		case OpAnd, OpOr:
			right, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on logic"))
			}
			left, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on logic"))
			}
			lb, err := runtimeTruthy(left)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			rb, err := runtimeTruthy(right)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			if ins.Op == OpAnd {
				stack = append(stack, ValueBool(lb && rb))
			} else {
				stack = append(stack, ValueBool(lb || rb))
			}
		case OpNot:
			value, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on not"))
			}
			b, err := runtimeTruthy(value)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, ValueBool(!b))
		case OpJump:
			if ins.Arg >= uint64(len(module.Code)) {
				return rollback(async.ResultExecutionFailed, errors.New("AVM jump target out of range"))
			}
			pc = uint32(ins.Arg)
			continue
		case OpJumpIfZero:
			value, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on conditional jump"))
			}
			b, err := runtimeTruthy(value)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			if !b {
				if ins.Arg >= uint64(len(module.Code)) {
					return rollback(async.ResultExecutionFailed, errors.New("AVM jump target out of range"))
				}
				pc = uint32(ins.Arg)
				continue
			}
		case OpAbort:
			return rollback(uint32(ins.Arg), fmt.Errorf("AVM abort with exit code %d", ins.Arg))
		case OpDup:
			if len(stack) == 0 {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on dup"))
			}
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, stack[len(stack)-1].clone())
		case OpDrop:
			if _, ok := pop(&stack); !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on drop"))
			}
		case OpCastCoins:
			value, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on cast to coins"))
			}
			num, err := runtimeNumeric(value)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, ValueCoins(num))
		case OpLoadLocal:
			slot := int(ins.Arg)
			if slot < 0 || slot >= len(locals) {
				return rollback(async.ResultExecutionFailed, fmt.Errorf("AVM load local slot %d out of range", ins.Arg))
			}
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, locals[slot].clone())
		case OpStoreLocal:
			if ins.Arg >= uint64(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			value, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on store local"))
			}
			slot := int(ins.Arg)
			if slot >= len(locals) {
				next := make([]RuntimeValue, slot+1)
				copy(next, locals)
				locals = next
			}
			locals[slot] = value.clone()
		case OpEmitInternal:
			if readOnly {
				return rollback(async.ResultExecutionFailed, errors.New("AVM getter entrypoint cannot emit internal messages"))
			}
			var (
				outgoing async.MessageEnvelope
				err      error
			)
			if len(stack) > 0 {
				msgValue, ok := pop(&stack)
				if !ok {
					return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on emit internal"))
				}
				if msgValue.Tag == TagMap {
					outgoing, err = runtimeMessageEnvelopeFromValue(msgValue, ctx, ins.Arg)
				} else {
					outgoing, err = runtimeLegacyMessageEnvelopeFromValue(msgValue, ctx, ins.Arg, ins.Data)
				}
			} else {
				outgoing, err = runtimeLegacyMessageEnvelopeFromValue(ValueNull(), ctx, ins.Arg, ins.Data)
			}
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			exec.Outgoing = append(exec.Outgoing, outgoing)
		case OpReadMsgOpcode:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			// In a bounced handler the envelope Opcode is the BounceOpcode
			// marker; the original message's opcode (needed so match(bounced)
			// can dispatch on the original message type) is carried in
			// OriginalOpcode. Everywhere else Opcode is authoritative.
			opcode := ctx.Message.Opcode
			if ctx.Message.Bounced && ctx.Message.OriginalOpcode != 0 {
				opcode = ctx.Message.OriginalOpcode
			}
			stack = append(stack, ValueUint64(uint64(opcode)))
		case OpReadMsgQueryID:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, ValueUint64(ctx.Message.QueryID))
		case OpReadBlock:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, ValueUint64(ctx.BlockHeight))
		case OpReadContractAddress:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			if len(ctx.ContractAddress) == 0 {
				stack = append(stack, ValueAddress(""))
			} else {
				stack = append(stack, ValueAddress(addressing.FormatAccAddress(ctx.ContractAddress)))
			}
		case OpReadOriginalBalance:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, ValueCoins(ctx.OriginalBalance.BigInt()))
		case OpReadAttachedValue:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, ValueCoins(ctx.AttachedValue.BigInt()))
		case OpReadLogicalTime:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, ValueTimestamp(ctx.LogicalTime))
		case OpReadBlockTimestamp:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, ValueTimestamp(ctx.BlockTimestamp))
		case OpReadRandom:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, ValueUint64(BeaconRandomU64(ctx.PrevStateRoot, ctx.BlockEntropy, ctx.Message, randomNonce)))
			randomNonce++
		case OpReadCurrentBlockLogicalTime:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, ValueTimestamp(ctx.CurrentBlockLogicalTime))
		case OpReadMsgSender:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			if len(ctx.Message.Source) == 0 {
				stack = append(stack, ValueAddress(""))
			} else {
				stack = append(stack, ValueAddress(addressing.FormatAccAddress(ctx.Message.Source)))
			}
		case OpReadMsgValue:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			amount, ok := new(big.Int).SetString(ctx.Message.Value.Amount.String(), 10)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM message value cannot be parsed"))
			}
			stack = append(stack, ValueCoins(amount))
		case OpReadMsgBody:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, ValueBytes(ctx.Message.Body))
		case OpReadMsgField:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			value, err := runtimeMessageFieldValue(ctx.Message.Body, string(ins.Data))
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, value)
		case OpReadField:
			source, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on read field"))
			}
			value, err := runtimeFieldValue(source, string(ins.Data))
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, value)
		case OpLen:
			value, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on len"))
			}
			n, err := runtimeLen(value)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, ValueUint64(n))
		case OpIsEmpty:
			value, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on is_empty"))
			}
			out, err := runtimeIsEmpty(value)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, ValueBool(out))
		case OpPushString:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, ValueString(string(ins.Data)))
		case OpPushAddress:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, ValueAddress(string(ins.Data)))
		case OpPushBytes:
			if len(stack) >= int(r.params.MaxStackDepth) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, ValueBytes(append([]byte(nil), ins.Data...)))
		case OpHash:
			value, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on hash"))
			}
			hash, err := runtimeHashValue(value)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, hash)
		case OpVerifySignature:
			publicKey, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on signature verify public key"))
			}
			signature, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on signature verify signature"))
			}
			data, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on signature verify data"))
			}
			verified, err := runtimeVerifySignature(data, signature, publicKey)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, ValueBool(verified))
		case OpCounterfactualAddress, OpAutoDeployAddress:
			value, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on counterfactual address"))
			}
			stateInit, addr, err := runtimeCounterfactualAddress(ctx, value)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, ValueAddressWithStateInit(addr, stateInit))
		case OpMapEmpty:
			stack = append(stack, ValueMapEmpty())
		case OpMapGet:
			key, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on map get key"))
			}
			m, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on map get map"))
			}
			entries, err := m.AsMap()
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			value, found, err := runtimeMapLookup(entries, key)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			if !found {
				value = ValueNull()
			}
			stack = append(stack, value)
		case OpMapSet:
			value, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on map set value"))
			}
			key, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on map set key"))
			}
			m, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on map set map"))
			}
			entries, err := m.AsMap()
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			updated, err := runtimeMapSet(entries, key, value)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, ValueMap(updated))
		case OpMapHas:
			key, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on map has key"))
			}
			m, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on map has map"))
			}
			entries, err := m.AsMap()
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			_, found, err := runtimeMapLookup(entries, key)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, ValueBool(found))
		case OpMapDelete:
			key, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on map delete key"))
			}
			m, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on map delete map"))
			}
			entries, err := m.AsMap()
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			updated, err := runtimeMapDelete(entries, key)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, ValueMap(updated))
		case OpMapKeys:
			limit, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on map keys limit"))
			}
			m, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on map keys map"))
			}
			entries, err := m.AsMap()
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			limitU64, err := limit.AsUint64()
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, runtimeMapKeys(entries, limitU64))
		case OpMapEntries:
			limit, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on map entries limit"))
			}
			m, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on map entries map"))
			}
			entries, err := m.AsMap()
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			limitU64, err := limit.AsUint64()
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, runtimeMapEntriesValue(entries, limitU64))
		case OpChargeGas:
			nextGas, overflow := safeAddU64(exec.GasUsed, ins.Arg)
			if overflow || nextGas > gasLimit {
				return rollback(async.ResultLimitExceeded, nil)
			}
			exec.GasUsed = nextGas
		case OpScheduleSelf:
			if readOnly {
				return rollback(async.ResultExecutionFailed, errors.New("AVM getter entrypoint cannot schedule self messages"))
			}
			if len(ctx.ContractAddress) == 0 {
				return rollback(async.ResultExecutionFailed, errors.New("AVM schedule self requires contract address"))
			}
			if ctx.BlockHeight == 0 {
				return rollback(async.ResultExecutionFailed, errors.New("AVM schedule self requires block height"))
			}
			if ins.Arg == 0 {
				return rollback(async.ResultExecutionFailed, errors.New("AVM schedule self delay must be positive"))
			}
			deliverAt, overflow := safeAddU64(ctx.BlockHeight, ins.Arg)
			if overflow {
				return rollback(async.ResultLimitExceeded, nil)
			}
			exec.Outgoing = append(exec.Outgoing, async.MessageEnvelope{
				Destination:    append(sdk.AccAddress(nil), ctx.ContractAddress...),
				Value:          sdk.NewCoin(appparams.BaseDenom, sdkmath.ZeroInt()),
				Opcode:         ctx.Message.Opcode,
				QueryID:        ctx.Message.QueryID,
				Body:           append([]byte(nil), ins.Data...),
				Bounce:         false,
				DeliverAtBlock: deliverAt,
				DeadlineBlock:  ctx.Message.DeadlineBlock,
				GasLimit:       ctx.Message.GasLimit,
				ForwardFee:     sdk.NewCoin(appparams.BaseDenom, async.DefaultParams().ForwardingFee),
			})
		case OpReturn:
			exec.ResultCode = uint32(ins.Arg)
			if len(stack) > 0 {
				exec.ReturnValue = stack[len(stack)-1].clone()
			}
			exec.State = state
			if exec.ResultCode != async.ResultOK {
				exec.State = originalState
				exec.Outgoing = nil
			}
			return exec, nil
		default:
			return rollback(async.ResultExecutionFailed, fmt.Errorf("AVM opcode 0x%02x is not executable", byte(ins.Op)))
		}
		pc++
	}
	exec.ResultCode = async.ResultOK
	exec.State = state
	if len(stack) > 0 {
		exec.ReturnValue = stack[len(stack)-1].clone()
	}
	return exec, nil
}

func (v RuntimeValue) clone() RuntimeValue {
	out := v
	if v.intVal != nil {
		out.intVal = new(big.Int).Set(v.intVal)
	}
	if v.bytesVal != nil {
		out.bytesVal = append([]byte(nil), v.bytesVal...)
	}
	if v.tupleVal != nil {
		out.tupleVal = append([]RuntimeValue(nil), v.tupleVal...)
	}
	if v.mapVal != nil {
		out.mapVal = runtimeMapClone(v.mapVal)
	}
	if v.stateInit != nil {
		normalized := v.stateInit.Normalize()
		out.stateInit = &normalized
	}
	return out
}

func runtimeValueFromStorage(bz []byte) RuntimeValue {
	if len(bz) == 0 {
		return ValueUint64(0)
	}
	if len(bz) == 8 {
		return ValueUint64(binary.BigEndian.Uint64(bz))
	}
	if value, _, err := CanonicalDecode(bz); err == nil {
		return value
	}
	return ValueBytes(bz)
}

func runtimeStorageSnapshotValue(state Storage) RuntimeValue {
	if len(state) == 0 {
		return ValueMapEmpty()
	}
	keys := make([]string, 0, len(state))
	for key := range state {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]runtimeMapEntry, 0, len(keys))
	for _, key := range keys {
		entry, err := runtimeMapEntryFrom(ValueString(key), runtimeValueFromStorage(state[key]))
		if err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return ValueMap(entries)
}

func runtimeStorageFromValue(value RuntimeValue) (Storage, error) {
	switch value.Tag {
	case TagNull:
		return Storage{}, nil
	case TagMap:
		entries, err := value.AsMap()
		if err != nil {
			return nil, err
		}
		state := make(Storage, len(entries))
		for _, entry := range entries {
			key, err := entry.Key.AsString()
			if err != nil {
				return nil, err
			}
			encoded, err := CanonicalEncode(entry.Value)
			if err != nil {
				return nil, err
			}
			state[key] = encoded
		}
		return state, nil
	case TagBytes:
		return DecodeSnapshot(append([]byte(nil), value.bytesVal...))
	case TagString:
		return DecodeSnapshot([]byte(value.strVal))
	default:
		encoded, err := CanonicalEncode(value)
		if err != nil {
			return nil, err
		}
		return DecodeSnapshot(encoded)
	}
}

func runtimeTruthy(v RuntimeValue) (bool, error) {
	switch v.Tag {
	case TagNull:
		return false, nil
	case TagBool:
		return v.boolVal, nil
	case TagBytes:
		return len(v.bytesVal) != 0, nil
	case TagString:
		return len(v.strVal) != 0, nil
	case TagTuple:
		return len(v.tupleVal) != 0, nil
	case TagMap:
		return len(v.mapVal) != 0, nil
	default:
		if IsInteger(v.Tag) || v.Tag == TagCoins || v.Tag == TagTimestamp {
			u, err := v.AsBigInt()
			if err != nil {
				return false, err
			}
			return u.Sign() != 0, nil
		}
		return true, nil
	}
}

func runtimeIsEmpty(v RuntimeValue) (bool, error) {
	switch v.Tag {
	case TagNull:
		return true, nil
	case TagBytes:
		return len(v.bytesVal) == 0, nil
	case TagString:
		return len(v.strVal) == 0, nil
	case TagTuple:
		return len(v.tupleVal) == 0, nil
	case TagMap:
		return len(v.mapVal) == 0, nil
	default:
		return false, nil
	}
}

func runtimeLen(v RuntimeValue) (uint64, error) {
	switch v.Tag {
	case TagNull:
		return 0, nil
	case TagBytes:
		return uint64(len(v.bytesVal)), nil
	case TagString:
		return uint64(len(v.strVal)), nil
	case TagTuple:
		return uint64(len(v.tupleVal)), nil
	case TagMap:
		return uint64(len(v.mapVal)), nil
	default:
		return 0, fmt.Errorf("AVM len requires a sized value, got %s", v.Tag)
	}
}

func runtimeNumeric(v RuntimeValue) (*big.Int, error) {
	switch v.Tag {
	case TagInt8, TagInt16, TagInt32, TagInt64, TagInt128, TagInt256, TagUint8, TagUint16, TagUint32, TagUint64, TagUint128, TagUint256, TagCoins, TagTimestamp:
		return v.AsBigInt()
	default:
		return nil, fmt.Errorf("AVM type error: expected numeric value, got %s", v.Tag)
	}
}

func runtimeAdd(left, right RuntimeValue) (RuntimeValue, error) {
	li, err := runtimeNumeric(left)
	if err != nil {
		return RuntimeValue{}, err
	}
	ri, err := runtimeNumeric(right)
	if err != nil {
		return RuntimeValue{}, err
	}
	sum := new(big.Int).Add(li, ri)
	return runtimeFromBigIntChecked(left.Tag, sum)
}

func runtimeSub(left, right RuntimeValue) (RuntimeValue, error) {
	li, err := runtimeNumeric(left)
	if err != nil {
		return RuntimeValue{}, err
	}
	ri, err := runtimeNumeric(right)
	if err != nil {
		return RuntimeValue{}, err
	}
	diff := new(big.Int).Sub(li, ri)
	if diff.Sign() < 0 && !IsSigned(left.Tag) {
		return RuntimeValue{}, errors.New("AVM unsigned subtraction underflow")
	}
	return runtimeFromBigIntChecked(left.Tag, diff)
}

func runtimeBinaryArithmetic(op Opcode, left, right RuntimeValue) (RuntimeValue, error) {
	switch op {
	case OpMul:
		li, err := runtimeNumeric(left)
		if err != nil {
			return RuntimeValue{}, err
		}
		ri, err := runtimeNumeric(right)
		if err != nil {
			return RuntimeValue{}, err
		}
		return runtimeFromBigIntChecked(left.Tag, new(big.Int).Mul(li, ri))
	case OpDiv, OpMod:
		li, err := runtimeNumeric(left)
		if err != nil {
			return RuntimeValue{}, err
		}
		ri, err := runtimeNumeric(right)
		if err != nil {
			return RuntimeValue{}, err
		}
		if ri.Sign() == 0 {
			return RuntimeValue{}, errors.New("AVM division by zero")
		}
		if op == OpDiv {
			return runtimeFromBigIntChecked(left.Tag, new(big.Int).Quo(li, ri))
		}
		return runtimeFromBigIntChecked(left.Tag, new(big.Int).Rem(li, ri))
	case OpShl, OpShr:
		li, err := runtimeNumeric(left)
		if err != nil {
			return RuntimeValue{}, err
		}
		ri, err := runtimeNumeric(right)
		if err != nil {
			return RuntimeValue{}, err
		}
		if ri.Sign() < 0 || !ri.IsUint64() || ri.Uint64() > 4096 {
			return RuntimeValue{}, errors.New("AVM invalid shift amount")
		}
		shift := uint(ri.Uint64())
		if op == OpShl {
			return runtimeFromBigIntChecked(left.Tag, new(big.Int).Lsh(li, shift))
		}
		return runtimeFromBigInt(left.Tag, new(big.Int).Rsh(li, shift)), nil
	case OpBitAnd, OpBitOr, OpBitXor:
		li, err := runtimeNumeric(left)
		if err != nil {
			return RuntimeValue{}, err
		}
		ri, err := runtimeNumeric(right)
		if err != nil {
			return RuntimeValue{}, err
		}
		out := new(big.Int)
		switch op {
		case OpBitAnd:
			out.And(li, ri)
		case OpBitOr:
			out.Or(li, ri)
		case OpBitXor:
			out.Xor(li, ri)
		}
		return runtimeFromBigInt(left.Tag, out), nil
	default:
		return RuntimeValue{}, fmt.Errorf("AVM opcode 0x%02x is not a binary arithmetic op", byte(op))
	}
}

func runtimeUnaryArithmetic(op Opcode, value RuntimeValue) (RuntimeValue, error) {
	switch op {
	case OpNeg:
		num, err := runtimeNumeric(value)
		if err != nil {
			return RuntimeValue{}, err
		}
		return runtimeFromBigIntChecked(value.Tag, new(big.Int).Neg(num))
	case OpBitNot:
		num, err := runtimeNumeric(value)
		if err != nil {
			return RuntimeValue{}, err
		}
		// For unsigned types, bitwise-not is complement within the type width:
		// ^v = (2^width - 1) - v. big.Int.Not yields -(v+1) (two's complement),
		// which is the correct value only for signed types; for wide unsigned
		// tags it would be negative and out of range.
		if width, ok := ValueBitWidth(value.Tag); ok && !IsSigned(value.Tag) {
			mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(width)), big.NewInt(1))
			return runtimeFromBigIntChecked(value.Tag, new(big.Int).Xor(num, mask))
		}
		return runtimeFromBigIntChecked(value.Tag, new(big.Int).Not(num))
	default:
		return RuntimeValue{}, fmt.Errorf("AVM opcode 0x%02x is not a unary arithmetic op", byte(op))
	}
}

func runtimeCompare(left, right RuntimeValue) (int, error) {
	if left.Tag == TagNull || right.Tag == TagNull {
		if left.Tag == right.Tag {
			return 0, nil
		}
		return -1, nil
	}
	if left.Tag == TagBool && right.Tag == TagBool {
		l := 0
		if left.boolVal {
			l = 1
		}
		r := 0
		if right.boolVal {
			r = 1
		}
		switch {
		case l < r:
			return -1, nil
		case l > r:
			return 1, nil
		default:
			return 0, nil
		}
	}
	if IsInteger(left.Tag) || left.Tag == TagCoins || left.Tag == TagTimestamp || IsInteger(right.Tag) || right.Tag == TagCoins || right.Tag == TagTimestamp {
		li, err := runtimeNumeric(left)
		if err != nil {
			return 0, err
		}
		ri, err := runtimeNumeric(right)
		if err != nil {
			return 0, err
		}
		return li.Cmp(ri), nil
	}
	if left.Tag == TagAddress && right.Tag == TagAddress {
		return strings.Compare(left.addrVal, right.addrVal), nil
	}
	if left.Tag == TagBytes && right.Tag == TagBytes {
		return bytes.Compare(left.bytesVal, right.bytesVal), nil
	}
	if left.Tag == TagString && right.Tag == TagString {
		return strings.Compare(left.strVal, right.strVal), nil
	}
	if left.Tag == TagHash && right.Tag == TagHash {
		return bytes.Compare(left.hashVal[:], right.hashVal[:]), nil
	}
	if left.Tag == TagTuple && right.Tag == TagTuple {
		if len(left.tupleVal) != len(right.tupleVal) {
			if len(left.tupleVal) < len(right.tupleVal) {
				return -1, nil
			}
			return 1, nil
		}
		for i := range left.tupleVal {
			cmp, err := runtimeCompare(left.tupleVal[i], right.tupleVal[i])
			if err != nil || cmp != 0 {
				return cmp, err
			}
		}
		return 0, nil
	}
	if left.Tag == TagMap && right.Tag == TagMap {
		leftEnc, err := CanonicalEncode(left)
		if err != nil {
			return 0, err
		}
		rightEnc, err := CanonicalEncode(right)
		if err != nil {
			return 0, err
		}
		return bytes.Compare(leftEnc, rightEnc), nil
	}
	if left.Tag == right.Tag {
		return 0, nil
	}
	return strings.Compare(left.Tag.String(), right.Tag.String()), nil
}

func runtimeFromBigInt(tag ValueTag, v *big.Int) RuntimeValue {
	switch tag {
	case TagInt8:
		return ValueInt8(int8(v.Int64()))
	case TagInt16:
		return ValueInt16(int16(v.Int64()))
	case TagInt32:
		return ValueInt32(int32(v.Int64()))
	case TagInt64:
		return ValueInt64(v.Int64())
	case TagUint8:
		return ValueUint8(uint8(v.Uint64()))
	case TagUint16:
		return ValueUint16(uint16(v.Uint64()))
	case TagUint32:
		return ValueUint32(uint32(v.Uint64()))
	case TagUint64:
		return ValueUint64(v.Uint64())
	case TagUint128:
		return ValueBigUint128(v)
	case TagUint256:
		return RuntimeValue{Tag: TagUint256, intVal: new(big.Int).Set(v)}
	case TagCoins:
		return ValueCoins(v)
	case TagTimestamp:
		return ValueTimestamp(v.Uint64())
	default:
		if IsSigned(tag) {
			return RuntimeValue{Tag: tag, intVal: new(big.Int).Set(v)}
		}
		return RuntimeValue{Tag: tag, intVal: new(big.Int).Set(v)}
	}
}

// enforceIntWidth rejects an arithmetic result that does not fit the target
// integer type's bit width. This applies to ALL integer widths: the <=64-bit
// tags used to be exempt because runtimeFromBigInt reduces them via
// Uint64/Int64 truncation, but that silently WRAPS modulo 2^width instead of
// trapping, so a u64 balance/supply counter could overflow undetected and
// bypass a contract's own invariants. Checking every width makes overflow
// fail-closed for u8..u256, matching the documented trap-on-overflow ISA
// semantics, the encode-time width check, and the existing underflow trap in
// runtimeSub. See SEC-MED: <128-bit int add/mul silently wraps.
func enforceIntWidth(tag ValueTag, v *big.Int) error {
	width, ok := ValueBitWidth(tag)
	if !ok || v == nil {
		return nil
	}
	if IsSigned(tag) {
		bound := new(big.Int).Lsh(big.NewInt(1), uint(width-1)) // 2^(w-1)
		maxInclusive := new(big.Int).Sub(bound, big.NewInt(1))  // 2^(w-1)-1
		minInclusive := new(big.Int).Neg(bound)                 // -2^(w-1)
		if v.Cmp(minInclusive) < 0 || v.Cmp(maxInclusive) > 0 {
			return fmt.Errorf("AVM integer overflow for %s", tag)
		}
		return nil
	}
	if v.Sign() < 0 {
		return fmt.Errorf("AVM unsigned integer underflow for %s", tag)
	}
	limit := new(big.Int).Lsh(big.NewInt(1), uint(width)) // 2^w
	if v.Cmp(limit) >= 0 {
		return fmt.Errorf("AVM integer overflow for %s", tag)
	}
	return nil
}

// runtimeFromBigIntChecked bounds the result of an arithmetic operation to the
// target type's width before constructing the value, returning an error on
// overflow/underflow instead of letting the magnitude grow unbounded.
func runtimeFromBigIntChecked(tag ValueTag, v *big.Int) (RuntimeValue, error) {
	if err := enforceIntWidth(tag, v); err != nil {
		return RuntimeValue{}, err
	}
	return runtimeFromBigInt(tag, v), nil
}

func runtimeMessageFieldValue(body []byte, field string) (RuntimeValue, error) {
	type codecFieldValue struct {
		Name  string          `json:"name"`
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	}
	var values []codecFieldValue
	if err := json.Unmarshal(body, &values); err != nil {
		if len(body) == 0 {
			return ValueNull(), nil
		}
		return ValueBytes(body), nil
	}
	for _, value := range values {
		if !strings.EqualFold(value.Name, field) {
			continue
		}
		return runtimeValueFromJSONField(value.Type, value.Value)
	}
	return ValueNull(), nil
}

func runtimeFieldValue(source RuntimeValue, field string) (RuntimeValue, error) {
	switch source.Tag {
	case TagNull:
		return ValueNull(), nil
	case TagBytes:
		value, err := runtimeMessageFieldValue(source.bytesVal, field)
		if err != nil {
			preview := string(source.bytesVal)
			if len(preview) > 128 {
				preview = preview[:128] + "..."
			}
			return RuntimeValue{}, fmt.Errorf("AVM bytes field access %q on %q failed: %w", field, preview, err)
		}
		return value, nil
	case TagString:
		value, err := runtimeMessageFieldValue([]byte(source.strVal), field)
		if err != nil {
			preview := source.strVal
			if len(preview) > 128 {
				preview = preview[:128] + "..."
			}
			return RuntimeValue{}, fmt.Errorf("AVM string field access %q on %q failed: %w", field, preview, err)
		}
		return value, nil
	case TagMap:
		entries, err := source.AsMap()
		if err != nil {
			return RuntimeValue{}, err
		}
		value, found, err := runtimeMapLookup(entries, ValueString(field))
		if err != nil {
			return RuntimeValue{}, err
		}
		if !found {
			return ValueNull(), nil
		}
		return value, nil
	case TagTuple:
		switch strings.ToLower(strings.TrimSpace(field)) {
		case "key", "first", "left", "0":
			if len(source.tupleVal) >= 1 {
				return source.tupleVal[0].clone(), nil
			}
		case "value", "second", "right", "1":
			if len(source.tupleVal) >= 2 {
				return source.tupleVal[1].clone(), nil
			}
		}
		return RuntimeValue{}, fmt.Errorf("AVM field access requires a structured bytes value, got %s", source.Tag)
	default:
		return RuntimeValue{}, fmt.Errorf("AVM field access requires a structured bytes value, got %s", source.Tag)
	}
}

func runtimeValueFromJSONField(typ string, raw json.RawMessage) (RuntimeValue, error) {
	kind := strings.ToLower(strings.TrimSpace(typ))
	kind = strings.TrimSuffix(kind, "?")
	decodeErr := func(err error) error {
		preview := string(raw)
		if len(preview) > 128 {
			preview = preview[:128] + "..."
		}
		return fmt.Errorf("AVM field decode %q from %q failed: %w", kind, preview, err)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return ValueNull(), nil
	}
	switch kind {
	case "bool":
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return RuntimeValue{}, decodeErr(err)
		}
		return ValueBool(b), nil
	case "u2", "u4", "u8", "u16", "u32", "u64", "timestamp":
		var v uint64
		if err := json.Unmarshal(raw, &v); err != nil {
			return RuntimeValue{}, decodeErr(err)
		}
		if maxV, ok := map[string]uint64{"u2": 3, "u4": 15, "u8": 255, "u16": 65535, "u32": 4294967295}[kind]; ok && v > maxV {
			return RuntimeValue{}, decodeErr(fmt.Errorf("value %d out of range for %s", v, kind))
		}
		return ValueUint64(v), nil
	case "i2", "i4", "i64":
		var v int64
		if err := json.Unmarshal(raw, &v); err != nil {
			return RuntimeValue{}, decodeErr(err)
		}
		switch kind {
		case "i2":
			if v < -2 || v > 1 {
				return RuntimeValue{}, decodeErr(fmt.Errorf("value %d out of range for i2", v))
			}
		case "i4":
			if v < -8 || v > 7 {
				return RuntimeValue{}, decodeErr(fmt.Errorf("value %d out of range for i4", v))
			}
		}
		return ValueInt64(v), nil
	case "coins":
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			v, ok := new(big.Int).SetString(strings.TrimSpace(s), 10)
			if !ok || v.Sign() < 0 {
				return RuntimeValue{}, decodeErr(fmt.Errorf("invalid coins amount %q", s))
			}
			return ValueCoins(v), nil
		}
		var v uint64
		if err := json.Unmarshal(raw, &v); err != nil {
			return RuntimeValue{}, decodeErr(err)
		}
		return ValueCoins(new(big.Int).SetUint64(v)), nil
	case "address":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return RuntimeValue{}, decodeErr(err)
		}
		return ValueAddress(s), nil
	case "hash", "hash32":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return RuntimeValue{}, decodeErr(err)
		}
		h, err := hex.DecodeString(strings.TrimSpace(s))
		if err != nil {
			return RuntimeValue{}, err
		}
		return ValueHashFromBytes(h), nil
	case "string":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return RuntimeValue{}, decodeErr(err)
		}
		return ValueString(s), nil
	case "bytes", "code", "chunk", "stateinit":
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			trimmed := strings.TrimSpace(s)
			if decoded, err := hex.DecodeString(trimmed); err == nil && len(trimmed)%2 == 0 {
				return ValueBytes(decoded), nil
			}
			if decoded, err := decodeBase64String(trimmed); err == nil {
				return ValueBytes(decoded), nil
			}
			return ValueBytes([]byte(s)), nil
		}
		return ValueBytes(append([]byte(nil), raw...)), nil
	default:
		if numericValue, ok := runtimeParseJSONNumericValue(raw); ok {
			return ValueUint64(numericValue), nil
		}
		return ValueBytes(append([]byte(nil), raw...)), nil
	}
}

func runtimeParseJSONNumericValue(raw json.RawMessage) (uint64, bool) {
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

func runtimeHashValue(v RuntimeValue) (RuntimeValue, error) {
	switch v.Tag {
	case TagHash:
		return v.clone(), nil
	case TagBytes, TagString:
		data, err := v.AsBytes()
		if err != nil {
			return RuntimeValue{}, err
		}
		root, err := ToChunkPayload(data, chunk.TypeNormal)
		if err != nil {
			sum := sha256.Sum256(data)
			return ValueHash(sum), nil
		}
		sum := root.Hash()
		var out [32]byte
		copy(out[:], sum)
		return ValueHash(out), nil
	default:
		encoded, err := CanonicalEncode(v)
		if err != nil {
			return RuntimeValue{}, err
		}
		sum := sha256.Sum256(encoded)
		return ValueHash(sum), nil
	}
}

func runtimeValueBytes(v RuntimeValue) ([]byte, error) {
	switch v.Tag {
	case TagHash:
		return append([]byte(nil), v.hashVal[:]...), nil
	case TagBytes, TagString:
		return v.AsBytes()
	default:
		return CanonicalEncode(v)
	}
}

func runtimeVerifySignature(data, signature, publicKey RuntimeValue) (bool, error) {
	message, err := runtimeValueBytes(data)
	if err != nil {
		return false, err
	}
	sig, err := runtimeValueBytes(signature)
	if err != nil {
		return false, err
	}
	pub, err := runtimeValueBytes(publicKey)
	if err != nil {
		return false, err
	}
	if len(pub) != ed25519.PublicKeySize {
		return false, errors.New("AVM ed25519 public key must be 32 bytes")
	}
	if len(sig) != ed25519.SignatureSize {
		return false, errors.New("AVM ed25519 signature must be 64 bytes")
	}
	return ed25519.Verify(ed25519.PublicKey(pub), message, sig), nil
}

func runtimeCounterfactualAddress(ctx RuntimeContext, value RuntimeValue) (*contracttypes.StateInit, string, error) {
	stateInit, err := runtimeStateInitFromValue(ctx, value)
	if err != nil {
		return nil, "", err
	}
	if len(ctx.ContractAddress) == 0 {
		return nil, "", errors.New("AVM counterfactual address requires contract address")
	}
	deployer := addressing.FormatAccAddress(ctx.ContractAddress)
	user, _, err := contracttypes.DeriveContractAddressFromStateInit(contracttypes.DefaultContractChainID, contracttypes.DefaultContractNamespace, deployer, *stateInit, contracttypes.DefaultParams())
	if err != nil {
		return nil, "", err
	}
	return stateInit, user, nil
}

func runtimeStateInitFromValue(ctx RuntimeContext, value RuntimeValue) (*contracttypes.StateInit, error) {
	if value.Tag != TagMap {
		return nil, fmt.Errorf("AVM state init requires a struct-like value, got %s", value.Tag)
	}
	entries, err := value.AsMap()
	if err != nil {
		return nil, err
	}
	fields := map[string]RuntimeValue{}
	for _, entry := range entries {
		name, err := entry.Key.AsString()
		if err != nil {
			// accept address-like keys encoded as strings only
			if entry.Key.Tag == TagString {
				name = entry.Key.strVal
			} else {
				return nil, fmt.Errorf("AVM state init field names must be strings, got %s", entry.Key.Tag)
			}
		}
		fields[strings.ToLower(strings.TrimSpace(name))] = entry.Value.clone()
	}
	codeValue, ok := fields["code"]
	if !ok {
		return nil, errors.New("AVM state init requires code field")
	}
	codeBytes, err := runtimeCodeBytes(codeValue)
	if err != nil {
		return nil, err
	}
	// Code identity is defined by CodeHash (compiler/compile.go, avm.CodeHash)
	// as sha256 of the encoded module bytes — a flat digest, not the
	// chunk-tree Merkle hash that runtimeHashValue/.hash() computes for
	// generic data. Deriving addresses with the chunk hash here would give
	// contracts an address their own compiled ModuleHash never matches.
	hashBytes := sha256.Sum256(codeBytes)
	dataValue, ok := fields["data"]
	if !ok {
		dataValue = ValueNull()
	}
	// InitData must be usable verbatim as the deployed child's initial
	// storage (async/process.go seeds ContractAccount.State straight from
	// it). A struct literal `data:` value (the canonical form for typed
	// child init) is converted from field-name/value pairs into
	// EncodeSnapshot-shaped bytes so a contract's own counterfactualAddress/
	// autoDeployAddress derives an address whose "storage" DecodeSnapshot
	// can actually parse. A raw bytes/string `data:` value (e.g.
	// Bytes.fromHex(...)) is already a caller-prepared opaque blob and is
	// used as-is.
	var initData []byte
	switch dataValue.Tag {
	case TagMap, TagNull:
		initStorage, err := runtimeStorageFromStructValue(dataValue)
		if err != nil {
			return nil, err
		}
		initData = EncodeSnapshot(initStorage)
	default:
		initData, err = dataValue.AsBytes()
		if err != nil {
			return nil, fmt.Errorf("AVM state init data must be a struct literal, bytes, or null: %w", err)
		}
	}
	owner := ""
	if len(ctx.ContractAddress) != 0 {
		owner = addressing.FormatAccAddress(ctx.ContractAddress)
	}
	owner = addressValueOrDefault(fields["owner"], owner)
	if owner == "" && len(ctx.ContractAddress) != 0 {
		owner = addressing.FormatAccAddress(ctx.ContractAddress)
	}
	salt := []byte{}
	if v, ok := fields["salt"]; ok {
		salt, err = v.AsBytes()
		if err != nil {
			return nil, err
		}
	}
	var init contracttypes.StateInit
	init.ABIVersion = 1
	init.CodeID = hex.EncodeToString(hashBytes[:])
	init.CodeHash = hex.EncodeToString(hashBytes[:])
	init.InitData = initData
	init.Salt = string(salt)
	init.Owner = owner
	init.InitialStorageRoot = contracttypes.DefaultInitialStorageRoot
	if v, ok := fields["balance"]; ok {
		if coins, err := v.AsUint64(); err == nil {
			init.InitialBalanceNAET = coins
		}
	}
	return &init, nil
}

// runtimeStorageFromStructValue converts a struct-literal RuntimeValue (as
// produced by IRExprStruct, e.g. the `data:` argument to
// counterfactualAddress/autoDeployAddress) into a Storage map, preserving
// field name casing exactly since OpReadStorage/OpWriteStorage key lookups
// are case-sensitive. TagNull yields an empty storage (no init data).
func runtimeStorageFromStructValue(value RuntimeValue) (Storage, error) {
	if value.Tag == TagNull {
		return Storage{}, nil
	}
	entries, err := value.AsMap()
	if err != nil {
		return nil, fmt.Errorf("AVM state init data must be a struct literal or null: %w", err)
	}
	out := make(Storage, len(entries))
	for _, entry := range entries {
		name, err := entry.Key.AsString()
		if err != nil {
			return nil, fmt.Errorf("AVM state init data field name must be a string: %w", err)
		}
		encoded, err := CanonicalEncode(entry.Value)
		if err != nil {
			return nil, fmt.Errorf("AVM state init data field %q: %w", name, err)
		}
		out[name] = encoded
	}
	return out, nil
}

func runtimeCodeBytes(value RuntimeValue) ([]byte, error) {
	switch value.Tag {
	case TagBytes:
		return append([]byte(nil), value.bytesVal...), nil
	case TagChunkRef:
		if value.chunkRef == nil {
			return nil, errors.New("AVM code chunk is empty")
		}
		return FromChunkPayload(value.chunkRef)
	case TagMap:
		entries, err := value.AsMap()
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.Key.Tag != TagString {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(entry.Key.strVal)) {
			case "hex":
				if entry.Value.Tag != TagString {
					return nil, errors.New("AVM code hex field must be a string")
				}
				data, err := hex.DecodeString(strings.TrimSpace(entry.Value.strVal))
				if err != nil {
					return nil, err
				}
				return data, nil
			case "base64":
				if entry.Value.Tag != TagString {
					return nil, errors.New("AVM code base64 field must be a string")
				}
				data, err := decodeBase64String(entry.Value.strVal)
				if err != nil {
					return nil, err
				}
				return data, nil
			case "chunks":
				if entry.Value.Tag != TagString {
					return nil, errors.New("AVM code chunks field must be a string")
				}
				return []byte(entry.Value.strVal), nil
			}
		}
		encoded, err := CanonicalEncode(value)
		if err != nil {
			return nil, err
		}
		return encoded, nil
	default:
		encoded, err := CanonicalEncode(value)
		if err != nil {
			return nil, err
		}
		return encoded, nil
	}
}

func runtimeCodecBytes(value RuntimeValue) ([]byte, error) {
	switch value.Tag {
	case TagNull:
		return nil, nil
	case TagBytes:
		return append([]byte(nil), value.bytesVal...), nil
	case TagString:
		return []byte(value.strVal), nil
	case TagBool:
		return json.Marshal(value.boolVal)
	case TagAddress:
		return json.Marshal(value.addrVal)
	case TagHash:
		return json.Marshal(hex.EncodeToString(value.hashVal[:]))
	case TagCoins, TagTimestamp, TagInt8, TagInt16, TagInt32, TagInt64, TagInt128, TagInt256, TagUint8, TagUint16, TagUint32, TagUint64, TagUint128, TagUint256:
		if value.intVal == nil {
			return json.Marshal(0)
		}
		if value.intVal.IsUint64() {
			return json.Marshal(value.intVal.Uint64())
		}
		return json.Marshal(value.intVal.String())
	case TagTuple:
		out := make([]json.RawMessage, 0, len(value.tupleVal))
		for _, elem := range value.tupleVal {
			encoded, err := runtimeCodecBytes(elem)
			if err != nil {
				return nil, err
			}
			out = append(out, json.RawMessage(encoded))
		}
		return json.Marshal(out)
	case TagMap:
		entries, err := value.AsMap()
		if err != nil {
			return nil, err
		}
		type codecFieldValue struct {
			Name  string          `json:"name"`
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		}
		fields := make([]codecFieldValue, 0, len(entries))
		for _, entry := range entries {
			name, err := entry.Key.AsString()
			if err != nil {
				return nil, err
			}
			fieldBytes, fieldType, err := runtimeCodecField(entry.Value)
			if err != nil {
				return nil, err
			}
			fields = append(fields, codecFieldValue{Name: name, Type: fieldType, Value: json.RawMessage(fieldBytes)})
		}
		return json.Marshal(fields)
	case TagChunkRef:
		return json.Marshal(runtimeChunkLikeCodecMap(value.chunkRef))
	default:
		encoded, err := CanonicalEncode(value)
		if err != nil {
			return nil, err
		}
		return encoded, nil
	}
}

// runtimeChunkLikeCodecMap mirrors the compiler's canonicalChunkLikeValue
// shape ({hex, base64, hash, chunks}) so a Chunk/Code value produced at
// runtime (e.g. inside autoDeployAddress/counterfactualAddress init data)
// encodes to the exact same host-side JSON representation the compiler's
// Codec uses, keeping runtime-derived and off-chain-derived addresses in
// sync for the same logical value.
func runtimeChunkLikeCodecMap(root *chunk.Chunk) map[string]any {
	if root == nil {
		return map[string]any{"hex": "", "base64": "", "hash": "", "chunks": ""}
	}
	data, err := FromChunkPayload(root)
	if err != nil {
		return map[string]any{"hex": "", "base64": "", "hash": "", "chunks": ""}
	}
	return map[string]any{
		"hex":    hex.EncodeToString(data),
		"base64": base64.StdEncoding.EncodeToString(data),
		"hash":   hex.EncodeToString(root.Hash()),
		"chunks": chunk.RenderSource(root),
	}
}

func runtimeCodecField(value RuntimeValue) ([]byte, string, error) {
	switch value.Tag {
	case TagNull:
		return []byte("null"), "null", nil
	case TagBool:
		bz, err := json.Marshal(value.boolVal)
		return bz, "bool", err
	case TagAddress:
		bz, err := json.Marshal(value.addrVal)
		return bz, "address", err
	case TagHash:
		bz, err := json.Marshal(hex.EncodeToString(value.hashVal[:]))
		return bz, "hash", err
	case TagBytes:
		bz, err := json.Marshal(hex.EncodeToString(value.bytesVal))
		return bz, "bytes", err
	case TagString:
		bz, err := json.Marshal(value.strVal)
		return bz, "string", err
	case TagCoins, TagTimestamp, TagInt8, TagInt16, TagInt32, TagInt64, TagInt128, TagInt256, TagUint8, TagUint16, TagUint32, TagUint64, TagUint128, TagUint256:
		if value.intVal == nil {
			bz, err := json.Marshal(0)
			return bz, value.Tag.String(), err
		}
		if value.intVal.IsUint64() {
			bz, err := json.Marshal(value.intVal.Uint64())
			return bz, value.Tag.String(), err
		}
		bz, err := json.Marshal(value.intVal.String())
		return bz, value.Tag.String(), err
	case TagMap, TagTuple:
		bz, err := runtimeCodecBytes(value)
		return bz, "bytes", err
	case TagChunkRef:
		bz, err := json.Marshal(runtimeChunkLikeCodecMap(value.chunkRef))
		return bz, "chunk", err
	default:
		bz, err := runtimeCodecBytes(value)
		return bz, "bytes", err
	}
}

func addressValueOrDefault(value RuntimeValue, fallback string) string {
	if value.Tag == TagAddress {
		if value.addrVal != "" {
			return value.addrVal
		}
	}
	if value.Tag == TagString {
		if value.strVal != "" {
			return value.strVal
		}
	}
	return fallback
}

func decodeBase64String(text string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text))
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

// emitCoinFromBigInt safely converts a contract-controlled amount into an
// outbound coin. A negative amount would panic sdk.NewCoin, and an amount wider
// than 256 bits would panic sdkmath.NewIntFromBigInt; a coins value can never
// legitimately be either, so both are rejected as a normal execution error
// (rolled back) instead of being allowed to panic the interpreter.
func emitCoinFromBigInt(amount *big.Int) (sdk.Coin, error) {
	if amount == nil {
		amount = new(big.Int)
	}
	if amount.Sign() < 0 {
		return sdk.Coin{}, errors.New("AVM emit amount must not be negative")
	}
	if amount.BitLen() > 128 {
		return sdk.Coin{}, fmt.Errorf("AVM emit amount exceeds coins width (128 bits)")
	}
	return sdk.NewCoin(appparams.BaseDenom, sdkmath.NewIntFromBigInt(amount)), nil
}

func runtimeMessageEnvelopeFromValue(value RuntimeValue, ctx RuntimeContext, defaultOpcode uint64) (async.MessageEnvelope, error) {
	if value.Tag != TagMap {
		return async.MessageEnvelope{}, fmt.Errorf("AVM emit internal requires a message envelope object, got %s", value.Tag)
	}
	entries, err := value.AsMap()
	if err != nil {
		return async.MessageEnvelope{}, err
	}
	get := func(name string) (RuntimeValue, bool, error) {
		return runtimeMapLookup(entries, ValueString(name))
	}
	receiver, ok, err := get("receiver")
	if err != nil {
		return async.MessageEnvelope{}, err
	}
	if !ok {
		receiver, ok, err = get("dest")
		if err != nil {
			return async.MessageEnvelope{}, err
		}
	}
	if !ok {
		return async.MessageEnvelope{}, errors.New("AVM emit internal requires receiver")
	}
	dest, err := receiver.AsAddress()
	if err != nil {
		return async.MessageEnvelope{}, fmt.Errorf("AVM emit internal receiver in entry %d opcode 0x%08x must be address-like, got %s: %w", ctx.Entry, ctx.Message.Opcode, receiver.Tag, err)
	}
	amountValue, ok, err := get("amount")
	if err != nil {
		return async.MessageEnvelope{}, err
	}
	if !ok {
		amountValue, ok, err = get("value")
		if err != nil {
			return async.MessageEnvelope{}, err
		}
	}
	amount := new(big.Int)
	if ok {
		if amount, err = amountValue.AsBigInt(); err != nil {
			return async.MessageEnvelope{}, err
		}
	}
	bodyValue, ok, err := get("body")
	if err != nil {
		return async.MessageEnvelope{}, err
	}
	if !ok {
		return async.MessageEnvelope{}, errors.New("AVM emit internal requires body")
	}
	body, err := runtimeCodecBytes(bodyValue)
	if err != nil {
		return async.MessageEnvelope{}, err
	}
	bounce := true
	if bounceValue, ok, err := get("bounce"); err != nil {
		return async.MessageEnvelope{}, err
	} else if ok {
		bounce, err = runtimeTruthy(bounceValue)
		if err != nil {
			return async.MessageEnvelope{}, err
		}
	}
	opcode := uint32(defaultOpcode)
	if opcodeValue, ok, err := get("opcode"); err != nil {
		return async.MessageEnvelope{}, err
	} else if ok {
		opcodeAmount, err := opcodeValue.AsUint64()
		if err != nil {
			return async.MessageEnvelope{}, err
		}
		if opcodeAmount <= uint64(^uint32(0)) {
			opcode = uint32(opcodeAmount)
		}
	}
	queryID := ctx.Message.QueryID
	if queryValue, ok, err := get("queryId"); err != nil {
		return async.MessageEnvelope{}, err
	} else if !ok {
		if queryValue, ok, err = get("query_id"); err != nil {
			return async.MessageEnvelope{}, err
		} else if ok {
			queryID, err = queryValue.AsUint64()
			if err != nil {
				return async.MessageEnvelope{}, err
			}
		}
	} else {
		queryID, err = queryValue.AsUint64()
		if err != nil {
			return async.MessageEnvelope{}, err
		}
	}
	var stateInit *contracttypes.StateInit
	if stateInitValue, ok, err := get("stateInit"); err != nil {
		return async.MessageEnvelope{}, err
	} else if ok {
		stateInit, err = runtimeStateInitFromValue(ctx, stateInitValue)
		if err != nil {
			return async.MessageEnvelope{}, err
		}
	}
	if stateInit == nil && receiver.stateInit != nil {
		normalized := receiver.stateInit.Normalize()
		stateInit = &normalized
	}
	if stateInit != nil {
		if len(ctx.ContractAddress) == 0 {
			return async.MessageEnvelope{}, errors.New("AVM emit internal requires contract address for stateInit attachment")
		}
		destFromStateInit, _, err := contracttypes.DeriveContractAddressFromStateInit(contracttypes.DefaultContractChainID, contracttypes.DefaultContractNamespace, addressing.FormatAccAddress(ctx.ContractAddress), *stateInit, contracttypes.DefaultParams())
		if err != nil {
			return async.MessageEnvelope{}, err
		}
		if !strings.EqualFold(destFromStateInit, dest) {
			return async.MessageEnvelope{}, fmt.Errorf("AVM emit internal receiver %s does not match derived state init address %s", dest, destFromStateInit)
		}
	}
	destAddr, err := addressing.ParseAccAddress(dest)
	if err != nil {
		return async.MessageEnvelope{}, err
	}
	value_, err := emitCoinFromBigInt(amount)
	if err != nil {
		return async.MessageEnvelope{}, err
	}
	msg := async.MessageEnvelope{
		Source:      append(sdk.AccAddress(nil), ctx.ContractAddress...),
		Destination: append(sdk.AccAddress(nil), destAddr...),
		Value:       value_,
		Opcode:      opcode,
		QueryID:     queryID,
		Body:        body,
		Bounce:      bounce,
		Bounced:     false,
		GasLimit:    ctx.Message.GasLimit,
		ForwardFee:  sdk.NewCoin(appparams.BaseDenom, async.DefaultParams().ForwardingFee),
	}
	if ctx.LogicalTime != 0 {
		msg.CreatedLogicalTime = ctx.LogicalTime + 1
	} else {
		msg.CreatedLogicalTime = ctx.CurrentBlockLogicalTime
	}
	msg.Depth = ctx.Message.Depth + 1
	if stateInit != nil {
		normalized := stateInit.Normalize()
		msg.StateInit = &normalized
	}
	return msg, nil
}

func runtimeLegacyMessageEnvelopeFromValue(value RuntimeValue, ctx RuntimeContext, defaultOpcode uint64, data []byte) (async.MessageEnvelope, error) {
	amount := new(big.Int)
	if value.Tag != TagNull {
		numeric, err := runtimeNumeric(value)
		if err != nil {
			return async.MessageEnvelope{}, err
		}
		amount = numeric
	}
	if len(ctx.EmitDestination) == 0 {
		return async.MessageEnvelope{}, errors.New("AVM emit internal requires EmitDestination for legacy send")
	}
	legacyValue, err := emitCoinFromBigInt(amount)
	if err != nil {
		return async.MessageEnvelope{}, err
	}
	msg := async.MessageEnvelope{
		Source:      append(sdk.AccAddress(nil), ctx.ContractAddress...),
		Destination: append(sdk.AccAddress(nil), ctx.EmitDestination...),
		Value:       legacyValue,
		Opcode:      uint32(defaultOpcode),
		QueryID:     ctx.Message.QueryID,
		Body:        append([]byte(nil), data...),
		Bounce:      true,
		Bounced:     false,
		GasLimit:    ctx.Message.GasLimit,
		ForwardFee:  sdk.NewCoin(appparams.BaseDenom, async.DefaultParams().ForwardingFee),
	}
	if ctx.LogicalTime != 0 {
		msg.CreatedLogicalTime = ctx.LogicalTime + 1
	} else {
		msg.CreatedLogicalTime = ctx.CurrentBlockLogicalTime
	}
	msg.Depth = ctx.Message.Depth + 1
	return msg, nil
}

func (r *Runner) AsyncHandler(module Module, storage Storage, ctx RuntimeContext) async.Handler {
	// autoDetectEntry is fixed once at registration time: it reflects whether
	// the caller asked for per-message routing (Entry left zero/EntryDeploy)
	// or pinned a specific entrypoint (e.g. EntryReceiveExternal). The
	// returned closure must never mutate the captured ctx in place — Go
	// closures capture by reference, so writing into ctx.Entry here would
	// leak the PREVIOUS call's entrypoint into the next call (e.g. an
	// internal message's EntryReceiveInternal would incorrectly stick
	// around for a later bounced message on the same handler instance).
	autoDetectEntry := ctx.Entry == 0 || ctx.Entry == EntryDeploy
	return func(contract async.ContractAccount, msg async.MessageEnvelope) async.ExecutionResult {
		callCtx := ctx
		baseStorage := CloneStorage(storage)
		if storage == nil && len(contract.State) > 0 {
			decoded, err := DecodeSnapshot(contract.State)
			if err != nil {
				return async.ExecutionResult{NewState: contract.State, ResultCode: async.ResultExecutionFailed, Error: err.Error()}
			}
			baseStorage = decoded
		}
		callCtx.ContractAddress = contract.Address
		callCtx.Message = msg
		if msg.ExecutionBlockHeight != 0 {
			callCtx.BlockHeight = msg.ExecutionBlockHeight
		}
		if autoDetectEntry {
			if msg.Bounced {
				callCtx.Entry = EntryReceiveBounced
			} else {
				callCtx.Entry = EntryReceiveInternal
			}
		}
		callCtx.GasLimit = msg.GasLimit
		exec, err := r.Run(module, baseStorage, callCtx)
		if err != nil {
			return async.ExecutionResult{NewState: contract.State, ResultCode: async.ResultExecutionFailed, Error: err.Error()}
		}
		if exec.ResultCode != async.ResultOK {
			return async.ExecutionResult{
				NewState:      contract.State,
				Outgoing:      nil,
				GasUsed:       exec.GasUsed,
				StorageWrites: exec.StorageWrites,
				ResultCode:    exec.ResultCode,
			}
		}
		snapshot := EncodeSnapshot(exec.State)
		return async.ExecutionResult{
			NewState:      snapshot,
			Outgoing:      exec.Outgoing,
			GasUsed:       exec.GasUsed,
			StorageWrites: exec.StorageWrites,
			ResultCode:    exec.ResultCode,
		}
	}
}

func EncodeModule(module Module) ([]byte, error) {
	if len(module.Code) == 0 {
		return nil, errors.New("AVM module code must not be empty")
	}
	buf := bytes.NewBuffer(nil)
	buf.WriteString(Magic)
	writeU16(buf, module.Version)
	buf.Write(module.MetadataHash[:])
	writeU16(buf, uint16(len(module.Imports)))
	for _, host := range module.Imports {
		writeU16(buf, uint16(host))
	}
	writeU16(buf, uint16(len(module.Exports)))
	entries := make([]int, 0, len(module.Exports))
	for entry := range module.Exports {
		entries = append(entries, int(entry))
	}
	sort.Ints(entries)
	for _, raw := range entries {
		entry := Entrypoint(raw)
		buf.WriteByte(byte(entry))
		writeU32(buf, module.Exports[entry])
	}
	writeU32(buf, uint32(len(module.Code)))
	for _, ins := range module.Code {
		buf.WriteByte(byte(ins.Op))
		writeU64(buf, ins.Arg)
		if len(ins.Data) > MaxKeySize {
			return nil, fmt.Errorf("AVM instruction data must be <= %d bytes", MaxKeySize)
		}
		writeU16(buf, uint16(len(ins.Data)))
		buf.Write(ins.Data)
	}
	return buf.Bytes(), nil
}

func DecodeModule(bz []byte) (Module, error) {
	if len(bz) < 4+2+MetadataHashLength {
		return Module{}, errors.New("AVM bytecode is malformed")
	}
	reader := bytes.NewReader(bz)
	magic := make([]byte, 4)
	if _, err := reader.Read(magic); err != nil {
		return Module{}, err
	}
	if string(magic) != Magic {
		return Module{}, errors.New("AVM bytecode has invalid module header")
	}
	version, err := readU16(reader)
	if err != nil {
		return Module{}, err
	}
	var metadata [MetadataHashLength]byte
	if _, err := reader.Read(metadata[:]); err != nil {
		return Module{}, err
	}
	importCount, err := readU16(reader)
	if err != nil {
		return Module{}, err
	}
	imports := make([]HostFunction, importCount)
	for i := range imports {
		value, err := readU16(reader)
		if err != nil {
			return Module{}, err
		}
		imports[i] = HostFunction(value)
	}
	exportCount, err := readU16(reader)
	if err != nil {
		return Module{}, err
	}
	exports := make(map[Entrypoint]uint32, exportCount)
	for i := uint16(0); i < exportCount; i++ {
		entry, err := reader.ReadByte()
		if err != nil {
			return Module{}, err
		}
		offset, err := readU32(reader)
		if err != nil {
			return Module{}, err
		}
		exports[Entrypoint(entry)] = offset
	}
	codeCount, err := readU32(reader)
	if err != nil {
		return Module{}, err
	}
	code := make([]Instruction, codeCount)
	for i := range code {
		op, err := reader.ReadByte()
		if err != nil {
			return Module{}, err
		}
		arg, err := readU64(reader)
		if err != nil {
			return Module{}, err
		}
		dataLen, err := readU16(reader)
		if err != nil {
			return Module{}, err
		}
		var data []byte
		if dataLen > 0 {
			data = make([]byte, dataLen)
			if _, err := io.ReadFull(reader, data); err != nil {
				return Module{}, err
			}
		}
		code[i] = Instruction{Op: Opcode(op), Arg: arg, Data: data}
	}
	if reader.Len() != 0 {
		return Module{}, errors.New("AVM bytecode has trailing data")
	}
	return Module{Version: version, Imports: imports, Exports: exports, MetadataHash: metadata, Code: code}, nil
}

func CodeHash(module Module) ([32]byte, error) {
	encoded, err := EncodeModule(module)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

func IsAllowedHostFunction(host HostFunction) bool {
	switch host {
	case HostReadStorage, HostWriteStorage, HostEmitInternal, HostInspectMsg, HostBlockContext, HostChargeGas, HostReturn, HostScheduleSelf, HostDeleteStorage:
		return true
	default:
		return false
	}
}

func IsValidEntrypoint(entry Entrypoint) bool {
	switch entry {
	case EntryDeploy, EntryReceiveExternal, EntryReceiveInternal, EntryReceiveBounced, EntryQuery, EntryMigrate:
		return true
	default:
		return false
	}
}

func IsReadOnlyEntrypoint(entry Entrypoint) bool {
	return entry == EntryQuery
}

func ValidateRuntimeContext(ctx RuntimeContext) error {
	if !IsValidEntrypoint(ctx.Entry) {
		return fmt.Errorf("AVM runtime entrypoint %d is invalid", ctx.Entry)
	}
	if ctx.Entry == EntryReceiveBounced && !ctx.Message.Bounced {
		return errors.New("AVM bounced entrypoint requires bounced message")
	}
	if ctx.Entry != EntryReceiveBounced && ctx.Message.Bounced {
		return fmt.Errorf("AVM entrypoint %d does not accept bounced messages", ctx.Entry)
	}
	return nil
}

func IsForbiddenOpcode(op Opcode) bool {
	switch op {
	case OpWallClock, OpRandom, OpFileRead, OpFloatAdd, OpIterMap:
		return true
	default:
		return false
	}
}

func IsAllowedOpcode(op Opcode) bool {
	switch op {
	case OpNop,
		OpPushU64,
		OpPushNull,
		OpReadStorage,
		OpWriteStorage,
		OpDeleteStorage,
		OpAdd,
		OpSub,
		OpMul,
		OpDiv,
		OpMod,
		OpShl,
		OpShr,
		OpBitAnd,
		OpBitOr,
		OpBitXor,
		OpNeg,
		OpBitNot,
		OpEmitInternal,
		OpReturn,
		OpReadMsgOpcode,
		OpReadMsgQueryID,
		OpReadBlock,
		OpChargeGas,
		OpScheduleSelf,
		OpEq,
		OpNe,
		OpLt,
		OpLe,
		OpGt,
		OpGe,
		OpCmp,
		OpAnd,
		OpOr,
		OpNot,
		OpJump,
		OpJumpIfZero,
		OpAbort,
		OpDup,
		OpDrop,
		OpLoadLocal,
		OpStoreLocal,
		OpReadField,
		OpLen,
		OpMapEmpty,
		OpMapGet,
		OpMapSet,
		OpMapHas,
		OpMapDelete,
		OpMapKeys,
		OpMapEntries,
		OpPushString,
		OpPushAddress,
		OpPushBytes,
		OpHash,
		OpReadContractAddress,
		OpReadOriginalBalance,
		OpReadAttachedValue,
		OpReadLogicalTime,
		OpReadBlockTimestamp,
		OpReadCurrentBlockLogicalTime,
		OpCounterfactualAddress,
		OpAutoDeployAddress,
		OpVerifySignature,
		OpReadMsgSender,
		OpReadMsgValue,
		OpReadMsgBody,
		OpIsEmpty,
		OpReadMsgField,
		OpCastCoins,
		OpReadRandom:
		return true
	default:
		return false
	}
}

func RequiredHostFunction(op Opcode) (HostFunction, bool) {
	switch op {
	case OpReadStorage:
		return HostReadStorage, true
	case OpWriteStorage:
		return HostWriteStorage, true
	case OpDeleteStorage:
		return HostDeleteStorage, true
	case OpEmitInternal:
		return HostEmitInternal, true
	case OpReturn:
		return HostReturn, true
	case OpReadMsgOpcode, OpReadMsgQueryID, OpReadMsgSender, OpReadMsgValue, OpReadMsgBody, OpIsEmpty, OpReadMsgField:
		return HostInspectMsg, true
	case OpReadBlock:
		return HostBlockContext, true
	case OpChargeGas:
		return HostChargeGas, true
	case OpScheduleSelf:
		return HostScheduleSelf, true
	default:
		return 0, false
	}
}

func validateInstructionArg(ins Instruction) error {
	const maxUint32 = uint64(^uint32(0))
	switch ins.Op {
	case OpEmitInternal:
		if ins.Arg > maxUint32 {
			return fmt.Errorf("AVM emit opcode argument %d exceeds uint32", ins.Arg)
		}
	case OpReturn:
		if ins.Arg > maxUint32 {
			return fmt.Errorf("AVM return code argument %d exceeds uint32", ins.Arg)
		}
	case OpJump, OpJumpIfZero:
		if ins.Arg > maxUint32 {
			return fmt.Errorf("AVM jump target %d exceeds uint32", ins.Arg)
		}
	case OpLoadLocal, OpStoreLocal:
		if ins.Arg > maxUint32 {
			return fmt.Errorf("AVM local slot %d exceeds uint32", ins.Arg)
		}
	}
	return nil
}

func hostImportSet(imports []HostFunction) map[HostFunction]struct{} {
	out := make(map[HostFunction]struct{}, len(imports))
	for _, host := range imports {
		out[host] = struct{}{}
	}
	return out
}

func EncodeU64(value uint64) []byte {
	var out [8]byte
	binary.BigEndian.PutUint64(out[:], value)
	return out[:]
}

func DecodeU64(bz []byte) uint64 {
	if len(bz) == 8 {
		return binary.BigEndian.Uint64(bz)
	}
	value, _, err := CanonicalDecode(bz)
	if err != nil {
		return 0
	}
	if u, err := value.AsUint64(); err == nil {
		return u
	}
	if i, err := value.AsInt64(); err == nil && i >= 0 {
		return uint64(i)
	}
	return 0
}

func CloneStorage(storage Storage) Storage {
	out := make(Storage, len(storage))
	for key, value := range storage {
		out[key] = append([]byte(nil), value...)
	}
	return out
}

func StorageMemoryBytes(storage Storage) uint64 {
	var total uint64
	for key, value := range storage {
		total += uint64(len(key) + len(value))
	}
	return total
}

func Snapshot(storage Storage) []SnapshotEntry {
	keys := make([]string, 0, len(storage))
	for key := range storage {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]SnapshotEntry, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, SnapshotEntry{Key: key, Value: append([]byte(nil), storage[key]...)})
	}
	return entries
}

func EncodeSnapshot(storage Storage) []byte {
	entries := Snapshot(storage)
	buf := bytes.NewBuffer(nil)
	writeU32(buf, uint32(len(entries)))
	for _, entry := range entries {
		writeU16(buf, uint16(len(entry.Key)))
		buf.WriteString(entry.Key)
		writeU32(buf, uint32(len(entry.Value)))
		buf.Write(entry.Value)
	}
	return buf.Bytes()
}

func DecodeSnapshot(bz []byte) (Storage, error) {
	if len(bz) == 0 {
		return nil, errors.New("AVM snapshot is empty")
	}
	reader := bytes.NewReader(bz)
	count, err := readU32(reader)
	if err != nil {
		return nil, err
	}
	// Bound the declared entry count against the remaining input before
	// pre-sizing the map: the smallest possible entry is 2 (key length) + 1
	// (key) + 4 (value length) = 7 bytes, so a count larger than remaining/7
	// is malformed. This stops a crafted 4-byte count from forcing a huge
	// make(Storage, count) allocation.
	if uint64(count) > uint64(reader.Len())/7 {
		return nil, fmt.Errorf("AVM snapshot entry count %d exceeds input size", count)
	}
	storage := make(Storage, count)
	var previous string
	for i := uint32(0); i < count; i++ {
		keyLen, err := readU16(reader)
		if err != nil {
			return nil, err
		}
		if keyLen == 0 {
			return nil, errors.New("AVM snapshot key must not be empty")
		}
		if keyLen > MaxKeySize {
			return nil, fmt.Errorf("AVM snapshot key must be <= %d bytes", MaxKeySize)
		}
		keyBytes := make([]byte, keyLen)
		if _, err := io.ReadFull(reader, keyBytes); err != nil {
			return nil, err
		}
		key := string(keyBytes)
		if i > 0 && previous >= key {
			return nil, errors.New("AVM snapshot keys must be sorted and unique")
		}
		valueLen, err := readU32(reader)
		if err != nil {
			return nil, err
		}
		// Bound the value length against the remaining input before allocating,
		// so a crafted 4-byte length cannot force a multi-gigabyte make([]byte).
		if uint64(valueLen) > uint64(reader.Len()) {
			return nil, fmt.Errorf("AVM snapshot value length %d exceeds remaining input", valueLen)
		}
		value := make([]byte, valueLen)
		if valueLen > 0 {
			if _, err := io.ReadFull(reader, value); err != nil {
				return nil, err
			}
		}
		storage[key] = value
		previous = key
	}
	if reader.Len() != 0 {
		return nil, errors.New("AVM snapshot has trailing data")
	}
	return storage, nil
}

func StorageRoot(storage Storage) [32]byte {
	return sha256.Sum256(EncodeSnapshot(storage))
}

func BuildExecutionProof(module Module, before Storage, ctx RuntimeContext, exec Execution) (ExecutionProof, error) {
	moduleHash, err := CodeHash(module)
	if err != nil {
		return ExecutionProof{}, err
	}
	return ExecutionProof{
		ModuleHash:    moduleHash,
		BeforeRoot:    StorageRoot(before),
		AfterRoot:     StorageRoot(exec.State),
		ContextHash:   RuntimeContextHash(ctx),
		OutgoingRoot:  OutgoingMessagesRoot(exec.Outgoing),
		TraceHash:     OpcodeTraceHash(exec.ExecutedOpcode),
		GasUsed:       exec.GasUsed,
		ResultCode:    exec.ResultCode,
		StorageWrites: exec.StorageWrites,
		ReturnValue:   runtimeValueToU64(exec.ReturnValue),
	}, nil
}

func ExecutionProofHash(proof ExecutionProof) [32]byte {
	buf := bytes.NewBuffer(nil)
	buf.Write(proof.ModuleHash[:])
	buf.Write(proof.BeforeRoot[:])
	buf.Write(proof.AfterRoot[:])
	buf.Write(proof.ContextHash[:])
	buf.Write(proof.OutgoingRoot[:])
	buf.Write(proof.TraceHash[:])
	writeU64(buf, proof.GasUsed)
	writeU32(buf, proof.ResultCode)
	writeU32(buf, proof.StorageWrites)
	writeU64(buf, proof.ReturnValue)
	return sha256.Sum256(buf.Bytes())
}

func RuntimeContextHash(ctx RuntimeContext) [32]byte {
	buf := bytes.NewBuffer(nil)
	buf.WriteByte(byte(ctx.Entry))
	writeBytes(buf, ctx.ContractAddress)
	writeMessageEnvelope(buf, ctx.Message)
	writeU64(buf, ctx.BlockHeight)
	writeU64(buf, ctx.BlockTimestamp)
	writeU64(buf, ctx.LogicalTime)
	writeU64(buf, ctx.CurrentBlockLogicalTime)
	writeString(buf, ctx.OriginalBalance.String())
	writeString(buf, ctx.AttachedValue.String())
	writeU64(buf, ctx.GasLimit)
	writeBytes(buf, ctx.EmitDestination)
	return sha256.Sum256(buf.Bytes())
}

func OutgoingMessagesRoot(messages []async.MessageEnvelope) [32]byte {
	buf := bytes.NewBuffer(nil)
	writeU32(buf, uint32(len(messages)))
	for _, msg := range messages {
		writeMessageEnvelope(buf, msg)
	}
	return sha256.Sum256(buf.Bytes())
}

func OpcodeTraceHash(trace []Opcode) [32]byte {
	buf := bytes.NewBuffer(nil)
	writeU32(buf, uint32(len(trace)))
	for _, op := range trace {
		buf.WriteByte(byte(op))
	}
	return sha256.Sum256(buf.Bytes())
}

func runtimeValueToU64(v RuntimeValue) uint64 {
	switch v.Tag {
	case TagUint8, TagUint16, TagUint32, TagUint64, TagUint128, TagUint256, TagTimestamp, TagCoins:
		if out, err := v.AsUint64(); err == nil {
			return out
		}
	case TagInt8, TagInt16, TagInt32, TagInt64, TagInt128, TagInt256:
		if out, err := v.AsInt64(); err == nil && out >= 0 {
			return uint64(out)
		}
	case TagBool:
		if v.boolVal {
			return 1
		}
	}
	return 0
}

func pop(stack *[]RuntimeValue) (RuntimeValue, bool) {
	if len(*stack) == 0 {
		return RuntimeValue{}, false
	}
	last := len(*stack) - 1
	value := (*stack)[last]
	*stack = (*stack)[:last]
	return value, true
}

func writeU16(buf *bytes.Buffer, value uint16) {
	var out [2]byte
	binary.BigEndian.PutUint16(out[:], value)
	buf.Write(out[:])
}

func writeU32(buf *bytes.Buffer, value uint32) {
	var out [4]byte
	binary.BigEndian.PutUint32(out[:], value)
	buf.Write(out[:])
}

func writeU64(buf *bytes.Buffer, value uint64) {
	var out [8]byte
	binary.BigEndian.PutUint64(out[:], value)
	buf.Write(out[:])
}

func writeBytes(buf *bytes.Buffer, value []byte) {
	writeU32(buf, uint32(len(value)))
	buf.Write(value)
}

func writeString(buf *bytes.Buffer, value string) {
	writeBytes(buf, []byte(value))
}

func writeMessageEnvelope(buf *bytes.Buffer, msg async.MessageEnvelope) {
	writeBytes(buf, msg.Source)
	writeBytes(buf, msg.Destination)
	writeString(buf, msg.Value.Denom)
	writeString(buf, msg.Value.Amount.String())
	writeU32(buf, msg.Opcode)
	writeU64(buf, msg.QueryID)
	writeBytes(buf, msg.Body)
	writeStateInit(buf, msg.StateInit)
	if msg.Bounce {
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}
	if msg.Bounced {
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}
	writeU64(buf, msg.CreatedLogicalTime)
	writeU64(buf, msg.DeliverAtBlock)
	writeU32(buf, msg.RetryCount)
	writeU32(buf, msg.MaxRetries)
	writeU64(buf, msg.RetryDelayBlocks)
	writeU64(buf, msg.DeadlineBlock)
	writeU64(buf, msg.GasLimit)
	writeString(buf, msg.ForwardFee.Denom)
	writeString(buf, msg.ForwardFee.Amount.String())
	writeU32(buf, msg.Depth)
}

func writeStateInit(buf *bytes.Buffer, stateInit *contracttypes.StateInit) {
	if stateInit == nil {
		buf.WriteByte(0)
		return
	}
	buf.WriteByte(1)
	normalized := stateInit.Normalize()
	writeString(buf, normalized.CodeID)
	writeString(buf, normalized.CodeHash)
	writeBytes(buf, normalized.InitData)
	writeString(buf, normalized.Salt)
	writeBytes(buf, normalized.SaltBytes)
	writeString(buf, normalized.Owner)
	writeString(buf, normalized.InitialStorageRoot)
	writeU64(buf, normalized.InitialBalanceNAET)
	writeU32(buf, uint32(len(normalized.Libraries)))
	for _, dep := range normalized.Libraries {
		writeString(buf, dep.CodeID)
		writeString(buf, dep.CodeHash)
	}
	writeU32(buf, uint32(len(normalized.Capabilities)))
	for _, capability := range normalized.Capabilities {
		writeString(buf, capability)
	}
}

func safeAddU64(left, right uint64) (uint64, bool) {
	if right > ^uint64(0)-left {
		return 0, true
	}
	return left + right, false
}

func readU16(reader *bytes.Reader) (uint16, error) {
	var in [2]byte
	if _, err := io.ReadFull(reader, in[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(in[:]), nil
}

func readU32(reader *bytes.Reader) (uint32, error) {
	var in [4]byte
	if _, err := io.ReadFull(reader, in[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(in[:]), nil
}

func readU64(reader *bytes.Reader) (uint64, error) {
	var in [8]byte
	if _, err := io.ReadFull(reader, in[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(in[:]), nil
}
