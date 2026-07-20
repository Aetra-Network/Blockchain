package compiler

import (
	"fmt"
	"strings"
)

// generics.go implements AVM generics v1 (design: compile-time
// monomorphization for functions and value-typed structs, revised after
// three adversarial review rounds -- see the task's design doc for the full
// rationale). This file is the monomorphization pass itself: mangling,
// type-parameter substitution, and the two lazy, cached "instantiate on
// first need" entry points (instantiateGenericFunction for calls,
// structDeclFor for struct field access) that the rest of the compiler
// (compile.go) calls into from a small, precise set of choke points rather
// than being generics-aware everywhere itself.
//
// Grammar (parser.go) and the declaration-site TypeParams/call-site
// TypeArgs AST fields (types.go) are additive and covered elsewhere; this
// file is where a TypeParams-bearing declaration and a TypeArgs-bearing
// call/struct-literal actually get resolved into concrete, ordinary
// (TypeParams-empty) declarations the rest of the compiler already knows
// how to validate/lower/call without any further generics-awareness.
//
// Design summary (see the task's design doc for the full argument):
//
//   - Function generics produce a REAL compiled call target: the
//     substituted, concrete FunctionDecl is registered into the SAME
//     `functions` map every other function lookup already shares by
//     reference, under its mangled name. From that point on it is,
//     structurally, indistinguishable from an ordinary hand-written
//     function to every other part of the compiler -- including the
//     existing intra-contract call mechanism (tryRealUserFunctionCall /
//     compileCalledFunction / claimLocalSlot, compile.go), which is what
//     gives monomorphized instantiations their slot-disjointness for free:
//     each instantiation is compiled through the IDENTICAL path a plain
//     function already uses, claiming its own module-wide-disjoint local-
//     slot range via the SAME claimLocalSlot helper, never a parallel
//     allocator.
//
//   - Struct generics are a type-checking-only, on-demand cache
//     (structDeclFor): a generic struct's concrete, field-substituted
//     instantiation is registered into the SAME `structs` map every other
//     struct lookup already shares by reference, under its mangled name,
//     the first time a value of that exact (name, type-argument-tuple)
//     shape is actually field-accessed anywhere in the module. It never
//     produces its own IREntry / codegen artifact: a struct VALUE in this
//     language is a runtime map keyed by field NAME (OpMapEmpty/OpMapSet/
//     OpReadField), duck-typed at the VM level regardless of how its
//     static type was derived, so nothing in avm.go needs to change at
//     all.
//
//   - Both share ONE hard, module-wide instantiation budget
//     (maxGenericInstantiations, claimGenericInstantiationBudget), charged
//     and checked the instant a NEW (name, type-argument-tuple) pair is
//     first discovered, before any substitution/validation/lowering work
//     for it begins -- structurally bounding total compile-time work to
//     O(maxGenericInstantiations) regardless of how the type-argument graph
//     is shaped, including a non-cyclic branching fanout that could
//     otherwise reach exponentially many distinct instantiations from
//     linear source.
//
//   - Discovery is entirely a side effect of typecheck()'s own, already-
//     deterministic, source-order AST walk (validateStatement /
//     inferExprType visiting every call/struct-literal expression in every
//     concrete function/message/getter body): a generic call/struct-literal
//     site is resolved and, if new, instantiated the moment inferExprType
//     first reaches it. Since typecheck() fully completes before buildIR
//     (IR lowering) ever starts, EVERY instantiation IR lowering could ever
//     need already exists, registered under its mangled name, by the time
//     lowering begins -- so the lowering-phase call sites (resolveUserFunction)
//     only ever need a plain, non-mutating mangled-name lookup, never a
//     fresh instantiation attempt of their own.

// maxGenericInstantiations is the hard ceiling on distinct (name,
// closed-type-argument-tuple) pairs -- functions AND structs combined -- a
// single module's monomorphization worklist may ever discover. This bounds
// total compile-time work to O(maxGenericInstantiations) regardless of how
// the type-argument graph is shaped, including a non-cyclic branching
// fanout (chained generic calls with composed type arguments) that could
// otherwise reach 2^N distinct instantiations from O(N) lines of source
// despite the bare-name call graph being acyclic (validateFunctionRecursion
// only proves the worklist TERMINATES, not that it stays small). Deliberately
// low for v1; raising it later is a one-line, non-breaking change once real
// usage patterns are known.
const maxGenericInstantiations = 128

// claimGenericInstantiationBudget MUST be called exactly once per newly
// discovered (name, type-args) pair -- function or struct -- BEFORE any
// substitution/validation/lowering work for that pair begins. Fails the
// whole compile immediately, right there, the instant the budget is
// exhausted -- it does not let discovery continue expanding first and tally
// afterward.
func (c *Compiler) claimGenericInstantiationBudget(kind, mangled string, pos Position) error {
	c.genericInstantiationCount++
	if c.genericInstantiationCount > maxGenericInstantiations {
		return fail("E_GENERIC_INSTANTIATION_BUDGET", pos, fmt.Sprintf(
			"module-wide generic instantiation budget (%d) exceeded discovering %s %q -- reduce distinct generic type-argument combinations or split the contract",
			maxGenericInstantiations, kind, mangled))
	}
	return nil
}

// mangleGenericName computes a generic instantiation's call-target /
// struct-table key: name + "$" + T1.String() + "$" + T2.String() + ... .
// Injective (two distinct (name, type-args) pairs can never collide): "$"
// never appears in a lexer-valid identifier (isIdentStart/isIdentPart,
// lexer.go) and TypeRef.String() (types.go) never emits one either, so the
// separator can never be produced by either the base name or by any
// argument's own rendered form, however deeply nested (TypeRef.String()
// recurses into Args using only "<", ">", "," and "?", none of which are
// "$" either). Used identically for both function and struct instantiations
// -- one scheme, not two, matching the design doc's own mangling proof.
func mangleGenericName(name string, args []TypeRef) string {
	var b strings.Builder
	b.WriteString(name)
	for _, a := range args {
		b.WriteByte('$')
		b.WriteString(a.String())
	}
	return b.String()
}

// validateTypeParamShadowing rejects a declaration-site type parameter list
// that is malformed (an invalid identifier -- structurally unreachable
// through the parser, checked defensively), contains a duplicate name
// (already rejected by parseTypeParamList too; kept here as well, mirroring
// this codebase's existing convention of doubled-up defense-in-depth checks
// at the validation layer, e.g. StatementSet's readOnly guard, compile.go),
// or shadows an existing struct/enum/type-alias name -- shadowing is
// rejected rather than merely discouraged because a shadowed type parameter
// would make substituteTypeRef's own bare-name replacement ambiguous with
// an ordinary, non-generic reference to the real type of the same name
// appearing elsewhere in the same declaration.
func validateTypeParamShadowing(typeParams []string, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl, pos Position) error {
	seen := map[string]struct{}{}
	for _, tp := range typeParams {
		if !isValidName(tp) {
			return fail("E_NAME", pos, fmt.Sprintf("invalid type parameter name %q", tp))
		}
		if _, dup := seen[tp]; dup {
			return fail("E_GENERIC_DUP_PARAM", pos, fmt.Sprintf("duplicate type parameter %q", tp))
		}
		seen[tp] = struct{}{}
		if _, ok := structs[tp]; ok {
			return fail("E_GENERIC_SHADOW", pos, fmt.Sprintf("type parameter %q shadows an existing struct type", tp))
		}
		if _, ok := enums[tp]; ok {
			return fail("E_GENERIC_SHADOW", pos, fmt.Sprintf("type parameter %q shadows an existing enum type", tp))
		}
		if _, ok := types[tp]; ok {
			return fail("E_GENERIC_SHADOW", pos, fmt.Sprintf("type parameter %q shadows an existing type alias", tp))
		}
	}
	return nil
}

// substituteTypeRef performs a plain, textual bare-name substitution of
// generic type parameters within a TypeRef, recursing into Args. It does
// NOT trigger struct instantiation/mangling -- that is structDeclFor's
// separate, later, lazy concern, triggered only where a value's concrete
// field types are actually needed (field access, struct-literal type
// inference), not eagerly at substitution time. This keeps function
// monomorphization (a pure syntactic clone-and-replace) fully decoupled
// from struct monomorphization (a type-checking-only, on-demand cache): a
// generic function's substituted return type may still be a
// compound reference like `Pair<uint64,uint64>` (Args intact, not yet
// resolved to a mangled struct name) -- exactly as it would look if a
// PLAIN, non-generic function had simply declared that return type
// directly, which is precisely the case structDeclFor's callers (resolvePathType,
// the two StatementSet targetStruct sites) already have to handle regardless
// of generics.
func substituteTypeRef(t TypeRef, subst map[string]TypeRef) TypeRef {
	if len(subst) == 0 {
		return t
	}
	if repl, ok := subst[t.Name]; ok && len(t.Args) == 0 {
		out := repl
		if t.Optional {
			out.Optional = true
		}
		return out
	}
	if len(t.Args) == 0 {
		return t
	}
	args := make([]TypeRef, len(t.Args))
	for i, a := range t.Args {
		args[i] = substituteTypeRef(a, subst)
	}
	return TypeRef{Name: t.Name, Args: args, Optional: t.Optional, Pos: t.Pos}
}

// substituteExprTypeArgs deep-clones an expression tree, substituting bare
// type-parameter names within every TypeArgs list reachable from it (i.e.
// every nested generic call / generic struct literal inside a generic
// function's own body -- design doc §3.2's "a nested call's own TypeArgs
// must be substituted before that nested call is lowered" obligation). It
// does NOT evaluate, fold, or otherwise interpret the expression -- purely a
// structural clone, walking every Expr-shaped field the AST defines
// (Left/Right/Cond/Else/Args/Fields), so a type parameter used anywhere
// inside an arbitrarily nested expression (a comparison operand, a ternary
// branch, a nested call argument, a struct-literal field value) is covered
// by the SAME single recursive walk rather than a per-shape special case.
func substituteExprTypeArgs(e Expr, subst map[string]TypeRef) Expr {
	out := e
	if len(e.TypeArgs) > 0 {
		args := make([]TypeRef, len(e.TypeArgs))
		for i, a := range e.TypeArgs {
			args[i] = substituteTypeRef(a, subst)
		}
		out.TypeArgs = args
	}
	if e.Left != nil {
		l := substituteExprTypeArgs(*e.Left, subst)
		out.Left = &l
	}
	if e.Right != nil {
		r := substituteExprTypeArgs(*e.Right, subst)
		out.Right = &r
	}
	if e.Cond != nil {
		cnd := substituteExprTypeArgs(*e.Cond, subst)
		out.Cond = &cnd
	}
	if e.Else != nil {
		el := substituteExprTypeArgs(*e.Else, subst)
		out.Else = &el
	}
	if len(e.Args) > 0 {
		args := make([]Expr, len(e.Args))
		for i, a := range e.Args {
			args[i] = substituteExprTypeArgs(a, subst)
		}
		out.Args = args
	}
	if len(e.Fields) > 0 {
		fields := make([]ExprField, len(e.Fields))
		for i, f := range e.Fields {
			fields[i] = ExprField{Name: f.Name, Value: substituteExprTypeArgs(f.Value, subst), Pos: f.Pos}
		}
		out.Fields = fields
	}
	return out
}

// substituteStatement/substituteStatements are substituteExprTypeArgs' the
// Statement-tree half: every Expr-shaped field a Statement carries
// (Value/Args/Extra/Start/End) is substituted, and every nested statement
// list (Then/Else/Arms) is walked recursively, so a type parameter used
// arbitrarily deep inside a generic function's body (nested if/while/match/
// for blocks) is reached by construction.
func substituteStatement(s Statement, subst map[string]TypeRef) Statement {
	out := s
	out.Value = substituteExprTypeArgs(s.Value, subst)
	if len(s.Args) > 0 {
		args := make([]Expr, len(s.Args))
		for i, a := range s.Args {
			args[i] = substituteExprTypeArgs(a, subst)
		}
		out.Args = args
	}
	if len(s.Extra) > 0 {
		extra := make(map[string]Expr, len(s.Extra))
		for k, v := range s.Extra {
			extra[k] = substituteExprTypeArgs(v, subst)
		}
		out.Extra = extra
	}
	out.Start = substituteExprTypeArgs(s.Start, subst)
	out.End = substituteExprTypeArgs(s.End, subst)
	out.Then = substituteStatements(s.Then, subst)
	out.Else = substituteStatements(s.Else, subst)
	if len(s.Arms) > 0 {
		arms := make([]MatchArm, len(s.Arms))
		for i, arm := range s.Arms {
			arms[i] = MatchArm{Pattern: arm.Pattern, Body: substituteStatements(arm.Body, subst), Pos: arm.Pos}
		}
		out.Arms = arms
	}
	return out
}

func substituteStatements(stmts []Statement, subst map[string]TypeRef) []Statement {
	if stmts == nil {
		return nil
	}
	out := make([]Statement, len(stmts))
	for i, s := range stmts {
		out[i] = substituteStatement(s, subst)
	}
	return out
}

// resolveDeclaredFunction finds the AS-DECLARED FunctionDecl a call
// expression refers to, by its bare or dotted callable name -- WITHOUT
// resolving any generic (design doc §1.1) type arguments the call site
// might carry. Both resolveCallFunction (typecheck phase: instantiates a
// generic declaration the first time a call site needs it) and
// resolveUserFunction (compile.go, lowering phase: looks up an
// already-instantiated, mangled entry a typecheck-phase call already
// registered) start from this same declaration lookup, so the dotted-vs-
// bare resolution rule lives in exactly one place.
func resolveDeclaredFunction(expr Expr, functions map[string]*FunctionDecl) *FunctionDecl {
	if len(expr.Path) >= 2 {
		if fn, ok := functions[strings.Join(expr.Path, ".")]; ok {
			return fn
		}
	}
	if fn, ok := functions[expr.Text]; ok {
		return fn
	}
	return nil
}

// resolveCallFunction resolves expr's call target during the TYPECHECK
// phase (inferExprType's ExprCall case is its only caller), transparently
// performing generic monomorphization when expr carries explicit turbofish
// type arguments: it resolves the DECLARED (possibly generic) function by
// name, and if that declaration is generic, instantiates (or reuses an
// already-cached instantiation of) the concrete substituted clone and
// returns THAT instead -- so every downstream consumer (argument/return-type
// checking) sees an ordinary, fully concrete FunctionDecl and never has to
// be generics-aware itself. Returns (nil, nil) when expr does not resolve
// to any declared function at all (the caller falls through to its other
// resolution paths -- builtins, structs, ordinary "unknown call" errors),
// exactly like resolveUserFunction's own contract.
func (c *Compiler) resolveCallFunction(expr Expr, functions map[string]*FunctionDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl, consts map[string]constValue, pos Position) (*FunctionDecl, error) {
	declared := resolveDeclaredFunction(expr, functions)
	if declared == nil {
		return nil, nil
	}
	if len(declared.TypeParams) == 0 {
		if len(expr.TypeArgs) > 0 {
			return nil, fail("E_GENERIC_ARGS_UNEXPECTED", pos, fmt.Sprintf("function %q is not generic: unexpected ::<...> type arguments", declared.Name))
		}
		return declared, nil
	}
	if len(expr.TypeArgs) != len(declared.TypeParams) {
		return nil, fail("E_GENERIC_ARITY", pos, fmt.Sprintf("generic function %q expects %d type argument(s) (::<%s>), got %d", declared.Name, len(declared.TypeParams), strings.Join(declared.TypeParams, ", "), len(expr.TypeArgs)))
	}
	return c.instantiateGenericFunction(declared, expr.TypeArgs, functions, structs, enums, types, consts, pos)
}

// instantiateGenericFunction substitutes fn's declared type parameters with
// concrete typeArgs, producing a self-contained, ordinary (TypeParams-empty)
// FunctionDecl -- registered into the SAME `functions` map under its
// mangled name (mangleGenericName) so every other lookup in the module
// (including the lowering-phase real-call mechanism, compile.go's
// resolveUserFunction/tryRealUserFunctionCall/compileCalledFunction) finds
// and compiles it exactly like any hand-written function, through the
// IDENTICAL claimLocalSlot-based allocator -- this is what gives each
// distinct instantiation its own disjoint local-slot range for free,
// without a parallel allocation mechanism.
//
// Cached by mangled name (a cache hit returns immediately, no re-validation,
// no re-charge against the instantiation budget) -- both for ordinary
// dedup (the same instantiation reached from multiple call sites) AND as a
// recursion guard: the new FunctionDecl is registered into `functions`
// BEFORE its substituted body is validated, so a generic function that
// (transitively) instantiates itself with the SAME closed type-argument
// tuple resolves to the in-flight entry on re-entry instead of recursing
// forever in the Go call stack. (True self-recursion, at the bare-name
// level, is separately and unconditionally rejected by
// validateFunctionRecursion regardless of type arguments -- this guard
// exists only because that check runs AFTER typecheck()'s own per-function
// validation loop, which is where instantiation discovery happens, so a
// pathological generic self-reference could otherwise blow the Go stack
// before validateFunctionRecursion ever gets a chance to reject it
// cleanly.)
func (c *Compiler) instantiateGenericFunction(fn *FunctionDecl, typeArgs []TypeRef, functions map[string]*FunctionDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl, consts map[string]constValue, pos Position) (*FunctionDecl, error) {
	mangled := mangleGenericName(fn.Name, typeArgs)
	if existing, ok := functions[mangled]; ok {
		return existing, nil
	}
	if err := c.claimGenericInstantiationBudget("function", mangled, pos); err != nil {
		return nil, err
	}
	subst := make(map[string]TypeRef, len(fn.TypeParams))
	for i, tp := range fn.TypeParams {
		subst[tp] = typeArgs[i]
	}
	params := make([]ParamDecl, len(fn.Params))
	for i, p := range fn.Params {
		params[i] = ParamDecl{Name: p.Name, Type: substituteTypeRef(p.Type, subst), Mutate: p.Mutate, Pos: p.Pos}
	}
	newFn := &FunctionDecl{
		Annotations: fn.Annotations,
		Pure:        fn.Pure,
		Name:        mangled,
		Params:      params,
		ReturnType:  substituteTypeRef(fn.ReturnType, subst),
		Body:        substituteStatements(fn.Body, subst),
		Pos:         fn.Pos,
	}
	functions[mangled] = newFn
	if err := c.validateFunctionBody(newFn, structs, enums, types, functions, consts); err != nil {
		delete(functions, mangled)
		return nil, err
	}
	if err := checkResourceBody(newFn.Params, newFn.Body, resourceStructNamesFromMap(structs), functions); err != nil {
		delete(functions, mangled)
		return nil, err
	}
	return newFn, nil
}

// structDeclFor resolves the *StructDecl a static TypeRef denotes,
// instantiating (and caching, into the SAME `structs` map every other
// lookup in the module shares by reference) a generic struct's concrete,
// field-substituted layout the first time a value of that exact
// (name, type-argument-tuple) shape is actually field-accessed anywhere in
// the program (design doc §3.5: a type-checking-only artifact, never its
// own codegen output -- a struct value is a runtime map keyed by field
// NAME, duck-typed at the VM level regardless of this bookkeeping). A
// non-generic TypeRef (t.Args empty, or t.Name not naming a generic
// struct at all -- including every scalar/stdlib-parametric/non-generic-
// struct type) is a zero-cost, zero-side-effect passthrough to
// structs[t.Name]: this is the compiler's single choke point for "what
// struct declaration backs this static type," so lookupStructField's
// callers (resolvePathType, the two StatementSet targetStruct sites) never
// need their own generics-awareness.
//
// A field's substituted type is stored AS-IS (via substituteTypeRef, which
// does not itself trigger further struct resolution) even when it is
// itself a concrete reference to another generic struct (e.g. instantiating
// `struct Wrap<T>{ inner: T }` with T=Pair<uint64,uint64> leaves the
// "inner" field's stored type as `Pair<uint64,uint64>`, Args intact, not
// yet mangled) -- resolving THAT is deferred to the identical lazy
// mechanism, triggered the next time something actually accesses
// `.inner.first`: resolvePathType calls structDeclFor again, on the now-
// current path segment's type, exactly as it would for a plain (non-
// nested-in-a-generic-instantiation) value of that same compound type. This
// keeps struct instantiation itself non-recursive and unconditionally
// terminating without needing its own separate cycle guard.
func (c *Compiler) structDeclFor(t TypeRef, structs map[string]*StructDecl, pos Position) (*StructDecl, error) {
	decl, ok := structs[t.Name]
	if !ok {
		return nil, nil
	}
	if len(decl.TypeParams) == 0 {
		return decl, nil
	}
	if len(t.Args) != len(decl.TypeParams) {
		return nil, fail("E_TYPE_ARITY", pos, fmt.Sprintf("type %q requires %d type argument(s), got %d", t.Name, len(decl.TypeParams), len(t.Args)))
	}
	mangled := mangleGenericName(t.Name, t.Args)
	if existing, ok := structs[mangled]; ok {
		return existing, nil
	}
	if err := c.claimGenericInstantiationBudget("struct", mangled, pos); err != nil {
		return nil, err
	}
	subst := make(map[string]TypeRef, len(decl.TypeParams))
	for i, tp := range decl.TypeParams {
		subst[tp] = t.Args[i]
	}
	fields := make([]FieldDecl, len(decl.Fields))
	for i, f := range decl.Fields {
		fields[i] = FieldDecl{Name: f.Name, Lazy: f.Lazy, Type: substituteTypeRef(f.Type, subst), Pos: f.Pos}
	}
	inst := &StructDecl{Name: mangled, Annotations: decl.Annotations, Fields: fields, Pos: decl.Pos}
	structs[mangled] = inst
	return inst, nil
}

// validateGenericStructLiteralArity checks a struct-literal expression's
// explicit ::<...> type-argument count (if any) against the referenced
// struct's declared TypeParams count, mirroring resolveCallFunction's
// identical arity check for a generic function call. Called from
// inferExprType's ExprStruct case; does not itself trigger instantiation
// (structDeclFor's job, lazily, only where the value is later field-
// accessed) -- this only confirms the literal is well-formed.
func validateGenericStructLiteralArity(expr Expr, structs map[string]*StructDecl, pos Position) error {
	decl, ok := structs[expr.Text]
	if !ok {
		return nil
	}
	if len(decl.TypeParams) == 0 {
		if len(expr.TypeArgs) > 0 {
			return fail("E_GENERIC_ARGS_UNEXPECTED", pos, fmt.Sprintf("struct %q is not generic: unexpected ::<...> type arguments", decl.Name))
		}
		return nil
	}
	if len(expr.TypeArgs) != len(decl.TypeParams) {
		return fail("E_GENERIC_ARITY", pos, fmt.Sprintf("generic struct %q expects %d type argument(s) (::<%s>), got %d", decl.Name, len(decl.TypeParams), strings.Join(decl.TypeParams, ", "), len(expr.TypeArgs)))
	}
	return nil
}
