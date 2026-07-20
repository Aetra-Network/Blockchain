package compiler

import "fmt"

// resource_abilities.go implements a compiler-only, static linear-use check
// for struct types annotated @resource (Move-style "no copy, no drop"
// semantics), so tokens/NFTs modeled as @resource structs cannot be silently
// duplicated or discarded at the source-language level.
//
// Phase E scoping note (docs/architecture/avm-language-roadmap.md): the
// adversarial review of the Phase E scoping doc confirmed this item as
// "plausibly tractable now WITHOUT waiting on Phase F -- but only if
// explicitly scoped to intra-function / inliner-only value flow (matching
// today's actual, very restricted call model) and explicitly excluded from
// anything crossing a real cross-function call boundary until Phase F
// lands." This file honors that scope explicitly; see the doc comment on
// CheckResourceAbilities below for the precise, honestly-stated boundaries.
//
// This is a standalone file: it does not modify parser.go's AST types,
// ir.go, or avm.go. It IS wired into Compiler.Compile()/CompileFiles()
// (compile.go, end of typecheck()), so every compile enforces @resource
// linearity automatically; see the package-level note at the bottom of this
// file for the integration point.

// ResourceAbilityError is returned by CheckResourceAbilities when a
// @resource-typed value is used in a way that would duplicate or silently
// discard it.
type ResourceAbilityError struct {
	Code    string
	Message string
	Pos     Position
}

func (e *ResourceAbilityError) Error() string {
	if e.Pos.Line > 0 {
		return fmt.Sprintf("%s: %s (%s)", e.Pos.String(), e.Message, e.Code)
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

// CheckResourceAbilities performs a compiler-only static linear-use check
// for struct types annotated @resource across every function-shaped body in
// a parsed source file (top-level functions, and every contract's
// functions/messages/getters).
//
// Rule: every @resource-typed function/getter/message parameter, and every
// local binding (`const`/`var`) whose static type is a @resource struct,
// must be referenced exactly once within its own declaring body:
//   - zero references is treated as a dropped resource (the type has no
//     'drop' ability -- E_RESOURCE_UNUSED);
//   - two or more references is treated as an implicit duplication (the
//     type has no 'copy' ability -- E_RESOURCE_DOUBLE_USE), because every
//     struct read in AVM v1 today is a value copy, never an alias (see
//     TestLocalStructLiteralFieldReadWriteAndCopySemantics /
//     commit 1165cf4f: "Copy-on-assign must NOT alias"), so a second
//     reference to a resource-typed binding really would materialize an
//     independent copy of the value.
//
// Explicit, deliberate scope limitations (v1, matching the review's
// tractable-now boundary):
//
//  1. Intra-function / inliner-only. This pass looks at a single
//     FunctionDecl/MessageDecl/GetterDecl body in isolation. It does not
//     attempt to track a resource value across a real call-stack boundary
//     -- AVM v1 has no call-stack abstraction today (tryInlineUserFunctionCall
//     is the only intra-contract call mechanism, and the shared Phase E/F
//     call-mechanism design has been rejected three consecutive adversarial
//     rounds; see docs/architecture/avm-phase-ef-call-design.md). A
//     resource-typed value passed as a call argument or returned from a
//     call is counted as "one use" at its call site/return site and is not
//     re-tracked inside the callee.
//
//  2. Branch-insensitive by design, not by oversight. References are
//     counted textually across the WHOLE body, including inside every
//     `if`/`else`/`match arm`/`while`/`for` block, rather than per
//     execution path. This can over-reject legitimate branch-exclusive
//     usage (e.g. a resource consumed in the `if` arm AND, separately, in
//     the `else` arm, which can never both execute) -- a known, explicit
//     v1 limitation. It is a conservative over-approximation: it can never
//     let a real double-use slip through undetected, it can only reject
//     some valid programs that a smarter, path-sensitive checker would
//     accept. A future pass may replace this with real CFG-based liveness
//     analysis; that is out of scope here.
//
//  3. No containment-safety check. A resource value nested as a field of a
//     struct LITERAL (`Wrapper{inner: tok}`) is treated as consumed (its
//     one required use), but this pass does not verify that the outer
//     struct type (`Wrapper`) is itself handled linearly, nor that
//     `Wrapper` is prevented from being freely copied elsewhere merely
//     because it embeds a resource field. Enforcing that a resource cannot
//     escape into a non-resource container is real, additional work
//     (effectively requiring resource-ness to propagate transitively
//     through struct field types) and is deliberately deferred.
//
//  4. No pattern-bound resource tracking. A resource value unwrapped via a
//     match-arm binding pattern (e.g. `Some(v) => ...`) is not tracked by
//     this pass at all -- match/enum/Option/Result support is separate,
//     larger Phase E work (traits/generics/enums) that is explicitly NOT
//     part of this tractable item per the review.
//
//  5. Field mutation on a resource-typed local (`set x.field = ...`) also
//     counts as a reference to `x`. This is intentionally conservative
//     (mutation only changes the value in place, per the same
//     copy-on-assign/no-aliasing guarantee cited above, so it is not
//     actually a duplication) rather than trying to distinguish
//     in-place-mutation from value-duplicating reads. It may
//     false-positive on a legitimate "mutate a field, then move" pattern;
//     documented here rather than silently mis-scoped.
//
// This pass needs no parser, IR, or VM changes: StructDecl already carries
// a free-form Annotations list (used identically by @storage/@message), and
// the runtime treats a @resource struct exactly like any other struct
// (OpMapEmpty/OpMapSet/OpReadField) -- @resource changes zero bytes of
// emitted bytecode, only what the static checker permits at compile time.
//
// functions is the whole-module, dotted-and-bare-name callable table
// (typecheck()'s own `functions` parameter, unconditionally covering every
// declared function -- including, by the time this runs at the END of
// typecheck, every generic function's already-instantiated, concrete clones
// registered under their mangled names, see generics.go) -- threaded
// through so a resource-typed CALL RESULT (see resolveCallReturnTypeName
// below) can be tracked, not just a struct-literal or local-copy RHS.
func CheckResourceAbilities(src *SourceFile, functions map[string]*FunctionDecl) error {
	if src == nil {
		return nil
	}
	resourceNames := resourceStructNames(src.Structs)
	if len(resourceNames) == 0 {
		return nil
	}
	for _, fn := range src.Functions {
		if fn == nil {
			continue
		}
		if err := checkResourceBody(fn.Params, fn.Body, resourceNames, functions); err != nil {
			return err
		}
	}
	for _, ct := range src.Contracts {
		if ct == nil {
			continue
		}
		for _, fn := range ct.Functions {
			if fn == nil {
				continue
			}
			if err := checkResourceBody(fn.Params, fn.Body, resourceNames, functions); err != nil {
				return err
			}
		}
		for _, msg := range ct.Messages {
			if msg == nil {
				continue
			}
			if err := checkResourceBody(msg.Params, msg.Body, resourceNames, functions); err != nil {
				return err
			}
		}
		for _, get := range ct.Getters {
			if get == nil {
				continue
			}
			if err := checkResourceBody(get.Params, get.Body, resourceNames, functions); err != nil {
				return err
			}
		}
	}
	return nil
}

// resourceStructNames returns the set of struct names annotated @resource.
func resourceStructNames(structs []*StructDecl) map[string]bool {
	names := map[string]bool{}
	for _, st := range structs {
		if st == nil {
			continue
		}
		for _, ann := range st.Annotations {
			if ann.Name == "@resource" {
				names[st.Name] = true
			}
		}
	}
	return names
}

// resourceStructNamesFromMap is resourceStructNames' map-keyed twin, used by
// instantiateGenericFunction (generics.go) to re-run the resource check
// against a generic function's substituted, concrete clone (design doc
// §3.3): the module-wide struct table is threaded through the compile as a
// map[string]*StructDecl there, not the []*StructDecl a parsed SourceFile
// exposes, so this avoids either duplicating the annotation-scan logic or
// forcing an unnecessary map->slice->map round trip at every instantiation.
func resourceStructNamesFromMap(structs map[string]*StructDecl) map[string]bool {
	names := map[string]bool{}
	for _, st := range structs {
		if st == nil {
			continue
		}
		for _, ann := range st.Annotations {
			if ann.Name == "@resource" {
				names[st.Name] = true
			}
		}
	}
	return names
}

// resourceBinding tracks a single @resource-typed name (parameter or local)
// declared/visible in a body, so it can be reported by name if misused.
type resourceBinding struct {
	name string
	typ  string
	pos  Position
}

// checkResourceBody enforces exactly-once use for every @resource-typed
// parameter and local binding within a single function-shaped body. See the
// CheckResourceAbilities doc comment for the full scope statement.
func checkResourceBody(params []ParamDecl, body []Statement, resourceNames map[string]bool, functions map[string]*FunctionDecl) error {
	// localTypes tracks the inferred struct type name of every binding seen
	// so far (parameters up front, locals as their StatementBinding is
	// walked in document order -- an approximation of execution order that
	// is sufficient for this pass's deliberately flat, whole-body scope).
	localTypes := map[string]string{}
	var bindings []resourceBinding

	for _, p := range params {
		if resourceNames[p.Type.Name] {
			localTypes[p.Name] = p.Type.Name
			bindings = append(bindings, resourceBinding{name: p.Name, typ: p.Type.Name, pos: p.Pos})
		}
	}

	collectResourceLocalBindings(body, resourceNames, localTypes, &bindings, functions)

	if len(bindings) == 0 {
		return nil
	}

	counts := map[string]int{}
	collectIdentRefCounts(body, counts)

	for _, b := range bindings {
		switch counts[b.name] {
		case 0:
			return &ResourceAbilityError{
				Code:    "E_RESOURCE_UNUSED",
				Message: fmt.Sprintf("resource value %q of type %q is never used -- %q has no 'drop' ability, so it must be moved exactly once", b.name, b.typ, b.typ),
				Pos:     b.pos,
			}
		case 1:
			// exactly-once: satisfies the linear-use rule.
		default:
			return &ResourceAbilityError{
				Code:    "E_RESOURCE_DOUBLE_USE",
				Message: fmt.Sprintf("resource value %q of type %q is used %d times -- %q has no 'copy' ability, so it may be moved at most once", b.name, b.typ, counts[b.name], b.typ),
				Pos:     b.pos,
			}
		}
	}
	return nil
}

// collectResourceLocalBindings recursively walks a statement list (including
// nested if/else/match-arm/while/for/do bodies -- flat, not scope-aware, per
// this pass's documented branch-insensitive design) collecting every
// StatementBinding whose RHS resolves to a @resource struct type.
func collectResourceLocalBindings(stmts []Statement, resourceNames map[string]bool, localTypes map[string]string, out *[]resourceBinding, functions map[string]*FunctionDecl) {
	for _, s := range stmts {
		if s.Kind == StatementBinding {
			if typ := bindingTypeName(s.Value, localTypes, functions); typ != "" && resourceNames[typ] {
				localTypes[s.Name] = typ
				*out = append(*out, resourceBinding{name: s.Name, typ: typ, pos: s.Pos})
			}
		}
		collectResourceLocalBindings(s.Then, resourceNames, localTypes, out, functions)
		collectResourceLocalBindings(s.Else, resourceNames, localTypes, out, functions)
		for _, arm := range s.Arms {
			collectResourceLocalBindings(arm.Body, resourceNames, localTypes, out, functions)
		}
	}
}

// bindingTypeName infers a local binding's static struct type name from its
// RHS expression, for the three shapes recognized: a struct literal
// (ExprStruct, whose Text carries the declared type name directly), a copy
// of an already-known local/param (bare ExprIdent looked up in localTypes),
// or a call to a declared function (ExprCall -- see resolveCallReturnTypeName;
// this closes a gap that existed independently of generics: a
// resource-returning real function call, e.g. `const t = mintToken(...)`,
// was never tracked by this pass at all before this case was added).
// Anything else resolves to "" (unknown/non-struct/non-resource), which
// this pass treats as "not a resource" -- conservative in the safe
// direction: it only ever flags bindings it can positively prove are
// @resource-typed.
func bindingTypeName(value Expr, localTypes map[string]string, functions map[string]*FunctionDecl) string {
	switch value.Kind {
	case ExprStruct:
		return value.Text
	case ExprIdent:
		return localTypes[value.Text]
	case ExprCall:
		return resolveCallReturnTypeName(value, functions)
	default:
		return ""
	}
}

// resolveCallReturnTypeName resolves a call expression's static return-type
// name for @resource tracking. Delegates to resolveUserFunction (compile.go),
// which is itself generics-aware (AVM generics v1 design, revised §1.3): for
// a generic call site (`const t = dup::<Token>(x)`), resolveUserFunction
// resolves through to the already-instantiated, concrete clone that
// typecheck's own inferExprType pass registered under its mangled name
// earlier in the SAME typecheck() call (CheckResourceAbilities runs last,
// deliberately, in typecheck's own sequencing) -- so by the time this runs,
// every call site's instantiation, if any, already exists. A miss (unknown
// callee, a call shape resolveUserFunction doesn't resolve at all such as a
// receiver-style call, or -- structurally impossible after a successful
// typecheck, but checked defensively -- a still-generic declaration slipping
// through) resolves to "": not positively provable as a resource type. This
// is the checker's existing, already-documented conservative-safe default
// (limitation 3 above, "no containment-safety check") -- this does not chase
// resource-ness through a generic container's OWN field types (e.g. whether
// Pair<Token,Token> itself contains a resource), which was already out of
// scope for a non-generic container and stays exactly as out of scope here.
func resolveCallReturnTypeName(call Expr, functions map[string]*FunctionDecl) string {
	if call.Kind != ExprCall || len(call.Path) != 1 {
		return ""
	}
	fn := resolveUserFunction(call, functions)
	if fn == nil || len(fn.TypeParams) > 0 {
		return ""
	}
	return fn.ReturnType.Name
}

// collectIdentRefCounts recursively walks a statement list and every
// expression reachable from it, counting how many times each bare name is
// referenced (an ExprIdent, or the root segment of a multi-segment
// ExprPath such as a field access `name.field`). Counting the root segment
// of a field-access path as "a use" is intentional: reading (or mutating a
// field of) a resource-typed value still requires holding/touching the
// whole value -- see limitation 5 in the CheckResourceAbilities doc
// comment.
func collectIdentRefCounts(stmts []Statement, counts map[string]int) {
	for _, s := range stmts {
		value := s.Value
		collectIdentRefCountsExpr(&value, counts)
		for i := range s.Args {
			collectIdentRefCountsExpr(&s.Args[i], counts)
		}
		start := s.Start
		collectIdentRefCountsExpr(&start, counts)
		end := s.End
		collectIdentRefCountsExpr(&end, counts)
		for _, e := range s.Extra {
			ex := e
			collectIdentRefCountsExpr(&ex, counts)
		}
		// s.Path (the assignment target of a `set` statement, e.g.
		// state.field or local.field) is walked as a plain root-name
		// reference too, matching limitation 5 above.
		if len(s.Path) > 0 {
			counts[s.Path[0]]++
		}
		collectIdentRefCounts(s.Then, counts)
		collectIdentRefCounts(s.Else, counts)
		for _, arm := range s.Arms {
			collectIdentRefCounts(arm.Body, counts)
		}
	}
}

// collectIdentRefCountsExpr is the Expr-tree half of collectIdentRefCounts.
// expr may be nil (e.g. a Statement with no Value) or the zero Expr (Kind
// == ""), both of which are no-ops.
func collectIdentRefCountsExpr(expr *Expr, counts map[string]int) {
	if expr == nil || expr.Kind == "" {
		return
	}
	switch expr.Kind {
	case ExprIdent:
		counts[expr.Text]++
	case ExprPath:
		if len(expr.Path) > 0 {
			counts[expr.Path[0]]++
		}
	}
	collectIdentRefCountsExpr(expr.Left, counts)
	collectIdentRefCountsExpr(expr.Right, counts)
	collectIdentRefCountsExpr(expr.Cond, counts)
	collectIdentRefCountsExpr(expr.Else, counts)
	for i := range expr.Args {
		collectIdentRefCountsExpr(&expr.Args[i], counts)
	}
	for i := range expr.Fields {
		collectIdentRefCountsExpr(&expr.Fields[i].Value, counts)
	}
}

// Integration note: CheckResourceAbilities(file) is called from
// (*Compiler).typecheck in compile.go, as the last check before typecheck
// returns -- after struct/enum/type validation, function/message/getter
// validation, and recursion validation have all already run, and before
// buildArtifacts/buildModule (codegen) starts. This makes @resource
// enforcement automatic on every Compile()/CompileFiles() call; it no longer
// needs to be invoked standalone (though it remains an exported function and
// resource_abilities_test.go still exercises it directly in some cases for
// isolation).
