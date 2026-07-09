package compiler

import (
	"fmt"
	"path/filepath"
	"strings"
)

type legacyTypeAlias struct {
	Name        string
	Replacement string
	Arity       int
	Ambiguous   bool
}

var legacyTypeAliases = map[string]legacyTypeAlias{
	"ref": {
		Name:        "Ref",
		Replacement: "ChunkRef",
		Arity:       1,
		Ambiguous:   true,
	},
}

func (c *Compiler) collectCompatibilityDiagnostics(sources []NamedSource, _ *SourceFile) ([]Diagnostic, error) {
	var diags []Diagnostic
	for _, src := range sources {
		diag, err := sourceCompatibilityDiagnostic(src.Name, c.opts.SurfaceCompatibility)
		if err != nil {
			return nil, err
		}
		if diag != nil {
			diags = append(diags, *diag)
		}
	}
	return diags, nil
}

func sourceCompatibilityDiagnostic(name string, mode SurfaceCompatibilityMode) (*Diagnostic, error) {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name)))
	switch ext {
	case ".atlx", "":
		return nil, nil
	case ".avm":
		diag := Diagnostic{
			Severity: SeverityWarning,
			Code:     "W_LEGACY_EXTENSION",
			Message:  fmt.Sprintf("source file %q uses legacy .avm extension; use .atlx", name),
			Pos:      Position{File: name},
		}
		if mode == SurfaceCompatibilityStrict {
			diag.Severity = SeverityError
			return nil, &CompileError{Diagnostics: []Diagnostic{diag}}
		}
		return &diag, nil
	default:
		return nil, &CompileError{Diagnostics: []Diagnostic{{
			Severity: SeverityError,
			Code:     "E_SOURCE_EXTENSION",
			Message:  fmt.Sprintf("source file %q must use .atlx extension", name),
			Pos:      Position{File: name},
		}}}
	}
}

func normalizeSourceFile(file *SourceFile, mode SurfaceCompatibilityMode) ([]Diagnostic, error) {
	if file == nil {
		return nil, nil
	}
	var diags []Diagnostic
	for _, st := range file.Structs {
		if st == nil {
			continue
		}
		for i, field := range st.Fields {
			normalized, fieldDiags, err := normalizeTypeRef(field.Type, mode)
			if err != nil {
				return nil, err
			}
			diags = append(diags, fieldDiags...)
			st.Fields[i].Type = normalized
			if st.Fields[i].Default.Kind != "" {
				def, defDiags, err := normalizeExpr(st.Fields[i].Default, mode)
				if err != nil {
					return nil, err
				}
				diags = append(diags, defDiags...)
				st.Fields[i].Default = def
			}
		}
	}
	for _, en := range file.Enums {
		if en == nil {
			continue
		}
		for vi, variant := range en.Variants {
			for fi, field := range variant.Fields {
				normalized, fieldDiags, err := normalizeTypeRef(field.Type, mode)
				if err != nil {
					return nil, err
				}
				diags = append(diags, fieldDiags...)
				en.Variants[vi].Fields[fi].Type = normalized
			}
		}
	}
	for _, fn := range file.Functions {
		if fn == nil {
			continue
		}
		for i, param := range fn.Params {
			normalized, paramDiags, err := normalizeTypeRef(param.Type, mode)
			if err != nil {
				return nil, err
			}
			diags = append(diags, paramDiags...)
			fn.Params[i].Type = normalized
		}
		normalized, retDiags, err := normalizeTypeRef(fn.ReturnType, mode)
		if err != nil {
			return nil, err
		}
		diags = append(diags, retDiags...)
		fn.ReturnType = normalized
	}
	for _, contract := range file.Contracts {
		if contract == nil {
			continue
		}
		for _, msg := range contract.Messages {
			if msg == nil {
				continue
			}
			for i, param := range msg.Params {
				normalized, paramDiags, err := normalizeTypeRef(param.Type, mode)
				if err != nil {
					return nil, err
				}
				diags = append(diags, paramDiags...)
				msg.Params[i].Type = normalized
			}
			if msg.ReturnType != nil {
				normalized, retDiags, err := normalizeTypeRef(*msg.ReturnType, mode)
				if err != nil {
					return nil, err
				}
				diags = append(diags, retDiags...)
				msg.ReturnType = &normalized
			}
		}
		for _, get := range contract.Getters {
			if get == nil {
				continue
			}
			for i, param := range get.Params {
				normalized, paramDiags, err := normalizeTypeRef(param.Type, mode)
				if err != nil {
					return nil, err
				}
				diags = append(diags, paramDiags...)
				get.Params[i].Type = normalized
			}
			normalized, retDiags, err := normalizeTypeRef(get.ReturnType, mode)
			if err != nil {
				return nil, err
			}
			diags = append(diags, retDiags...)
			get.ReturnType = normalized
		}
		for _, wallet := range contract.WalletActions {
			if wallet == nil {
				continue
			}
			for i, param := range wallet.Inputs {
				normalized, paramDiags, err := normalizeTypeRef(param.Type, mode)
				if err != nil {
					return nil, err
				}
				diags = append(diags, paramDiags...)
				wallet.Inputs[i].Type = normalized
			}
			for i, param := range wallet.Outputs {
				normalized, paramDiags, err := normalizeTypeRef(param.Type, mode)
				if err != nil {
					return nil, err
				}
				diags = append(diags, paramDiags...)
				wallet.Outputs[i].Type = normalized
			}
		}
	}
	return diags, nil
}

func normalizeExpr(expr Expr, mode SurfaceCompatibilityMode) (Expr, []Diagnostic, error) {
	var diags []Diagnostic
	switch expr.Kind {
	case ExprCall:
		for i, arg := range expr.Args {
			normalized, argDiags, err := normalizeExpr(arg, mode)
			if err != nil {
				return Expr{}, nil, err
			}
			diags = append(diags, argDiags...)
			expr.Args[i] = normalized
		}
	case ExprStruct:
		for i, field := range expr.Fields {
			normalized, fieldDiags, err := normalizeExpr(field.Value, mode)
			if err != nil {
				return Expr{}, nil, err
			}
			diags = append(diags, fieldDiags...)
			expr.Fields[i].Value = normalized
		}
	case ExprBinary, ExprCompare, ExprLogic:
		if expr.Left != nil {
			normalized, leftDiags, err := normalizeExpr(*expr.Left, mode)
			if err != nil {
				return Expr{}, nil, err
			}
			diags = append(diags, leftDiags...)
			expr.Left = &normalized
		}
		if expr.Right != nil {
			normalized, rightDiags, err := normalizeExpr(*expr.Right, mode)
			if err != nil {
				return Expr{}, nil, err
			}
			diags = append(diags, rightDiags...)
			expr.Right = &normalized
		}
	case ExprTry:
		if expr.Left != nil {
			normalized, leftDiags, err := normalizeExpr(*expr.Left, mode)
			if err != nil {
				return Expr{}, nil, err
			}
			diags = append(diags, leftDiags...)
			expr.Left = &normalized
		}
		if expr.Else != nil {
			normalized, elseDiags, err := normalizeExpr(*expr.Else, mode)
			if err != nil {
				return Expr{}, nil, err
			}
			diags = append(diags, elseDiags...)
			expr.Else = &normalized
		}
	}
	return expr, diags, nil
}

func normalizeTypeRef(typ TypeRef, mode SurfaceCompatibilityMode) (TypeRef, []Diagnostic, error) {
	var diags []Diagnostic
	normalized := typ
	if alias, ok := legacyTypeAliases[strings.ToLower(strings.TrimSpace(typ.Name))]; ok {
		diag := Diagnostic{
			Severity: SeverityWarning,
			Code:     "W_LEGACY_ALIAS",
			Message:  legacyAliasMessage(alias),
			Pos:      typ.Pos,
		}
		if mode == SurfaceCompatibilityStrict {
			diag.Severity = SeverityError
			return TypeRef{}, nil, &CompileError{Diagnostics: []Diagnostic{diag}}
		}
		diags = append(diags, diag)
		normalized.Name = alias.Replacement
	}
	for i, arg := range normalized.Args {
		next, argDiags, err := normalizeTypeRef(arg, mode)
		if err != nil {
			return TypeRef{}, nil, err
		}
		diags = append(diags, argDiags...)
		normalized.Args[i] = next
	}
	return normalized, diags, nil
}

func legacyAliasMessage(alias legacyTypeAlias) string {
	switch strings.ToLower(alias.Name) {
	case "ref":
		return "Ref<T> is deprecated; use ChunkRef<T> or ChunkLink<T> explicitly based on ownership semantics"
	default:
		return fmt.Sprintf("%s is deprecated; use %s", alias.Name, alias.Replacement)
	}
}
