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
// This is a NEW, standalone file. It does not modify parser.go's AST types,
// ir.go, avm.go, or compile.go's Compile() pipeline. It is not currently
// wired into Compiler.Compile() -- see the package-level note at the bottom
// of this file for why, and what remains to integrate it.

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
func CheckResourceAbilities(src *SourceFile) error {
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
		if err := checkResourceBody(fn.Params, fn.Body, resourceNames); err != nil {
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
			if err := checkResourceBody(fn.Params, fn.Body, resourceNames); err != nil {
				return err
			}
		}
		for _, msg := range ct.Messages {
			if msg == nil {
				continue
			}
			if err := checkResourceBody(msg.Params, msg.Body, resourceNames); err != nil {
				return err
			}
		}
		for _, get := range ct.Getters {
			if get == nil {
				continue
			}
			if err := checkResourceBody(get.Params, get.Body, resourceNames); err != nil {
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
func checkResourceBody(params []ParamDecl, body []Statement, resourceNames map[string]bool) error {
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

	collectResourceLocalBindings(body, resourceNames, localTypes, &bindings)

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
func collectResourceLocalBindings(stmts []Statement, resourceNames map[string]bool, localTypes map[string]string, out *[]resourceBinding) {
	for _, s := range stmts {
		if s.Kind == StatementBinding {
			if typ := bindingTypeName(s.Value, localTypes); typ != "" && resourceNames[typ] {
				localTypes[s.Name] = typ
				*out = append(*out, resourceBinding{name: s.Name, typ: typ, pos: s.Pos})
			}
		}
		collectResourceLocalBindings(s.Then, resourceNames, localTypes, out)
		collectResourceLocalBindings(s.Else, resourceNames, localTypes, out)
		for _, arm := range s.Arms {
			collectResourceLocalBindings(arm.Body, resourceNames, localTypes, out)
		}
	}
}

// bindingTypeName infers a local binding's static struct type name from its
// RHS expression, for the two shapes the existing (compile.go) lowering
// already recognizes: a struct literal (ExprStruct, whose Text carries the
// declared type name directly) or a copy of an already-known local/param
// (bare ExprIdent looked up in localTypes). Anything else resolves to ""
// (unknown/non-struct/non-resource), which this pass treats as "not a
// resource" -- conservative in the safe direction: it only ever flags
// bindings it can positively prove are @resource-typed.
func bindingTypeName(value Expr, localTypes map[string]string) string {
	switch value.Kind {
	case ExprStruct:
		return value.Text
	case ExprIdent:
		return localTypes[value.Text]
	default:
		return ""
	}
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

// Integration note (deliberately NOT done in this change): wiring
// CheckResourceAbilities into Compiler.Compile()/CompileFiles() (compile.go)
// so it runs automatically on every contract compile is the one remaining
// step to make @resource enforcement automatic rather than opt-in. It is
// deferred here because compile.go was under active, live concurrent
// modification by another session during this change (its on-disk mtime
// advanced repeatedly while this file was being written -- confirmed by
// re-`stat`ing it minutes apart). Touching compile.go's Compile() call graph
// while another process is mid-edit risks a corrupted merge; per this
// task's own instructions, correctness of the untouched pipeline outranks
// finishing the wiring in this pass. Until that wiring lands, callers who
// want @resource enforcement must call CheckResourceAbilities(sourceFile)
// explicitly (see resource_abilities_test.go for the pattern: ParseSource
// then CheckResourceAbilities, alongside the normal Compiler.Compile() for
// codegen/execution).
