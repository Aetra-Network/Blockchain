package compiler

import "fmt"

type Position struct {
	File   string
	Line   int
	Column int
}

func (p Position) String() string {
	if p.Line <= 0 {
		if p.File != "" {
			return p.File
		}
		return "<unknown>"
	}
	if p.File != "" {
		return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Column)
	}
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}

type SourceFile struct {
	Package   string
	Imports   []ImportDecl
	Consts    []*ConstDecl
	Structs   []*StructDecl
	Enums     []*EnumDecl
	Types     []*TypeDecl
	Functions []*FunctionDecl
	Contracts []*ContractDecl
}

type ConstDecl struct {
	Name  string
	Value Expr
	Pos   Position
}

type Annotation struct {
	Name  string
	Value *uint32
	Pos   Position
}

type ImportDecl struct {
	Alias   string
	Path    string
	Version string
	Pos     Position
}

type StructDecl struct {
	Annotations []Annotation
	Name        string
	Fields      []FieldDecl
	// TypeParams is the declaration-site generic parameter list, `<A, B>`
	// (AVM generics v1 design, revised — see generics.go). Nil for every
	// non-generic struct. A generic struct is a compile-time-only,
	// value-position construct: it can never be a contract's storage type
	// or a @message payload (validateStruct rejects both explicitly), and
	// it never produces its own codegen artifact — structDeclFor
	// (generics.go) substitutes and caches a concrete, field-type-resolved
	// clone (TypeParams nil) the first time a value of a given
	// (name, type-argument-tuple) shape is actually field-accessed
	// anywhere in the module.
	TypeParams []string
	Pos        Position
}

type TypeDecl struct {
	Name    string
	Members []TypeRef
	Pos     Position
}

type EnumDecl struct {
	Name     string
	Variants []VariantDecl
	Pos      Position
}

type ContractDecl struct {
	Name                 string
	StorageTypeName      string
	Author               string
	Description          string
	Version              string
	IncomingMessagesType string
	IncomingExternalType string
	Namespace            string
	ChainID              string
	DeployerAddress      string
	Salt                 string
	InitialBalance       uint64
	StorageDefaults      map[string]Expr
	Functions            []*FunctionDecl
	Messages             []*MessageDecl
	Getters              []*GetterDecl
	Events               []*EventDecl
	WalletActions        []*WalletActionDecl
	Pos                  Position
}

type FunctionDecl struct {
	Annotations []Annotation
	Pure        bool
	Name        string
	Params      []ParamDecl
	ReturnType  TypeRef
	Body        []Statement
	// TypeParams is the declaration-site generic parameter list, `<T, U>`
	// (AVM generics v1 design, revised — see generics.go), parsed
	// immediately after the function name and before its parameter list.
	// Nil for every non-generic function (the overwhelming majority), so no
	// existing AST shape changes. A generic declaration is never itself
	// lowered/validated as a callable body — see validateFunction's
	// TypeParams-guard early return and instantiateGenericFunction
	// (generics.go), which substitutes a concrete clone (TypeParams nil on
	// the clone) per distinct call-site type-argument tuple.
	TypeParams []string
	Pos        Position
}

type MessageKind string

const (
	MessageKindExternal MessageKind = "external"
	MessageKindInternal MessageKind = "internal"
	MessageKindBounced  MessageKind = "bounced"
	MessageKindDeploy   MessageKind = "deploy"
	MessageKindMigrate  MessageKind = "migrate"
)

func (k MessageKind) String() string { return string(k) }

type FieldDecl struct {
	Name    string
	Lazy    bool
	Type    TypeRef
	Default Expr
	Pos     Position
}

type VariantDecl struct {
	Name   string
	Fields []FieldDecl
	Pos    Position
}

type ParamDecl struct {
	Name   string
	Type   TypeRef
	Mutate bool
	Pos    Position
}

type MessageDecl struct {
	Annotations []Annotation
	Name        string
	Kind        MessageKind
	Params      []ParamDecl
	ReturnType  *TypeRef
	Body        []Statement
	ExplicitSel *uint32
	Pos         Position
}

type GetterDecl struct {
	Annotations []Annotation
	Name        string
	Params      []ParamDecl
	ReturnType  TypeRef
	Body        []Statement
	ExplicitSel *uint32
	Pos         Position
}

type EventDecl struct {
	Name   string
	Fields []ParamDecl
	Pos    Position
}

type WalletActionDecl struct {
	Name                   string
	Title                  string
	HasTitle               bool
	Risk                   string
	HasRisk                bool
	ConfirmLabel           string
	HasConfirmLabel        bool
	WarningLevel           string
	HasWarningLevel        bool
	ExpectedSideEffects    []string
	HasExpectedSideEffects bool
	FundAccess             bool
	HasFundAccess          bool
	ApprovalSemantics      string
	HasApprovalSemantics   bool
	Inputs                 []ParamDecl
	Outputs                []ParamDecl
	Pos                    Position
}

type TypeRef struct {
	Name     string
	Args     []TypeRef
	Optional bool
	Pos      Position
}

func (t TypeRef) String() string {
	out := t.Name
	if len(t.Args) > 0 {
		out += "<"
		for i, arg := range t.Args {
			if i > 0 {
				out += ","
			}
			out += arg.String()
		}
		out += ">"
	}
	if t.Optional {
		out += "?"
	}
	return out
}

type StatementKind string

const (
	StatementBinding  StatementKind = "binding"
	StatementSet      StatementKind = "set"
	StatementEmit     StatementKind = "emit"
	StatementReturn   StatementKind = "return"
	StatementRefund   StatementKind = "refund"
	StatementSend     StatementKind = "send"
	StatementSelf     StatementKind = "self"
	StatementExpr     StatementKind = "expr"
	StatementAssert   StatementKind = "assert"
	StatementThrow    StatementKind = "throw"
	StatementBreak    StatementKind = "break"
	StatementContinue StatementKind = "continue"
	StatementIf       StatementKind = "if"
	StatementWhile    StatementKind = "while"
	StatementDo       StatementKind = "do"
	StatementRepeat   StatementKind = "repeat"
	StatementMatch    StatementKind = "match"
	StatementFor      StatementKind = "for"
)

type Statement struct {
	Kind    StatementKind
	Name    string
	Path    []string
	Args    []Expr
	Value   Expr
	Extra   map[string]Expr
	Then    []Statement
	Else    []Statement
	Arms    []MatchArm
	Start   Expr
	End     Expr
	Index   string
	Mutable bool
	// Names is set instead of Name for a destructuring binding,
	// `const (a, b) = f()` (call mechanism v5 design doc §2.4) -- nil for
	// every ordinary single-name StatementBinding, so no existing program's
	// AST shape changes. Value must lower to a TagTuple RuntimeValue with
	// exactly len(Names) elements; each name is bound, in order, to the
	// tuple element at its own index.
	Names []string
	Pos   Position
}

type MatchArm struct {
	Pattern Pattern
	Body    []Statement
	Pos     Position
}

type PatternKind string

const (
	PatternWildcard PatternKind = "wildcard"
	PatternName     PatternKind = "name"
	PatternBind     PatternKind = "bind"
)

type Pattern struct {
	Kind     PatternKind
	Name     string
	Bindings []string
	Pos      Position
}

type ExprKind string

const (
	ExprIdent   ExprKind = "ident"
	ExprNumber  ExprKind = "number"
	ExprString  ExprKind = "string"
	ExprBool    ExprKind = "bool"
	ExprBytes   ExprKind = "bytes"
	ExprUnary   ExprKind = "unary"
	ExprBinary  ExprKind = "binary"
	ExprCall    ExprKind = "call"
	ExprPath    ExprKind = "path"
	ExprNull    ExprKind = "null"
	ExprTry     ExprKind = "try"
	ExprCompare ExprKind = "compare"
	ExprLogic   ExprKind = "logic"
	ExprTernary ExprKind = "ternary"
	ExprStruct  ExprKind = "struct"
	// ExprTupleLiteral is a parenthesized, comma-separated expression list,
	// `(a, b, ...)` (call mechanism v5 design doc §2.5) -- distinct from an
	// ordinary parenthesized grouping expression (`(a)`, still just `Args[0]`
	// unwrapped by the parser, unchanged), which this is additive to: a
	// comma inside the parens is what selects this Kind instead. Elements
	// live in Args, in source order.
	ExprTupleLiteral ExprKind = "tuple_literal"
)

type ExprField struct {
	Name  string
	Value Expr
	Pos   Position
}

type Expr struct {
	Kind   ExprKind
	Text   string
	Bool   bool
	Bytes  []byte
	Left   *Expr
	Right  *Expr
	Cond   *Expr
	Op     string
	Args   []Expr
	Path   []string
	Else   *Expr
	Fields []ExprField
	Unwrap bool
	// TypeArgs carries a call/struct-literal expression's explicit,
	// turbofish-marked type arguments, `::<T, U, ...>` (AVM generics v1
	// design, revised — parser.go's parsePrimary, right after parsePath).
	// Set only on ExprCall (a generic function call, `f::<T>(...)`) and
	// ExprStruct (a generic struct literal, `Pair::<A,B>{...}`); nil for
	// every non-generic call/literal and for every other ExprKind, so no
	// existing expression's shape changes. `::<` was syntactically
	// unreachable before this addition (lexer.go had no tokenColonColon),
	// so there is no existing program whose parse this could ever affect.
	TypeArgs []TypeRef
	Pos      Position
}

type DiagnosticSeverity string

const (
	SeverityError   DiagnosticSeverity = "error"
	SeverityWarning DiagnosticSeverity = "warning"
)

type SurfaceCompatibilityMode string

const (
	SurfaceCompatibilityWarnings SurfaceCompatibilityMode = "warnings"
	SurfaceCompatibilityStrict   SurfaceCompatibilityMode = "strict"
)

type Diagnostic struct {
	Severity DiagnosticSeverity
	Code     string
	Message  string
	Pos      Position
}

type NamedSource struct {
	Name string
	Data []byte
}

type DependencyResolver interface {
	ResolveImport(ImportDecl) (ResolvedDependency, *SourceFile, error)
}

type ResolvedDependency struct {
	Path       string   `json:"path"`
	Version    string   `json:"version"`
	Alias      string   `json:"alias,omitempty"`
	ABIHash    [32]byte `json:"abi_hash"`
	SourceHash [32]byte `json:"source_hash"`
	LockHash   [32]byte `json:"lock_hash"`
}

type DependencyLock struct {
	Package  string               `json:"package,omitempty"`
	Entries  []ResolvedDependency `json:"entries"`
	LockHash [32]byte             `json:"lock_hash"`
}

type CompileError struct {
	Diagnostics []Diagnostic
}

func (e *CompileError) Error() string {
	if e == nil || len(e.Diagnostics) == 0 {
		return "compile failed"
	}
	diag := e.Diagnostics[0]
	if diag.Pos.Line > 0 {
		return fmt.Sprintf("%s: %s", diag.Pos.String(), diag.Message)
	}
	return diag.Message
}
