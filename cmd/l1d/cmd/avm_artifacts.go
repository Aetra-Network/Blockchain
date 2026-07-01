package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

func writeAVMArtifacts(result *compiler.Result, dir string) error {
	if result == nil {
		return fmt.Errorf("nil compile result")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "module.bin"), result.ModuleBytes, 0o644); err != nil {
		return err
	}
	if err := writeArtifactJSON(filepath.Join(dir, "interface.json"), result.Manifest); err != nil {
		return err
	}
	if err := writeArtifactJSON(filepath.Join(dir, "stateinit.json"), map[string]any{
		"abi_version":      result.StateInit.ABIVersion,
		"code_hash":        fmt.Sprintf("%x", result.StateInit.CodeHash),
		"init_data":        fmt.Sprintf("%x", result.StateInit.InitData),
		"salt":             fmt.Sprintf("%x", result.StateInit.Salt),
		"deployer_address": result.StateInit.DeployerAddress,
		"chain_id":         result.StateInit.ChainID,
		"namespace":        result.StateInit.Namespace,
		"initial_balance":  result.StateInit.InitialBalance,
		"capabilities":     result.StateInit.Capabilities.Flags,
		"state_init_hash":  fmt.Sprintf("%x", result.StateInitHash[:]),
		"module_hash":      fmt.Sprintf("%x", result.ModuleHash[:]),
	}); err != nil {
		return err
	}
	if err := writeArtifactJSON(filepath.Join(dir, "storage-layout.json"), result.StorageLayout); err != nil {
		return err
	}
	if err := writeArtifactJSON(filepath.Join(dir, "selector-registry.json"), result.SelectorRegistry); err != nil {
		return err
	}
	if err := writeArtifactJSON(filepath.Join(dir, "codecs.json"), map[string]any{
		"storage":  result.StorageCodec,
		"messages": result.MessageCodecs,
		"getters":  result.GetterCodecs,
		"events":   result.EventCodecs,
	}); err != nil {
		return err
	}
	if err := writeArtifactJSON(filepath.Join(dir, "diagnostics.json"), result.Diagnostics); err != nil {
		return err
	}
	if result.CodeChunk != nil {
		codeChunkBytes, err := result.CodeChunk.Serialize()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "module.chunk"), codeChunkBytes, 0o644); err != nil {
			return err
		}
	}
	if err := writeArtifactJSON(filepath.Join(dir, "ir.json"), result.IR); err != nil {
		return err
	}
	if err := writeArtifactJSON(filepath.Join(dir, "dependency-lock.json"), result.DependencyLock); err != nil {
		return err
	}
	return nil
}

func writeArtifactJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
