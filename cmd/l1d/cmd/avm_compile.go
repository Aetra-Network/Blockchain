package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

func NewAVMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "avm",
		Short: "AVM language and artifact tooling",
	}
	cmd.AddCommand(
		newAVMCompileCmd(),
		newAVMFormatCmd(),
		newAVMLintCmd(),
		newAVMDisasmCmd(),
		newAVMGasCmd(),
		newAVMInspectCmd(),
		newAVMTestCmd(),
		newAVMSelectorsCmd(),
		newAVMLSPCmd(),
	)
	return cmd
}

func newAVMCompileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compile [source-file]",
		Short: "Compile an AVM source file into canonical artifacts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sourcePath := strings.TrimSpace(args[0])
			src, err := os.ReadFile(sourcePath)
			if err != nil {
				return err
			}
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
			c, err := compiler.New(opts)
			if err != nil {
				return err
			}
			result, err := c.Compile(src)
			if err != nil {
				return err
			}
			outDir, _ := cmd.Flags().GetString("out")
			if strings.TrimSpace(outDir) == "" {
				outDir = ".avm-out"
			}
			if err := writeAVMArtifacts(result, outDir); err != nil {
				return err
			}
			return writeCommandJSON(cmd, map[string]any{
				"source":          sourcePath,
				"out_dir":         outDir,
				"contract":        result.Contract.Name,
				"module_hash":     fmt.Sprintf("%x", result.ModuleHash[:]),
				"manifest_hash":   fmt.Sprintf("%x", result.ManifestHash[:]),
				"state_init_hash": fmt.Sprintf("%x", result.StateInitHash[:]),
				"selector_count":  len(result.SelectorRegistry.Entries),
			})
		},
	}
	cmd.Flags().String("out", ".avm-out", "output directory for canonical artifacts")
	addAVMCompileFlags(cmd)
	return cmd
}
