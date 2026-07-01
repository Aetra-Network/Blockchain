package compiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

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
	ChainID           string
	Namespace         string
	DeployerAddress   string
	Salt              string
	InitialBalance    uint64
	MaxCodeBytes      uint32
	MaxPayloadBytes   uint32
	MaxStorageBytes   uint32
	MaxStateInitBytes uint32
	Resolver          DependencyResolver
}

func DefaultOptions() Options {
	return Options{
		ChainID:           DefaultChainID,
		Namespace:         DefaultNamespace,
		DeployerAddress:   DefaultDeployerAddress,
		Salt:              DefaultSalt,
		MaxCodeBytes:      DefaultMaxCodeBytes,
		MaxPayloadBytes:   DefaultMaxPayloadBytes,
		MaxStorageBytes:   DefaultMaxStorageBytes,
		MaxStateInitBytes: DefaultMaxStateInitBytes,
	}
}

type Compiler struct {
	opts Options
}

type Result struct {
	Source           *SourceFile
	Contract         *ContractDecl
	Module           avm.Module
	ModuleBytes      []byte
	ModuleHash       [32]byte
	Manifest         avm.InterfaceManifest
	ManifestHash     [32]byte
	StateInit        *avm.StateInit
	StateInitHash    [32]byte
	CodeChunk        *chunk.Chunk
	CodeChunkHash    [32]byte
	StorageLayout    StorageLayout
	StorageCodec     Codec
	MessageCodecs    map[string]Codec
	GetterCodecs     map[string]Codec
	EventCodecs      map[string]Codec
	SelectorRegistry SelectorRegistry
	Diagnostics      []Diagnostic
	IR               *IRProgram
	DependencyLock   DependencyLock
}

type StorageLayout struct {
	Name       string
	Fields     []CodecField
	LayoutHash [32]byte
}

type CodecField struct {
	Name    string
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
	return &Compiler{opts: merged}, nil
}

func (c *Compiler) Compile(src []byte) (*Result, error) {
	return c.CompileFiles([]NamedSource{{Name: "main.avm", Data: src}})
}

func (c *Compiler) CompileFiles(sources []NamedSource) (*Result, error) {
	file, err := parsePackageSources(sources, c.opts.Resolver)
	if err != nil {
		return nil, err
	}
	if len(file.Contracts) != 1 {
		return nil, fail("E_CONTRACT_COUNT", Position{}, "package must declare exactly one contract")
	}
	contract := file.Contracts[0]
	functions, err := buildFunctionMap(file.Functions)
	if err != nil {
		return nil, err
	}
	result := &Result{Source: file, Contract: contract, MessageCodecs: map[string]Codec{}, GetterCodecs: map[string]Codec{}, EventCodecs: map[string]Codec{}}
	lock, err := c.buildDependencyLock(file)
	if err != nil {
		return nil, err
	}
	result.DependencyLock = lock

	if err := c.typecheck(file, contract, functions); err != nil {
		return nil, err
	}

	manifest, registry, layout, storageCodec, msgCodecs, getterCodecs, eventCodecs, err := c.buildArtifacts(file, contract)
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
	result.GetterCodecs = getterCodecs
	result.EventCodecs = eventCodecs

	module, moduleBytes, ir, err := c.buildModule(file, contract, manifest, result.SelectorRegistry, msgCodecs, getterCodecs, eventCodecs, lock, functions)
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
		merged.Enums = append(merged.Enums, file.Enums...)
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
	merged.Enums = append(merged.Enums, src.Enums...)
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

func (c *Compiler) typecheck(file *SourceFile, contract *ContractDecl, functions map[string]*FunctionDecl) error {
	structs := map[string]*StructDecl{}
	enums := map[string]*EnumDecl{}
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
	storage, ok := structs[contract.StorageTypeName]
	if !ok {
		return fail("E_STORAGE_TYPE", contract.Pos, fmt.Sprintf("storage type %q not found", contract.StorageTypeName))
	}
	seenCallables := map[string]struct{}{}
	if err := c.validateStruct(storage, structs, enums); err != nil {
		return err
	}
	for _, st := range file.Structs {
		if err := c.validateStruct(st, structs, enums); err != nil {
			return err
		}
	}
	for _, en := range file.Enums {
		if err := c.validateEnum(en, structs, enums); err != nil {
			return err
		}
	}
	for _, fn := range file.Functions {
		if err := c.validateFunction(fn, structs, enums, functions); err != nil {
			return err
		}
		if _, ok := seenCallables[fn.Name]; ok {
			return fail("E_DUP_CALLABLE", fn.Pos, fmt.Sprintf("duplicate callable name %q", fn.Name))
		}
		seenCallables[fn.Name] = struct{}{}
	}
	for _, msg := range contract.Messages {
		if _, ok := seenCallables[msg.Name]; ok {
			return fail("E_DUP_CALLABLE", msg.Pos, fmt.Sprintf("duplicate callable name %q", msg.Name))
		}
		seenCallables[msg.Name] = struct{}{}
		if err := c.validateMessage(msg, contract, storage, structs, enums, functions); err != nil {
			return err
		}
	}
	for _, get := range contract.Getters {
		if _, ok := seenCallables[get.Name]; ok {
			return fail("E_DUP_CALLABLE", get.Pos, fmt.Sprintf("duplicate callable name %q", get.Name))
		}
		seenCallables[get.Name] = struct{}{}
		if err := c.validateGetter(get, contract, storage, structs, enums, functions); err != nil {
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
		if err := c.validateEvent(event, structs, enums); err != nil {
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
	if err := validateFunctionRecursion(file.Functions); err != nil {
		return err
	}
	return nil
}

func (c *Compiler) validateFunction(fn *FunctionDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, functions map[string]*FunctionDecl) error {
	if fn == nil {
		return fail("E_FUNCTION", Position{}, "nil function")
	}
	if err := c.validateCallableName(fn.Name, fn.Pos); err != nil {
		return err
	}
	if err := validateParamNames(fn.Params, "function "+fn.Name, fn.Pos); err != nil {
		return err
	}
	if err := c.validateType(fn.ReturnType, structs, enums); err != nil {
		return err
	}
	env := c.buildEnv(fn.Params, nil, structs, enums)
	for _, stmt := range fn.Body {
		if err := c.validateStatement(stmt, env, map[string]constValue{}, nil, structs, enums, &fn.ReturnType, functions, true); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) validateStruct(st *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl) error {
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
		if err := c.validateType(field.Type, structs, enums); err != nil {
			return err
		}
		if field.Default.Kind != "" {
			typ, err := c.inferExprType(field.Default, nil, st, structs, enums, nil, false)
			if err != nil {
				return err
			}
			if !compatibleTypes(typ, field.Type) {
				return fail("E_DEFAULT_TYPE", field.Pos, fmt.Sprintf("default for %s.%s has incompatible type %s", st.Name, field.Name, typ.String()))
			}
		}
	}
	return nil
}

func (c *Compiler) validateEnum(en *EnumDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl) error {
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
			if err := c.validateType(field.Type, structs, enums); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Compiler) validateMessage(msg *MessageDecl, contract *ContractDecl, storage *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, functions map[string]*FunctionDecl) error {
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
		if err := c.validateType(param.Type, structs, enums); err != nil {
			return err
		}
	}
	if msg.ReturnType != nil {
		if err := c.validateType(*msg.ReturnType, structs, enums); err != nil {
			return err
		}
	}
	env := c.buildEnv(msg.Params, storage, structs, enums)
	for _, stmt := range msg.Body {
		if err := c.validateStatement(stmt, env, map[string]constValue{}, storage, structs, enums, msg.ReturnType, functions, false); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) validateGetter(get *GetterDecl, contract *ContractDecl, storage *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, functions map[string]*FunctionDecl) error {
	if get == nil {
		return fail("E_GETTER", Position{}, "nil getter")
	}
	if err := c.validateCallableName(get.Name, get.Pos); err != nil {
		return err
	}
	if err := validateParamNames(get.Params, "getter "+get.Name, get.Pos); err != nil {
		return err
	}
	if err := c.validateType(get.ReturnType, structs, enums); err != nil {
		return err
	}
	env := c.buildEnv(get.Params, storage, structs, enums)
	for _, stmt := range get.Body {
		if err := c.validateStatement(stmt, env, map[string]constValue{}, storage, structs, enums, &get.ReturnType, functions, true); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) validateEvent(event *EventDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl) error {
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
		if err := c.validateType(field.Type, structs, enums); err != nil {
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
	if strings.TrimSpace(wallet.Risk) == "" {
		return fail("E_WALLET_RISK", wallet.Pos, fmt.Sprintf("wallet action %q requires risk", wallet.Name))
	}
	if strings.TrimSpace(wallet.ConfirmLabel) == "" {
		return fail("E_WALLET_CONFIRM", wallet.Pos, fmt.Sprintf("wallet action %q requires confirm label", wallet.Name))
	}
	if strings.TrimSpace(wallet.WarningLevel) == "" {
		return fail("E_WALLET_WARNING", wallet.Pos, fmt.Sprintf("wallet action %q requires warning level", wallet.Name))
	}
	if strings.TrimSpace(wallet.ApprovalSemantics) == "" {
		return fail("E_WALLET_APPROVAL", wallet.Pos, fmt.Sprintf("wallet action %q requires approval semantics", wallet.Name))
	}
	return nil
}

func (c *Compiler) validateCallableName(name string, pos Position) error {
	if !isValidName(name) {
		return fail("E_NAME", pos, fmt.Sprintf("invalid callable name %q", name))
	}
	return nil
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

func (c *Compiler) validateType(typ TypeRef, structs map[string]*StructDecl, enums map[string]*EnumDecl) error {
	if typ.Name == "" {
		return fail("E_TYPE", typ.Pos, "empty type")
	}
	switch strings.ToLower(typ.Name) {
	case "bool", "u8", "u16", "u32", "u64", "i64", "bytes", "string", "hash32":
	default:
		if desc, ok := standards.DefaultRegistry().Find(typ.Name); ok {
			if desc.Arity != len(typ.Args) {
				return fail("E_TYPE_ARITY", typ.Pos, fmt.Sprintf("type %q requires %d type arguments", typ.Name, desc.Arity))
			}
		} else if _, ok := structs[typ.Name]; !ok {
			if _, ok := enums[typ.Name]; !ok {
				return fail("E_UNKNOWN_TYPE", typ.Pos, fmt.Sprintf("unknown type %q", typ.Name))
			}
		}
	}
	for _, arg := range typ.Args {
		if err := c.validateType(arg, structs, enums); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) validateStatement(stmt Statement, env map[string]TypeRef, consts map[string]constValue, storage *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, ret *TypeRef, functions map[string]*FunctionDecl, inPure bool) error {
	switch stmt.Kind {
	case StatementLet:
		typ, err := c.inferExprType(stmt.Value, env, storage, structs, enums, functions, inPure)
		if err != nil {
			return err
		}
		env[stmt.Name] = typ
		if value, ok := evalConstValue(stmt.Value, loweringEnv{params: map[string]int{}, consts: consts}, functions, map[string]bool{}); ok {
			consts[stmt.Name] = value
		}
		return nil
	case StatementSet:
		if inPure {
			return fail("E_PURE_MUTATION", stmt.Pos, "pure functions cannot write state")
		}
		if len(stmt.Path) < 2 || stmt.Path[0] != "state" {
			return fail("E_SET_PATH", stmt.Pos, "set statements must target state.<field>")
		}
		fieldType, ok := lookupStructField(storage, stmt.Path[1])
		if !ok {
			return fail("E_SET_FIELD", stmt.Pos, fmt.Sprintf("storage field %q not found", stmt.Path[1]))
		}
		typ, err := c.inferExprType(stmt.Value, env, storage, structs, enums, functions, inPure)
		if err != nil {
			return err
		}
		if !compatibleTypes(typ, fieldType) {
			return fail("E_SET_TYPE", stmt.Pos, fmt.Sprintf("cannot assign %s to %s", typ.String(), fieldType.String()))
		}
		return nil
	case StatementEmit:
		if inPure {
			return fail("E_PURE_EFFECT", stmt.Pos, "pure functions cannot emit events")
		}
		return nil
	case StatementReturn:
		typ, err := c.inferExprType(stmt.Value, env, storage, structs, enums, functions, inPure)
		if err != nil {
			return err
		}
		if ret != nil && !compatibleTypes(typ, *ret) {
			return fail("E_RETURN_TYPE", stmt.Pos, fmt.Sprintf("return type %s does not match %s", typ.String(), ret.String()))
		}
		return nil
	case StatementRefund, StatementSend, StatementSelf:
		if inPure {
			return fail("E_PURE_EFFECT", stmt.Pos, "pure functions cannot send/refund/schedule self")
		}
		return nil
	case StatementIf:
		typ, err := c.inferExprType(stmt.Value, env, storage, structs, enums, functions, inPure)
		if err != nil {
			return err
		}
		if !strings.EqualFold(typ.Name, "bool") {
			return fail("E_IF_COND", stmt.Pos, "if condition must be bool")
		}
		thenEnv := cloneTypeEnv(env)
		thenConsts := cloneConstEnv(consts)
		for _, inner := range stmt.Then {
			if err := c.validateStatement(inner, thenEnv, thenConsts, storage, structs, enums, ret, functions, inPure); err != nil {
				return err
			}
		}
		elseEnv := cloneTypeEnv(env)
		elseConsts := cloneConstEnv(consts)
		for _, inner := range stmt.Else {
			if err := c.validateStatement(inner, elseEnv, elseConsts, storage, structs, enums, ret, functions, inPure); err != nil {
				return err
			}
		}
		return nil
	case StatementMatch:
		scrutineeType, err := c.inferExprType(stmt.Value, env, storage, structs, enums, functions, inPure)
		if err != nil {
			return err
		}
		if err := c.validateMatchStatement(stmt, scrutineeType, env, consts, storage, structs, enums, ret, functions, inPure); err != nil {
			return err
		}
		return nil
	case StatementFor:
		startTyp, err := c.inferExprType(stmt.Start, env, storage, structs, enums, functions, inPure)
		if err != nil {
			return err
		}
		endTyp, err := c.inferExprType(stmt.End, env, storage, structs, enums, functions, inPure)
		if err != nil {
			return err
		}
		if !isNumericType(startTyp) || !isNumericType(endTyp) {
			return fail("E_FOR_BOUNDS", stmt.Pos, "for bounds must be numeric")
		}
		bodyEnv := cloneTypeEnv(env)
		bodyConsts := cloneConstEnv(consts)
		bodyEnv[stmt.Index] = TypeRef{Name: "u64"}
		for _, inner := range stmt.Then {
			if err := c.validateStatement(inner, bodyEnv, bodyConsts, storage, structs, enums, ret, functions, inPure); err != nil {
				return err
			}
		}
		return nil
	default:
		return fail("E_STMT", stmt.Pos, fmt.Sprintf("unsupported statement kind %q", stmt.Kind))
	}
}

func (c *Compiler) validateMatchStatement(stmt Statement, scrutineeType TypeRef, env map[string]TypeRef, consts map[string]constValue, storage *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, ret *TypeRef, functions map[string]*FunctionDecl, inPure bool) error {
	exhaustive := false
	covered := map[string]struct{}{}
	if en, ok := enums[scrutineeType.Name]; ok {
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
						found = true
						variant = &en.Variants[i]
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
				if len(arm.Pattern.Bindings) != len(variant.Fields) {
					return fail("E_MATCH_BINDINGS", arm.Pos, fmt.Sprintf("variant %s.%s expects %d bindings", en.Name, patternName, len(variant.Fields)))
				}
				armEnv := cloneTypeEnv(env)
				armConsts := cloneConstEnv(consts)
				for i, bind := range arm.Pattern.Bindings {
					armEnv[bind] = variant.Fields[i].Type
				}
				for _, inner := range arm.Body {
					if err := c.validateStatement(inner, armEnv, armConsts, storage, structs, enums, ret, functions, inPure); err != nil {
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
		for _, arm := range stmt.Arms {
			switch arm.Pattern.Kind {
			case PatternWildcard:
				exhaustive = true
			case PatternName:
				if patternTail(arm.Pattern.Name) != st.Name {
					return fail("E_MATCH_STRUCT", arm.Pos, fmt.Sprintf("struct match must use %s pattern or _", st.Name))
				}
				if len(arm.Pattern.Bindings) != len(st.Fields) {
					return fail("E_MATCH_BINDINGS", arm.Pos, fmt.Sprintf("struct %s expects %d bindings", st.Name, len(st.Fields)))
				}
				armEnv := cloneTypeEnv(env)
				armConsts := cloneConstEnv(consts)
				for i, bind := range arm.Pattern.Bindings {
					armEnv[bind] = st.Fields[i].Type
				}
				for _, inner := range arm.Body {
					if err := c.validateStatement(inner, armEnv, armConsts, storage, structs, enums, ret, functions, inPure); err != nil {
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
	return fail("E_MATCH_TYPE", stmt.Pos, fmt.Sprintf("match scrutinee %s is not an enum or struct", scrutineeType.String()))
}

func cloneTypeEnv(env map[string]TypeRef) map[string]TypeRef {
	out := make(map[string]TypeRef, len(env))
	for k, v := range env {
		out[k] = v
	}
	return out
}

func patternTail(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 && i+1 < len(name) {
		return name[i+1:]
	}
	return name
}

func validateFunctionRecursion(funcs []*FunctionDecl) error {
	callGraph := map[string][]string{}
	for _, fn := range funcs {
		callGraph[fn.Name] = append(callGraph[fn.Name], collectFunctionCallsFromStatements(fn.Body)...)
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

func collectFunctionCallsFromStatements(stmts []Statement) []string {
	var out []string
	for _, stmt := range stmts {
		out = append(out, collectFunctionCallsFromStatement(stmt)...)
	}
	return out
}

func collectFunctionCallsFromStatement(stmt Statement) []string {
	var out []string
	out = append(out, collectFunctionCallsFromExpr(stmt.Value)...)
	out = append(out, collectFunctionCallsFromExpr(stmt.Start)...)
	out = append(out, collectFunctionCallsFromExpr(stmt.End)...)
	for _, arg := range stmt.Args {
		out = append(out, collectFunctionCallsFromExpr(arg)...)
	}
	for _, ex := range stmt.Extra {
		out = append(out, collectFunctionCallsFromExpr(ex)...)
	}
	for _, inner := range stmt.Then {
		out = append(out, collectFunctionCallsFromStatement(inner)...)
	}
	for _, inner := range stmt.Else {
		out = append(out, collectFunctionCallsFromStatement(inner)...)
	}
	for _, arm := range stmt.Arms {
		for _, inner := range arm.Body {
			out = append(out, collectFunctionCallsFromStatement(inner)...)
		}
	}
	return out
}

func collectFunctionCallsFromExpr(expr Expr) []string {
	var out []string
	switch expr.Kind {
	case ExprCall:
		if expr.Text != "hash" && expr.Text != "len" && expr.Text != "ok" && expr.Text != "err" {
			out = append(out, expr.Text)
		}
		for _, arg := range expr.Args {
			out = append(out, collectFunctionCallsFromExpr(arg)...)
		}
	case ExprBinary, ExprCompare, ExprLogic:
		if expr.Left != nil {
			out = append(out, collectFunctionCallsFromExpr(*expr.Left)...)
		}
		if expr.Right != nil {
			out = append(out, collectFunctionCallsFromExpr(*expr.Right)...)
		}
	case ExprTry:
		if expr.Left != nil {
			out = append(out, collectFunctionCallsFromExpr(*expr.Left)...)
		}
		if expr.Else != nil {
			out = append(out, collectFunctionCallsFromExpr(*expr.Else)...)
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
	return env
}

func (c *Compiler) inferExprType(expr Expr, env map[string]TypeRef, storage *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, functions map[string]*FunctionDecl, inPure bool) (TypeRef, error) {
	switch expr.Kind {
	case ExprNumber:
		return TypeRef{Name: "u64"}, nil
	case ExprString:
		return TypeRef{Name: "string"}, nil
	case ExprBool:
		return TypeRef{Name: "bool"}, nil
	case ExprIdent:
		if env != nil {
			if typ, ok := env[expr.Text]; ok {
				return typ, nil
			}
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
		return c.resolvePathType(expr.Path, env, storage, structs, enums, expr.Pos)
	case ExprCall:
		switch strings.ToLower(expr.Text) {
		case "hash":
			return TypeRef{Name: "hash32"}, nil
		case "len":
			return TypeRef{Name: "u64"}, nil
		case "ok":
			if len(expr.Args) != 1 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "ok() requires one argument")
			}
			argType, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, functions, inPure)
			if err != nil {
				return TypeRef{}, err
			}
			return TypeRef{Name: "Option", Args: []TypeRef{argType}}, nil
		case "err":
			if len(expr.Args) != 1 {
				return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, "err() requires one argument")
			}
			argType, err := c.inferExprType(expr.Args[0], env, storage, structs, enums, functions, inPure)
			if err != nil {
				return TypeRef{}, err
			}
			return TypeRef{Name: "Result", Args: []TypeRef{{Name: "u64"}, argType}}, nil
		default:
			if fn, ok := functions[expr.Text]; ok {
				if len(expr.Args) != len(fn.Params) {
					return TypeRef{}, fail("E_CALL_ARITY", expr.Pos, fmt.Sprintf("function %q expects %d args", fn.Name, len(fn.Params)))
				}
				for i, arg := range expr.Args {
					argType, err := c.inferExprType(arg, env, storage, structs, enums, functions, inPure)
					if err != nil {
						return TypeRef{}, err
					}
					if !compatibleTypes(argType, fn.Params[i].Type) {
						return TypeRef{}, fail("E_CALL_TYPE", arg.Pos, fmt.Sprintf("argument %q has type %s, want %s", fn.Params[i].Name, argType.String(), fn.Params[i].Type.String()))
					}
				}
				return fn.ReturnType, nil
			}
			return TypeRef{}, fail("E_CALL", expr.Pos, fmt.Sprintf("unknown function %q", expr.Text))
		}
	case ExprBinary:
		left, err := c.inferExprType(*expr.Left, env, storage, structs, enums, functions, inPure)
		if err != nil {
			return TypeRef{}, err
		}
		right, err := c.inferExprType(*expr.Right, env, storage, structs, enums, functions, inPure)
		if err != nil {
			return TypeRef{}, err
		}
		if !compatibleTypes(left, right) {
			return TypeRef{}, fail("E_BINARY_TYPE", expr.Pos, fmt.Sprintf("binary %q requires matching types, got %s and %s", expr.Op, left.String(), right.String()))
		}
		if !isNumericType(left) && !strings.EqualFold(left.Name, "string") && !strings.EqualFold(left.Name, "bytes") {
			return TypeRef{}, fail("E_BINARY_TYPE", expr.Pos, fmt.Sprintf("binary %q not supported on %s", expr.Op, left.String()))
		}
		return left, nil
	case ExprCompare, ExprLogic:
		return TypeRef{Name: "bool"}, nil
	case ExprTry:
		t, err := c.inferExprType(*expr.Left, env, storage, structs, enums, functions, inPure)
		if err != nil {
			return TypeRef{}, err
		}
		if expr.Else != nil {
			t2, err := c.inferExprType(*expr.Else, env, storage, structs, enums, functions, inPure)
			if err != nil {
				return TypeRef{}, err
			}
			if !compatibleTypes(t, t2) {
				return TypeRef{}, fail("E_TRY_TYPE", expr.Pos, "try branches must have matching types")
			}
			return t, nil
		}
		return t, nil
	default:
		return TypeRef{}, fail("E_EXPR", expr.Pos, fmt.Sprintf("unsupported expression kind %q", expr.Kind))
	}
}

func (c *Compiler) resolvePathType(path []string, env map[string]TypeRef, storage *StructDecl, structs map[string]*StructDecl, enums map[string]*EnumDecl, pos Position) (TypeRef, error) {
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
		if current.Name == storage.Name {
			fieldType, ok := lookupStructField(storage, path[i])
			if !ok {
				return TypeRef{}, fail("E_PATH_FIELD", pos, fmt.Sprintf("storage field %q not found", path[i]))
			}
			current = fieldType
			continue
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

func lookupStructField(st *StructDecl, name string) (TypeRef, bool) {
	for _, field := range st.Fields {
		if field.Name == name {
			return field.Type, true
		}
	}
	return TypeRef{}, false
}

func compatibleTypes(a, b TypeRef) bool {
	if strings.EqualFold(a.Name, b.Name) {
		if a.Optional != b.Optional {
			return false
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
	if strings.EqualFold(a.Name, "string") && isStringLikeType(b) {
		return true
	}
	if strings.EqualFold(b.Name, "string") && isStringLikeType(a) {
		return true
	}
	return false
}

func isNumericType(t TypeRef) bool {
	switch strings.ToLower(t.Name) {
	case "u8", "u16", "u32", "u64", "i64", "coins":
		return true
	default:
		return false
	}
}

func isStringLikeType(t TypeRef) bool {
	switch strings.ToLower(t.Name) {
	case "bytes", "hash32", "address":
		return true
	default:
		return false
	}
}

func (c *Compiler) buildArtifacts(file *SourceFile, contract *ContractDecl) (avm.InterfaceManifest, SelectorRegistry, StorageLayout, Codec, map[string]Codec, map[string]Codec, map[string]Codec, error) {
	storage, _ := findStruct(file.Structs, contract.StorageTypeName)
	layout, storageCodec := buildStorageLayout(storage)
	msgCodecs := map[string]Codec{}
	getterCodecs := map[string]Codec{}
	eventCodecs := map[string]Codec{}
	registry := SelectorRegistry{Contract: contract.Name}
	manifest := avm.InterfaceManifest{Name: contract.Name, Version: DefaultABIVersion}

	seenSelectors := map[uint32]string{}
	appendSelector := func(kind, name, signature string, selector uint32, topic string, entrypoint string) error {
		if prev, ok := seenSelectors[selector]; ok && prev != signature {
			return fail("E_SELECTOR_COLLISION", Position{}, fmt.Sprintf("selector collision for 0x%08x between %q and %q", selector, prev, signature))
		}
		seenSelectors[selector] = signature
		registry.Entries = append(registry.Entries, SelectorEntry{Kind: kind, Name: name, Signature: signature, Selector: selector, Topic: topic, Entrypoint: entrypoint})
		return nil
	}

	sort.SliceStable(contract.Messages, func(i, j int) bool {
		if contract.Messages[i].Kind != contract.Messages[j].Kind {
			return messageKindOrder(contract.Messages[i].Kind) < messageKindOrder(contract.Messages[j].Kind)
		}
		if contract.Messages[i].Name != contract.Messages[j].Name {
			return contract.Messages[i].Name < contract.Messages[j].Name
		}
		return signatureForMessage(contract.Messages[i]) < signatureForMessage(contract.Messages[j])
	})
	for _, msg := range contract.Messages {
		sig := signatureForMessage(msg)
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
			return avm.InterfaceManifest{}, SelectorRegistry{}, StorageLayout{}, Codec{}, nil, nil, nil, err
		}
		msgCodecs[msg.Name] = Codec{Name: msg.Name, Kind: "message", Fields: paramsToCodecFields(msg.Params), ReturnType: msg.ReturnType, Hash: sha256.Sum256([]byte(sig))}
	}

	sort.SliceStable(contract.Getters, func(i, j int) bool {
		if contract.Getters[i].Name != contract.Getters[j].Name {
			return contract.Getters[i].Name < contract.Getters[j].Name
		}
		return signatureForGetter(contract.Getters[i]) < signatureForGetter(contract.Getters[j])
	})
	for _, get := range contract.Getters {
		sig := signatureForGetter(get)
		sel := selectorFromSignature(sig)
		if get.ExplicitSel != nil {
			sel = *get.ExplicitSel
		}
		manifest.GetMethods = append(manifest.GetMethods, avm.InterfaceGetMethod{
			Name: get.Name, Entrypoint: avm.EntryQuery, Selector: sel, Params: paramsToInterface(get.Params), Results: []avm.InterfaceResultDescriptor{{Name: "result", Kind: interfaceKindForType(get.ReturnType), Required: true}}, Cacheable: true, MaxResponseBytes: c.opts.MaxPayloadBytes, Description: "",
		})
		if err := appendSelector("getter", get.Name, sig, sel, "", entrypointName(avm.EntryQuery)); err != nil {
			return avm.InterfaceManifest{}, SelectorRegistry{}, StorageLayout{}, Codec{}, nil, nil, nil, err
		}
		ret := get.ReturnType
		getterCodecs[get.Name] = Codec{Name: get.Name, Kind: "getter", Fields: paramsToCodecFields(get.Params), ReturnType: &ret, Hash: sha256.Sum256([]byte(sig))}
	}

	sort.SliceStable(contract.Events, func(i, j int) bool { return contract.Events[i].Name < contract.Events[j].Name })
	for _, event := range contract.Events {
		sig := signatureForEvent(event)
		hash := sha256.Sum256([]byte(sig))
		sel := binary.BigEndian.Uint32(hash[:4])
		manifest.Events = append(manifest.Events, avm.InterfaceEvent{Name: event.Name, Opcode: sel, Fields: paramsToInterface(event.Fields)})
		if err := appendSelector("event", event.Name, sig, sel, fmt.Sprintf("%x", hash[:]), "event"); err != nil {
			return avm.InterfaceManifest{}, SelectorRegistry{}, StorageLayout{}, Codec{}, nil, nil, nil, err
		}
		eventCodecs[event.Name] = Codec{Name: event.Name, Kind: "event", Fields: paramsToCodecFields(event.Fields), Hash: hash}
	}

	sort.SliceStable(contract.WalletActions, func(i, j int) bool { return contract.WalletActions[i].Name < contract.WalletActions[j].Name })
	for _, wallet := range contract.WalletActions {
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
	}

	manifest.CLIBindings = buildCLIBindings(contract)
	manifest.SDKBindings = buildSDKBindings(contract)
	if manifest.Name == "" {
		manifest.Name = contract.Name
	}

	if _, err := avm.InterfaceHash(manifest); err != nil {
		return avm.InterfaceManifest{}, SelectorRegistry{}, StorageLayout{}, Codec{}, nil, nil, nil, err
	}
	registry.RegistryHash = sha256.Sum256(registryHashBytes(registry))
	layout.LayoutHash = sha256.Sum256(layoutHashBytes(layout))
	storageCodec.Hash = sha256.Sum256(codecHashBytes(storageCodec))
	return manifest, registry, layout, storageCodec, msgCodecs, getterCodecs, eventCodecs, nil
}

func buildStorageLayout(storage *StructDecl) (StorageLayout, Codec) {
	layout := StorageLayout{Name: storage.Name}
	codec := Codec{Name: storage.Name, Kind: "storage"}
	for _, field := range storage.Fields {
		layout.Fields = append(layout.Fields, CodecField{Name: field.Name, Type: field.Type, Default: field.Default, Pos: field.Pos})
		codec.Fields = append(codec.Fields, CodecField{Name: field.Name, Type: field.Type, Default: field.Default, Pos: field.Pos})
	}
	return layout, codec
}

func (c *Compiler) buildModule(file *SourceFile, contract *ContractDecl, manifest avm.InterfaceManifest, registry SelectorRegistry, msgCodecs map[string]Codec, getterCodecs map[string]Codec, eventCodecs map[string]Codec, lock DependencyLock, functions map[string]*FunctionDecl) (avm.Module, []byte, *IRProgram, error) {
	ir, err := c.buildIR(file, contract, registry, lock, functions)
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
	for _, entry := range ir.Entries {
		if _, exists := module.Exports[entry.Entrypoint]; exists {
			return avm.Module{}, nil, nil, fail("E_ENTRY_DISPATCH", entry.Pos, fmt.Sprintf("multiple handlers lower to AVM entrypoint %s; selector dispatch opcode is not available in AVM v1", entrypointName(entry.Entrypoint)))
		}
		module.Exports[entry.Entrypoint] = uint32(len(code))
		lowered, err := c.lowerIREntry(entry)
		if err != nil {
			return avm.Module{}, nil, nil, err
		}
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

func (c *Compiler) buildIR(file *SourceFile, contract *ContractDecl, registry SelectorRegistry, lock DependencyLock, functions map[string]*FunctionDecl) (*IRProgram, error) {
	program := &IRProgram{
		Contract:         contract.Name,
		Package:          file.Package,
		TraceCommitments: map[string]string{},
		Dependencies:     append([]ResolvedDependency(nil), lock.Entries...),
		LoweringRules:    StatementLoweringRules(),
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
				Selector:   selectorForMessage(msg),
				Statements: []IRStmt{{Kind: IRStmtTrace, Data: commit[:], Position: msg.Pos}},
				Pos:        msg.Pos,
			}
			body, err := c.lowerStatementsToIR(msg.Body, msg.Params, msg.ReturnType, false, true, functions, loweringEnv{})
			if err != nil {
				return nil, err
			}
			entry.Statements = append(entry.Statements, body...)
			program.TraceCommitments[entrypointName(entry.Entrypoint)+":"+entry.Name] = fmt.Sprintf("%x", commit[:])
			program.Entries = append(program.Entries, entry)
		}
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
			Selector:   selectorForGetter(get),
			Statements: []IRStmt{{Kind: IRStmtTrace, Data: commit[:], Position: get.Pos}},
			Pos:        get.Pos,
		}
		body, err := c.lowerStatementsToIR(get.Body, get.Params, &ret, true, true, functions, loweringEnv{})
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
	return program, nil
}

func (c *Compiler) lowerStatementsToIR(stmts []Statement, params []ParamDecl, ret *TypeRef, readOnly bool, ensureReturn bool, functions map[string]*FunctionDecl, envInit loweringEnv) ([]IRStmt, error) {
	env := loweringEnv{params: map[string]int{}, consts: map[string]constValue{}}
	for i, param := range params {
		env.params[param.Name] = i
	}
	for name, value := range envInit.consts {
		env.consts[name] = value
	}
	var out []IRStmt
	for _, stmt := range stmts {
		switch stmt.Kind {
		case StatementLet:
			v, ok := evalConstValue(stmt.Value, env, functions, map[string]bool{})
			if !ok {
				return nil, fail("E_LOWER_LOCAL", stmt.Pos, "AVM v1 lowering only supports compile-time constant let bindings")
			}
			env.consts[stmt.Name] = v
			var arg uint64
			if v.Kind == constKindU64 {
				arg = v.U64
			}
			out = append(out, IRStmt{Kind: IRStmtLetConst, Name: stmt.Name, Arg: arg, Position: stmt.Pos})
		case StatementSet:
			if readOnly {
				return nil, fail("E_GETTER_MUTATION", stmt.Pos, "getter cannot write storage")
			}
			if len(stmt.Path) != 2 || stmt.Path[0] != "state" {
				return nil, fail("E_LOWER_SET", stmt.Pos, "AVM v1 lowering supports only direct state.<field> writes")
			}
			expr, err := lowerExprToIR(stmt.Value, env, functions, map[string]bool{})
			if err != nil {
				return nil, err
			}
			out = append(out, IRStmt{Kind: IRStmtStoreState, Key: stmt.Path[1], Expr: expr, Position: stmt.Pos})
		case StatementEmit:
			out = append(out, IRStmt{Kind: IRStmtTrace, Name: stmt.Name, Data: eventTraceData(stmt), Position: stmt.Pos})
		case StatementSend:
			if readOnly {
				return nil, fail("E_GETTER_SEND", stmt.Pos, "getter cannot send internal messages")
			}
			opcode, err := staticOpcode(stmt)
			if err != nil {
				return nil, err
			}
			out = append(out, IRStmt{Kind: IRStmtEmitInternal, Opcode: opcode, Data: statementTraceData(stmt), Position: stmt.Pos})
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
				return nil, fail("E_LOWER_SELF", stmt.Pos, "self delay must be a positive u64 constant")
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
			cond, ok := evalConstBool(stmt.Value, env, functions, map[string]bool{})
			if !ok {
				return nil, fail("E_LOWER_IF", stmt.Pos, "if condition must be compile-time constant on AVM v1")
			}
			var branch []Statement
			if cond {
				branch = stmt.Then
			} else {
				branch = stmt.Else
			}
			branchIR, err := c.lowerStatementsToIR(branch, params, ret, readOnly, false, functions, env)
			if err != nil {
				return nil, err
			}
			out = append(out, branchIR...)
		case StatementMatch:
			tag, ok := evalConstEnum(stmt.Value, env, functions, map[string]bool{})
			if !ok {
				return nil, fail("E_LOWER_MATCH", stmt.Pos, "match scrutinee must be compile-time constant on AVM v1")
			}
			var matched []Statement
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
			if matched == nil {
				return nil, fail("E_LOWER_MATCH", stmt.Pos, fmt.Sprintf("no match arm for enum variant %s", tag))
			}
		matchedBranch:
			branchIR, err := c.lowerStatementsToIR(matched, params, ret, readOnly, false, functions, env)
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
			for i := start; i < end; i++ {
				iterEnv := loweringEnv{params: env.params, consts: cloneConstEnv(env.consts)}
				iterEnv.consts[stmt.Index] = constValue{Kind: constKindU64, U64: i}
				bodyIR, err := c.lowerStatementsToIR(stmt.Then, params, ret, readOnly, false, functions, iterEnv)
				if err != nil {
					return nil, err
				}
				out = append(out, bodyIR...)
			}
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
	for _, stmt := range entry.Statements {
		switch stmt.Kind {
		case IRStmtTrace:
			code = append(code, avm.Instruction{Op: avm.OpNop, Data: append([]byte(nil), stmt.Data...)})
		case IRStmtLetConst:
		case IRStmtStoreState:
			if err := emitIRExpr(stmt.Expr, &code); err != nil {
				return nil, err
			}
			code = append(code, avm.Instruction{Op: avm.OpWriteStorage, Data: []byte(stmt.Key)})
		case IRStmtEmitInternal:
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
	return code, nil
}

const refundOpcode uint32 = 0xfffffff0

type loweringEnv struct {
	params map[string]int
	consts map[string]constValue
}

type constValueKind string

const (
	constKindU64  constValueKind = "u64"
	constKindBool constValueKind = "bool"
	constKindEnum constValueKind = "enum"
	constKindText constValueKind = "text"
)

type constValue struct {
	Kind constValueKind
	U64  uint64
	Bool bool
	Text string
	Type string
}

func lowerExprToIR(expr Expr, env loweringEnv, functions map[string]*FunctionDecl, seen map[string]bool) (*IRExpr, error) {
	switch expr.Kind {
	case ExprNumber:
		v, ok := constU64(expr)
		if !ok {
			return nil, fail("E_LOWER_EXPR", expr.Pos, "invalid u64 literal")
		}
		return &IRExpr{Kind: IRExprConstU64, Value: v, Pos: expr.Pos}, nil
	case ExprBool:
		if expr.Bool {
			return &IRExpr{Kind: IRExprConstU64, Value: 1, Pos: expr.Pos}, nil
		}
		return &IRExpr{Kind: IRExprConstU64, Pos: expr.Pos}, nil
	case ExprIdent:
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
		}
		switch strings.ToLower(expr.Text) {
		case "opcode":
			return &IRExpr{Kind: IRExprMsgOpcode, Pos: expr.Pos}, nil
		case "query_id":
			return &IRExpr{Kind: IRExprMsgQueryID, Pos: expr.Pos}, nil
		case "block_height":
			return &IRExpr{Kind: IRExprBlockHeight, Pos: expr.Pos}, nil
		}
		if idx, ok := env.params[expr.Text]; ok {
			if idx == 0 {
				return &IRExpr{Kind: IRExprMsgQueryID, Pos: expr.Pos}, nil
			}
			return nil, fail("E_LOWER_PARAM", expr.Pos, fmt.Sprintf("parameter %q cannot be decoded by AVM v1; only the first u64 parameter is mapped to query_id", expr.Text))
		}
		return nil, fail("E_LOWER_IDENT", expr.Pos, fmt.Sprintf("identifier %q is not lowerable", expr.Text))
	case ExprPath:
		if len(expr.Path) == 2 && expr.Path[0] == "state" {
			return &IRExpr{Kind: IRExprStateRead, Key: expr.Path[1], Pos: expr.Pos}, nil
		}
		return nil, fail("E_LOWER_PATH", expr.Pos, "AVM v1 lowering supports only state.<field> reads")
	case ExprCall:
		value, ok := evalConstExpr(expr, env, functions, seen)
		if !ok {
			return nil, fail("E_LOWER_CALL", expr.Pos, fmt.Sprintf("call %q must be compile-time constant on AVM v1", expr.Text))
		}
		switch value.Kind {
		case constKindU64:
			return &IRExpr{Kind: IRExprConstU64, Value: value.U64, Pos: expr.Pos}, nil
		case constKindBool:
			if value.Bool {
				return &IRExpr{Kind: IRExprConstU64, Value: 1, Pos: expr.Pos}, nil
			}
			return &IRExpr{Kind: IRExprConstU64, Pos: expr.Pos}, nil
		default:
			return nil, fail("E_LOWER_CALL", expr.Pos, "only numeric/boolean constant calls can be lowered")
		}
	case ExprBinary:
		if expr.Op != "+" {
			return nil, fail("E_LOWER_BINARY", expr.Pos, fmt.Sprintf("binary %q has no AVM v1 opcode", expr.Op))
		}
		left, err := lowerExprToIR(*expr.Left, env, functions, seen)
		if err != nil {
			return nil, err
		}
		right, err := lowerExprToIR(*expr.Right, env, functions, seen)
		if err != nil {
			return nil, err
		}
		return &IRExpr{Kind: IRExprAdd, Left: left, Right: right, Pos: expr.Pos}, nil
	case ExprCompare, ExprLogic, ExprTry:
		value, ok := evalConstExpr(expr, env, functions, seen)
		if !ok {
			return nil, fail("E_LOWER_EXPR", expr.Pos, fmt.Sprintf("expression %q must be compile-time constant on AVM v1", expr.Kind))
		}
		if value.Kind == constKindBool && value.Bool {
			return &IRExpr{Kind: IRExprConstU64, Value: 1, Pos: expr.Pos}, nil
		}
		return &IRExpr{Kind: IRExprConstU64, Pos: expr.Pos}, nil
	default:
		return nil, fail("E_LOWER_EXPR", expr.Pos, fmt.Sprintf("expression %q is ABI-only and cannot be executed by AVM v1", expr.Kind))
	}
}

func emitIRExpr(expr *IRExpr, code *[]avm.Instruction) error {
	if expr == nil {
		return nil
	}
	switch expr.Kind {
	case IRExprConstU64:
		*code = append(*code, avm.Instruction{Op: avm.OpPushU64, Arg: expr.Value})
	case IRExprStateRead:
		*code = append(*code, avm.Instruction{Op: avm.OpReadStorage, Data: []byte(expr.Key)})
	case IRExprAdd:
		if err := emitIRExpr(expr.Left, code); err != nil {
			return err
		}
		if err := emitIRExpr(expr.Right, code); err != nil {
			return err
		}
		*code = append(*code, avm.Instruction{Op: avm.OpAdd})
	case IRExprMsgOpcode:
		*code = append(*code, avm.Instruction{Op: avm.OpReadMsgOpcode})
	case IRExprMsgQueryID:
		*code = append(*code, avm.Instruction{Op: avm.OpReadMsgQueryID})
	case IRExprBlockHeight:
		*code = append(*code, avm.Instruction{Op: avm.OpReadBlock})
	default:
		return fail("E_LOWER_EXPR", expr.Pos, fmt.Sprintf("unsupported IR expression %q", expr.Kind))
	}
	return nil
}

func constU64(expr Expr) (uint64, bool) {
	switch expr.Kind {
	case ExprNumber:
		v, err := strconv.ParseUint(expr.Text, 10, 64)
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

func cloneConstEnv(in map[string]constValue) map[string]constValue {
	out := make(map[string]constValue, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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
	case ExprIdent:
		if v, ok := env.consts[expr.Text]; ok {
			return v, true
		}
		switch strings.ToLower(expr.Text) {
		case "true":
			return constValue{Kind: constKindBool, Bool: true}, true
		case "false":
			return constValue{Kind: constKindBool, Bool: false}, true
		}
		return constValue{}, false
	case ExprPath:
		if len(expr.Path) == 2 {
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
	case ExprCall:
		switch strings.ToLower(expr.Text) {
		case "hash", "len":
			return constValue{}, false
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
		case StatementLet:
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

func staticOpcode(stmt Statement) (uint32, error) {
	if stmt.Extra != nil {
		if expr, ok := stmt.Extra["opcode"]; ok {
			v, ok := constU64(expr)
			if !ok || v > uint64(^uint32(0)) {
				return 0, fail("E_LOWER_OPCODE", expr.Pos, "send opcode must be a u32 constant")
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

func selectorForMessage(msg *MessageDecl) uint32 {
	if msg.ExplicitSel != nil {
		return *msg.ExplicitSel
	}
	return selectorFromSignature(signatureForMessage(msg))
}

func selectorForGetter(get *GetterDecl) uint32 {
	if get.ExplicitSel != nil {
		return *get.ExplicitSel
	}
	return selectorFromSignature(signatureForGetter(get))
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
		out = append(out, canonicalHandle{
			Name:       msg.Name,
			Signature:  signatureForMessage(msg),
			Selector:   selectorFromSignature(signatureForMessage(msg)),
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
		sig := signatureForGetter(get)
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
	case ExprBool:
		return strconv.FormatBool(expr.Bool)
	case ExprPath:
		return strings.Join(expr.Path, ".")
	case ExprCall:
		return expr.Text + "(" + strings.Join(canonicalExprStrings(expr.Args), ",") + ")"
	case ExprBinary:
		return "(" + canonicalExprString(*expr.Left) + expr.Op + canonicalExprString(*expr.Right) + ")"
	case ExprCompare, ExprLogic:
		return "(" + canonicalExprString(*expr.Left) + expr.Op + canonicalExprString(*expr.Right) + ")"
	case ExprTry:
		out := "try(" + canonicalExprString(*expr.Left) + ")"
		if expr.Else != nil {
			out += "else(" + canonicalExprString(*expr.Else) + ")"
		}
		return out
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
	case "u8", "u16", "u32", "u64", "i64", "coins":
		return avm.InterfaceValueU64
	case "bytes", "hash32":
		return avm.InterfaceValueBytes
	case "string":
		return avm.InterfaceValueString
	case "address":
		return avm.InterfaceValueAddress
	default:
		return avm.InterfaceValueBytes
	}
}

func signatureForMessage(msg *MessageDecl) string {
	return "message " + msg.Kind.String() + " " + msg.Name + "(" + strings.Join(typeNamesFromParams(msg.Params), ",") + ")" + returnSignature(msg.ReturnType)
}

func signatureForGetter(get *GetterDecl) string {
	return "getter " + get.Name + "(" + strings.Join(typeNamesFromParams(get.Params), ",") + ")" + returnSignature(&get.ReturnType)
}

func signatureForEvent(event *EventDecl) string {
	return "event " + event.Name + "(" + strings.Join(typeNamesFromParams(event.FieldsToParams()), ",") + ")"
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

func buildChunkTree(data []byte) (*chunk.Chunk, error) {
	if len(data) == 0 {
		return chunk.NewEmptyChunk(), nil
	}
	if len(data) <= 256 {
		builder := chunk.NewBuilder().SetTypeTag(chunk.TypeNormal).SetData(data, uint16(len(data)*8))
		return builder.Build()
	}
	parts := splitBytes(data, 8)
	builder := chunk.NewBuilder().SetTypeTag(chunk.TypeNormal)
	for i, part := range parts {
		child, err := buildChunkTree(part)
		if err != nil {
			return nil, err
		}
		builder.SetRef(i, child)
	}
	return builder.Build()
}

func splitBytes(data []byte, maxParts int) [][]byte {
	if len(data) == 0 {
		return nil
	}
	if maxParts <= 1 {
		return [][]byte{append([]byte(nil), data...)}
	}
	partSize := (len(data) + maxParts - 1) / maxParts
	var out [][]byte
	for i := 0; i < len(data); i += partSize {
		end := i + partSize
		if end > len(data) {
			end = len(data)
		}
		out = append(out, append([]byte(nil), data[i:end]...))
	}
	for len(out) > 8 {
		next := make([][]byte, 0, 8)
		chunkSize := (len(out) + 7) / 8
		for i := 0; i < len(out); i += chunkSize {
			end := i + chunkSize
			if end > len(out) {
				end = len(out)
			}
			var merged []byte
			for _, part := range out[i:end] {
				merged = append(merged, part...)
			}
			next = append(next, merged)
		}
		out = next
	}
	return out
}

func encodeStateInit(si *avm.StateInit) ([]byte, error) {
	if si == nil {
		return nil, fmt.Errorf("nil state init")
	}
	return si.CanonicalEncode()
}
