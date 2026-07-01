package compiler

import (
	"encoding/hex"
	"strconv"
	"sort"
	"strings"
)

func FormatSource(file *SourceFile) string {
	if file == nil {
		return ""
	}
	var b strings.Builder
	if file.Package != "" {
		b.WriteString("package ")
		b.WriteString(file.Package)
		b.WriteString("\n\n")
	}
	for _, imp := range file.Imports {
		b.WriteString(formatImport(imp))
		b.WriteString("\n")
	}
	if len(file.Imports) > 0 {
		b.WriteString("\n")
	}
	for _, st := range file.Structs {
		b.WriteString(formatStructDecl(st))
		b.WriteString("\n\n")
	}
	for _, en := range file.Enums {
		b.WriteString(formatEnumDecl(en))
		b.WriteString("\n\n")
	}
	for _, fn := range file.Functions {
		b.WriteString(formatFunctionDecl(fn))
		b.WriteString("\n\n")
	}
	for _, contract := range file.Contracts {
		b.WriteString(formatContractDecl(contract))
		b.WriteString("\n\n")
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return ""
	}
	return out + "\n"
}

func FormatSourceNamed(fileName, src string) (string, error) {
	file, err := ParseSourceNamed(fileName, src)
	if err != nil {
		return "", err
	}
	return FormatSource(file), nil
}

func formatImport(imp ImportDecl) string {
	var b strings.Builder
	b.WriteString("import ")
	if strings.TrimSpace(imp.Alias) != "" {
		b.WriteString(imp.Alias)
		b.WriteString(" ")
	}
	b.WriteString(strconv.Quote(imp.Path))
	if strings.TrimSpace(imp.Version) != "" && imp.Version != "unversioned" {
		b.WriteString(" version ")
		b.WriteString(strconv.Quote(imp.Version))
	}
	b.WriteString(";")
	return b.String()
}

func formatStructDecl(st *StructDecl) string {
	if st == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("struct ")
	b.WriteString(st.Name)
	b.WriteString(" {\n")
	for i, field := range st.Fields {
		b.WriteString("  ")
		b.WriteString(field.Name)
		b.WriteString(": ")
		b.WriteString(formatTypeRef(field.Type))
		if field.Default.Kind != "" {
			b.WriteString(" = ")
			b.WriteString(formatExpr(field.Default, 0))
		}
		if i < len(st.Fields)-1 {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("}")
	return b.String()
}

func formatEnumDecl(en *EnumDecl) string {
	if en == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("enum ")
	b.WriteString(en.Name)
	b.WriteString(" {\n")
	for i, variant := range en.Variants {
		b.WriteString("  ")
		b.WriteString(variant.Name)
		if len(variant.Fields) > 0 {
			b.WriteString("(")
			for j, field := range variant.Fields {
				if j > 0 {
					b.WriteString(", ")
				}
				b.WriteString(field.Name)
				b.WriteString(": ")
				b.WriteString(formatTypeRef(field.Type))
			}
			b.WriteString(")")
		}
		if i < len(en.Variants)-1 {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("}")
	return b.String()
}

func formatFunctionDecl(fn *FunctionDecl) string {
	if fn == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("fn ")
	b.WriteString(fn.Name)
	b.WriteString(formatParamList(fn.Params))
	if fn.ReturnType.Name != "" {
		b.WriteString(" -> ")
		b.WriteString(formatTypeRef(fn.ReturnType))
	}
	b.WriteString(" ")
	b.WriteString(formatBlock(fn.Body, 0))
	return b.String()
}

func formatContractDecl(contract *ContractDecl) string {
	if contract == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("contract ")
	b.WriteString(contract.Name)
	b.WriteString(" {\n")
	if contract.StorageTypeName != "" {
		b.WriteString("  storage ")
		b.WriteString(contract.StorageTypeName)
		b.WriteString("\n")
	}
	if contract.Namespace != "" {
		b.WriteString("  namespace ")
		b.WriteString(strconv.Quote(contract.Namespace))
		b.WriteString("\n")
	}
	if contract.ChainID != "" {
		b.WriteString("  chain ")
		b.WriteString(strconv.Quote(contract.ChainID))
		b.WriteString("\n")
	}
	if contract.DeployerAddress != "" {
		b.WriteString("  deployer ")
		b.WriteString(strconv.Quote(contract.DeployerAddress))
		b.WriteString("\n")
	}
	if contract.Salt != "" {
		b.WriteString("  salt ")
		b.WriteString(strconv.Quote(contract.Salt))
		b.WriteString("\n")
	}
	if contract.InitialBalance != 0 {
		b.WriteString("  initial_balance ")
		b.WriteString(strconv.FormatUint(contract.InitialBalance, 10))
		b.WriteString("\n")
	}
	for _, msg := range contract.Messages {
		b.WriteString(formatMessageDecl(msg))
		b.WriteString("\n")
	}
	for _, get := range contract.Getters {
		b.WriteString(formatGetterDecl(get))
		b.WriteString("\n")
	}
	for _, event := range contract.Events {
		b.WriteString(formatEventDecl(event))
		b.WriteString("\n")
	}
	for _, wallet := range contract.WalletActions {
		b.WriteString(formatWalletActionDecl(wallet))
		b.WriteString("\n")
	}
	b.WriteString("}")
	return b.String()
}

func formatMessageDecl(msg *MessageDecl) string {
	if msg == nil {
		return ""
	}
	if msg.Kind == MessageKindDeploy {
		return "  deploy " + formatBlock(msg.Body, 1)
	}
	var b strings.Builder
	b.WriteString("  message ")
	b.WriteString(msg.Kind.String())
	b.WriteString(" ")
	b.WriteString(msg.Name)
	b.WriteString(formatParamList(msg.Params))
	if msg.ReturnType != nil && msg.ReturnType.Name != "" {
		b.WriteString(" -> ")
		b.WriteString(formatTypeRef(*msg.ReturnType))
	}
	if msg.ExplicitSel != nil {
		b.WriteString(" selector = ")
		b.WriteString(strconv.FormatUint(uint64(*msg.ExplicitSel), 10))
	}
	b.WriteString(" ")
	b.WriteString(formatBlock(msg.Body, 1))
	return b.String()
}

func formatGetterDecl(get *GetterDecl) string {
	if get == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("  getter ")
	b.WriteString(get.Name)
	b.WriteString(formatParamList(get.Params))
	if get.ReturnType.Name != "" {
		b.WriteString(" -> ")
		b.WriteString(formatTypeRef(get.ReturnType))
	}
	if get.ExplicitSel != nil {
		b.WriteString(" selector = ")
		b.WriteString(strconv.FormatUint(uint64(*get.ExplicitSel), 10))
	}
	b.WriteString(" ")
	b.WriteString(formatBlock(get.Body, 1))
	return b.String()
}

func formatEventDecl(event *EventDecl) string {
	if event == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("  event ")
	b.WriteString(event.Name)
	b.WriteString(formatParamList(event.FieldsToParams()))
	return b.String()
}

func formatWalletActionDecl(action *WalletActionDecl) string {
	if action == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("  wallet action ")
	b.WriteString(action.Name)
	b.WriteString(" {\n")
	writeWalletField(&b, "title", action.Title)
	writeWalletField(&b, "risk", action.Risk)
	writeWalletField(&b, "confirm_label", action.ConfirmLabel)
	writeWalletField(&b, "warning_level", action.WarningLevel)
	if len(action.ExpectedSideEffects) > 0 {
		b.WriteString("    expected_side_effects = [")
		for i, item := range action.ExpectedSideEffects {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Quote(item))
		}
		b.WriteString("]\n")
	}
	b.WriteString("    fund_access = ")
	b.WriteString(strconv.FormatBool(action.FundAccess))
	b.WriteString("\n")
	writeWalletField(&b, "approval_semantics", action.ApprovalSemantics)
	b.WriteString("  }")
	return b.String()
}

func writeWalletField(b *strings.Builder, name, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	b.WriteString("    ")
	b.WriteString(name)
	b.WriteString(" = ")
	b.WriteString(strconv.Quote(value))
	b.WriteString("\n")
}

func formatParamList(params []ParamDecl) string {
	var b strings.Builder
	b.WriteString("(")
	for i, param := range params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(param.Name)
		b.WriteString(": ")
		b.WriteString(formatTypeRef(param.Type))
	}
	b.WriteString(")")
	return b.String()
}

func formatBlock(stmts []Statement, indent int) string {
	var b strings.Builder
	b.WriteString("{\n")
	for _, stmt := range stmts {
		b.WriteString(strings.Repeat("  ", indent+1))
		b.WriteString(formatStatement(stmt, indent+1))
		b.WriteString("\n")
	}
	b.WriteString(strings.Repeat("  ", indent))
	b.WriteString("}")
	return b.String()
}

func formatStatement(stmt Statement, indent int) string {
	switch stmt.Kind {
	case StatementLet:
		return "let " + stmt.Name + " = " + formatExpr(stmt.Value, 0)
	case StatementSet:
		return "set " + strings.Join(stmt.Path, ".") + " = " + formatExpr(stmt.Value, 0)
	case StatementEmit:
		return "emit " + stmt.Name + formatExprList(stmt.Args)
	case StatementReturn:
		return "return " + formatExpr(stmt.Value, 0)
	case StatementRefund:
		return "refund " + formatExpr(stmt.Value, 0)
	case StatementSend:
		var b strings.Builder
		b.WriteString("send ")
		b.WriteString(formatExpr(stmt.Value, 0))
		b.WriteString(" to ")
		if len(stmt.Args) > 0 {
			b.WriteString(formatExpr(stmt.Args[0], 0))
		}
		if len(stmt.Extra) > 0 {
			keys := sortedKeys(stmt.Extra)
			for _, key := range keys {
				b.WriteString(" ")
				b.WriteString(key)
				b.WriteString(" = ")
				b.WriteString(formatExpr(stmt.Extra[key], 0))
			}
		}
		return b.String()
	case StatementSelf:
		var b strings.Builder
		b.WriteString("self ")
		b.WriteString(formatExpr(stmt.Value, 0))
		if len(stmt.Extra) > 0 {
			keys := sortedKeys(stmt.Extra)
			for _, key := range keys {
				b.WriteString(" ")
				b.WriteString(key)
				b.WriteString(" = ")
				b.WriteString(formatExpr(stmt.Extra[key], 0))
			}
		}
		return b.String()
	case StatementIf:
		var b strings.Builder
		b.WriteString("if ")
		b.WriteString(formatExpr(stmt.Value, 0))
		b.WriteString(" ")
		b.WriteString(formatBlock(stmt.Then, indent))
		if len(stmt.Else) > 0 {
			b.WriteString(" else ")
			b.WriteString(formatBlock(stmt.Else, indent))
		}
		return b.String()
	case StatementMatch:
		var b strings.Builder
		b.WriteString("match ")
		b.WriteString(formatExpr(stmt.Value, 0))
		b.WriteString(" {\n")
		for _, arm := range stmt.Arms {
			b.WriteString(strings.Repeat("  ", indent+1))
			if arm.Pattern.Kind == PatternWildcard {
				b.WriteString("_")
			} else {
				b.WriteString(arm.Pattern.Name)
				if len(arm.Pattern.Bindings) > 0 {
					b.WriteString("(")
					for i, bind := range arm.Pattern.Bindings {
						if i > 0 {
							b.WriteString(", ")
						}
						b.WriteString(bind)
					}
					b.WriteString(")")
				}
			}
			b.WriteString(" ")
			b.WriteString(formatBlock(arm.Body, indent+1))
			b.WriteString("\n")
		}
		b.WriteString(strings.Repeat("  ", indent))
		b.WriteString("}")
		return b.String()
	case StatementFor:
		return "for " + stmt.Index + " in " + formatExpr(stmt.Start, 0) + " to " + formatExpr(stmt.End, 0) + " " + formatBlock(stmt.Then, indent)
	default:
		return string(stmt.Kind)
	}
}

func formatExprList(exprs []Expr) string {
	if len(exprs) == 0 {
		return "()"
	}
	var b strings.Builder
	b.WriteString("(")
	for i, expr := range exprs {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(formatExpr(expr, 0))
	}
	b.WriteString(")")
	return b.String()
}

func formatTypeRef(t TypeRef) string {
	var b strings.Builder
	b.WriteString(t.Name)
	if len(t.Args) > 0 {
		b.WriteString("<")
		for i, arg := range t.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(formatTypeRef(arg))
		}
		b.WriteString(">")
	}
	if t.Optional {
		b.WriteString("?")
	}
	return b.String()
}

func formatExpr(expr Expr, parentPrec int) string {
	switch expr.Kind {
	case ExprNumber, ExprIdent:
		return expr.Text
	case ExprString:
		return strconv.Quote(expr.Text)
	case ExprBool:
		return strconv.FormatBool(expr.Bool)
	case ExprBytes:
		return "0x" + hex.EncodeToString(expr.Bytes)
	case ExprPath:
		return strings.Join(expr.Path, ".")
	case ExprNull:
		return "null"
	case ExprCall:
		name := expr.Text
		if name == "" {
			name = strings.Join(expr.Path, ".")
		}
		return name + formatExprList(expr.Args)
	case ExprTry:
		out := "try " + formatExpr(*expr.Left, 0)
		if expr.Else != nil {
			out += " else " + formatExpr(*expr.Else, 0)
		}
		return out
	case ExprBinary, ExprCompare, ExprLogic:
		prec := exprPrecedence(expr.Kind)
		left := formatExpr(*expr.Left, prec)
		right := formatExpr(*expr.Right, prec+1)
		out := left + " " + expr.Op + " " + right
		if prec < parentPrec {
			return "(" + out + ")"
		}
		return out
	default:
		if expr.Text != "" {
			return expr.Text
		}
		return string(expr.Kind)
	}
}

func exprPrecedence(kind ExprKind) int {
	switch kind {
	case ExprLogic:
		return 1
	case ExprCompare:
		return 2
	case ExprBinary:
		return 3
	default:
		return 4
	}
}

func sortedKeys(values map[string]Expr) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
