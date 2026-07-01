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
	IRStmtEmitInternal IRStmtKind = "emit_internal"
	IRStmtScheduleSelf IRStmtKind = "schedule_self"
	IRStmtReturn       IRStmtKind = "return"
)

type IRStmt struct {
	Kind     IRStmtKind `json:"kind"`
	Name     string     `json:"name,omitempty"`
	Key      string     `json:"key,omitempty"`
	Opcode   uint32     `json:"opcode,omitempty"`
	Arg      uint64     `json:"arg,omitempty"`
	Data     []byte     `json:"data,omitempty"`
	Expr     *IRExpr    `json:"expr,omitempty"`
	Position Position   `json:"position"`
}

type IRExprKind string

const (
	IRExprConstU64    IRExprKind = "const_u64"
	IRExprStateRead   IRExprKind = "state_read"
	IRExprAdd         IRExprKind = "add"
	IRExprMsgOpcode   IRExprKind = "message_opcode"
	IRExprMsgQueryID  IRExprKind = "message_query_id"
	IRExprBlockHeight IRExprKind = "block_height"
)

type IRExpr struct {
	Kind  IRExprKind `json:"kind"`
	Value uint64     `json:"value,omitempty"`
	Key   string     `json:"key,omitempty"`
	Left  *IRExpr    `json:"left,omitempty"`
	Right *IRExpr    `json:"right,omitempty"`
	Pos   Position   `json:"pos"`
}

type StatementLoweringRule struct {
	Statement StatementKind `json:"statement"`
	Sequence  []string      `json:"sequence"`
	Notes     string        `json:"notes,omitempty"`
}

func StatementLoweringRules() []StatementLoweringRule {
	return []StatementLoweringRule{
		{Statement: StatementLet, Sequence: []string{"constant fold literal let bindings"}, Notes: "AVM v1 has no local-slot opcode; non-constant locals are rejected by lowering."},
		{Statement: StatementSet, Sequence: []string{"lower expression", "OpWriteStorage(key=state field)"}, Notes: "Only direct state.<field> writes are executable."},
		{Statement: StatementEmit, Sequence: []string{"OpNop(data=event topic commitment)"}, Notes: "Event logs are trace-only until AVM exposes an event host function."},
		{Statement: StatementSend, Sequence: []string{"OpEmitInternal(arg=opcode,data=canonical payload commitment)"}, Notes: "Destination is supplied by RuntimeContext.EmitDestination in AVM v1."},
		{Statement: StatementRefund, Sequence: []string{"OpEmitInternal(arg=refund opcode,data=refund commitment)"}, Notes: "Refund is represented as a canonical internal emission until a dedicated refund opcode is introduced."},
		{Statement: StatementSelf, Sequence: []string{"OpScheduleSelf(arg=delay)"}, Notes: "Delay must be statically known and positive."},
		{Statement: StatementReturn, Sequence: []string{"lower expression", "OpReturn(arg=result code)"}, Notes: "For value returns the lowered expression remains on stack."},
	}
}
