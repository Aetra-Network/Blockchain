package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

func newAVMFormatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fmt [paths...]",
		Short: "Format AVM source into canonical source form",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			write, _ := cmd.Flags().GetBool("write")
			sources, err := loadAVMSources(args)
			if err != nil {
				return err
			}
			if write {
				for _, src := range sources {
					formatted, err := compiler.FormatSourceNamed(src.Name, string(src.Data))
					if err != nil {
						return err
					}
					if err := os.WriteFile(src.Name, []byte(formatted), 0o644); err != nil {
						return err
					}
				}
				return nil
			}
			for i, src := range sources {
				formatted, err := compiler.FormatSourceNamed(src.Name, string(src.Data))
				if err != nil {
					return err
				}
				if len(sources) > 1 {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "// file: %s\n", src.Name); err != nil {
						return err
					}
				}
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), formatted); err != nil {
					return err
				}
				if i < len(sources)-1 {
					if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("write", false, "rewrite files in place")
	return cmd
}

func newAVMLintCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lint [paths...]",
		Short: "Compile AVM source and report static diagnostics",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := compileAVMSources(args, avmCompileOptionsFromCmd(cmd))
			if err != nil {
				return err
			}
			return writeCommandJSON(cmd, map[string]any{
				"ok":              true,
				"contract":        res.Contract.Name,
				"module_hash":     fmt.Sprintf("%x", res.ModuleHash[:]),
				"manifest_hash":   fmt.Sprintf("%x", res.ManifestHash[:]),
				"state_init_hash": fmt.Sprintf("%x", res.StateInitHash[:]),
				"selector_count":  len(res.SelectorRegistry.Entries),
				"warnings":        res.Diagnostics,
			})
		},
	}
	addAVMCompileFlags(cmd)
	return cmd
}

func newAVMDisasmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disasm [path]",
		Short: "Disassemble an AVM module or source file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			module, summary, err := loadCompiledModule(args[0], avmCompileOptionsFromCmd(cmd))
			if err != nil {
				return err
			}
			return writeCommandJSON(cmd, map[string]any{
				"module_hash": fmt.Sprintf("%x", summary.ModuleHash[:]),
				"exports":     summaryExports(module),
				"code":        avm.DisassembleModule(module),
			})
		},
	}
	addAVMCompileFlags(cmd)
	return cmd
}

func newAVMGasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gas [paths...]",
		Short: "Profile AVM gas usage from compiled module",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			module, summary, err := loadCompiledModule(args[0], avmCompileOptionsFromCmd(cmd))
			if err != nil {
				return err
			}
			profile := avm.ProfileGas(module, avm.DefaultParams().GasSchedule)
			return writeCommandJSON(cmd, map[string]any{
				"contract":          summaryContractName(summary),
				"module_hash":       fmt.Sprintf("%x", summary.ModuleHash[:]),
				"gas_total":         profile.Total,
				"gas_breakdown":     profile.Lines,
				"gas_schedule":      "default",
				"instruction_count": len(module.Code),
			})
		},
	}
	addAVMCompileFlags(cmd)
	return cmd
}

func newAVMInspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect [paths...]",
		Short: "Inspect ABI, state init, storage layout, and selectors",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := compileAVMSources(args, avmCompileOptionsFromCmd(cmd))
			if err != nil {
				return err
			}
			walletActions := make([]map[string]any, 0, len(res.Manifest.WalletActions))
			for _, action := range res.Manifest.WalletActions {
				walletActions = append(walletActions, map[string]any{
					"method":                action.Method,
					"title":                 action.Title,
					"risk":                  action.Risk,
					"confirm_label":         action.ConfirmLabel,
					"warning_level":         action.WarningLevel,
					"expected_side_effects": action.ExpectedSideEffects,
					"fund_access":           action.FundAccess,
					"approval_semantics":    action.ApprovalSemantics,
				})
			}
			return writeCommandJSON(cmd, map[string]any{
				"contract":        res.Contract.Name,
				"package":         res.Source.Package,
				"module_hash":     fmt.Sprintf("%x", res.ModuleHash[:]),
				"manifest_hash":   fmt.Sprintf("%x", res.ManifestHash[:]),
				"state_init_hash": fmt.Sprintf("%x", res.StateInitHash[:]),
				"storage_layout":  res.StorageLayout,
				"selectors":       res.SelectorRegistry,
				"wallet_actions":  walletActions,
				"code_chunk_hash": fmt.Sprintf("%x", res.CodeChunkHash[:]),
				"dependency_lock": res.DependencyLock,
			})
		},
	}
	addAVMCompileFlags(cmd)
	return cmd
}

func newAVMTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test [paths...]",
		Short: "Compile AVM source and run deterministic runtime smoke checks",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := compileAVMSources(args, avmCompileOptionsFromCmd(cmd))
			if err != nil {
				return err
			}
			outDir, _ := cmd.Flags().GetString("out")
			if strings.TrimSpace(outDir) == "" {
				outDir = ".avm-test"
			}
			if err := writeAVMArtifacts(res, outDir); err != nil {
				return err
			}
			report, runErr := runAVMTestSmoke(res)
			report.ArtifactDir = outDir
			if err := writeArtifactJSON(filepath.Join(outDir, "test-report.json"), report); err != nil {
				return err
			}
			if runErr != nil {
				return runErr
			}
			return writeCommandJSON(cmd, report)
		},
	}
	cmd.Flags().String("out", ".avm-test", "output directory for test artifacts")
	addAVMCompileFlags(cmd)
	return cmd
}

func newAVMSelectorsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "selectors [paths...]",
		Short: "Export the canonical selector registry as JSON",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := compileAVMSources(args, avmCompileOptionsFromCmd(cmd))
			if err != nil {
				return err
			}
			return writeCommandJSON(cmd, res.SelectorRegistry)
		},
	}
	addAVMCompileFlags(cmd)
	return cmd
}

func newAVMLSPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lsp",
		Short: "Run a minimal AVM language server over stdio",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAVMLanguageServer()
		},
	}
	return cmd
}

func addAVMCompileFlags(cmd *cobra.Command) {
	cmd.Flags().String("chain-id", "", "override chain id")
	cmd.Flags().String("namespace", "", "override namespace")
	cmd.Flags().String("deployer", "", "override deployer address")
	cmd.Flags().String("salt", "", "override deployer salt")
	cmd.Flags().Uint64("initial-balance", 0, "override initial balance")
}

func avmCompileOptionsFromCmd(cmd *cobra.Command) compiler.Options {
	opts := compiler.DefaultOptions()
	if v, _ := cmd.Flags().GetString("chain-id"); strings.TrimSpace(v) != "" {
		opts.ChainID = strings.TrimSpace(v)
	}
	if v, _ := cmd.Flags().GetString("namespace"); strings.TrimSpace(v) != "" {
		opts.Namespace = strings.TrimSpace(v)
	}
	if v, _ := cmd.Flags().GetString("deployer"); strings.TrimSpace(v) != "" {
		opts.DeployerAddress = strings.TrimSpace(v)
	}
	if v, _ := cmd.Flags().GetString("salt"); strings.TrimSpace(v) != "" {
		opts.Salt = strings.TrimSpace(v)
	}
	if v, _ := cmd.Flags().GetUint64("initial-balance"); v != 0 {
		opts.InitialBalance = v
	}
	return opts
}

func compileAVMSources(paths []string, opts compiler.Options) (*compiler.Result, error) {
	sources, err := loadAVMSources(paths)
	if err != nil {
		return nil, err
	}
	c, err := compiler.New(opts)
	if err != nil {
		return nil, err
	}
	return c.CompileFiles(sources)
}

type avmTestFailure struct {
	Entrypoint string `json:"entrypoint"`
	Error      string `json:"error"`
}

type avmTestExecution struct {
	Entrypoint    string `json:"entrypoint"`
	Selector      uint32 `json:"selector"`
	GasUsed       uint64 `json:"gas_used"`
	ResultCode    uint32 `json:"result_code"`
	StorageWrites uint32 `json:"storage_writes"`
	ReturnValue   uint64 `json:"return_value"`
	Outgoing      uint32 `json:"outgoing"`
}

type avmTestReport struct {
	Contract      string             `json:"contract"`
	Package       string             `json:"package"`
	ModuleHash    string             `json:"module_hash"`
	ManifestHash  string             `json:"manifest_hash"`
	StateInitHash string             `json:"state_init_hash"`
	ArtifactDir   string             `json:"artifact_dir"`
	Passed        bool               `json:"passed"`
	Executions    []avmTestExecution `json:"executions"`
	Failures      []avmTestFailure   `json:"failures,omitempty"`
}

func runAVMTestSmoke(res *compiler.Result) (avmTestReport, error) {
	if res == nil {
		return avmTestReport{}, errors.New("nil compile result")
	}
	runner, err := avm.NewRunner(avm.DefaultParams())
	if err != nil {
		return avmTestReport{}, err
	}
	report := avmTestReport{
		Contract:      summaryContractName(res),
		Package:       res.Source.Package,
		ModuleHash:    fmt.Sprintf("%x", res.ModuleHash[:]),
		ManifestHash:  fmt.Sprintf("%x", res.ManifestHash[:]),
		StateInitHash: fmt.Sprintf("%x", res.StateInitHash[:]),
		Passed:        true,
	}
	for _, entry := range sortedModuleEntrypoints(res.Module.Exports) {
		label := entrypointLabel(entry)
		selector, ok := selectorForEntrypoint(res.SelectorRegistry.Entries, label)
		if !ok {
			report.Passed = false
			report.Failures = append(report.Failures, avmTestFailure{
				Entrypoint: label,
				Error:      fmt.Sprintf("selector for entrypoint %q not found", label),
			})
			continue
		}
		ctx := avm.RuntimeContext{
			Entry:           entry,
			GasLimit:        100_000,
			BlockHeight:     1,
			ContractAddress: dummyAVMAddress(),
			EmitDestination: dummyAVMAddress(),
			Message: async.MessageEnvelope{
				Opcode:   selector,
				QueryID:  uint64(selector),
				GasLimit: 100_000,
				Bounced:  entry == avm.EntryReceiveBounced,
			},
		}
		exec, err := runner.Run(res.Module, avm.Storage{}, ctx)
		if err != nil {
			report.Passed = false
			report.Failures = append(report.Failures, avmTestFailure{
				Entrypoint: label,
				Error:      err.Error(),
			})
			continue
		}
		report.Executions = append(report.Executions, avmTestExecution{
			Entrypoint:    label,
			Selector:      selector,
			GasUsed:       exec.GasUsed,
			ResultCode:    exec.ResultCode,
			StorageWrites: exec.StorageWrites,
			ReturnValue:   exec.ReturnValue,
			Outgoing:      uint32(len(exec.Outgoing)),
		})
	}
	if !report.Passed {
		return report, fmt.Errorf("AVM runtime smoke failed")
	}
	return report, nil
}

func sortedModuleEntrypoints(exports map[avm.Entrypoint]uint32) []avm.Entrypoint {
	keys := make([]int, 0, len(exports))
	for entry := range exports {
		keys = append(keys, int(entry))
	}
	sort.Ints(keys)
	out := make([]avm.Entrypoint, 0, len(keys))
	for _, key := range keys {
		out = append(out, avm.Entrypoint(key))
	}
	return out
}

func selectorForEntrypoint(entries []compiler.SelectorEntry, label string) (uint32, bool) {
	for _, entry := range entries {
		if strings.EqualFold(strings.TrimSpace(entry.Entrypoint), label) {
			return entry.Selector, true
		}
	}
	return 0, false
}

func dummyAVMAddress() sdk.AccAddress {
	return sdk.AccAddress(bytes.Repeat([]byte{0x11}, 20))
}

func loadCompiledModule(path string, opts compiler.Options) (avm.Module, *compiler.Result, error) {
	info, err := os.Stat(path)
	if err != nil {
		return avm.Module{}, nil, err
	}
	if info.IsDir() {
		modulePath := filepath.Join(path, "module.bin")
		raw, err := os.ReadFile(modulePath)
		if err != nil {
			return avm.Module{}, nil, err
		}
		module, err := avm.DecodeModule(raw)
		if err != nil {
			return avm.Module{}, nil, err
		}
		hash, _ := avm.CodeHash(module)
		return module, &compiler.Result{Module: module, ModuleHash: hash}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return avm.Module{}, nil, err
	}
	if module, err := avm.DecodeModule(raw); err == nil {
		hash, _ := avm.CodeHash(module)
		return module, &compiler.Result{Module: module, ModuleHash: hash}, nil
	}
	res, err := compileAVMSources([]string{path}, opts)
	if err != nil {
		return avm.Module{}, nil, err
	}
	return res.Module, res, nil
}

func loadAVMSources(paths []string) ([]compiler.NamedSource, error) {
	if len(paths) == 0 {
		return nil, errors.New("at least one path is required")
	}
	var sources []compiler.NamedSource
	for _, path := range paths {
		if err := collectAVMSources(filepath.Clean(path), &sources); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	return sources, nil
}

func collectAVMSources(path string, out *[]compiler.NamedSource) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if !strings.EqualFold(filepath.Ext(path), ".avm") {
			return fmt.Errorf("source file must use .avm extension: %s", path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		*out = append(*out, compiler.NamedSource{Name: path, Data: raw})
		return nil
	}
	return filepath.WalkDir(path, func(curr string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(curr), ".avm") {
			return nil
		}
		raw, err := os.ReadFile(curr)
		if err != nil {
			return err
		}
		*out = append(*out, compiler.NamedSource{Name: curr, Data: raw})
		return nil
	})
}

func summaryExports(module avm.Module) map[string]uint32 {
	out := make(map[string]uint32, len(module.Exports))
	keys := make([]int, 0, len(module.Exports))
	for entry := range module.Exports {
		keys = append(keys, int(entry))
	}
	sort.Ints(keys)
	for _, key := range keys {
		entry := avm.Entrypoint(key)
		out[entrypointLabel(entry)] = module.Exports[entry]
	}
	return out
}

func entrypointLabel(entry avm.Entrypoint) string {
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
		return fmt.Sprintf("entrypoint_%d", entry)
	}
}

func summaryContractName(summary *compiler.Result) string {
	if summary == nil || summary.Contract == nil {
		return ""
	}
	return summary.Contract.Name
}
