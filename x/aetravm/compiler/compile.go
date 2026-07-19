package compiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/chunk"
	"github.com/sovereign-l1/l1/x/aetravm/standards"
	"lukechampine.com/blake3"
)

const (
	DefaultABIName           = "avm"
	DefaultABIVersion        = 1
	DefaultChainID           = "avm-local"
	DefaultNamespace         = "default"
	DefaultDeployerAddress   = "AEcompiler"
	DefaultSalt              = "compiler"
	DefaultMaxCodeBytes      = 64 * 1024
	DefaultMaxPayloadBytes   = 1 << 20
	DefaultMaxStorageBytes   = 1 << 20
	DefaultMaxStateInitBytes = avm.MaxStateInitSize
)

type Options struct {
	ChainID              string
	Namespace            string
	DeployerAddress      string
	Salt                 string
	InitialBalance       uint64
	MaxCodeBytes         uint32
	MaxPayloadBytes      uint32
	MaxStorageBytes      uint32
	MaxStateInitBytes    uint32
	Resolver             DependencyResolver
	SurfaceCompatibility SurfaceCompatibilityMode
}

func DefaultOptions() Options {
	return Options{
		ChainID:              DefaultChainID,
		Namespace:            DefaultNamespace,
		DeployerAddress:      DefaultDeployerAddress,
		Salt:                 DefaultSalt,
		MaxCodeBytes:         DefaultMaxCodeBytes,
		MaxPayloadBytes:      DefaultMaxPayloadBytes,
		MaxStorageBytes:      DefaultMaxStorageBytes,
		MaxStateInitBytes:    DefaultMaxStateInitBytes,
		SurfaceCompatibility: SurfaceCompatibilityWarnings,
	}
}

func (c *Compiler) nextLabel(prefix string) string {
	c.labelSeq++
	return fmt.Sprintf("%s_%d", prefix, c.labelSeq)
}

type Compiler struct {
	opts         Options
	diags        []Diagnostic
	labelSeq     uint64
	globalConsts map[string]constValue
}

type Result struct {
	Source             *SourceFile
	Contract           *ContractDecl
	Module             avm.Module
	ModuleBytes        []byte
	ModuleHash         [32]byte
	Manifest           avm.InterfaceManifest
	ManifestHash       [32]byte
	StateInit          *avm.StateInit
	StateInitHash      [32]byte
	CodeChunk          *chunk.Chunk
	CodeChunkHash      [32]byte
	StorageLayout      StorageLayout
	StorageCodec       Codec
	MessageCodecs      map[string]Codec
	MessageBodies      map[string]Codec
	MessageBodyOpcodes map[string]uint32
	MessageUnions      map[string]MessageUnion
	GetterCodecs       map[string]Codec
	EventCodecs        map[string]Codec
	SelectorRegistry   SelectorRegistry
	Diagnostics        []Diagnostic
	IR                 *IRProgram
	DependencyLock     DependencyLock
}

type StorageLayout struct {
	Name       string
	Fields     []CodecField
	LayoutHash [32]byte
}

type CodecField struct {
	Name    string
	Lazy    bool
	Type    TypeRef
	Default Expr
	Pos     Position
}

type Codec struct {
	Name       string
	Kind       string
	Fields     []CodecField
	ReturnType *TypeRef
	Hash       [32]byte
	// MaxBytes bounds the encoded payload size; zero means unlimited.
	MaxBytes int
}

type MessageUnion struct {
	Name     string
	Variants []MessageUnionVariant
	Hash     [32]byte
}

type MessageUnionVariant struct {
	Name   string
	Type   string
	Opcode uint32
}

type SelectorEntry struct {
	Kind       string
	Name       string
	Signature  string
	Selector   uint32
	Topic      string
	Entrypoint string
}

type SelectorRegistry struct {
	Contract     string
	Entries      []SelectorEntry
	RegistryHash [32]byte
}

func New(opts Options) (*Compiler, error) {
	merged := DefaultOptions()
	if opts.ChainID != "" {
		merged.ChainID = opts.ChainID
	}
	if opts.Namespace != "" {
		merged.Namespace = opts.Namespace
	}
	if opts.DeployerAddress != "" {
		merged.DeployerAddress = opts.DeployerAddress
	}
	if opts.Salt != "" {
		merged.Salt = opts.Salt
	}
	if opts.InitialBalance != 0 {
		merged.InitialBalance = opts.InitialBalance
	}
	if opts.MaxCodeBytes != 0 {
		merged.MaxCodeBytes = opts.MaxCodeBytes
	}
	if opts.MaxPayloadBytes != 0 {
		merged.MaxPayloadBytes = opts.MaxPayloadBytes
	}
	if opts.MaxStorageBytes != 0 {
		merged.MaxStorageBytes = opts.MaxStorageBytes
	}
	if opts.MaxStateInitBytes != 0 {
		merged.MaxStateInitBytes = opts.MaxStateInitBytes
	}
	if opts.Resolver != nil {
		merged.Resolver = opts.Resolver
	}
	if opts.SurfaceCompatibility != "" {
		merged.SurfaceCompatibility = opts.SurfaceCompatibility
	}
	return &Compiler{opts: merged}, nil
}

func (c *Compiler) Compile(src []byte) (*Result, error) {
	return c.CompileFiles([]NamedSource{{Name: "main.atlx", Data: src}})
}

func (c *Compiler) CompileFiles(sources []NamedSource) (*Result, error) {
	c.diags = nil
	file, err := parsePackageSources(sources, c.opts.Resolver)
	if err != nil {
		return nil, err
	}
	if diags, err := c.collectCompatibilityDiagnostics(sources, file); err != nil {
		return nil, err
	} else {
		c.diags = append(c.diags, diags...)
	}
	if diags, err := normalizeSourceFile(file, c.opts.SurfaceCompatibility); err != nil {
		return nil, err
	} else {
		c.diags = append(c.diags, diags...)
	}
	if len(file.Contracts) != 1 {
		return nil, fail("E_CONTRACT_COUNT", Position{}, "package must declare exactly one contract")
	}
	contract := file.Contracts[0]
	allFunctions := append([]*FunctionDecl(nil), file.Functions...)
	allFunctions = append(allFunctions, contract.Functions...)
	functions, err := buildFunctionMap(allFunctions)
	if err != nil {
		return nil, err
	}
	if err := inferFunctionPurity(allFunctions, functions); err != nil {
		return nil, err
	}
	result := &Result{
		Source:             file,
		Contract:           contract,
		MessageCodecs:      map[string]Codec{},
		MessageBodies:      map[string]Codec{},
		MessageBodyOpcodes: map[string]uint32{},
		MessageUnions:      map[string]MessageUnion{},
		GetterCodecs:       map[string]Codec{},
		EventCodecs:        map[string]Codec{},
	}
	lock, err := c.buildDependencyLock(file)
	if err != nil {
		return nil, err
	}
	result.DependencyLock = lock

	consts, err := c.buildConstEnv(file, functions)
	if err != nil {
		return nil, err
	}
	if err := c.typecheck(file, contract, functions, consts); err != nil {
		return nil, err
	}

	manifest, registry, layout, storageCodec, msgCodecs, bodyCodecs, bodyOpcodes, unions, getterCodecs, eventCodecs, err := c.buildArtifacts(file, contract)
	if err != nil {
		return nil, err
	}
	result.Manifest = manifest
	result.ManifestHash, err = avm.InterfaceHash(manifest)
	if err != nil {
		return nil, err
	}
	result.SelectorRegistry = registry
	result.StorageLayout = layout
	result.StorageCodec = storageCodec
	result.MessageCodecs = msgCodecs
	result.MessageBodies = bodyCodecs
	result.MessageBodyOpcodes = bodyOpcodes
	result.MessageUnions = unions
	result.GetterCodecs = getterCodecs
	result.EventCodecs = eventCodecs

	module, moduleBytes, ir, err := c.buildModule(file, contract, manifest, result.SelectorRegistry, msgCodecs, getterCodecs, eventCodecs, bodyOpcodes, lock, functions)
	if err != nil {
		return nil, err
	}
	result.Module = module
	result.ModuleBytes = moduleBytes
	result.IR = ir
	result.ModuleHash, err = avm.CodeHash(module)
	if err != nil {
		return nil, err
	}
	result.CodeChunk, err = buildChunkTree(moduleBytes)
	if err != nil {
		return nil, err
	}
	result.CodeChunkHash = [32]byte{}
	if result.CodeChunk != nil {
		copy(result.CodeChunkHash[:], result.CodeChunk.Hash())
	}

	stateInit, stateInitHash, err := c.buildStateInit(contract, result.ModuleHash, storageCodec, layout, lock)
	if err != nil {
		return nil, err
	}
	result.StateInit = stateInit
	result.StateInitHash = stateInitHash
	result.Diagnostics = append([]Diagnostic(nil), c.diags...)

	if len(result.ModuleBytes) > int(c.opts.MaxCodeBytes) {
		return nil, fail("E_CODE_SIZE", Position{}, fmt.Sprintf("generated module exceeds code size limit %d", c.opts.MaxCodeBytes))
	}
	if storageBytes, err := result.StorageCodec.EncodeDefaults(); err != nil {
		return nil, err
	} else if len(storageBytes) > int(c.opts.MaxStorageBytes) {
		return nil, fail("E_STORAGE_SIZE", Position{}, fmt.Sprintf("generated storage exceeds size limit %d", c.opts.MaxStorageBytes))
	}
	if stateInitBytes, err := encodeStateInit(result.StateInit); err != nil {
		return nil, err
	} else if uint32(len(stateInitBytes)) > c.opts.MaxStateInitBytes {
		return nil, fail("E_STATEINIT_SIZE", Position{}, fmt.Sprintf("generated state init exceeds size limit %d", c.opts.MaxStateInitBytes))
	}
	return result, nil
}

func parsePackageSources(sources []NamedSource, resolver DependencyResolver) (*SourceFile, error) {
	if len(sources) == 0 {
		return nil, fail("E_SOURCE", Position{}, "no source files supplied")
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	merged := &SourceFile{}
	seenImports := map[string]struct{}{}
	for _, src := range sources {
		file, err := ParseSourceNamed(src.Name, string(src.Data))
		if err != nil {
			return nil, err
		}
		if file.Package != "" {
			if merged.Package != "" && merged.Package != file.Package {
				return nil, fail("E_PACKAGE", Position{}, fmt.Sprintf("source %q declares package %q, expected %q", src.Name, file.Package, merged.Package))
			}
			merged.Package = file.Package
		}
		for _, imp := range file.Imports {
			key := imp.Path + "@" + imp.Version + "#" + imp.Alias
			if _, ok := seenImports[key]; ok {
				continue
			}
			seenImports[key] = struct{}{}
			merged.Imports = append(merged.Imports, imp)
		}
		merged.Structs = append(merged.Structs, file.Structs...)
		merged.Consts = append(merged.Consts, file.Consts...)
		merged.Enums = append(merged.Enums, file.Enums...)
		merged.Types = append(merged.Types, file.Types...)
		merged.Functions = append(merged.Functions, file.Functions...)
		merged.Contracts = append(merged.Contracts, file.Contracts...)
	}
	if resolver != nil {
		if err := mergeResolvedImports(merged, resolver, map[string]struct{}{}); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(merged.Imports, func(i, j int) bool {
		a := merged.Imports[i].Path + "@" + merged.Imports[i].Version
		b := merged.Imports[j].Path + "@" + merged.Imports[j].Version
		if a == b {
			return merged.Imports[i].Alias < merged.Imports[j].Alias
		}
		return a < b
	})
	return merged, nil
}

func mergeResolvedImports(merged *SourceFile, resolver DependencyResolver, seen map[string]struct{}) error {
	imports := append([]ImportDecl(nil), merged.Imports...)
	sort.SliceStable(imports, func(i, j int) bool {
		a := imports[i].Path + "@" + imports[i].Version + "#" + imports[i].Alias
		b := imports[j].Path + "@" + imports[j].Version + "#" + imports[j].Alias
		return a < b
	})
	for _, imp := range imports {
		if err := mergeImportedSource(merged, resolver, seen, imp); err != nil {
			return err
		}
	}
	return nil
}

func mergeImportedSource(merged *SourceFile, resolver DependencyResolver, seen map[string]struct{}, imp ImportDecl) error {
	key := imp.Path + "@" + imp.Version + "#" + imp.Alias
	if _, ok := seen[key]; ok {
		return nil
	}
	seen[key] = struct{}{}
	_, src, err := resolver.ResolveImport(imp)
	if err != nil {
		return err
	}
	if src == nil {
		return nil
	}
	if merged.Package == "" && src.Package != "" {
		merged.Package = src.Package
	}
	merged.Imports = append(merged.Imports, src.Imports...)
	merged.Structs = append(merged.Structs, src.Structs...)
	merged.Consts = append(merged.Consts, src.Consts...)
	merged.Enums = append(merged.Enums, src.Enums...)
	merged.Types = append(merged.Types, src.Types...)
	merged.Functions = append(merged.Functions, src.Functions...)
	merged.Contracts = append(merged.Contracts, src.Contracts...)
	return mergeResolvedImports(merged, resolver, seen)
}

func buildFunctionMap(funcs []*FunctionDecl) (map[string]*FunctionDecl, error) {
	out := make(map[string]*FunctionDecl, len(funcs))
	for _, fn := range funcs {
		if fn == nil {
			return nil, fail("E_FUNCTION", Position{}, "nil function")
		}
		if _, ok := out[fn.Name]; ok {
			return nil, fail("E_DUP_FUNCTION", fn.Pos, fmt.Sprintf("duplicate function %q", fn.Name))
		}
		out[fn.Name] = fn
	}
	return out, nil
}

func inferFunctionPurity(funcs []*FunctionDecl, functions map[string]*FunctionDecl) error {
	if len(funcs) == 0 {
		return nil
	}
	funcNames := make(map[string]bool, len(functions))
	for name := range functions {
		funcNames[name] = true
	}
	pure := make(map[string]bool, len(funcs))
	calls := make(map[string][]string, len(funcs))
	for _, fn := range funcs {
		if fn == nil {
			continue
		}
		hasHandler := false
		if ann, ok := functionHandlerAnnotation(fn.Annotations); ok && ann != "" {
			hasHandler = true
		}
		pure[fn.Name] = !hasHandler && !isStoreStatefulFunction(fn) && !functionHasDirectEffects(fn.Body) && !hasImpureAnnotation(fn.Annotations)
		calls[fn.Name] = collectFunctionCallsFromStatements(fn.Body, funcNames)
	}
	changed := true
	for changed {
		changed = false
		for _, fn := range funcs {
			if fn == nil || !pure[fn.Name] {
				continue
			}
			for _, call := range calls[fn.Name] {
				if callee, ok := functions[call]; ok && callee != nil && !pure[callee.Name] {
					pure[fn.Name] = false
					changed = true
					break
				}
			}
		}
	}
	for _, fn := range funcs {
		if fn == nil {
			continue
		}
		fn.Pure = pure[fn.Name]
	}
	return nil
}

func functionHasDirectEffects(stmts []Statement) bool {
	_, ok := functionDirectEffectMessage(stmts)
	return ok
}

func functionDirectEffectMessage(stmts []Statement) (string, bool) {
	for _, stmt := range stmts {
		switch stmt.Kind {
		case StatementExpr:
			if stmt.Value.Kind == ExprCall && len(stmt.Value.Path) >= 2 {
				method := strings.ToLower(stmt.Value.Path[len(stmt.Value.Path)-1])
				switch method {
				case "setdata", "save", "touch", "deletedata":
					return "pure functions cannot write state or perform chain-visible side effects", true
				}
			}
		case StatementSet:
			return "pure functions cannot write state or perform chain-visible side effects", true
		case StatementEmit:
			return "pure functions cannot emit events or perform chain-visible side effects", true
		case StatementRefund, StatementSend, StatementSelf:
			return "pure functions cannot send/refund/schedule self or perform chain-visible side effects", true
		case StatementIf:
			if msg, ok := functionDirectEffectMessage(stmt.Then); ok {
				return msg, true
			}
			if msg, ok := functionDirectEffectMessage(stmt.Else); ok {
				return msg, true
			}
		case StatementWhile, StatementDo, StatementRepeat, StatementFor:
			if msg, ok := functionDirectEffectMessage(stmt.Then); ok {
				return msg, true
			}
		case StatementMatch:
			for _, arm := range stmt.Arms {
				if msg, ok := functionDirectEffectMessage(arm.Body); ok {
					return msg, true
				}
			}
		}
	}
	return "", false
}

func (c *Compiler) buildDependencyLock(file *SourceFile) (DependencyLock, error) {
	lock := DependencyLock{Package: file.Package}
	stdHash := standards.DefaultRegistry().Hash()
	lock.Entries = append(lock.Entries, dependencyFromParts("avm/stdlib", standards.CanonicalVersion, "", stdHash, stdHash))
	seen := map[string]struct{}{}
	stack := map[string]struct{}{}
	var visitImports func([]ImportDecl) error
	visitImports = func(imports []ImportDecl) error {
		for _, imp := range imports {
			key := imp.Path + "@" + imp.Version + "#" + imp.Alias
			if _, ok := stack[key]; ok {
				return fail("E_IMPORT_CYCLE", imp.Pos, fmt.Sprintf("import cycle detected at %q", key))
			}
			if _, ok := seen[key]; ok {
				continue
			}
			stack[key] = struct{}{}
			var dep ResolvedDependency
			var imported *SourceFile
			var err error
			if c.opts.Resolver != nil {
				dep, imported, err = c.opts.Resolver.ResolveImport(imp)
				if err != nil {
					return err
				}
			} else {
				sourceHash := sha256.Sum256([]byte(imp.Path + "@" + imp.Version))
				abiHash := sha256.Sum256([]byte("abi:" + imp.Path + "@" + imp.Version))
				dep = dependencyFromParts(imp.Path, imp.Version, imp.Alias, abiHash, sourceHash)
			}
			if dep.Path == "" {
				dep.Path = imp.Path
			}
			if dep.Version == "" {
				dep.Version = imp.Version
			}
			if dep.Alias == "" {
				dep.Alias = imp.Alias
			}
			if dep.LockHash == ([32]byte{}) {
				dep = dependencyFromParts(dep.Path, dep.Version, dep.Alias, dep.ABIHash, dep.SourceHash)
			}
			lock.Entries = append(lock.Entries, dep)
			seen[key] = struct{}{}
			if imported != nil {
				if err := visitImports(imported.Imports); err != nil {
					return err
				}
			}
			delete(stack, key)
		}
		return nil
	}
	if err := visitImports(file.Imports); err != nil {
		return DependencyLock{}, err
	}
	sort.SliceStable(lock.Entries, func(i, j int) bool {
		a := lock.Entries[i].Path + "@" + lock.Entries[i].Version
		b := lock.Entries[j].Path + "@" + lock.Entries[j].Version
		if a == b {
			return lock.Entries[i].Alias < lock.Entries[j].Alias
		}
		return a < b
	})
	lock.LockHash = sha256.Sum256(dependencyLockBytes(lock))
	return lock, nil
}

func dependencyFromParts(path, version, alias string, abiHash, sourceHash [32]byte) ResolvedDependency {
	dep := ResolvedDependency{Path: path, Version: version, Alias: alias, ABIHash: abiHash, SourceHash: sourceHash}
	dep.LockHash = sha256.Sum256(dependencyBytes(dep))
	return dep
}

func (c *Compiler) typecheck(file *SourceFile, contract *ContractDecl, functions map[string]*FunctionDecl, consts map[string]constValue) error {
	allFunctions := append([]*FunctionDecl(nil), file.Functions...)
	allFunctions = append(allFunctions, contract.Functions...)
	structs := map[string]*StructDecl{}
	enums := map[string]*EnumDecl{}
	types := map[string]*TypeDecl{}
	for _, st := range file.Structs {
		if _, ok := structs[st.Name]; ok {
			return fail("E_DUP_STRUCT", st.Pos, fmt.Sprintf("duplicate struct %q", st.Name))
		}
		structs[st.Name] = st
	}
	for _, en := range file.Enums {
		if _, ok := enums[en.Name]; ok {
			return fail("E_DUP_ENUM", en.Pos, fmt.Sprintf("duplicate enum %q", en.Name))
		}
		enums[en.Name] = en
	}
	for _, td := range file.Types {
		if _, ok := types[td.Name]; ok {
			return fail("E_DUP_TYPE", td.Pos, fmt.Sprintf("duplicate type %q", td.Name))
		}
		types[td.Name] = td
	}
	var storage *StructDecl
	if contract.StorageTypeName != "" {
		var ok bool
		storage, ok = structs[contract.StorageTypeName]
		if !ok {
			return fail("E_STORAGE_TYPE", contract.Pos, fmt.Sprintf("storage type %q not found", contract.StorageTypeName))
		}
	}
	seenCallables := map[string]struct{}{}
	if storage != nil {
		if err := c.validateStruct(storage, structs, enums, types, consts, true); err != nil {
			return err
		}
	}
	if err := inferMissingReturnTypes(allFunctions, storage, structs, enums, types, functions, consts); err != nil {
		return err
	}
	for _, st := range file.Structs {
		if err := c.validateStruct(st, structs, enums, types, consts, st == storage); err != nil {
			return err
		}
	}
	for _, en := range file.Enums {
		if err := c.validateEnum(en, structs, enums, types); err != nil {
			return err
		}
	}
	for _, td := range file.Types {
		if err := c.validateTypeDecl(td, structs, enums, types); err != nil {
			return err
		}
	}
	for _, fn := range file.Functions {
		if err := c.validateFunction(fn, false, structs, enums, types, functions, consts); err != nil {
			return err
		}
	}
	for _, fn := range contract.Functions {
		if err := c.validateFunction(fn, true, structs, enums, types, functions, consts); err != nil {
			return err
		}
	}
	if err := validateCanonicalHandlerUniqueness(contract.Functions); err != nil {
		return err
	}
	for _, msg := range contract.Messages {
		if _, ok := seenCallables[msg.Name]; ok {
			return fail("E_DUP_CALLABLE", msg.Pos, fmt.Sprintf("duplicate callable name %q", msg.Name))
		}
		seenCallables[msg.Name] = struct{}{}
		if err := c.validateMessage(msg, contract, storage, structs, enums, types, functions, consts); err != nil {
			return err
		}
	}
	for _, get := range contract.Getters {
		if _, ok := seenCallables[get.Name]; ok {
			return fail("E_DUP_CALLABLE", get.Pos, fmt.Sprintf("duplicate callable name %q", get.Name))
		}
		seenCallables[get.Name] = struct{}{}
		if err := c.validateGetter(get, contract, storage, structs, enums, types, functions, consts); err != nil {
			return err
		}
	}
	for _, event := range contract.Events {
		// event names are part of the exported ABI surface and must remain unique.
		// The manifest hash will sort and commit them, but we reject duplicates early.
		// This keeps selector/topic collisions explicit during compilation.
		if _, ok := seenCallables[event.Name]; ok {
			return fail("E_DUP_EVENT", event.Pos, fmt.Sprintf("duplicate event name %q", event.Name))
		}
		seenCallables[event.Name] = struct{}{}
		if err := c.validateEvent(event, structs, enums, types); err != nil {
			return err
		}
	}
	for _, wallet := range contract.WalletActions {
		if err := c.validateWallet(wallet, contract); err != nil {
			return err
		}
	}
	hasBounce := false
	for _, msg := range contract.Messages {
		if msg.Kind == MessageKindBounced {
			hasBounce = true
			break
		}
	}
	needsBounce := false
	for _, msg := range contract.Messages {
		if msg.Kind == MessageKindExternal || msg.Kind == MessageKindInternal || msg.Kind == MessageKindDeploy {
			needsBounce = true
			break
		}
	}
	if needsBounce && !hasBounce {
		return fail("E_BOUNCE_HANDLER", contract.Pos, "contract has bounceable entrypoints but no bounced handler")
	}
	if err := validateFunctionRecursion(allFunctions); err != nil {
		return err
	}
	if err := CheckResourceAbilities(file); err != nil {
		return err
	}
	return nil
}

func (c *Compiler) validateFunction(fn *FunctionDecl, inContract bool, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl, functions map[string]*FunctionDecl, consts map[string]constValue) error {
	if fn == nil {
		return fail("E_FUNCTION", Position{}, "nil function")
	}
	if handlerAnn, ok := functionHandlerAnnotation(fn.Annotations); ok {
		if !inContract {
			return fail("E_HANDLER_SCOPE", fn.Pos, fmt.Sprintf("%s handlers are only allowed inside contract blocks", handlerAnn))
		}
		expectedName := handlerExpectedName(handlerAnn)
		if fn.Name != expectedName {
			if reservedAnn, reserved := reservedHandlerAnnotation(fn.Name); reserved {
				return fail("E_HANDLER_NAME", fn.Pos, fmt.Sprintf("Function name `%s` is reserved for `%s` handlers. Expected `%s` for `%s`.", fn.Name, reservedAnn, expectedName, handlerAnn))
			}
			return fail("E_HANDLER_NAME", fn.Pos, fmt.Sprintf("Expected function name `%s` for handler annotated with `%s`.", expectedName, handlerAnn))
		}
		if err := validateCanonicalHandlerSignature(handlerAnn, fn); err != nil {
			return err
		}
	} else if reservedAnn, ok := reservedHandlerAnnotation(fn.Name); ok {
		return fail("E_HANDLER_NAME", fn.Pos, fmt.Sprintf("`%s` is a reserved message handler name and can only be used with `%s`.", fn.Name, reservedAnn))
	}
	if hasPureAnnotation(fn.Annotations) && !fn.Pure {
		return fail("E_PURE_DECL", fn.Pos, fmt.Sprintf("function %q is annotated @pure but has side effects", fn.Name))
	}
	if _, ok := functionHandlerAnnotation(fn.Annotations); !ok {
		if msg, ok := functionDirectEffectMessage(fn.Body); ok && !hasImpureAnnotation(fn.Annotations) && !hasStoreAnnotation(fn.Annotations) {
			return fail("E_PURE_DECL", fn.Pos, msg)
		}
	}
	if err := c.validateCallableName(fn.Name, fn.Pos); err != nil {
		return err
	}
	if err := validateParamNames(fn.Params, "function "+fn.Name, fn.Pos); err != nil {
		return err
	}
	if fn.ReturnType.Name != "" {
		if err := c.validateType(fn.ReturnType, structs, enums, types); err != nil {
			return err
		}
	}
	env := c.buildEnv(fn.Params, nil, structs, enums)
	mutables := c.buildMutableEnv(fn.Params, false)
	scope := c.initialScope(fn.Params, false)
	for _, stmt := range fn.Body {
		if err := c.validateStatement(stmt, env, mutables, scope, cloneConstEnv(consts), nil, structs, enums, types, &fn.ReturnType, functions, fn.Pure, 0); err != nil {
			return err
		}
	}
	return nil
}

func validateCanonicalHandlerSignature(annotation string, fn *FunctionDecl) error {
	if fn == nil {
		return fail("E_FUNCTION", Position{}, "nil function")
	}
	if len(fn.Params) != 1 {
		return fail("E_HANDLER_SIGNATURE", fn.Pos, fmt.Sprintf("handler %q must take exactly one parameter", fn.Name))
	}
	if fn.ReturnType.Name != "" {
		return fail("E_HANDLER_SIGNATURE", fn.Pos, fmt.Sprintf("handler %q must not declare a return type", fn.Name))
	}
	switch annotation {
	case "@internal":
		if fn.Params[0].Name != "in" || fn.Params[0].Type.Name != "InMessage" {
			return fail("E_HANDLER_SIGNATURE", fn.Pos, "internal handler must be `func onInternalMessage(in: InMessage)`")
		}
	case "@external":
		if fn.Params[0].Name != "inMsg" || fn.Params[0].Type.Name != "Segment" {
			return fail("E_HANDLER_SIGNATURE", fn.Pos, "external handler must be `func onExternalMessage(inMsg: Segment)`")
		}
	case "@bounced":
		if fn.Params[0].Name != "in" || fn.Params[0].Type.Name != "InMessageBounced" {
			return fail("E_HANDLER_SIGNATURE", fn.Pos, "bounced handler must be `func onBouncedMessage(in: InMessageBounced)`")
		}
	default:
		return fail("E_HANDLER_SIGNATURE", fn.Pos, fmt.Sprintf("unsupported handler annotation %q", annotation))
	}
	return nil
}

func validateCanonicalHandlerUniqueness(funcs []*FunctionDecl) error {
	counts := map[string]int{}
	positions := map[string]Position{}
	for _, fn := range funcs {
		if fn == nil {
			continue
		}
		kind, ok := functionHandlerAnnotation(fn.Annotations)
		if !ok {
			continue
		}
		counts[kind]++
		if _, exists := positions[kind]; !exists {
			positions[kind] = fn.Pos
		}
	}
	for _, kind := range []string{"@internal", "@external", "@bounced"} {
		if counts[kind] > 1 {
			return fail("E_HANDLER_DUP", positions[kind], fmt.Sprintf("only one %s handler is allowed per contract", kind))
		}
	}
	return nil
}

func hasPureAnnotation(annotations []Annotation) bool {
	for _, annotation := range annotations {
		if annotation.Name == "@pure" {
			return true
		}
	}
	return false
}

func hasImpureAnnotation(annotations []Annotation) bool {
	for _, annotation := range annotations {
		if annotation.Name == "@impure" {
			return true
		}
	}
	return false
}

func hasStoreAnnotation(annotations []Annotation) bool {
	for _, annotation := range annotations {
		if annotation.Name == "@store" {
			return true
		}
	}
	return false
}

func isStoreStatefulFunction(fn *FunctionDecl) bool {
	if fn == nil || !hasStoreAnnotation(fn.Annotations) {
		return false
	}
	name := strings.ToLower(fn.Name)
	return strings.HasSuffix(name, ".save") || strings.HasSuffix(name, ".touch") || strings.HasSuffix(name, ".delete")
}

func hasAnnotation(annotations []Annotation, name string) bool {
	for _, annotation := range annotations {
		if annotation.Name == name {
			return true
		}
	}
	return false
}

func (c *Compiler) validateStruct(st *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl, consts map[string]constValue, allowLazy bool) error {
	if st == nil {
		return fail("E_STRUCT", Position{}, "nil struct")
	}
	seen := map[string]struct{}{}
	for _, field := range st.Fields {
		if field.Name == "" {
			return fail("E_FIELD_NAME", field.Pos, fmt.Sprintf("struct %q has empty field name", st.Name))
		}
		if _, ok := seen[field.Name]; ok {
			return fail("E_DUP_FIELD", field.Pos, fmt.Sprintf("struct %q has duplicate field %q", st.Name, field.Name))
		}
		seen[field.Name] = struct{}{}
		if field.Lazy && !allowLazy {
			return fail("E_LAZY_FIELD", field.Pos, fmt.Sprintf("lazy field %q is only allowed in storage structs", field.Name))
		}
		if err := c.validateType(field.Type, structs, enums, types); err != nil {
			return err
		}
		if field.Default.Kind != "" {
			typ, err := c.inferExprType(field.Default, nil, st, structs, enums, types, nil, consts, false)
			if err != nil {
				return err
			}
			if !compatibleTypesResolved(typ, field.Type, types) {
				return fail("E_DEFAULT_TYPE", field.Pos, fmt.Sprintf("default for %s.%s has incompatible type %s", st.Name, field.Name, typ.String()))
			}
		}
	}
	return nil
}

func (c *Compiler) validateEnum(en *EnumDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl) error {
	if en == nil {
		return fail("E_ENUM", Position{}, "nil enum")
	}
	seen := map[string]struct{}{}
	for _, variant := range en.Variants {
		if _, ok := seen[variant.Name]; ok {
			return fail("E_DUP_VARIANT", variant.Pos, fmt.Sprintf("enum %q has duplicate variant %q", en.Name, variant.Name))
		}
		seen[variant.Name] = struct{}{}
		for _, field := range variant.Fields {
			if err := c.validateType(field.Type, structs, enums, types); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Compiler) validateTypeDecl(td *TypeDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl) error {
	if td == nil {
		return fail("E_TYPE_DECL", Position{}, "nil type")
	}
	if !isValidName(td.Name) {
		return fail("E_NAME", td.Pos, fmt.Sprintf("invalid type name %q", td.Name))
	}
	if len(td.Members) == 0 {
		return fail("E_TYPE_EMPTY", td.Pos, fmt.Sprintf("type %q must declare at least one member", td.Name))
	}
	seen := map[string]struct{}{}
	for _, member := range td.Members {
		if err := c.validateType(member, structs, enums, types); err != nil {
			return err
		}
		if _, ok := seen[member.String()]; ok {
			return fail("E_DUP_TYPE_VARIANT", member.Pos, fmt.Sprintf("type %q has duplicate member %q", td.Name, member.String()))
		}
		seen[member.String()] = struct{}{}
	}
	if _, err := c.expandTypeDeclMembers(td.Name, types, map[string]bool{}); err != nil {
		return err
	}
	return nil
}

func (c *Compiler) validateMessage(msg *MessageDecl, contract *ContractDecl, storage *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl, functions map[string]*FunctionDecl, consts map[string]constValue) error {
	if msg == nil {
		return fail("E_MESSAGE", Position{}, "nil message")
	}
	if err := c.validateCallableName(msg.Name, msg.Pos); err != nil {
		return err
	}
	if err := validateParamNames(msg.Params, "message "+msg.Name, msg.Pos); err != nil {
		return err
	}
	for _, param := range msg.Params {
		if err := c.validateType(param.Type, structs, enums, types); err != nil {
			return err
		}
	}
	if msg.ReturnType != nil {
		if err := c.validateType(*msg.ReturnType, structs, enums, types); err != nil {
			return err
		}
	}
	env := c.buildEnv(msg.Params, storage, structs, enums)
	mutables := c.buildMutableEnv(msg.Params, storage != nil)
	scope := c.initialScope(msg.Params, storage != nil)
	for _, stmt := range msg.Body {
		if err := c.validateStatement(stmt, env, mutables, scope, cloneConstEnv(consts), storage, structs, enums, types, msg.ReturnType, functions, false, 0); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) validateGetter(get *GetterDecl, contract *ContractDecl, storage *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl, functions map[string]*FunctionDecl, consts map[string]constValue) error {
	if get == nil {
		return fail("E_GETTER", Position{}, "nil getter")
	}
	if err := c.validateCallableName(get.Name, get.Pos); err != nil {
		return err
	}
	if err := validateParamNames(get.Params, "getter "+get.Name, get.Pos); err != nil {
		return err
	}
	if get.ReturnType.Name != "" {
		if err := c.validateType(get.ReturnType, structs, enums, types); err != nil {
			return err
		}
	}
	env := c.buildEnv(get.Params, storage, structs, enums)
	mutables := c.buildMutableEnv(get.Params, storage != nil)
	scope := c.initialScope(get.Params, storage != nil)
	for _, stmt := range get.Body {
		if err := c.validateStatement(stmt, env, mutables, scope, cloneConstEnv(consts), storage, structs, enums, types, &get.ReturnType, functions, true, 0); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) inferFunctionReturnType(fn *FunctionDecl, storage *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl, functions map[string]*FunctionDecl, consts map[string]constValue) (TypeRef, bool, error) {
	env := c.buildEnv(fn.Params, storage, structs, enums)
	var inferred TypeRef
	var hasInferred bool
	var walk func([]Statement) error
	walk = func(stmts []Statement) error {
		for _, stmt := range stmts {
			switch stmt.Kind {
			case StatementReturn:
				typ, err := c.inferExprType(stmt.Value, env, storage, structs, enums, types, functions, consts, fn.Pure)
				if err != nil {
					return err
				}
				if !hasInferred {
					inferred = typ
					hasInferred = true
					continue
				}
				if !compatibleTypesResolved(typ, inferred, types) {
					return fail("E_RETURN_TYPE", stmt.Pos, fmt.Sprintf("return type %s does not match %s", typ.String(), inferred.String()))
				}
			case StatementIf:
				if err := walk(stmt.Then); err != nil {
					return err
				}
				if err := walk(stmt.Else); err != nil {
					return err
				}
			case StatementMatch:
				for _, arm := range stmt.Arms {
					if err := walk(arm.Body); err != nil {
						return err
					}
				}
			case StatementWhile, StatementDo, StatementRepeat, StatementFor:
				if err := walk(stmt.Then); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(fn.Body); err != nil {
		return TypeRef{}, false, err
	}
	return inferred, hasInferred, nil
}

func inferMissingReturnTypes(funcs []*FunctionDecl, storage *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl, functions map[string]*FunctionDecl, consts map[string]constValue) error {
	changed := true
	for changed {
		changed = false
		for _, fn := range funcs {
			if fn == nil || fn.ReturnType.Name != "" {
				continue
			}
			inferred, ok, err := (&Compiler{}).inferFunctionReturnType(fn, storage, structs, enums, types, functions, consts)
			if err != nil {
				return err
			}
			if ok {
				fn.ReturnType = inferred
				changed = true
			}
		}
	}
	return nil
}

func (c *Compiler) validateEvent(event *EventDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl) error {
	if event == nil {
		return fail("E_EVENT", Position{}, "nil event")
	}
	if err := c.validateCallableName(event.Name, event.Pos); err != nil {
		return err
	}
	if err := validateParamNames(event.FieldsToParams(), "event "+event.Name, event.Pos); err != nil {
		return err
	}
	for _, field := range event.Fields {
		if err := c.validateType(field.Type, structs, enums, types); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) validateWallet(wallet *WalletActionDecl, contract *ContractDecl) error {
	if wallet == nil {
		return fail("E_WALLET", Position{}, "nil wallet action")
	}
	if err := c.validateCallableName(wallet.Name, wallet.Pos); err != nil {
		return err
	}
	if err := validateParamNames(wallet.Inputs, "wallet action "+wallet.Name+" input", wallet.Pos); err != nil {
		return err
	}
	if err := validateParamNames(wallet.Outputs, "wallet action "+wallet.Name+" output", wallet.Pos); err != nil {
		return err
	}
	if strings.TrimSpace(wallet.Title) == "" {
		return fail("E_WALLET_TITLE", wallet.Pos, fmt.Sprintf("wallet action %q requires title", wallet.Name))
	}
	if !wallet.HasTitle {
		return fail("E_WALLET_TITLE", wallet.Pos, fmt.Sprintf("wallet action %q requires explicit title", wallet.Name))
	}
	if strings.TrimSpace(wallet.Risk) == "" {
		return fail("E_WALLET_RISK", wallet.Pos, fmt.Sprintf("wallet action %q requires risk", wallet.Name))
	}
	if !wallet.HasRisk {
		return fail("E_WALLET_RISK", wallet.Pos, fmt.Sprintf("wallet action %q requires explicit risk", wallet.Name))
	}
	if strings.TrimSpace(wallet.ConfirmLabel) == "" {
		return fail("E_WALLET_CONFIRM", wallet.Pos, fmt.Sprintf("wallet action %q requires confirm label", wallet.Name))
	}
	if !wallet.HasConfirmLabel {
		return fail("E_WALLET_CONFIRM", wallet.Pos, fmt.Sprintf("wallet action %q requires explicit confirm label", wallet.Name))
	}
	if strings.TrimSpace(wallet.WarningLevel) == "" {
		return fail("E_WALLET_WARNING", wallet.Pos, fmt.Sprintf("wallet action %q requires warning level", wallet.Name))
	}
	if !wallet.HasWarningLevel {
		return fail("E_WALLET_WARNING", wallet.Pos, fmt.Sprintf("wallet action %q requires explicit warning level", wallet.Name))
	}
	if !wallet.HasExpectedSideEffects {
		return fail("E_WALLET_EFFECTS", wallet.Pos, fmt.Sprintf("wallet action %q requires expected_side_effects", wallet.Name))
	}
	if !wallet.HasFundAccess {
		return fail("E_WALLET_FUND_ACCESS", wallet.Pos, fmt.Sprintf("wallet action %q requires fund_access", wallet.Name))
	}
	if strings.TrimSpace(wallet.ApprovalSemantics) == "" {
		return fail("E_WALLET_APPROVAL", wallet.Pos, fmt.Sprintf("wallet action %q requires approval semantics", wallet.Name))
	}
	if !wallet.HasApprovalSemantics {
		return fail("E_WALLET_APPROVAL", wallet.Pos, fmt.Sprintf("wallet action %q requires explicit approval semantics", wallet.Name))
	}
	return nil
}

func (c *Compiler) buildConstEnv(file *SourceFile, functions map[string]*FunctionDecl) (map[string]constValue, error) {
	consts := map[string]constValue{}
	env := loweringEnv{params: map[string]int{}, consts: consts}
	for _, decl := range file.Consts {
		value, ok := evalConstValue(decl.Value, env, functions, map[string]bool{})
		if !ok {
			return nil, fail("E_CONST", decl.Pos, fmt.Sprintf("const %s must be compile-time constant", decl.Name))
		}
		consts[decl.Name] = value
		env.consts[decl.Name] = value
	}
	return consts, nil
}

func (c *Compiler) validateCallableName(name string, pos Position) error {
	parts := strings.Split(name, ".")
	if len(parts) == 0 {
		return fail("E_NAME", pos, fmt.Sprintf("invalid callable name %q", name))
	}
	for _, part := range parts {
		if !isValidName(part) {
			return fail("E_NAME", pos, fmt.Sprintf("invalid callable name %q", name))
		}
	}
	if strings.Contains(name, "..") || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return fail("E_NAME", pos, fmt.Sprintf("invalid callable name %q", name))
	}
	return nil
}

func functionHandlerAnnotation(annotations []Annotation) (string, bool) {
	for _, annotation := range annotations {
		switch annotation.Name {
		case "@external", "@internal", "@bounced":
			return annotation.Name, true
		}
	}
	return "", false
}

func handlerAnnotationEntrypoint(annotation string) (avm.Entrypoint, bool) {
	switch annotation {
	case "@external":
		return avm.EntryReceiveExternal, true
	case "@internal":
		return avm.EntryReceiveInternal, true
	case "@bounced":
		return avm.EntryReceiveBounced, true
	default:
		return 0, false
	}
}

func handlerExpectedName(annotation string) string {
	switch annotation {
	case "@external":
		return "onExternalMessage"
	case "@internal":
		return "onInternalMessage"
	case "@bounced":
		return "onBouncedMessage"
	default:
		return ""
	}
}

func reservedHandlerAnnotation(name string) (string, bool) {
	switch name {
	case "onExternalMessage":
		return "@external", true
	case "onInternalMessage":
		return "@internal", true
	case "onBouncedMessage":
		return "@bounced", true
	default:
		return "", false
	}
}

func validateParamNames(params []ParamDecl, label string, pos Position) error {
	seen := map[string]struct{}{}
	for _, param := range params {
		if param.Name == "" {
			return fail("E_PARAM_NAME", pos, fmt.Sprintf("%s has empty parameter name", label))
		}
		if _, ok := seen[param.Name]; ok {
			return fail("E_DUP_PARAM", pos, fmt.Sprintf("%s has duplicate parameter %q", label, param.Name))
		}
		seen[param.Name] = struct{}{}
	}
	return nil
}

func (c *Compiler) validateType(typ TypeRef, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl) error {
	if typ.Name == "" {
		return fail("E_TYPE", typ.Pos, "empty type")
	}
	switch canonicalCodecTypeName(typ.Name) {
	case "bool", "u2", "u4", "u8", "u16", "u32", "u64", "u128", "u256", "i2", "i4", "i8", "i16", "i32", "i64", "i128", "i256", "uint2", "uint4", "uint8", "uint16", "uint32", "uint64", "uint128", "uint256", "int2", "int4", "int8", "int16", "int32", "int64", "int128", "int256", "bytes", "string", "hash32", "address", "coins", "timestamp", "messageenvelope", "inmessage", "inmessagebounced", "contractcontext":
	default:
		if desc, ok := standards.DefaultRegistry().Find(typ.Name); ok {
			if strings.EqualFold(desc.Name, "Chunk") && len(typ.Args) == 1 {
				break
			}
			if desc.Arity != len(typ.Args) {
				return fail("E_TYPE_ARITY", typ.Pos, fmt.Sprintf("type %q requires %d type arguments", typ.Name, desc.Arity))
			}
		} else if _, ok := structs[typ.Name]; !ok {
			if _, ok := enums[typ.Name]; !ok {
				if _, ok := types[typ.Name]; !ok {
					return fail("E_UNKNOWN_TYPE", typ.Pos, fmt.Sprintf("unknown type %q", typ.Name))
				}
			}
		}
	}
	for _, arg := range typ.Args {
		if err := c.validateType(arg, structs, enums, types); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) validateCallArgs(expr Expr, fn *FunctionDecl, env map[string]TypeRef, storage *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl, functions map[string]*FunctionDecl, consts map[string]constValue, inPure bool) error {
	if fn == nil {
		return fail("E_CALL", expr.Pos, "nil function")
	}
	if len(expr.Args) != len(fn.Params) {
		return fail("E_CALL_ARITY", expr.Pos, fmt.Sprintf("function %q expects %d args", fn.Name, len(fn.Params)))
	}
	for i, arg := range expr.Args {
		argType, err := c.inferExprType(arg, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return err
		}
		if !compatibleTypes(argType, fn.Params[i].Type) {
			return fail("E_CALL_TYPE", arg.Pos, fmt.Sprintf("argument %q has type %s, want %s", fn.Params[i].Name, argType.String(), fn.Params[i].Type.String()))
		}
	}
	return nil
}

func (c *Compiler) inferBuiltinMethodCallType(expr Expr, env map[string]TypeRef, storage *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl, functions map[string]*FunctionDecl, consts map[string]constValue, inPure bool) (TypeRef, bool, error) {
	if len(expr.Path) < 2 {
		return TypeRef{}, false, nil
	}
	method := strings.ToLower(expr.Path[len(expr.Path)-1])
	receiverPath := append([]string(nil), expr.Path[:len(expr.Path)-1]...)
	var receiverType TypeRef
	var receiverKnown bool
	if len(receiverPath) == 1 {
		switch receiverPath[0] {
		case "contract":
			receiverType = TypeRef{Name: "ContractContext"}
			receiverKnown = true
		default:
			if st, ok := structs[receiverPath[0]]; ok {
				receiverType = TypeRef{Name: st.Name}
				receiverKnown = true
			} else if _, ok := types[receiverPath[0]]; ok {
				receiverType = TypeRef{Name: receiverPath[0]}
				receiverKnown = true
			} else if _, ok := enums[receiverPath[0]]; ok {
				receiverType = TypeRef{Name: receiverPath[0]}
				receiverKnown = true
			} else if typ, ok := env[receiverPath[0]]; ok {
				receiverType = typ
				receiverKnown = true
			} else if desc, ok := standards.DefaultRegistry().Find(receiverPath[0]); ok {
				receiverType = TypeRef{Name: desc.Name}
				receiverKnown = true
			}
		}
	}
	if !receiverKnown {
		if typ, err := c.resolvePathType(receiverPath, env, storage, structs, enums, types, expr.Pos); err == nil {
			receiverType = typ
			receiverKnown = true
		}
	}
	receiverType = resolveSingleMemberTypeRef(receiverType, types)

	mapMethodType := func() (TypeRef, TypeRef, bool) {
		if !receiverKnown || !isMapFamilyType(receiverType.Name) {
			return TypeRef{}, TypeRef{}, false
		}
		keyType := TypeRef{Name: "bytes"}
		valueType := TypeRef{Name: "bytes"}
		if len(receiverType.Args) > 0 {
			keyType = receiverType.Args[0]
		}
		if len(receiverType.Args) > 1 {
			valueType = receiverType.Args[1]
		}
		return keyType, valueType, true
	}

	validateArity := func(expected int) (TypeRef, bool) {
		if len(expr.Args) != expected {
			return TypeRef{}, false
		}
		return receiverType, true
	}

	// pureMutation reports a purity-bypass error for a mutating builtin
	// (setData/deleteData/save/touch and map set/delete) invoked from a @pure
	// function or a @get getter/getter-block. This single guard covers every
	// mutating builtin for both function and block forms.
	pureMutation := func(op string) error {
		return fail("E_PURE_MUTATION", expr.Pos, fmt.Sprintf("pure functions and getters cannot call %s, which mutates state", op))
	}
	switch method {
	case "empty":
		if len(expr.Args) != 0 {
			return TypeRef{}, false, nil
		}
		if receiverKnown && isMapFamilyType(receiverType.Name) {
			return receiverType, true, nil
		}
	case "fromsegment", "fromchunk", "fromstate", "fromhex", "frombase64":
		if len(expr.Args) != 1 {
			return TypeRef{}, false, nil
		}
		if receiverKnown {
			return receiverType, true, nil
		}
		if len(receiverPath) == 1 {
			return TypeRef{Name: receiverPath[0]}, true, nil
		}
	case "tochunk":
		if len(expr.Args) != 0 {
			return TypeRef{}, false, nil
		}
		if receiverKnown {
			return TypeRef{Name: "Chunk"}, true, nil
		}
	case "hash":
		if len(expr.Args) != 0 {
			return TypeRef{}, false, nil
		}
		if receiverKnown {
			return TypeRef{Name: "hash32"}, true, nil
		}
	case "bitshash":
		if len(expr.Args) != 0 {
			return TypeRef{}, false, nil
		}
		if receiverKnown {
			return TypeRef{Name: "hash32"}, true, nil
		}
	case "len":
		if len(expr.Args) != 0 {
			return TypeRef{}, false, nil
		}
		if receiverKnown {
			return TypeRef{Name: "uint64"}, true, nil
		}
	case "get":
		if len(expr.Args) != 1 {
			return TypeRef{}, false, nil
		}
		keyType, valueType, ok := mapMethodType()
		if !ok {
			return TypeRef{}, false, nil
		}
		argType, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return TypeRef{}, false, nil
		}
		if !compatibleTypesResolved(argType, keyType, types) {
			return TypeRef{}, false, nil
		}
		return TypeRef{Name: valueType.Name, Args: append([]TypeRef(nil), valueType.Args...), Optional: true}, true, nil
	case "set":
		if len(expr.Args) != 2 {
			return TypeRef{}, false, nil
		}
		keyType, valueType, ok := mapMethodType()
		if !ok {
			return TypeRef{}, false, nil
		}
		if inPure {
			return TypeRef{}, true, pureMutation("map set()")
		}
		keyArg, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return TypeRef{}, false, nil
		}
		if !compatibleTypesResolved(keyArg, keyType, types) {
			return TypeRef{}, false, nil
		}
		valArg, err := c.inferExprType(expr.Args[1], env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return TypeRef{}, false, nil
		}
		if !compatibleTypesResolved(valArg, valueType, types) {
			return TypeRef{}, false, nil
		}
		return receiverType, true, nil
	case "has":
		if len(expr.Args) != 1 {
			return TypeRef{}, false, nil
		}
		keyType, _, ok := mapMethodType()
		if !ok {
			return TypeRef{}, false, nil
		}
		argType, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return TypeRef{}, false, nil
		}
		if !compatibleTypesResolved(argType, keyType, types) {
			return TypeRef{}, false, nil
		}
		return TypeRef{Name: "bool"}, true, nil
	case "delete":
		if len(expr.Args) != 1 {
			return TypeRef{}, false, nil
		}
		keyType, _, ok := mapMethodType()
		if !ok {
			return TypeRef{}, false, nil
		}
		if inPure {
			return TypeRef{}, true, pureMutation("map delete()")
		}
		argType, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return TypeRef{}, false, nil
		}
		if !compatibleTypesResolved(argType, keyType, types) {
			return TypeRef{}, false, nil
		}
		return receiverType, true, nil
	case "keys":
		if len(expr.Args) != 1 {
			return TypeRef{}, false, nil
		}
		keyType, _, ok := mapMethodType()
		if !ok {
			return TypeRef{}, false, nil
		}
		argType, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return TypeRef{}, false, nil
		}
		if !isNumericLikeType(argType, types) {
			return TypeRef{}, false, nil
		}
		return TypeRef{Name: "List", Args: []TypeRef{keyType}}, true, nil
	case "entries":
		if len(expr.Args) != 1 {
			return TypeRef{}, false, nil
		}
		keyType, valueType, ok := mapMethodType()
		if !ok {
			return TypeRef{}, false, nil
		}
		argType, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return TypeRef{}, false, nil
		}
		if !isNumericLikeType(argType, types) {
			return TypeRef{}, false, nil
		}
		return TypeRef{Name: "List", Args: []TypeRef{{Name: "MapEntry", Args: []TypeRef{keyType, valueType}}}}, true, nil
	case "getdata":
		if _, ok := validateArity(0); ok {
			return TypeRef{Name: "Chunk"}, true, nil
		}
	case "deletedata":
		if _, ok := validateArity(0); ok {
			if inPure {
				return TypeRef{}, true, pureMutation("deleteData()")
			}
			return TypeRef{Name: "Chunk"}, true, nil
		}
	case "setdata":
		if _, ok := validateArity(1); ok {
			if inPure {
				return TypeRef{}, true, pureMutation("setData()")
			}
			return TypeRef{Name: "Chunk"}, true, nil
		}
	case "touch", "save":
		if _, ok := validateArity(0); ok {
			if inPure {
				return TypeRef{}, true, pureMutation(method + "()")
			}
			if receiverKnown {
				return receiverType, true, nil
			}
			return TypeRef{Name: "uint64"}, true, nil
		}
	case "isempty":
		if _, ok := validateArity(0); ok {
			return TypeRef{Name: "bool"}, true, nil
		}
	case "skipbouncedprefix":
		if _, ok := validateArity(0); ok {
			if receiverKnown {
				return receiverType, true, nil
			}
			return TypeRef{Name: "Segment"}, true, nil
		}
	}
	return TypeRef{}, false, nil
}

func (c *Compiler) validateStatement(stmt Statement, env map[string]TypeRef, mutables map[string]bool, scope map[string]struct{}, consts map[string]constValue, storage *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl, ret *TypeRef, functions map[string]*FunctionDecl, inPure bool, loopDepth int) error {
	switch stmt.Kind {
	case StatementBinding:
		if _, exists := scope[stmt.Name]; exists {
			return fail("E_DUP_BINDING", stmt.Pos, fmt.Sprintf("duplicate binding %q in the same scope", stmt.Name))
		}
		typ, err := c.inferExprType(stmt.Value, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return err
		}
		scope[stmt.Name] = struct{}{}
		env[stmt.Name] = typ
		if mutables == nil {
			mutables = map[string]bool{}
		}
		mutables[stmt.Name] = stmt.Mutable
		if !stmt.Mutable {
			if value, ok := evalConstValue(stmt.Value, loweringEnv{params: map[string]int{}, consts: consts}, functions, map[string]bool{}); ok {
				consts[stmt.Name] = value
			}
		}
		return nil
	case StatementSet:
		if inPure {
			return fail("E_PURE_MUTATION", stmt.Pos, "pure functions cannot write state or perform chain-visible side effects")
		}
		if len(stmt.Path) == 1 {
			root := stmt.Path[0]
			if isMutable, ok := mutables[root]; ok {
				if !isMutable {
					return fail("E_SET_IMMUTABLE", stmt.Pos, fmt.Sprintf("cannot assign to immutable binding %q", root))
				}
				typ, err := c.inferExprType(stmt.Value, env, storage, structs, enums, types, functions, consts, inPure)
				if err != nil {
					return err
				}
				if !compatibleTypesResolved(typ, env[root], types) {
					return fail("E_SET_TYPE", stmt.Pos, fmt.Sprintf("cannot assign %s to %s", typ.String(), env[root].String()))
				}
				return nil
			}
		}
		if len(stmt.Path) < 2 {
			return fail("E_SET_PATH", stmt.Pos, "set statements must target state.<field> or a mutable local binding")
		}
		if len(stmt.Path) >= 3 {
			// Nested (3+ segment) struct field WRITES are not supported: the
			// lowering below (and this validation) only ever inspects
			// stmt.Path[1], so a target like `st.outer.b` would silently type-
			// check the RHS against `outer`'s type (the container) instead of
			// `b`'s type, and lower to a whole-field overwrite of `outer` that
			// discards the `.b` suffix entirely -- a real, silent state-
			// corruption bug, not merely an unimplemented feature (found via
			// this session's own adversarial review: `set st.outer.b =
			// st.spare` compiles and executes with ResultOK, clobbering ALL of
			// `outer` with `spare`'s value whenever their container types
			// happen to match). Reject explicitly here instead. Read chains of
			// arbitrary depth ARE supported (see the ExprPath len>=3 branch in
			// lowerExprToIR) -- only the write side has this restriction.
			return fail("E_SET_NESTED_UNSUPPORTED", stmt.Pos, fmt.Sprintf(
				"cannot assign to %q: nested (3+ segment) struct field writes are not supported in AVM v1 -- construct a new struct literal (copying any fields you want unchanged) and assign it to %q as a whole instead",
				joinPath(stmt.Path), joinPath(stmt.Path[:len(stmt.Path)-1])))
		}
		var targetStruct *StructDecl
		if stmt.Path[0] == "state" {
			targetStruct = storage
		} else if rootType, ok := env[stmt.Path[0]]; ok {
			targetStruct = structs[rootType.Name]
		}
		if targetStruct == nil {
			// Resolve the write target through the binding environment: a
			// self/receiver binding in a storage-type method, or a mutable
			// local struct. NOTE: a bare `state.<field>` write in a genuinely
			// storage-less contract is not hard-errored here because
			// storage-type methods (`func Storage.method(self)`) desugar `self`
			// to `state` while their storage struct is threaded separately, so
			// the two cases are indistinguishable at this point; the lenient
			// fall-through preserves valid storage methods.
			if rootType, ok := env[stmt.Path[0]]; ok {
				resolved := resolveSingleMemberTypeRef(rootType, types)
				if st, isStruct := structs[resolved.Name]; isStruct {
					targetStruct = st
				} else {
					return fail("E_SET_TARGET", stmt.Pos, fmt.Sprintf("cannot assign to %q: %s is not an indexable struct", joinPath(stmt.Path), rootType.String()))
				}
			} else {
				_, err := c.inferExprType(stmt.Value, env, storage, structs, enums, types, functions, consts, inPure)
				return err
			}
		}
		fieldType, ok := lookupStructField(targetStruct, stmt.Path[1])
		if !ok {
			return fail("E_SET_FIELD", stmt.Pos, fmt.Sprintf("field %q not found on %s", stmt.Path[1], targetStruct.Name))
		}
		typ, err := c.inferExprType(stmt.Value, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return err
		}
		if !compatibleTypesResolved(typ, fieldType, types) {
			return fail("E_SET_TYPE", stmt.Pos, fmt.Sprintf("cannot assign %s to %s", typ.String(), fieldType.String()))
		}
		return nil
	case StatementEmit:
		if inPure {
			return fail("E_PURE_EFFECT", stmt.Pos, "pure functions cannot emit events or perform chain-visible side effects")
		}
		return nil
	case StatementAssert:
		_, err := c.inferExprType(stmt.Value, env, storage, structs, enums, types, functions, consts, inPure)
		return err
	case StatementThrow:
		_, err := c.inferExprType(stmt.Value, env, storage, structs, enums, types, functions, consts, inPure)
		return err
	case StatementBreak, StatementContinue:
		if loopDepth == 0 {
			return fail("E_LOOP_CTRL", stmt.Pos, fmt.Sprintf("%s is only allowed inside a loop", stmt.Kind))
		}
		return nil
	case StatementExpr:
		_, err := c.inferExprType(stmt.Value, env, storage, structs, enums, types, functions, consts, inPure)
		return err
	case StatementReturn:
		typ, err := c.inferExprType(stmt.Value, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return err
		}
		if ret != nil && !compatibleTypesResolved(typ, *ret, types) {
			return fail("E_RETURN_TYPE", stmt.Pos, fmt.Sprintf("return type %s does not match %s", typ.String(), ret.String()))
		}
		return nil
	case StatementRefund, StatementSend, StatementSelf:
		if inPure {
			return fail("E_PURE_EFFECT", stmt.Pos, "pure functions cannot send/refund/schedule self or perform chain-visible side effects")
		}
		return nil
	case StatementIf:
		typ, err := c.inferExprType(stmt.Value, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return err
		}
		if !strings.EqualFold(typ.Name, "bool") {
			return fail("E_IF_COND", stmt.Pos, "if condition must be bool")
		}
		thenEnv := cloneTypeEnv(env)
		thenMutables := cloneBoolEnv(mutables)
		thenConsts := cloneConstEnv(consts)
		thenScope := map[string]struct{}{}
		for _, inner := range stmt.Then {
			if err := c.validateStatement(inner, thenEnv, thenMutables, thenScope, thenConsts, storage, structs, enums, types, ret, functions, inPure, loopDepth); err != nil {
				return err
			}
		}
		elseEnv := cloneTypeEnv(env)
		elseMutables := cloneBoolEnv(mutables)
		elseConsts := cloneConstEnv(consts)
		elseScope := map[string]struct{}{}
		for _, inner := range stmt.Else {
			if err := c.validateStatement(inner, elseEnv, elseMutables, elseScope, elseConsts, storage, structs, enums, types, ret, functions, inPure, loopDepth); err != nil {
				return err
			}
		}
		return nil
	case StatementWhile:
		typ, err := c.inferExprType(stmt.Value, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return err
		}
		if !strings.EqualFold(typ.Name, "bool") {
			return fail("E_WHILE_COND", stmt.Pos, "while condition must be bool")
		}
		bodyEnv := cloneTypeEnv(env)
		bodyMutables := cloneBoolEnv(mutables)
		bodyConsts := cloneConstEnv(consts)
		bodyScope := map[string]struct{}{}
		for _, inner := range stmt.Then {
			if err := c.validateStatement(inner, bodyEnv, bodyMutables, bodyScope, bodyConsts, storage, structs, enums, types, ret, functions, inPure, loopDepth+1); err != nil {
				return err
			}
		}
		return nil
	case StatementDo:
		bodyEnv := cloneTypeEnv(env)
		bodyMutables := cloneBoolEnv(mutables)
		bodyConsts := cloneConstEnv(consts)
		bodyScope := map[string]struct{}{}
		for _, inner := range stmt.Then {
			if err := c.validateStatement(inner, bodyEnv, bodyMutables, bodyScope, bodyConsts, storage, structs, enums, types, ret, functions, inPure, loopDepth+1); err != nil {
				return err
			}
		}
		typ, err := c.inferExprType(stmt.Value, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return err
		}
		if !strings.EqualFold(typ.Name, "bool") {
			return fail("E_DO_WHILE_COND", stmt.Pos, "do while condition must be bool")
		}
		return nil
	case StatementRepeat:
		countTyp, err := c.inferExprType(stmt.Value, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return err
		}
		if !isNumericLikeType(countTyp, types) {
			return fail("E_REPEAT_COUNT", stmt.Pos, "repeat count must be numeric")
		}
		bodyEnv := cloneTypeEnv(env)
		bodyMutables := cloneBoolEnv(mutables)
		bodyConsts := cloneConstEnv(consts)
		bodyScope := map[string]struct{}{}
		for _, inner := range stmt.Then {
			if err := c.validateStatement(inner, bodyEnv, bodyMutables, bodyScope, bodyConsts, storage, structs, enums, types, ret, functions, inPure, loopDepth+1); err != nil {
				return err
			}
		}
		return nil
	case StatementMatch:
		scrutineeType, err := c.inferExprType(stmt.Value, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return err
		}
		if err := c.validateMatchStatement(stmt, scrutineeType, env, mutables, scope, consts, storage, structs, enums, types, ret, functions, inPure, loopDepth); err != nil {
			return err
		}
		return nil
	case StatementFor:
		startTyp, err := c.inferExprType(stmt.Start, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return err
		}
		endTyp, err := c.inferExprType(stmt.End, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return err
		}
		if !isNumericType(startTyp) || !isNumericType(endTyp) {
			return fail("E_FOR_BOUNDS", stmt.Pos, "for bounds must be numeric")
		}
		if _, exists := scope[stmt.Index]; exists {
			return fail("E_DUP_BINDING", stmt.Pos, fmt.Sprintf("duplicate binding %q in the same scope", stmt.Index))
		}
		bodyEnv := cloneTypeEnv(env)
		bodyMutables := cloneBoolEnv(mutables)
		bodyConsts := cloneConstEnv(consts)
		bodyScope := map[string]struct{}{stmt.Index: struct{}{}}
		bodyEnv[stmt.Index] = TypeRef{Name: "uint64"}
		bodyMutables[stmt.Index] = false
		for _, inner := range stmt.Then {
			if err := c.validateStatement(inner, bodyEnv, bodyMutables, bodyScope, bodyConsts, storage, structs, enums, types, ret, functions, inPure, loopDepth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return fail("E_STMT", stmt.Pos, fmt.Sprintf("unsupported statement kind %q", stmt.Kind))
	}
}

func (c *Compiler) validateMatchStatement(stmt Statement, scrutineeType TypeRef, env map[string]TypeRef, mutables map[string]bool, scope map[string]struct{}, consts map[string]constValue, storage *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl, ret *TypeRef, functions map[string]*FunctionDecl, inPure bool, loopDepth int) error {
	if td, ok := types[scrutineeType.Name]; ok {
		members, err := c.expandTypeDeclMembers(td.Name, types, map[string]bool{})
		if err != nil {
			return err
		}
		if len(members) == 0 {
			return fail("E_MATCH_TYPE", stmt.Pos, fmt.Sprintf("type %s has no matchable members", td.Name))
		}
		covered := map[string]struct{}{}
		exhaustive := false
		for _, arm := range stmt.Arms {
			switch arm.Pattern.Kind {
			case PatternWildcard:
				exhaustive = true
			case PatternName:
				patternName := patternTail(arm.Pattern.Name)
				found := false
				var matched TypeRef
				for _, candidate := range members {
					if candidate.Name == patternName {
						matched = candidate
						found = true
						break
					}
				}
				if !found {
					return fail("E_MATCH_VARIANT", arm.Pos, fmt.Sprintf("type %s has no member %q", td.Name, arm.Pattern.Name))
				}
				if _, ok := covered[patternName]; ok {
					return fail("E_MATCH_DUP", arm.Pos, fmt.Sprintf("duplicate match arm for %s.%s", td.Name, patternName))
				}
				covered[patternName] = struct{}{}
				armEnv := cloneTypeEnv(env)
				armMutables := cloneBoolEnv(mutables)
				armConsts := cloneConstEnv(consts)
				armScope := map[string]struct{}{}
				if st, ok := structs[matched.Name]; ok {
					if len(arm.Pattern.Bindings) > 0 && len(arm.Pattern.Bindings) != len(st.Fields) {
						return fail("E_MATCH_BINDINGS", arm.Pos, fmt.Sprintf("match arm %s expects %d bindings, got %d", arm.Pattern.Name, len(st.Fields), len(arm.Pattern.Bindings)))
					}
					for i, bind := range arm.Pattern.Bindings {
						if _, exists := armScope[bind]; exists {
							return fail("E_MATCH_BINDINGS", arm.Pos, fmt.Sprintf("duplicate match binding %q", bind))
						}
						armEnv[bind] = st.Fields[i].Type
						armMutables[bind] = false
						armScope[bind] = struct{}{}
					}
				} else if len(arm.Pattern.Bindings) > 0 {
					return fail("E_MATCH_BINDINGS", arm.Pos, fmt.Sprintf("match arm %s cannot destructure non-struct member %s", arm.Pattern.Name, matched.String()))
				}
				for _, inner := range arm.Body {
					if err := c.validateStatement(inner, armEnv, armMutables, armScope, armConsts, storage, structs, enums, types, ret, functions, inPure, loopDepth); err != nil {
						return err
					}
				}
			default:
				return fail("E_MATCH_PATTERN", arm.Pos, "unsupported pattern kind")
			}
		}
		if !exhaustive {
			for _, member := range members {
				if _, ok := covered[member.Name]; !ok {
					return fail("E_MATCH_EXHAUSTIVE", stmt.Pos, fmt.Sprintf("match on %s is missing variant %s", td.Name, member.Name))
				}
			}
		}
		return nil
	}
	if en, ok := enums[scrutineeType.Name]; ok {
		exhaustive := false
		covered := map[string]struct{}{}
		for _, arm := range stmt.Arms {
			switch arm.Pattern.Kind {
			case PatternWildcard:
				exhaustive = true
			case PatternName:
				patternName := patternTail(arm.Pattern.Name)
				found := false
				var variant *VariantDecl
				for i := range en.Variants {
					if en.Variants[i].Name == patternName {
						variant = &en.Variants[i]
						found = true
						break
					}
				}
				if !found {
					return fail("E_MATCH_VARIANT", arm.Pos, fmt.Sprintf("enum %s has no variant %q", en.Name, arm.Pattern.Name))
				}
				if _, ok := covered[patternName]; ok {
					return fail("E_MATCH_DUP", arm.Pos, fmt.Sprintf("duplicate match arm for %s.%s", en.Name, patternName))
				}
				covered[patternName] = struct{}{}
				armEnv := cloneTypeEnv(env)
				armMutables := cloneBoolEnv(mutables)
				armConsts := cloneConstEnv(consts)
				armScope := map[string]struct{}{}
				if variant != nil {
					if len(arm.Pattern.Bindings) > 0 && len(arm.Pattern.Bindings) != len(variant.Fields) {
						return fail("E_MATCH_BINDINGS", arm.Pos, fmt.Sprintf("match arm %s expects %d bindings, got %d", arm.Pattern.Name, len(variant.Fields), len(arm.Pattern.Bindings)))
					}
					for i, bind := range arm.Pattern.Bindings {
						if _, exists := armScope[bind]; exists {
							return fail("E_MATCH_BINDINGS", arm.Pos, fmt.Sprintf("duplicate match binding %q", bind))
						}
						armEnv[bind] = variant.Fields[i].Type
						armMutables[bind] = false
						armScope[bind] = struct{}{}
					}
				} else if len(arm.Pattern.Bindings) > 0 {
					return fail("E_MATCH_BINDINGS", arm.Pos, fmt.Sprintf("match arm %s cannot destructure variant without fields", arm.Pattern.Name))
				}
				for _, inner := range arm.Body {
					if err := c.validateStatement(inner, armEnv, armMutables, armScope, armConsts, storage, structs, enums, types, ret, functions, inPure, loopDepth); err != nil {
						return err
					}
				}
			default:
				return fail("E_MATCH_PATTERN", arm.Pos, "unsupported pattern kind")
			}
		}
		if !exhaustive {
			for _, variant := range en.Variants {
				if _, ok := covered[variant.Name]; !ok {
					return fail("E_MATCH_EXHAUSTIVE", stmt.Pos, fmt.Sprintf("match on %s is missing variant %s", en.Name, variant.Name))
				}
			}
		}
		return nil
	}
	if st, ok := structs[scrutineeType.Name]; ok {
		exhaustive := false
		for _, arm := range stmt.Arms {
			switch arm.Pattern.Kind {
			case PatternWildcard:
				exhaustive = true
			case PatternName:
				if patternTail(arm.Pattern.Name) != st.Name {
					return fail("E_MATCH_STRUCT", arm.Pos, fmt.Sprintf("struct match must use %s pattern or _", st.Name))
				}
				armEnv := cloneTypeEnv(env)
				armMutables := cloneBoolEnv(mutables)
				armConsts := cloneConstEnv(consts)
				armScope := map[string]struct{}{}
				if len(arm.Pattern.Bindings) > 0 && len(arm.Pattern.Bindings) != len(st.Fields) {
					return fail("E_MATCH_BINDINGS", arm.Pos, fmt.Sprintf("struct %s expects %d bindings, got %d", st.Name, len(st.Fields), len(arm.Pattern.Bindings)))
				}
				for i, bind := range arm.Pattern.Bindings {
					if _, exists := armScope[bind]; exists {
						return fail("E_MATCH_BINDINGS", arm.Pos, fmt.Sprintf("duplicate match binding %q", bind))
					}
					armEnv[bind] = st.Fields[i].Type
					armMutables[bind] = false
					armScope[bind] = struct{}{}
				}
				for _, inner := range arm.Body {
					if err := c.validateStatement(inner, armEnv, armMutables, armScope, armConsts, storage, structs, enums, types, ret, functions, inPure, loopDepth); err != nil {
						return err
					}
				}
			default:
				return fail("E_MATCH_PATTERN", arm.Pos, "unsupported pattern kind")
			}
		}
		if !exhaustive {
			return fail("E_MATCH_EXHAUSTIVE", stmt.Pos, fmt.Sprintf("struct match on %s requires a wildcard arm", st.Name))
		}
		return nil
	}
	return fail("E_MATCH_TYPE", stmt.Pos, fmt.Sprintf("match scrutinee %s is not an enum, union, or struct", scrutineeType.String()))
}

func (c *Compiler) expandTypeDeclMembers(name string, types map[string]*TypeDecl, stack map[string]bool) ([]TypeRef, error) {
	td, ok := types[name]
	if !ok {
		return nil, nil
	}
	if stack[name] {
		return nil, fail("E_TYPE_CYCLE", td.Pos, fmt.Sprintf("type %q forms a cycle", name))
	}
	stack[name] = true
	defer delete(stack, name)
	var out []TypeRef
	seen := map[string]struct{}{}
	for _, member := range td.Members {
		if nested, ok := types[member.Name]; ok && !member.Optional && len(member.Args) == 0 {
			nestedMembers, err := c.expandTypeDeclMembers(nested.Name, types, stack)
			if err != nil {
				return nil, err
			}
			for _, nestedMember := range nestedMembers {
				key := nestedMember.String()
				if _, exists := seen[key]; exists {
					return nil, fail("E_DUP_TYPE_VARIANT", nestedMember.Pos, fmt.Sprintf("type %q has duplicate member %q", name, key))
				}
				seen[key] = struct{}{}
				out = append(out, nestedMember)
			}
			continue
		}
		key := member.String()
		if _, exists := seen[key]; exists {
			return nil, fail("E_DUP_TYPE_VARIANT", member.Pos, fmt.Sprintf("type %q has duplicate member %q", name, key))
		}
		seen[key] = struct{}{}
		out = append(out, member)
	}
	return out, nil
}

func cloneTypeEnv(env map[string]TypeRef) map[string]TypeRef {
	out := make(map[string]TypeRef, len(env))
	for k, v := range env {
		out[k] = v
	}
	return out
}

func cloneBoolEnv(env map[string]bool) map[string]bool {
	out := make(map[string]bool, len(env))
	for k, v := range env {
		out[k] = v
	}
	return out
}

func statementContainsLoopControl(stmts []Statement) bool {
	for _, stmt := range stmts {
		if statementContainsLoopControlStmt(stmt) {
			return true
		}
	}
	return false
}

func statementContainsLoopControlStmt(stmt Statement) bool {
	switch stmt.Kind {
	case StatementBreak, StatementContinue:
		return true
	case StatementIf:
		if statementContainsLoopControl(stmt.Then) {
			return true
		}
		return statementContainsLoopControl(stmt.Else)
	case StatementWhile, StatementDo, StatementRepeat, StatementFor:
		if statementContainsLoopControl(stmt.Then) {
			return true
		}
	case StatementMatch:
		for _, arm := range stmt.Arms {
			if statementContainsLoopControl(arm.Body) {
				return true
			}
		}
	}
	return false
}

func patternTail(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 && i+1 < len(name) {
		return name[i+1:]
	}
	return name
}

func validateFunctionRecursion(funcs []*FunctionDecl) error {
	funcNames := make(map[string]bool, len(funcs))
	for _, fn := range funcs {
		if fn != nil {
			funcNames[fn.Name] = true
		}
	}
	callGraph := map[string][]string{}
	for _, fn := range funcs {
		callGraph[fn.Name] = append(callGraph[fn.Name], collectFunctionCallsFromStatements(fn.Body, funcNames)...)
	}
	seen := map[string]bool{}
	stack := map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if stack[name] {
			return fail("E_RECURSION", Position{}, fmt.Sprintf("recursive function call cycle detected at %q", name))
		}
		if seen[name] {
			return nil
		}
		seen[name] = true
		stack[name] = true
		for _, callee := range callGraph[name] {
			if err := visit(callee); err != nil {
				return err
			}
		}
		delete(stack, name)
		return nil
	}
	for name := range callGraph {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}

func collectFunctionCallsFromStatements(stmts []Statement, funcNames map[string]bool) []string {
	var out []string
	for _, stmt := range stmts {
		out = append(out, collectFunctionCallsFromStatement(stmt, funcNames)...)
	}
	return out
}

func collectFunctionCallsFromStatement(stmt Statement, funcNames map[string]bool) []string {
	var out []string
	out = append(out, collectFunctionCallsFromExpr(stmt.Value, funcNames)...)
	out = append(out, collectFunctionCallsFromExpr(stmt.Start, funcNames)...)
	out = append(out, collectFunctionCallsFromExpr(stmt.End, funcNames)...)
	for _, arg := range stmt.Args {
		out = append(out, collectFunctionCallsFromExpr(arg, funcNames)...)
	}
	for _, ex := range stmt.Extra {
		out = append(out, collectFunctionCallsFromExpr(ex, funcNames)...)
	}
	switch stmt.Kind {
	case StatementIf, StatementWhile, StatementDo, StatementRepeat, StatementFor:
		for _, inner := range stmt.Then {
			out = append(out, collectFunctionCallsFromStatement(inner, funcNames)...)
		}
	}
	if stmt.Kind == StatementIf {
		for _, inner := range stmt.Else {
			out = append(out, collectFunctionCallsFromStatement(inner, funcNames)...)
		}
	}
	if stmt.Kind == StatementMatch {
		for _, arm := range stmt.Arms {
			for _, inner := range arm.Body {
				out = append(out, collectFunctionCallsFromStatement(inner, funcNames)...)
			}
		}
	}
	return out
}

// builtinCallNames are call targets handled as intrinsics rather than user
// functions. They are excluded from the recursion/purity call graph ONLY when
// no user function shadows the name; otherwise a self-recursive user function
// named e.g. `hash` would escape recursion detection.
var builtinCallNames = map[string]struct{}{
	"hash": {}, "len": {}, "ok": {}, "err": {},
}

func collectFunctionCallsFromExpr(expr Expr, funcNames map[string]bool) []string {
	var out []string
	switch expr.Kind {
	case ExprCall:
		if len(expr.Path) == 1 {
			name := expr.Text
			_, isBuiltin := builtinCallNames[name]
			if funcNames[name] || !isBuiltin {
				out = append(out, name)
			}
		}
		for _, arg := range expr.Args {
			out = append(out, collectFunctionCallsFromExpr(arg, funcNames)...)
		}
	case ExprBinary, ExprCompare, ExprLogic:
		if expr.Left != nil {
			out = append(out, collectFunctionCallsFromExpr(*expr.Left, funcNames)...)
		}
		if expr.Right != nil {
			out = append(out, collectFunctionCallsFromExpr(*expr.Right, funcNames)...)
		}
	case ExprUnary:
		if expr.Left != nil {
			out = append(out, collectFunctionCallsFromExpr(*expr.Left, funcNames)...)
		}
	case ExprTernary:
		if expr.Left != nil {
			out = append(out, collectFunctionCallsFromExpr(*expr.Left, funcNames)...)
		}
		if expr.Right != nil {
			out = append(out, collectFunctionCallsFromExpr(*expr.Right, funcNames)...)
		}
		if expr.Else != nil {
			out = append(out, collectFunctionCallsFromExpr(*expr.Else, funcNames)...)
		}
	case ExprTry:
		if expr.Left != nil {
			out = append(out, collectFunctionCallsFromExpr(*expr.Left, funcNames)...)
		}
		if expr.Else != nil {
			out = append(out, collectFunctionCallsFromExpr(*expr.Else, funcNames)...)
		}
	case ExprStruct:
		for _, field := range expr.Fields {
			out = append(out, collectFunctionCallsFromExpr(field.Value, funcNames)...)
		}
	}
	return out
}

func (c *Compiler) buildEnv(params []ParamDecl, storage *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl) map[string]TypeRef {
	env := map[string]TypeRef{}
	for _, param := range params {
		env[param.Name] = param.Type
	}
	if storage != nil {
		env["state"] = TypeRef{Name: storage.Name}
	}
	env["contract"] = TypeRef{Name: "ContractContext"}
	return env
}

func (c *Compiler) buildMutableEnv(params []ParamDecl, hasStorage bool) map[string]bool {
	mutables := map[string]bool{}
	for _, param := range params {
		mutables[param.Name] = false
	}
	if hasStorage {
		mutables["state"] = false
	}
	mutables["contract"] = false
	return mutables
}

func (c *Compiler) initialScope(params []ParamDecl, hasStorage bool) map[string]struct{} {
	scope := map[string]struct{}{}
	for _, param := range params {
		scope[param.Name] = struct{}{}
	}
	if hasStorage {
		scope["state"] = struct{}{}
	}
	scope["contract"] = struct{}{}
	return scope
}

func (c *Compiler) inferExprType(expr Expr, env map[string]TypeRef, storage *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl, functions map[string]*FunctionDecl, consts map[string]constValue, inPure bool) (TypeRef, error) {
	switch expr.Kind {
	case ExprNumber:
		return TypeRef{Name: "uint64"}, nil
	case ExprString:
		return TypeRef{Name: "string"}, nil
	case ExprBytes:
		return TypeRef{Name: "bytes"}, nil
	case ExprBool:
		return TypeRef{Name: "bool"}, nil
	case ExprNull:
		return TypeRef{Name: "null"}, nil
	case ExprIdent:
		if env != nil {
			if typ, ok := env[expr.Text]; ok {
				return typ, nil
			}
		}
		if consts != nil {
			if value, ok := consts[expr.Text]; ok {
				return constValueType(value), nil
			}
		}
		if _, ok := builtinSendModeValue(expr.Text); ok {
			return TypeRef{Name: "uint32"}, nil
		}
		if strings.HasPrefix(strings.ToUpper(expr.Text), "ERR_") {
			return TypeRef{Name: "uint64"}, nil
		}
		if fn, ok := functions[expr.Text]; ok && fn != nil {
			return fn.ReturnType, nil
		}
		return TypeRef{}, fail("E_UNKNOWN_IDENT", expr.Pos, fmt.Sprintf("unknown identifier %q", expr.Text))
	case ExprPath:
		if len(expr.Path) == 2 {
			if en, ok := enums[expr.Path[0]]; ok {
				for _, variant := range en.Variants {
					if variant.Name == expr.Path[1] {
						return TypeRef{Name: en.Name}, nil
					}
				}
			}
		}
		return c.resolvePathType(expr.Path, env, storage, structs, enums, types, expr.Pos)
	case ExprStruct:
		if expr.Text != "" {
			if isMapFamilyType(expr.Text) && len(expr.Fields) > 0 {
				return TypeRef{}, fail("E_STRUCT", expr.Pos, "map literals do not support inline fields; use set() to populate a map")
			}
			return TypeRef{Name: expr.Text}, nil
		}
		if len(expr.Path) > 0 {
			return TypeRef{Name: joinPath(expr.Path)}, nil
		}
		return TypeRef{Name: "struct"}, nil
	case ExprUnary:
		if expr.Left == nil {
			return TypeRef{}, fail("E_UNARY", expr.Pos, "unary expression is missing operand")
		}
		operand, err := c.inferExprType(*expr.Left, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return TypeRef{}, err
		}
		switch expr.Op {
		case "!":
			if !strings.EqualFold(operand.Name, "bool") {
				return TypeRef{}, fail("E_UNARY_TYPE", expr.Pos, "logical not requires bool operand")
			}
			return TypeRef{Name: "bool"}, nil
		case "-", "~":
			if !isNumericLikeType(operand, types) {
				return TypeRef{}, fail("E_UNARY_TYPE", expr.Pos, fmt.Sprintf("unary %q requires numeric operand, got %s", expr.Op, operand.String()))
			}
			return operand, nil
		default:
			return TypeRef{}, fail("E_UNARY_OP", expr.Pos, fmt.Sprintf("unary %q is not supported", expr.Op))
		}
	case ExprCall:
		if fn, ok := functions[joinPath(expr.Path)]; ok {
			if inPure && !fn.Pure {
				return TypeRef{}, fail("E_PURE_CALL", expr.Pos, fmt.Sprintf("pure functions cannot call impure function %q", fn.Name))
			}
			if err := c.validateCallArgs(expr, fn, env, storage, structs, enums, types, functions, consts, inPure); err != nil {
				return TypeRef{}, err
			}
			return fn.ReturnType, nil
		}
		if ret, ok, err := c.inferBuiltinMethodCallType(expr, env, storage, structs, enums, types, functions, consts, inPure); err != nil {
			return TypeRef{}, err
		} else if ok {
			return ret, nil
		}
		if st, ok := structs[expr.Text]; ok && len(expr.Args) >= 0 {
			return TypeRef{Name: st.Name}, nil
		}
		if _, ok := types[expr.Text]; ok && len(expr.Args) >= 0 {
			return TypeRef{Name: expr.Text}, nil
		}
		switch strings.ToLower(expr.Text) {
		case "buildmessage":
			if len(expr.Args) != 1 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "buildMessage() requires one struct argument")
			}
			if expr.Args[0].Kind != ExprStruct {
				return TypeRef{}, fail("E_CALL_TYPE", expr.Args[0].Pos, "buildMessage() requires a struct literal argument")
			}
			if err := validateBuildMessageFields(expr.Args[0]); err != nil {
				return TypeRef{}, err
			}
			return TypeRef{Name: "MessageEnvelope"}, nil
		case "wrapmessage":
			if len(expr.Args) != 1 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "wrapMessage() requires one struct literal argument")
			}
			if expr.Args[0].Kind != ExprStruct || expr.Args[0].Text == "" {
				return TypeRef{}, fail("E_CALL_TYPE", expr.Args[0].Pos, "wrapMessage() requires a struct literal argument")
			}
			st, ok := structs[expr.Args[0].Text]
			if !ok || !structHasAnnotation(st, "@message") {
				return TypeRef{}, fail("E_WRAP_MESSAGE", expr.Pos, fmt.Sprintf("wrapMessage() requires a @message-annotated struct literal, got %q", expr.Args[0].Text))
			}
			return TypeRef{Name: st.Name}, nil
		case "getaddress":
			if len(expr.Args) != 0 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "getAddress() requires no arguments")
			}
			return TypeRef{Name: "address"}, nil
		case "address":
			if len(expr.Args) != 1 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "address() requires one argument")
			}
			if expr.Args[0].Kind != ExprString {
				return TypeRef{}, fail("E_CALL_TYPE", expr.Args[0].Pos, "address() requires a string literal argument")
			}
			return TypeRef{Name: "address"}, nil
		case "getoriginalbalance", "getbalance":
			return TypeRef{Name: "coins"}, nil
		case "getattachedvalue":
			return TypeRef{Name: "coins"}, nil
		case "counterfactualaddress", "autodeployaddress":
			if len(expr.Args) != 1 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, expr.Text+"() requires one struct argument")
			}
			if expr.Args[0].Kind != ExprStruct {
				return TypeRef{}, fail("E_CALL_TYPE", expr.Args[0].Pos, expr.Text+"() requires a struct literal argument")
			}
			if err := validateAddressBuilderFields(expr.Args[0], expr.Text); err != nil {
				return TypeRef{}, err
			}
			return TypeRef{Name: "address"}, nil
		case "hash":
			if len(expr.Args) != 1 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "hash() requires one argument")
			}
			return TypeRef{Name: "hash32"}, nil
		case "sha256", "keccak256", "blake2b":
			// Byte-exact 32-byte hashes over raw bytes (distinct from hash()).
			if len(expr.Args) != 1 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, expr.Text+"() requires one argument")
			}
			if _, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, types, functions, consts, inPure); err != nil {
				return TypeRef{}, err
			}
			return TypeRef{Name: "hash32"}, nil
		case "ripemd160", "sha512":
			// Byte-exact hashes whose digest is not 32 bytes (20 / 64), returned
			// as bytes since there is no 20-/64-byte hash tag.
			if len(expr.Args) != 1 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, expr.Text+"() requires one argument")
			}
			if _, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, types, functions, consts, inPure); err != nil {
				return TypeRef{}, err
			}
			return TypeRef{Name: "bytes"}, nil
		case "concat":
			if len(expr.Args) != 2 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "concat() requires two arguments")
			}
			for _, arg := range expr.Args {
				if _, err := c.inferExprType(arg, env, storage, structs, enums, types, functions, consts, inPure); err != nil {
					return TypeRef{}, err
				}
			}
			return TypeRef{Name: "bytes"}, nil
		case "subbytes":
			if len(expr.Args) != 3 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "subBytes() requires three arguments")
			}
			for _, arg := range expr.Args {
				if _, err := c.inferExprType(arg, env, storage, structs, enums, types, functions, consts, inPure); err != nil {
					return TypeRef{}, err
				}
			}
			return TypeRef{Name: "bytes"}, nil
		case "byteat":
			if len(expr.Args) != 2 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "byteAt() requires two arguments")
			}
			for _, arg := range expr.Args {
				if _, err := c.inferExprType(arg, env, storage, structs, enums, types, functions, consts, inPure); err != nil {
					return TypeRef{}, err
				}
			}
			return TypeRef{Name: "uint8"}, nil
		case "tobytesbe":
			if len(expr.Args) != 2 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "toBytesBE() requires two arguments")
			}
			for _, arg := range expr.Args {
				if _, err := c.inferExprType(arg, env, storage, structs, enums, types, functions, consts, inPure); err != nil {
					return TypeRef{}, err
				}
			}
			return TypeRef{Name: "bytes"}, nil
		case "frombytesbe":
			if len(expr.Args) != 1 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "fromBytesBE() requires one argument")
			}
			if _, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, types, functions, consts, inPure); err != nil {
				return TypeRef{}, err
			}
			return TypeRef{Name: "uint256"}, nil
		case "issignaturevalid", "verifysignature":
			if len(expr.Args) != 3 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, expr.Text+"() requires three arguments")
			}
			return TypeRef{Name: "bool"}, nil
		case "muldiv", "muldivroundup", "muldivnearest", "muldivfloor", "muldivceil":
			// Full-width a*b/c on uint256; the a*b product cannot overflow (it is
			// computed in an unbounded intermediate), only the uint256 result.
			// mulDivNearest rounds half-up (floor(a*b/c), rounded up iff the
			// remainder doubled is >= c). mulDivFloor/mulDivCeil are alternate
			// spellings of mulDiv/mulDivRoundUp -- same opcode, same behavior.
			if len(expr.Args) != 3 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, expr.Text+"() requires three arguments")
			}
			for _, arg := range expr.Args {
				if _, err := c.inferExprType(arg, env, storage, structs, enums, types, functions, consts, inPure); err != nil {
					return TypeRef{}, err
				}
			}
			return TypeRef{Name: "uint256"}, nil
		case "isqrt":
			// isqrt(x: uint256) -> uint256: floor(sqrt(x)).
			if len(expr.Args) != 1 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "isqrt() requires one argument")
			}
			if _, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, types, functions, consts, inPure); err != nil {
				return TypeRef{}, err
			}
			return TypeRef{Name: "uint256"}, nil
		case "mulcmp":
			// mulCmp(a,b,c,d) -> int256: sign(a*b - c*d) as -1/0/+1. Both products
			// are formed at unbounded width, so it never traps on a >uint256
			// product — the full-range replacement for the ratioGtBounded path.
			if len(expr.Args) != 4 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "mulCmp() requires four arguments")
			}
			for _, arg := range expr.Args {
				if _, err := c.inferExprType(arg, env, storage, structs, enums, types, functions, consts, inPure); err != nil {
					return TypeRef{}, err
				}
			}
			return TypeRef{Name: "int256"}, nil
		case "muldivsigned":
			// mulDivSigned(a,b,c) -> int256: (a*b)/c truncated toward zero over
			// signed operands; the a*b product is formed at unbounded width.
			if len(expr.Args) != 3 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "mulDivSigned() requires three arguments")
			}
			for _, arg := range expr.Args {
				if _, err := c.inferExprType(arg, env, storage, structs, enums, types, functions, consts, inPure); err != nil {
					return TypeRef{}, err
				}
			}
			return TypeRef{Name: "int256"}, nil
		case "touint128":
			// toUint128(x) -> uint128: checked narrowing cast, TRAPS if x does
			// not fit uint128 (or is negative). Needed to store a
			// mulDiv/mulDivRoundUp/mulDivNearest/mulCmp result (always
			// uint256) into a genuinely uint128-backed field.
			if len(expr.Args) != 1 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "toUint128() requires one argument")
			}
			if _, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, types, functions, consts, inPure); err != nil {
				return TypeRef{}, err
			}
			return TypeRef{Name: "uint128"}, nil
		case "toint128":
			// toInt128(x) -> int128: checked narrowing cast, TRAPS if x does not
			// fit int128. Needed to store a mulDivSigned result (always int256)
			// into a genuinely int128-backed field.
			if len(expr.Args) != 1 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "toInt128() requires one argument")
			}
			if _, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, types, functions, consts, inPure); err != nil {
				return TypeRef{}, err
			}
			return TypeRef{Name: "int128"}, nil
		case "toint256":
			// toInt256(x) -> int256: checked re-tagging cast, TRAPS if x does
			// not fit int256 (>= 2^255). Needed to store an unsigned
			// (Ratio256/BasisPoints-derived) magnitude, e.g. a mulDiv/
			// mulDivRoundUp/mulDivNearest result, into a genuinely
			// int256-backed signed field.
			if len(expr.Args) != 1 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "toInt256() requires one argument")
			}
			if _, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, types, functions, consts, inPure); err != nil {
				return TypeRef{}, err
			}
			return TypeRef{Name: "int256"}, nil
		case "verifysecp256k1":
			// verifySecp256k1(msgHash: hash, sig: bytes, pubkey: bytes) -> bool.
			if len(expr.Args) != 3 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "verifySecp256k1() requires three arguments")
			}
			for _, arg := range expr.Args {
				if _, err := c.inferExprType(arg, env, storage, structs, enums, types, functions, consts, inPure); err != nil {
					return TypeRef{}, err
				}
			}
			return TypeRef{Name: "bool"}, nil
		case "ecrecover":
			// ecrecover(msgHash: hash, sig: bytes) -> bytes (64-byte X‖Y pubkey).
			if len(expr.Args) != 2 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "ecrecover() requires two arguments")
			}
			for _, arg := range expr.Args {
				if _, err := c.inferExprType(arg, env, storage, structs, enums, types, functions, consts, inPure); err != nil {
					return TypeRef{}, err
				}
			}
			return TypeRef{Name: "bytes"}, nil
		case "bn254g1add":
			// bn254G1Add(a: bytes, b: bytes) -> bytes (64-byte G1 point).
			if len(expr.Args) != 2 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "bn254G1Add() requires two arguments")
			}
			for _, arg := range expr.Args {
				if _, err := c.inferExprType(arg, env, storage, structs, enums, types, functions, consts, inPure); err != nil {
					return TypeRef{}, err
				}
			}
			return TypeRef{Name: "bytes"}, nil
		case "bn254g1scalarmul":
			// bn254G1ScalarMul(point: bytes, scalar: uint256) -> bytes (64-byte G1 point).
			if len(expr.Args) != 2 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "bn254G1ScalarMul() requires two arguments")
			}
			for _, arg := range expr.Args {
				if _, err := c.inferExprType(arg, env, storage, structs, enums, types, functions, consts, inPure); err != nil {
					return TypeRef{}, err
				}
			}
			return TypeRef{Name: "bytes"}, nil
		case "bn254g1isoncurve":
			// bn254G1IsOnCurve(point: bytes) -> bool.
			if len(expr.Args) != 1 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "bn254G1IsOnCurve() requires one argument")
			}
			if _, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, types, functions, consts, inPure); err != nil {
				return TypeRef{}, err
			}
			return TypeRef{Name: "bool"}, nil
		case "bn254g2add":
			// bn254G2Add(a: bytes, b: bytes) -> bytes (128-byte G2 point).
			if len(expr.Args) != 2 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "bn254G2Add() requires two arguments")
			}
			for _, arg := range expr.Args {
				if _, err := c.inferExprType(arg, env, storage, structs, enums, types, functions, consts, inPure); err != nil {
					return TypeRef{}, err
				}
			}
			return TypeRef{Name: "bytes"}, nil
		case "bn254g2scalarmul":
			// bn254G2ScalarMul(point: bytes, scalar: uint256) -> bytes (128-byte G2 point).
			if len(expr.Args) != 2 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "bn254G2ScalarMul() requires two arguments")
			}
			for _, arg := range expr.Args {
				if _, err := c.inferExprType(arg, env, storage, structs, enums, types, functions, consts, inPure); err != nil {
					return TypeRef{}, err
				}
			}
			return TypeRef{Name: "bytes"}, nil
		case "bn254pairingcheck":
			// bn254PairingCheck(g1s: bytes, g2s: bytes, k: uint256) -> bool.
			if len(expr.Args) != 3 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "bn254PairingCheck() requires three arguments")
			}
			for _, arg := range expr.Args {
				if _, err := c.inferExprType(arg, env, storage, structs, enums, types, functions, consts, inPure); err != nil {
					return TypeRef{}, err
				}
			}
			return TypeRef{Name: "bool"}, nil
		case "poseidon2bn254":
			// poseidon2Bn254(data: bytes, n: uint256) -> hash32 (32-byte digest).
			if len(expr.Args) != 2 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "poseidon2Bn254() requires two arguments")
			}
			for _, arg := range expr.Args {
				if _, err := c.inferExprType(arg, env, storage, structs, enums, types, functions, consts, inPure); err != nil {
					return TypeRef{}, err
				}
			}
			return TypeRef{Name: "hash32"}, nil
		case "len":
			return TypeRef{Name: "uint64"}, nil
		case "aet":
			return TypeRef{Name: "Coins"}, nil
		case "now":
			return TypeRef{Name: "int64"}, nil
		case "logicaltime", "currentblocklogicaltime":
			return TypeRef{Name: "uint64"}, nil
		case "random":
			return TypeRef{Name: "uint256"}, nil
		case "ok":
			if len(expr.Args) != 1 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "ok() requires one argument")
			}
			argType, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, types, functions, consts, inPure)
			if err != nil {
				return TypeRef{}, err
			}
			return TypeRef{Name: "Option", Args: []TypeRef{argType}}, nil
		case "err":
			if len(expr.Args) != 1 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "err() requires one argument")
			}
			argType, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, types, functions, consts, inPure)
			if err != nil {
				return TypeRef{}, err
			}
			return TypeRef{Name: "Result", Args: []TypeRef{{Name: "uint64"}, argType}}, nil
		default:
			if fn, ok := functions[expr.Text]; ok {
				if inPure && !fn.Pure {
					return TypeRef{}, fail("E_PURE_CALL", expr.Pos, fmt.Sprintf("pure functions cannot call impure function %q", fn.Name))
				}
				if len(expr.Args) != len(fn.Params) {
					return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, fmt.Sprintf("function %q expects %d args", fn.Name, len(fn.Params)))
				}
				for i, arg := range expr.Args {
					argType, err := c.inferExprType(arg, env, storage, structs, enums, types, functions, consts, inPure)
					if err != nil {
						return TypeRef{}, err
					}
					if !compatibleTypesResolved(argType, fn.Params[i].Type, types) {
						return TypeRef{}, fail("E_CALL_TYPE", arg.Pos, fmt.Sprintf("argument %q has type %s, want %s", fn.Params[i].Name, argType.String(), fn.Params[i].Type.String()))
					}
				}
				return fn.ReturnType, nil
			}
			return TypeRef{}, fail("E_CALL", expr.Pos, fmt.Sprintf("unknown function %q", expr.Text))
		}
	case ExprBinary:
		left, err := c.inferExprType(*expr.Left, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return TypeRef{}, err
		}
		right, err := c.inferExprType(*expr.Right, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return TypeRef{}, err
		}
		switch expr.Op {
		case "+":
			if !isNumericLikeType(left, types) || !isNumericLikeType(right, types) {
				return TypeRef{}, fail("E_BINARY_TYPE", expr.Pos, fmt.Sprintf("binary %q requires numeric operands", expr.Op))
			}
			return left, nil
		case "-":
			if !isNumericLikeType(left, types) || !isNumericLikeType(right, types) {
				return TypeRef{}, fail("E_BINARY_TYPE", expr.Pos, fmt.Sprintf("binary %q requires numeric operands", expr.Op))
			}
			return left, nil
		case "*", "/", "%", "<<", ">>", "&", "|", "^":
			if !isNumericLikeType(left, types) || !isNumericLikeType(right, types) {
				return TypeRef{}, fail("E_BINARY_TYPE", expr.Pos, fmt.Sprintf("binary %q requires numeric operands", expr.Op))
			}
			return left, nil
		case "??":
			if compatibleTypesResolved(left, right, types) {
				return left, nil
			}
			return right, nil
		case "==", "!=", "<", "<=", ">", ">=", "and", "or":
			return TypeRef{Name: "bool"}, nil
		default:
			return TypeRef{}, fail("E_BINARY_OP", expr.Pos, fmt.Sprintf("binary %q is not supported", expr.Op))
		}
	case ExprCompare:
		if expr.Op == "<=>" {
			return TypeRef{Name: "int64"}, nil
		}
		return TypeRef{Name: "bool"}, nil
	case ExprLogic:
		left, err := c.inferExprType(*expr.Left, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return TypeRef{}, err
		}
		right, err := c.inferExprType(*expr.Right, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return TypeRef{}, err
		}
		if !strings.EqualFold(left.Name, "bool") || !strings.EqualFold(right.Name, "bool") {
			return TypeRef{}, fail("E_LOGIC_TYPE", expr.Pos, "logical operators require bool operands")
		}
		return TypeRef{Name: "bool"}, nil
	case ExprTernary:
		if expr.Left == nil || expr.Right == nil || expr.Else == nil {
			return TypeRef{}, fail("E_TERNARY", expr.Pos, "ternary expression is incomplete")
		}
		condType, err := c.inferExprType(*expr.Left, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return TypeRef{}, err
		}
		if !strings.EqualFold(condType.Name, "bool") {
			return TypeRef{}, fail("E_TERNARY_COND", expr.Pos, "ternary condition must be bool")
		}
		thenType, err := c.inferExprType(*expr.Right, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return TypeRef{}, err
		}
		elseType, err := c.inferExprType(*expr.Else, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return TypeRef{}, err
		}
		if compatibleTypesResolved(thenType, elseType, types) {
			return thenType, nil
		}
		if compatibleTypesResolved(elseType, thenType, types) {
			return elseType, nil
		}
		return TypeRef{}, fail("E_TERNARY_TYPE", expr.Pos, fmt.Sprintf("ternary branches must have matching types, got %s and %s", thenType.String(), elseType.String()))
	case ExprTry:
		t, err := c.inferExprType(*expr.Left, env, storage, structs, enums, types, functions, consts, inPure)
		if err != nil {
			return TypeRef{}, err
		}
		if expr.Else != nil {
			t2, err := c.inferExprType(*expr.Else, env, storage, structs, enums, types, functions, consts, inPure)
			if err != nil {
				return TypeRef{}, err
			}
			if !compatibleTypesResolved(t, t2, types) {
				return TypeRef{}, fail("E_TRY_TYPE", expr.Pos, "try branches must have matching types")
			}
			return t, nil
		}
		return t, nil
	default:
		return TypeRef{}, fail("E_EXPR", expr.Pos, fmt.Sprintf("unsupported expression kind %q", expr.Kind))
	}
}

func validateBuildMessageFields(expr Expr) error {
	allowed := map[string]struct{}{
		"bounce":      {},
		"amount":      {},
		"receiver":    {},
		"body":        {},
		"opcode":      {},
		"queryId":     {},
		"stateInit":   {},
		"mode":        {},
		"textComment": {},
	}
	seen := map[string]struct{}{}
	for _, field := range expr.Fields {
		if _, ok := allowed[field.Name]; !ok {
			return fail("E_BUILD_MESSAGE_FIELD", field.Pos, fmt.Sprintf("unknown buildMessage field %q", field.Name))
		}
		if _, ok := seen[field.Name]; ok {
			// At most one textComment per message so an explorer has a single
			// canonical memo to display; duplicate any field is rejected.
			return fail("E_BUILD_MESSAGE_FIELD", field.Pos, fmt.Sprintf("duplicate buildMessage field %q", field.Name))
		}
		seen[field.Name] = struct{}{}
	}
	if _, ok := seen["receiver"]; !ok {
		return fail("E_BUILD_MESSAGE_FIELD", expr.Pos, "buildMessage requires receiver")
	}
	if _, ok := seen["body"]; !ok {
		return fail("E_BUILD_MESSAGE_FIELD", expr.Pos, "buildMessage requires body")
	}
	for _, field := range expr.Fields {
		if field.Name == "mode" {
			if err := validateSendModeExpr(field.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateSendModeExpr statically checks a buildMessage `mode:` value: it must
// be a combination of SEND_* constants joined by `+`, each flag appearing at
// most once, and the resulting bitmask must be a logically coherent set (e.g.
// DRAIN_BALANCE and CARRY_REMAINDER are mutually exclusive, and ESTIMATE_ONLY
// cannot combine with value-moving flags). This turns "illogical mode
// addition" into a compile error rather than surprising runtime behavior.
func validateSendModeExpr(expr Expr) error {
	flags, ok := collectSendModeFlags(expr)
	if !ok {
		return fail("E_SEND_MODE", expr.Pos, "send mode must be SEND_* constants combined with +")
	}
	seen := map[string]struct{}{}
	var mask uint32
	for _, f := range flags {
		if _, dup := seen[f.name]; dup {
			return fail("E_SEND_MODE", expr.Pos, fmt.Sprintf("send mode %s specified more than once", f.name))
		}
		seen[f.name] = struct{}{}
		mask |= f.value
	}
	const (
		drain  = 128
		carry  = 64
		est    = 1024
		anyVal = drain | carry
	)
	if mask&drain != 0 && mask&carry != 0 {
		return fail("E_SEND_MODE", expr.Pos, "SEND_DRAIN_BALANCE and SEND_CARRY_REMAINDER are mutually exclusive")
	}
	if mask&est != 0 && mask&^uint32(est) != 0 {
		return fail("E_SEND_MODE", expr.Pos, "SEND_ESTIMATE_ONLY cannot be combined with other send modes")
	}
	return nil
}

type sendModeFlag struct {
	name  string
	value uint32
}

// collectSendModeFlags flattens a `+`-chain of SEND_* identifiers (and the
// bare SEND_DEFAULT / numeric 0) into its component flags. Returns ok=false if
// any leaf is not a recognized send-mode constant.
func collectSendModeFlags(expr Expr) ([]sendModeFlag, bool) {
	switch expr.Kind {
	case ExprBinary:
		if expr.Op != "+" || expr.Left == nil || expr.Right == nil {
			return nil, false
		}
		left, ok := collectSendModeFlags(*expr.Left)
		if !ok {
			return nil, false
		}
		right, ok := collectSendModeFlags(*expr.Right)
		if !ok {
			return nil, false
		}
		return append(left, right...), true
	case ExprIdent, ExprPath:
		name := expr.Text
		if name == "" && len(expr.Path) == 1 {
			name = expr.Path[0]
		}
		v, ok := builtinSendModeValue(name)
		if !ok {
			return nil, false
		}
		if v == 0 {
			return nil, true // SEND_DEFAULT contributes no flags
		}
		return []sendModeFlag{{name: strings.ToUpper(strings.TrimSpace(name)), value: v}}, true
	case ExprNumber:
		if strings.TrimSpace(expr.Text) == "0" {
			return nil, true
		}
		return nil, false
	default:
		return nil, false
	}
}

func validateAddressBuilderFields(expr Expr, name string) error {
	allowed := map[string]struct{}{
		"code":      {},
		"data":      {},
		"salt":      {},
		"deployer":  {},
		"chainId":   {},
		"namespace": {},
		"balance":   {},
	}
	seen := map[string]struct{}{}
	for _, field := range expr.Fields {
		if _, ok := allowed[field.Name]; !ok {
			return fail("E_ADDRESS_BUILDER_FIELD", field.Pos, fmt.Sprintf("unknown %s field %q", name, field.Name))
		}
		if _, ok := seen[field.Name]; ok {
			return fail("E_ADDRESS_BUILDER_FIELD", field.Pos, fmt.Sprintf("duplicate %s field %q", name, field.Name))
		}
		seen[field.Name] = struct{}{}
	}
	if _, ok := seen["code"]; !ok {
		return fail("E_ADDRESS_BUILDER_FIELD", expr.Pos, fmt.Sprintf("%s requires code", name))
	}
	if _, ok := seen["data"]; !ok {
		return fail("E_ADDRESS_BUILDER_FIELD", expr.Pos, fmt.Sprintf("%s requires data", name))
	}
	return nil
}

func (c *Compiler) resolvePathType(path []string, env map[string]TypeRef, storage *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, types map[string]*TypeDecl, pos Position) (TypeRef, error) {
	if len(path) == 0 {
		return TypeRef{}, fail("E_PATH", pos, "empty path")
	}
	typ, ok := env[path[0]]
	if !ok {
		return TypeRef{}, fail("E_PATH_IDENT", pos, fmt.Sprintf("unknown path root %q", path[0]))
	}
	if len(path) == 1 {
		return typ, nil
	}
	current := typ
	for i := 1; i < len(path); i++ {
		if storage != nil && current.Name == storage.Name {
			fieldType, ok := lookupStructField(storage, path[i])
			if !ok {
				return TypeRef{}, fail("E_PATH_FIELD", pos, fmt.Sprintf("storage field %q not found", path[i]))
			}
			current = fieldType
			continue
		}
		if fieldType, ok := builtinContextFieldType(current, path[i]); ok {
			current = fieldType
			continue
		}
		if td, ok := types[current.Name]; ok {
			members, err := c.expandTypeDeclMembers(td.Name, types, map[string]bool{})
			if err != nil {
				return TypeRef{}, err
			}
			var matched TypeRef
			found := false
			compatible := true
			for _, member := range members {
				st, ok := structs[member.Name]
				if !ok {
					continue
				}
				fieldType, ok := lookupStructField(st, path[i])
				if !ok {
					continue
				}
				if !found {
					matched = fieldType
					found = true
					continue
				}
				if !compatibleTypesResolved(fieldType, matched, types) {
					compatible = false
					break
				}
			}
			if found && compatible {
				current = matched
				continue
			}
		}
		if st, ok := structs[current.Name]; ok {
			fieldType, ok := lookupStructField(st, path[i])
			if !ok {
				return TypeRef{}, fail("E_PATH_FIELD", pos, fmt.Sprintf("field %q not found in %s", path[i], current.Name))
			}
			current = fieldType
			continue
		}
		return TypeRef{}, fail("E_PATH_TYPE", pos, fmt.Sprintf("cannot select field %q from %s", path[i], current.String()))
	}
	return current, nil
}

func builtinContextFieldType(typ TypeRef, field string) (TypeRef, bool) {
	switch strings.ToLower(typ.Name) {
	case "inmessage":
		switch field {
		case "body":
			return TypeRef{Name: "Segment"}, true
		case "sender":
			return TypeRef{Name: "address"}, true
		case "senderAddress":
			return TypeRef{Name: "address"}, true
		case "value":
			return TypeRef{Name: "coins"}, true
		case "valueCoins":
			return TypeRef{Name: "coins"}, true
		case "opcode":
			return TypeRef{Name: "uint32"}, true
		case "queryId":
			return TypeRef{Name: "uint64"}, true
		case "logicalTime":
			return TypeRef{Name: "uint64"}, true
		case "attachedValue":
			return TypeRef{Name: "coins"}, true
		case "originalForwardFee":
			return TypeRef{Name: "coins"}, true
		}
	case "inmessagebounced":
		switch field {
		case "bouncedBody":
			return TypeRef{Name: "Segment"}, true
		case "body":
			return TypeRef{Name: "Segment"}, true
		}
	case "contractcontext":
		switch field {
		case "address":
			return TypeRef{Name: "address"}, true
		case "balance":
			return TypeRef{Name: "coins"}, true
		case "data":
			return TypeRef{Name: "Chunk"}, true
		}
	case "mapentry":
		switch field {
		case "key":
			if len(typ.Args) > 0 {
				return typ.Args[0], true
			}
		case "value":
			if len(typ.Args) > 1 {
				return typ.Args[1], true
			}
		}
	}
	return TypeRef{}, false
}

func lookupStructField(st *StructDecl, name string) (TypeRef, bool) {
	for _, field := range st.Fields {
		if field.Name == name {
			return field.Type, true
		}
	}
	return TypeRef{}, false
}

func compatibleTypes(a, b TypeRef) bool {
	if strings.EqualFold(a.Name, "null") && b.Optional {
		return true
	}
	if strings.EqualFold(b.Name, "null") && a.Optional {
		return true
	}
	if isMapFamilyType(a.Name) && isMapFamilyType(b.Name) {
		if len(a.Args) == 0 || len(b.Args) == 0 {
			return true
		}
		if len(a.Args) != len(b.Args) {
			return false
		}
		for i := range a.Args {
			if !compatibleTypes(a.Args[i], b.Args[i]) {
				return false
			}
		}
		return true
	}
	if strings.EqualFold(a.Name, b.Name) {
		if isMapFamilyType(a.Name) && (len(a.Args) == 0 || len(b.Args) == 0) {
			return true
		}
		if len(a.Args) != len(b.Args) {
			return false
		}
		for i := range a.Args {
			if !compatibleTypes(a.Args[i], b.Args[i]) {
				return false
			}
		}
		return true
	}
	if isNumericType(a) && isNumericType(b) {
		return true
	}
	if isStringLikeType(a) && isStringLikeType(b) {
		return true
	}
	if isChunkLikeType(a) && isChunkLikeType(b) {
		return true
	}
	if strings.EqualFold(a.Name, "string") && isStringLikeType(b) {
		return true
	}
	if strings.EqualFold(b.Name, "string") && isStringLikeType(a) {
		return true
	}
	return false
}

func isMapFamilyType(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "map", "dict":
		return true
	default:
		return false
	}
}

// stateReadTypeHint looks up a @storage field's declared type by name and
// returns the IRExprStateRead.TypeHint to attach for it -- "map" when the
// field is a Map<K,V> family type (so a truly-absent key on a fresh contract
// defaults to an empty map instead of uint64(0)), "" otherwise (preserving
// the pre-existing uint64(0) default for every scalar field). fieldTypes may
// be nil (e.g. lowering outside a contract's own field set); a missing entry
// is treated the same as a non-Map field.
func stateReadTypeHint(fieldTypes map[string]TypeRef, field string) string {
	if fieldTypes == nil {
		return ""
	}
	if typ, ok := fieldTypes[field]; ok && isMapFamilyType(typ.Name) {
		return "map"
	}
	return ""
}

func resolveSingleMemberTypeRef(t TypeRef, types map[string]*TypeDecl) TypeRef {
	seen := map[string]bool{}
	for {
		if td, ok := types[t.Name]; ok && len(td.Members) == 1 {
			if seen[t.Name] {
				return t
			}
			seen[t.Name] = true
			t = td.Members[0]
			continue
		}
		return t
	}
}

func compatibleTypesResolved(a, b TypeRef, types map[string]*TypeDecl) bool {
	if compatibleTypes(a, b) {
		return true
	}
	if td, ok := types[a.Name]; ok && len(td.Members) == 1 {
		return compatibleTypesResolved(td.Members[0], b, types)
	}
	if td, ok := types[b.Name]; ok && len(td.Members) == 1 {
		return compatibleTypesResolved(a, td.Members[0], types)
	}
	return false
}

func isNumericType(t TypeRef) bool {
	switch canonicalCodecTypeName(t.Name) {
	case "u2", "u4", "u8", "u16", "u32", "u64", "uint", "uint2", "uint4", "uint8", "uint16", "uint32", "uint64", "uint128", "uint256", "int", "i2", "i4", "i8", "i16", "i32", "i64", "int2", "int4", "int8", "int16", "int32", "int64", "int128", "int256", "coins":
		return true
	default:
		return false
	}
}

func isNumericLikeType(t TypeRef, types map[string]*TypeDecl) bool {
	if isNumericType(t) {
		return true
	}
	seen := map[string]bool{}
	var walk func(TypeRef) bool
	walk = func(cur TypeRef) bool {
		name := strings.TrimSpace(cur.Name)
		if name == "" || seen[name] {
			return false
		}
		seen[name] = true
		td, ok := types[name]
		if !ok || len(td.Members) != 1 {
			return false
		}
		if isNumericType(td.Members[0]) {
			return true
		}
		return walk(td.Members[0])
	}
	return walk(t)
}

func isStringLikeType(t TypeRef) bool {
	switch strings.ToLower(t.Name) {
	case "bytes", "hash", "hash32", "address":
		return true
	default:
		return false
	}
}

func isChunkLikeType(t TypeRef) bool {
	switch strings.ToLower(t.Name) {
	case "chunk", "code", "stateinit":
		return true
	default:
		return false
	}
}

func (c *Compiler) buildArtifacts(file *SourceFile, contract *ContractDecl) (avm.InterfaceManifest, SelectorRegistry, StorageLayout, Codec, map[string]Codec, map[string]Codec, map[string]uint32, map[string]MessageUnion, map[string]Codec, map[string]Codec, error) {
	allFunctions := append([]*FunctionDecl(nil), file.Functions...)
	allFunctions = append(allFunctions, contract.Functions...)
	storage, _ := findStruct(file.Structs, contract.StorageTypeName)
	layout, storageCodec := buildStorageLayout(storage)
	msgCodecs := map[string]Codec{}
	bodyCodecs := map[string]Codec{}
	bodyOpcodes := map[string]uint32{}
	unions := map[string]MessageUnion{}
	getterCodecs := map[string]Codec{}
	eventCodecs := map[string]Codec{}
	registry := SelectorRegistry{Contract: contract.Name}
	manifest := avm.InterfaceManifest{Name: contract.Name, Version: DefaultABIVersion, UnknownMessagePolicy: "reject"}
	structs := map[string]*StructDecl{}
	for _, st := range file.Structs {
		if st != nil {
			structs[st.Name] = st
		}
	}
	types := map[string]*TypeDecl{}
	for _, td := range file.Types {
		if td != nil {
			types[td.Name] = td
		}
	}

	seenSelectors := map[uint32]string{}
	seenBodyOpcodes := map[uint32]string{}
	appendSelector := func(kind, name, signature string, selector uint32, topic string, entrypoint string) error {
		if prev, ok := seenSelectors[selector]; ok && prev != signature {
			return fail("E_SELECTOR_COLLISION", Position{}, fmt.Sprintf("selector collision for 0x%08x between %q and %q", selector, prev, signature))
		}
		seenSelectors[selector] = signature
		registry.Entries = append(registry.Entries, SelectorEntry{Kind: kind, Name: name, Signature: signature, Selector: selector, Topic: topic, Entrypoint: entrypoint})
		return nil
	}

	for _, st := range file.Structs {
		if st == nil {
			continue
		}
		if !structHasAnnotation(st, "@message") {
			continue
		}
		opcode, ok := structAnnotationValue(st, "@message")
		if !ok {
			return avm.InterfaceManifest{}, SelectorRegistry{}, StorageLayout{}, Codec{}, nil, nil, nil, nil, nil, nil, fail("E_MESSAGE_OPCODE", st.Pos, fmt.Sprintf("message schema %q requires an opcode", st.Name))
		}
		if prev, exists := bodyOpcodes[st.Name]; exists && prev != opcode {
			return avm.InterfaceManifest{}, SelectorRegistry{}, StorageLayout{}, Codec{}, nil, nil, nil, nil, nil, nil, fail("E_MESSAGE_OPCODE", st.Pos, fmt.Sprintf("message schema %q has conflicting opcodes", st.Name))
		}
		if prev, exists := seenBodyOpcodes[opcode]; exists && prev != st.Name {
			return avm.InterfaceManifest{}, SelectorRegistry{}, StorageLayout{}, Codec{}, nil, nil, nil, nil, nil, nil, fail("E_MESSAGE_OPCODE", st.Pos, fmt.Sprintf("opcode 0x%08x is already bound to message schema %q", opcode, prev))
		}
		codec := Codec{Name: st.Name, Kind: "message_body", Fields: codecFieldsFromStructFields(st.Fields), MaxBytes: int(c.opts.MaxPayloadBytes)}
		buf := make([]byte, 4)
		binary.BigEndian.PutUint32(buf, opcode)
		codec.Hash = sha256.Sum256(append(codecHashBytes(codec), buf...))
		bodyOpcodes[st.Name] = opcode
		seenBodyOpcodes[opcode] = st.Name
		bodyCodecs[st.Name] = codec
	}
	for _, td := range file.Types {
		if td == nil {
			continue
		}
		members, err := c.expandTypeDeclMembers(td.Name, types, map[string]bool{})
		if err != nil {
			return avm.InterfaceManifest{}, SelectorRegistry{}, StorageLayout{}, Codec{}, nil, nil, nil, nil, nil, nil, err
		}
		var variants []MessageUnionVariant
		allMessages := len(members) > 0
		for _, member := range members {
			if member.Optional || len(member.Args) > 0 {
				allMessages = false
				break
			}
			st, ok := structs[member.Name]
			if !ok || !structHasAnnotation(st, "@message") {
				allMessages = false
				break
			}
			opcode, ok := bodyOpcodes[member.Name]
			if !ok {
				allMessages = false
				break
			}
			variants = append(variants, MessageUnionVariant{Name: member.Name, Type: member.String(), Opcode: opcode})
		}
		if !allMessages {
			continue
		}
		sort.SliceStable(variants, func(i, j int) bool {
			if variants[i].Opcode != variants[j].Opcode {
				return variants[i].Opcode < variants[j].Opcode
			}
			if variants[i].Name != variants[j].Name {
				return variants[i].Name < variants[j].Name
			}
			return variants[i].Type < variants[j].Type
		})
		unionHash := sha256.Sum256(messageUnionHashBytes(td.Name, variants))
		unions[td.Name] = MessageUnion{Name: td.Name, Variants: variants, Hash: unionHash}
	}

	sort.SliceStable(contract.Messages, func(i, j int) bool {
		if contract.Messages[i].Kind != contract.Messages[j].Kind {
			return messageKindOrder(contract.Messages[i].Kind) < messageKindOrder(contract.Messages[j].Kind)
		}
		if contract.Messages[i].Name != contract.Messages[j].Name {
			return contract.Messages[i].Name < contract.Messages[j].Name
		}
		return signatureForMessage(contract.Name, contract.Messages[i]) < signatureForMessage(contract.Name, contract.Messages[j])
	})
	for _, msg := range contract.Messages {
		sig := signatureForMessage(contract.Name, msg)
		sel := selectorFromSignature(sig)
		if msg.ExplicitSel != nil {
			sel = *msg.ExplicitSel
		}
		entry := SelectorEntry{Kind: "message", Name: msg.Name, Signature: sig, Selector: sel, Entrypoint: messageKindEntrypointName(msg.Kind)}
		switch msg.Kind {
		case MessageKindDeploy:
			manifest.Methods = append(manifest.Methods, avm.InterfaceMethod{
				Name: msg.Name, Entrypoint: avm.EntryDeploy, Opcode: sel, Async: false,
				Params: paramsToInterface(msg.Params), Results: resultsToInterface(msg.ReturnType), Description: "",
			})
			entry.Entrypoint = entrypointName(avm.EntryDeploy)
		case MessageKindMigrate:
			manifest.Methods = append(manifest.Methods, avm.InterfaceMethod{
				Name: msg.Name, Entrypoint: avm.EntryMigrate, Opcode: sel, Async: false,
				Params: paramsToInterface(msg.Params), Results: resultsToInterface(msg.ReturnType), Description: "",
			})
			entry.Entrypoint = entrypointName(avm.EntryMigrate)
		case MessageKindExternal, MessageKindInternal, MessageKindBounced:
			manifest.AsyncHandlers = append(manifest.AsyncHandlers, avm.InterfaceAsyncHandler{
				Name: msg.Name, Entrypoint: kindToEntrypoint(msg.Kind), Opcode: sel, MessageType: msg.Kind.String() + ":" + msg.Name, Bounced: msg.Kind == MessageKindBounced,
				Idempotent: false, Params: paramsToInterface(msg.Params), Results: resultsToInterface(msg.ReturnType), Description: "",
			})
			entry.Entrypoint = entrypointName(kindToEntrypoint(msg.Kind))
		}
		if err := appendSelector("message", msg.Name, sig, sel, "", entry.Entrypoint); err != nil {
			return avm.InterfaceManifest{}, SelectorRegistry{}, StorageLayout{}, Codec{}, nil, nil, nil, nil, nil, nil, err
		}
		msgCodecs[msg.Name] = Codec{Name: msg.Name, Kind: "message", Fields: paramsToCodecFields(msg.Params), ReturnType: msg.ReturnType, Hash: sha256.Sum256([]byte(sig)), MaxBytes: int(c.opts.MaxPayloadBytes)}
	}
	for _, fn := range contract.Functions {
		kind, ok := functionHandlerAnnotation(fn.Annotations)
		if !ok {
			continue
		}
		entrypoint, ok := handlerAnnotationEntrypoint(kind)
		if !ok {
			return avm.InterfaceManifest{}, SelectorRegistry{}, StorageLayout{}, Codec{}, nil, nil, nil, nil, nil, nil, fail("E_IR", fn.Pos, fmt.Sprintf("unsupported handler annotation %q", kind))
		}
		sig := signatureForFunction(fn)
		sel := selectorFromSignature(sig)
		manifest.AsyncHandlers = append(manifest.AsyncHandlers, avm.InterfaceAsyncHandler{
			Name:        fn.Name,
			Entrypoint:  entrypoint,
			Opcode:      sel,
			MessageType: kind + ":" + fn.Name,
			Bounced:     kind == "@bounced",
			Idempotent:  false,
			Params:      paramsToInterface(fn.Params),
			Results:     resultsToInterface(&fn.ReturnType),
			Description: "",
		})
		if err := appendSelector("message", fn.Name, sig, sel, "", entrypointName(entrypoint)); err != nil {
			return avm.InterfaceManifest{}, SelectorRegistry{}, StorageLayout{}, Codec{}, nil, nil, nil, nil, nil, nil, err
		}
	}

	sort.SliceStable(contract.Getters, func(i, j int) bool {
		if contract.Getters[i].Name != contract.Getters[j].Name {
			return contract.Getters[i].Name < contract.Getters[j].Name
		}
		return signatureForGetter(contract.Name, contract.Getters[i]) < signatureForGetter(contract.Name, contract.Getters[j])
	})
	for _, get := range contract.Getters {
		sig := signatureForGetter(contract.Name, get)
		sel := selectorFromSignature(sig)
		if get.ExplicitSel != nil {
			sel = *get.ExplicitSel
		}
		manifest.GetMethods = append(manifest.GetMethods, avm.InterfaceGetMethod{
			Name: get.Name, Entrypoint: avm.EntryQuery, Selector: sel, Params: paramsToInterface(get.Params), Results: []avm.InterfaceResultDescriptor{{Name: "result", Kind: interfaceKindForType(get.ReturnType), Required: true}}, Cacheable: true, MaxResponseBytes: c.opts.MaxPayloadBytes, Description: "",
		})
		if err := appendSelector("getter", get.Name, sig, sel, "", entrypointName(avm.EntryQuery)); err != nil {
			return avm.InterfaceManifest{}, SelectorRegistry{}, StorageLayout{}, Codec{}, nil, nil, nil, nil, nil, nil, err
		}
		ret := get.ReturnType
		getterCodecs[get.Name] = Codec{Name: get.Name, Kind: "getter", Fields: paramsToCodecFields(get.Params), ReturnType: &ret, Hash: sha256.Sum256([]byte(sig))}
	}
	for _, fn := range contract.Functions {
		if !hasAnnotation(fn.Annotations, "@get") {
			continue
		}
		sig := signatureForGetterFunction(contract.Name, fn)
		sel := selectorFromSignature(sig)
		manifest.GetMethods = append(manifest.GetMethods, avm.InterfaceGetMethod{
			Name: fn.Name, Entrypoint: avm.EntryQuery, Selector: sel, Params: paramsToInterface(fn.Params), Results: []avm.InterfaceResultDescriptor{{Name: "result", Kind: interfaceKindForType(fn.ReturnType), Required: true}}, Cacheable: true, MaxResponseBytes: c.opts.MaxPayloadBytes, Description: "",
		})
		if err := appendSelector("getter", fn.Name, sig, sel, "", entrypointName(avm.EntryQuery)); err != nil {
			return avm.InterfaceManifest{}, SelectorRegistry{}, StorageLayout{}, Codec{}, nil, nil, nil, nil, nil, nil, err
		}
		ret := fn.ReturnType
		getterCodecs[fn.Name] = Codec{Name: fn.Name, Kind: "getter", Fields: paramsToCodecFields(fn.Params), ReturnType: &ret, Hash: sha256.Sum256([]byte(sig))}
	}

	sort.SliceStable(contract.Events, func(i, j int) bool { return contract.Events[i].Name < contract.Events[j].Name })
	for _, event := range contract.Events {
		sig := signatureForEvent(contract.Name, event)
		hash := sha256.Sum256([]byte(sig))
		sel := binary.BigEndian.Uint32(hash[:4])
		manifest.Events = append(manifest.Events, avm.InterfaceEvent{Name: event.Name, Opcode: sel, Fields: paramsToInterface(event.Fields)})
		if err := appendSelector("event", event.Name, sig, sel, fmt.Sprintf("%x", hash[:]), "event"); err != nil {
			return avm.InterfaceManifest{}, SelectorRegistry{}, StorageLayout{}, Codec{}, nil, nil, nil, nil, nil, nil, err
		}
		eventCodecs[event.Name] = Codec{Name: event.Name, Kind: "event", Fields: paramsToCodecFields(event.Fields), Hash: hash}
	}

	sort.SliceStable(contract.WalletActions, func(i, j int) bool { return contract.WalletActions[i].Name < contract.WalletActions[j].Name })
	for _, wallet := range contract.WalletActions {
		sig := signatureForWalletAction(contract.Name, wallet)
		sel := selectorFromSignature(sig)
		hash := sha256.Sum256([]byte(sig))
		manifest.WalletActions = append(manifest.WalletActions, avm.InterfaceWalletAction{
			Method:              wallet.Name,
			Title:               wallet.Title,
			Description:         "",
			Category:            contract.Name,
			Icon:                "",
			Risk:                avm.InterfaceWalletRisk(wallet.Risk),
			ConfirmLabel:        wallet.ConfirmLabel,
			WarningLevel:        avm.InterfaceWalletWarningLevel(wallet.WarningLevel),
			ExpectedSideEffects: append([]string(nil), wallet.ExpectedSideEffects...),
			FundAccess:          wallet.FundAccess,
			ApprovalSemantics:   avm.InterfaceWalletApprovalSemantics(wallet.ApprovalSemantics),
			Inputs:              paramsToInterface(wallet.Inputs),
			Outputs:             paramsToInterface(wallet.Outputs),
		})
		if err := appendSelector("wallet_action", wallet.Name, sig, sel, fmt.Sprintf("%x", hash[:]), "wallet_action"); err != nil {
			return avm.InterfaceManifest{}, SelectorRegistry{}, StorageLayout{}, Codec{}, nil, nil, nil, nil, nil, nil, err
		}
	}

	manifest.CLIBindings = buildCLIBindings(contract)
	manifest.SDKBindings = buildSDKBindings(contract)
	if manifest.Name == "" {
		manifest.Name = contract.Name
	}

	if _, err := avm.InterfaceHash(manifest); err != nil {
		return avm.InterfaceManifest{}, SelectorRegistry{}, StorageLayout{}, Codec{}, nil, nil, nil, nil, nil, nil, err
	}
	registry.RegistryHash = sha256.Sum256(registryHashBytes(registry))
	layout.LayoutHash = sha256.Sum256(layoutHashBytes(layout))
	storageCodec.Hash = sha256.Sum256(codecHashBytes(storageCodec))
	return manifest, registry, layout, storageCodec, msgCodecs, bodyCodecs, bodyOpcodes, unions, getterCodecs, eventCodecs, nil
}

func buildStorageLayout(storage *StructDecl) (StorageLayout, Codec) {
	if storage == nil {
		return StorageLayout{}, Codec{Kind: "storage"}
	}
	layout := StorageLayout{Name: storage.Name}
	codec := Codec{Name: storage.Name, Kind: "storage"}
	for _, field := range storage.Fields {
		item := CodecField{Name: field.Name, Lazy: field.Lazy, Type: field.Type, Default: field.Default, Pos: field.Pos}
		layout.Fields = append(layout.Fields, item)
		codec.Fields = append(codec.Fields, item)
	}
	return layout, codec
}

func codecFieldsFromStructFields(fields []FieldDecl) []CodecField {
	out := make([]CodecField, 0, len(fields))
	for _, field := range fields {
		out = append(out, CodecField{Name: field.Name, Lazy: field.Lazy, Type: field.Type, Default: field.Default, Pos: field.Pos})
	}
	return out
}

func structHasAnnotation(st *StructDecl, name string) bool {
	if st == nil {
		return false
	}
	for _, ann := range st.Annotations {
		if ann.Name == name {
			return true
		}
	}
	return false
}

func structAnnotationValue(st *StructDecl, name string) (uint32, bool) {
	if st == nil {
		return 0, false
	}
	for _, ann := range st.Annotations {
		if ann.Name == name && ann.Value != nil {
			return *ann.Value, true
		}
	}
	return 0, false
}

func (c *Compiler) buildModule(file *SourceFile, contract *ContractDecl, manifest avm.InterfaceManifest, registry SelectorRegistry, msgCodecs map[string]Codec, getterCodecs map[string]Codec, eventCodecs map[string]Codec, msgOpcodes map[string]uint32, lock DependencyLock, functions map[string]*FunctionDecl) (avm.Module, []byte, *IRProgram, error) {
	ir, err := c.buildIR(file, contract, registry, msgOpcodes, lock, functions)
	if err != nil {
		return avm.Module{}, nil, nil, err
	}
	module := avm.Module{Version: avm.Version, Imports: nil, Exports: map[avm.Entrypoint]uint32{}, MetadataHash: [avm.MetadataHashLength]byte{}}
	h, err := avm.InterfaceHash(manifest)
	if err != nil {
		return avm.Module{}, nil, nil, err
	}
	copy(module.MetadataHash[:], h[:])
	var code []avm.Instruction
	grouped := map[avm.Entrypoint][]IREntry{}
	var entryOrder []avm.Entrypoint
	for _, entry := range ir.Entries {
		if _, ok := grouped[entry.Entrypoint]; !ok {
			entryOrder = append(entryOrder, entry.Entrypoint)
		}
		grouped[entry.Entrypoint] = append(grouped[entry.Entrypoint], entry)
	}
	for _, entrypoint := range entryOrder {
		entries := grouped[entrypoint]
		entryBase := uint32(len(code))
		module.Exports[entrypoint] = entryBase
		combined := entries[0]
		// EntryQuery always dispatches — even with a single getter — so an
		// unknown method selector aborts instead of silently running whichever
		// getter happens to be compiled in.
		if len(entries) > 1 || entrypoint == avm.EntryQuery {
			// Several entries share one entrypoint (e.g. multiple getters on
			// EntryQuery). Dispatch by selector and fail closed on unknown
			// selectors instead of silently dropping entries.
			combined = IREntry{
				Name:       entries[0].Name,
				Kind:       entries[0].Kind,
				Entrypoint: entrypoint,
				Selector:   entries[0].Selector,
				Pos:        entries[0].Pos,
			}
			var stmts []IRStmt
			for i, e := range entries {
				skip := fmt.Sprintf("__dispatch_%d_%d", entrypoint, i)
				cond := &IRExpr{
					Kind:  IRExprEq,
					Left:  &IRExpr{Kind: IRExprMsgOpcode, Pos: e.Pos},
					Right: &IRExpr{Kind: IRExprConstU64, Value: uint64(e.Selector), Pos: e.Pos},
					Pos:   e.Pos,
				}
				if entrypoint == avm.EntryQuery {
					// A getter is also callable by its NAME alone: the
					// name-alias selector (avm.GetterNameSelector) routes to
					// the same body, binding explorer/wallet calls to the
					// exact source-level function name.
					aliasCond := &IRExpr{
						Kind:  IRExprEq,
						Left:  &IRExpr{Kind: IRExprMsgOpcode, Pos: e.Pos},
						Right: &IRExpr{Kind: IRExprConstU64, Value: uint64(avm.GetterNameSelector(e.Name)), Pos: e.Pos},
						Pos:   e.Pos,
					}
					cond = &IRExpr{Kind: IRExprOr, Left: cond, Right: aliasCond, Pos: e.Pos}
				}
				stmts = append(stmts, IRStmt{Kind: IRStmtJumpIfZero, Target: skip, Expr: cond, Position: e.Pos})
				stmts = append(stmts, e.Statements...)
				stmts = append(stmts, IRStmt{Kind: IRStmtLabel, Name: skip, Position: e.Pos})
			}
			stmts = append(stmts, IRStmt{Kind: IRStmtAbort, Arg: 0xffff, Position: entries[0].Pos})
			combined.Statements = stmts
		}
		lowered, err := c.lowerIREntry(combined)
		if err != nil {
			return avm.Module{}, nil, nil, err
		}
		relocateJumpTargets(lowered, entryBase)
		code = append(code, lowered...)
	}
	module.Code = code
	module.Imports = importsForCode(code)
	encoded, err := avm.EncodeModule(module)
	if err != nil {
		return avm.Module{}, nil, nil, err
	}
	return module, encoded, ir, nil
}

func relocateJumpTargets(code []avm.Instruction, base uint32) {
	if base == 0 {
		return
	}
	for i := range code {
		switch code[i].Op {
		case avm.OpJump, avm.OpJumpIfZero:
			code[i].Arg += uint64(base)
		}
	}
}

func (c *Compiler) buildIR(file *SourceFile, contract *ContractDecl, registry SelectorRegistry, msgOpcodes map[string]uint32, lock DependencyLock, functions map[string]*FunctionDecl) (*IRProgram, error) {
	allFunctions := append([]*FunctionDecl(nil), file.Functions...)
	allFunctions = append(allFunctions, contract.Functions...)
	structs := map[string]*StructDecl{}
	for _, st := range file.Structs {
		if st != nil {
			structs[st.Name] = st
		}
	}
	// storageFieldTypes maps the contract's @storage struct's field names to
	// their declared types, so a `<storageAlias>.<field>` read can tag its
	// IRExprStateRead with a type-aware zero-default hint (see loweringEnv
	// and the IRExprStateRead lowering below) instead of always falling back
	// to the generic uint64(0) default when the field is absent from a
	// fresh/genesis contract's storage.
	storageFieldTypes := map[string]TypeRef{}
	if storageStruct, ok := structs[contract.StorageTypeName]; ok && storageStruct != nil {
		for _, f := range storageStruct.Fields {
			storageFieldTypes[f.Name] = f.Type
		}
	}
	consts, err := c.buildConstEnv(file, functions)
	if err != nil {
		return nil, err
	}
	oldConsts := c.globalConsts
	c.globalConsts = consts
	defer func() {
		c.globalConsts = oldConsts
	}()
	program := &IRProgram{
		Contract:         contract.Name,
		Package:          file.Package,
		TraceCommitments: map[string]string{},
		Dependencies:     append([]ResolvedDependency(nil), lock.Entries...),
		LoweringRules:    StatementLoweringRules(),
	}
	externalPrelude := func(params []ParamDecl) []IRStmt {
		if len(params) != 1 {
			return nil
		}
		if !strings.EqualFold(params[0].Type.Name, "Segment") {
			return nil
		}
		return []IRStmt{
			{Kind: IRStmtPushU64, Expr: &IRExpr{Kind: IRExprMsgBody, Pos: params[0].Pos}, Position: params[0].Pos},
			{Kind: IRStmtStoreLocal, Slot: 0, Position: params[0].Pos},
		}
	}
	blockOrder := []struct {
		kind       MessageKind
		entrypoint avm.Entrypoint
	}{
		{MessageKindDeploy, avm.EntryDeploy},
		{MessageKindExternal, avm.EntryReceiveExternal},
		{MessageKindInternal, avm.EntryReceiveInternal},
		{MessageKindBounced, avm.EntryReceiveBounced},
		{MessageKindMigrate, avm.EntryMigrate},
	}
	for _, block := range blockOrder {
		handlers := c.collectHandlersForKind(contract, block.kind)
		for _, handle := range handlers {
			commit, err := traceCommitment(contract.Name, block.kind.String(), []canonicalHandle{handle})
			if err != nil {
				return nil, err
			}
			msg := findMessage(contract, handle.Name, block.kind)
			if msg == nil {
				return nil, fail("E_IR", Position{}, fmt.Sprintf("message %q not found while lowering", handle.Name))
			}
			entry := IREntry{
				Name:       msg.Name,
				Kind:       block.kind.String(),
				Entrypoint: block.entrypoint,
				Selector:   selectorForMessage(contract.Name, msg),
				Statements: []IRStmt{{Kind: IRStmtTrace, Data: commit[:], Position: msg.Pos}},
				Pos:        msg.Pos,
			}
			body, err := c.lowerStatementsToIR(msg.Body, msg.Params, msg.ReturnType, false, true, functions, structs, msgOpcodes, loweringEnv{types: map[string]TypeRef{}, consts: consts, msgOpcodes: msgOpcodes, storageFieldTypes: storageFieldTypes}, c.initialScope(msg.Params, true), nil)
			if err != nil {
				return nil, err
			}
			entry.Statements = append(entry.Statements, body...)
			if block.entrypoint == avm.EntryReceiveExternal {
				entry.Statements = append(externalPrelude(msg.Params), entry.Statements...)
			}
			program.TraceCommitments[entrypointName(entry.Entrypoint)+":"+entry.Name] = fmt.Sprintf("%x", commit[:])
			program.Entries = append(program.Entries, entry)
		}
	}
	for _, fn := range contract.Functions {
		kind, ok := functionHandlerAnnotation(fn.Annotations)
		if !ok {
			continue
		}
		entrypoint, ok := handlerAnnotationEntrypoint(kind)
		if !ok {
			return nil, fail("E_IR", fn.Pos, fmt.Sprintf("unsupported handler annotation %q", kind))
		}
		handle := canonicalHandle{
			Name:       fn.Name,
			Signature:  signatureForFunction(fn),
			Selector:   selectorFromSignature(signatureForFunction(fn)),
			Entrypoint: entrypointName(entrypoint),
			Params:     typeNamesFromParams(fn.Params),
			Results:    typeNamesFromResults(&fn.ReturnType),
			Body:       canonicalStatements(fn.Body),
		}
		commit, err := traceCommitment(contract.Name, kind[1:], []canonicalHandle{handle})
		if err != nil {
			return nil, err
		}
		body, err := c.lowerStatementsToIR(fn.Body, fn.Params, &fn.ReturnType, false, true, functions, structs, msgOpcodes, loweringEnv{types: map[string]TypeRef{}, consts: consts, msgOpcodes: msgOpcodes, storageFieldTypes: storageFieldTypes}, c.initialScope(fn.Params, true), nil)
		if err != nil {
			return nil, err
		}
		program.TraceCommitments[entrypointName(entrypoint)+":"+fn.Name] = fmt.Sprintf("%x", commit[:])
		statements := append([]IRStmt{{Kind: IRStmtTrace, Data: commit[:], Position: fn.Pos}}, body...)
		if entrypoint == avm.EntryReceiveExternal {
			statements = append(externalPrelude(fn.Params), statements...)
		}
		program.Entries = append(program.Entries, IREntry{
			Name:       fn.Name,
			Kind:       kind[1:],
			Entrypoint: entrypoint,
			Selector:   selectorFromSignature(signatureForFunction(fn)),
			Statements: statements,
			Pos:        fn.Pos,
		})
	}
	for _, fn := range contract.Functions {
		if !hasAnnotation(fn.Annotations, "@get") {
			continue
		}
		sig := signatureForGetterFunction(contract.Name, fn)
		handle := canonicalHandle{
			Name:       fn.Name,
			Signature:  sig,
			Selector:   selectorFromSignature(sig),
			Entrypoint: entrypointName(avm.EntryQuery),
			Params:     typeNamesFromParams(fn.Params),
			Results:    typeNamesFromResults(&fn.ReturnType),
			Body:       canonicalStatements(fn.Body),
		}
		commit, err := traceCommitment(contract.Name, "query", []canonicalHandle{handle})
		if err != nil {
			return nil, err
		}
		body, err := c.lowerStatementsToIR(fn.Body, fn.Params, &fn.ReturnType, true, true, functions, structs, msgOpcodes, loweringEnv{types: map[string]TypeRef{}, consts: consts, msgOpcodes: msgOpcodes, storageFieldTypes: storageFieldTypes}, c.initialScope(fn.Params, true), nil)
		if err != nil {
			return nil, err
		}
		program.TraceCommitments[entrypointName(avm.EntryQuery)+":"+fn.Name] = fmt.Sprintf("%x", commit[:])
		program.Entries = append(program.Entries, IREntry{
			Name:       fn.Name,
			Kind:       "query",
			Entrypoint: avm.EntryQuery,
			Selector:   selectorFromSignature(sig),
			Statements: append([]IRStmt{{Kind: IRStmtTrace, Data: commit[:], Position: fn.Pos}}, body...),
			Pos:        fn.Pos,
		})
	}
	for _, handle := range c.collectGetters(contract) {
		commit, err := traceCommitment(contract.Name, "query", []canonicalHandle{handle})
		if err != nil {
			return nil, err
		}
		get := findGetter(contract, handle.Name)
		if get == nil {
			return nil, fail("E_IR", Position{}, fmt.Sprintf("getter %q not found while lowering", handle.Name))
		}
		ret := get.ReturnType
		entry := IREntry{
			Name:       get.Name,
			Kind:       "query",
			Entrypoint: avm.EntryQuery,
			Selector:   selectorForGetter(contract.Name, get),
			Statements: []IRStmt{{Kind: IRStmtTrace, Data: commit[:], Position: get.Pos}},
			Pos:        get.Pos,
		}
		body, err := c.lowerStatementsToIR(get.Body, get.Params, &ret, true, true, functions, structs, msgOpcodes, loweringEnv{types: map[string]TypeRef{}, consts: consts, msgOpcodes: msgOpcodes, storageFieldTypes: storageFieldTypes}, c.initialScope(get.Params, true), nil)
		if err != nil {
			return nil, err
		}
		entry.Statements = append(entry.Statements, body...)
		program.TraceCommitments[entrypointName(entry.Entrypoint)+":"+entry.Name] = fmt.Sprintf("%x", commit[:])
		program.Entries = append(program.Entries, entry)
	}
	sort.SliceStable(program.Entries, func(i, j int) bool {
		if program.Entries[i].Entrypoint == program.Entries[j].Entrypoint {
			return program.Entries[i].Name < program.Entries[j].Name
		}
		return program.Entries[i].Entrypoint < program.Entries[j].Entrypoint
	})
	for i := range program.Entries {
		for j := range program.Entries[i].Statements {
			coerceStructLiteralFieldTypes(program.Entries[i].Statements[j].Expr, structs)
		}
	}
	return program, nil
}

// coerceStructLiteralFieldTypes walks a lowered expression tree looking for
// IRExprStruct nodes and retags bare-literal field values to match their
// declared struct field type where the default lowering (IRExprConstU64,
// always TagUint64) would otherwise diverge from the canonical tag a
// non-literal source (message field, storage read) carries for the same
// field. Without this, e.g. `SomeStorage { balance: 0 }` for a `coins`-typed
// field encodes as TagUint64 while every other source of that field encodes
// TagCoins, breaking anything that hashes the struct — such as an address
// derived via counterfactualAddress/autoDeployAddress from that literal.
func coerceStructLiteralFieldTypes(expr *IRExpr, structs map[string]*StructDecl) {
	if expr == nil {
		return
	}
	if expr.Kind == IRExprStruct {
		if st, ok := structs[expr.Text]; ok {
			for i := range expr.Fields {
				field := &expr.Fields[i]
				fieldType, ok := lookupStructField(st, field.Name)
				if !ok || field.Expr == nil {
					continue
				}
				if strings.EqualFold(fieldType.Name, "coins") && field.Expr.Kind == IRExprConstU64 {
					field.Expr = &IRExpr{Kind: IRExprCoinsCast, Left: field.Expr, Pos: field.Expr.Pos}
				}
			}
		}
	}
	coerceStructLiteralFieldTypes(expr.Left, structs)
	coerceStructLiteralFieldTypes(expr.Right, structs)
	coerceStructLiteralFieldTypes(expr.Else, structs)
	for _, arg := range expr.Args {
		coerceStructLiteralFieldTypes(arg, structs)
	}
	for i := range expr.Fields {
		coerceStructLiteralFieldTypes(expr.Fields[i].Expr, structs)
	}
}

func (c *Compiler) lowerStatementsToIR(stmts []Statement, params []ParamDecl, ret *TypeRef, readOnly bool, ensureReturn bool, functions map[string]*FunctionDecl, structs map[string]*StructDecl, msgOpcodes map[string]uint32, envInit loweringEnv, scopeInit map[string]struct{}, loops []loopContext) ([]IRStmt, error) {
	env := cloneLoweringEnv(envInit)
	for i, param := range params {
		env.params[param.Name] = i
		env.types[param.Name] = param.Type
	}
	if len(env.consts) == 0 && len(c.globalConsts) > 0 {
		for name, value := range c.globalConsts {
			env.consts[name] = value
		}
	}
	scope := map[string]struct{}{}
	for name := range scopeInit {
		scope[name] = struct{}{}
	}
	var out []IRStmt
	for _, stmt := range stmts {
		switch stmt.Kind {
		case StatementBinding:
			if _, exists := scope[stmt.Name]; exists {
				return nil, fail("E_LOWER_BINDING", stmt.Pos, fmt.Sprintf("duplicate binding %q in the same scope", stmt.Name))
			}
			if stmt.Value.Kind == ExprCall && len(stmt.Value.Path) >= 2 {
				env.types[stmt.Name] = TypeRef{Name: stmt.Value.Path[0], Pos: stmt.Value.Pos}
			} else if stmt.Value.Kind == ExprCall && len(stmt.Value.Path) == 1 {
				// A plain (non-receiver) call to a declared helper function
				// (e.g. `const cap = mintCapabilityFor(...)`) needs the
				// callee's own declared return type, not a Path[0] proxy —
				// there's no dotted receiver segment to borrow one from.
				// Without this, a local bound from a struct-returning @pure
				// helper never gets a lowering-time type, so a later
				// `cap.field` access (including through inlining substitution
				// at another call site) is rejected as unsupported even
				// though the value is a perfectly ordinary struct map.
				if fn := resolveUserFunction(stmt.Value, functions); fn != nil {
					env.types[stmt.Name] = fn.ReturnType
				}
			} else if stmt.Value.Kind == ExprPath && len(stmt.Value.Path) >= 1 {
				env.types[stmt.Name] = TypeRef{Name: stmt.Value.Path[0], Pos: stmt.Value.Pos}
			} else if stmt.Value.Kind == ExprStruct && stmt.Value.Text != "" {
				// A struct-literal RHS (`Bp{bps: 30}`) lowers to a runtime map
				// (IRExprStruct -> OpMapEmpty/OpMapSet), and OpReadField already
				// knows how to read a named field out of that map at runtime —
				// the only missing piece was tagging the local's lowering-time
				// type so `b.bps` isn't rejected by isFieldLikeType("").
				env.types[stmt.Name] = TypeRef{Name: stmt.Value.Text, Pos: stmt.Value.Pos}
			} else if stmt.Value.Kind == ExprIdent {
				// Copying an existing struct-typed local/param (`var c = b`)
				// must carry the source's field-like type forward too, so a
				// subsequent `c.field` access resolves the same way `b.field`
				// would. The actual value copy stays a plain local load/store;
				// see runtimeMapSet (avm/value.go), which never mutates a
				// shared map in place, so this copy cannot alias b.
				if binding, ok := env.locals[stmt.Value.Text]; ok {
					env.types[stmt.Name] = binding.Type
				} else if typ, ok := env.types[stmt.Value.Text]; ok {
					env.types[stmt.Name] = typ
				}
			}
			v, ok := evalConstValue(stmt.Value, env, functions, map[string]bool{})
			if !stmt.Mutable && ok {
				scope[stmt.Name] = struct{}{}
				env.consts[stmt.Name] = v
				var arg uint64
				if v.Kind == constKindU64 {
					arg = v.U64
				}
				out = append(out, IRStmt{Kind: IRStmtLetConst, Name: stmt.Name, Arg: arg, Position: stmt.Pos})
				continue
			}
			if isStorageLoadBinding(stmt.Value) {
				scope[stmt.Name] = struct{}{}
				env.storageAliases[stmt.Name] = struct{}{}
				continue
			}
			binding := localBinding{Slot: env.nextLocalSlot, Type: env.types[stmt.Name], Mutable: stmt.Mutable, FromHandlerMessage: exprIsHandlerMessage(stmt.Value, env)}
			env.nextLocalSlot++
			env.locals[stmt.Name] = binding
			scope[stmt.Name] = struct{}{}
			expr, err := lowerExprToIR(stmt.Value, env, functions, map[string]bool{})
			if err == nil && stmt.Value.Kind == ExprCall && strings.EqualFold(stmt.Value.Text, "buildMessage") {
				expr, err = lowerBuildMessageExpr(stmt.Value, env, functions, msgOpcodes)
			}
			if err != nil {
				return nil, err
			}
			out = append(out, IRStmt{Kind: IRStmtPushU64, Expr: expr, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtStoreLocal, Slot: binding.Slot, Position: stmt.Pos})
		case StatementSet:
			if readOnly {
				return nil, fail("E_GETTER_MUTATION", stmt.Pos, "getter cannot write storage")
			}
			if len(stmt.Path) == 1 {
				root := stmt.Path[0]
				if binding, ok := env.locals[root]; ok {
					if !binding.Mutable {
						return nil, fail("E_LOWER_SET", stmt.Pos, fmt.Sprintf("binding %q is immutable", root))
					}
					expr, err := lowerExprToIR(stmt.Value, env, functions, map[string]bool{})
					if err != nil {
						return nil, err
					}
					out = append(out, IRStmt{Kind: IRStmtPushU64, Expr: expr, Position: stmt.Pos})
					out = append(out, IRStmt{Kind: IRStmtStoreLocal, Slot: binding.Slot, Position: stmt.Pos})
					continue
				}
			}
			if len(stmt.Path) < 2 {
				return nil, fail("E_LOWER_SET", stmt.Pos, "AVM v1 lowering supports only direct state.<field> writes")
			}
			if len(stmt.Path) >= 3 {
				// Already rejected by validateStatement's E_SET_NESTED_UNSUPPORTED
				// check, which always runs before lowering -- this is a
				// redundant, defense-in-depth guard at the lowering layer
				// itself (mirroring this switch's other doubled-up checks,
				// e.g. the readOnly guard just below), so a future change to
				// the earlier validation pass can never silently let a
				// 3+-segment write reach here. Without this guard, both
				// branches below only ever inspect stmt.Path[1], which would
				// silently truncate a target like `st.outer.b` into a
				// whole-field overwrite of `outer`, discarding `.b` entirely --
				// a real, silent state-corruption bug found via this session's
				// own adversarial review.
				return nil, fail("E_LOWER_SET", stmt.Pos, "AVM v1 lowering supports only direct state.<field> writes")
			}
			if binding, ok := env.locals[stmt.Path[0]]; ok {
				if !binding.Mutable {
					return nil, fail("E_LOWER_SET", stmt.Pos, fmt.Sprintf("binding %q is immutable", stmt.Path[0]))
				}
				if !isFieldLikeType(binding.Type.Name) {
					return nil, fail("E_LOWER_SET", stmt.Pos, "local field assignment requires a struct-like binding")
				}
				targetStruct := structs[binding.Type.Name]
				if targetStruct == nil {
					return nil, fail("E_LOWER_SET", stmt.Pos, fmt.Sprintf("local binding %q has unknown struct type %s", stmt.Path[0], binding.Type.Name))
				}
				if _, ok := lookupStructField(targetStruct, stmt.Path[1]); !ok {
					return nil, fail("E_LOWER_SET", stmt.Pos, fmt.Sprintf("field %q not found on %s", stmt.Path[1], targetStruct.Name))
				}
				expr, err := lowerExprToIR(stmt.Value, env, functions, map[string]bool{})
				if err != nil {
					return nil, err
				}
				updated := &IRExpr{
					Kind: IRExprMapSet,
					Left: &IRExpr{Kind: IRExprLocalLoad, Slot: binding.Slot, Pos: stmt.Pos},
					Args: []*IRExpr{{Kind: IRExprConstString, Text: stmt.Path[1], Pos: stmt.Pos}, expr},
					Pos:  stmt.Pos,
				}
				out = append(out, IRStmt{Kind: IRStmtPushU64, Expr: updated, Position: stmt.Pos})
				out = append(out, IRStmt{Kind: IRStmtStoreLocal, Slot: binding.Slot, Position: stmt.Pos})
				continue
			}
			if stmt.Path[0] != "state" {
				if _, ok := env.storageAliases[stmt.Path[0]]; !ok {
					return nil, fail("E_LOWER_SET", stmt.Pos, "AVM v1 lowering supports only state aliases bound from <StorageType>.load()")
				}
			}
			expr, err := lowerExprToIR(stmt.Value, env, functions, map[string]bool{})
			if err != nil {
				return nil, err
			}
			out = append(out, IRStmt{Kind: IRStmtStoreState, Key: stmt.Path[1], Expr: expr, Position: stmt.Pos})
		case StatementEmit:
			out = append(out, IRStmt{Kind: IRStmtTrace, Name: stmt.Name, Data: eventTraceData(stmt), Position: stmt.Pos})
		case StatementAssert:
			cond, err := lowerExprToIR(stmt.Value, env, functions, map[string]bool{})
			if err != nil {
				return nil, err
			}
			throwExpr := stmt.Extra["throw"]
			code := uint64(0)
			if throwExpr.Kind != "" {
				if v, ok := evalConstU64(throwExpr, env, functions, map[string]bool{}); ok {
					code = v
				}
			}
			failLabel := c.nextLabel("assert_fail")
			endLabel := c.nextLabel("assert_end")
			out = append(out, IRStmt{Kind: IRStmtJumpIfZero, Target: failLabel, Expr: cond, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtJump, Target: endLabel, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtLabel, Name: failLabel, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtAbort, Arg: code, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtLabel, Name: endLabel, Position: stmt.Pos})
		case StatementThrow:
			code, ok := evalConstU64(stmt.Value, env, functions, map[string]bool{})
			if !ok {
				return nil, fail("E_LOWER_THROW", stmt.Pos, "throw exit code must be compile-time constant")
			}
			out = append(out, IRStmt{Kind: IRStmtAbort, Arg: code, Position: stmt.Pos})
		case StatementBreak:
			if len(loops) == 0 {
				return nil, fail("E_LOWER_LOOP", stmt.Pos, "break is only allowed inside a loop")
			}
			out = append(out, IRStmt{Kind: IRStmtJump, Target: loops[len(loops)-1].breakTarget, Position: stmt.Pos})
		case StatementContinue:
			if len(loops) == 0 {
				return nil, fail("E_LOWER_LOOP", stmt.Pos, "continue is only allowed inside a loop")
			}
			out = append(out, IRStmt{Kind: IRStmtJump, Target: loops[len(loops)-1].continueTarget, Position: stmt.Pos})
		case StatementExpr:
			if stmt.Value.Kind == ExprCall && len(stmt.Value.Path) >= 2 {
				method := strings.ToLower(stmt.Value.Path[len(stmt.Value.Path)-1])
				switch method {
				case "save":
					// `.save()` as a bare statement is provably a no-op and is
					// deliberately compiled away instead of emitting a real
					// OpReadStorage+OpWriteStorage pair.
					//
					// AVM v1's ONLY storage-write path is the direct
					// `state.<field> = expr` / `<alias>.<field> = expr`
					// assignment handled by `case StatementSet:` above (which
					// also covers `+=`/`-=`, desugared to the same form by the
					// parser) -- it performs an immediate, single-key
					// OpWriteStorage the moment a field changes. By the time
					// `.save()` runs, every field the message touched this call
					// is therefore already durably persisted under its own key.
					//
					// The whole-snapshot form this used to lower to read the
					// ENTIRE current `Storage` map (OpReadStorage with an empty
					// key -> runtimeStorageSnapshotValue: CanonicalDecode every
					// stored key, O(total contract storage)) and immediately
					// wrote that exact same decoded value straight back out
					// (OpWriteStorage with an empty key ->
					// runtimeStorageFromValue: CanonicalEncode every entry back
					// to its own key, also O(total contract storage)) with zero
					// transformation in between. Since every byte ever stored
					// under a field key was itself produced by CanonicalEncode,
					// that decode-then-re-encode round trip is byte-identical to
					// what was already there -- so the old lowering billed gas
					// proportional to the CONTRACT'S ENTIRE STORAGE FOOTPRINT on
					// every `.save()` call for zero observable effect on state,
					// regardless of how many fields the message actually
					// touched. See security-audit/05-findings/
					// FINDING-001-avm-gas-mispricing-dos.md and the "Write-path
					// gas boundedness" note in
					// examples/avm/collections/pagination_stdlib.atlx for the
					// measured before/after numbers.
					//
					// The `@store func <Type>.save(self) { contract.setData(self.
					// toChunk()) }` body a contract author writes is boilerplate
					// required for the type/shape declaration, exactly like
					// `.load()`'s body (see isStorageLoadBinding above) -- it is
					// never actually executed for this bare-statement idiom.
					//
					// A getter calling `.save()` is already rejected earlier by
					// functionDirectEffectMessage's StatementSet case (compile.go
					// ~line 476), which runs before this lowering pass -- this
					// check can never actually trigger. It is kept anyway as a
					// redundant, defense-in-depth guard at the lowering layer
					// itself, mirroring `case StatementSet:` just above (line
					// 3947-3950), which carries the exact same doubled-up
					// readOnly check for the same reason: if the earlier
					// validation pass's coverage of "which statements count as
					// mutation" ever narrows or diverges from this switch, the
					// lowering layer still refuses to silently emit no-op
					// bytecode for a getter that thinks it wrote storage.
					if readOnly {
						return nil, fail("E_GETTER_MUTATION", stmt.Pos, "getter cannot write storage")
					}
					continue
				case "deletedata":
					out = append(out, IRStmt{Kind: IRStmtDeleteState, Key: "", Position: stmt.Pos})
					continue
				case "setdata":
					if len(stmt.Value.Args) != 1 {
						return nil, fail("E_LOWER_CALL", stmt.Pos, "setData() requires one argument")
					}
					expr, err := lowerExprToIR(stmt.Value.Args[0], env, functions, map[string]bool{})
					if err != nil {
						return nil, err
					}
					out = append(out, IRStmt{Kind: IRStmtStoreState, Key: "", Expr: expr, Position: stmt.Pos})
					continue
				}
			}
			continue
		case StatementSend:
			if readOnly {
				return nil, fail("E_GETTER_SEND", stmt.Pos, "getter cannot send internal messages")
			}
			opcode, err := staticOpcode(stmt)
			if err != nil {
				return nil, err
			}
			expr, err := lowerExprToIR(stmt.Value, env, functions, map[string]bool{})
			if err != nil {
				return nil, err
			}
			// The send mode is declared exclusively in buildMessage via the
			// `mode:` field (validated there and carried in the runtime
			// message value). `.send()` takes no arguments and the statement
			// form accepts no `mode =` extra, so there is a single canonical
			// place for delivery semantics.
			if modeExpr, ok := stmt.Extra["mode"]; ok {
				return nil, fail("E_SEND_MODE", modeExpr.Pos, "send statements do not accept a mode: declare it in buildMessage via the mode: field")
			}
			out = append(out, IRStmt{Kind: IRStmtEmitInternal, Opcode: opcode, Expr: expr, Data: statementTraceData(stmt), Position: stmt.Pos})
		case StatementRefund:
			if readOnly {
				return nil, fail("E_GETTER_REFUND", stmt.Pos, "getter cannot refund")
			}
			out = append(out, IRStmt{Kind: IRStmtEmitInternal, Opcode: refundOpcode, Data: statementTraceData(stmt), Position: stmt.Pos})
		case StatementSelf:
			if readOnly {
				return nil, fail("E_GETTER_SELF", stmt.Pos, "getter cannot schedule self messages")
			}
			delay, ok := constU64(stmt.Value)
			if !ok || delay == 0 {
				return nil, fail("E_LOWER_SELF", stmt.Pos, "self delay must be a positive uint64 constant")
			}
			out = append(out, IRStmt{Kind: IRStmtScheduleSelf, Arg: delay, Data: statementTraceData(stmt), Position: stmt.Pos})
		case StatementReturn:
			expr, err := lowerExprToIR(stmt.Value, env, functions, map[string]bool{})
			if err != nil {
				return nil, err
			}
			resultCode := uint64(0)
			if ret == nil {
				if v, ok := constU64(stmt.Value); ok {
					resultCode = v
					expr = nil
				}
			}
			out = append(out, IRStmt{Kind: IRStmtReturn, Arg: resultCode, Expr: expr, Position: stmt.Pos})
		case StatementIf:
			cond, err := lowerExprToIR(stmt.Value, env, functions, map[string]bool{})
			if err != nil {
				return nil, err
			}
			elseLabel := c.nextLabel("if_else")
			endLabel := c.nextLabel("if_end")
			out = append(out, IRStmt{Kind: IRStmtJumpIfZero, Target: elseLabel, Expr: cond, Position: stmt.Pos})
			thenIR, err := c.lowerStatementsToIR(stmt.Then, params, ret, readOnly, false, functions, structs, msgOpcodes, cloneLoweringEnv(env), map[string]struct{}{}, loops)
			if err != nil {
				return nil, err
			}
			out = append(out, thenIR...)
			out = append(out, IRStmt{Kind: IRStmtJump, Target: endLabel, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtLabel, Name: elseLabel, Position: stmt.Pos})
			elseIR, err := c.lowerStatementsToIR(stmt.Else, params, ret, readOnly, false, functions, structs, msgOpcodes, cloneLoweringEnv(env), map[string]struct{}{}, loops)
			if err != nil {
				return nil, err
			}
			out = append(out, elseIR...)
			out = append(out, IRStmt{Kind: IRStmtLabel, Name: endLabel, Position: stmt.Pos})
		case StatementWhile:
			startLabel := c.nextLabel("while_start")
			endLabel := c.nextLabel("while_end")
			out = append(out, IRStmt{Kind: IRStmtLabel, Name: startLabel, Position: stmt.Pos})
			cond, err := lowerExprToIR(stmt.Value, env, functions, map[string]bool{})
			if err != nil {
				return nil, err
			}
			out = append(out, IRStmt{Kind: IRStmtJumpIfZero, Target: endLabel, Expr: cond, Position: stmt.Pos})
			bodyIR, err := c.lowerStatementsToIR(stmt.Then, params, ret, readOnly, false, functions, structs, msgOpcodes, cloneLoweringEnv(env), map[string]struct{}{}, append(loops, loopContext{breakTarget: endLabel, continueTarget: startLabel}))
			if err != nil {
				return nil, err
			}
			out = append(out, bodyIR...)
			out = append(out, IRStmt{Kind: IRStmtJump, Target: startLabel, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtLabel, Name: endLabel, Position: stmt.Pos})
		case StatementDo:
			startLabel := c.nextLabel("do_start")
			continueLabel := c.nextLabel("do_continue")
			endLabel := c.nextLabel("do_end")
			out = append(out, IRStmt{Kind: IRStmtLabel, Name: startLabel, Position: stmt.Pos})
			bodyIR, err := c.lowerStatementsToIR(stmt.Then, params, ret, readOnly, false, functions, structs, msgOpcodes, cloneLoweringEnv(env), map[string]struct{}{}, append(loops, loopContext{breakTarget: endLabel, continueTarget: continueLabel}))
			if err != nil {
				return nil, err
			}
			out = append(out, bodyIR...)
			out = append(out, IRStmt{Kind: IRStmtLabel, Name: continueLabel, Position: stmt.Pos})
			cond, err := lowerExprToIR(stmt.Value, env, functions, map[string]bool{})
			if err != nil {
				return nil, err
			}
			out = append(out, IRStmt{Kind: IRStmtJumpIfZero, Target: endLabel, Expr: cond, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtJump, Target: startLabel, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtLabel, Name: endLabel, Position: stmt.Pos})
		case StatementRepeat:
			count, ok := evalConstU64(stmt.Value, env, functions, map[string]bool{})
			if ok && !statementContainsLoopControl(stmt.Then) {
				// Eager unroll bound: a constant count may be as large as
				// max-uint64, so re-lowering the body `count` times is an
				// unbounded compile-time DoS. Cap total emitted instructions at
				// a generous multiple of the code-size limit -- an unroll past
				// that can never materialize within MaxCodeBytes (each emitted
				// instruction lowers to >= 1 byte) and would be rejected by the
				// post-materialization size check anyway, so reject it up front
				// instead of doing all the work first. The multiplier keeps the
				// cap safely above the IR any valid function (with its 0-byte
				// labels) can hold, so no program that currently compiles
				// changes its output.
				maxUnroll := 8 * int(c.opts.MaxCodeBytes)
				for i := uint64(0); i < count; i++ {
					bodyIR, err := c.lowerStatementsToIR(stmt.Then, params, ret, readOnly, false, functions, structs, msgOpcodes, cloneLoweringEnv(env), map[string]struct{}{}, loops)
					if err != nil {
						return nil, err
					}
					if len(bodyIR) == 0 {
						// Empty body emits nothing however many times it is
						// repeated. The body IR is deterministic in the freshly
						// cloned env, so an empty first iteration is empty for
						// all iterations -- stop now rather than spinning `count`
						// (up to max-uint64) times doing nothing.
						break
					}
					out = append(out, bodyIR...)
					if len(out) > maxUnroll {
						return nil, fail("E_REPEAT_UNROLL", stmt.Pos, fmt.Sprintf("repeat unrolls past the %d-instruction limit; use a smaller constant count or a runtime-counted loop", maxUnroll))
					}
				}
				break
			}
			startLabel := c.nextLabel("repeat_start")
			continueLabel := c.nextLabel("repeat_continue")
			endLabel := c.nextLabel("repeat_end")
			countIR, err := lowerExprToIR(stmt.Value, env, functions, map[string]bool{})
			if err != nil {
				return nil, err
			}
			out = append(out, IRStmt{Kind: IRStmtPushU64, Arg: 0, Expr: countIR, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtLabel, Name: startLabel, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtDup, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtJumpIfZero, Target: endLabel, Position: stmt.Pos})
			bodyIR, err := c.lowerStatementsToIR(stmt.Then, params, ret, readOnly, false, functions, structs, msgOpcodes, cloneLoweringEnv(env), map[string]struct{}{}, append(loops, loopContext{breakTarget: endLabel, continueTarget: continueLabel}))
			if err != nil {
				return nil, err
			}
			out = append(out, bodyIR...)
			out = append(out, IRStmt{Kind: IRStmtLabel, Name: continueLabel, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtPushU64, Arg: 1, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtSub, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtJump, Target: startLabel, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtLabel, Name: endLabel, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtDrop, Position: stmt.Pos})
		case StatementMatch:
			endLabel := c.nextLabel("match_end")
			useMessageMatch := false
			for _, arm := range stmt.Arms {
				if arm.Pattern.Kind == PatternName {
					if _, ok := msgOpcodes[patternTail(arm.Pattern.Name)]; ok {
						useMessageMatch = true
						break
					}
				}
			}
			if useMessageMatch {
				// A message-union match is either over the handler's own
				// top-level incoming message (ctx.Message.Opcode is the
				// authoritative, host-verified discriminant there — every
				// existing handler matches this way) or over some other
				// derived value, e.g. a nested payload pulled out of a field
				// and decoded separately (wrapMessage()/fromChunk()). Those
				// two cases need different discriminant/field-read sources;
				// exprIsHandlerMessage tells them apart. Preserving the
				// ctx.Message.Opcode path exactly for the top-level case is
				// what keeps every existing `match (msg)` handler unchanged.
				topLevelScrutinee := exprIsHandlerMessage(stmt.Value, env)
				var scrutineeIR *IRExpr
				if !topLevelScrutinee {
					var err error
					scrutineeIR, err = lowerExprToIR(stmt.Value, env, functions, map[string]bool{})
					if err != nil {
						return nil, err
					}
				}
				opcodeSource := func(pos Position) *IRExpr {
					if topLevelScrutinee {
						return &IRExpr{Kind: IRExprMsgOpcode, Pos: pos}
					}
					// Reads the wrapMessage()-injected synthetic field off
					// the scrutinee value itself; a scrutinee that was never
					// wrapMessage()-tagged has no such field and this fails
					// at runtime with a clear "field not found"-shaped error
					// rather than silently mismatching.
					return &IRExpr{Kind: IRExprField, Left: scrutineeIR, Text: nestedMessageOpcodeField, Pos: pos}
				}
				fieldSource := func(name string, pos Position) *IRExpr {
					if topLevelScrutinee {
						return &IRExpr{Kind: IRExprMsgField, Text: name, Pos: pos}
					}
					return &IRExpr{Kind: IRExprField, Left: scrutineeIR, Text: name, Pos: pos}
				}
				hasWildcard := false
				for i, arm := range stmt.Arms {
					switch arm.Pattern.Kind {
					case PatternWildcard:
						hasWildcard = true
						armLabel := c.nextLabel("match_wild")
						out = append(out, IRStmt{Kind: IRStmtLabel, Name: armLabel, Position: arm.Pos})
						armIR, err := c.lowerStatementsToIR(arm.Body, params, ret, readOnly, false, functions, structs, msgOpcodes, cloneLoweringEnv(env), map[string]struct{}{}, loops)
						if err != nil {
							return nil, err
						}
						out = append(out, armIR...)
						out = append(out, IRStmt{Kind: IRStmtJump, Target: endLabel, Position: arm.Pos})
					case PatternName:
						opcode, ok := msgOpcodes[patternTail(arm.Pattern.Name)]
						if !ok {
							return nil, fail("E_LOWER_MATCH", arm.Pos, fmt.Sprintf("unknown message variant %q", arm.Pattern.Name))
						}
						armEnv := cloneLoweringEnv(env)
						armScope := map[string]struct{}{}
						armPrelude := make([]IRStmt, 0, len(arm.Pattern.Bindings)*2)
						if st, ok := structs[patternTail(arm.Pattern.Name)]; ok {
							if len(arm.Pattern.Bindings) > 0 && len(arm.Pattern.Bindings) != len(st.Fields) {
								return nil, fail("E_LOWER_MATCH", arm.Pos, fmt.Sprintf("match arm %s expects %d bindings, got %d", arm.Pattern.Name, len(st.Fields), len(arm.Pattern.Bindings)))
							}
							for i, bind := range arm.Pattern.Bindings {
								if _, exists := armScope[bind]; exists {
									return nil, fail("E_LOWER_MATCH", arm.Pos, fmt.Sprintf("duplicate match binding %q", bind))
								}
								binding := localBinding{Slot: armEnv.nextLocalSlot, Type: st.Fields[i].Type, Mutable: false}
								armEnv.nextLocalSlot++
								armEnv.locals[bind] = binding
								armEnv.types[bind] = st.Fields[i].Type
								armScope[bind] = struct{}{}
								armPrelude = append(armPrelude,
									IRStmt{Kind: IRStmtPushU64, Expr: fieldSource(st.Fields[i].Name, arm.Pos), Position: arm.Pos},
									IRStmt{Kind: IRStmtStoreLocal, Slot: binding.Slot, Position: arm.Pos},
								)
							}
						} else if len(arm.Pattern.Bindings) > 0 {
							return nil, fail("E_LOWER_MATCH", arm.Pos, fmt.Sprintf("match arm %s cannot destructure variant without struct fields", arm.Pattern.Name))
						}
						armLabel := c.nextLabel(fmt.Sprintf("match_arm_%d", i))
						cond := &IRExpr{
							Kind:  IRExprEq,
							Left:  opcodeSource(stmt.Pos),
							Right: &IRExpr{Kind: IRExprConstU64, Value: uint64(opcode), Pos: stmt.Pos},
							Pos:   stmt.Pos,
						}
						out = append(out, IRStmt{Kind: IRStmtJumpIfZero, Target: armLabel, Expr: cond, Position: arm.Pos})
						armIR, err := c.lowerStatementsToIR(arm.Body, params, ret, readOnly, false, functions, structs, msgOpcodes, armEnv, armScope, loops)
						if err != nil {
							return nil, err
						}
						out = append(out, armPrelude...)
						out = append(out, armIR...)
						out = append(out, IRStmt{Kind: IRStmtJump, Target: endLabel, Position: arm.Pos})
						out = append(out, IRStmt{Kind: IRStmtLabel, Name: armLabel, Position: arm.Pos})
					default:
						return nil, fail("E_LOWER_MATCH", arm.Pos, "unsupported match pattern")
					}
				}
				if !hasWildcard {
					out = append(out, IRStmt{Kind: IRStmtAbort, Arg: 0xffff, Position: stmt.Pos})
				}
				out = append(out, IRStmt{Kind: IRStmtLabel, Name: endLabel, Position: stmt.Pos})
				break
			}
			tag, ok := evalConstEnum(stmt.Value, env, functions, map[string]bool{})
			var matched []Statement
			if ok {
				for _, arm := range stmt.Arms {
					switch arm.Pattern.Kind {
					case PatternWildcard:
						matched = arm.Body
						goto matchedBranch
					case PatternName:
						if arm.Pattern.Name == tag {
							if len(arm.Pattern.Bindings) > 0 {
								return nil, fail("E_LOWER_MATCH", arm.Pos, "pattern bindings require runtime destructuring, which AVM v1 does not provide")
							}
							matched = arm.Body
							goto matchedBranch
						}
					}
				}
			} else {
				for _, arm := range stmt.Arms {
					if arm.Pattern.Kind == PatternWildcard {
						matched = arm.Body
						goto matchedBranch
					}
				}
				if len(stmt.Arms) > 0 {
					matched = stmt.Arms[0].Body
				}
			}
			if matched == nil {
				return nil, fail("E_LOWER_MATCH", stmt.Pos, fmt.Sprintf("no match arm for enum variant %s", tag))
			}
		matchedBranch:
			branchIR, err := c.lowerStatementsToIR(matched, params, ret, readOnly, false, functions, structs, msgOpcodes, cloneLoweringEnv(env), map[string]struct{}{}, loops)
			if err != nil {
				return nil, err
			}
			out = append(out, branchIR...)
		case StatementFor:
			start, okStart := evalConstU64(stmt.Start, env, functions, map[string]bool{})
			end, okEnd := evalConstU64(stmt.End, env, functions, map[string]bool{})
			if !okStart || !okEnd {
				return nil, fail("E_LOWER_FOR", stmt.Pos, "bounded loop bounds must be compile-time constants")
			}
			if end < start {
				return nil, fail("E_LOWER_FOR", stmt.Pos, "loop end must be >= start")
			}
			if _, exists := scope[stmt.Index]; exists {
				return nil, fail("E_LOWER_FOR", stmt.Pos, fmt.Sprintf("duplicate binding %q in the same scope", stmt.Index))
			}
			bodyEnv := cloneLoweringEnv(env)
			bodyBinding := localBinding{Slot: bodyEnv.nextLocalSlot, Type: TypeRef{Name: "uint64"}, Mutable: false}
			bodyEnv.nextLocalSlot++
			bodyEnv.locals[stmt.Index] = bodyBinding
			bodyEnv.types[stmt.Index] = TypeRef{Name: "uint64"}
			bodyScope := map[string]struct{}{stmt.Index: struct{}{}}
			checkLabel := c.nextLabel("for_check")
			continueLabel := c.nextLabel("for_continue")
			endLabel := c.nextLabel("for_end")
			out = append(out, IRStmt{Kind: IRStmtPushU64, Arg: start, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtStoreLocal, Slot: bodyBinding.Slot, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtLabel, Name: checkLabel, Position: stmt.Pos})
			cond := &IRExpr{
				Kind:  IRExprLt,
				Left:  &IRExpr{Kind: IRExprLocalLoad, Slot: bodyBinding.Slot, Pos: stmt.Pos},
				Right: &IRExpr{Kind: IRExprConstU64, Value: end, Pos: stmt.Pos},
				Pos:   stmt.Pos,
			}
			out = append(out, IRStmt{Kind: IRStmtJumpIfZero, Target: endLabel, Expr: cond, Position: stmt.Pos})
			bodyIR, err := c.lowerStatementsToIR(stmt.Then, params, ret, readOnly, false, functions, structs, msgOpcodes, bodyEnv, bodyScope, append(loops, loopContext{breakTarget: endLabel, continueTarget: continueLabel}))
			if err != nil {
				return nil, err
			}
			out = append(out, bodyIR...)
			out = append(out, IRStmt{Kind: IRStmtLabel, Name: continueLabel, Position: stmt.Pos})
			increment := &IRExpr{
				Kind:  IRExprAdd,
				Left:  &IRExpr{Kind: IRExprLocalLoad, Slot: bodyBinding.Slot, Pos: stmt.Pos},
				Right: &IRExpr{Kind: IRExprConstU64, Value: 1, Pos: stmt.Pos},
				Pos:   stmt.Pos,
			}
			out = append(out, IRStmt{Kind: IRStmtPushU64, Expr: increment, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtStoreLocal, Slot: bodyBinding.Slot, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtJump, Target: checkLabel, Position: stmt.Pos})
			out = append(out, IRStmt{Kind: IRStmtLabel, Name: endLabel, Position: stmt.Pos})
		default:
			return nil, fail("E_LOWER_STMT", stmt.Pos, fmt.Sprintf("unsupported statement %q", stmt.Kind))
		}
	}
	if ensureReturn && (len(out) == 0 || out[len(out)-1].Kind != IRStmtReturn) {
		out = append(out, IRStmt{Kind: IRStmtReturn, Arg: 0})
	}
	return out, nil
}

func (c *Compiler) lowerIREntry(entry IREntry) ([]avm.Instruction, error) {
	var code []avm.Instruction
	type jumpPatch struct {
		index  int
		target string
		pos    Position
	}
	var patches []jumpPatch
	labels := map[string]int{}
	for _, stmt := range entry.Statements {
		switch stmt.Kind {
		case IRStmtTrace:
			code = append(code, avm.Instruction{Op: avm.OpNop, Data: append([]byte(nil), stmt.Data...)})
		case IRStmtLetConst:
		case IRStmtLabel:
			if stmt.Name == "" {
				return nil, fail("E_LOWER_IR", stmt.Position, "label requires a name")
			}
			if _, exists := labels[stmt.Name]; exists {
				return nil, fail("E_LOWER_IR", stmt.Position, fmt.Sprintf("duplicate label %q", stmt.Name))
			}
			labels[stmt.Name] = len(code)
		case IRStmtJump:
			patches = append(patches, jumpPatch{index: len(code), target: stmt.Target, pos: stmt.Position})
			code = append(code, avm.Instruction{Op: avm.OpJump})
		case IRStmtJumpIfZero:
			if stmt.Expr != nil {
				if err := emitIRExpr(stmt.Expr, &code); err != nil {
					return nil, err
				}
			}
			patches = append(patches, jumpPatch{index: len(code), target: stmt.Target, pos: stmt.Position})
			code = append(code, avm.Instruction{Op: avm.OpJumpIfZero})
		case IRStmtPushU64:
			if stmt.Expr != nil {
				if err := emitIRExpr(stmt.Expr, &code); err != nil {
					return nil, err
				}
				break
			}
			code = append(code, avm.Instruction{Op: avm.OpPushU64, Arg: stmt.Arg})
		case IRStmtDup:
			code = append(code, avm.Instruction{Op: avm.OpDup})
		case IRStmtDrop:
			code = append(code, avm.Instruction{Op: avm.OpDrop})
		case IRStmtStoreLocal:
			code = append(code, avm.Instruction{Op: avm.OpStoreLocal, Arg: uint64(stmt.Slot)})
		case IRStmtSub:
			code = append(code, avm.Instruction{Op: avm.OpSub})
		case IRStmtAbort:
			code = append(code, avm.Instruction{Op: avm.OpAbort, Arg: stmt.Arg})
		case IRStmtStoreState:
			if err := emitIRExpr(stmt.Expr, &code); err != nil {
				return nil, err
			}
			code = append(code, avm.Instruction{Op: avm.OpWriteStorage, Data: []byte(stmt.Key)})
		case IRStmtDeleteState:
			code = append(code, avm.Instruction{Op: avm.OpDeleteStorage, Data: []byte(stmt.Key)})
		case IRStmtEmitInternal:
			if err := emitIRExpr(stmt.Expr, &code); err != nil {
				return nil, err
			}
			// The Arg carries the opcode only. The send mode travels in the
			// runtime message value (buildMessage `mode:` field); the VM still
			// honours a legacy mode bitmask in the high 32 bits for artifacts
			// compiled before mode moved into the message.
			code = append(code, avm.Instruction{Op: avm.OpEmitInternal, Arg: uint64(stmt.Opcode), Data: append([]byte(nil), stmt.Data...)})
		case IRStmtScheduleSelf:
			code = append(code, avm.Instruction{Op: avm.OpScheduleSelf, Arg: stmt.Arg, Data: append([]byte(nil), stmt.Data...)})
		case IRStmtReturn:
			if stmt.Expr != nil {
				if err := emitIRExpr(stmt.Expr, &code); err != nil {
					return nil, err
				}
			}
			code = append(code, avm.Instruction{Op: avm.OpReturn, Arg: stmt.Arg})
		default:
			return nil, fail("E_LOWER_IR", stmt.Position, fmt.Sprintf("unsupported IR statement %q", stmt.Kind))
		}
	}
	for _, patch := range patches {
		target, ok := labels[patch.target]
		if !ok {
			return nil, fail("E_LOWER_IR", patch.pos, fmt.Sprintf("unknown label %q", patch.target))
		}
		code[patch.index].Arg = uint64(target)
	}
	return code, nil
}

const refundOpcode uint32 = 0xfffffff0

type loweringEnv struct {
	params         map[string]int
	consts         map[string]constValue
	types          map[string]TypeRef
	locals         map[string]localBinding
	storageAliases map[string]struct{}
	unknowns       map[string]struct{}
	nextLocalSlot  uint32
	// msgOpcodes is the file-wide map of @message(N) struct name -> opcode,
	// threaded through here (rather than as its own lowerExprToIR parameter)
	// so wrapMessage() can resolve an opcode from any expression position,
	// not just the handful of statement-level call sites that already had
	// msgOpcodes in scope. Read-only: never mutated after construction, so
	// sharing the same map across cloned envs is safe.
	msgOpcodes map[string]uint32
	// storageFieldTypes is the contract's @storage struct's field-name ->
	// declared-type map, threaded through the same way as msgOpcodes
	// (read-only, shared by reference across cloned envs). It lets a
	// `<storageAlias>.<field>` read (IRExprStateRead) tag its emitted
	// instruction with the field's declared type, so a fresh/absent field
	// can decode to a type-aware zero value (e.g. an empty map for a
	// Map<K,V> field) instead of the generic uint64(0) fallback. See
	// runtimeValueFromStorage / OpReadStorage in x/aetravm/avm/avm.go.
	storageFieldTypes map[string]TypeRef
}

type localBinding struct {
	Slot    uint32
	Type    TypeRef
	Mutable bool
	// FromHandlerMessage marks a binding whose value chain traces, through
	// zero or more no-op fromSegment/fromChunk/fromState re-tags, back to the
	// current handler's own incoming message (the in/inMsg parameter or its
	// .body/.bouncedBody field). StatementMatch uses this to decide whether a
	// message-union match should keep comparing the host-verified
	// ctx.Message.Opcode (top-level, e.g. `match (msg)` where msg came from
	// in.body) or must instead read the discriminant field off the scrutinee
	// value itself (nested, e.g. a payload extracted from another message's
	// field and decoded separately). See exprIsHandlerMessage.
	FromHandlerMessage bool
}

type loopContext struct {
	breakTarget    string
	continueTarget string
}

func cloneLoweringEnv(in loweringEnv) loweringEnv {
	out := loweringEnv{
		params:            map[string]int{},
		consts:            map[string]constValue{},
		types:             map[string]TypeRef{},
		locals:            map[string]localBinding{},
		storageAliases:    map[string]struct{}{},
		unknowns:          map[string]struct{}{},
		nextLocalSlot:     in.nextLocalSlot,
		msgOpcodes:        in.msgOpcodes,
		storageFieldTypes: in.storageFieldTypes,
	}
	for k, v := range in.params {
		out.params[k] = v
	}
	for k, v := range in.consts {
		out.consts[k] = v
	}
	for k, v := range in.types {
		out.types[k] = v
	}
	for k, v := range in.locals {
		out.locals[k] = v
	}
	for k := range in.storageAliases {
		out.storageAliases[k] = struct{}{}
	}
	for k := range in.unknowns {
		out.unknowns[k] = struct{}{}
	}
	return out
}

// nestedMessageOpcodeField is the synthetic field wrapMessage() injects into
// a message struct literal so a *nested* match (see exprIsHandlerMessage) can
// recover the variant's opcode via the same generic OpReadField mechanism
// normal field access already uses. It contains "$", which the lexer never
// accepts in an identifier (isIdentStart/isIdentPart, lexer.go), so no
// user-declared ATLX field can ever collide with it — no reservation check
// is needed on struct field declarations.
const nestedMessageOpcodeField = "$opcode"

// exprIsHandlerMessage reports whether expr's value, after unwrapping any
// chain of no-op fromSegment/fromChunk/fromState re-tags, traces back to the
// current function's own handler-message parameter (in/inMsg, or its
// .body/.bouncedBody field) rather than to some other derived value such as
// a nested payload extracted from a field and decoded separately with
// wrapMessage()/fromChunk(). StatementMatch relies on this to keep using the
// authoritative ctx.Message.Opcode for every existing top-level
// `match (msg)` handler unchanged, while switching nested matches to a
// value-based field read instead.
func exprIsHandlerMessage(expr Expr, env loweringEnv) bool {
	for expr.Kind == ExprCall && len(expr.Path) >= 2 && len(expr.Args) == 1 {
		switch strings.ToLower(expr.Path[len(expr.Path)-1]) {
		case "fromsegment", "fromchunk", "fromstate":
			expr = expr.Args[0]
			continue
		}
		break
	}
	switch expr.Kind {
	case ExprIdent:
		return identIsHandlerMessage(expr.Text, env)
	case ExprPath:
		if len(expr.Path) == 1 {
			return identIsHandlerMessage(expr.Path[0], env)
		}
		if len(expr.Path) == 2 {
			if _, ok := env.locals[expr.Path[0]]; ok {
				// Field access on a local (e.g. msg.forwardPayload) yields a
				// different, nested value — not a re-reference to the
				// handler's own message.
				return false
			}
			if typ, ok := env.types[expr.Path[0]]; ok {
				switch strings.ToLower(typ.Name) {
				case "inmessage":
					return expr.Path[1] == "body"
				case "inmessagebounced":
					return expr.Path[1] == "bouncedBody" || expr.Path[1] == "body"
				}
			}
		}
	}
	return false
}

func identIsHandlerMessage(name string, env loweringEnv) bool {
	if binding, ok := env.locals[name]; ok {
		return binding.FromHandlerMessage
	}
	if _, ok := env.params[name]; ok {
		if typ, ok := env.types[name]; ok {
			switch strings.ToLower(typ.Name) {
			case "segment", "chunk", "inmessage", "inmessagebounced":
				return true
			}
		}
	}
	return false
}

func isStorageLoadBinding(expr Expr) bool {
	if expr.Kind != ExprCall || len(expr.Path) != 2 {
		return false
	}
	return strings.EqualFold(expr.Path[1], "load")
}

func isFieldLikeType(name string) bool {
	switch canonicalCodecTypeName(name) {
	case "", "bool", "u2", "u4", "u8", "u16", "u32", "u64", "u128", "u256", "uint2", "uint4", "uint8", "uint16", "uint32", "uint64", "uint128", "uint256", "i2", "i4", "i8", "i16", "i32", "i64", "i128", "i256", "int2", "int4", "int8", "int16", "int32", "int64", "int128", "int256", "bytes", "string", "hash32", "address", "coins", "timestamp", "chunk", "code", "null", "map", "dict", "list", "option", "result":
		return false
	default:
		return true
	}
}

type constValueKind string

const (
	constKindU64   constValueKind = "u64"
	constKindBool  constValueKind = "bool"
	constKindNull  constValueKind = "null"
	constKindEnum  constValueKind = "enum"
	constKindText  constValueKind = "text"
	constKindAddr  constValueKind = "addr"
	constKindBytes constValueKind = "bytes"
)

type constValue struct {
	Kind  constValueKind
	U64   uint64
	Bool  bool
	Text  string
	Bytes []byte
	Type  string
}

func constValueType(v constValue) TypeRef {
	switch v.Kind {
	case constKindBool:
		return TypeRef{Name: "bool"}
	case constKindNull:
		return TypeRef{Name: "null"}
	case constKindText:
		return TypeRef{Name: "string"}
	case constKindAddr:
		return TypeRef{Name: "address"}
	case constKindBytes:
		if strings.TrimSpace(v.Type) != "" {
			return TypeRef{Name: v.Type}
		}
		return TypeRef{Name: "bytes"}
	case constKindEnum:
		if strings.TrimSpace(v.Type) != "" {
			return TypeRef{Name: v.Type}
		}
		return TypeRef{Name: "enum"}
	default:
		return TypeRef{Name: "uint64"}
	}
}

// lowerExprArgs lowers a positional argument list to IR in source order,
// preserving order so the emitter can push operands in the sequence the VM
// dispatch pops them (last-pushed-first). Used by the multi-operand byte
// builtins (concat/subBytes/byteAt/toBytesBE).
func lowerExprArgs(args []Expr, env loweringEnv, functions map[string]*FunctionDecl, seen map[string]bool) ([]*IRExpr, error) {
	out := make([]*IRExpr, len(args))
	for i, arg := range args {
		lowered, err := lowerExprToIR(arg, env, functions, seen)
		if err != nil {
			return nil, err
		}
		out[i] = lowered
	}
	return out, nil
}

func lowerExprToIR(expr Expr, env loweringEnv, functions map[string]*FunctionDecl, seen map[string]bool) (*IRExpr, error) {
	switch expr.Kind {
	case ExprNumber:
		v, ok := constU64(expr)
		if !ok {
			return nil, fail("E_LOWER_EXPR", expr.Pos, "invalid uint64 literal")
		}
		return &IRExpr{Kind: IRExprConstU64, Value: v, Pos: expr.Pos}, nil
	case ExprString:
		return &IRExpr{Kind: IRExprConstString, Text: expr.Text, Pos: expr.Pos}, nil
	case ExprBytes:
		return &IRExpr{Kind: IRExprConstBytes, Data: append([]byte(nil), expr.Bytes...), Pos: expr.Pos}, nil
	case ExprBool:
		if expr.Bool {
			return &IRExpr{Kind: IRExprConstU64, Value: 1, Pos: expr.Pos}, nil
		}
		return &IRExpr{Kind: IRExprConstU64, Pos: expr.Pos}, nil
	case ExprIdent:
		if binding, ok := env.locals[expr.Text]; ok {
			return &IRExpr{Kind: IRExprLocalLoad, Slot: binding.Slot, Pos: expr.Pos}, nil
		}
		if v, ok := env.consts[expr.Text]; ok {
			if v.Kind == constKindU64 {
				return &IRExpr{Kind: IRExprConstU64, Value: v.U64, Pos: expr.Pos}, nil
			}
			if v.Kind == constKindBool {
				if v.Bool {
					return &IRExpr{Kind: IRExprConstU64, Value: 1, Pos: expr.Pos}, nil
				}
				return &IRExpr{Kind: IRExprConstU64, Pos: expr.Pos}, nil
			}
			if v.Kind == constKindAddr {
				return &IRExpr{Kind: IRExprConstAddress, Text: v.Text, Pos: expr.Pos}, nil
			}
			if v.Kind == constKindBytes {
				return &IRExpr{Kind: IRExprConstBytes, Data: append([]byte(nil), v.Bytes...), Pos: expr.Pos}, nil
			}
			if v.Kind == constKindText && (strings.EqualFold(v.Type, "hash32") || strings.EqualFold(v.Type, "hash")) {
				decoded, err := hex.DecodeString(strings.TrimSpace(v.Text))
				if err != nil {
					return nil, fail("E_LOWER_IDENT", expr.Pos, "invalid hex hash constant")
				}
				return &IRExpr{Kind: IRExprConstBytes, Data: decoded, Pos: expr.Pos}, nil
			}
		}
		if mode, ok := builtinSendModeValue(expr.Text); ok {
			return &IRExpr{Kind: IRExprConstU64, Value: uint64(mode), Pos: expr.Pos}, nil
		}
		if _, ok := env.unknowns[expr.Text]; ok {
			return &IRExpr{Kind: IRExprConstU64, Pos: expr.Pos}, nil
		}
		switch strings.ToLower(expr.Text) {
		case "opcode":
			return &IRExpr{Kind: IRExprMsgOpcode, Pos: expr.Pos}, nil
		case "query_id":
			return &IRExpr{Kind: IRExprMsgQueryID, Pos: expr.Pos}, nil
		case "block_height":
			return &IRExpr{Kind: IRExprBlockHeight, Pos: expr.Pos}, nil
		case "getaddress":
			return &IRExpr{Kind: IRExprContractAddress, Pos: expr.Pos}, nil
		case "getoriginalbalance":
			return &IRExpr{Kind: IRExprOriginalBalance, Pos: expr.Pos}, nil
		case "getattachedvalue":
			return &IRExpr{Kind: IRExprAttachedValue, Pos: expr.Pos}, nil
		case "now":
			return &IRExpr{Kind: IRExprBlockTimestamp, Pos: expr.Pos}, nil
		case "logicaltime":
			return &IRExpr{Kind: IRExprLogicalTime, Pos: expr.Pos}, nil
		case "currentblocklogicaltime":
			return &IRExpr{Kind: IRExprCurrentBlockLogicalTime, Pos: expr.Pos}, nil
		}
		if idx, ok := env.params[expr.Text]; ok {
			if typ, ok := env.types[expr.Text]; ok {
				switch strings.ToLower(typ.Name) {
				case "segment", "chunk":
					return &IRExpr{Kind: IRExprMsgBody, Pos: expr.Pos}, nil
				}
			}
			// Every other (scalar) parameter is read from the message body as
			// a named field, using the synthetic positional name "arg0",
			// "arg1", … — the same {name,type,value} field-array format
			// already used for message-body field reads generally (see
			// runtimeMessageFieldValue). This lets a getter or entrypoint
			// accept any number of typed arguments, in any position, each
			// decoded with its own declared type — not just a single value
			// forced through the message's numeric query_id.
			return &IRExpr{Kind: IRExprMsgField, Text: fmt.Sprintf("arg%d", idx), Pos: expr.Pos}, nil
		}
		return nil, fail("E_LOWER_IDENT", expr.Pos, fmt.Sprintf("identifier %q is not lowerable", expr.Text))
	case ExprPath:
		if v, ok := builtinBounceModeValuePath(expr.Path); ok {
			return &IRExpr{Kind: IRExprConstU64, Value: v, Pos: expr.Pos}, nil
		}
		if len(expr.Path) == 1 {
			if binding, ok := env.locals[expr.Path[0]]; ok {
				return &IRExpr{Kind: IRExprLocalLoad, Slot: binding.Slot, Pos: expr.Pos}, nil
			}
			if typ, ok := env.types[expr.Path[0]]; ok {
				switch strings.ToLower(typ.Name) {
				case "segment", "chunk":
					return &IRExpr{Kind: IRExprMsgBody, Pos: expr.Pos}, nil
				}
			}
			if _, ok := env.unknowns[expr.Path[0]]; ok {
				return &IRExpr{Kind: IRExprConstU64, Pos: expr.Pos}, nil
			}
		}
		if len(expr.Path) == 2 {
			if expr.Path[0] == "state" {
				return &IRExpr{Kind: IRExprStateRead, Key: expr.Path[1], TypeHint: stateReadTypeHint(env.storageFieldTypes, expr.Path[1]), Pos: expr.Pos}, nil
			}
			if _, ok := env.storageAliases[expr.Path[0]]; ok {
				return &IRExpr{Kind: IRExprStateRead, Key: expr.Path[1], TypeHint: stateReadTypeHint(env.storageFieldTypes, expr.Path[1]), Pos: expr.Pos}, nil
			}
			if binding, ok := env.locals[expr.Path[0]]; ok {
				if isFieldLikeType(binding.Type.Name) {
					return &IRExpr{Kind: IRExprField, Left: &IRExpr{Kind: IRExprLocalLoad, Slot: binding.Slot, Pos: expr.Pos}, Text: expr.Path[1], Pos: expr.Pos}, nil
				}
				return nil, fail("E_LOWER_PATH", expr.Pos, "local field access is not supported in AVM v1")
			}
			if typ, ok := env.types[expr.Path[0]]; ok {
				switch strings.ToLower(typ.Name) {
				case "inmessage":
					switch expr.Path[1] {
					case "sender":
						return &IRExpr{Kind: IRExprMsgSender, Pos: expr.Pos}, nil
					case "senderAddress":
						return &IRExpr{Kind: IRExprMsgSender, Pos: expr.Pos}, nil
					case "value":
						return &IRExpr{Kind: IRExprMsgValue, Pos: expr.Pos}, nil
					case "valueCoins":
						return &IRExpr{Kind: IRExprMsgValue, Pos: expr.Pos}, nil
					case "body":
						return &IRExpr{Kind: IRExprMsgBody, Pos: expr.Pos}, nil
					case "opcode":
						return &IRExpr{Kind: IRExprMsgOpcode, Pos: expr.Pos}, nil
					case "queryId":
						return &IRExpr{Kind: IRExprMsgQueryID, Pos: expr.Pos}, nil
					case "logicalTime":
						return &IRExpr{Kind: IRExprLogicalTime, Pos: expr.Pos}, nil
					case "attachedValue":
						return &IRExpr{Kind: IRExprAttachedValue, Pos: expr.Pos}, nil
					}
				case "inmessagebounced":
					switch expr.Path[1] {
					case "bouncedBody", "body":
						return &IRExpr{Kind: IRExprMsgBody, Pos: expr.Pos}, nil
					}
				case "contractcontext":
					switch expr.Path[1] {
					case "address":
						return &IRExpr{Kind: IRExprMsgSender, Pos: expr.Pos}, nil
					case "balance":
						return &IRExpr{Kind: IRExprMsgValue, Pos: expr.Pos}, nil
					case "data":
						return &IRExpr{Kind: IRExprMsgBody, Pos: expr.Pos}, nil
					}
				default:
					// A struct-typed parameter (including a method's `self`,
					// which is just a parameter named "self") carries its
					// declared type in env.types unconditionally (set in
					// lowerStatementsToIR), but — unlike a local — it isn't in
					// env.locals, so the check above never saw it. Its value
					// lives in the message body as the synthetic positional
					// field "argN" (see the ExprIdent param case), so a field
					// read on it must go through that same arg slot rather
					// than falling to the generic top-level MsgField read
					// below, which would read a same-named top-level message
					// field instead of the parameter's own field.
					if idx, ok := env.params[expr.Path[0]]; ok {
						lowerName := strings.ToLower(typ.Name)
						if lowerName != "segment" && lowerName != "chunk" && isFieldLikeType(typ.Name) {
							argLeft := &IRExpr{Kind: IRExprMsgField, Text: fmt.Sprintf("arg%d", idx), Pos: expr.Pos}
							return &IRExpr{Kind: IRExprField, Left: argLeft, Text: expr.Path[1], Pos: expr.Pos}, nil
						}
					}
				}
				return &IRExpr{Kind: IRExprMsgField, Text: expr.Path[1], Pos: expr.Pos}, nil
			}
			if _, ok := env.unknowns[expr.Path[0]]; ok {
				return &IRExpr{Kind: IRExprConstU64, Pos: expr.Pos}, nil
			}
		}
		if len(expr.Path) >= 3 {
			// Nested struct field chain (3+ segments), e.g. `o.inner.z` or
			// `state.inner.z`/`storageAlias.inner.z`: a plain local/parameter/
			// storage-rooted binding whose OWN declared type is a struct, where
			// every intermediate segment (`inner`, and any further ones) is
			// itself a struct-typed field of the previous segment's type. This
			// generalizes the len==2 IRExprField lowering directly above to
			// arbitrary depth N by chaining OpReadField reads: emitIRExpr's
			// IRExprField case already emits its Left receiver first and its
			// own OpReadField second (recursively), and a nested struct literal
			// already lowers to a nested runtime map value (IRExprStruct emits
			// a fresh OpMapEmpty for every nested field, see emitIRExpr), so
			// repeated OpReadField calls correctly walk down one level at a
			// time — the same mechanism, just applied more than once.
			//
			// This lowering does not re-resolve each intermediate field's type
			// (loweringEnv carries no `structs`/`enums`/`types` decl maps, only
			// the root binding's own declared type) because it doesn't need
			// to: by the time an expression reaches lowering, the whole chain
			// has already been walked and validated field-by-field by the
			// static type-checker (resolvePathType, invoked via inferExprType
			// during typecheck, which always runs before lowering). A chain
			// that resolvePathType would have rejected never reaches here.
			//
			// OUT OF SCOPE (deliberately, not handled by this branch): a chain
			// through a Map.get()-then-unwrap, e.g. `x.get(k)!.field.nested` —
			// a map ENTRY's value being a struct, not a local/storage binding's
			// OWN nested struct field. That shape doesn't even reach this
			// function as an ExprPath: `x.get(k)` parses as ExprCall (see
			// parser.go parsePrimary), and postfix `.field` selectors are not
			// parsed after a call/its trailing `!`, so it is rejected earlier
			// (at parse time, or — if written some other way the grammar does
			// accept — at typecheck) rather than silently accepted here. It
			// remains unsupported; do not conflate the two shapes.
			_, isStorageAlias := env.storageAliases[expr.Path[0]]
			var base *IRExpr
			tailStart := 1
			if expr.Path[0] == "state" || isStorageAlias {
				base = &IRExpr{Kind: IRExprStateRead, Key: expr.Path[1], TypeHint: stateReadTypeHint(env.storageFieldTypes, expr.Path[1]), Pos: expr.Pos}
				tailStart = 2
			} else if binding, ok := env.locals[expr.Path[0]]; ok {
				if !isFieldLikeType(binding.Type.Name) {
					return nil, fail("E_LOWER_PATH", expr.Pos, "local field access is not supported in AVM v1")
				}
				base = &IRExpr{Kind: IRExprLocalLoad, Slot: binding.Slot, Pos: expr.Pos}
			} else if idx, ok := env.params[expr.Path[0]]; ok {
				if typ, ok := env.types[expr.Path[0]]; ok {
					lowerName := strings.ToLower(typ.Name)
					if lowerName != "segment" && lowerName != "chunk" && isFieldLikeType(typ.Name) {
						base = &IRExpr{Kind: IRExprMsgField, Text: fmt.Sprintf("arg%d", idx), Pos: expr.Pos}
					}
				}
			}
			if base != nil {
				result := base
				for _, seg := range expr.Path[tailStart:] {
					result = &IRExpr{Kind: IRExprField, Left: result, Text: seg, Pos: expr.Pos}
				}
				return result, nil
			}
		}
		return nil, fail("E_LOWER_PATH", expr.Pos, "AVM v1 lowering supports only state.<field> reads")
	case ExprNull:
		return &IRExpr{Kind: IRExprNull, Pos: expr.Pos}, nil
	case ExprUnary:
		if expr.Left == nil {
			return nil, fail("E_LOWER_EXPR", expr.Pos, "unary expression is missing operand")
		}
		left, err := lowerExprToIR(*expr.Left, env, functions, seen)
		if err != nil {
			return nil, err
		}
		switch expr.Op {
		case "!":
			return &IRExpr{Kind: IRExprNot, Left: left, Pos: expr.Pos}, nil
		case "-":
			return &IRExpr{Kind: IRExprNeg, Left: left, Pos: expr.Pos}, nil
		case "~":
			return &IRExpr{Kind: IRExprBitNot, Left: left, Pos: expr.Pos}, nil
		default:
			return nil, fail("E_LOWER_UNARY", expr.Pos, fmt.Sprintf("unary %q has no AVM v1 opcode", expr.Op))
		}
	case ExprCall:
		if callNameIs(expr, "address") {
			if len(expr.Args) != 1 || expr.Args[0].Kind != ExprString {
				return nil, fail("E_LOWER_CALL", expr.Pos, "address() requires one string argument")
			}
			addr, err := parseAddressLiteral(expr.Args[0].Text)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprConstAddress, Text: addr, Pos: expr.Pos}, nil
		}
		if len(expr.Path) >= 2 {
			method := strings.ToLower(expr.Path[len(expr.Path)-1])
			receiver := Expr{Kind: ExprPath, Path: append([]string(nil), expr.Path[:len(expr.Path)-1]...), Pos: expr.Pos}
			switch method {
			case "hash":
				if len(expr.Args) != 0 {
					return nil, fail("E_LOWER_CALL", expr.Pos, "hash() takes no arguments")
				}
				left, err := lowerExprToIR(receiver, env, functions, seen)
				if err != nil {
					return nil, err
				}
				return &IRExpr{Kind: IRExprHash, Left: left, Pos: expr.Pos}, nil
			case "bitshash":
				if len(expr.Args) != 0 {
					return nil, fail("E_LOWER_CALL", expr.Pos, "bitsHash() takes no arguments")
				}
				left, err := lowerExprToIR(receiver, env, functions, seen)
				if err != nil {
					return nil, err
				}
				return &IRExpr{Kind: IRExprBitsHash, Left: left, Pos: expr.Pos}, nil
			case "empty":
				if len(expr.Args) != 0 {
					return nil, fail("E_LOWER_CALL", expr.Pos, "empty() takes no arguments")
				}
				return &IRExpr{Kind: IRExprMapEmpty, Pos: expr.Pos}, nil
			case "isempty":
				if len(expr.Args) != 0 {
					return nil, fail("E_LOWER_CALL", expr.Pos, "isEmpty() takes no arguments")
				}
				left, err := lowerExprToIR(receiver, env, functions, seen)
				if err != nil {
					return nil, err
				}
				return &IRExpr{Kind: IRExprIsEmpty, Left: left, Pos: expr.Pos}, nil
			case "skipbouncedprefix":
				if len(expr.Args) != 0 {
					return nil, fail("E_LOWER_CALL", expr.Pos, "skipBouncedPrefix() takes no arguments")
				}
				return lowerExprToIR(receiver, env, functions, seen)
			case "fromsegment", "fromchunk", "fromstate":
				if len(expr.Args) != 1 {
					return nil, fail("E_LOWER_CALL", expr.Pos, fmt.Sprintf("%s() requires one argument", expr.Text))
				}
				return lowerExprToIR(expr.Args[0], env, functions, seen)
			case "fromhex":
				if len(expr.Args) != 1 || expr.Args[0].Kind != ExprString {
					return nil, fail("E_LOWER_CALL", expr.Pos, "fromHex() requires one string literal argument")
				}
				decoded, err := hex.DecodeString(strings.TrimSpace(expr.Args[0].Text))
				if err != nil {
					return nil, fail("E_LOWER_CALL", expr.Pos, "fromHex() argument is not valid hex")
				}
				return &IRExpr{Kind: IRExprConstBytes, Data: decoded, Pos: expr.Pos}, nil
			case "frombase64":
				if len(expr.Args) != 1 || expr.Args[0].Kind != ExprString {
					return nil, fail("E_LOWER_CALL", expr.Pos, "fromBase64() requires one string literal argument")
				}
				decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(expr.Args[0].Text))
				if err != nil {
					return nil, fail("E_LOWER_CALL", expr.Pos, "fromBase64() argument is not valid base64")
				}
				return &IRExpr{Kind: IRExprConstBytes, Data: decoded, Pos: expr.Pos}, nil
			case "len":
				if len(expr.Args) != 0 {
					return nil, fail("E_LOWER_CALL", expr.Pos, "len() takes no arguments")
				}
				left, err := lowerExprToIR(receiver, env, functions, seen)
				if err != nil {
					return nil, err
				}
				return &IRExpr{Kind: IRExprLen, Left: left, Pos: expr.Pos}, nil
			case "get":
				if len(expr.Args) != 1 {
					return nil, fail("E_LOWER_CALL", expr.Pos, "get() requires one argument")
				}
				left, err := lowerExprToIR(receiver, env, functions, seen)
				if err != nil {
					return nil, err
				}
				arg, err := lowerExprToIR(expr.Args[0], env, functions, seen)
				if err != nil {
					return nil, err
				}
				return &IRExpr{Kind: IRExprMapGet, Left: left, Args: []*IRExpr{arg}, Pos: expr.Pos}, nil
			case "set":
				if len(expr.Args) != 2 {
					return nil, fail("E_LOWER_CALL", expr.Pos, "set() requires two arguments")
				}
				left, err := lowerExprToIR(receiver, env, functions, seen)
				if err != nil {
					return nil, err
				}
				key, err := lowerExprToIR(expr.Args[0], env, functions, seen)
				if err != nil {
					return nil, err
				}
				value, err := lowerExprToIR(expr.Args[1], env, functions, seen)
				if err != nil {
					return nil, err
				}
				return &IRExpr{Kind: IRExprMapSet, Left: left, Args: []*IRExpr{key, value}, Pos: expr.Pos}, nil
			case "has":
				if len(expr.Args) != 1 {
					return nil, fail("E_LOWER_CALL", expr.Pos, "has() requires one argument")
				}
				left, err := lowerExprToIR(receiver, env, functions, seen)
				if err != nil {
					return nil, err
				}
				arg, err := lowerExprToIR(expr.Args[0], env, functions, seen)
				if err != nil {
					return nil, err
				}
				return &IRExpr{Kind: IRExprMapHas, Left: left, Args: []*IRExpr{arg}, Pos: expr.Pos}, nil
			case "delete":
				if len(expr.Args) != 1 {
					return nil, fail("E_LOWER_CALL", expr.Pos, "delete() requires one argument")
				}
				left, err := lowerExprToIR(receiver, env, functions, seen)
				if err != nil {
					return nil, err
				}
				arg, err := lowerExprToIR(expr.Args[0], env, functions, seen)
				if err != nil {
					return nil, err
				}
				return &IRExpr{Kind: IRExprMapDelete, Left: left, Args: []*IRExpr{arg}, Pos: expr.Pos}, nil
			case "keys":
				if len(expr.Args) != 1 {
					return nil, fail("E_LOWER_CALL", expr.Pos, "keys() requires one argument")
				}
				left, err := lowerExprToIR(receiver, env, functions, seen)
				if err != nil {
					return nil, err
				}
				arg, err := lowerExprToIR(expr.Args[0], env, functions, seen)
				if err != nil {
					return nil, err
				}
				return &IRExpr{Kind: IRExprMapKeys, Left: left, Args: []*IRExpr{arg}, Pos: expr.Pos}, nil
			case "entries":
				if len(expr.Args) != 1 {
					return nil, fail("E_LOWER_CALL", expr.Pos, "entries() requires one argument")
				}
				left, err := lowerExprToIR(receiver, env, functions, seen)
				if err != nil {
					return nil, err
				}
				arg, err := lowerExprToIR(expr.Args[0], env, functions, seen)
				if err != nil {
					return nil, err
				}
				return &IRExpr{Kind: IRExprMapEntries, Left: left, Args: []*IRExpr{arg}, Pos: expr.Pos}, nil
			case "getdata":
				if len(expr.Args) != 0 {
					return nil, fail("E_LOWER_CALL", expr.Pos, "getData() takes no arguments")
				}
				return &IRExpr{Kind: IRExprStateRead, Key: "", Pos: expr.Pos}, nil
			case "tochunk":
				if len(expr.Args) != 0 {
					return nil, fail("E_LOWER_CALL", expr.Pos, "toChunk() takes no arguments")
				}
				return lowerExprToIR(receiver, env, functions, seen)
			case "setdata", "touch", "save":
				if len(expr.Args) > 1 {
					return nil, fail("E_LOWER_CALL", expr.Pos, fmt.Sprintf("%s() has too many arguments", expr.Text))
				}
				if len(expr.Args) == 1 {
					return lowerExprToIR(expr.Args[0], env, functions, seen)
				}
				if len(expr.Path) >= 2 {
					receiver := Expr{Kind: ExprPath, Path: append([]string(nil), expr.Path[:len(expr.Path)-1]...), Pos: expr.Pos}
					return lowerExprToIR(receiver, env, functions, seen)
				}
				return &IRExpr{Kind: IRExprConstU64, Pos: expr.Pos}, nil
			}
		}
		if strings.EqualFold(expr.Text, "len") {
			if len(expr.Args) != 1 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "len() requires one argument")
			}
			left, err := lowerExprToIR(expr.Args[0], env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprLen, Left: left, Pos: expr.Pos}, nil
		}
		switch strings.ToLower(expr.Text) {
		case "buildmessage":
			if len(expr.Args) != 1 || expr.Args[0].Kind != ExprStruct {
				return nil, fail("E_LOWER_CALL", expr.Pos, "buildMessage() requires one struct literal argument")
			}
			return lowerExprToIR(expr.Args[0], env, functions, seen)
		case "wrapmessage":
			if len(expr.Args) != 1 || expr.Args[0].Kind != ExprStruct || expr.Args[0].Text == "" {
				return nil, fail("E_LOWER_CALL", expr.Pos, "wrapMessage() requires one struct literal argument")
			}
			opcode, ok := env.msgOpcodes[expr.Args[0].Text]
			if !ok {
				return nil, fail("E_LOWER_CALL", expr.Pos, fmt.Sprintf("wrapMessage() requires a @message struct literal argument, got %q", expr.Args[0].Text))
			}
			inner, err := lowerExprToIR(expr.Args[0], env, functions, seen)
			if err != nil {
				return nil, err
			}
			if inner.Kind != IRExprStruct {
				return nil, fail("E_LOWER_CALL", expr.Pos, "wrapMessage() requires a struct literal argument")
			}
			// Carries the variant's opcode inside the value itself (rather
			// than only in the envelope, like buildMessage's own opcode
			// field) so a nested match — one whose scrutinee is not the
			// handler's own top-level message — can recover the
			// discriminant via a plain field read. See nestedMessageOpcodeField
			// and the StatementMatch codegen in lowerStatementsToIR.
			inner.Fields = append(inner.Fields, IRStructField{
				Name: nestedMessageOpcodeField,
				Expr: &IRExpr{Kind: IRExprConstU64, Value: uint64(opcode), Pos: expr.Pos},
			})
			return inner, nil
		case "hash":
			if len(expr.Args) != 1 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "hash() requires one argument")
			}
			left, err := lowerExprToIR(expr.Args[0], env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprHash, Left: left, Pos: expr.Pos}, nil
		case "sha256", "keccak256", "ripemd160", "sha512", "blake2b":
			// Byte-exact hashes over the raw operand bytes. Distinct from hash()
			// (IRExprHash / OpHash), which is the BLAKE3 chunk-tree root over a
			// tagged canonical encoding.
			if len(expr.Args) != 1 {
				return nil, fail("E_LOWER_CALL", expr.Pos, expr.Text+"() requires one argument")
			}
			left, err := lowerExprToIR(expr.Args[0], env, functions, seen)
			if err != nil {
				return nil, err
			}
			kind := IRExprSha256
			switch strings.ToLower(expr.Text) {
			case "sha256":
				kind = IRExprSha256
			case "keccak256":
				kind = IRExprKeccak256
			case "ripemd160":
				kind = IRExprRipemd160
			case "sha512":
				kind = IRExprSha512
			case "blake2b":
				kind = IRExprBlake2b
			}
			return &IRExpr{Kind: kind, Left: left, Pos: expr.Pos}, nil
		case "concat":
			if len(expr.Args) != 2 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "concat() requires two arguments")
			}
			args, err := lowerExprArgs(expr.Args, env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprConcat, Args: args, Pos: expr.Pos}, nil
		case "subbytes":
			if len(expr.Args) != 3 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "subBytes() requires three arguments")
			}
			args, err := lowerExprArgs(expr.Args, env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprSlice, Args: args, Pos: expr.Pos}, nil
		case "byteat":
			if len(expr.Args) != 2 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "byteAt() requires two arguments")
			}
			args, err := lowerExprArgs(expr.Args, env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprByteAt, Args: args, Pos: expr.Pos}, nil
		case "tobytesbe":
			if len(expr.Args) != 2 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "toBytesBE() requires two arguments")
			}
			args, err := lowerExprArgs(expr.Args, env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprToBytesBE, Args: args, Pos: expr.Pos}, nil
		case "frombytesbe":
			if len(expr.Args) != 1 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "fromBytesBE() requires one argument")
			}
			left, err := lowerExprToIR(expr.Args[0], env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprFromBytesBE, Left: left, Pos: expr.Pos}, nil
		case "address":
			if len(expr.Args) != 1 || expr.Args[0].Kind != ExprString {
				return nil, fail("E_LOWER_CALL", expr.Pos, "address() requires one string argument")
			}
			addr, err := parseAddressLiteral(expr.Args[0].Text)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprConstAddress, Text: addr, Pos: expr.Pos}, nil
		case "getaddress":
			if len(expr.Args) != 0 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "getAddress() takes no arguments")
			}
			return &IRExpr{Kind: IRExprContractAddress, Pos: expr.Pos}, nil
		case "getoriginalbalance":
			if len(expr.Args) != 0 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "getOriginalBalance() takes no arguments")
			}
			return &IRExpr{Kind: IRExprOriginalBalance, Pos: expr.Pos}, nil
		case "getattachedvalue":
			if len(expr.Args) != 0 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "getAttachedValue() takes no arguments")
			}
			return &IRExpr{Kind: IRExprAttachedValue, Pos: expr.Pos}, nil
		case "now":
			if len(expr.Args) != 0 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "now() takes no arguments")
			}
			return &IRExpr{Kind: IRExprBlockTimestamp, Pos: expr.Pos}, nil
		case "logicaltime":
			if len(expr.Args) != 0 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "logicalTime() takes no arguments")
			}
			return &IRExpr{Kind: IRExprLogicalTime, Pos: expr.Pos}, nil
		case "currentblocklogicaltime":
			if len(expr.Args) != 0 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "currentBlockLogicalTime() takes no arguments")
			}
			return &IRExpr{Kind: IRExprCurrentBlockLogicalTime, Pos: expr.Pos}, nil
		case "issignaturevalid", "verifysignature":
			if len(expr.Args) != 3 {
				return nil, fail("E_LOWER_CALL", expr.Pos, expr.Text+"() requires three arguments")
			}
			data, err := lowerExprToIR(expr.Args[0], env, functions, seen)
			if err != nil {
				return nil, err
			}
			signature, err := lowerExprToIR(expr.Args[1], env, functions, seen)
			if err != nil {
				return nil, err
			}
			publicKey, err := lowerExprToIR(expr.Args[2], env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprSignatureVerify, Args: []*IRExpr{data, signature, publicKey}, Pos: expr.Pos}, nil
		case "muldiv", "muldivroundup", "muldivnearest", "muldivfloor", "muldivceil":
			if len(expr.Args) != 3 {
				return nil, fail("E_LOWER_CALL", expr.Pos, expr.Text+"() requires three arguments")
			}
			args, err := lowerExprArgs(expr.Args, env, functions, seen)
			if err != nil {
				return nil, err
			}
			kind := IRExprMulDiv
			switch {
			case strings.EqualFold(expr.Text, "muldivroundup"), strings.EqualFold(expr.Text, "muldivceil"):
				kind = IRExprMulDivRoundUp
			case strings.EqualFold(expr.Text, "muldivnearest"):
				kind = IRExprMulDivNearest
			}
			return &IRExpr{Kind: kind, Args: args, Pos: expr.Pos}, nil
		case "isqrt":
			if len(expr.Args) != 1 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "isqrt() requires one argument")
			}
			arg, err := lowerExprToIR(expr.Args[0], env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprIsqrt, Args: []*IRExpr{arg}, Pos: expr.Pos}, nil
		case "mulcmp":
			if len(expr.Args) != 4 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "mulCmp() requires four arguments")
			}
			args, err := lowerExprArgs(expr.Args, env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprMulCmp, Args: args, Pos: expr.Pos}, nil
		case "muldivsigned":
			if len(expr.Args) != 3 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "mulDivSigned() requires three arguments")
			}
			args, err := lowerExprArgs(expr.Args, env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprMulDivSigned, Args: args, Pos: expr.Pos}, nil
		case "touint128":
			if len(expr.Args) != 1 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "toUint128() requires one argument")
			}
			arg, err := lowerExprToIR(expr.Args[0], env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprNarrowToUint128, Args: []*IRExpr{arg}, Pos: expr.Pos}, nil
		case "toint128":
			if len(expr.Args) != 1 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "toInt128() requires one argument")
			}
			arg, err := lowerExprToIR(expr.Args[0], env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprNarrowToInt128, Args: []*IRExpr{arg}, Pos: expr.Pos}, nil
		case "toint256":
			if len(expr.Args) != 1 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "toInt256() requires one argument")
			}
			arg, err := lowerExprToIR(expr.Args[0], env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprNarrowToInt256, Args: []*IRExpr{arg}, Pos: expr.Pos}, nil
		case "verifysecp256k1":
			if len(expr.Args) != 3 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "verifySecp256k1() requires three arguments")
			}
			args, err := lowerExprArgs(expr.Args, env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprVerifySecp256k1, Args: args, Pos: expr.Pos}, nil
		case "ecrecover":
			if len(expr.Args) != 2 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "ecrecover() requires two arguments")
			}
			args, err := lowerExprArgs(expr.Args, env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprEcrecover, Args: args, Pos: expr.Pos}, nil
		case "bn254g1add":
			if len(expr.Args) != 2 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "bn254G1Add() requires two arguments")
			}
			args, err := lowerExprArgs(expr.Args, env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprBn254G1Add, Args: args, Pos: expr.Pos}, nil
		case "bn254g1scalarmul":
			if len(expr.Args) != 2 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "bn254G1ScalarMul() requires two arguments")
			}
			args, err := lowerExprArgs(expr.Args, env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprBn254G1ScalarMul, Args: args, Pos: expr.Pos}, nil
		case "bn254g1isoncurve":
			if len(expr.Args) != 1 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "bn254G1IsOnCurve() requires one argument")
			}
			arg, err := lowerExprToIR(expr.Args[0], env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprBn254G1IsOnCurve, Args: []*IRExpr{arg}, Pos: expr.Pos}, nil
		case "bn254g2add":
			if len(expr.Args) != 2 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "bn254G2Add() requires two arguments")
			}
			args, err := lowerExprArgs(expr.Args, env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprBn254G2Add, Args: args, Pos: expr.Pos}, nil
		case "bn254g2scalarmul":
			if len(expr.Args) != 2 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "bn254G2ScalarMul() requires two arguments")
			}
			args, err := lowerExprArgs(expr.Args, env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprBn254G2ScalarMul, Args: args, Pos: expr.Pos}, nil
		case "bn254pairingcheck":
			if len(expr.Args) != 3 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "bn254PairingCheck() requires three arguments")
			}
			args, err := lowerExprArgs(expr.Args, env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprBn254PairingCheck, Args: args, Pos: expr.Pos}, nil
		case "poseidon2bn254":
			if len(expr.Args) != 2 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "poseidon2Bn254() requires two arguments")
			}
			args, err := lowerExprArgs(expr.Args, env, functions, seen)
			if err != nil {
				return nil, err
			}
			return &IRExpr{Kind: IRExprPoseidon2Bn254, Args: args, Pos: expr.Pos}, nil
		case "counterfactualaddress", "autodeployaddress":
			if len(expr.Args) != 1 || expr.Args[0].Kind != ExprStruct {
				return nil, fail("E_LOWER_CALL", expr.Pos, expr.Text+"() requires one struct literal argument")
			}
			left, err := lowerExprToIR(expr.Args[0], env, functions, seen)
			if err != nil {
				return nil, err
			}
			kind := IRExprCounterfactualAddress
			if strings.EqualFold(expr.Text, "autodeployaddress") {
				kind = IRExprAutoDeployAddress
			}
			return &IRExpr{Kind: kind, Left: left, Pos: expr.Pos}, nil
		case "getbalance":
			if len(expr.Args) != 0 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "getBalance() takes no arguments")
			}
			return &IRExpr{Kind: IRExprOriginalBalance, Pos: expr.Pos}, nil
		case "random":
			// random() lowers to OpReadRandom, the deterministic block
			// randomness beacon (SHA256 over previous state root, block hash,
			// message hash, and a per-call domain). It is NOT process entropy:
			// the forbidden OpRandom capability remains banned. All validators
			// derive identical values, and successive calls within one execution
			// are domain-separated so they differ.
			if len(expr.Args) != 0 {
				return nil, fail("E_LOWER_CALL", expr.Pos, "random() takes no arguments")
			}
			return &IRExpr{Kind: IRExprRandom, Pos: expr.Pos}, nil
		}
		value, ok := evalConstExpr(expr, env, functions, seen)
		if !ok {
			if inlined, handled, err := tryInlineUserFunctionCall(expr, env, functions, seen); handled {
				return inlined, err
			}
			return nil, fail("E_LOWER_CALL", expr.Pos, fmt.Sprintf("call %q cannot be lowered by AVM v1", callDisplayName(expr)))
		}
		switch value.Kind {
		case constKindU64:
			return &IRExpr{Kind: IRExprConstU64, Value: value.U64, Pos: expr.Pos}, nil
		case constKindBool:
			if value.Bool {
				return &IRExpr{Kind: IRExprConstU64, Value: 1, Pos: expr.Pos}, nil
			}
			return &IRExpr{Kind: IRExprConstU64, Pos: expr.Pos}, nil
		case constKindBytes:
			return &IRExpr{Kind: IRExprConstBytes, Data: append([]byte(nil), value.Bytes...), Pos: expr.Pos}, nil
		case constKindAddr:
			return &IRExpr{Kind: IRExprConstAddress, Text: value.Text, Pos: expr.Pos}, nil
		case constKindText:
			if strings.EqualFold(value.Type, "hash32") || strings.EqualFold(value.Type, "hash") {
				decoded, err := hex.DecodeString(strings.TrimSpace(value.Text))
				if err != nil {
					return nil, fail("E_LOWER_CALL", expr.Pos, "invalid hex hash constant")
				}
				return &IRExpr{Kind: IRExprConstBytes, Data: decoded, Pos: expr.Pos}, nil
			}
			return &IRExpr{Kind: IRExprConstString, Text: value.Text, Pos: expr.Pos}, nil
		default:
			return nil, fail("E_LOWER_CALL", expr.Pos, "only numeric/boolean constant calls can be lowered")
		}
	case ExprStruct:
		fields := make([]IRStructField, 0, len(expr.Fields))
		for _, field := range expr.Fields {
			value, err := lowerExprToIR(field.Value, env, functions, seen)
			if err != nil {
				return nil, err
			}
			fields = append(fields, IRStructField{Name: field.Name, Expr: value})
		}
		return &IRExpr{Kind: IRExprStruct, Text: expr.Text, Fields: fields, Pos: expr.Pos}, nil
	case ExprBinary:
		left, err := lowerExprToIR(*expr.Left, env, functions, seen)
		if err != nil {
			return nil, err
		}
		right, err := lowerExprToIR(*expr.Right, env, functions, seen)
		if err != nil {
			return nil, err
		}
		switch expr.Op {
		case "+":
			return &IRExpr{Kind: IRExprAdd, Left: left, Right: right, Pos: expr.Pos}, nil
		case "-":
			return &IRExpr{Kind: IRExprSub, Left: left, Right: right, Pos: expr.Pos}, nil
		case "*":
			return &IRExpr{Kind: IRExprMul, Left: left, Right: right, Pos: expr.Pos}, nil
		case "/":
			return &IRExpr{Kind: IRExprDiv, Left: left, Right: right, Pos: expr.Pos}, nil
		case "%":
			return &IRExpr{Kind: IRExprMod, Left: left, Right: right, Pos: expr.Pos}, nil
		case "<<":
			return &IRExpr{Kind: IRExprShl, Left: left, Right: right, Pos: expr.Pos}, nil
		case ">>":
			return &IRExpr{Kind: IRExprShr, Left: left, Right: right, Pos: expr.Pos}, nil
		case "&":
			return &IRExpr{Kind: IRExprBitAnd, Left: left, Right: right, Pos: expr.Pos}, nil
		case "|":
			return &IRExpr{Kind: IRExprBitOr, Left: left, Right: right, Pos: expr.Pos}, nil
		case "^":
			return &IRExpr{Kind: IRExprBitXor, Left: left, Right: right, Pos: expr.Pos}, nil
		case "??":
			return &IRExpr{Kind: IRExprCoalesce, Left: left, Right: right, Pos: expr.Pos}, nil
		default:
			return nil, fail("E_LOWER_BINARY", expr.Pos, fmt.Sprintf("binary %q has no AVM v1 opcode", expr.Op))
		}
	case ExprCompare:
		left, err := lowerExprToIR(*expr.Left, env, functions, seen)
		if err != nil {
			return nil, err
		}
		right, err := lowerExprToIR(*expr.Right, env, functions, seen)
		if err != nil {
			return nil, err
		}
		switch expr.Op {
		case "==":
			return &IRExpr{Kind: IRExprEq, Left: left, Right: right, Pos: expr.Pos}, nil
		case "!=":
			return &IRExpr{Kind: IRExprNe, Left: left, Right: right, Pos: expr.Pos}, nil
		case "<":
			return &IRExpr{Kind: IRExprLt, Left: left, Right: right, Pos: expr.Pos}, nil
		case "<=":
			return &IRExpr{Kind: IRExprLe, Left: left, Right: right, Pos: expr.Pos}, nil
		case ">":
			return &IRExpr{Kind: IRExprGt, Left: left, Right: right, Pos: expr.Pos}, nil
		case ">=":
			return &IRExpr{Kind: IRExprGe, Left: left, Right: right, Pos: expr.Pos}, nil
		case "<=>":
			return &IRExpr{Kind: IRExprCompare, Text: expr.Op, Left: left, Right: right, Pos: expr.Pos}, nil
		default:
			return nil, fail("E_LOWER_COMPARE", expr.Pos, fmt.Sprintf("comparison %q has no AVM opcode", expr.Op))
		}
	case ExprLogic:
		left, err := lowerExprToIR(*expr.Left, env, functions, seen)
		if err != nil {
			return nil, err
		}
		right, err := lowerExprToIR(*expr.Right, env, functions, seen)
		if err != nil {
			return nil, err
		}
		switch expr.Op {
		case "&&":
			return &IRExpr{Kind: IRExprAnd, Left: left, Right: right, Pos: expr.Pos}, nil
		case "||":
			return &IRExpr{Kind: IRExprOr, Left: left, Right: right, Pos: expr.Pos}, nil
		default:
			return nil, fail("E_LOWER_LOGIC", expr.Pos, fmt.Sprintf("logic %q has no AVM opcode", expr.Op))
		}
	case ExprTry:
		value, ok := evalConstExpr(expr, env, functions, seen)
		if !ok {
			return nil, fail("E_LOWER_EXPR", expr.Pos, "try expressions must be compile-time constant on AVM v1")
		}
		if value.Kind == constKindBool && value.Bool {
			return &IRExpr{Kind: IRExprConstU64, Value: 1, Pos: expr.Pos}, nil
		}
		return &IRExpr{Kind: IRExprConstU64, Pos: expr.Pos}, nil
	case ExprTernary:
		if expr.Left == nil || expr.Right == nil || expr.Else == nil {
			return nil, fail("E_LOWER_EXPR", expr.Pos, "ternary expression is incomplete")
		}
		cond, err := lowerExprToIR(*expr.Left, env, functions, seen)
		if err != nil {
			return nil, err
		}
		thenExpr, err := lowerExprToIR(*expr.Right, env, functions, seen)
		if err != nil {
			return nil, err
		}
		elseExpr, err := lowerExprToIR(*expr.Else, env, functions, seen)
		if err != nil {
			return nil, err
		}
		return &IRExpr{Kind: IRExprTernary, Left: cond, Right: thenExpr, Else: elseExpr, Pos: expr.Pos}, nil
	default:
		return nil, fail("E_LOWER_EXPR", expr.Pos, fmt.Sprintf("expression %q is ABI-only and cannot be executed by AVM v1", expr.Kind))
	}
}

func lowerBuildMessageExpr(expr Expr, env loweringEnv, functions map[string]*FunctionDecl, msgOpcodes map[string]uint32) (*IRExpr, error) {
	if expr.Kind != ExprCall || !strings.EqualFold(expr.Text, "buildMessage") || len(expr.Args) != 1 || expr.Args[0].Kind != ExprStruct {
		return nil, fail("E_LOWER_CALL", expr.Pos, "buildMessage() requires one struct literal argument")
	}
	envelope := expr.Args[0]
	fields := make([]IRStructField, 0, len(envelope.Fields)+1)
	needsOpcode := uint32(0)
	hasOpcode := false
	for _, field := range envelope.Fields {
		value, err := lowerExprToIR(field.Value, env, functions, map[string]bool{})
		if err != nil {
			return nil, err
		}
		fields = append(fields, IRStructField{Name: field.Name, Expr: value})
		if strings.EqualFold(field.Name, "opcode") {
			hasOpcode = true
		}
		if strings.EqualFold(field.Name, "body") && field.Value.Kind == ExprStruct && field.Value.Text != "" {
			if opcode, ok := msgOpcodes[field.Value.Text]; ok {
				needsOpcode = opcode
			}
		}
	}
	if needsOpcode != 0 && !hasOpcode {
		fields = append(fields, IRStructField{
			Name: "opcode",
			Expr: &IRExpr{Kind: IRExprConstU64, Value: uint64(needsOpcode), Pos: expr.Pos},
		})
	}
	return &IRExpr{Kind: IRExprStruct, Text: "MessageEnvelope", Fields: fields, Pos: expr.Pos}, nil
}

func emitIRExpr(expr *IRExpr, code *[]avm.Instruction) error {
	if expr == nil {
		return nil
	}
	switch expr.Kind {
	case IRExprConstU64:
		*code = append(*code, avm.Instruction{Op: avm.OpPushU64, Arg: expr.Value})
	case IRExprConstString:
		*code = append(*code, avm.Instruction{Op: avm.OpPushString, Data: []byte(expr.Text)})
	case IRExprConstAddress:
		*code = append(*code, avm.Instruction{Op: avm.OpPushAddress, Data: []byte(expr.Text)})
	case IRExprConstBytes:
		*code = append(*code, avm.Instruction{Op: avm.OpPushBytes, Data: append([]byte(nil), expr.Data...)})
	case IRExprLocalLoad:
		*code = append(*code, avm.Instruction{Op: avm.OpLoadLocal, Arg: uint64(expr.Slot)})
	case IRExprNull:
		*code = append(*code, avm.Instruction{Op: avm.OpPushNull})
	case IRExprStateRead:
		var hint uint64
		if expr.TypeHint == "map" {
			hint = avm.StateReadHintMap
		}
		*code = append(*code, avm.Instruction{Op: avm.OpReadStorage, Arg: hint, Data: []byte(expr.Key)})
	case IRExprAdd, IRExprSub, IRExprMul, IRExprDiv, IRExprMod, IRExprShl, IRExprShr, IRExprBitAnd, IRExprBitOr, IRExprBitXor:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		if err := emitIRExpr(expr.Right, code); err != nil {
			return err
		}
		op := avm.OpAdd
		switch expr.Kind {
		case IRExprAdd:
			op = avm.OpAdd
		case IRExprSub:
			op = avm.OpSub
		case IRExprMul:
			op = avm.OpMul
		case IRExprDiv:
			op = avm.OpDiv
		case IRExprMod:
			op = avm.OpMod
		case IRExprShl:
			op = avm.OpShl
		case IRExprShr:
			op = avm.OpShr
		case IRExprBitAnd:
			op = avm.OpBitAnd
		case IRExprBitOr:
			op = avm.OpBitOr
		case IRExprBitXor:
			op = avm.OpBitXor
		}
		*code = append(*code, avm.Instruction{Op: op})
	case IRExprEq, IRExprNe, IRExprLt, IRExprLe, IRExprGt, IRExprGe, IRExprCompare, IRExprAnd, IRExprOr:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		if err := emitIRExpr(expr.Right, code); err != nil {
			return err
		}
		op := avm.OpEq
		switch expr.Kind {
		case IRExprEq:
			op = avm.OpEq
		case IRExprNe:
			op = avm.OpNe
		case IRExprLt:
			op = avm.OpLt
		case IRExprLe:
			op = avm.OpLe
		case IRExprGt:
			op = avm.OpGt
		case IRExprGe:
			op = avm.OpGe
		case IRExprCompare:
			op = avm.OpCmp
		case IRExprAnd:
			op = avm.OpAnd
		case IRExprOr:
			op = avm.OpOr
		}
		*code = append(*code, avm.Instruction{Op: op})
	case IRExprNot:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpNot})
	case IRExprNeg:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpNeg})
	case IRExprBitNot:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpBitNot})
	case IRExprCoalesce:
		if expr.Left == nil || expr.Right == nil {
			return fail("E_LOWER_EXPR", expr.Pos, "coalesce expression is incomplete")
		}
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpDup})
		*code = append(*code, avm.Instruction{Op: avm.OpIsEmpty})
		jumpToKeepLeft := len(*code)
		*code = append(*code, avm.Instruction{Op: avm.OpJumpIfZero, Arg: 0})
		*code = append(*code, avm.Instruction{Op: avm.OpDrop})
		if err := emitIRExpr(expr.Right, code); err != nil {
			return err
		}
		patchJumpTarget(*code, jumpToKeepLeft, len(*code))
	case IRExprTernary:
		if expr.Left == nil || expr.Right == nil || expr.Else == nil {
			return fail("E_LOWER_EXPR", expr.Pos, "ternary expression is incomplete")
		}
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		condJump := len(*code)
		*code = append(*code, avm.Instruction{Op: avm.OpJumpIfZero, Arg: 0})
		if err := emitIRExpr(expr.Right, code); err != nil {
			return err
		}
		endJump := len(*code)
		*code = append(*code, avm.Instruction{Op: avm.OpJump, Arg: 0})
		patchJumpTarget(*code, condJump, len(*code))
		if err := emitIRExpr(expr.Else, code); err != nil {
			return err
		}
		patchJumpTarget(*code, endJump, len(*code))
	case IRExprMsgOpcode:
		*code = append(*code, avm.Instruction{Op: avm.OpReadMsgOpcode})
	case IRExprMsgQueryID:
		*code = append(*code, avm.Instruction{Op: avm.OpReadMsgQueryID})
	case IRExprMsgSender:
		*code = append(*code, avm.Instruction{Op: avm.OpReadMsgSender})
	case IRExprMsgValue:
		*code = append(*code, avm.Instruction{Op: avm.OpReadMsgValue})
	case IRExprMsgBody:
		*code = append(*code, avm.Instruction{Op: avm.OpReadMsgBody})
	case IRExprMsgField:
		*code = append(*code, avm.Instruction{Op: avm.OpReadMsgField, Data: []byte(expr.Text)})
	case IRExprField:
		if expr.Left == nil {
			return fail("E_LOWER_IR", expr.Pos, "field expression is missing receiver")
		}
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpReadField, Data: []byte(expr.Text)})
	case IRExprIsEmpty:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpIsEmpty})
	case IRExprBlockHeight:
		*code = append(*code, avm.Instruction{Op: avm.OpReadBlock})
	case IRExprCurrentBlockLogicalTime:
		*code = append(*code, avm.Instruction{Op: avm.OpReadCurrentBlockLogicalTime})
	case IRExprHash, IRExprBitsHash:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpHash})
	case IRExprSha256, IRExprKeccak256, IRExprRipemd160, IRExprSha512, IRExprBlake2b:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		op := avm.OpSha256
		switch expr.Kind {
		case IRExprSha256:
			op = avm.OpSha256
		case IRExprKeccak256:
			op = avm.OpKeccak256
		case IRExprRipemd160:
			op = avm.OpRipemd160
		case IRExprSha512:
			op = avm.OpSha512
		case IRExprBlake2b:
			op = avm.OpBlake2b
		}
		*code = append(*code, avm.Instruction{Op: op})
	case IRExprConcat, IRExprSlice, IRExprByteAt, IRExprToBytesBE:
		// Operands are pushed in source order; the VM dispatch pops them
		// last-pushed-first (e.g. subBytes pops len, start, b).
		for _, arg := range expr.Args {
			if err := emitIRExpr(arg, code); err != nil {
				return err
			}
		}
		op := avm.OpConcat
		switch expr.Kind {
		case IRExprConcat:
			op = avm.OpConcat
		case IRExprSlice:
			op = avm.OpSlice
		case IRExprByteAt:
			op = avm.OpByteAt
		case IRExprToBytesBE:
			op = avm.OpToBytesBE
		}
		*code = append(*code, avm.Instruction{Op: op})
	case IRExprFromBytesBE:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpFromBytesBE})
	case IRExprSignatureVerify:
		if len(expr.Args) != 3 {
			return fail("E_LOWER_EXPR", expr.Pos, "signature verify expects exactly three arguments")
		}
		if err := emitIRExpr(expr.Args[0], code); err != nil {
			return err
		}
		if err := emitIRExpr(expr.Args[1], code); err != nil {
			return err
		}
		if err := emitIRExpr(expr.Args[2], code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpVerifySignature})
	case IRExprMulDiv, IRExprMulDivRoundUp, IRExprMulDivNearest, IRExprVerifySecp256k1, IRExprEcrecover, IRExprIsqrt, IRExprMulCmp, IRExprMulDivSigned, IRExprNarrowToUint128, IRExprNarrowToInt128, IRExprNarrowToInt256:
		// Operands are pushed in source order; the VM dispatch pops them
		// last-pushed-first (mulDiv/mulDivNearest pop c, b, a; verifySecp256k1
		// pops pubkey, sig, msgHash; ecrecover pops sig, msgHash; mulCmp pops
		// d, c, b, a; mulDivSigned pops c, b, a; the narrowing casts pop their
		// single operand).
		for _, arg := range expr.Args {
			if err := emitIRExpr(arg, code); err != nil {
				return err
			}
		}
		op := avm.OpMulDiv
		switch expr.Kind {
		case IRExprMulDiv:
			op = avm.OpMulDiv
		case IRExprMulDivRoundUp:
			op = avm.OpMulDivRoundUp
		case IRExprMulDivNearest:
			op = avm.OpMulDivNearest
		case IRExprVerifySecp256k1:
			op = avm.OpVerifySecp256k1
		case IRExprEcrecover:
			op = avm.OpEcrecover
		case IRExprIsqrt:
			op = avm.OpIsqrt
		case IRExprMulCmp:
			op = avm.OpMulCmp
		case IRExprMulDivSigned:
			op = avm.OpMulDivSigned
		case IRExprNarrowToUint128:
			op = avm.OpNarrowToUint128
		case IRExprNarrowToInt128:
			op = avm.OpNarrowToInt128
		case IRExprNarrowToInt256:
			op = avm.OpNarrowToInt256
		}
		*code = append(*code, avm.Instruction{Op: op})
	case IRExprBn254G1Add, IRExprBn254G1ScalarMul, IRExprBn254G1IsOnCurve, IRExprBn254G2Add, IRExprBn254G2ScalarMul, IRExprBn254PairingCheck, IRExprPoseidon2Bn254:
		// Operands are pushed in source order; the VM dispatch pops them
		// last-pushed-first (G1Add/G2Add pop b, a; G1ScalarMul/G2ScalarMul pop
		// scalar, point; PairingCheck pops k, g2s, g1s; Poseidon2Bn254 pops n,
		// data) -- see each opcode's own doc comment in avm.go.
		for _, arg := range expr.Args {
			if err := emitIRExpr(arg, code); err != nil {
				return err
			}
		}
		op := avm.OpBn254G1Add
		switch expr.Kind {
		case IRExprBn254G1Add:
			op = avm.OpBn254G1Add
		case IRExprBn254G1ScalarMul:
			op = avm.OpBn254G1ScalarMul
		case IRExprBn254G1IsOnCurve:
			op = avm.OpBn254G1IsOnCurve
		case IRExprBn254G2Add:
			op = avm.OpBn254G2Add
		case IRExprBn254G2ScalarMul:
			op = avm.OpBn254G2ScalarMul
		case IRExprBn254PairingCheck:
			op = avm.OpBn254PairingCheck
		case IRExprPoseidon2Bn254:
			op = avm.OpPoseidon2Bn254
		}
		*code = append(*code, avm.Instruction{Op: op})
	case IRExprCoinsCast:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpCastCoins})
	case IRExprStruct:
		*code = append(*code, avm.Instruction{Op: avm.OpMapEmpty})
		for _, field := range expr.Fields {
			*code = append(*code, avm.Instruction{Op: avm.OpPushString, Data: []byte(field.Name)})
			if err := emitIRExpr(field.Expr, code); err != nil {
				return err
			}
			*code = append(*code, avm.Instruction{Op: avm.OpMapSet})
		}
	case IRExprContractAddress:
		*code = append(*code, avm.Instruction{Op: avm.OpReadContractAddress})
	case IRExprOriginalBalance:
		*code = append(*code, avm.Instruction{Op: avm.OpReadOriginalBalance})
	case IRExprAttachedValue:
		*code = append(*code, avm.Instruction{Op: avm.OpReadAttachedValue})
	case IRExprLogicalTime:
		*code = append(*code, avm.Instruction{Op: avm.OpReadLogicalTime})
	case IRExprBlockTimestamp:
		*code = append(*code, avm.Instruction{Op: avm.OpReadBlockTimestamp})
	case IRExprRandom:
		*code = append(*code, avm.Instruction{Op: avm.OpReadRandom})
	case IRExprCounterfactualAddress, IRExprAutoDeployAddress:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		op := avm.OpCounterfactualAddress
		if expr.Kind == IRExprAutoDeployAddress {
			op = avm.OpAutoDeployAddress
		}
		*code = append(*code, avm.Instruction{Op: op})
	case IRExprLen:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpLen})
	case IRExprMapEmpty:
		*code = append(*code, avm.Instruction{Op: avm.OpMapEmpty})
	case IRExprMapGet:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		if len(expr.Args) != 1 {
			return fail("E_LOWER_EXPR", expr.Pos, "map.get() expects exactly one argument")
		}
		if err := emitIRExpr(expr.Args[0], code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpMapGet})
	case IRExprMapSet:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		if len(expr.Args) != 2 {
			return fail("E_LOWER_EXPR", expr.Pos, "map.set() expects exactly two arguments")
		}
		if err := emitIRExpr(expr.Args[0], code); err != nil {
			return err
		}
		if err := emitIRExpr(expr.Args[1], code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpMapSet})
	case IRExprMapHas:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		if len(expr.Args) != 1 {
			return fail("E_LOWER_EXPR", expr.Pos, "map.has() expects exactly one argument")
		}
		if err := emitIRExpr(expr.Args[0], code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpMapHas})
	case IRExprMapDelete:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		if len(expr.Args) != 1 {
			return fail("E_LOWER_EXPR", expr.Pos, "map.delete() expects exactly one argument")
		}
		if err := emitIRExpr(expr.Args[0], code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpMapDelete})
	case IRExprMapKeys:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		if len(expr.Args) != 1 {
			return fail("E_LOWER_EXPR", expr.Pos, "map.keys() expects exactly one argument")
		}
		if err := emitIRExpr(expr.Args[0], code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpMapKeys})
	case IRExprMapEntries:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		if len(expr.Args) != 1 {
			return fail("E_LOWER_EXPR", expr.Pos, "map.entries() expects exactly one argument")
		}
		if err := emitIRExpr(expr.Args[0], code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpMapEntries})
	default:
		return fail("E_LOWER_EXPR", expr.Pos, fmt.Sprintf("unsupported IR expression %q", expr.Kind))
	}
	return nil
}

func patchJumpTarget(code []avm.Instruction, index int, target int) {
	if index < 0 || index >= len(code) {
		return
	}
	code[index].Arg = uint64(target)
}

func constU64(expr Expr) (uint64, bool) {
	switch expr.Kind {
	case ExprNumber:
		text := strings.TrimSpace(expr.Text)
		base := 10
		if strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X") {
			text = text[2:]
			base = 16
		}
		v, err := strconv.ParseUint(text, base, 64)
		return v, err == nil
	case ExprBool:
		if expr.Bool {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

func parseAetLiteral(text string) (uint64, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0, fmt.Errorf("empty aet literal")
	}
	parts := strings.SplitN(trimmed, ".", 2)
	whole, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, err
	}
	const base = uint64(1_000_000_000)
	if len(parts) == 1 {
		return mulU64(whole, base)
	}
	fraction := parts[1]
	if len(fraction) == 0 || len(fraction) > 9 {
		return 0, fmt.Errorf("fractional precision exceeds 9 digits")
	}
	for len(fraction) < 9 {
		fraction += "0"
	}
	frac, err := strconv.ParseUint(fraction, 10, 64)
	if err != nil {
		return 0, err
	}
	wholeScaled, err := mulU64(whole, base)
	if err != nil {
		return 0, err
	}
	return addU64(wholeScaled, frac)
}

func parseAddressLiteral(text string) (string, error) {
	addr, err := addressing.ParseAccAddress(strings.TrimSpace(text))
	if err != nil {
		return "", err
	}
	return addressing.FormatAccAddress(addr), nil
}

func callNameIs(expr Expr, name string) bool {
	if strings.EqualFold(expr.Text, name) {
		return true
	}
	return len(expr.Path) == 1 && strings.EqualFold(expr.Path[0], name)
}

func builtinSendModeValue(name string) (uint32, bool) {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "SEND_DEFAULT":
		return 0, true
	case "SEND_CARRY_REMAINDER":
		return 64, true
	case "SEND_DRAIN_BALANCE":
		return 128, true
	case "SEND_ESTIMATE_ONLY":
		return 1024, true
	case "SEND_FEE_FROM_BALANCE":
		return 1, true
	case "SEND_IGNORE_ERRORS":
		return 2, true
	case "SEND_BOUNCE_ON_FAIL":
		return 16, true
	case "SEND_DESTROY_IF_EMPTY":
		return 32, true
	default:
		return 0, false
	}
}

func builtinBounceModeValuePath(path []string) (uint64, bool) {
	if len(path) != 2 || !strings.EqualFold(path[0], "BounceMode") {
		return 0, false
	}
	switch strings.ToLower(strings.TrimSpace(path[1])) {
	case "nobounce":
		return 0, true
	case "only256bitsofbody", "richbounce", "richbounceonlyrootchunk":
		return 1, true
	default:
		return 0, false
	}
}

func addU64(a, b uint64) (uint64, error) {
	sum := a + b
	if sum < a {
		return 0, fmt.Errorf("aet literal overflows uint64")
	}
	return sum, nil
}

func mulU64(a, b uint64) (uint64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	if a > ^uint64(0)/b {
		return 0, fmt.Errorf("aet literal overflows uint64")
	}
	return a * b, nil
}

func cloneConstEnv(in map[string]constValue) map[string]constValue {
	out := make(map[string]constValue, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func constValueToRuntimeValue(v constValue) (avm.RuntimeValue, bool) {
	switch v.Kind {
	case constKindU64:
		return avm.ValueUint64(v.U64), true
	case constKindBool:
		return avm.ValueBool(v.Bool), true
	case constKindNull:
		return avm.ValueNull(), true
	case constKindBytes:
		return avm.ValueBytes(append([]byte(nil), v.Bytes...)), true
	case constKindAddr:
		return avm.ValueAddress(v.Text), true
	case constKindText:
		return avm.ValueString(v.Text), true
	case constKindEnum:
		if v.Type != "" {
			return avm.ValueString(v.Type + "." + v.Text), true
		}
		return avm.ValueString(v.Text), true
	default:
		return avm.RuntimeValue{}, false
	}
}

func evalConstU64(expr Expr, env loweringEnv, functions map[string]*FunctionDecl, seen map[string]bool) (uint64, bool) {
	v, ok := evalConstExpr(expr, env, functions, seen)
	if !ok {
		return 0, false
	}
	switch v.Kind {
	case constKindU64:
		return v.U64, true
	case constKindBool:
		if v.Bool {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

func evalConstBool(expr Expr, env loweringEnv, functions map[string]*FunctionDecl, seen map[string]bool) (bool, bool) {
	v, ok := evalConstExpr(expr, env, functions, seen)
	if !ok {
		return false, false
	}
	if v.Kind != constKindBool {
		return false, false
	}
	return v.Bool, true
}

func evalConstEnum(expr Expr, env loweringEnv, functions map[string]*FunctionDecl, seen map[string]bool) (string, bool) {
	v, ok := evalConstExpr(expr, env, functions, seen)
	if !ok || v.Kind != constKindEnum {
		return "", false
	}
	return v.Text, true
}

func evalConstValue(expr Expr, env loweringEnv, functions map[string]*FunctionDecl, seen map[string]bool) (constValue, bool) {
	switch expr.Kind {
	case ExprNumber:
		v, ok := constU64(expr)
		if !ok {
			return constValue{}, false
		}
		return constValue{Kind: constKindU64, U64: v}, true
	case ExprBool:
		return constValue{Kind: constKindBool, Bool: expr.Bool}, true
	case ExprString:
		return constValue{Kind: constKindText, Text: expr.Text}, true
	case ExprBytes:
		return constValue{Kind: constKindBytes, Bytes: append([]byte(nil), expr.Bytes...), Type: "bytes"}, true
	case ExprNull:
		return constValue{Kind: constKindNull}, true
	case ExprUnary:
		if expr.Left == nil {
			return constValue{}, false
		}
		value, ok := evalConstValue(*expr.Left, env, functions, seen)
		if !ok {
			return constValue{}, false
		}
		switch expr.Op {
		case "!":
			switch value.Kind {
			case constKindBool:
				return constValue{Kind: constKindBool, Bool: !value.Bool}, true
			case constKindU64:
				return constValue{Kind: constKindBool, Bool: value.U64 == 0}, true
			case constKindNull:
				return constValue{Kind: constKindBool, Bool: true}, true
			default:
				return constValue{}, false
			}
		case "-":
			if value.Kind != constKindU64 {
				return constValue{}, false
			}
			return constValue{Kind: constKindU64, U64: ^value.U64 + 1}, true
		case "~":
			if value.Kind != constKindU64 {
				return constValue{}, false
			}
			return constValue{Kind: constKindU64, U64: ^value.U64}, true
		default:
			return constValue{}, false
		}
	case ExprIdent:
		if v, ok := env.consts[expr.Text]; ok {
			return v, true
		}
		if mode, ok := builtinSendModeValue(expr.Text); ok {
			return constValue{Kind: constKindU64, U64: uint64(mode)}, true
		}
		switch strings.ToLower(expr.Text) {
		case "true":
			return constValue{Kind: constKindBool, Bool: true}, true
		case "false":
			return constValue{Kind: constKindBool, Bool: false}, true
		}
		return constValue{}, false
	case ExprPath:
		if v, ok := builtinBounceModeValuePath(expr.Path); ok {
			return constValue{Kind: constKindU64, U64: v}, true
		}
		if len(expr.Path) == 1 {
			if v, ok := env.consts[expr.Path[0]]; ok {
				return v, true
			}
		}
		if len(expr.Path) == 2 {
			// A two-segment path is an enum constant only when its root is not
			// a runtime binding; storage aliases, locals, and params are field
			// reads that must lower as runtime expressions.
			root := expr.Path[0]
			if _, ok := env.storageAliases[root]; ok {
				return constValue{}, false
			}
			if _, ok := env.locals[root]; ok {
				return constValue{}, false
			}
			if _, ok := env.params[root]; ok {
				return constValue{}, false
			}
			return constValue{Kind: constKindEnum, Type: expr.Path[0], Text: expr.Path[1]}, true
		}
		return constValue{}, false
	case ExprBinary:
		left, ok := evalConstValue(*expr.Left, env, functions, seen)
		if !ok {
			return constValue{}, false
		}
		right, ok := evalConstValue(*expr.Right, env, functions, seen)
		if !ok {
			return constValue{}, false
		}
		switch expr.Op {
		case "+":
			if left.Kind != constKindU64 || right.Kind != constKindU64 {
				return constValue{}, false
			}
			return constValue{Kind: constKindU64, U64: left.U64 + right.U64}, true
		case "-":
			if left.Kind != constKindU64 || right.Kind != constKindU64 || left.U64 < right.U64 {
				return constValue{}, false
			}
			return constValue{Kind: constKindU64, U64: left.U64 - right.U64}, true
		case "*":
			if left.Kind != constKindU64 || right.Kind != constKindU64 {
				return constValue{}, false
			}
			return constValue{Kind: constKindU64, U64: left.U64 * right.U64}, true
		case "/":
			if left.Kind != constKindU64 || right.Kind != constKindU64 || right.U64 == 0 {
				return constValue{}, false
			}
			return constValue{Kind: constKindU64, U64: left.U64 / right.U64}, true
		case "%":
			if left.Kind != constKindU64 || right.Kind != constKindU64 || right.U64 == 0 {
				return constValue{}, false
			}
			return constValue{Kind: constKindU64, U64: left.U64 % right.U64}, true
		case "<<":
			if left.Kind != constKindU64 || right.Kind != constKindU64 || right.U64 >= 64 {
				return constValue{}, false
			}
			return constValue{Kind: constKindU64, U64: left.U64 << right.U64}, true
		case ">>":
			if left.Kind != constKindU64 || right.Kind != constKindU64 || right.U64 >= 64 {
				return constValue{}, false
			}
			return constValue{Kind: constKindU64, U64: left.U64 >> right.U64}, true
		case "&":
			if left.Kind != constKindU64 || right.Kind != constKindU64 {
				return constValue{}, false
			}
			return constValue{Kind: constKindU64, U64: left.U64 & right.U64}, true
		case "|":
			if left.Kind != constKindU64 || right.Kind != constKindU64 {
				return constValue{}, false
			}
			return constValue{Kind: constKindU64, U64: left.U64 | right.U64}, true
		case "^":
			if left.Kind != constKindU64 || right.Kind != constKindU64 {
				return constValue{}, false
			}
			return constValue{Kind: constKindU64, U64: left.U64 ^ right.U64}, true
		case "??":
			if left.Kind != constKindNull {
				return left, true
			}
			return right, true
		default:
			return constValue{}, false
		}
	case ExprCompare:
		left, ok := evalConstValue(*expr.Left, env, functions, seen)
		if !ok {
			return constValue{}, false
		}
		right, ok := evalConstValue(*expr.Right, env, functions, seen)
		if !ok {
			return constValue{}, false
		}
		if left.Kind != constKindU64 || right.Kind != constKindU64 {
			return constValue{}, false
		}
		switch expr.Op {
		case "==":
			return constValue{Kind: constKindBool, Bool: left.U64 == right.U64}, true
		case "!=":
			return constValue{Kind: constKindBool, Bool: left.U64 != right.U64}, true
		case "<":
			return constValue{Kind: constKindBool, Bool: left.U64 < right.U64}, true
		case "<=":
			return constValue{Kind: constKindBool, Bool: left.U64 <= right.U64}, true
		case ">":
			return constValue{Kind: constKindBool, Bool: left.U64 > right.U64}, true
		case ">=":
			return constValue{Kind: constKindBool, Bool: left.U64 >= right.U64}, true
		default:
			return constValue{}, false
		}
	case ExprLogic:
		left, ok := evalConstBool(*expr.Left, env, functions, seen)
		if !ok {
			return constValue{}, false
		}
		right, ok := evalConstBool(*expr.Right, env, functions, seen)
		if !ok {
			return constValue{}, false
		}
		switch expr.Op {
		case "&&":
			return constValue{Kind: constKindBool, Bool: left && right}, true
		case "||":
			return constValue{Kind: constKindBool, Bool: left || right}, true
		default:
			return constValue{}, false
		}
	case ExprTry:
		if expr.Left != nil {
			if left, ok := evalConstValue(*expr.Left, env, functions, seen); ok {
				return left, true
			}
		}
		if expr.Else != nil {
			return evalConstValue(*expr.Else, env, functions, seen)
		}
		return constValue{}, false
	case ExprTernary:
		if expr.Left == nil || expr.Right == nil || expr.Else == nil {
			return constValue{}, false
		}
		cond, ok := evalConstBool(*expr.Left, env, functions, seen)
		if !ok {
			return constValue{}, false
		}
		if cond {
			return evalConstValue(*expr.Right, env, functions, seen)
		}
		return evalConstValue(*expr.Else, env, functions, seen)
	case ExprCall:
		if callNameIs(expr, "address") {
			if len(expr.Args) != 1 || expr.Args[0].Kind != ExprString {
				return constValue{}, false
			}
			addr, err := parseAddressLiteral(expr.Args[0].Text)
			if err != nil {
				return constValue{}, false
			}
			return constValue{Kind: constKindAddr, Text: addr, Type: "address"}, true
		}
		if len(expr.Path) >= 2 {
			method := strings.ToLower(expr.Path[len(expr.Path)-1])
			receiver := strings.ToLower(expr.Path[0])
			switch receiver {
			// bytes/hash32 join chunk/code here so a byte constant can be declared
			// at compile time via `const X = bytes.fromHex("...")` — exactly the
			// domain-separation tags, magic prefixes, and expected digests that
			// bridge/PoW contracts need. fromHex/fromBase64 are pure decodes of a
			// string literal, so they are compile-time constant.
			case "chunk", "code", "bytes", "hash32":
				switch method {
				case "fromhex":
					if len(expr.Args) != 1 || expr.Args[0].Kind != ExprString {
						return constValue{}, false
					}
					data, err := hex.DecodeString(strings.TrimSpace(expr.Args[0].Text))
					if err != nil {
						return constValue{}, false
					}
					return constValue{Kind: constKindBytes, Bytes: data, Type: expr.Path[0]}, true
				case "frombase64":
					if len(expr.Args) != 1 || expr.Args[0].Kind != ExprString {
						return constValue{}, false
					}
					data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(expr.Args[0].Text))
					if err != nil {
						return constValue{}, false
					}
					return constValue{Kind: constKindBytes, Bytes: data, Type: expr.Path[0]}, true
				case "fromchunk", "fromsegment", "fromstate":
					if len(expr.Args) != 1 {
						return constValue{}, false
					}
					arg, ok := evalConstValue(expr.Args[0], env, functions, seen)
					if !ok {
						return constValue{}, false
					}
					switch arg.Kind {
					case constKindBytes:
						return constValue{Kind: constKindBytes, Bytes: append([]byte(nil), arg.Bytes...), Type: expr.Path[0]}, true
					case constKindText:
						data, err := hex.DecodeString(strings.TrimSpace(arg.Text))
						if err != nil {
							return constValue{}, false
						}
						return constValue{Kind: constKindBytes, Bytes: data, Type: expr.Path[0]}, true
					default:
						return constValue{}, false
					}
				}
			}
		}
		if len(expr.Path) >= 2 {
			method := strings.ToLower(expr.Path[len(expr.Path)-1])
			if len(expr.Path) == 2 {
				if v, ok := env.consts[expr.Path[0]]; ok && v.Kind == constKindBytes {
					switch method {
					case "tochunk":
						return constValue{Kind: constKindBytes, Bytes: append([]byte(nil), v.Bytes...), Type: "Chunk"}, true
					case "hash":
						root, err := avm.ToChunkPayload(v.Bytes, chunk.TypeNormal)
						if err != nil {
							return constValue{}, false
						}
						return constValue{Kind: constKindText, Text: fmt.Sprintf("%x", root.Hash()), Type: "hash32"}, true
					}
				}
			}
		}
		switch strings.ToLower(expr.Text) {
		case "hash":
			if len(expr.Args) != 1 {
				return constValue{}, false
			}
			arg, ok := evalConstValue(expr.Args[0], env, functions, seen)
			if !ok {
				return constValue{}, false
			}
			runtimeValue, ok := constValueToRuntimeValue(arg)
			if !ok {
				return constValue{}, false
			}
			switch runtimeValue.Tag {
			case avm.TagHash:
				hashBytes, err := runtimeValue.AsHash()
				if err != nil {
					return constValue{}, false
				}
				return constValue{Kind: constKindText, Text: fmt.Sprintf("%x", hashBytes), Type: "hash32"}, true
			case avm.TagBytes, avm.TagString:
				data, err := runtimeValue.AsBytes()
				if err != nil {
					return constValue{}, false
				}
				root, err := avm.ToChunkPayload(data, chunk.TypeNormal)
				if err != nil {
					sum := sha256.Sum256(data)
					return constValue{Kind: constKindText, Text: fmt.Sprintf("%x", sum), Type: "hash32"}, true
				}
				return constValue{Kind: constKindText, Text: fmt.Sprintf("%x", root.Hash()), Type: "hash32"}, true
			default:
				encoded, err := avm.CanonicalEncode(runtimeValue)
				if err != nil {
					return constValue{}, false
				}
				sum := sha256.Sum256(encoded)
				return constValue{Kind: constKindText, Text: fmt.Sprintf("%x", sum), Type: "hash32"}, true
			}
		case "len":
			return constValue{}, false
		case "aet":
			if len(expr.Args) != 1 || expr.Args[0].Kind != ExprString {
				return constValue{}, false
			}
			value, err := parseAetLiteral(expr.Args[0].Text)
			if err != nil {
				return constValue{}, false
			}
			return constValue{Kind: constKindU64, U64: value}, true
		case "ok":
			if len(expr.Args) != 1 {
				return constValue{}, false
			}
			return evalConstValue(expr.Args[0], env, functions, seen)
		case "err":
			if len(expr.Args) != 1 {
				return constValue{}, false
			}
			return evalConstValue(expr.Args[0], env, functions, seen)
		}
		fn, ok := functions[expr.Text]
		if !ok || fn == nil {
			return constValue{}, false
		}
		if seen[fn.Name] {
			return constValue{}, false
		}
		if len(expr.Args) != len(fn.Params) {
			return constValue{}, false
		}
		nextSeen := cloneSeen(seen)
		nextSeen[fn.Name] = true
		callEnv := loweringEnv{params: map[string]int{}, consts: map[string]constValue{}}
		for i, param := range fn.Params {
			arg, ok := evalConstValue(expr.Args[i], env, functions, seen)
			if !ok {
				return constValue{}, false
			}
			callEnv.consts[param.Name] = arg
		}
		return evalConstStatements(fn.Body, callEnv, functions, nextSeen)
	default:
		return constValue{}, false
	}
}

func evalConstExpr(expr Expr, env loweringEnv, functions map[string]*FunctionDecl, seen map[string]bool) (constValue, bool) {
	return evalConstValue(expr, env, functions, seen)
}

func evalConstStatements(stmts []Statement, env loweringEnv, functions map[string]*FunctionDecl, seen map[string]bool) (constValue, bool) {
	for _, stmt := range stmts {
		switch stmt.Kind {
		case StatementBinding:
			v, ok := evalConstValue(stmt.Value, env, functions, seen)
			if !ok {
				return constValue{}, false
			}
			env.consts[stmt.Name] = v
		case StatementIf:
			cond, ok := evalConstBool(stmt.Value, env, functions, seen)
			if !ok {
				return constValue{}, false
			}
			var branch []Statement
			if cond {
				branch = stmt.Then
			} else {
				branch = stmt.Else
			}
			v, ok := evalConstStatements(branch, env, functions, seen)
			if !ok {
				return constValue{}, false
			}
			return v, true
		case StatementMatch:
			tag, ok := evalConstEnum(stmt.Value, env, functions, seen)
			if !ok {
				return constValue{}, false
			}
			for _, arm := range stmt.Arms {
				if arm.Pattern.Kind == PatternWildcard || patternTail(arm.Pattern.Name) == tag {
					if len(arm.Pattern.Bindings) > 0 {
						return constValue{}, false
					}
					v, ok := evalConstStatements(arm.Body, env, functions, seen)
					if !ok {
						return constValue{}, false
					}
					return v, true
				}
			}
			return constValue{}, false
		case StatementFor:
			start, ok := evalConstU64(stmt.Start, env, functions, seen)
			if !ok {
				return constValue{}, false
			}
			end, ok := evalConstU64(stmt.End, env, functions, seen)
			if !ok {
				return constValue{}, false
			}
			if end < start {
				return constValue{}, false
			}
			var last constValue
			for i := start; i < end; i++ {
				iterEnv := loweringEnv{params: env.params, consts: cloneConstEnv(env.consts)}
				iterEnv.consts[stmt.Index] = constValue{Kind: constKindU64, U64: i}
				v, ok := evalConstStatements(stmt.Then, iterEnv, functions, seen)
				if !ok {
					return constValue{}, false
				}
				last = v
			}
			return last, true
		case StatementReturn:
			return evalConstValue(stmt.Value, env, functions, seen)
		default:
			return constValue{}, false
		}
	}
	return constValue{}, false
}

func cloneSeen(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func callDisplayName(expr Expr) string {
	if len(expr.Path) >= 2 {
		return strings.Join(expr.Path, ".")
	}
	return expr.Text
}

// resolveUserFunction finds the declared function a call refers to, both for
// plain calls (walletAddressFor(...)) and receiver-style calls
// (TokenWalletStorage.zeroFor(...)).
func resolveUserFunction(expr Expr, functions map[string]*FunctionDecl) *FunctionDecl {
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

// substituteExprForInline clones expr replacing parameter identifiers with
// the caller's argument expressions. Only identifier/path arguments can be
// spliced into nested paths; richer substitutions fail loudly.
func substituteExprForInline(expr Expr, subst map[string]Expr) (Expr, error) {
	out := expr
	switch expr.Kind {
	case ExprIdent:
		if replacement, ok := subst[expr.Text]; ok {
			return replacement, nil
		}
		return out, nil
	case ExprPath:
		if len(expr.Path) == 0 {
			return out, nil
		}
		replacement, ok := subst[expr.Path[0]]
		if !ok {
			out.Path = append([]string(nil), expr.Path...)
			return out, nil
		}
		switch replacement.Kind {
		case ExprIdent:
			out.Path = append([]string{replacement.Text}, expr.Path[1:]...)
			return out, nil
		case ExprPath:
			out.Path = append(append([]string(nil), replacement.Path...), expr.Path[1:]...)
			return out, nil
		default:
			if len(expr.Path) == 1 {
				return replacement, nil
			}
			return Expr{}, fmt.Errorf("cannot substitute complex argument into field access %q", strings.Join(expr.Path, "."))
		}
	case ExprCall:
		args := make([]Expr, len(expr.Args))
		for i, arg := range expr.Args {
			sub, err := substituteExprForInline(arg, subst)
			if err != nil {
				return Expr{}, err
			}
			args[i] = sub
		}
		out.Args = args
		if len(expr.Path) > 0 {
			if replacement, ok := subst[expr.Path[0]]; ok {
				switch replacement.Kind {
				case ExprIdent:
					out.Path = append([]string{replacement.Text}, expr.Path[1:]...)
				case ExprPath:
					out.Path = append(append([]string(nil), replacement.Path...), expr.Path[1:]...)
				default:
					return Expr{}, fmt.Errorf("cannot substitute complex argument into call receiver %q", strings.Join(expr.Path, "."))
				}
			} else {
				out.Path = append([]string(nil), expr.Path...)
			}
		}
		return out, nil
	case ExprStruct:
		fields := make([]ExprField, len(expr.Fields))
		for i, field := range expr.Fields {
			sub, err := substituteExprForInline(field.Value, subst)
			if err != nil {
				return Expr{}, err
			}
			fields[i] = ExprField{Name: field.Name, Value: sub, Pos: field.Pos}
		}
		out.Fields = fields
		return out, nil
	case ExprBinary, ExprUnary, ExprTernary, ExprCompare, ExprLogic:
		// Compare (<, >, ==, ...) and logic (&&, ||) nodes carry their operands
		// in Left/Right exactly like binary nodes, so a parameter used inside a
		// comparison in an inlinable helper (e.g. min(a, b) or a bounds check)
		// must be substituted too — otherwise it lowers to an unbound
		// identifier.
		if expr.Left != nil {
			sub, err := substituteExprForInline(*expr.Left, subst)
			if err != nil {
				return Expr{}, err
			}
			out.Left = &sub
		}
		if expr.Right != nil {
			sub, err := substituteExprForInline(*expr.Right, subst)
			if err != nil {
				return Expr{}, err
			}
			out.Right = &sub
		}
		if expr.Else != nil {
			sub, err := substituteExprForInline(*expr.Else, subst)
			if err != nil {
				return Expr{}, err
			}
			out.Else = &sub
		}
		return out, nil
	default:
		return out, nil
	}
}

// tryInlineUserFunctionCall inlines calls to declared helper functions whose
// bodies consist of optional `lazy Storage.load()` bindings followed by a
// single return expression. The inlined expression is lowered in the caller
// environment, so runtime arguments (message fields, locals, envelope data)
// stay live instead of silently degrading. Returns handled=false when the
// call does not refer to a declared function.
func tryInlineUserFunctionCall(expr Expr, env loweringEnv, functions map[string]*FunctionDecl, seen map[string]bool) (*IRExpr, bool, error) {
	fn := resolveUserFunction(expr, functions)
	if fn == nil {
		return nil, false, nil
	}
	name := callDisplayName(expr)
	if seen[fn.Name] {
		return nil, true, fail("E_LOWER_CALL", expr.Pos, fmt.Sprintf("recursive call %q cannot be inlined by AVM v1", name))
	}
	if len(expr.Args) != len(fn.Params) {
		return nil, true, fail("E_LOWER_CALL", expr.Pos, fmt.Sprintf("call %q expects %d arguments, got %d", name, len(fn.Params), len(expr.Args)))
	}

	inlineEnv := cloneLoweringEnv(env)
	var retExpr *Expr
	for _, stmt := range fn.Body {
		switch stmt.Kind {
		case StatementBinding:
			if isStorageLoadBinding(stmt.Value) {
				inlineEnv.storageAliases[stmt.Name] = struct{}{}
				continue
			}
			return nil, true, fail("E_LOWER_CALL", stmt.Pos, fmt.Sprintf("call %q cannot be inlined by AVM v1: only lazy storage bindings and a single return are supported", name))
		case StatementReturn:
			value := stmt.Value
			retExpr = &value
		default:
			return nil, true, fail("E_LOWER_CALL", stmt.Pos, fmt.Sprintf("call %q cannot be inlined by AVM v1: unsupported statement in function body", name))
		}
		if retExpr != nil {
			break
		}
	}
	if retExpr == nil {
		return nil, true, fail("E_LOWER_CALL", expr.Pos, fmt.Sprintf("call %q cannot be inlined by AVM v1: function has no return expression", name))
	}

	subst := make(map[string]Expr, len(fn.Params))
	for i, param := range fn.Params {
		subst[param.Name] = expr.Args[i]
	}
	substituted, err := substituteExprForInline(*retExpr, subst)
	if err != nil {
		return nil, true, fail("E_LOWER_CALL", expr.Pos, fmt.Sprintf("call %q cannot be inlined by AVM v1: %v", name, err))
	}

	nextSeen := cloneSeen(seen)
	nextSeen[fn.Name] = true
	lowered, err := lowerExprToIR(substituted, inlineEnv, functions, nextSeen)
	if err != nil {
		return nil, true, err
	}
	return lowered, true, nil
}

func staticOpcode(stmt Statement) (uint32, error) {
	if stmt.Extra != nil {
		if expr, ok := stmt.Extra["opcode"]; ok {
			v, ok := constU64(expr)
			if !ok || v > uint64(^uint32(0)) {
				return 0, fail("E_LOWER_OPCODE", expr.Pos, "send opcode must be a uint32 constant")
			}
			return uint32(v), nil
		}
	}
	return 0, nil
}

func traceCommitment(contract string, kind string, handlers []canonicalHandle) ([32]byte, error) {
	payload, err := json.Marshal(canonicalBlock{Contract: contract, Kind: kind, Handlers: handlers})
	if err != nil {
		return [32]byte{}, err
	}
	return blake3.Sum256(payload), nil
}

func statementTraceData(stmt Statement) []byte {
	payload, _ := json.Marshal(canonicalStatements([]Statement{stmt})[0])
	sum := blake3.Sum256(payload)
	return sum[:]
}

func eventTraceData(stmt Statement) []byte {
	payload, _ := json.Marshal(map[string]any{
		"event": stmt.Name,
		"args":  canonicalExprStrings(stmt.Args),
	})
	sum := blake3.Sum256(payload)
	return sum[:]
}

func importsForCode(code []avm.Instruction) []avm.HostFunction {
	seen := map[avm.HostFunction]struct{}{}
	for _, ins := range code {
		if required, ok := avm.RequiredHostFunction(ins.Op); ok {
			seen[required] = struct{}{}
		}
	}
	out := make([]avm.HostFunction, 0, len(seen))
	for host := range seen {
		out = append(out, host)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func findMessage(contract *ContractDecl, name string, kind MessageKind) *MessageDecl {
	for _, msg := range contract.Messages {
		if msg.Name == name && msg.Kind == kind {
			return msg
		}
	}
	return nil
}

func findGetter(contract *ContractDecl, name string) *GetterDecl {
	for _, get := range contract.Getters {
		if get.Name == name {
			return get
		}
	}
	return nil
}

func selectorForMessage(contractName string, msg *MessageDecl) uint32 {
	if msg.ExplicitSel != nil {
		return *msg.ExplicitSel
	}
	return selectorFromSignature(signatureForMessage(contractName, msg))
}

func selectorForGetter(contractName string, get *GetterDecl) uint32 {
	if get.ExplicitSel != nil {
		return *get.ExplicitSel
	}
	return selectorFromSignature(signatureForGetter(contractName, get))
}

func (c *Compiler) buildStateInit(contract *ContractDecl, moduleHash [32]byte, storageCodec Codec, layout StorageLayout, lock DependencyLock) (*avm.StateInit, [32]byte, error) {
	initData, err := storageCodec.EncodeDefaults()
	if err != nil {
		return nil, [32]byte{}, err
	}
	root, err := buildChunkTree(initData)
	if err != nil {
		return nil, [32]byte{}, err
	}
	si := &avm.StateInit{
		ABIVersion:       DefaultABIVersion,
		CodeHash:         moduleHash,
		InitData:         initData,
		Salt:             []byte(firstNonEmpty(contract.Salt, c.opts.Salt)),
		DeployerAddress:  firstNonEmpty(contract.DeployerAddress, c.opts.DeployerAddress),
		ChainID:          firstNonEmpty(contract.ChainID, c.opts.ChainID),
		Namespace:        firstNonEmpty(contract.Namespace, c.opts.Namespace),
		DependencyHashes: dependencyHashes(lock),
		InitialStateRoot: root,
		InitialBalance:   firstNonZero(contract.InitialBalance, c.opts.InitialBalance),
		Capabilities:     avm.DeployCapabilityMask{},
	}
	hash, err := avm.HashStateInit(si)
	if err != nil {
		return nil, [32]byte{}, err
	}
	return si, hash, nil
}

type canonicalBlock struct {
	Contract string            `json:"contract"`
	Kind     string            `json:"kind"`
	Handlers []canonicalHandle `json:"handlers"`
}

type canonicalHandle struct {
	Name       string          `json:"name"`
	Signature  string          `json:"signature"`
	Selector   uint32          `json:"selector"`
	Topic      string          `json:"topic,omitempty"`
	Entrypoint string          `json:"entrypoint"`
	Params     []string        `json:"params"`
	Results    []string        `json:"results"`
	Body       []canonicalStmt `json:"body"`
}

type canonicalStmt struct {
	Kind  string            `json:"kind"`
	Name  string            `json:"name,omitempty"`
	Path  []string          `json:"path,omitempty"`
	Value string            `json:"value,omitempty"`
	Args  []string          `json:"args,omitempty"`
	Extra map[string]string `json:"extra,omitempty"`
	Then  []canonicalStmt   `json:"then,omitempty"`
	Else  []canonicalStmt   `json:"else,omitempty"`
	Arms  []canonicalArm    `json:"arms,omitempty"`
	Start string            `json:"start,omitempty"`
	End   string            `json:"end,omitempty"`
	Index string            `json:"index,omitempty"`
}

type canonicalArm struct {
	Pattern canonicalPattern `json:"pattern"`
	Body    []canonicalStmt  `json:"body"`
}

type canonicalPattern struct {
	Kind     string   `json:"kind"`
	Name     string   `json:"name,omitempty"`
	Bindings []string `json:"bindings,omitempty"`
}

func (c *Compiler) collectHandlersForKind(contract *ContractDecl, kind MessageKind) []canonicalHandle {
	var out []canonicalHandle
	for _, msg := range contract.Messages {
		if msg.Kind != kind {
			continue
		}
		sig := signatureForMessage(contract.Name, msg)
		out = append(out, canonicalHandle{
			Name:       msg.Name,
			Signature:  sig,
			Selector:   selectorFromSignature(sig),
			Entrypoint: messageKindEntrypointName(kind),
			Params:     typeNamesFromParams(msg.Params),
			Results:    typeNamesFromResults(msg.ReturnType),
			Body:       canonicalStatements(msg.Body),
		})
	}
	return out
}

func (c *Compiler) collectGetters(contract *ContractDecl) []canonicalHandle {
	var out []canonicalHandle
	for _, get := range contract.Getters {
		sig := signatureForGetter(contract.Name, get)
		out = append(out, canonicalHandle{
			Name:       get.Name,
			Signature:  sig,
			Selector:   selectorFromSignature(sig),
			Entrypoint: entrypointName(avm.EntryQuery),
			Params:     typeNamesFromParams(get.Params),
			Results:    typeNamesFromResults(&get.ReturnType),
			Body:       canonicalStatements(get.Body),
		})
	}
	return out
}

func canonicalStatements(stmts []Statement) []canonicalStmt {
	out := make([]canonicalStmt, 0, len(stmts))
	for _, stmt := range stmts {
		c := canonicalStmt{Kind: string(stmt.Kind), Name: stmt.Name, Path: append([]string(nil), stmt.Path...), Value: canonicalExprString(stmt.Value), Args: canonicalExprStrings(stmt.Args), Extra: map[string]string{}, Start: canonicalExprString(stmt.Start), End: canonicalExprString(stmt.End), Index: stmt.Index}
		if len(stmt.Extra) == 0 {
			c.Extra = nil
		} else {
			keys := make([]string, 0, len(stmt.Extra))
			for key := range stmt.Extra {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				c.Extra[key] = canonicalExprString(stmt.Extra[key])
			}
		}
		if len(stmt.Then) > 0 {
			c.Then = canonicalStatements(stmt.Then)
		}
		if len(stmt.Else) > 0 {
			c.Else = canonicalStatements(stmt.Else)
		}
		if len(stmt.Arms) > 0 {
			c.Arms = make([]canonicalArm, 0, len(stmt.Arms))
			for _, arm := range stmt.Arms {
				c.Arms = append(c.Arms, canonicalArm{
					Pattern: canonicalPattern{Kind: string(arm.Pattern.Kind), Name: arm.Pattern.Name, Bindings: append([]string(nil), arm.Pattern.Bindings...)},
					Body:    canonicalStatements(arm.Body),
				})
			}
		}
		out = append(out, c)
	}
	return out
}

func canonicalExprStrings(exprs []Expr) []string {
	out := make([]string, 0, len(exprs))
	for _, expr := range exprs {
		out = append(out, canonicalExprString(expr))
	}
	return out
}

func canonicalExprString(expr Expr) string {
	switch expr.Kind {
	case ExprNumber, ExprString, ExprIdent:
		return expr.Text
	case ExprBytes:
		return "0x" + hex.EncodeToString(expr.Bytes)
	case ExprBool:
		return strconv.FormatBool(expr.Bool)
	case ExprPath:
		return strings.Join(expr.Path, ".")
	case ExprCall:
		name := expr.Text
		if len(expr.Path) > 1 {
			name = strings.Join(expr.Path, ".")
		}
		return name + "(" + strings.Join(canonicalExprStrings(expr.Args), ",") + ")"
	case ExprUnary:
		out := expr.Op + canonicalExprString(*expr.Left)
		if expr.Unwrap {
			out += "!"
		}
		return out
	case ExprBinary:
		return "(" + canonicalExprString(*expr.Left) + expr.Op + canonicalExprString(*expr.Right) + ")"
	case ExprCompare, ExprLogic:
		return "(" + canonicalExprString(*expr.Left) + expr.Op + canonicalExprString(*expr.Right) + ")"
	case ExprTernary:
		return "(" + canonicalExprString(*expr.Left) + "?" + canonicalExprString(*expr.Right) + ":" + canonicalExprString(*expr.Else) + ")"
	case ExprTry:
		out := "try(" + canonicalExprString(*expr.Left) + ")"
		if expr.Else != nil {
			out += "else(" + canonicalExprString(*expr.Else) + ")"
		}
		return out
	case ExprStruct:
		var b strings.Builder
		if expr.Text != "" {
			b.WriteString(expr.Text)
		}
		b.WriteString("{")
		for i, field := range expr.Fields {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(field.Name)
			b.WriteString(":")
			b.WriteString(canonicalExprString(field.Value))
		}
		b.WriteString("}")
		return b.String()
	default:
		return string(expr.Kind)
	}
}

func paramsToInterface(params []ParamDecl) []avm.InterfaceParamDescriptor {
	out := make([]avm.InterfaceParamDescriptor, 0, len(params))
	for _, param := range params {
		out = append(out, avm.InterfaceParamDescriptor{Name: param.Name, Kind: interfaceKindForType(param.Type), Required: !param.Type.Optional})
	}
	return out
}

func resultsToInterface(ret *TypeRef) []avm.InterfaceResultDescriptor {
	if ret == nil {
		return nil
	}
	return []avm.InterfaceResultDescriptor{{Name: "result", Kind: interfaceKindForType(*ret), Required: !ret.Optional}}
}

func paramsToCodecFields(params []ParamDecl) []CodecField {
	out := make([]CodecField, 0, len(params))
	for _, param := range params {
		out = append(out, CodecField{Name: param.Name, Type: param.Type, Pos: param.Pos})
	}
	return out
}

func interfaceKindForType(typ TypeRef) avm.InterfaceValueKind {
	switch strings.ToLower(typ.Name) {
	case "bool":
		return avm.InterfaceValueBool
	case "u2", "u4", "u8", "u16", "u32", "u64", "uint2", "uint4", "uint8", "uint16", "uint32", "uint64", "i2", "i4", "i8", "i16", "i32", "i64", "int2", "int4", "int8", "int16", "int32", "int64", "timestamp", "coins":
		return avm.InterfaceValueU64
	case "bytes", "hash", "hash32", "stateinit", "code", "chunk":
		return avm.InterfaceValueBytes
	case "string":
		return avm.InterfaceValueString
	case "address":
		return avm.InterfaceValueAddress
	default:
		return avm.InterfaceValueBytes
	}
}

func signatureForMessage(contractName string, msg *MessageDecl) string {
	return strings.Join([]string{
		"message",
		msg.Kind.String(),
		contractName,
		msg.Name + "(" + strings.Join(typeNamesFromParams(msg.Params), ",") + ")" + returnSignature(msg.ReturnType),
	}, ":")
}

func signatureForGetter(contractName string, get *GetterDecl) string {
	return strings.Join([]string{
		"getter",
		contractName,
		get.Name + "(" + strings.Join(typeNamesFromParams(get.Params), ",") + ")" + returnSignature(&get.ReturnType),
	}, ":")
}

func signatureForGetterFunction(contractName string, fn *FunctionDecl) string {
	return strings.Join([]string{
		"getter",
		contractName,
		fn.Name + "(" + strings.Join(typeNamesFromParams(fn.Params), ",") + ")" + returnSignature(&fn.ReturnType),
	}, ":")
}

func signatureForFunction(fn *FunctionDecl) string {
	return strings.Join([]string{
		"function",
		fn.Name + "(" + strings.Join(typeNamesFromParams(fn.Params), ",") + ")" + returnSignature(&fn.ReturnType),
	}, ":")
}

func signatureForEvent(contractName string, event *EventDecl) string {
	return strings.Join([]string{
		"event",
		contractName,
		event.Name + "(" + strings.Join(typeNamesFromParams(event.FieldsToParams()), ",") + ")",
	}, ":")
}

func signatureForWalletAction(contractName string, wallet *WalletActionDecl) string {
	metadata := []string{
		"wallet_action",
		contractName,
		wallet.Name,
		wallet.Title,
		wallet.Risk,
		wallet.ConfirmLabel,
		wallet.WarningLevel,
		strings.Join(wallet.ExpectedSideEffects, ","),
		strconv.FormatBool(wallet.FundAccess),
		wallet.ApprovalSemantics,
	}
	return strings.Join(metadata, ":")
}

func (e *EventDecl) FieldsToParams() []ParamDecl {
	params := make([]ParamDecl, 0, len(e.Fields))
	for _, field := range e.Fields {
		params = append(params, ParamDecl{Name: field.Name, Type: field.Type, Pos: field.Pos})
	}
	return params
}

func typeNamesFromParams(params []ParamDecl) []string {
	out := make([]string, 0, len(params))
	for _, param := range params {
		out = append(out, param.Type.String())
	}
	return out
}

func typeNamesFromResults(ret *TypeRef) []string {
	if ret == nil {
		return nil
	}
	return []string{ret.String()}
}

func returnSignature(ret *TypeRef) string {
	if ret == nil {
		return ""
	}
	return "->" + ret.String()
}

func selectorFromSignature(signature string) uint32 {
	sum := blake3.Sum256([]byte(signature))
	return binary.BigEndian.Uint32(sum[:4])
}

func messageUnionHashBytes(name string, variants []MessageUnionVariant) []byte {
	var b bytes.Buffer
	b.WriteString(name)
	for _, variant := range variants {
		b.WriteByte(0)
		b.WriteString(variant.Name)
		b.WriteByte(0)
		b.WriteString(variant.Type)
		b.WriteByte(0)
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], variant.Opcode)
		b.Write(buf[:])
	}
	return b.Bytes()
}

func messageKindOrder(kind MessageKind) int {
	switch kind {
	case MessageKindDeploy:
		return 0
	case MessageKindExternal:
		return 1
	case MessageKindInternal:
		return 2
	case MessageKindBounced:
		return 3
	case MessageKindMigrate:
		return 4
	default:
		return 5
	}
}

func kindToEntrypoint(kind MessageKind) avm.Entrypoint {
	switch kind {
	case MessageKindExternal:
		return avm.EntryReceiveExternal
	case MessageKindInternal:
		return avm.EntryReceiveInternal
	case MessageKindBounced:
		return avm.EntryReceiveBounced
	case MessageKindMigrate:
		return avm.EntryMigrate
	default:
		return avm.EntryDeploy
	}
}

func messageKindEntrypointName(kind MessageKind) string {
	return entrypointName(kindToEntrypoint(kind))
}

func entrypointName(entry avm.Entrypoint) string {
	switch entry {
	case avm.EntryDeploy:
		return "deploy"
	case avm.EntryReceiveExternal:
		return "external"
	case avm.EntryReceiveInternal:
		return "internal"
	case avm.EntryReceiveBounced:
		return "bounced"
	case avm.EntryQuery:
		return "query"
	case avm.EntryMigrate:
		return "migrate"
	default:
		return strconv.Itoa(int(entry))
	}
}

func buildCLIBindings(contract *ContractDecl) []avm.InterfaceCLIBinding {
	var out []avm.InterfaceCLIBinding
	for _, msg := range contract.Messages {
		out = append(out, avm.InterfaceCLIBinding{
			Method:       msg.Name,
			Command:      strings.ToLower(msg.Name),
			Use:          contract.Name + " " + msg.Name,
			InputFormat:  "json",
			OutputFormat: "json",
		})
	}
	for _, get := range contract.Getters {
		out = append(out, avm.InterfaceCLIBinding{
			Method:       get.Name,
			Command:      strings.ToLower(get.Name),
			Use:          contract.Name + " " + get.Name,
			InputFormat:  "json",
			OutputFormat: "json",
		})
	}
	return out
}

func buildSDKBindings(contract *ContractDecl) []avm.InterfaceSDKBinding {
	var out []avm.InterfaceSDKBinding
	for _, msg := range contract.Messages {
		out = append(out, avm.InterfaceSDKBinding{
			Method:       msg.Name,
			Package:      contract.Name,
			Service:      contract.Name,
			MethodName:   msg.Name,
			RequestType:  contract.Name + msg.Name + "Request",
			ResponseType: contract.Name + msg.Name + "Response",
			Async:        msg.Kind != MessageKindDeploy && msg.Kind != MessageKindMigrate,
		})
	}
	for _, get := range contract.Getters {
		out = append(out, avm.InterfaceSDKBinding{
			Method:       get.Name,
			Package:      contract.Name,
			Service:      contract.Name,
			MethodName:   get.Name,
			RequestType:  contract.Name + get.Name + "Request",
			ResponseType: contract.Name + get.Name + "Response",
			Async:        false,
		})
	}
	return out
}

func registryHashBytes(reg SelectorRegistry) []byte {
	type entry struct {
		Kind       string `json:"kind"`
		Name       string `json:"name"`
		Signature  string `json:"signature"`
		Selector   uint32 `json:"selector"`
		Topic      string `json:"topic"`
		Entrypoint string `json:"entrypoint"`
	}
	out := make([]entry, len(reg.Entries))
	for i, item := range reg.Entries {
		out[i] = entry(item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Signature < out[j].Signature
	})
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(out)
	return buf.Bytes()
}

func layoutHashBytes(layout StorageLayout) []byte {
	type field struct {
		Name    string `json:"name"`
		Lazy    bool   `json:"lazy,omitempty"`
		Type    string `json:"type"`
		Default string `json:"default"`
	}
	out := make([]field, len(layout.Fields))
	for i, item := range layout.Fields {
		out[i] = field{Name: item.Name, Type: item.Type.String(), Default: canonicalExprString(item.Default)}
	}
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(out)
	return buf.Bytes()
}

func codecHashBytes(codec Codec) []byte {
	type field struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Default string `json:"default"`
	}
	type record struct {
		Name       string  `json:"name"`
		Kind       string  `json:"kind"`
		Fields     []field `json:"fields"`
		ReturnType string  `json:"return_type,omitempty"`
	}
	rec := record{Name: codec.Name, Kind: codec.Kind, Fields: make([]field, len(codec.Fields))}
	for i, item := range codec.Fields {
		rec.Fields[i] = field{Name: item.Name, Type: item.Type.String(), Default: canonicalExprString(item.Default)}
	}
	if codec.ReturnType != nil {
		rec.ReturnType = codec.ReturnType.String()
	}
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(rec)
	return buf.Bytes()
}

func dependencyBytes(dep ResolvedDependency) []byte {
	type record struct {
		Path       string `json:"path"`
		Version    string `json:"version"`
		Alias      string `json:"alias,omitempty"`
		ABIHash    string `json:"abi_hash"`
		SourceHash string `json:"source_hash"`
	}
	rec := record{
		Path:       dep.Path,
		Version:    dep.Version,
		Alias:      dep.Alias,
		ABIHash:    fmt.Sprintf("%x", dep.ABIHash[:]),
		SourceHash: fmt.Sprintf("%x", dep.SourceHash[:]),
	}
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(rec)
	return buf.Bytes()
}

func dependencyLockBytes(lock DependencyLock) []byte {
	type entry struct {
		Path       string `json:"path"`
		Version    string `json:"version"`
		Alias      string `json:"alias,omitempty"`
		ABIHash    string `json:"abi_hash"`
		SourceHash string `json:"source_hash"`
		LockHash   string `json:"lock_hash"`
	}
	rec := struct {
		Package string  `json:"package,omitempty"`
		Entries []entry `json:"entries"`
	}{Package: lock.Package, Entries: make([]entry, len(lock.Entries))}
	for i, dep := range lock.Entries {
		rec.Entries[i] = entry{
			Path:       dep.Path,
			Version:    dep.Version,
			Alias:      dep.Alias,
			ABIHash:    fmt.Sprintf("%x", dep.ABIHash[:]),
			SourceHash: fmt.Sprintf("%x", dep.SourceHash[:]),
			LockHash:   fmt.Sprintf("%x", dep.LockHash[:]),
		}
	}
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(rec)
	return buf.Bytes()
}

func dependencyHashes(lock DependencyLock) [][32]byte {
	out := make([][32]byte, 0, len(lock.Entries)+1)
	for _, dep := range lock.Entries {
		out = append(out, dep.LockHash)
	}
	if lock.LockHash != ([32]byte{}) {
		out = append(out, lock.LockHash)
	}
	return out
}

func findStruct(structs []*StructDecl, name string) (*StructDecl, bool) {
	for _, st := range structs {
		if st.Name == name {
			return st, true
		}
	}
	return nil, false
}

func fail(code string, pos Position, msg string) error {
	return &CompileError{Diagnostics: []Diagnostic{{Severity: SeverityError, Code: code, Message: msg, Pos: pos}}}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonZero(values ...uint64) uint64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func isValidName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if !(r == '_' || ('A' <= r && r <= 'Z') || ('a' <= r && r <= 'z')) {
				return false
			}
			continue
		}
		if !(r == '_' || r == '-' || ('0' <= r && r <= '9') || ('A' <= r && r <= 'Z') || ('a' <= r && r <= 'z')) {
			return false
		}
	}
	return true
}

// buildChunkTree delegates to the canonical packing in the chunk package so
// the compiler's CodeChunkHash and any off-chain "cells" rendering of the same
// bytes always agree.
func buildChunkTree(data []byte) (*chunk.Chunk, error) {
	return chunk.BuildTree(data)
}

func encodeStateInit(si *avm.StateInit) ([]byte, error) {
	if si == nil {
		return nil, fmt.Errorf("nil state init")
	}
	return si.CanonicalEncode()
}
