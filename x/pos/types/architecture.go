package types

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

type CosmosSDKExtensionMode string

const (
	CosmosSDKExtensionModeExtend  CosmosSDKExtensionMode = "extend"
	CosmosSDKExtensionModeReplace CosmosSDKExtensionMode = "replace"
)

type CosmosSDKModuleExtension struct {
	ModuleName          string
	ModulePath          string
	ExtensionMode       CosmosSDKExtensionMode
	PreservedInterfaces []string
	AddedState          []string
	RewardInputs        []string
}

type PosModuleRequirement struct {
	ModuleName string
	ModulePath string
	Required   bool
}

type PosCompatibilityMiddleware struct {
	Name          string
	Layer         PosLayer
	Extends       []string
	ReadsModules  []string
	WritesModules []string
}

type CosmosSDKCompatibilityManifest struct {
	Extensions []CosmosSDKModuleExtension
	Modules    []PosModuleRequirement
	Middleware []PosCompatibilityMiddleware
	Root       string
}

type PosModuleBoundary struct {
	ModuleName     string
	ModulePath     string
	Owns           []string
	ReadsModules   []string
	WritesModules  []string
	QueryEndpoints []string
}

type PosModuleBoundaryManifest struct {
	Boundaries []PosModuleBoundary
	Root       string
}

type KeeperInterfaceSpec struct {
	KeeperName       string
	ModuleName       string
	InterfaceName    string
	IntegrationPoint string
	Reads            []string
	Writes           []string
}

type KeeperHookSpec struct {
	SourceKeeper       string
	HookName           string
	Trigger            string
	TargetModules      []string
	PreservesBaseState bool
	DeterministicOrder bool
}

type RewardMultiplierIntegration struct {
	SourceModule       string
	DistributionKeeper string
	MintKeeper         string
	MultiplierField    string
	RewardInputs       []string
}

type MigrationHandlerSpec struct {
	ModuleName                    string
	FromVersion                   uint64
	ToVersion                     uint64
	PreservesExistingStakingState bool
	ExportsGenesis                bool
	ImportsGenesis                bool
}

type ModuleExportImportSpec struct {
	ModuleName            string
	ExportsGenesis        bool
	ImportsGenesis        bool
	DeterministicEncoding bool
}

type KeeperIntegrationManifest struct {
	KeeperInterfaces      []KeeperInterfaceSpec
	StakingLifecycleHooks []KeeperHookSpec
	SlashingHooks         []KeeperHookSpec
	RewardIntegrations    []RewardMultiplierIntegration
	MigrationHandlers     []MigrationHandlerSpec
	ExportImport          []ModuleExportImportSpec
	Root                  string
}

type StateKeySpec struct {
	Domain     string
	Name       string
	Template   string
	Components []string
}

type StateModelManifest struct {
	Keys []StateKeySpec
	Root string
}

type PosLayer string

const (
	PosLayerEconomicConsensus  PosLayer = "economic_consensus"
	PosLayerTaskAssignment     PosLayer = "task_assignment"
	PosLayerValidatorExecution PosLayer = "validator_execution"
	PosLayerStakingCapital     PosLayer = "staking_capital"
	PosLayerBaseCometBFT       PosLayer = "base_cometbft"
)

type PosLayerSpec struct {
	Layer            PosLayer
	Responsibilities []string
	DependsOn        []PosLayer
}

type LayeredPosArchitecture struct {
	Layers []PosLayerSpec
	Root   string
}

func DefaultLayeredPosArchitecture() LayeredPosArchitecture {
	layers := []PosLayerSpec{
		{
			Layer: PosLayerEconomicConsensus,
			Responsibilities: []string{
				"validator scoring",
				"performance incentives",
				"stake saturation",
				"role-specific reward weights",
				"slashing severity",
				"reporter incentives",
				"treasury, burn, and stabilization routing",
			},
			DependsOn: []PosLayer{PosLayerTaskAssignment, PosLayerValidatorExecution, PosLayerStakingCapital, PosLayerBaseCometBFT},
		},
		{
			Layer: PosLayerTaskAssignment,
			Responsibilities: []string{
				"workload grouping",
				"shard validator groups",
				"zone validator groups",
				"evidence verification subsets",
				"collator and verifier assignments",
			},
			DependsOn: []PosLayer{PosLayerValidatorExecution, PosLayerStakingCapital, PosLayerBaseCometBFT},
		},
		{
			Layer: PosLayerValidatorExecution,
			Responsibilities: []string{
				"block production",
				"state transition verification",
				"cross-domain proof verification",
				"signature production",
				"fault rejection",
			},
			DependsOn: []PosLayer{PosLayerStakingCapital, PosLayerBaseCometBFT},
		},
		{
			Layer: PosLayerStakingCapital,
			Responsibilities: []string{
				"validators",
				"delegators",
				"bonded stake",
				"unbonding",
				"redelegation",
				"capital risk preferences",
				"commission and delegation market metadata",
			},
			DependsOn: []PosLayer{PosLayerBaseCometBFT},
		},
		{
			Layer: PosLayerBaseCometBFT,
			Responsibilities: []string{
				"finality",
				"proposal and vote protocol",
				"validator public key set",
				"consensus safety and liveness",
			},
		},
	}
	architecture := LayeredPosArchitecture{Layers: layers}
	architecture.Root = ComputeLayeredPosArchitectureRoot(layers)
	return architecture
}

func DefaultCosmosSDKCompatibilityManifest() CosmosSDKCompatibilityManifest {
	manifest := CosmosSDKCompatibilityManifest{
		Extensions: []CosmosSDKModuleExtension{
			{
				ModuleName:          "staking",
				ModulePath:          "x/staking",
				ExtensionMode:       CosmosSDKExtensionModeExtend,
				PreservedInterfaces: []string{"ValidatorI", "Delegation", "Redelegation", "UnbondingDelegation", "staking keeper hooks"},
				AddedState:          []string{"delegation activation epoch", "validator score references", "risk window references", "capacity declarations"},
			},
			{
				ModuleName:          "slashing",
				ModulePath:          "x/slashing",
				ExtensionMode:       CosmosSDKExtensionModeExtend,
				PreservedInterfaces: []string{"ValidatorSigningInfo", "tombstone", "jail", "missed block bitmap"},
				AddedState:          []string{"severity matrix", "role suspension", "future election score penalty", "delegator slash exposure"},
			},
			{
				ModuleName:          "distribution",
				ModulePath:          "x/distribution",
				ExtensionMode:       CosmosSDKExtensionModeExtend,
				PreservedInterfaces: []string{"delegator rewards", "validator outstanding rewards", "fee pool"},
				AddedState:          []string{"role reward weights", "performance reward multiplier", "reporter reward routing"},
				RewardInputs:        []string{"uptime score", "correctness score", "task completion rate", "role weight"},
			},
			{
				ModuleName:          "mint",
				ModulePath:          "x/mint",
				ExtensionMode:       CosmosSDKExtensionModeExtend,
				PreservedInterfaces: []string{"mint params", "minter", "fee collector emission"},
				AddedState:          []string{"epoch reward budget", "workload-aware emission inputs", "security metric feedback"},
				RewardInputs:        []string{"base emission", "participation rate", "security score", "performance budget"},
			},
		},
		Modules: []PosModuleRequirement{
			{ModuleName: "epoch", ModulePath: "x/epoch", Required: true},
			{ModuleName: "validator_economy", ModulePath: "x/validator-economy", Required: true},
			{ModuleName: "taskgroups", ModulePath: "x/taskgroups", Required: true},
			{ModuleName: "evidence", ModulePath: "x/evidence", Required: true},
			{ModuleName: "performance", ModulePath: "x/performance", Required: true},
			{ModuleName: "delegation_market", ModulePath: "x/delegation-market", Required: false},
			{ModuleName: "collators", ModulePath: "x/collators", Required: false},
			{ModuleName: "fishermen", ModulePath: "x/fishermen", Required: false},
			{ModuleName: "security_metrics", ModulePath: "x/security-metrics", Required: false},
		},
		Middleware: []PosCompatibilityMiddleware{
			{Name: "epoch_management", Layer: PosLayerStakingCapital, Extends: []string{"staking", "slashing"}, ReadsModules: []string{"epoch", "staking"}, WritesModules: []string{"epoch"}},
			{Name: "validator_scoring", Layer: PosLayerEconomicConsensus, Extends: []string{"staking"}, ReadsModules: []string{"staking", "slashing", "performance", "validator_economy"}, WritesModules: []string{"validator_economy"}},
			{Name: "task_assignment", Layer: PosLayerTaskAssignment, Extends: []string{"staking"}, ReadsModules: []string{"epoch", "validator_economy", "staking"}, WritesModules: []string{"taskgroups"}},
			{Name: "performance_accounting", Layer: PosLayerEconomicConsensus, Extends: []string{"distribution", "mint"}, ReadsModules: []string{"performance", "taskgroups", "staking"}, WritesModules: []string{"performance", "distribution"}},
			{Name: "evidence_slashing", Layer: PosLayerEconomicConsensus, Extends: []string{"slashing", "distribution"}, ReadsModules: []string{"evidence", "taskgroups", "staking"}, WritesModules: []string{"evidence", "slashing", "distribution"}},
		},
	}
	manifest.Root = ComputeCosmosSDKCompatibilityRoot(manifest)
	return manifest
}

func RequiredPoSModuleNames(manifest CosmosSDKCompatibilityManifest) []string {
	out := make([]string, 0)
	for _, module := range manifest.Modules {
		if module.Required {
			out = append(out, module.ModuleName)
		}
	}
	return out
}

func OptionalPoSModuleNames(manifest CosmosSDKCompatibilityManifest) []string {
	out := make([]string, 0)
	for _, module := range manifest.Modules {
		if !module.Required {
			out = append(out, module.ModuleName)
		}
	}
	return out
}

func (m CosmosSDKCompatibilityManifest) Validate() error {
	if len(m.Extensions) == 0 {
		return errors.New("cosmos sdk compatibility extensions are required")
	}
	if len(m.Modules) == 0 {
		return errors.New("pos compatibility modules are required")
	}
	if len(m.Middleware) == 0 {
		return errors.New("pos compatibility middleware is required")
	}
	extensionByName := make(map[string]struct{}, len(m.Extensions))
	for _, extension := range m.Extensions {
		if err := extension.Validate(); err != nil {
			return err
		}
		if extension.ExtensionMode != CosmosSDKExtensionModeExtend {
			return fmt.Errorf("cosmos sdk module %s must be extended, not replaced", extension.ModuleName)
		}
		if _, found := extensionByName[extension.ModuleName]; found {
			return fmt.Errorf("duplicate cosmos sdk extension %s", extension.ModuleName)
		}
		extensionByName[extension.ModuleName] = struct{}{}
	}
	for _, required := range []string{"staking", "slashing", "distribution", "mint"} {
		if _, found := extensionByName[required]; !found {
			return fmt.Errorf("required cosmos sdk extension %s is missing", required)
		}
	}
	moduleByName := make(map[string]PosModuleRequirement, len(m.Modules))
	for _, module := range m.Modules {
		if err := module.Validate(); err != nil {
			return err
		}
		if _, found := moduleByName[module.ModuleName]; found {
			return fmt.Errorf("duplicate pos compatibility module %s", module.ModuleName)
		}
		moduleByName[module.ModuleName] = module
	}
	for _, required := range []string{"epoch", "validator_economy", "taskgroups", "evidence", "performance"} {
		module, found := moduleByName[required]
		if !found || !module.Required {
			return fmt.Errorf("required pos module %s is missing", required)
		}
	}
	for _, optional := range []string{"delegation_market", "collators", "fishermen", "security_metrics"} {
		module, found := moduleByName[optional]
		if !found || module.Required {
			return fmt.Errorf("optional pos module %s is missing or marked required", optional)
		}
	}
	for _, middleware := range m.Middleware {
		if err := middleware.Validate(extensionByName, moduleByName); err != nil {
			return err
		}
	}
	if err := validatePosHash("cosmos sdk compatibility root", m.Root); err != nil {
		return err
	}
	if expected := ComputeCosmosSDKCompatibilityRoot(m); expected != m.Root {
		return errors.New("cosmos sdk compatibility root mismatch")
	}
	return nil
}

func (e CosmosSDKModuleExtension) Validate() error {
	if err := validatePosToken("cosmos sdk extension module name", e.ModuleName); err != nil {
		return err
	}
	if err := validatePosToken("cosmos sdk extension module path", e.ModulePath); err != nil {
		return err
	}
	if e.ExtensionMode != CosmosSDKExtensionModeExtend && e.ExtensionMode != CosmosSDKExtensionModeReplace {
		return fmt.Errorf("unsupported cosmos sdk extension mode %s", e.ExtensionMode)
	}
	if len(e.PreservedInterfaces) == 0 {
		return fmt.Errorf("cosmos sdk extension %s must preserve baseline interfaces", e.ModuleName)
	}
	if len(e.AddedState) == 0 && len(e.RewardInputs) == 0 {
		return fmt.Errorf("cosmos sdk extension %s must add state or reward inputs", e.ModuleName)
	}
	for _, value := range e.PreservedInterfaces {
		if err := validatePosResponsibility("cosmos sdk preserved interface", value); err != nil {
			return err
		}
	}
	for _, value := range e.AddedState {
		if err := validatePosResponsibility("cosmos sdk added state", value); err != nil {
			return err
		}
	}
	for _, value := range e.RewardInputs {
		if err := validatePosResponsibility("cosmos sdk reward input", value); err != nil {
			return err
		}
	}
	return nil
}

func (r PosModuleRequirement) Validate() error {
	if err := validatePosToken("pos compatibility module name", r.ModuleName); err != nil {
		return err
	}
	return validatePosToken("pos compatibility module path", r.ModulePath)
}

func (m PosCompatibilityMiddleware) Validate(extensions map[string]struct{}, modules map[string]PosModuleRequirement) error {
	if err := validatePosToken("pos compatibility middleware name", m.Name); err != nil {
		return err
	}
	if err := validatePosLayer(m.Layer); err != nil {
		return err
	}
	if len(m.Extends) == 0 {
		return fmt.Errorf("pos compatibility middleware %s must extend at least one sdk module", m.Name)
	}
	for _, extension := range m.Extends {
		if err := validatePosToken("pos compatibility middleware extension", extension); err != nil {
			return err
		}
		if _, found := extensions[extension]; !found {
			return fmt.Errorf("pos compatibility middleware %s extends unknown sdk module %s", m.Name, extension)
		}
	}
	if len(m.ReadsModules) == 0 {
		return fmt.Errorf("pos compatibility middleware %s must read at least one module", m.Name)
	}
	referencedModules := append([]string{}, m.ReadsModules...)
	referencedModules = append(referencedModules, m.WritesModules...)
	for _, module := range referencedModules {
		if err := validatePosToken("pos compatibility middleware module", module); err != nil {
			return err
		}
		if _, sdkFound := extensions[module]; sdkFound {
			continue
		}
		if _, posFound := modules[module]; !posFound {
			return fmt.Errorf("pos compatibility middleware %s references unknown module %s", m.Name, module)
		}
	}
	return nil
}

func ComputeCosmosSDKCompatibilityRoot(manifest CosmosSDKCompatibilityManifest) string {
	return posHashRoot("aetheris-pos-cosmos-sdk-compatibility-v1", func(w posByteWriter) {
		posWriteUint64(w, uint64(len(manifest.Extensions)))
		for _, extension := range manifest.Extensions {
			posWritePart(w, extension.ModuleName)
			posWritePart(w, extension.ModulePath)
			posWritePart(w, string(extension.ExtensionMode))
			posWriteStringSlice(w, extension.PreservedInterfaces)
			posWriteStringSlice(w, extension.AddedState)
			posWriteStringSlice(w, extension.RewardInputs)
		}
		posWriteUint64(w, uint64(len(manifest.Modules)))
		for _, module := range manifest.Modules {
			posWritePart(w, module.ModuleName)
			posWritePart(w, module.ModulePath)
			posWriteUint64(w, boolAsUint64(module.Required))
		}
		posWriteUint64(w, uint64(len(manifest.Middleware)))
		for _, middleware := range manifest.Middleware {
			posWritePart(w, middleware.Name)
			posWritePart(w, string(middleware.Layer))
			posWriteStringSlice(w, middleware.Extends)
			posWriteStringSlice(w, middleware.ReadsModules)
			posWriteStringSlice(w, middleware.WritesModules)
		}
	})
}

func DefaultPoSModuleBoundaryManifest() PosModuleBoundaryManifest {
	manifest := PosModuleBoundaryManifest{
		Boundaries: []PosModuleBoundary{
			{
				ModuleName:     "epoch",
				ModulePath:     "x/epoch",
				Owns:           []string{"epoch lifecycle", "phase transitions", "epoch seed", "epoch queries"},
				ReadsModules:   []string{"staking"},
				WritesModules:  []string{"epoch"},
				QueryEndpoints: []string{"QueryCurrentEpoch", "QueryEpochHistory"},
			},
			{
				ModuleName:     "validator_economy",
				ModulePath:     "x/validator-economy",
				Owns:           []string{"validator score", "effective stake", "stake saturation", "election ranking", "role eligibility"},
				ReadsModules:   []string{"staking", "slashing", "performance"},
				WritesModules:  []string{"validator_economy"},
				QueryEndpoints: []string{"QueryValidatorScore", "QueryElectionRanking", "QueryValidatorSaturation", "QueryRoleEligibility"},
			},
			{
				ModuleName:     "taskgroups",
				ModulePath:     "x/taskgroups",
				Owns:           []string{"workload registry", "task group assignment", "proposer rotation", "verification groups"},
				ReadsModules:   []string{"epoch", "validator_economy", "staking"},
				WritesModules:  []string{"taskgroups"},
				QueryEndpoints: []string{"QueryWorkloadRegistry", "QueryTaskGroup", "QueryProposerRotation", "QueryVerificationGroup"},
			},
			{
				ModuleName:     "evidence",
				ModulePath:     "x/evidence",
				Owns:           []string{"structured evidence records", "evidence deposits", "verification group decisions", "reporter rewards"},
				ReadsModules:   []string{"taskgroups", "staking", "slashing"},
				WritesModules:  []string{"evidence", "slashing", "distribution"},
				QueryEndpoints: []string{"QueryEvidenceRecord", "QueryEvidenceDeposit", "QueryEvidenceDecision", "QueryReporterRewards"},
			},
			{
				ModuleName:     "performance",
				ModulePath:     "x/performance",
				Owns:           []string{"uptime", "latency", "correctness", "task completion", "reward multipliers"},
				ReadsModules:   []string{"taskgroups", "staking", "distribution"},
				WritesModules:  []string{"performance", "distribution"},
				QueryEndpoints: []string{"QueryPerformanceRecord", "QueryOperatorPerformanceHistory", "QueryRolePerformance", "QueryRewardMultiplier"},
			},
		},
	}
	manifest.Root = ComputePoSModuleBoundaryRoot(manifest)
	return manifest
}

func (m PosModuleBoundaryManifest) Validate(compatibility CosmosSDKCompatibilityManifest) error {
	if err := compatibility.Validate(); err != nil {
		return err
	}
	if len(m.Boundaries) == 0 {
		return errors.New("pos module boundaries are required")
	}
	knownModules := make(map[string]struct{})
	for _, extension := range compatibility.Extensions {
		knownModules[extension.ModuleName] = struct{}{}
	}
	for _, module := range compatibility.Modules {
		knownModules[module.ModuleName] = struct{}{}
	}
	required := RequiredPoSModuleNames(compatibility)
	boundaryByName := make(map[string]PosModuleBoundary, len(m.Boundaries))
	owned := make(map[string]string)
	for _, boundary := range m.Boundaries {
		if err := boundary.Validate(knownModules); err != nil {
			return err
		}
		if _, found := boundaryByName[boundary.ModuleName]; found {
			return fmt.Errorf("duplicate pos module boundary %s", boundary.ModuleName)
		}
		boundaryByName[boundary.ModuleName] = boundary
		for _, item := range boundary.Owns {
			if owner, found := owned[item]; found {
				return fmt.Errorf("pos boundary ownership %q overlaps between %s and %s", item, owner, boundary.ModuleName)
			}
			owned[item] = boundary.ModuleName
		}
	}
	for _, moduleName := range required {
		if _, found := boundaryByName[moduleName]; !found {
			return fmt.Errorf("required pos module boundary %s is missing", moduleName)
		}
	}
	if err := validatePosHash("pos module boundary root", m.Root); err != nil {
		return err
	}
	if expected := ComputePoSModuleBoundaryRoot(m); expected != m.Root {
		return errors.New("pos module boundary root mismatch")
	}
	return nil
}

func (b PosModuleBoundary) Validate(knownModules map[string]struct{}) error {
	if err := validatePosToken("pos module boundary name", b.ModuleName); err != nil {
		return err
	}
	if err := validatePosToken("pos module boundary path", b.ModulePath); err != nil {
		return err
	}
	if len(b.Owns) == 0 {
		return fmt.Errorf("pos module boundary %s must own at least one responsibility", b.ModuleName)
	}
	for _, item := range b.Owns {
		if err := validatePosResponsibility("pos module boundary ownership", item); err != nil {
			return err
		}
	}
	if len(b.QueryEndpoints) == 0 {
		return fmt.Errorf("pos module boundary %s must expose query endpoints", b.ModuleName)
	}
	for _, endpoint := range b.QueryEndpoints {
		if err := validatePosToken("pos module boundary query endpoint", endpoint); err != nil {
			return err
		}
	}
	referenced := append([]string{}, b.ReadsModules...)
	referenced = append(referenced, b.WritesModules...)
	for _, moduleName := range referenced {
		if err := validatePosToken("pos module boundary referenced module", moduleName); err != nil {
			return err
		}
		if _, found := knownModules[moduleName]; !found {
			return fmt.Errorf("pos module boundary %s references unknown module %s", b.ModuleName, moduleName)
		}
	}
	return nil
}

func PoSModuleBoundaryByName(manifest PosModuleBoundaryManifest, moduleName string) (PosModuleBoundary, bool) {
	for _, boundary := range manifest.Boundaries {
		if boundary.ModuleName == moduleName {
			return boundary, true
		}
	}
	return PosModuleBoundary{}, false
}

func ComputePoSModuleBoundaryRoot(manifest PosModuleBoundaryManifest) string {
	return posHashRoot("aetheris-pos-module-boundaries-v1", func(w posByteWriter) {
		posWriteUint64(w, uint64(len(manifest.Boundaries)))
		for _, boundary := range manifest.Boundaries {
			posWritePart(w, boundary.ModuleName)
			posWritePart(w, boundary.ModulePath)
			posWriteStringSlice(w, boundary.Owns)
			posWriteStringSlice(w, boundary.ReadsModules)
			posWriteStringSlice(w, boundary.WritesModules)
			posWriteStringSlice(w, boundary.QueryEndpoints)
		}
	})
}

func DefaultKeeperIntegrationManifest() KeeperIntegrationManifest {
	manifest := KeeperIntegrationManifest{
		KeeperInterfaces: []KeeperInterfaceSpec{
			{KeeperName: "staking", ModuleName: "staking", InterfaceName: "StakingKeeper", IntegrationPoint: "validator and delegation state", Reads: []string{"validators", "delegations", "redelegations", "unbonding delegations"}, Writes: []string{"staking hooks only"}},
			{KeeperName: "slashing", ModuleName: "slashing", InterfaceName: "SlashingKeeper", IntegrationPoint: "jail tombstone and slash execution", Reads: []string{"validator signing info", "missed block bitmap"}, Writes: []string{"jail", "tombstone", "slash execution"}},
			{KeeperName: "distribution", ModuleName: "distribution", InterfaceName: "DistributionKeeper", IntegrationPoint: "reward allocation", Reads: []string{"fee pool", "outstanding rewards"}, Writes: []string{"validator rewards", "delegator rewards", "reporter rewards"}},
			{KeeperName: "mint", ModuleName: "mint", InterfaceName: "MintKeeper", IntegrationPoint: "epoch reward budget", Reads: []string{"minter", "mint params"}, Writes: []string{"epoch reward budget"}},
			{KeeperName: "bank", ModuleName: "bank", InterfaceName: "BankKeeper", IntegrationPoint: "deposits reporter rewards and penalty routing", Reads: []string{"module balances", "account balances"}, Writes: []string{"evidence deposits", "reporter rewards", "penalty routing"}},
			{KeeperName: "gov", ModuleName: "gov", InterfaceName: "GovernanceKeeper", IntegrationPoint: "parameter updates", Reads: []string{"governance authority", "parameter proposals"}, Writes: []string{"pos params", "economy params", "security params"}},
		},
		StakingLifecycleHooks: []KeeperHookSpec{
			{SourceKeeper: "staking", HookName: "AfterValidatorCreated", Trigger: "validator registration", TargetModules: []string{"epoch", "validator_economy"}, PreservesBaseState: true, DeterministicOrder: true},
			{SourceKeeper: "staking", HookName: "AfterValidatorBonded", Trigger: "validator bonded", TargetModules: []string{"epoch", "validator_economy", "taskgroups"}, PreservesBaseState: true, DeterministicOrder: true},
			{SourceKeeper: "staking", HookName: "AfterDelegationModified", Trigger: "delegation modified", TargetModules: []string{"epoch", "validator_economy"}, PreservesBaseState: true, DeterministicOrder: true},
			{SourceKeeper: "staking", HookName: "BeforeDelegationRemoved", Trigger: "delegation exit", TargetModules: []string{"epoch", "validator_economy"}, PreservesBaseState: true, DeterministicOrder: true},
		},
		SlashingHooks: []KeeperHookSpec{
			{SourceKeeper: "slashing", HookName: "AfterValidatorSlashed", Trigger: "slash execution", TargetModules: []string{"performance", "validator_economy"}, PreservesBaseState: true, DeterministicOrder: true},
			{SourceKeeper: "slashing", HookName: "AfterValidatorJailed", Trigger: "validator jail", TargetModules: []string{"performance", "validator_economy", "taskgroups"}, PreservesBaseState: true, DeterministicOrder: true},
			{SourceKeeper: "slashing", HookName: "AfterValidatorTombstoned", Trigger: "validator tombstone", TargetModules: []string{"performance", "validator_economy", "taskgroups"}, PreservesBaseState: true, DeterministicOrder: true},
		},
		RewardIntegrations: []RewardMultiplierIntegration{
			{SourceModule: "performance", DistributionKeeper: "distribution", MintKeeper: "mint", MultiplierField: "reward_multiplier_bps", RewardInputs: []string{"uptime", "latency", "correctness", "task completion"}},
		},
		MigrationHandlers: []MigrationHandlerSpec{
			{ModuleName: "epoch", FromVersion: 1, ToVersion: 2, PreservesExistingStakingState: true, ExportsGenesis: true, ImportsGenesis: true},
			{ModuleName: "validator_economy", FromVersion: 1, ToVersion: 2, PreservesExistingStakingState: true, ExportsGenesis: true, ImportsGenesis: true},
			{ModuleName: "taskgroups", FromVersion: 1, ToVersion: 2, PreservesExistingStakingState: true, ExportsGenesis: true, ImportsGenesis: true},
			{ModuleName: "evidence", FromVersion: 1, ToVersion: 2, PreservesExistingStakingState: true, ExportsGenesis: true, ImportsGenesis: true},
			{ModuleName: "performance", FromVersion: 1, ToVersion: 2, PreservesExistingStakingState: true, ExportsGenesis: true, ImportsGenesis: true},
		},
		ExportImport: []ModuleExportImportSpec{
			{ModuleName: "epoch", ExportsGenesis: true, ImportsGenesis: true, DeterministicEncoding: true},
			{ModuleName: "validator_economy", ExportsGenesis: true, ImportsGenesis: true, DeterministicEncoding: true},
			{ModuleName: "taskgroups", ExportsGenesis: true, ImportsGenesis: true, DeterministicEncoding: true},
			{ModuleName: "evidence", ExportsGenesis: true, ImportsGenesis: true, DeterministicEncoding: true},
			{ModuleName: "performance", ExportsGenesis: true, ImportsGenesis: true, DeterministicEncoding: true},
		},
	}
	manifest.Root = ComputeKeeperIntegrationRoot(manifest)
	return manifest
}

func (m KeeperIntegrationManifest) Validate(compatibility CosmosSDKCompatibilityManifest, boundaries PosModuleBoundaryManifest) error {
	if err := compatibility.Validate(); err != nil {
		return err
	}
	if err := boundaries.Validate(compatibility); err != nil {
		return err
	}
	knownModules := knownKeeperIntegrationModules(compatibility)
	if len(m.KeeperInterfaces) == 0 {
		return errors.New("keeper interfaces are required")
	}
	keepers := make(map[string]KeeperInterfaceSpec, len(m.KeeperInterfaces))
	for _, keeper := range m.KeeperInterfaces {
		if err := keeper.Validate(knownModules); err != nil {
			return err
		}
		if _, found := keepers[keeper.KeeperName]; found {
			return fmt.Errorf("duplicate keeper interface %s", keeper.KeeperName)
		}
		keepers[keeper.KeeperName] = keeper
	}
	for _, required := range []string{"staking", "slashing", "distribution", "mint", "bank", "gov"} {
		if _, found := keepers[required]; !found {
			return fmt.Errorf("required keeper interface %s is missing", required)
		}
	}
	if err := validateHookSet("staking lifecycle", m.StakingLifecycleHooks, "staking", knownModules); err != nil {
		return err
	}
	if err := validateHookSet("slashing", m.SlashingHooks, "slashing", knownModules); err != nil {
		return err
	}
	if len(m.RewardIntegrations) == 0 {
		return errors.New("distribution reward multiplier integration is required")
	}
	for _, integration := range m.RewardIntegrations {
		if err := integration.Validate(knownModules); err != nil {
			return err
		}
	}
	requiredPoS := RequiredPoSModuleNames(compatibility)
	if err := validateMigrationHandlers(m.MigrationHandlers, requiredPoS, knownModules); err != nil {
		return err
	}
	if err := validateExportImportSupport(m.ExportImport, requiredPoS, knownModules); err != nil {
		return err
	}
	if err := validatePosHash("keeper integration root", m.Root); err != nil {
		return err
	}
	if expected := ComputeKeeperIntegrationRoot(m); expected != m.Root {
		return errors.New("keeper integration root mismatch")
	}
	return nil
}

func (s KeeperInterfaceSpec) Validate(knownModules map[string]struct{}) error {
	if err := validatePosToken("keeper name", s.KeeperName); err != nil {
		return err
	}
	if err := validatePosToken("keeper module name", s.ModuleName); err != nil {
		return err
	}
	if _, found := knownModules[s.ModuleName]; !found {
		return fmt.Errorf("keeper %s references unknown module %s", s.KeeperName, s.ModuleName)
	}
	if err := validatePosToken("keeper interface name", s.InterfaceName); err != nil {
		return err
	}
	if err := validatePosResponsibility("keeper integration point", s.IntegrationPoint); err != nil {
		return err
	}
	if len(s.Reads) == 0 && len(s.Writes) == 0 {
		return fmt.Errorf("keeper %s must declare reads or writes", s.KeeperName)
	}
	for _, value := range append(append([]string{}, s.Reads...), s.Writes...) {
		if err := validatePosResponsibility("keeper access", value); err != nil {
			return err
		}
	}
	return nil
}

func (h KeeperHookSpec) Validate(expectedSource string, knownModules map[string]struct{}) error {
	if h.SourceKeeper != expectedSource {
		return fmt.Errorf("%s hook source must be %s", h.HookName, expectedSource)
	}
	if err := validatePosToken("keeper hook source", h.SourceKeeper); err != nil {
		return err
	}
	if err := validatePosToken("keeper hook name", h.HookName); err != nil {
		return err
	}
	if err := validatePosResponsibility("keeper hook trigger", h.Trigger); err != nil {
		return err
	}
	if len(h.TargetModules) == 0 {
		return fmt.Errorf("keeper hook %s must target at least one module", h.HookName)
	}
	for _, moduleName := range h.TargetModules {
		if err := validatePosToken("keeper hook target module", moduleName); err != nil {
			return err
		}
		if _, found := knownModules[moduleName]; !found {
			return fmt.Errorf("keeper hook %s references unknown module %s", h.HookName, moduleName)
		}
	}
	if !h.PreservesBaseState {
		return fmt.Errorf("keeper hook %s must preserve base sdk state", h.HookName)
	}
	if !h.DeterministicOrder {
		return fmt.Errorf("keeper hook %s must use deterministic order", h.HookName)
	}
	return nil
}

func (r RewardMultiplierIntegration) Validate(knownModules map[string]struct{}) error {
	for _, item := range []struct {
		name  string
		value string
	}{
		{name: "reward source module", value: r.SourceModule},
		{name: "reward distribution keeper", value: r.DistributionKeeper},
		{name: "reward mint keeper", value: r.MintKeeper},
		{name: "reward multiplier field", value: r.MultiplierField},
	} {
		if err := validatePosToken(item.name, item.value); err != nil {
			return err
		}
	}
	for _, moduleName := range []string{r.SourceModule, r.DistributionKeeper, r.MintKeeper} {
		if _, found := knownModules[moduleName]; !found {
			return fmt.Errorf("reward integration references unknown module %s", moduleName)
		}
	}
	if r.SourceModule != "performance" || r.DistributionKeeper != "distribution" || r.MintKeeper != "mint" {
		return errors.New("reward multiplier integration must connect performance to distribution and mint")
	}
	if len(r.RewardInputs) == 0 {
		return errors.New("reward multiplier integration inputs are required")
	}
	for _, input := range r.RewardInputs {
		if err := validatePosResponsibility("reward multiplier input", input); err != nil {
			return err
		}
	}
	return nil
}

func ComputeKeeperIntegrationRoot(manifest KeeperIntegrationManifest) string {
	return posHashRoot("aetheris-pos-keeper-integration-v1", func(w posByteWriter) {
		posWriteUint64(w, uint64(len(manifest.KeeperInterfaces)))
		for _, keeper := range manifest.KeeperInterfaces {
			posWritePart(w, keeper.KeeperName)
			posWritePart(w, keeper.ModuleName)
			posWritePart(w, keeper.InterfaceName)
			posWritePart(w, keeper.IntegrationPoint)
			posWriteStringSlice(w, keeper.Reads)
			posWriteStringSlice(w, keeper.Writes)
		}
		posWriteHookSpecs(w, manifest.StakingLifecycleHooks)
		posWriteHookSpecs(w, manifest.SlashingHooks)
		posWriteUint64(w, uint64(len(manifest.RewardIntegrations)))
		for _, integration := range manifest.RewardIntegrations {
			posWritePart(w, integration.SourceModule)
			posWritePart(w, integration.DistributionKeeper)
			posWritePart(w, integration.MintKeeper)
			posWritePart(w, integration.MultiplierField)
			posWriteStringSlice(w, integration.RewardInputs)
		}
		posWriteUint64(w, uint64(len(manifest.MigrationHandlers)))
		for _, migration := range manifest.MigrationHandlers {
			posWritePart(w, migration.ModuleName)
			posWriteUint64(w, migration.FromVersion)
			posWriteUint64(w, migration.ToVersion)
			posWriteUint64(w, boolAsUint64(migration.PreservesExistingStakingState))
			posWriteUint64(w, boolAsUint64(migration.ExportsGenesis))
			posWriteUint64(w, boolAsUint64(migration.ImportsGenesis))
		}
		posWriteUint64(w, uint64(len(manifest.ExportImport)))
		for _, spec := range manifest.ExportImport {
			posWritePart(w, spec.ModuleName)
			posWriteUint64(w, boolAsUint64(spec.ExportsGenesis))
			posWriteUint64(w, boolAsUint64(spec.ImportsGenesis))
			posWriteUint64(w, boolAsUint64(spec.DeterministicEncoding))
		}
	})
}

func validateHookSet(label string, hooks []KeeperHookSpec, source string, knownModules map[string]struct{}) error {
	if len(hooks) == 0 {
		return fmt.Errorf("%s hooks are required", label)
	}
	seen := make(map[string]struct{}, len(hooks))
	for _, hook := range hooks {
		if err := hook.Validate(source, knownModules); err != nil {
			return err
		}
		if _, found := seen[hook.HookName]; found {
			return fmt.Errorf("duplicate %s hook %s", label, hook.HookName)
		}
		seen[hook.HookName] = struct{}{}
	}
	return nil
}

func validateMigrationHandlers(handlers []MigrationHandlerSpec, requiredModules []string, knownModules map[string]struct{}) error {
	if len(handlers) == 0 {
		return errors.New("migration handlers are required")
	}
	byModule := make(map[string]MigrationHandlerSpec, len(handlers))
	for _, handler := range handlers {
		if err := handler.Validate(knownModules); err != nil {
			return err
		}
		if _, found := byModule[handler.ModuleName]; found {
			return fmt.Errorf("duplicate migration handler %s", handler.ModuleName)
		}
		byModule[handler.ModuleName] = handler
	}
	for _, moduleName := range requiredModules {
		if _, found := byModule[moduleName]; !found {
			return fmt.Errorf("migration handler for %s is missing", moduleName)
		}
	}
	return nil
}

func (m MigrationHandlerSpec) Validate(knownModules map[string]struct{}) error {
	if err := validatePosToken("migration module name", m.ModuleName); err != nil {
		return err
	}
	if _, found := knownModules[m.ModuleName]; !found {
		return fmt.Errorf("migration references unknown module %s", m.ModuleName)
	}
	if m.FromVersion == 0 || m.ToVersion <= m.FromVersion {
		return fmt.Errorf("migration %s must advance module version", m.ModuleName)
	}
	if !m.PreservesExistingStakingState {
		return fmt.Errorf("migration %s must preserve existing staking state", m.ModuleName)
	}
	if !m.ExportsGenesis || !m.ImportsGenesis {
		return fmt.Errorf("migration %s must preserve export and import support", m.ModuleName)
	}
	return nil
}

func validateExportImportSupport(specs []ModuleExportImportSpec, requiredModules []string, knownModules map[string]struct{}) error {
	if len(specs) == 0 {
		return errors.New("export import support is required")
	}
	byModule := make(map[string]ModuleExportImportSpec, len(specs))
	for _, spec := range specs {
		if err := spec.Validate(knownModules); err != nil {
			return err
		}
		if _, found := byModule[spec.ModuleName]; found {
			return fmt.Errorf("duplicate export import support %s", spec.ModuleName)
		}
		byModule[spec.ModuleName] = spec
	}
	for _, moduleName := range requiredModules {
		if _, found := byModule[moduleName]; !found {
			return fmt.Errorf("export import support for %s is missing", moduleName)
		}
	}
	return nil
}

func (s ModuleExportImportSpec) Validate(knownModules map[string]struct{}) error {
	if err := validatePosToken("export import module name", s.ModuleName); err != nil {
		return err
	}
	if _, found := knownModules[s.ModuleName]; !found {
		return fmt.Errorf("export import references unknown module %s", s.ModuleName)
	}
	if !s.ExportsGenesis || !s.ImportsGenesis {
		return fmt.Errorf("module %s must support export and import", s.ModuleName)
	}
	if !s.DeterministicEncoding {
		return fmt.Errorf("module %s export import encoding must be deterministic", s.ModuleName)
	}
	return nil
}

func knownKeeperIntegrationModules(compatibility CosmosSDKCompatibilityManifest) map[string]struct{} {
	known := make(map[string]struct{})
	for _, extension := range compatibility.Extensions {
		known[extension.ModuleName] = struct{}{}
	}
	for _, module := range compatibility.Modules {
		known[module.ModuleName] = struct{}{}
	}
	known["bank"] = struct{}{}
	known["gov"] = struct{}{}
	return known
}

func DefaultStateModelManifest() StateModelManifest {
	manifest := StateModelManifest{Keys: []StateKeySpec{
		{Domain: "epoch", Name: "current", Template: "epoch/current"},
		{Domain: "epoch", Name: "records", Template: "epoch/records/{epoch_id}", Components: []string{"epoch_id"}},
		{Domain: "epoch", Name: "phase", Template: "epoch/phase/{epoch_id}", Components: []string{"epoch_id"}},
		{Domain: "epoch", Name: "seed", Template: "epoch/seed/{epoch_id}", Components: []string{"epoch_id"}},
		{Domain: "validator_economy", Name: "scores", Template: "valecon/scores/{epoch_id}/{validator}", Components: []string{"epoch_id", "validator"}},
		{Domain: "validator_economy", Name: "effective_stake", Template: "valecon/effective_stake/{epoch_id}/{validator}", Components: []string{"epoch_id", "validator"}},
		{Domain: "validator_economy", Name: "saturation", Template: "valecon/saturation/{epoch_id}/{validator}", Components: []string{"epoch_id", "validator"}},
		{Domain: "validator_economy", Name: "roles", Template: "valecon/roles/{epoch_id}/{validator}/{role}", Components: []string{"epoch_id", "validator", "role"}},
		{Domain: "taskgroups", Name: "groups", Template: "taskgroups/groups/{epoch_id}/{task_group_id}", Components: []string{"epoch_id", "task_group_id"}},
		{Domain: "taskgroups", Name: "workloads", Template: "taskgroups/workloads/{workload_id}", Components: []string{"workload_id"}},
		{Domain: "taskgroups", Name: "assignments", Template: "taskgroups/assignments/{epoch_id}/{validator}/{task_group_id}", Components: []string{"epoch_id", "validator", "task_group_id"}},
		{Domain: "taskgroups", Name: "proposer", Template: "taskgroups/proposer/{epoch_id}/{slot}/{task_group_id}", Components: []string{"epoch_id", "slot", "task_group_id"}},
		{Domain: "evidence", Name: "records", Template: "evidence/records/{evidence_id}", Components: []string{"evidence_id"}},
		{Domain: "evidence", Name: "by_accused", Template: "evidence/by_accused/{validator}/{evidence_id}", Components: []string{"validator", "evidence_id"}},
		{Domain: "evidence", Name: "by_reporter", Template: "evidence/by_reporter/{reporter}/{evidence_id}", Components: []string{"reporter", "evidence_id"}},
		{Domain: "evidence", Name: "verification_groups", Template: "evidence/verification_groups/{evidence_id}", Components: []string{"evidence_id"}},
		{Domain: "evidence", Name: "deposits", Template: "evidence/deposits/{evidence_id}", Components: []string{"evidence_id"}},
		{Domain: "performance", Name: "records", Template: "performance/records/{epoch_id}/{operator}/{role}", Components: []string{"epoch_id", "operator", "role"}},
		{Domain: "performance", Name: "uptime", Template: "performance/uptime/{epoch_id}/{validator}", Components: []string{"epoch_id", "validator"}},
		{Domain: "performance", Name: "correctness", Template: "performance/correctness/{epoch_id}/{validator}", Components: []string{"epoch_id", "validator"}},
		{Domain: "performance", Name: "tasks", Template: "performance/tasks/{epoch_id}/{validator}", Components: []string{"epoch_id", "validator"}},
		{Domain: "risk", Name: "unbonding", Template: "risk/unbonding/{delegator}/{validator}/{creation_height}", Components: []string{"delegator", "validator", "creation_height"}},
		{Domain: "risk", Name: "redelegation", Template: "risk/redelegation/{delegator}/{src_validator}/{dst_validator}/{epoch_id}", Components: []string{"delegator", "src_validator", "dst_validator", "epoch_id"}},
		{Domain: "risk", Name: "exposure", Template: "risk/exposure/{epoch_id}/{validator}/{delegator}", Components: []string{"epoch_id", "validator", "delegator"}},
	}}
	manifest.Root = ComputeStateModelRoot(manifest)
	return manifest
}

func (m StateModelManifest) Validate() error {
	if len(m.Keys) == 0 {
		return errors.New("state model keys are required")
	}
	seenTemplates := make(map[string]struct{}, len(m.Keys))
	seenNames := make(map[string]struct{}, len(m.Keys))
	for _, key := range m.Keys {
		if err := key.Validate(); err != nil {
			return err
		}
		if _, found := seenTemplates[key.Template]; found {
			return fmt.Errorf("duplicate state key template %s", key.Template)
		}
		seenTemplates[key.Template] = struct{}{}
		qualified := key.Domain + "/" + key.Name
		if _, found := seenNames[qualified]; found {
			return fmt.Errorf("duplicate state key name %s", qualified)
		}
		seenNames[qualified] = struct{}{}
	}
	if err := validatePosHash("state model root", m.Root); err != nil {
		return err
	}
	if expected := ComputeStateModelRoot(m); expected != m.Root {
		return errors.New("state model root mismatch")
	}
	return nil
}

func (s StateKeySpec) Validate() error {
	if err := validatePosToken("state key domain", s.Domain); err != nil {
		return err
	}
	if err := validatePosToken("state key name", s.Name); err != nil {
		return err
	}
	if strings.TrimSpace(s.Template) != s.Template || s.Template == "" {
		return errors.New("state key template is required and must not have surrounding whitespace")
	}
	if strings.Contains(s.Template, "//") {
		return fmt.Errorf("state key template %s must not contain empty segments", s.Template)
	}
	for _, component := range s.Components {
		if err := validatePosToken("state key component", component); err != nil {
			return err
		}
		if !strings.Contains(s.Template, "{"+component+"}") {
			return fmt.Errorf("state key component %s is not present in template %s", component, s.Template)
		}
	}
	return nil
}

func ComputeStateModelRoot(manifest StateModelManifest) string {
	return posHashRoot("aetheris-pos-state-model-v1", func(w posByteWriter) {
		posWriteUint64(w, uint64(len(manifest.Keys)))
		for _, key := range manifest.Keys {
			posWritePart(w, key.Domain)
			posWritePart(w, key.Name)
			posWritePart(w, key.Template)
			posWriteStringSlice(w, key.Components)
		}
	})
}

func (a LayeredPosArchitecture) Validate() error {
	if len(a.Layers) != len(DefaultPosLayerOrder()) {
		return errors.New("layered pos architecture must define all layers")
	}
	expectedOrder := DefaultPosLayerOrder()
	seen := make(map[PosLayer]int, len(a.Layers))
	for i, layer := range a.Layers {
		if layer.Layer != expectedOrder[i] {
			return fmt.Errorf("pos layer %d must be %s", i, expectedOrder[i])
		}
		if _, found := seen[layer.Layer]; found {
			return fmt.Errorf("duplicate pos layer %s", layer.Layer)
		}
		seen[layer.Layer] = i
		if err := layer.Validate(); err != nil {
			return err
		}
	}
	for _, layer := range a.Layers {
		layerIndex := seen[layer.Layer]
		for _, dependency := range layer.DependsOn {
			dependencyIndex, found := seen[dependency]
			if !found {
				return fmt.Errorf("pos layer %s depends on unknown layer %s", layer.Layer, dependency)
			}
			if dependencyIndex <= layerIndex {
				return fmt.Errorf("pos layer %s must depend only on lower layers", layer.Layer)
			}
		}
	}
	if err := validatePosHash("layered pos architecture root", a.Root); err != nil {
		return err
	}
	if expected := ComputeLayeredPosArchitectureRoot(a.Layers); a.Root != expected {
		return errors.New("layered pos architecture root mismatch")
	}
	return nil
}

func (s PosLayerSpec) Validate() error {
	if err := validatePosLayer(s.Layer); err != nil {
		return err
	}
	if len(s.Responsibilities) == 0 {
		return fmt.Errorf("pos layer %s responsibilities are required", s.Layer)
	}
	for _, responsibility := range s.Responsibilities {
		if err := validatePosResponsibility("pos layer responsibility", responsibility); err != nil {
			return err
		}
	}
	seen := make(map[PosLayer]struct{}, len(s.DependsOn))
	for _, dependency := range s.DependsOn {
		if err := validatePosLayer(dependency); err != nil {
			return err
		}
		if dependency == s.Layer {
			return fmt.Errorf("pos layer %s cannot depend on itself", s.Layer)
		}
		if _, found := seen[dependency]; found {
			return fmt.Errorf("duplicate dependency %s for pos layer %s", dependency, s.Layer)
		}
		seen[dependency] = struct{}{}
	}
	return nil
}

func DefaultPosLayerOrder() []PosLayer {
	return []PosLayer{
		PosLayerEconomicConsensus,
		PosLayerTaskAssignment,
		PosLayerValidatorExecution,
		PosLayerStakingCapital,
		PosLayerBaseCometBFT,
	}
}

func ComputeLayeredPosArchitectureRoot(layers []PosLayerSpec) string {
	return posHashRoot("aetheris-pos-layered-architecture-v1", func(w posByteWriter) {
		posWriteUint64(w, uint64(len(layers)))
		for _, layer := range layers {
			posWritePart(w, string(layer.Layer))
			posWriteUint64(w, uint64(len(layer.Responsibilities)))
			for _, responsibility := range layer.Responsibilities {
				posWritePart(w, responsibility)
			}
			posWriteUint64(w, uint64(len(layer.DependsOn)))
			for _, dependency := range layer.DependsOn {
				posWritePart(w, string(dependency))
			}
		}
	})
}

func validatePosLayer(layer PosLayer) error {
	switch layer {
	case PosLayerEconomicConsensus, PosLayerTaskAssignment, PosLayerValidatorExecution, PosLayerStakingCapital, PosLayerBaseCometBFT:
		return nil
	default:
		return fmt.Errorf("unsupported pos layer %q", layer)
	}
}

func validatePosToken(fieldName string, value string) error {
	if strings.TrimSpace(value) != value || value == "" {
		return fmt.Errorf("%s is required and must not have surrounding whitespace", fieldName)
	}
	if len(value) > maxPosTokenLength {
		return fmt.Errorf("%s must be <= %d bytes", fieldName, maxPosTokenLength)
	}
	for _, r := range value {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || r == ':' || r == '/' {
			continue
		}
		return fmt.Errorf("%s contains invalid character", fieldName)
	}
	return nil
}

func validatePosResponsibility(fieldName string, value string) error {
	if strings.TrimSpace(value) != value || value == "" {
		return fmt.Errorf("%s is required and must not have surrounding whitespace", fieldName)
	}
	if len(value) > maxPosTokenLength {
		return fmt.Errorf("%s must be <= %d bytes", fieldName, maxPosTokenLength)
	}
	for _, r := range value {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || r == ':' || r == '/' || r == ' ' || r == '+' || r == ',' {
			continue
		}
		return fmt.Errorf("%s contains invalid character", fieldName)
	}
	return nil
}

func validatePosHash(fieldName string, value string) error {
	if len(value) != PosHashHexLength {
		return fmt.Errorf("%s must be %d lowercase hex chars", fieldName, PosHashHexLength)
	}
	for _, r := range value {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'f' {
			continue
		}
		return fmt.Errorf("%s must be %d lowercase hex chars", fieldName, PosHashHexLength)
	}
	return nil
}

type posByteWriter interface {
	Write([]byte) (int, error)
}

func posHashRoot(domain string, write func(posByteWriter)) string {
	h := sha256.New()
	posWritePart(h, domain)
	write(h)
	return hex.EncodeToString(h.Sum(nil))
}

func posWritePart(w posByteWriter, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = w.Write(length[:])
	_, _ = w.Write([]byte(value))
}

func posWriteStringSlice(w posByteWriter, values []string) {
	posWriteUint64(w, uint64(len(values)))
	for _, value := range values {
		posWritePart(w, value)
	}
}

func posWriteHookSpecs(w posByteWriter, hooks []KeeperHookSpec) {
	posWriteUint64(w, uint64(len(hooks)))
	for _, hook := range hooks {
		posWritePart(w, hook.SourceKeeper)
		posWritePart(w, hook.HookName)
		posWritePart(w, hook.Trigger)
		posWriteStringSlice(w, hook.TargetModules)
		posWriteUint64(w, boolAsUint64(hook.PreservesBaseState))
		posWriteUint64(w, boolAsUint64(hook.DeterministicOrder))
	}
}

func posWriteUint64(w posByteWriter, value uint64) {
	var bz [8]byte
	binary.BigEndian.PutUint64(bz[:], value)
	_, _ = w.Write(bz[:])
}
