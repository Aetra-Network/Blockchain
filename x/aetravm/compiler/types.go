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
	Structs   []*StructDecl
	Enums     []*EnumDecl
	Functions []*FunctionDecl
	Contracts []*ContractDecl
}

type ImportDecl struct {
	Alias   string
	Path    string
	Version string
	Pos     Position
}

type StructDecl struct {
	Name   string
	Fields []FieldDecl
	Pos    Position
}

type EnumDecl struct {
	Name     string
	Variants []VariantDecl
	Pos      Position
}

type ContractDecl struct {
	Name            string
	StorageTypeName string
	Namespace       string
	ChainID         string
	DeployerAddress string
	Salt            string
	InitialBalance  uint64
	StorageDefaults map[string]Expr
	Messages        []*MessageDecl
	Getters         []*GetterDecl
	Events          []*EventDecl
	WalletActions   []*WalletActionDecl
	Pos             Position
}

type FunctionDecl struct {
	Name       string
	Params     []ParamDecl
	ReturnType TypeRef
	Body       []Statement
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
	Name string
	Type TypeRef
	Pos  Position
}

type MessageDecl struct {
	Name        string
	Kind        MessageKind
	Params      []ParamDecl
	ReturnType  *TypeRef
	Body        []Statement
	ExplicitSel *uint32
	Pos         Position
}

type GetterDecl struct {
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
	Name                string
	Title               string
	Risk                string
	ConfirmLabel        string
	WarningLevel        string
	ExpectedSideEffects []string
	FundAccess          bool
	ApprovalSemantics   string
	Inputs              []ParamDecl
	Outputs             []ParamDecl
	Pos                 Position
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
	StatementLet    StatementKind = "let"
	StatementSet    StatementKind = "set"
	StatementEmit   StatementKind = "emit"
	StatementReturn StatementKind = "return"
	StatementRefund StatementKind = "refund"
	StatementSend   StatementKind = "send"
	StatementSelf   StatementKind = "self"
	StatementIf     StatementKind = "if"
	StatementMatch  StatementKind = "match"
	StatementFor    StatementKind = "for"
)

type Statement struct {
	Kind  StatementKind
	Name  string
	Path  []string
	Args  []Expr
	Value Expr
	Extra map[string]Expr
	Then  []Statement
	Else  []Statement
	Arms  []MatchArm
	Start Expr
	End   Expr
	Index string
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
	ExprBinary  ExprKind = "binary"
	ExprCall    ExprKind = "call"
	ExprPath    ExprKind = "path"
	ExprNull    ExprKind = "null"
	ExprTry     ExprKind = "try"
	ExprCompare ExprKind = "compare"
	ExprLogic   ExprKind = "logic"
)

type Expr struct {
	Kind  ExprKind
	Text  string
	Bool  bool
	Bytes []byte
	Left  *Expr
	Right *Expr
	Op    string
	Args  []Expr
	Path  []string
	Else  *Expr
	Pos   Position
}

type DiagnosticSeverity string

const (
	SeverityError   DiagnosticSeverity = "error"
	SeverityWarning DiagnosticSeverity = "warning"
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
