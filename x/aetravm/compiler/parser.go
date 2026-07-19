package compiler

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// maxParseDepth bounds recursive-descent nesting so pathological input such as
// deeply nested "((((", "Chunk<Chunk<...>>", "!!!!", or nested blocks returns a
// normal parse error instead of overflowing the goroutine stack (a fatal Go
// error that recover() cannot catch).
const maxParseDepth = 128

// maxSourceBytes caps total input size up front to bound total work and token
// count before any recursive descent begins.
const maxSourceBytes = 1 << 20 // 1 MiB

type parser struct {
	lex   *lexer
	cur   token
	err   error
	depth int
}

// enter increments recursion depth and fails if the configured limit is
// exceeded. Every recursive production pairs it with a deferred leave().
func (p *parser) enter() error {
	p.depth++
	if p.depth > maxParseDepth {
		return fmt.Errorf("expression or type nesting too deep (max %d) at %s", maxParseDepth, p.cur.pos)
	}
	return nil
}

func (p *parser) leave() {
	p.depth--
}

func ParseSource(src string) (*SourceFile, error) {
	return ParseSourceNamed("", src)
}

func ParseSourceNamed(fileName, src string) (*SourceFile, error) {
	if len(src) > maxSourceBytes {
		return nil, fmt.Errorf("source %q is %d bytes, exceeds limit %d", fileName, len(src), maxSourceBytes)
	}
	p := &parser{lex: newLexer(fileName, src)}
	if err := p.read(); err != nil {
		return nil, err
	}
	file := &SourceFile{}
	for p.cur.kind != tokenEOF && p.err == nil {
		annotations, err := p.parseAnnotationList()
		if err != nil {
			return nil, err
		}
		if len(annotations) > 0 && p.cur.text != "func" && p.cur.text != "struct" {
			return nil, fmt.Errorf("annotations are only allowed before func or struct declarations at %s", p.cur.pos)
		}
		switch p.cur.text {
		case "const":
			cd, err := p.parseConstDecl()
			if err != nil {
				return nil, err
			}
			file.Consts = append(file.Consts, cd)
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
			st, err := p.parseStruct(annotations)
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
		case "type":
			td, err := p.parseTypeDecl()
			if err != nil {
				return nil, err
			}
			file.Types = append(file.Types, td)
		case "func":
			fn, err := p.parseFunction(annotations)
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

func (p *parser) parseFunction(annotations []Annotation) (*FunctionDecl, error) {
	pos := p.cur.pos
	if err := p.expectIdent("func"); err != nil {
		return nil, err
	}
	nameParts, err := p.parsePath()
	if err != nil {
		return nil, err
	}
	name := joinPath(nameParts)
	params, ret, body, err := p.parseFunctionTail()
	if err != nil {
		return nil, err
	}
	if strings.Contains(name, ".") && len(params) > 0 && params[0].Name == "self" && params[0].Type.Name == "" {
		params[0].Type = TypeRef{Name: strings.SplitN(name, ".", 2)[0]}
	}
	if err := validateAnnotationCompatibility(annotations, "func", pos); err != nil {
		return nil, err
	}
	var rt TypeRef
	if ret != nil {
		rt = *ret
	}
	return &FunctionDecl{Annotations: canonicalAnnotations(annotations), Pure: functionIsPure(annotations), Name: name, Params: params, ReturnType: rt, Body: body, Pos: pos}, nil
}

func (p *parser) parseConstDecl() (*ConstDecl, error) {
	pos := p.cur.pos
	if err := p.expectIdent("const"); err != nil {
		return nil, err
	}
	name, err := p.expectBindingName()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tokenEqual); err != nil {
		return nil, err
	}
	value, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.cur.kind == tokenSemicolon {
		if err := p.read(); err != nil {
			return nil, err
		}
	}
	return &ConstDecl{Name: name, Value: value, Pos: pos}, nil
}

func (p *parser) parseAnnotationList() ([]Annotation, error) {
	var annotations []Annotation
	for p.cur.kind == tokenAt {
		pos := p.cur.pos
		if err := p.read(); err != nil {
			return nil, err
		}
		name, err := p.expectName()
		if err != nil {
			return nil, err
		}
		ann := Annotation{Name: "@" + name, Pos: pos}
		if p.cur.kind == tokenLParen {
			// Only @message carries an argument (its uint32 opcode). Every
			// other annotation is bare: parameters belong to the function
			// signature — a bare @external above
			// func onExternalMessage(inMsg: Segment), never an annotation
			// argument list.
			if ann.Name != "@message" {
				return nil, fmt.Errorf("annotation %s takes no arguments at %s: declare parameters in the function signature instead", ann.Name, pos)
			}
			if err := p.read(); err != nil {
				return nil, err
			}
			opPos := p.cur.pos
			value, err := p.expectNumberUint64()
			if err != nil {
				return nil, fmt.Errorf("@message requires a numeric opcode argument at %s: %w", opPos, err)
			}
			if value > math.MaxUint32 {
				return nil, fmt.Errorf("annotation opcode %d exceeds uint32 range at %s", value, opPos)
			}
			v := uint32(value)
			ann.Value = &v
			if err := p.expect(tokenRParen); err != nil {
				return nil, err
			}
		}
		switch ann.Name {
		case "@internal", "@external", "@bounced", "@get", "@pure", "@impure", "@storage", "@message", "@store", "@resource":
		default:
			return nil, fmt.Errorf("unknown annotation %q at %s", ann.Name, pos)
		}
		annotations = append(annotations, ann)
	}
	if len(annotations) > 1 {
		return nil, fmt.Errorf("only one annotation is allowed per declaration at %s", annotations[1].Pos)
	}
	return annotations, nil
}

func canonicalAnnotations(annotations []Annotation) []Annotation {
	if len(annotations) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(annotations))
	out := make([]Annotation, 0, len(annotations))
	for _, annotation := range annotations {
		key := annotationKey(annotation)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, annotation)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return annotationRank(out[i]) < annotationRank(out[j])
	})
	return out
}

func annotationKey(annotation Annotation) string {
	if annotation.Value == nil {
		return annotation.Name
	}
	return fmt.Sprintf("%s:%d", annotation.Name, *annotation.Value)
}

func annotationRank(annotation Annotation) int {
	switch annotation.Name {
	case "@internal":
		return 0
	case "@external":
		return 1
	case "@bounced":
		return 2
	case "@get":
		return 3
	case "@pure":
		return 4
	case "@impure":
		return 5
	case "@storage":
		return 6
	case "@message":
		return 7
	case "@store":
		return 8
	default:
		return 9
	}
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
	if p.cur.kind == tokenArrow || p.cur.kind == tokenColon {
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

func (p *parser) parseStruct(annotations []Annotation) (*StructDecl, error) {
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
		lazy := false
		if p.cur.kind == tokenColon {
			if err := p.read(); err != nil {
				return nil, err
			}
		}
		if p.cur.kind == tokenIdent && p.cur.text == "lazy" {
			lazy = true
			if err := p.read(); err != nil {
				return nil, err
			}
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
		fields = append(fields, FieldDecl{Name: fieldName, Lazy: lazy, Type: typ, Default: def, Pos: p.cur.pos})
		if p.cur.kind == tokenSemicolon {
			if err := p.read(); err != nil {
				return nil, err
			}
		}
	}
	if err := p.expect(tokenRBrace); err != nil {
		return nil, err
	}
	if err := validateAnnotationCompatibility(annotations, "struct", pos); err != nil {
		return nil, err
	}
	return &StructDecl{Annotations: canonicalAnnotations(annotations), Name: name, Fields: fields, Pos: pos}, nil
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

func (p *parser) parseTypeDecl() (*TypeDecl, error) {
	pos := p.cur.pos
	if err := p.expectIdent("type"); err != nil {
		return nil, err
	}
	name, err := p.expectName()
	if err != nil {
		return nil, err
	}
	if err := p.expect(tokenEqual); err != nil {
		return nil, err
	}
	first, err := p.parseTypeRef()
	if err != nil {
		return nil, err
	}
	members := []TypeRef{first}
	for p.cur.kind == tokenPipe {
		if err := p.read(); err != nil {
			return nil, err
		}
		member, err := p.parseTypeRef()
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	if p.cur.kind == tokenSemicolon {
		if err := p.read(); err != nil {
			return nil, err
		}
	}
	return &TypeDecl{Name: name, Members: members, Pos: pos}, nil
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
		annotations, err := p.parseAnnotationList()
		if err != nil {
			return nil, err
		}
		if len(annotations) > 0 {
			switch p.cur.text {
			case "func":
			default:
				return nil, fmt.Errorf("annotations are only allowed before func declarations at %s", p.cur.pos)
			}
		}
		switch p.cur.text {
		case "storage":
			if err := p.read(); err != nil {
				return nil, err
			}
			if err := p.expect(tokenColon); err != nil {
				return nil, fmt.Errorf("contract metadata key \"storage\" requires a colon (storage: TypeName) at %s", p.cur.pos)
			}
			stype, err := p.parseTypeRef()
			if err != nil {
				return nil, err
			}
			ct.StorageTypeName = stype.String()
		case "author":
			if err := p.read(); err != nil {
				return nil, err
			}
			if err := p.expect(tokenColon); err != nil {
				return nil, fmt.Errorf("contract metadata key \"author\" requires a colon (author: \"...\") at %s", p.cur.pos)
			}
			v, err := p.expectString()
			if err != nil {
				return nil, err
			}
			ct.Author = v
		case "description":
			if err := p.read(); err != nil {
				return nil, err
			}
			if err := p.expect(tokenColon); err != nil {
				return nil, fmt.Errorf("contract metadata key \"description\" requires a colon (description: \"...\") at %s", p.cur.pos)
			}
			v, err := p.expectString()
			if err != nil {
				return nil, err
			}
			ct.Description = v
		case "version":
			if err := p.read(); err != nil {
				return nil, err
			}
			if err := p.expect(tokenColon); err != nil {
				return nil, fmt.Errorf("contract metadata key \"version\" requires a colon (version: \"...\") at %s", p.cur.pos)
			}
			v, err := p.expectString()
			if err != nil {
				return nil, err
			}
			ct.Version = v
		case "incomingMessages":
			if err := p.read(); err != nil {
				return nil, err
			}
			if err := p.expect(tokenColon); err != nil {
				return nil, fmt.Errorf("contract metadata key \"incomingMessages\" requires a colon (incomingMessages: UnionType) at %s", p.cur.pos)
			}
			typ, err := p.parseTypeRef()
			if err != nil {
				return nil, err
			}
			ct.IncomingMessagesType = typ.String()
		case "incomingExternal":
			if err := p.read(); err != nil {
				return nil, err
			}
			if err := p.expect(tokenColon); err != nil {
				return nil, fmt.Errorf("contract metadata key \"incomingExternal\" requires a colon (incomingExternal: UnionType) at %s", p.cur.pos)
			}
			typ, err := p.parseTypeRef()
			if err != nil {
				return nil, err
			}
			ct.IncomingExternalType = typ.String()
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
		case "func":
			fn, err := p.parseFunction(annotations)
			if err != nil {
				return nil, err
			}
			ct.Functions = append(ct.Functions, fn)
		case "message":
			return nil, fmt.Errorf("legacy declaration \"message\" is not part of ATLX at %s: declare a @message(opcode) struct and handle it in @internal func onInternalMessage / @external func onExternalMessage", p.cur.pos)
		case "getter":
			return nil, fmt.Errorf("legacy declaration \"getter\" is not part of ATLX at %s: use @get func name(): T", p.cur.pos)
		case "event":
			return nil, fmt.Errorf("legacy declaration \"event\" is not part of ATLX at %s", p.cur.pos)
		case "wallet":
			return nil, fmt.Errorf("legacy declaration \"wallet action\" is not part of ATLX at %s", p.cur.pos)
		case "selector":
			return nil, fmt.Errorf("\"selector\" is not part of ATLX at %s: message opcodes come from @message(opcode) struct annotations", p.cur.pos)
		default:
			return nil, fmt.Errorf("unexpected contract item %q at %s", p.cur.text, p.cur.pos)
		}
	}
	if err := p.expect(tokenRBrace); err != nil {
		return nil, err
	}
	if ct.IncomingMessagesType == "" && ct.IncomingExternalType == "" {
		return nil, fmt.Errorf("contract %q must declare incomingMessages, incomingExternal, or both at %s", name, pos)
	}
	return ct, nil
}


func validateAnnotationCompatibility(annotations []Annotation, target string, pos Position) error {
	if len(annotations) == 0 {
		return nil
	}
	hasStorage := false
	hasMessage := false
	hasPure := false
	hasImpure := false
	for _, annotation := range annotations {
		switch target {
		case "func":
			switch annotation.Name {
			case "@pure":
				hasPure = true
			case "@impure":
				hasImpure = true
			case "@get", "@external", "@internal", "@bounced", "@store":
			default:
				return fmt.Errorf("annotation %q is not valid on func at %s", annotation.Name, pos)
			}
		case "struct":
			switch annotation.Name {
			case "@storage":
				if annotation.Value != nil {
					return fmt.Errorf("annotation %q does not take an argument at %s", annotation.Name, pos)
				}
				hasStorage = true
			case "@message":
				if annotation.Value == nil {
					return fmt.Errorf("annotation %q requires an opcode at %s", annotation.Name, pos)
				}
				hasMessage = true
			case "@resource":
				if annotation.Value != nil {
					return fmt.Errorf("annotation %q does not take an argument at %s", annotation.Name, pos)
				}
			default:
				return fmt.Errorf("annotation %q is not valid on struct at %s", annotation.Name, pos)
			}
		}
	}
	if target == "struct" {
		if hasStorage && hasMessage {
			return fmt.Errorf("struct annotations @storage and @message cannot be combined at %s", pos)
		}
	}
	if target == "func" && hasPure && hasImpure {
		return fmt.Errorf("@pure and @impure cannot be combined at %s", pos)
	}
	return nil
}

func functionIsPure(annotations []Annotation) bool {
	for _, annotation := range annotations {
		switch annotation.Name {
		case "@impure", "@external", "@internal", "@bounced":
			return false
		}
	}
	return true
}

func (p *parser) parseParamList() ([]ParamDecl, error) {
	if err := p.expect(tokenLParen); err != nil {
		return nil, err
	}
	var params []ParamDecl
	if p.cur.kind != tokenRParen {
		for {
			mutate := false
			if p.cur.kind == tokenIdent && p.cur.text == "mutate" {
				mutate = true
				if err := p.read(); err != nil {
					return nil, err
				}
			}
			name, err := p.expectName()
			if err != nil {
				return nil, err
			}
			var typ TypeRef
			if p.cur.kind == tokenColon {
				if err := p.read(); err != nil {
					return nil, err
				}
				typ, err = p.parseTypeRef()
				if err != nil {
					return nil, err
				}
			} else if name != "self" {
				return nil, fmt.Errorf("expected type for parameter %q at %s", name, p.cur.pos)
			}
			params = append(params, ParamDecl{Name: name, Type: typ, Mutate: mutate, Pos: p.cur.pos})
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
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
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
	if err := p.enter(); err != nil {
		return Statement{}, err
	}
	defer p.leave()
	pos := p.cur.pos
	switch p.cur.text {
	case "const", "var":
		mutable := p.cur.text == "var"
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		name, err := p.expectBindingName()
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
		return Statement{Kind: StatementBinding, Name: name, Value: expr, Mutable: mutable, Pos: pos}, nil
	case "let", "val", "mut":
		return Statement{}, fmt.Errorf("local bindings must use const or var at %s", p.cur.pos)
	case "assert":
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		if err := p.expect(tokenLParen); err != nil {
			return Statement{}, err
		}
		cond, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		if err := p.expect(tokenRParen); err != nil {
			return Statement{}, err
		}
		if err := p.expectIdent("throw"); err != nil {
			return Statement{}, err
		}
		code, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		extra := map[string]Expr{"throw": code}
		return Statement{Kind: StatementAssert, Value: cond, Extra: extra, Pos: pos}, nil
	case "throw":
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		code, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		return Statement{Kind: StatementThrow, Value: code, Pos: pos}, nil
	case "break":
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		return Statement{Kind: StatementBreak, Pos: pos}, nil
	case "continue":
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		return Statement{Kind: StatementContinue, Pos: pos}, nil
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
		if p.cur.kind == tokenDot {
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
			value, err := p.parseExpr()
			if err != nil {
				return Statement{}, err
			}
			path = append([]string{"state"}, path...)
			return Statement{Kind: StatementSet, Path: path, Value: value, Pos: pos}, nil
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
			if p.cur.kind == tokenIdent && p.cur.text == "if" {
				nested, err := p.parseStatement()
				if err != nil {
					return Statement{}, err
				}
				elseBody = []Statement{nested}
			} else {
				elseBody, err = p.parseBlock()
				if err != nil {
					return Statement{}, err
				}
			}
		}
		return Statement{Kind: StatementIf, Value: cond, Then: thenBody, Else: elseBody, Pos: pos}, nil
	case "while":
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		cond, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		body, err := p.parseBlock()
		if err != nil {
			return Statement{}, err
		}
		return Statement{Kind: StatementWhile, Value: cond, Then: body, Pos: pos}, nil
	case "do":
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		body, err := p.parseBlock()
		if err != nil {
			return Statement{}, err
		}
		if err := p.expectIdent("while"); err != nil {
			return Statement{}, err
		}
		cond, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		return Statement{Kind: StatementDo, Value: cond, Then: body, Pos: pos}, nil
	case "repeat":
		if err := p.read(); err != nil {
			return Statement{}, err
		}
		count, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		body, err := p.parseBlock()
		if err != nil {
			return Statement{}, err
		}
		return Statement{Kind: StatementRepeat, Value: count, Then: body, Pos: pos}, nil
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
		expr, err := p.parseExpr()
		if err != nil {
			return Statement{}, err
		}
		switch p.cur.kind {
		case tokenEqual, tokenPlusEqual, tokenMinusEqual:
			op := p.cur.kind
			if err := p.read(); err != nil {
				return Statement{}, err
			}
			rhs, err := p.parseExpr()
			if err != nil {
				return Statement{}, err
			}
			path, ok := exprAsPath(expr)
			if !ok {
				return Statement{}, fmt.Errorf("assignment target must be a path at %s", expr.Pos)
			}
			value := rhs
			switch op {
			case tokenPlusEqual:
				left := expr
				value = Expr{Kind: ExprBinary, Op: "+", Left: &left, Right: &rhs, Pos: expr.Pos}
			case tokenMinusEqual:
				left := expr
				value = Expr{Kind: ExprBinary, Op: "-", Left: &left, Right: &rhs, Pos: expr.Pos}
			}
			return Statement{Kind: StatementSet, Path: path, Value: value, Pos: pos}, nil
		default:
			if expr.Kind == ExprCall && len(expr.Path) >= 2 && strings.EqualFold(expr.Path[len(expr.Path)-1], "send") {
				// Canonical surface: `.send()` takes no arguments. The message
				// is fully self-describing — delivery semantics live in the
				// buildMessage `mode:` field, so there is exactly one place to
				// declare a send mode.
				if len(expr.Args) != 0 {
					return Statement{}, fmt.Errorf(".send() takes no arguments at %s: declare the send mode in buildMessage via the mode: field (e.g. mode: SEND_BOUNCE_ON_FAIL)", pos)
				}
				receiverPath := expr.Path[:len(expr.Path)-1]
				var value Expr
				if len(receiverPath) == 1 {
					value = Expr{Kind: ExprIdent, Text: receiverPath[0], Pos: expr.Pos}
				} else {
					value = Expr{Kind: ExprPath, Path: append([]string(nil), receiverPath...), Pos: expr.Pos}
				}
				return Statement{Kind: StatementSend, Value: value, Pos: pos}, nil
			}
			return Statement{Kind: StatementExpr, Value: expr, Pos: pos}, nil
		}
	}
}

func exprAsPath(expr Expr) ([]string, bool) {
	switch expr.Kind {
	case ExprIdent:
		return []string{expr.Text}, true
	case ExprPath:
		return append([]string(nil), expr.Path...), true
	default:
		return nil, false
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
		if p.cur.kind == tokenFatArrow || p.cur.kind == tokenArrow {
			if err := p.read(); err != nil {
				return nil, err
			}
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
	if p.cur.text == "else" {
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
	if err := p.enter(); err != nil {
		return Expr{}, err
	}
	defer p.leave()
	return p.parseTernary()
}

func (p *parser) parseTernary() (Expr, error) {
	cond, err := p.parseCoalesce()
	if err != nil {
		return Expr{}, err
	}
	if p.cur.kind != tokenQuestion {
		return cond, nil
	}
	pos := p.cur.pos
	if err := p.read(); err != nil {
		return Expr{}, err
	}
	thenExpr, err := p.parseExpr()
	if err != nil {
		return Expr{}, err
	}
	if err := p.expect(tokenColon); err != nil {
		return Expr{}, err
	}
	elseExpr, err := p.parseExpr()
	if err != nil {
		return Expr{}, err
	}
	c := cond
	t := thenExpr
	e := elseExpr
	return Expr{Kind: ExprTernary, Left: &c, Right: &t, Else: &e, Pos: pos}, nil
}

func (p *parser) parseCoalesce() (Expr, error) {
	left, err := p.parseLogicOr()
	if err != nil {
		return Expr{}, err
	}
	for p.cur.kind == tokenQuestionQuestion {
		op := p.cur.text
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		right, err := p.parseLogicOr()
		if err != nil {
			return Expr{}, err
		}
		lhs := left
		rhs := right
		left = Expr{Kind: ExprBinary, Op: op, Left: &lhs, Right: &rhs, Pos: lhs.Pos}
	}
	return left, nil
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
	left, err := p.parseBitwiseOr()
	if err != nil {
		return Expr{}, err
	}
	for p.cur.kind == tokenAndAnd {
		op := p.cur.text
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		right, err := p.parseBitwiseOr()
		if err != nil {
			return Expr{}, err
		}
		lhs := left
		rhs := right
		left = Expr{Kind: ExprLogic, Op: op, Left: &lhs, Right: &rhs, Pos: lhs.Pos}
	}
	return left, nil
}

func (p *parser) parseBitwiseOr() (Expr, error) {
	left, err := p.parseBitwiseXor()
	if err != nil {
		return Expr{}, err
	}
	for p.cur.kind == tokenPipe {
		op := p.cur.text
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		right, err := p.parseBitwiseXor()
		if err != nil {
			return Expr{}, err
		}
		lhs := left
		rhs := right
		left = Expr{Kind: ExprBinary, Op: op, Left: &lhs, Right: &rhs, Pos: lhs.Pos}
	}
	return left, nil
}

func (p *parser) parseBitwiseXor() (Expr, error) {
	left, err := p.parseBitwiseAnd()
	if err != nil {
		return Expr{}, err
	}
	for p.cur.kind == tokenCaret {
		op := p.cur.text
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		right, err := p.parseBitwiseAnd()
		if err != nil {
			return Expr{}, err
		}
		lhs := left
		rhs := right
		left = Expr{Kind: ExprBinary, Op: op, Left: &lhs, Right: &rhs, Pos: lhs.Pos}
	}
	return left, nil
}

func (p *parser) parseBitwiseAnd() (Expr, error) {
	left, err := p.parseEquality()
	if err != nil {
		return Expr{}, err
	}
	for p.cur.kind == tokenAmpersand {
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
		left = Expr{Kind: ExprBinary, Op: op, Left: &lhs, Right: &rhs, Pos: lhs.Pos}
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
	left, err := p.parseShift()
	if err != nil {
		return Expr{}, err
	}
	for p.cur.kind == tokenLess || p.cur.kind == tokenGreater || p.cur.kind == tokenLessEqual || p.cur.kind == tokenGreaterEqual || p.cur.kind == tokenSpaceship {
		op := p.cur.text
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		right, err := p.parseShift()
		if err != nil {
			return Expr{}, err
		}
		lhs := left
		rhs := right
		left = Expr{Kind: ExprCompare, Op: op, Left: &lhs, Right: &rhs, Pos: lhs.Pos}
	}
	return left, nil
}

func (p *parser) parseShift() (Expr, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return Expr{}, err
	}
	for p.cur.kind == tokenLessLess || p.cur.kind == tokenGreaterGreater {
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
		left = Expr{Kind: ExprBinary, Op: op, Left: &lhs, Right: &rhs, Pos: lhs.Pos}
	}
	return left, nil
}

func (p *parser) parseAdditive() (Expr, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return Expr{}, err
	}
	for p.cur.kind == tokenPlus || p.cur.kind == tokenMinus {
		op := p.cur.text
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		right, err := p.parseMultiplicative()
		if err != nil {
			return Expr{}, err
		}
		lhs := left
		rhs := right
		left = Expr{Kind: ExprBinary, Op: op, Left: &lhs, Right: &rhs, Pos: lhs.Pos}
	}
	return left, nil
}

func (p *parser) parseMultiplicative() (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return Expr{}, err
	}
	for p.cur.kind == tokenStar || p.cur.kind == tokenSlash || p.cur.kind == tokenPercent {
		op := p.cur.text
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		right, err := p.parseUnary()
		if err != nil {
			return Expr{}, err
		}
		lhs := left
		rhs := right
		left = Expr{Kind: ExprBinary, Op: op, Left: &lhs, Right: &rhs, Pos: lhs.Pos}
	}
	return left, nil
}

func (p *parser) parseUnary() (Expr, error) {
	if err := p.enter(); err != nil {
		return Expr{}, err
	}
	defer p.leave()
	switch p.cur.kind {
	case tokenBang, tokenMinus, tokenTilde:
		op := p.cur.text
		pos := p.cur.pos
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		expr, err := p.parseUnary()
		if err != nil {
			return Expr{}, err
		}
		inner := expr
		return Expr{Kind: ExprUnary, Op: op, Left: &inner, Pos: pos}, nil
	default:
		return p.parsePrimary()
	}
}

func (p *parser) parsePrimary() (Expr, error) {
	finish := func(expr Expr) (Expr, error) {
		for p.cur.kind == tokenBang {
			if err := p.read(); err != nil {
				return Expr{}, err
			}
			expr.Unwrap = true
		}
		return expr, nil
	}
	switch p.cur.kind {
	case tokenNumber:
		text := p.cur.text
		pos := p.cur.pos
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		return finish(Expr{Kind: ExprNumber, Text: text, Pos: pos})
	case tokenString:
		text := p.cur.text
		pos := p.cur.pos
		if err := p.read(); err != nil {
			return Expr{}, err
		}
		return finish(Expr{Kind: ExprString, Text: text, Pos: pos})
	case tokenIdent:
		pos := p.cur.pos
		if p.cur.text == "lazy" {
			if err := p.read(); err != nil {
				return Expr{}, err
			}
			expr, err := p.parsePrimary()
			if err != nil {
				return Expr{}, err
			}
			return finish(expr)
		}
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
			return finish(Expr{Kind: ExprTry, Left: &expr, Else: fallback, Pos: pos})
		}
		if p.cur.text == "null" {
			if err := p.read(); err != nil {
				return Expr{}, err
			}
			return finish(Expr{Kind: ExprNull, Pos: pos})
		}
		if p.cur.text == "true" || p.cur.text == "false" {
			boolValue := p.cur.text == "true"
			if err := p.read(); err != nil {
				return Expr{}, err
			}
			return finish(Expr{Kind: ExprBool, Bool: boolValue, Pos: pos})
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
			return finish(Expr{Kind: ExprCall, Text: path[0], Path: path, Args: args, Pos: pos})
		}
		if p.cur.kind == tokenLBrace && p.looksLikeStructLiteral() {
			fields, err := p.parseExprStructFields()
			if err != nil {
				return Expr{}, err
			}
			return finish(Expr{Kind: ExprStruct, Text: joinPath(path), Path: path, Fields: fields, Pos: pos})
		}
		if len(path) == 1 {
			return finish(Expr{Kind: ExprIdent, Text: path[0], Pos: pos})
		}
		return finish(Expr{Kind: ExprPath, Path: path, Pos: pos})
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
		return finish(expr)
	case tokenLBrace:
		pos := p.cur.pos
		fields, err := p.parseExprStructFields()
		if err != nil {
			return Expr{}, err
		}
		return finish(Expr{Kind: ExprStruct, Fields: fields, Pos: pos})
	default:
		return Expr{}, fmt.Errorf("unexpected expression token %q at %s", p.cur.text, p.cur.pos)
	}
}

func (p *parser) parseExprStructFields() ([]ExprField, error) {
	pos := p.cur.pos
	if err := p.expect(tokenLBrace); err != nil {
		return nil, err
	}
	var fields []ExprField
	seen := map[string]struct{}{}
	for p.cur.kind != tokenRBrace {
		if p.cur.kind == tokenEOF {
			return nil, fmt.Errorf("unterminated struct literal starting at %s", pos)
		}
		name, err := p.expectName()
		if err != nil {
			return nil, err
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("duplicate struct literal field %q at %s", name, p.cur.pos)
		}
		seen[name] = struct{}{}
		if err := p.expect(tokenColon); err != nil {
			return nil, err
		}
		value, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		fields = append(fields, ExprField{Name: name, Value: value, Pos: p.cur.pos})
		if p.cur.kind == tokenComma || p.cur.kind == tokenSemicolon {
			if err := p.read(); err != nil {
				return nil, err
			}
		}
	}
	if err := p.expect(tokenRBrace); err != nil {
		return nil, err
	}
	return fields, nil
}

func (p *parser) looksLikeStructLiteral() bool {
	if p.cur.kind != tokenLBrace {
		return false
	}
	clone := *p
	lexCopy := *p.lex
	clone.lex = &lexCopy
	if err := clone.read(); err != nil {
		return false
	}
	if clone.cur.kind == tokenRBrace {
		return true
	}
	if clone.cur.kind != tokenIdent {
		return false
	}
	if err := clone.read(); err != nil {
		return false
	}
	return clone.cur.kind == tokenColon
}

func (p *parser) parseTypeRef() (TypeRef, error) {
	if err := p.enter(); err != nil {
		return TypeRef{}, err
	}
	defer p.leave()
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
		if err := p.expectTypeClose(); err != nil {
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

// expectTypeClose consumes a single closing '>' of a generic type-argument
// list. The lexer greedily merges two adjacent '>' into one tokenGreaterGreater
// (the right-shift operator), so a nested generic whose brackets abut --
// e.g. Chunk<Chunk<Leaf>> or Map<addr, Chunk<V>> -- would otherwise fail to
// parse even though it is well-formed. When the current token is '>>', split
// it: consume one '>' here and leave a synthetic '>' as the current token for
// the enclosing type level to close. This affects type context ONLY; the '>>'
// binary operator in expression context is parsed elsewhere and is untouched.
func (p *parser) expectTypeClose() error {
	switch p.cur.kind {
	case tokenGreater:
		return p.read()
	case tokenGreaterGreater:
		p.cur = token{kind: tokenGreater, text: ">", pos: p.cur.pos}
		return nil
	default:
		return fmt.Errorf("expected %v at %s, got %q", tokenGreater, p.cur.pos, p.cur.text)
	}
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

// reservedBindingNames are language keywords that cannot be used as const/var
// binding names. Parameter names such as `in` and `self` stay valid because
// the canonical handler signatures require them.
var reservedBindingNames = map[string]struct{}{
	"import": {}, "contract": {}, "struct": {}, "enum": {}, "type": {},
	"func": {}, "const": {}, "var": {}, "if": {}, "else": {}, "while": {},
	"do": {}, "repeat": {}, "for": {}, "match": {}, "break": {},
	"continue": {}, "return": {}, "assert": {}, "throw": {}, "lazy": {},
	"mutate": {}, "set": {}, "emit": {}, "send": {}, "refund": {},
	"true": {}, "false": {}, "null": {},
}

func (p *parser) expectBindingName() (string, error) {
	pos := p.cur.pos
	name, err := p.expectName()
	if err != nil {
		return "", err
	}
	if _, reserved := reservedBindingNames[name]; reserved {
		return "", fmt.Errorf("cannot use keyword %q as a binding name at %s", name, pos)
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
	value, err := parseUintLiteral(p.cur.text)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q at %s: %w", p.cur.text, p.cur.pos, err)
	}
	if err := p.read(); err != nil {
		return 0, err
	}
	return value, nil
}

func parseUintLiteral(text string) (uint64, error) {
	switch {
	case strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X"):
		return strconv.ParseUint(text[2:], 16, 64)
	default:
		return strconv.ParseUint(text, 10, 64)
	}
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
