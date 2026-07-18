package compiler

import "github.com/sovereign-l1/l1/x/aetravm/avm"

type IRProgram struct {
	Contract         string                  `json:"contract"`
	Package          string                  `json:"package,omitempty"`
	Entries          []IREntry               `json:"entries"`
	TraceCommitments map[string]string       `json:"trace_commitments,omitempty"`
	Dependencies     []ResolvedDependency    `json:"dependencies,omitempty"`
	LoweringRules    []StatementLoweringRule `json:"lowering_rules,omitempty"`
}

type IREntry struct {
	Name       string         `json:"name"`
	Kind       string         `json:"kind"`
	Entrypoint avm.Entrypoint `json:"entrypoint"`
	Selector   uint32         `json:"selector"`
	Statements []IRStmt       `json:"statements"`
	Pos        Position       `json:"pos"`
}

type IRStmtKind string

const (
	IRStmtTrace        IRStmtKind = "trace_commitment"
	IRStmtLetConst     IRStmtKind = "let_const"
	IRStmtStoreState   IRStmtKind = "store_state"
	IRStmtDeleteState  IRStmtKind = "delete_state"
	IRStmtEmitInternal IRStmtKind = "emit_internal"
	IRStmtScheduleSelf IRStmtKind = "schedule_self"
	IRStmtPushU64      IRStmtKind = "push_u64"
	IRStmtDup          IRStmtKind = "dup"
	IRStmtDrop         IRStmtKind = "drop"
	IRStmtSub          IRStmtKind = "sub"
	IRStmtStoreLocal   IRStmtKind = "store_local"
	IRStmtLabel        IRStmtKind = "label"
	IRStmtJump         IRStmtKind = "jump"
	IRStmtJumpIfZero   IRStmtKind = "jump_if_zero"
	IRStmtAbort        IRStmtKind = "abort"
	IRStmtReturn       IRStmtKind = "return"
)

type IRStmt struct {
	Kind     IRStmtKind `json:"kind"`
	Name     string     `json:"name,omitempty"`
	Key      string     `json:"key,omitempty"`
	Slot     uint32     `json:"slot,omitempty"`
	Opcode   uint32     `json:"opcode,omitempty"`
	Arg      uint64     `json:"arg,omitempty"`
	Data     []byte     `json:"data,omitempty"`
	Target   string     `json:"target,omitempty"`
	Expr     *IRExpr    `json:"expr,omitempty"`
	Position Position   `json:"position"`
}

type IRExprKind string

const (
	IRExprConstU64                IRExprKind = "const_u64"
	IRExprConstString             IRExprKind = "const_string"
	IRExprConstAddress            IRExprKind = "const_address"
	IRExprConstBytes              IRExprKind = "const_bytes"
	IRExprLocalLoad               IRExprKind = "local_load"
	IRExprField                   IRExprKind = "field"
	IRExprNull                    IRExprKind = "null"
	IRExprStateRead               IRExprKind = "state_read"
	IRExprStruct                  IRExprKind = "struct"
	IRExprAdd                     IRExprKind = "add"
	IRExprSub                     IRExprKind = "sub"
	IRExprMul                     IRExprKind = "mul"
	IRExprDiv                     IRExprKind = "div"
	IRExprMod                     IRExprKind = "mod"
	IRExprShl                     IRExprKind = "shl"
	IRExprShr                     IRExprKind = "shr"
	IRExprBitAnd                  IRExprKind = "bit_and"
	IRExprBitOr                   IRExprKind = "bit_or"
	IRExprBitXor                  IRExprKind = "bit_xor"
	IRExprEq                      IRExprKind = "eq"
	IRExprNe                      IRExprKind = "ne"
	IRExprLt                      IRExprKind = "lt"
	IRExprLe                      IRExprKind = "le"
	IRExprGt                      IRExprKind = "gt"
	IRExprGe                      IRExprKind = "ge"
	IRExprCompare                 IRExprKind = "compare"
	IRExprAnd                     IRExprKind = "and"
	IRExprOr                      IRExprKind = "or"
	IRExprNot                     IRExprKind = "not"
	IRExprNeg                     IRExprKind = "neg"
	IRExprBitNot                  IRExprKind = "bit_not"
	IRExprLen                     IRExprKind = "len"
	IRExprTernary                 IRExprKind = "ternary"
	IRExprCoalesce                IRExprKind = "coalesce"
	IRExprMapEmpty                IRExprKind = "map_empty"
	IRExprMapGet                  IRExprKind = "map_get"
	IRExprMapSet                  IRExprKind = "map_set"
	IRExprMapHas                  IRExprKind = "map_has"
	IRExprMapDelete               IRExprKind = "map_delete"
	IRExprMapKeys                 IRExprKind = "map_keys"
	IRExprMapEntries              IRExprKind = "map_entries"
	IRExprMsgOpcode               IRExprKind = "message_opcode"
	IRExprMsgQueryID              IRExprKind = "message_query_id"
	IRExprMsgSender               IRExprKind = "message_sender"
	IRExprMsgValue                IRExprKind = "message_value"
	IRExprMsgBody                 IRExprKind = "message_body"
	IRExprMsgField                IRExprKind = "message_field"
	IRExprIsEmpty                 IRExprKind = "is_empty"
	IRExprBlockHeight             IRExprKind = "block_height"
	IRExprCurrentBlockLogicalTime IRExprKind = "current_block_logical_time"
	IRExprHash                    IRExprKind = "hash"
	IRExprBitsHash                IRExprKind = "bits_hash"
	IRExprContractAddress         IRExprKind = "contract_address"
	IRExprOriginalBalance         IRExprKind = "original_balance"
	IRExprAttachedValue           IRExprKind = "attached_value"
	IRExprLogicalTime             IRExprKind = "logical_time"
	IRExprBlockTimestamp          IRExprKind = "block_timestamp"
	IRExprRandom                  IRExprKind = "random"
	IRExprCounterfactualAddress   IRExprKind = "counterfactual_address"
	IRExprAutoDeployAddress       IRExprKind = "auto_deploy_address"
	IRExprSignatureVerify         IRExprKind = "signature_verify"
	// IRExprCoinsCast retags a lowered integer constant as canonical `coins`
	// (see compile.go coerceStructLiteralFieldTypes) so a coins-typed struct
	// field initialized with a bare literal encodes identically to a coins
	// value sourced from a message or storage field.
	IRExprCoinsCast IRExprKind = "coins_cast"

	// Byte-exact hashes over raw operand bytes (distinct from IRExprHash, which
	// is the BLAKE3 chunk-tree root over a tagged canonical encoding). Single
	// operand in Left. sha256/keccak256/blake2b yield hash32; ripemd160 (20B)
	// and sha512 (64B) yield bytes.
	IRExprSha256    IRExprKind = "sha256"
	IRExprKeccak256 IRExprKind = "keccak256"
	IRExprRipemd160 IRExprKind = "ripemd160"
	IRExprSha512    IRExprKind = "sha512"
	IRExprBlake2b   IRExprKind = "blake2b"

	// Byte manipulation for building/parsing hash preimages. concat/slice/byteAt/
	// toBytesBE carry their operands in Args (in source order); fromBytesBE uses
	// Left.
	IRExprConcat      IRExprKind = "concat"
	IRExprSlice       IRExprKind = "slice"
	IRExprByteAt      IRExprKind = "byte_at"
	IRExprToBytesBE   IRExprKind = "to_bytes_be"
	IRExprFromBytesBE IRExprKind = "from_bytes_be"

	// Full-width fused multiply-divide (mulDiv / mulDivRoundUp). Three uint256
	// operands carried in Args (source order a, b, c); yields uint256.
	IRExprMulDiv        IRExprKind = "mul_div"
	IRExprMulDivRoundUp IRExprKind = "mul_div_round_up"

	// secp256k1 signature verification / public-key recovery. verifySecp256k1
	// carries (msgHash, sig, pubkey) in Args and yields bool; ecrecover carries
	// (msgHash, sig) in Args and yields bytes (the 64-byte X‖Y pubkey body).
	IRExprVerifySecp256k1 IRExprKind = "verify_secp256k1"
	IRExprEcrecover       IRExprKind = "ecrecover"

	// Integer square root over uint256 (isqrt): one uint256 operand in Args,
	// one uint256 result. Maps 1:1 to avm.OpIsqrt.
	IRExprIsqrt IRExprKind = "isqrt"

	// Full-range cross-product compare (mulCmp): four unsigned operands
	// (source order a, b, c, d) in Args, yields int256 sign(a*b - c*d) as
	// -1/0/+1. Maps 1:1 to avm.OpMulCmp.
	IRExprMulCmp IRExprKind = "mul_cmp"

	// Signed fused multiply-divide (mulDivSigned): three signed int256 operands
	// (source order a, b, c) in Args, yields int256 (a*b)/c truncated toward
	// zero. Maps 1:1 to avm.OpMulDivSigned.
	IRExprMulDivSigned IRExprKind = "mul_div_signed"
)

type IRStructField struct {
	Name string  `json:"name"`
	Expr *IRExpr `json:"expr,omitempty"`
}

type IRExpr struct {
	Kind   IRExprKind      `json:"kind"`
	Value  uint64          `json:"value,omitempty"`
	Slot   uint32          `json:"slot,omitempty"`
	Key    string          `json:"key,omitempty"`
	Text   string          `json:"text,omitempty"`
	Data   []byte          `json:"data,omitempty"`
	Left   *IRExpr         `json:"left,omitempty"`
	Right  *IRExpr         `json:"right,omitempty"`
	Else   *IRExpr         `json:"else,omitempty"`
	Args   []*IRExpr       `json:"args,omitempty"`
	Fields []IRStructField `json:"fields,omitempty"`
	Pos    Position        `json:"pos"`
}

type StatementLoweringRule struct {
	Statement StatementKind `json:"statement"`
	Sequence  []string      `json:"sequence"`
	Notes     string        `json:"notes,omitempty"`
}

func StatementLoweringRules() []StatementLoweringRule {
	return []StatementLoweringRule{
		{Statement: StatementBinding, Sequence: []string{"constant fold immutable literals", "runtime slot init for mutable or non-constant locals"}, Notes: "Mutable locals lower to dedicated AVM local slots; immutable compile-time constants stay folded."},
		{Statement: StatementSet, Sequence: []string{"lower expression", "OpWriteStorage(key=state field)"}, Notes: "Only direct state.<field> writes are executable."},
		{Statement: StatementEmit, Sequence: []string{"OpNop(data=event topic commitment)"}, Notes: "Event logs are trace-only until AVM exposes an event host function."},
		{Statement: StatementSend, Sequence: []string{"OpEmitInternal(arg=opcode,data=canonical payload commitment)"}, Notes: "Destination is supplied by RuntimeContext.EmitDestination in AVM v1."},
		{Statement: StatementRefund, Sequence: []string{"OpEmitInternal(arg=refund opcode,data=refund commitment)"}, Notes: "Refund is represented as a canonical internal emission until a dedicated refund opcode is introduced."},
		{Statement: StatementSelf, Sequence: []string{"OpScheduleSelf(arg=delay)"}, Notes: "Delay must be statically known and positive."},
		{Statement: StatementRepeat, Sequence: []string{"static count -> unroll", "dynamic count -> stack-backed loop"}, Notes: "Compile-time constants are unrolled; runtime counts use a stack-preserving decrement loop."},
		{Statement: StatementBreak, Sequence: []string{"jump to enclosing loop end"}, Notes: "Break is lowered as an unconditional jump to the nearest active loop end."},
		{Statement: StatementContinue, Sequence: []string{"jump to enclosing loop continue"}, Notes: "Continue is lowered as an unconditional jump to the nearest active loop continue target."},
		{Statement: StatementReturn, Sequence: []string{"lower expression", "OpReturn(arg=result code)"}, Notes: "For value returns the lowered expression remains on stack."},
	}
}
