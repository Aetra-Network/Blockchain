package compiler

import (
	"fmt"
	"strconv"
	"strings"
)

type parser struct {
	lex *lexer
	cur token
	err error
}

func ParseSource(src string) (*SourceFile, error) {
	return ParseSourceNamed("", src)
}

func ParseSourceNamed(fileName, src string) (*SourceFile, error) {
	p := &parser{lex: newLexer(fileName, src)}
	if err := p.read(); err != nil {
		return nil, err
	}
	file := &SourceFile{}
	for p.cur.kind != tokenEOF && p.err == nil {
		switch p.cur.text {
		case "package":
			if file.Package != "" {
				return nil, fmt.Errorf("duplicate package declaration at %s", p.cur.pos)
			}
			name, err := p.parsePackageDecl()
			if err != nil {
				return nil, err
			}
			file.Package = name
		case "import":
			imp, err := p.parseImportDecl()
			if err != nil {
				return nil, err
			}
			file.Imports = append(file.Imports, imp)
		case "struct":
			st, err := p.parseStruct()
			if err != nil {
				return nil, err
			}
			file.Structs = append(file.Structs, st)
		case "enum":
			en, err := p.parseEnum()
			if err != nil {
				return nil, err
			}
			file.Enums = append(file.Enums, en)
		case "fn":
			fn, err := p.parseFunction()
			if err != nil {
				return nil, err
			}
			file.Functions = append(file.Functions, fn)
		case "contract":
			ct, err := p.parseContract()
			if err != nil {
				return nil, err
			}
			file.Contracts = append(file.Contracts, ct)
		default:
			return nil, fmt.Errorf("unexpected top-level declaration %q at %s", p.cur.text, p.cur.pos)
		}
	}
	if p.err != nil {
		return nil, p.err
	}
	return file, nil
}

func (p *parser) parseFunction() (*FunctionDecl, error) {
	pos := p.cur.pos
	if err := p.expectIdent("fn"); err != nil {
		return nil, err
	}
	name, err := p.expectName()
	if err != nil {
		return nil, err
	}
	params, ret, body, err := p.parseFunctionTail()
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return nil, fmt.Errorf("function %q requires a return type at %s", name, pos)
	}
	return &FunctionDecl{Name: name, Params: params, ReturnType: *ret, Body: body, Pos: pos}, nil
}

func (p *parser) parsePackageDecl() (string, error) {
	if err := p.expectIdent("package"); err != nil {
		return "", err
	}
	path, err := p.parsePath()
	if err != nil {
		return "", err
	}
	if p.cur.kind == tokenSemicolon {
		if err := p.read(); err != nil {
			return "", err
		}
	}
	return joinPath(path), nil
}

func (p *parser) parseImportDecl() (ImportDecl, error) {
	pos := p.cur.pos
	if err := p.expectIdent("import"); err != nil {
		return ImportDecl{}, err
	}
	imp := ImportDecl{Pos: pos}
	if p.cur.kind == tokenIdent {
		next := p.cur
		if err := p.read(); err != nil {
			return ImportDecl{}, err
		}
		if p.cur.kind == tokenString {
			imp.Alias = next.text
		} else {
			return ImportDecl{}, fmt.Errorf("expected import path after alias %q at %s", next.text, p.cur.pos)
		}
	}
	path, err := p.expectString()
	if err != nil {
		return ImportDecl{}, err
	}
	imp.Path, imp.Version = splitImportPathVersion(path)
	if p.cur.kind == tokenIdent && p.cur.text == "version" {
		if err := p.read(); err != nil {
			return ImportDecl{}, err
		}
		version, err := p.expectString()
		if err != nil {
			return ImportDecl{}, err
		}
		imp.Version = version
	}
	if p.cur.kind == tokenIdent && p.cur.text == "as" {
		if err := p.read(); err != nil {
			return ImportDecl{}, err
		}
		alias, err := p.expectName()
		if err != nil {
			return ImportDecl{}, err
		}
		imp.Alias = alias
	}
	if p.cur.kind == tokenSemicolon {
		if err := p.read(); err != nil {
			return ImportDecl{}, err
		}
	}
	if imp.Path == "" {
		return ImportDecl{}, fmt.Errorf("empty import path at %s", pos)
	}
	if imp.Version == "" {
		imp.Version = "unversioned"
	}
	return imp, nil
}

func (p *parser) parseFunctionTail() ([]ParamDecl, *TypeRef, []Statement, error) {
	params, err := p.parseParamList()
	if err != nil {
		return nil, nil, nil, err
	}
	var ret *TypeRef
	if p.cur.kind == tokenArrow {
		if err := p.read(); err != nil {
			return nil, nil, nil, err
		}
		typ, err := p.parseTypeRef()
		if err != nil {
			return nil, nil, nil, err
		}
		ret = &typ
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, nil, nil, err
	}
	return params, ret, body, nil
}

func (p *parser) parseStruct() (*StructDecl, error) {
	pos := p.cur.pos
	if err := p.expectIdent("struct"); err != nil {
		return nil, err
	}
	name, err := p.expectName()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tokenLBrace); err != nil {
		return nil, err
	}
	var fields []FieldDecl
	for p.cur.kind != tokenRBrace {
		if p.cur.kind == tokenEOF {
			return nil, fmt.Errorf("unterminated struct %q starting at %s", name, pos)
		}
		fieldName, err := p.expectName()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tokenColon); err != nil {
			return nil, err
		}
		typ, err := p.parseTypeRef()
		if err != nil {
			return nil, err
		}
		var def Expr
		if p.cur.kind == tokenEqual {
			if err := p.read(); err != nil {
				return nil, err
			}
			expr, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			def = expr
		}
		fields = append(fields, FieldDecl{Name: fieldName, Type: typ, Default: def, Pos: p.cur.pos})
		if p.cur.kind == tokenSemicolon {
			if err := p.read(); err != nil {
				return nil, err
			}
		}
	}
	if err := p.expect(tokenRBrace); err != nil {
		return nil, err
	}
	return &StructDecl{Name: name, Fields: fields, Pos: pos}, nil
}

func (p *parser) parseEnum() (*EnumDecl, error) {
	pos := p.cur.pos
	if err := p.expectIdent("enum"); err != nil {
		return nil, err
	}
	name, err := p.expectName()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tokenLBrace); err != nil {
		return nil, err
	}
	var variants []VariantDecl
	for p.cur.kind != tokenRBrace {
		if p.cur.kind == tokenEOF {
			return nil, fmt.Errorf("unterminated enum %q starting at %s", name, pos)
		}
		varName, err := p.expectName()
		if err != nil {
			return nil, err
		}
		var fields []FieldDecl
		if p.cur.kind == tokenLParen {
			if err := p.read(); err != nil {
				return nil, err
			}
			if p.cur.kind != tokenRParen {
				for {
					fname, err := p.expectName()
					if err != nil {
						return nil, err
					}
					if err := p.expect(tokenColon); err != nil {
						return nil, err
					}
					typ, err := p.parseTypeRef()
					if err != nil {
						return nil, err
					}
					fields = append(fields, FieldDecl{Name: fname, Type: typ, Pos: p.cur.pos})
					if p.cur.kind != tokenComma {
						break
					}
					if err := p.read(); err != nil {
						return nil, err
					}
				}
			}
			if err := p.expect(tokenRParen); err != nil {
				return nil, err
			}
		}
		variants = append(variants, VariantDecl{Name: varName, Fields: fields, Pos: p.cur.pos})
		if p.cur.kind == tokenSemicolon {
			if err := p.read(); err != nil {
				return nil, err
			}
		}
	}
	if err := p.expect(tokenRBrace); err != nil {
		return nil, err
	}
	return &EnumDecl{Name: name, Variants: variants, Pos: pos}, nil
}

func (p *parser) parseContract() (*ContractDecl, error) {
	pos := p.cur.pos
	if err := p.expectIdent("contract"); err != nil {
		return nil, err
	}
	name, err := p.expectName()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tokenLBrace); err != nil {
		return nil, err
	}
	ct := &ContractDecl{Name: name, Pos: pos, StorageDefaults: map[string]Expr{}}
	for p.cur.kind != tokenRBrace {
		if p.cur.kind == tokenEOF {
			return nil, fmt.Errorf("unterminated contract %q starting at %s", name, pos)
		}
		switch p.cur.text {
		case "storage":
			if err := p.read(); err != nil {
				return nil, err
			}
			stype, err := p.parseTypeRef()
			if err != nil {
				return nil, err
			}
			ct.StorageTypeName = stype.String()
		case "namespace":
			if err := p.read(); err != nil {
				return nil, err
			}
			v, err := p.expectString()
			if err != nil {
				return nil, err
			}
			ct.Namespace = v
		case "chain":
			if err := p.read(); err != nil {
				return nil, err
			}
			v, err := p.expectString()
			if err != nil {
				return nil, err
			}
			ct.ChainID = v
		case "deployer":
			if err := p.read(); err != nil {
				return nil, err
			}
			v, err := p.expectString()
			if err != nil {
				return nil, err
			}
			ct.DeployerAddress = v
		case "salt":
			if err := p.read(); err != nil {
				return nil, err
			}
			v, err := p.expectString()
			if err != nil {
				return nil, err
			}
			ct.Salt = v
		case "initial_balance":
			if err := p.read(); err != nil {
				return nil, err
			}
			v, err := p.expectNumberUint64()
			if err != nil {
				return nil, err
			}
			ct.InitialBalance = v
		case "message":
			msg, err := p.parseMessage()
			if err != nil {
				return nil, err
			}
			ct.Messages = append(ct.Messages, msg)
		case "getter":
			get, err := p.parseGetter()
			if err != nil {
				return nil, err
			}
			ct.Getters = append(ct.Getters, get)
		case "event":
			event, err := p.parseEvent()
			if err != nil {
				return nil, err
			}
			ct.Events = append(ct.Events, event)
		case "wallet":
			act, err := p.parseWalletAction()
			if err != nil {
				return nil, err
			}
			ct.WalletActions = append(ct.WalletActions, act)
		case "deploy":
			msg, err := p.parseDeployBlock()
			if err != nil {
				return nil, err
			}
			ct.Messages = append(ct.Messages, msg)
		default:
			return nil, fmt.Errorf("unexpected contract item %q at %s", p.cur.text, p.cur.pos)
		}
	}
	if err := p.expect(tokenRBrace); err != nil {
		return nil, err
	}
	return ct, nil
}

func (p *parser) parseMessage() (*MessageDecl, error) {
	pos := p.cur.pos
	if err := p.expectIdent("message"); err != nil {
		return nil, err
	}
	kind := MessageKindExternal
	if p.cur.kind == tokenIdent {
		switch MessageKind(p.cur.text) {
		case MessageKindExternal, MessageKindInternal, MessageKindBounced, MessageKindDeploy, MessageKindMigrate:
			kind = MessageKind(p.cur.text)
			if err := p.read(); err != nil {
				return nil, err
			}
		}
	}
	name, err := p.expectName()
	if err != nil {
		return nil, err
	}
	params, ret, body, selector, err := p.parseCallableTail(true)
	if err != nil {
		return nil, err
	}
	return &MessageDecl{Name: name, Kind: kind, Params: params, ReturnType: ret, Body: body, ExplicitSel: selector, Pos: pos}, nil
}

func (p *parser) parseDeployBlock() (*MessageDecl, error) {
	pos := p.cur.pos
	if err := p.expectIdent("deploy"); err != nil {
		return nil, err
	}
	body, err := p.parseBlock()
	if err != nil {
		return nil, err
	}
	return &MessageDecl{Name: "Deploy", Kind: MessageKindDeploy, Body: body, Pos: pos}, nil
}

func (p *parser) parseGetter() (*GetterDecl, error) {
	pos := p.cur.pos
	if err := p.expectIdent("getter"); err != nil {
		return nil, err
	}
	name, err := p.expectName()
	if err != nil {
		return nil, err
	}
	params, ret, body, selector, err := p.parseCallableTail(false)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return nil, fmt.Errorf("getter %q requires a return type at %s", name, pos)
	}
	return &GetterDecl{Name: name, Params: params, ReturnType: *ret, Body: body, ExplicitSel: selector, Pos: pos}, nil
}

func (p *parser) parseEvent() (*EventDecl, error) {
	pos := p.cur.pos
	if err := p.expectIdent("event"); err != nil {
		return nil, err
	}
	name, err := p.expectName()
	if err != nil {
		return nil, err
	}
	fields, err := p.parseParamList()
	if err != nil {
		return nil, err
	}
	if p.cur.kind == tokenSemicolon {
		if err := p.read(); err != nil {
			return nil, err
		}
	}
	return &EventDecl{Name: name, Fields: fields, Pos: pos}, nil
}

func (p *parser) parseWalletAction() (*WalletActionDecl, error) {
	pos := p.cur.pos
	if err := p.expectIdent("wallet"); err != nil {
		return nil, err
	}
	if err := p.expectIdent("action"); err != nil {
		return nil, err
	}
	name, err := p.expectName()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tokenLBrace); err != nil {
		return nil, err
	}
	act := &WalletActionDecl{Name: name, Pos: pos}
	for p.cur.kind != tokenRBrace {
		if p.cur.kind == tokenEOF {
			return nil, fmt.Errorf("unterminated wallet action %q starting at %s", name, pos)
		}
		key, err := p.expectName()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tokenEqual); err != nil {
			return nil, err
		}
		switch key {
		case "title":
			act.Title, err = p.expectString()
		case "risk":
			act.Risk, err = p.expectString()
		case "confirm_label":
			act.ConfirmLabel, err = p.expectString()
		case "warning_level":
			act.WarningLevel, err = p.expectString()
		case "expected_side_effects":
			act.ExpectedSideEffects, err = p.expectStringList()
		case "fund_access":
			act.FundAccess, err = p.expectBool()
		case "approval_semantics":
			act.ApprovalSemantics, err = p.expectString()
		default:
			err = fmt.Errorf("unknown wallet action field %q at %s", key, p.cur.pos)
		}
		if err != nil {
			return nil, err
		}
		if p.cur.kind == tokenSemicolon {
			if err := p.read(); err != nil {
				return nil, err
			}
		}
	}
	if err := p.expect(tokenRBrace); err != nil {
		return nil, err
	}
	return act, nil
}

func (p *parser) parseCallableTail(requireReturn bool) ([]ParamDecl, *TypeRef, []Statement, *uint32, error) {
	params, err := p.parseParamList()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var ret *TypeRef
	if p.cur.kind == tokenArrow {
		if err := p.read(); err != nil {
			return nil, nil, nil, nil, err
		}
		typ, err := p.parseTypeRef()
		if err != nil {
			return nil, nil, nil, nil, err
		}
		ret = &typ
	} else if requireReturn {
		ret = nil
	}
	var selector *uint32
	for p.cur.kind == tokenIdent {
		switch p.cur.text {
		case "selector":
			if err := p.read(); err != nil {
				return nil, nil, nil, nil, err
			}
			if err := p.expect(tokenEqual); err != nil {
				return nil, nil, nil, nil, err
			}
			value, err := p.expectNumberUint64()
			if err != nil {
				return nil, nil, nil, nil, err
			}
			selector = new(uint32)
			*selector = uint32(value)
		default:
			goto body
		}
	}
body:
	body, err := p.parseBlock()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return params, ret, body, selector, nil
}

func (p *parser) parseParamList() ([]ParamDecl, error) {
	if err := p.expect(tokenLParen); err != nil {
		return nil, err
	}
	var params []ParamDecl
	if p.cur.kind != tokenRParen {
		for {
			name, err := p.expectName()
			if err != nil {
				return nil, err
			}
			if err := p.expect(tokenColon); err != nil {
				return nil, err
			}
			typ, err := p.parseTypeRef()
			if err != nil {
				return nil, err
			}
			params = append(params, ParamDecl{Name: name, Type: typ, Pos: p.cur.pos})
			if p.cur.kind != tokenComma {
				break
			}
			if err := p.read(); err != nil {
				return nil, err
			}
		}
	}
	if err := p.expect(tokenRParen); err != nil {
		return nil, err
	}
	return params, nil
}

func (p *parser) parseBlock() ([]Statement, error) {
	if err := p.expect(tokenLBrace); err != nil {
		return nil, err
	}
	var stmts []Statement
	for p.cur.kind != tokenRBrace {
		if p.cur.kind == tokenEOF {
			return nil, fmt.Errorf("unterminated block starting at %s", p.cur.pos)
		}
		stmt, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, stmt)
		if p.cur.kind == tokenSemicolon {
			if err := p.read(); err != nil {
				return nil, err
			}
		}
	}
	if err := p.expect(tokenRBrace); err != nil {
		return nil, err
	}
	return stmts, nil
}

func (p *parser) parseStatement() (Statement, error) {
	pos := p.cur.pos
	switch p.cur.text {
	case "let":
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		name, err := p.expectName()
		if err != nil {
			return Statement{}, err
		}
		if err := p.expect(tokenEqual); err != nil {
			return Statement{}, err
		}
		expr, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		return Statement{Kind: StatementLet, Name: name, Value: expr, Pos: pos}, nil
	case "set":
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		path, err := p.parsePath()
		if err != nil {
			return Statement{}, err
		}
		if err := p.expect(tokenEqual); err != nil {
			return Statement{}, err
		}
		expr, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		return Statement{Kind: StatementSet, Path: path, Value: expr, Pos: pos}, nil
	case "emit":
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		name, err := p.expectName()
		if err != nil {
			return Statement{}, err
		}
		args, err := p.parseExprList()
		if err != nil {
			return Statement{}, err
		}
		return Statement{Kind: StatementEmit, Name: name, Args: args, Pos: pos}, nil
	case "return":
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		expr, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		return Statement{Kind: StatementReturn, Value: expr, Pos: pos}, nil
	case "refund":
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		expr, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		return Statement{Kind: StatementRefund, Value: expr, Pos: pos}, nil
	case "send":
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		value, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		if err := p.expectIdent("to"); err != nil {
			return Statement{}, err
		}
		target, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		extra := map[string]Expr{}
		for p.cur.kind == tokenIdent {
			key := p.cur.text
			if err := p.read(); err != nil {
				return Statement{}, err
			}
			if err := p.expect(tokenEqual); err != nil {
				return Statement{}, err
			}
			expr, err := p.parseExpr()
			if err != nil {
				return Statement{}, err
			}
			extra[key] = expr
		}
		return Statement{Kind: StatementSend, Value: value, Args: []Expr{target}, Extra: extra, Pos: pos}, nil
	case "self":
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		value, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		extra := map[string]Expr{}
		for p.cur.kind == tokenIdent {
			key := p.cur.text
			if err := p.read(); err != nil {
				return Statement{}, err
			}
			if err := p.expect(tokenEqual); err != nil {
				return Statement{}, err
			}
			expr, err := p.parseExpr()
			if err != nil {
				return Statement{}, err
			}
			extra[key] = expr
		}
		return Statement{Kind: StatementSelf, Value: value, Extra: extra, Pos: pos}, nil
	case "if":
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		cond, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		thenBody, err := p.parseBlock()
		if err != nil {
			return Statement{}, err
		}
		var elseBody []Statement
		if p.cur.kind == tokenIdent && p.cur.text == "else" {
			if err := p.read(); err != nil {
				return Statement{}, err
			}
			elseBody, err = p.parseBlock()
			if err != nil {
				return Statement{}, err
			}
		}
		return Statement{Kind: StatementIf, Value: cond, Then: thenBody, Else: elseBody, Pos: pos}, nil
	case "match":
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		scrutinee, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		arms, err := p.parseMatchArms()
		if err != nil {
			return Statement{}, err
		}
		return Statement{Kind: StatementMatch, Value: scrutinee, Arms: arms, Pos: pos}, nil
	case "for":
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		index, err := p.expectName()
		if err != nil {
			return Statement{}, err
		}
		if err := p.expectIdent("in"); err != nil {
			return Statement{}, err
		}
		start, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		if err := p.expectIdent("to"); err != nil {
			return Statement{}, err
		}
		end, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		body, err := p.parseBlock()
		if err != nil {
			return Statement{}, err
		}
		return Statement{Kind: StatementFor, Index: index, Start: start, End: end, Then: body, Pos: pos}, nil
	default:
		return Statement{}, fmt.Errorf("unexpected statement %q at %s", p.cur.text, p.cur.pos)
	}
}

func (p *parser) parseMatchArms() ([]MatchArm, error) {
	if err := p.expect(tokenLBrace); err != nil {
		return nil, err
	}
	var arms []MatchArm
	for p.cur.kind != tokenRBrace {
		if p.cur.kind == tokenEOF {
			return nil, fmt.Errorf("unterminated match starting at %s", p.cur.pos)
		}
		if p.cur.kind == tokenIdent && p.cur.text == "case" {
			if err := p.read(); err != nil {
				return nil, err
			}
		}
		pattern, err := p.parsePattern()
		if err != nil {
			return nil, err
		}
		body, err := p.parseBlock()
		if err != nil {
			return nil, err
		}
		arms = append(arms, MatchArm{Pattern: pattern, Body: body, Pos: pattern.Pos})
	}
	if err := p.expect(tokenRBrace); err != nil {
		return nil, err
	}
	return arms, nil
}

func (p *parser) parsePattern() (Pattern, error) {
	pos := p.cur.pos
	if p.cur.kind != tokenIdent {
		return Pattern{}, fmt.Errorf("expected pattern at %s, got %q", p.cur.pos, p.cur.text)
	}
	if p.cur.text == "_" {
		if err := p.read(); err != nil {
			return Pattern{}, err
		}
		return Pattern{Kind: PatternWildcard, Pos: pos}, nil
	}
	path, err := p.parsePath()
	if err != nil {
		return Pattern{}, err
	}
	name := joinPath(path)
	var binds []string
	if p.cur.kind == tokenLParen {
		if err := p.read(); err != nil {
			return Pattern{}, err
		}
		if p.cur.kind != tokenRParen {
			for {
				bind, err := p.expectName()
				if err != nil {
					return Pattern{}, err
				}
				binds = append(binds, bind)
				if p.cur.kind != tokenComma {
					break
				}
				if err := p.read(); err != nil {
					return Pattern{}, err
				}
			}
		}
		if err := p.expect(tokenRParen); err != nil {
			return Pattern{}, err
		}
	}
	return Pattern{Kind: PatternName, Name: name, Bindings: binds, Pos: pos}, nil
}

func (p *parser) parseExprList() ([]Expr, error) {
	if p.cur.kind != tokenLParen {
		return nil, nil
	}
	if err := p.read(); err != nil {
		return nil, err
	}
	var out []Expr
	if p.cur.kind != tokenRParen {
		for {
			expr, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			out = append(out, expr)
			if p.cur.kind != tokenComma {
				break
			}
			if err := p.read(); err != nil {
				return nil, err
			}
		}
	}
	if err := p.expect(tokenRParen); err != nil {
		return nil, err
	}
	return out, nil
}

func joinPath(path []string) string {
	return strings.Join(path, ".")
}

func splitImportPathVersion(path string) (string, string) {
	at := strings.LastIndex(path, "@")
	if at <= 0 || at == len(path)-1 {
		return path, ""
	}
	return path[:at], path[at+1:]
}

func (p *parser) parsePath() ([]string, error) {
	name, err := p.expectName()
	if err != nil {
		return nil, err
	}
	out := []string{name}
	for p.cur.kind == tokenDot {
		if err := p.read(); err != nil {
			return nil, err
		}
		next, err := p.expectName()
		if err != nil {
			return nil, err
		}
		out = append(out, next)
	}
	return out, nil
}

func (p *parser) parseExpr() (Expr, error) {
	return p.parseLogicOr()
}

func (p *parser) parseLogicOr() (Expr, error) {
	left, err := p.parseLogicAnd()
	if err != nil {
		return Expr{}, err
	}
	for p.cur.kind == tokenOrOr {
		op := p.cur.text
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		right, err := p.parseLogicAnd()
		if err != nil {
			return Expr{}, err
		}
		lhs := left
		rhs := right
		left = Expr{Kind: ExprLogic, Op: op, Left: &lhs, Right: &rhs, Pos: lhs.Pos}
	}
	return left, nil
}

func (p *parser) parseLogicAnd() (Expr, error) {
	left, err := p.parseEquality()
	if err != nil {
		return Expr{}, err
	}
	for p.cur.kind == tokenAndAnd {
		op := p.cur.text
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		right, err := p.parseEquality()
		if err != nil {
			return Expr{}, err
		}
		lhs := left
		rhs := right
		left = Expr{Kind: ExprLogic, Op: op, Left: &lhs, Right: &rhs, Pos: lhs.Pos}
	}
	return left, nil
}

func (p *parser) parseEquality() (Expr, error) {
	left, err := p.parseComparison()
	if err != nil {
		return Expr{}, err
	}
	for p.cur.kind == tokenEqualEqual || p.cur.kind == tokenBangEqual {
		op := p.cur.text
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		right, err := p.parseComparison()
		if err != nil {
			return Expr{}, err
		}
		lhs := left
		rhs := right
		left = Expr{Kind: ExprCompare, Op: op, Left: &lhs, Right: &rhs, Pos: lhs.Pos}
	}
	return left, nil
}

func (p *parser) parseComparison() (Expr, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return Expr{}, err
	}
	for p.cur.kind == tokenLess || p.cur.kind == tokenGreater || p.cur.kind == tokenLessEqual || p.cur.kind == tokenGreaterEqual {
		op := p.cur.text
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		right, err := p.parseAdditive()
		if err != nil {
			return Expr{}, err
		}
		lhs := left
		rhs := right
		left = Expr{Kind: ExprCompare, Op: op, Left: &lhs, Right: &rhs, Pos: lhs.Pos}
	}
	return left, nil
}

func (p *parser) parseAdditive() (Expr, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return Expr{}, err
	}
	for p.cur.kind == tokenPlus || p.cur.kind == tokenMinus {
		op := p.cur.text
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		right, err := p.parsePrimary()
		if err != nil {
			return Expr{}, err
		}
		lhs := left
		rhs := right
		left = Expr{Kind: ExprBinary, Op: op, Left: &lhs, Right: &rhs, Pos: lhs.Pos}
	}
	return left, nil
}

func (p *parser) parsePrimary() (Expr, error) {
	switch p.cur.kind {
	case tokenNumber:
		text := p.cur.text
		pos := p.cur.pos
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		return Expr{Kind: ExprNumber, Text: text, Pos: pos}, nil
	case tokenString:
		text := p.cur.text
		pos := p.cur.pos
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		return Expr{Kind: ExprString, Text: text, Pos: pos}, nil
	case tokenIdent:
		pos := p.cur.pos
		if p.cur.text == "try" {
			if err := p.read(); err != nil {
				return Expr{}, err
			}
			expr, err := p.parseExpr()
			if err != nil {
				return Expr{}, err
			}
			var fallback *Expr
			if p.cur.kind == tokenIdent && p.cur.text == "else" {
				if err := p.read(); err != nil {
					return Expr{}, err
				}
				next, err := p.parseExpr()
				if err != nil {
					return Expr{}, err
				}
				fallback = &next
			}
			return Expr{Kind: ExprTry, Left: &expr, Else: fallback, Pos: pos}, nil
		}
		if p.cur.text == "true" || p.cur.text == "false" {
			val := p.cur.text == "true"
			if err := p.read(); err != nil {
				return Expr{}, err
			}
			return Expr{Kind: ExprBool, Bool: val, Pos: pos}, nil
		}
		path, err := p.parsePath()
		if err != nil {
			return Expr{}, err
		}
		if p.cur.kind == tokenLParen {
			args, err := p.parseExprList()
			if err != nil {
				return Expr{}, err
			}
			return Expr{Kind: ExprCall, Text: path[0], Path: path, Args: args, Pos: pos}, nil
		}
		if len(path) == 1 {
			return Expr{Kind: ExprIdent, Text: path[0], Pos: pos}, nil
		}
		return Expr{Kind: ExprPath, Path: path, Pos: pos}, nil
	case tokenLParen:
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		expr, err := p.parseExpr()
		if err != nil {
			return Expr{}, err
		}
		if err := p.expect(tokenRParen); err != nil {
			return Expr{}, err
		}
		return expr, nil
	default:
		return Expr{}, fmt.Errorf("unexpected expression token %q at %s", p.cur.text, p.cur.pos)
	}
}

func (p *parser) parseTypeRef() (TypeRef, error) {
	name, err := p.expectName()
	if err != nil {
		return TypeRef{}, err
	}
	typ := TypeRef{Name: name, Pos: p.cur.pos}
	if p.cur.kind == tokenLess {
		if err := p.read(); err != nil {
			return TypeRef{}, err
		}
		for {
			arg, err := p.parseTypeRef()
			if err != nil {
				return TypeRef{}, err
			}
			typ.Args = append(typ.Args, arg)
			if p.cur.kind != tokenComma {
				break
			}
			if err := p.read(); err != nil {
				return TypeRef{}, err
			}
		}
		if err := p.expect(tokenGreater); err != nil {
			return TypeRef{}, err
		}
	}
	if p.cur.kind == tokenQuestion {
		if err := p.read(); err != nil {
			return TypeRef{}, err
		}
		typ.Optional = true
	}
	return typ, nil
}

func (p *parser) expect(kind tokenKind) error {
	if p.cur.kind != kind {
		return fmt.Errorf("expected %v at %s, got %q", kind, p.cur.pos, p.cur.text)
	}
	return p.read()
}

func (p *parser) expectIdent(name string) error {
	if p.cur.kind != tokenIdent || p.cur.text != name {
		return fmt.Errorf("expected %q at %s, got %q", name, p.cur.pos, p.cur.text)
	}
	return p.read()
}

func (p *parser) expectName() (string, error) {
	if p.cur.kind != tokenIdent {
		return "", fmt.Errorf("expected identifier at %s, got %q", p.cur.pos, p.cur.text)
	}
	name := p.cur.text
	if err := p.read(); err != nil {
		return "", err
	}
	return name, nil
}

func (p *parser) expectString() (string, error) {
	if p.cur.kind != tokenString {
		return "", fmt.Errorf("expected string at %s, got %q", p.cur.pos, p.cur.text)
	}
	text := p.cur.text
	if err := p.read(); err != nil {
		return "", err
	}
	return text, nil
}

func (p *parser) expectNumberUint64() (uint64, error) {
	if p.cur.kind != tokenNumber {
		return 0, fmt.Errorf("expected number at %s, got %q", p.cur.pos, p.cur.text)
	}
	value, err := strconv.ParseUint(p.cur.text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q at %s: %w", p.cur.text, p.cur.pos, err)
	}
	if err := p.read(); err != nil {
		return 0, err
	}
	return value, nil
}

func (p *parser) expectBool() (bool, error) {
	if p.cur.kind != tokenIdent || (p.cur.text != "true" && p.cur.text != "false") {
		return false, fmt.Errorf("expected bool at %s, got %q", p.cur.pos, p.cur.text)
	}
	value := p.cur.text == "true"
	return value, p.read()
}

func (p *parser) expectStringList() ([]string, error) {
	if err := p.expect(tokenLBracket); err != nil {
		return nil, err
	}
	var out []string
	if p.cur.kind != tokenRBracket {
		for {
			value, err := p.expectString()
			if err != nil {
				return nil, err
			}
			out = append(out, value)
			if p.cur.kind != tokenComma {
				break
			}
			if err := p.read(); err != nil {
				return nil, err
			}
		}
	}
	if err := p.expect(tokenRBracket); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *parser) read() error {
	if p.err != nil {
		return p.err
	}
	tok, err := p.lex.nextToken()
	if err != nil {
		p.err = err
		return err
	}
	p.cur = tok
	return nil
}
