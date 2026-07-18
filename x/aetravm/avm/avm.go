package avm

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/sha512"
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
	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/hdevalence/ed25519consensus"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/ripemd160"
	"golang.org/x/crypto/sha3"

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

	// Byte-exact hash opcodes. Unlike OpHash (0x38) — which returns the BLAKE3
	// chunk-tree Merkle root over a TAG+LENGTH-prefixed canonical encoding and is
	// used for the chain's own content-addressing — these hash the RAW operand
	// bytes with a standard algorithm, so a contract can recompute a foreign
	// chain's digest (bridge headers, Bitcoin-style PoW). Each pops one
	// bytes/string/hash value, charges base + 1 gas per input byte BEFORE
	// hashing, and pushes the digest.
	OpSha256    Opcode = 0x47
	OpKeccak256 Opcode = 0x48
	OpRipemd160 Opcode = 0x49
	OpSha512    Opcode = 0x4a
	OpBlake2b   Opcode = 0x4b

	// Byte-manipulation opcodes for building hash preimages (nonce||challenge for
	// PoW, header bytes for a bridge) and parsing them back. Every out-of-range
	// index/length or overflow is a deterministic trap (rollback), never a panic,
	// so all validators reject identical inputs identically.
	OpConcat      Opcode = 0x4c
	OpSlice       Opcode = 0x4d
	OpByteAt      Opcode = 0x4e
	OpToBytesBE   Opcode = 0x4f
	OpFromBytesBE Opcode = 0x50

	// Full-width fused multiply-divide. mulDiv(a,b,c) = floor(a*b/c) and
	// mulDivRoundUp(a,b,c) = ceil(a*b/c), each computing the a*b product in an
	// unbounded big.Int intermediate so a*b may exceed 2^256 as long as the
	// final quotient fits uint256 — the constant-product AMM needs this
	// (reserveIn*reserveOut overflows u64/u256 at real reserves, but the
	// quotient does not). Traps on c==0 and on a quotient that overflows u256.
	OpMulDiv        Opcode = 0x51
	OpMulDivRoundUp Opcode = 0x52

	// secp256k1 signature verification and public-key recovery over a
	// PRE-COMPUTED 32-byte digest (Ethereum/bridge style — the opcode never
	// re-hashes the message). Both enforce canonical low-S so a malleated
	// high-S signature is rejected identically on every validator. Malformed
	// inputs soft-fail (false / empty bytes), matching Ethereum's ecrecover;
	// traps are reserved for gas exhaustion.
	OpVerifySecp256k1 Opcode = 0x53
	OpEcrecover       Opcode = 0x54

	// Integer square root: isqrt(x) = floor(sqrt(x)) over uint256 via big.Int's
	// deterministic integer Newton iteration (no float). The root of a 256-bit
	// value is at most 128-bit, so it never traps on width. The constant-product
	// AMM needs it for sqrt(k) LP minting and for sqrtPrice tick math.
	OpIsqrt Opcode = 0x55

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
	// GasPerOperandUnit is an additional, operand-size-proportional gas
	// charge layered on top of an opcode's flat GasSchedule cost, for
	// opcodes whose real implementation cost scales with the size of the
	// map/tuple/bytes/string value they clone, normalize, or iterate --
	// OpDup, OpLoadLocal, OpStoreLocal, OpReturn (all of which call
	// RuntimeValue.clone()), the OpMap* family (which clones the whole map
	// via AsMap()), and OpReadStorage's whole-state snapshot form. "Unit" is
	// runtimeValueSizeUnits(v): the number of map entries, tuple elements,
	// or bytes/string length. Without this, e.g. OpDup on an N-entry runtime
	// map is deep-cloned (O(N) real work) at the SAME flat price as
	// duplicating a single integer, regardless of N -- see FINDING-001
	// (security-audit/05-findings/FINDING-001-avm-gas-mispricing-dos.md).
	// It is charged in addition to, not instead of, the opcode's flat
	// GasSchedule cost, so small/typical values (a handful of struct
	// fields) incur only a negligible extra charge.
	GasPerOperandUnit uint64
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
			// Byte-exact hashes: flat base here + 1 gas/input byte charged inside
			// the handler (chargeOperandUnits) BEFORE hashing, mirroring OpHash so
			// an oversized preimage cannot be hashed at a flat price (invisible
			// DoS). sha256 is cheapest; the sponge/legacy constructions cost a
			// touch more.
			OpSha256:    80,
			OpKeccak256: 90,
			OpRipemd160: 90,
			OpSha512:    90,
			OpBlake2b:   90,
			// Byte ops. concat/slice/toBytesBE also charge 1 gas per OUTPUT byte
			// inside the handler before allocating; byteAt/fromBytesBE are O(1)
			// (index / <=32-byte parse) so the flat base is the whole cost.
			OpConcat:      8,
			OpSlice:       8,
			OpByteAt:      4,
			OpToBytesBE:   8,
			OpFromBytesBE: 8,
			// Fused multiply-divide: a big.Int multiply + divide (+ one add for
			// the round-up variant) on <=512-bit intermediates. Flat — operands
			// are fixed-width integers (0 operand-size units), so there is no
			// per-byte component.
			OpMulDiv:        12,
			OpMulDivRoundUp: 13,
			// Elliptic-curve crypto over secp256k1. Flat and inputs are
			// fixed-size, so no per-byte term; priced well above the hashes
			// because a curve verify/recover is orders of magnitude more work,
			// and an under-priced crypto op is an invisible DoS. ecrecover costs
			// more than a bare verify (it adds a field inversion + point mul to
			// reconstruct the public key).
			OpVerifySecp256k1: 6_000,
			OpEcrecover:       8_000,
			// Integer square root: a bounded run of big.Int Newton iterations on a
			// 256-bit operand -- well above the fused mul-div (fixed-width divides),
			// far below any curve op. Flat, fixed-size operand, so no per-byte term.
			OpIsqrt: 30,
		},
		// See the GasPerOperandUnit doc comment: 1 extra gas per map entry /
		// tuple element / byte cloned or iterated, on top of the flat
		// opcode cost above. A typical contract value (a handful of struct
		// fields, short strings) is 0 units under runtimeValueSizeUnits, so
		// this adds nothing for the common case; a large runtime map now
		// costs proportionally more the more it is cloned, closing
		// FINDING-001.
		GasPerOperandUnit: 1,
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
	// GasPerOperandUnit must be positive: it is the ONLY thing that prices
	// OpDup/OpLoadLocal/OpStoreLocal/OpReturn/OpMap*/OpReadStorage-snapshot
	// proportionally to the runtime map/tuple/bytes/string they clone or
	// iterate (see FINDING-001). A zero value here would silently reopen
	// that gap by collapsing every operand-size charge to zero while still
	// passing Validate(), so require it explicitly rather than allowing an
	// accidental zero-value Params to disable the mitigation.
	if p.GasPerOperandUnit == 0 {
		return errors.New("gas per operand unit must be positive")
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
		OpSha256,
		OpKeccak256,
		OpRipemd160,
		OpSha512,
		OpBlake2b,
		OpConcat,
		OpSlice,
		OpByteAt,
		OpToBytesBE,
		OpFromBytesBE,
		OpMulDiv,
		OpMulDivRoundUp,
		OpVerifySecp256k1,
		OpEcrecover,
		OpIsqrt,
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
				// Whole-state snapshot: runtimeStorageSnapshotValue sorts
				// the keys, clones the map, AND runs a CanonicalDecode on
				// EVERY stored value (O(total stored bytes)), so charging
				// only by key COUNT undercharges a state that holds a few
				// large values. Bill both the per-entry map overhead and the
				// total decoded bytes. See FINDING-001.
				if !chargeOperandUnits(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, StorageMemoryBytes(state)+uint64(len(state))) {
					return rollback(async.ResultLimitExceeded, nil)
				}
				value = runtimeStorageSnapshotValue(state)
			} else {
				// Single-key read: runtimeValueFromStorage runs a
				// CanonicalDecode over the stored bytes, O(len(value)), and a
				// single key can hold up to MaxMemoryBytes. Charge for the
				// decoded size so a large-valued key is not read at the same
				// flat price as a small one. Sibling of the snapshot branch
				// above / FINDING-001.
				key := string(ins.Data)
				if !chargeOperandUnits(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, uint64(len(state[key]))) {
					return rollback(async.ResultLimitExceeded, nil)
				}
				value = runtimeValueFromStorage(state[key])
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
			// clone() deep-copies the whole value (e.g. every entry of a
			// runtime map), so an oversized top-of-stack value must not be
			// duplicated at the same flat price as duplicating a single
			// integer. See FINDING-001.
			top := stack[len(stack)-1]
			if !chargeOperandGas(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, top) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			stack = append(stack, top.clone())
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
			// Same clone() amplification as OpDup, just reached via a local
			// slot instead of the stack top -- a contract that keeps its
			// large map in a local and repeatedly OpLoadLocal's it would
			// otherwise bypass the OpDup fix entirely. See FINDING-001.
			if !chargeOperandGas(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, locals[slot]) {
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
			// locals[slot] = value.clone() below is the same O(N) clone as
			// OpDup/OpLoadLocal, just on the store side. See FINDING-001.
			if !chargeOperandGas(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, value) {
				return rollback(async.ResultLimitExceeded, nil)
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
			// The emit Arg packs the message opcode in the low 32 bits and the
			// .send(mode) send-mode bitmask in the high 32 bits.
			emitOpcode := ins.Arg & 0xFFFFFFFF
			stmtMode := uint32(ins.Arg >> 32)
			if len(stack) > 0 {
				msgValue, ok := pop(&stack)
				if !ok {
					return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on emit internal"))
				}
				if msgValue.Tag == TagMap {
					outgoing, err = runtimeMessageEnvelopeFromValue(msgValue, ctx, emitOpcode)
				} else {
					outgoing, err = runtimeLegacyMessageEnvelopeFromValue(msgValue, ctx, emitOpcode, ins.Data)
				}
			} else {
				outgoing, err = runtimeLegacyMessageEnvelopeFromValue(ValueNull(), ctx, emitOpcode, ins.Data)
			}
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			// A message-map `mode:` field takes precedence; otherwise apply the
			// .send(mode) bitmask.
			if outgoing.Mode == 0 {
				outgoing.Mode = stmtMode
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
			// runtimeFieldValue's TagMap branch does the exact same
			// AsMap() full-map clone as OpMapGet before looking up a
			// single named field, and its TagBytes/TagString branches
			// parse the whole segment-encoded value to find the field --
			// both O(N) in the source's size. See FINDING-001.
			if !chargeOperandGas(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, source) {
				return rollback(async.ResultLimitExceeded, nil)
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
			// runtimeHashValue does O(value-size) work (chunk-tree build or
			// CanonicalEncode, then sha256), so charge for the operand size
			// rather than the flat OpHash price. See FINDING-001.
			if !chargeOperandGas(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, value) {
				return rollback(async.ResultLimitExceeded, nil)
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
		case OpSha256, OpKeccak256, OpRipemd160, OpSha512, OpBlake2b:
			value, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on byte hash"))
			}
			// Charge base (flat, already added at the loop top) + 1 gas per input
			// byte BEFORE hashing, so an oversized preimage cannot be hashed at a
			// flat price. runtimeValueSizeUnits(value) is the raw byte length for
			// bytes/string and 0 for the fixed 32-byte hash tag (covered by base).
			if !chargeOperandGas(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, value) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			raw, err := runtimeRawBytes(value)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			result, err := runtimeByteHash(ins.Op, raw)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, result)
		case OpConcat:
			// concat(a, b): b is on top (pushed last). TRAP if the result would
			// exceed MaxBytesLength; charge 1 gas per output byte before the copy.
			right, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on concat"))
			}
			left, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on concat"))
			}
			la, err := runtimeRawBytes(left)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			lb, err := runtimeRawBytes(right)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			total := uint64(len(la)) + uint64(len(lb))
			if total > uint64(MaxBytesLength) {
				return rollback(async.ResultExecutionFailed, errors.New("AVM concat result exceeds max bytes length"))
			}
			if !chargeOperandUnits(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, total) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			out := make([]byte, 0, total)
			out = append(out, la...)
			out = append(out, lb...)
			stack = append(stack, ValueBytes(out))
		case OpSlice:
			// slice(b, start, len): len on top, then start, then b. TRAP if the
			// window runs past the end; charge 1 gas per output byte.
			lenVal, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on slice length"))
			}
			startVal, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on slice start"))
			}
			bVal, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on slice bytes"))
			}
			data, err := runtimeRawBytes(bVal)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			start, err := runtimeByteIndex(startVal, "slice start")
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			length, err := runtimeByteIndex(lenVal, "slice length")
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			// start and length are each <= MaxUint32, so the sum cannot overflow
			// uint64; the single bounds check below is exact.
			end := uint64(start) + uint64(length)
			if end > uint64(len(data)) {
				return rollback(async.ResultExecutionFailed, errors.New("AVM slice out of range"))
			}
			if !chargeOperandUnits(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, uint64(length)) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			out := make([]byte, length)
			copy(out, data[start:end])
			stack = append(stack, ValueBytes(out))
		case OpByteAt:
			// byteAt(b, i): i on top. O(1) — flat base only. TRAP if out of range.
			iVal, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on byteAt index"))
			}
			bVal, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on byteAt bytes"))
			}
			data, err := runtimeRawBytes(bVal)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			i, err := runtimeByteIndex(iVal, "byteAt index")
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			if uint64(i) >= uint64(len(data)) {
				return rollback(async.ResultExecutionFailed, errors.New("AVM byteAt index out of range"))
			}
			stack = append(stack, ValueUint8(data[i]))
		case OpToBytesBE:
			// toBytesBE(v, n): n on top. Big-endian, zero-padded, fixed width n.
			// TRAP if v is negative, n exceeds MaxBytesLength, or v does not fit in
			// n bytes. Charge 1 gas per output byte before allocating.
			nVal, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on toBytesBE length"))
			}
			vVal, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on toBytesBE value"))
			}
			num, err := runtimeNumeric(vVal)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			if num.Sign() < 0 {
				return rollback(async.ResultExecutionFailed, errors.New("AVM toBytesBE requires a non-negative value"))
			}
			n, err := runtimeByteIndex(nVal, "toBytesBE length")
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			if uint64(n) > uint64(MaxBytesLength) {
				return rollback(async.ResultExecutionFailed, errors.New("AVM toBytesBE length exceeds max bytes length"))
			}
			if num.BitLen() > int(n)*8 {
				return rollback(async.ResultExecutionFailed, errors.New("AVM toBytesBE value does not fit in n bytes"))
			}
			if !chargeOperandUnits(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, uint64(n)) {
				return rollback(async.ResultLimitExceeded, nil)
			}
			buf := make([]byte, n)
			num.FillBytes(buf)
			stack = append(stack, ValueBytes(buf))
		case OpFromBytesBE:
			// fromBytesBE(b): big-endian decode into uint256 (widest lossless
			// target). TRAP if the input exceeds 32 bytes. O(<=32) — flat base.
			bVal, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on fromBytesBE"))
			}
			data, err := runtimeRawBytes(bVal)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			if len(data) > 32 {
				return rollback(async.ResultExecutionFailed, errors.New("AVM fromBytesBE requires <= 32 bytes"))
			}
			result, err := runtimeFromBigIntChecked(TagUint256, new(big.Int).SetBytes(data))
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, result)
		case OpMulDiv, OpMulDivRoundUp:
			// mulDiv(a, b, c): c on top (pushed last), then b, then a. Computes
			// floor(a*b/c) — or ceil for the round-up variant — with the a*b
			// product held in an unbounded big.Int so it never overflows; only
			// the narrowed uint256 result is width-checked. TRAP on c==0 or a
			// result that does not fit uint256.
			cVal, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on mulDiv divisor"))
			}
			bVal, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on mulDiv multiplicand"))
			}
			aVal, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on mulDiv multiplier"))
			}
			result, err := runtimeMulDiv(aVal, bVal, cVal, ins.Op == OpMulDivRoundUp)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, result)
		case OpIsqrt:
			// isqrt(x): floor(sqrt(x)) over uint256. Pops one operand and pushes
			// its integer square root. big.Int.Sqrt is deterministic and, on an
			// unsigned operand, never negative -- so the checked conversion never
			// traps on width.
			xVal, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on isqrt operand"))
			}
			result, err := runtimeIsqrt(xVal)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, result)
		case OpVerifySecp256k1:
			// verifySecp256k1(msgHash, sig, pubkey): pubkey on top (pushed last),
			// then sig, then msgHash. Verifies a 64-byte compact R‖S signature
			// against a 33-byte compressed key over a 32-byte digest, enforcing
			// canonical low-S. Malformed input soft-fails to false.
			pubVal, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on secp256k1 public key"))
			}
			sigVal, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on secp256k1 signature"))
			}
			hashVal, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on secp256k1 message hash"))
			}
			verified, err := runtimeVerifySecp256k1(hashVal, sigVal, pubVal)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, ValueBool(verified))
		case OpEcrecover:
			// ecrecover(msgHash, sig): sig on top (pushed last), then msgHash.
			// Recovers the 64-byte uncompressed public-key body (X‖Y) from a
			// 65-byte Ethereum-layout signature (R‖S‖v) over a 32-byte digest,
			// enforcing canonical low-S. Malformed input or a failed recovery
			// soft-fails to empty bytes.
			sigVal, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on ecrecover signature"))
			}
			hashVal, ok := pop(&stack)
			if !ok {
				return rollback(async.ResultExecutionFailed, errors.New("AVM stack underflow on ecrecover message hash"))
			}
			recovered, err := runtimeEcrecover(hashVal, sigVal)
			if err != nil {
				return rollback(async.ResultExecutionFailed, err)
			}
			stack = append(stack, ValueBytes(recovered))
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
			// AsMap() deep-clones the ENTIRE map before the lookup even
			// though the lookup itself only touches O(log N) entries, so
			// the clone must be priced by map size. See FINDING-001.
			if !chargeOperandGas(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, m) {
				return rollback(async.ResultLimitExceeded, nil)
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
			// AsMap() clones the whole map, runtimeMapSet below rebuilds
			// another full copy, and ValueMap() normalizes (sorts + clones)
			// a third time -- all O(N) in the map's current size. See
			// FINDING-001.
			if !chargeOperandGas(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, m) {
				return rollback(async.ResultLimitExceeded, nil)
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
			// Same AsMap() full-map clone as OpMapGet. See FINDING-001.
			if !chargeOperandGas(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, m) {
				return rollback(async.ResultLimitExceeded, nil)
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
			// Same AsMap()+rebuild+renormalize triple O(N) pass as
			// OpMapSet. See FINDING-001.
			if !chargeOperandGas(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, m) {
				return rollback(async.ResultLimitExceeded, nil)
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
			// AsMap() clones the whole map even though the result is
			// truncated to `limit` keys afterward. See FINDING-001.
			if !chargeOperandGas(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, m) {
				return rollback(async.ResultLimitExceeded, nil)
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
			// Same AsMap() full-map clone as OpMapKeys. See FINDING-001.
			if !chargeOperandGas(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, m) {
				return rollback(async.ResultLimitExceeded, nil)
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
			// The final clone() below is the same O(N) operation as OpDup;
			// price it the same way before committing to a result. See
			// FINDING-001.
			if len(stack) > 0 {
				if !chargeOperandGas(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, stack[len(stack)-1]) {
					return rollback(async.ResultLimitExceeded, nil)
				}
			}
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
	// Same OpReturn-path clone() charge for the implicit return when
	// execution falls off the end of the code. See FINDING-001.
	if len(stack) > 0 {
		if !chargeOperandGas(&exec.GasUsed, gasLimit, r.params.GasPerOperandUnit, stack[len(stack)-1]) {
			return rollback(async.ResultLimitExceeded, nil)
		}
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

// runtimeMulDiv computes floor(a*b/c) — or ceil(a*b/c) when roundUp is set — as
// a uint256. The a*b product is formed in an unbounded big.Int, so it may exceed
// 2^256 (as the constant-product AMM's reserveIn*reserveOut does at real
// reserves); only the final quotient is width-checked. Traps on a zero divisor
// (reusing the "AVM division by zero" error so it is indistinguishable from an
// ordinary integer divide-by-zero) and on a quotient that overflows uint256
// (via runtimeFromBigIntChecked/enforceIntWidth, e.g. c==1 with a*b >= 2^256).
func runtimeMulDiv(a, b, c RuntimeValue, roundUp bool) (RuntimeValue, error) {
	ai, err := runtimeNumeric(a)
	if err != nil {
		return RuntimeValue{}, err
	}
	bi, err := runtimeNumeric(b)
	if err != nil {
		return RuntimeValue{}, err
	}
	ci, err := runtimeNumeric(c)
	if err != nil {
		return RuntimeValue{}, err
	}
	if ci.Sign() == 0 {
		return RuntimeValue{}, errors.New("AVM division by zero")
	}
	prod := new(big.Int).Mul(ai, bi)
	quo := new(big.Int)
	rem := new(big.Int)
	quo.QuoRem(prod, ci, rem)
	if roundUp && rem.Sign() != 0 {
		quo.Add(quo, big.NewInt(1))
	}
	return runtimeFromBigIntChecked(TagUint256, quo)
}

func runtimeIsqrt(a RuntimeValue) (RuntimeValue, error) {
	ai, err := runtimeNumeric(a)
	if err != nil {
		return RuntimeValue{}, err
	}
	// Operands are unsigned (uint256), so Sqrt is always defined; floor(sqrt(ai))
	// is at most 128-bit and therefore always fits the checked uint256 result.
	root := new(big.Int).Sqrt(ai)
	return runtimeFromBigIntChecked(TagUint256, root)
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
	case "timestamp":
		v, err := parseJSONBigInt(raw)
		if err != nil {
			return RuntimeValue{}, decodeErr(err)
		}
		return ValueUint64(v.Uint64()), nil
	default:
		// Integer types: both the canonical long form the compiler actually
		// emits (uint64, int256, …) and the short form (u64, i256, …), for
		// every width the language surface declares (2/4/8/16/32/64/128/256).
		// JSON numbers lose precision above 2^53, so the value may also
		// arrive as a decimal string — parseJSONBigInt accepts both.
		if tag, bits, signed, ok := integerKindTag(kind); ok {
			v, err := parseJSONBigInt(raw)
			if err != nil {
				return RuntimeValue{}, decodeErr(err)
			}
			if !signed && v.Sign() < 0 {
				return RuntimeValue{}, decodeErr(fmt.Errorf("negative value %s for unsigned type %s", v, kind))
			}
			if bits > 0 && bits < 256 {
				limit := new(big.Int).Lsh(big.NewInt(1), uint(bits))
				if signed {
					half := new(big.Int).Rsh(limit, 1)
					negHalf := new(big.Int).Neg(half)
					if v.Cmp(negHalf) < 0 || v.Cmp(half) >= 0 {
						return RuntimeValue{}, decodeErr(fmt.Errorf("value %s out of range for %s", v, kind))
					}
				} else if v.Cmp(limit) >= 0 {
					return RuntimeValue{}, decodeErr(fmt.Errorf("value %s out of range for %s", v, kind))
				}
			}
			return RuntimeValue{Tag: tag, intVal: v}, nil
		}
	}
	switch kind {
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

// integerKindKey pairs an integer bit width with signedness for the lookup
// table below.
type integerKindKey struct {
	bits   int
	signed bool
}

// integerKindTags maps every integer type spelling the language surface uses
// — both the canonical long form (uint64, int256, …) and the short form
// (u64, i256, …) — to its runtime tag, bit width, and signedness. Built once
// from the canonical (bits, signed) -> tag table so the two can never drift.
var integerKindTags = buildIntegerKindTags()

func buildIntegerKindTags() map[string]struct {
	tag    ValueTag
	bits   int
	signed bool
} {
	byWidth := map[integerKindKey]ValueTag{
		{2, false}: TagUint8, {4, false}: TagUint8, // sub-byte widths still occupy a Uint8 slot; range-checked separately
		{8, false}: TagUint8, {16, false}: TagUint16, {32, false}: TagUint32,
		{64, false}: TagUint64, {128, false}: TagUint128, {256, false}: TagUint256,
		{2, true}: TagInt8, {4, true}: TagInt8,
		{8, true}: TagInt8, {16, true}: TagInt16, {32, true}: TagInt32,
		{64, true}: TagInt64, {128, true}: TagInt128, {256, true}: TagInt256,
	}
	out := map[string]struct {
		tag    ValueTag
		bits   int
		signed bool
	}{}
	for _, bits := range []int{2, 4, 8, 16, 32, 64, 128, 256} {
		for _, signed := range []bool{false, true} {
			prefix := "u"
			longPrefix := "uint"
			if signed {
				prefix = "i"
				longPrefix = "int"
			}
			entry := struct {
				tag    ValueTag
				bits   int
				signed bool
			}{tag: byWidth[integerKindKey{bits, signed}], bits: bits, signed: signed}
			out[fmt.Sprintf("%s%d", prefix, bits)] = entry
			out[fmt.Sprintf("%s%d", longPrefix, bits)] = entry
		}
	}
	return out
}

// integerKindTag resolves a type-name string (any spelling in
// integerKindTags) to its runtime tag, bit width, and signedness.
func integerKindTag(kind string) (tag ValueTag, bits int, signed bool, ok bool) {
	entry, found := integerKindTags[kind]
	if !found {
		return 0, 0, false, false
	}
	return entry.tag, entry.bits, entry.signed, true
}

// parseJSONBigInt decodes a JSON number or a JSON string of decimal digits
// into a big.Int. JSON numbers lose precision above 2^53, so wide integer
// types (u128/u256/i128/i256) are expected to travel as decimal strings —
// both spellings are accepted here.
func parseJSONBigInt(raw json.RawMessage) (*big.Int, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		v, ok := new(big.Int).SetString(strings.TrimSpace(s), 10)
		if !ok {
			return nil, fmt.Errorf("invalid decimal integer %q", s)
		}
		return v, nil
	}
	text := strings.TrimSpace(string(raw))
	v, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return nil, fmt.Errorf("invalid integer %q", text)
	}
	return v, nil
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

// runtimeRawBytes returns the EXACT bytes a byte-exact hash or byte op operates
// on: the raw payload of a bytes/string value, or the 32 bytes of a hash value.
// Unlike runtimeHashValue / CanonicalEncode it never prepends a tag or a length
// prefix, so a digest computed over these bytes matches what a foreign chain
// computes over the same bytes (the whole point of the byte-exact opcodes). Any
// other tag is a deterministic type error, i.e. a trap on every validator.
func runtimeRawBytes(v RuntimeValue) ([]byte, error) {
	switch v.Tag {
	case TagBytes, TagString:
		return v.AsBytes()
	case TagHash:
		return append([]byte(nil), v.hashVal[:]...), nil
	default:
		return nil, fmt.Errorf("AVM byte operation requires bytes/string/hash, got %s", v.Tag)
	}
}

// runtimeByteHash applies one standard, byte-exact, deterministic hash to raw
// input bytes. 32-byte digests (sha256/keccak256/blake2b) return as TagHash;
// ripemd160 (20B) and sha512 (64B) return as TagBytes because there is no
// 20-/64-byte hash tag. Keccak256 MUST use the legacy (pre-NIST) Keccak padding
// so a bridge contract's digest matches Ethereum — sha3.New256 (FIPS SHA3-256)
// uses different padding and would silently mismatch.
func runtimeByteHash(op Opcode, data []byte) (RuntimeValue, error) {
	switch op {
	case OpSha256:
		sum := sha256.Sum256(data)
		return ValueHash(sum), nil
	case OpKeccak256:
		h := sha3.NewLegacyKeccak256()
		h.Write(data)
		var out [32]byte
		copy(out[:], h.Sum(nil))
		return ValueHash(out), nil
	case OpBlake2b:
		sum := blake2b.Sum256(data)
		return ValueHash(sum), nil
	case OpRipemd160:
		h := ripemd160.New()
		h.Write(data)
		return ValueBytes(h.Sum(nil)), nil
	case OpSha512:
		sum := sha512.Sum512(data)
		return ValueBytes(sum[:]), nil
	default:
		return RuntimeValue{}, fmt.Errorf("AVM opcode 0x%02x is not a byte hash", byte(op))
	}
}

// runtimeByteIndex extracts a non-negative index/length that must fit in a
// uint32 from a runtime numeric value, trapping otherwise. Bounding it to uint32
// keeps start+length additions overflow-free in uint64 and matches the
// uint32-typed slice/index builtins in the language surface.
func runtimeByteIndex(v RuntimeValue, what string) (uint32, error) {
	n, err := runtimeNumeric(v)
	if err != nil {
		return 0, err
	}
	if n.Sign() < 0 || !n.IsUint64() || n.Uint64() > 0xFFFFFFFF {
		return 0, fmt.Errorf("AVM %s must fit uint32", what)
	}
	return uint32(n.Uint64()), nil
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
	// Verify under ZIP-215 (ed25519consensus) rather than crypto/ed25519. ZIP-215
	// fixes ONE canonical accept/reject for every (pubkey, sig, message) triple,
	// removing the cofactor / non-canonical-encoding edge cases where stdlib's
	// answer can differ from other implementations. That guarantees every
	// validator — and a foreign chain verifying the same triple — agrees, which
	// is exactly what cross-chain signature checks require. Same opcode, gas, and
	// 32/64-byte length contract as before; only the verify routine changed.
	return ed25519consensus.Verify(ed25519.PublicKey(pub), message, sig), nil
}

// secp256k1CompactSigSize is the byte length of a compact R‖S ECDSA signature
// (two 32-byte scalars), the form verifySecp256k1 accepts.
const secp256k1CompactSigSize = 64

// secp256k1RecoverableSigSize is the byte length of an Ethereum-layout
// recoverable signature: 32-byte R, 32-byte S, then a 1-byte recovery id v.
const secp256k1RecoverableSigSize = 65

// runtimeSecpDigest extracts the fixed 32-byte message digest a secp256k1 op
// operates on. A hash value is always 32 bytes; a bytes/string value must be
// exactly 32 bytes (the caller passes a pre-computed digest). A wrong length
// soft-fails (ok=false) so the caller returns the canonical false/empty result
// rather than trapping; only a non-byte tag is a type error (trap).
func runtimeSecpDigest(v RuntimeValue) (digest []byte, ok bool, err error) {
	raw, err := runtimeRawBytes(v)
	if err != nil {
		return nil, false, err
	}
	if len(raw) != 32 {
		return nil, false, nil
	}
	return raw, true, nil
}

// runtimeSecpLowSSignature parses a 64-byte compact R‖S signature into a decred
// ecdsa.Signature, enforcing canonical encoding: R and S must each be below the
// group order N, and S must be in the lower half (<= N/2). Rejecting high-S
// makes signature malleability a deterministic reject on every validator (the
// same rule cosmos's secp256k1.VerifySignature applies). ok=false on any
// non-canonical or malformed component; the caller soft-fails.
func runtimeSecpLowSSignature(sig []byte) (parsed *ecdsa.Signature, ok bool) {
	if len(sig) != secp256k1CompactSigSize {
		return nil, false
	}
	var r, s secp256k1.ModNScalar
	if overflow := r.SetByteSlice(sig[:32]); overflow {
		return nil, false
	}
	if overflow := s.SetByteSlice(sig[32:64]); overflow {
		return nil, false
	}
	if s.IsOverHalfOrder() {
		return nil, false
	}
	return ecdsa.NewSignature(&r, &s), true
}

// runtimeVerifySecp256k1 verifies a 64-byte compact R‖S signature over a 32-byte
// digest against a 33-byte compressed SEC1 public key. Uncompressed/hybrid keys
// are rejected up front (the 33-byte length gate) so acceptance is deterministic
// and a bridge cannot smuggle a differently-encoded key past one validator. Any
// malformed input (wrong lengths, non-canonical signature, high-S, unparsable
// key) returns false rather than trapping, mirroring Ethereum's ecrecover
// soft-fail; traps are reserved for gas exhaustion.
func runtimeVerifySecp256k1(msgHash, signature, publicKey RuntimeValue) (bool, error) {
	digest, ok, err := runtimeSecpDigest(msgHash)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	sig, err := runtimeRawBytes(signature)
	if err != nil {
		return false, err
	}
	pub, err := runtimeRawBytes(publicKey)
	if err != nil {
		return false, err
	}
	if len(pub) != secp256k1.PubKeyBytesLenCompressed {
		return false, nil
	}
	parsedSig, ok := runtimeSecpLowSSignature(sig)
	if !ok {
		return false, nil
	}
	parsedPub, err := secp256k1.ParsePubKey(pub)
	if err != nil {
		return false, nil
	}
	return parsedSig.Verify(digest, parsedPub), nil
}

// runtimeEcrecover recovers the signer's public key from a 65-byte
// Ethereum-layout recoverable signature (R‖S‖v) over a 32-byte digest and
// returns the 64-byte uncompressed public-key body X‖Y (so a contract can
// derive an Ethereum address via keccak256(pub) then slice(_, 12, 20)). The v
// byte is accepted as {0,1} or {27,28} and normalized; low-S is enforced so
// recovery is canonical. Any malformed input or failed recovery returns empty
// bytes (soft-fail), never a trap, matching Ethereum's ecrecover.
func runtimeEcrecover(msgHash, signature RuntimeValue) ([]byte, error) {
	digest, ok, err := runtimeSecpDigest(msgHash)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []byte{}, nil
	}
	sig, err := runtimeRawBytes(signature)
	if err != nil {
		return nil, err
	}
	if len(sig) != secp256k1RecoverableSigSize {
		return []byte{}, nil
	}
	// Enforce canonical low-S on R‖S before recovery so a malleated signature is
	// rejected identically everywhere (RecoverCompact itself does not reject
	// high-S).
	if _, ok := runtimeSecpLowSSignature(sig[:64]); !ok {
		return []byte{}, nil
	}
	// Normalize the Ethereum recovery id (v last, {0,1} or {27,28}) to decred's
	// compact layout (recovery code FIRST, 27 + recid, no compressed bit because
	// we return the uncompressed body).
	v := sig[64]
	if v >= 27 {
		v -= 27
	}
	if v > 1 {
		return []byte{}, nil
	}
	compact := make([]byte, secp256k1RecoverableSigSize)
	compact[0] = 27 + v
	copy(compact[1:], sig[:64])
	pub, _, err := ecdsa.RecoverCompact(compact, digest)
	if err != nil {
		return []byte{}, nil
	}
	// SerializeUncompressed is 0x04 ‖ X(32) ‖ Y(32); drop the 0x04 tag so the
	// result is the bare 64-byte X‖Y body.
	uncompressed := pub.SerializeUncompressed()
	return append([]byte(nil), uncompressed[1:]...), nil
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
	// Optional combined send-mode bitmask (SEND_* flags OR'd/added together).
	var mode uint32
	if modeValue, ok, err := get("mode"); err != nil {
		return async.MessageEnvelope{}, err
	} else if ok {
		modeAmount, err := modeValue.AsUint64()
		if err != nil {
			return async.MessageEnvelope{}, err
		}
		if modeAmount > uint64(^uint32(0)) {
			return async.MessageEnvelope{}, fmt.Errorf("AVM emit internal mode %d exceeds uint32", modeAmount)
		}
		mode = uint32(modeAmount)
	}
	// Optional user-facing text memo (textComment). At most one; bounded.
	var comment string
	if commentValue, ok, err := get("textComment"); err != nil {
		return async.MessageEnvelope{}, err
	} else if ok {
		comment, err = commentValue.AsString()
		if err != nil {
			return async.MessageEnvelope{}, fmt.Errorf("AVM emit internal textComment must be a string: %w", err)
		}
		if len(comment) > async.MaxCommentBytes {
			return async.MessageEnvelope{}, fmt.Errorf("AVM emit internal textComment exceeds %d bytes", async.MaxCommentBytes)
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
		Mode:        mode,
		Comment:     comment,
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
		OpReadRandom,
		OpSha256,
		OpKeccak256,
		OpRipemd160,
		OpSha512,
		OpBlake2b,
		OpConcat,
		OpSlice,
		OpByteAt,
		OpToBytesBE,
		OpFromBytesBE,
		OpMulDiv,
		OpMulDivRoundUp,
		OpVerifySecp256k1,
		OpEcrecover,
		OpIsqrt:
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
		// Low 32 bits = message opcode, high 32 bits = send-mode bitmask.
		if ins.Arg&0xFFFFFFFF > maxUint32 {
			return fmt.Errorf("AVM emit opcode argument %d exceeds uint32", ins.Arg&0xFFFFFFFF)
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

func safeMulU64(left, right uint64) (uint64, bool) {
	if left == 0 || right == 0 {
		return 0, false
	}
	result := left * right
	if result/left != right {
		return 0, true
	}
	return result, false
}

// chargeOperandUnits adds an operand-size-proportional charge (rate * units)
// on top of whatever gas has already been charged, honoring the same
// overflow and gas-limit rules as the interpreter's per-instruction flat
// charge. It reports false (caller must roll back with
// ResultLimitExceeded) when the charge would overflow or exceed gasLimit,
// exactly like the flat per-opcode charge at the top of the interpreter
// loop. See the Params.GasPerOperandUnit doc comment / FINDING-001.
func chargeOperandUnits(gasUsed *uint64, gasLimit, rate, units uint64) bool {
	if rate == 0 || units == 0 {
		return true
	}
	extra, overflow := safeMulU64(rate, units)
	if overflow {
		return false
	}
	next, overflow := safeAddU64(*gasUsed, extra)
	if overflow || next > gasLimit {
		return false
	}
	*gasUsed = next
	return true
}

// chargeOperandGas charges chargeOperandUnits for the O(N) work that would
// be performed by cloning, normalizing, or iterating v (a map, tuple, bytes,
// or string). Called BEFORE the actual clone/AsMap/iteration happens, so an
// oversized operand is billed (or the execution is aborted for exceeding the
// gas limit) before, not after, the expensive work runs.
func chargeOperandGas(gasUsed *uint64, gasLimit, rate uint64, v RuntimeValue) bool {
	return chargeOperandUnits(gasUsed, gasLimit, rate, runtimeValueSizeUnits(v))
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
