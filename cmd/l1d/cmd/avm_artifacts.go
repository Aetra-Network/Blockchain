package cmd

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/chunk"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

func writeAVMArtifacts(result *compiler.Result, dir string) error {
	if result == nil {
		return fmt.Errorf("nil compile result")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	if err := root.WriteFile("module.bin", result.ModuleBytes, 0o644); err != nil {
		return err
	}
	if err := writeArtifactJSON(root, "interface.json", result.Manifest); err != nil {
		return err
	}
	rawDataHash := sha256.Sum256(result.StateInit.InitData)
	rawDataChunkView, err := renderAVMDataChunks(result.StateInit.InitData)
	if err != nil {
		return err
	}
	if err := writeArtifactJSON(root, "stateinit.json", map[string]any{
		"abi_version":        result.StateInit.ABIVersion,
		"code_hash":          fmt.Sprintf("%x", result.StateInit.CodeHash),
		"init_data":          fmt.Sprintf("%x", result.StateInit.InitData),
		"init_data_base64":   base64.StdEncoding.EncodeToString(result.StateInit.InitData),
		"init_data_hex_hash": fmt.Sprintf("%x", rawDataHash[:]),
		"init_data_chunks":   rawDataChunkView,
		"salt":               fmt.Sprintf("%x", result.StateInit.Salt),
		"deployer_address":   result.StateInit.DeployerAddress,
		"chain_id":           result.StateInit.ChainID,
		"namespace":          result.StateInit.Namespace,
		"initial_balance":    result.StateInit.InitialBalance,
		"capabilities":       result.StateInit.Capabilities.Flags,
		"state_init_hash":    fmt.Sprintf("%x", result.StateInitHash[:]),
		"module_hash":        fmt.Sprintf("%x", result.ModuleHash[:]),
	}); err != nil {
		return err
	}
	if err := writeArtifactJSON(root, "storage-layout.json", result.StorageLayout); err != nil {
		return err
	}
	if err := writeArtifactJSON(root, "selector-registry.json", result.SelectorRegistry); err != nil {
		return err
	}
	if err := writeArtifactJSON(root, "codecs.json", map[string]any{
		"storage":  result.StorageCodec,
		"messages": result.MessageCodecs,
		"getters":  result.GetterCodecs,
		"events":   result.EventCodecs,
	}); err != nil {
		return err
	}
	if err := writeArtifactJSON(root, "diagnostics.json", result.Diagnostics); err != nil {
		return err
	}
	if result.CodeChunk != nil {
		codeChunkBytes, err := result.CodeChunk.Serialize()
		if err != nil {
			return err
		}
		if err := root.WriteFile("module.chunk", codeChunkBytes, 0o644); err != nil {
			return err
		}
	}
	if err := writeArtifactJSON(root, "ir.json", result.IR); err != nil {
		return err
	}
	if err := writeArtifactJSON(root, "dependency-lock.json", result.DependencyLock); err != nil {
		return err
	}
	return nil
}

func renderAVMDataChunks(data []byte) (string, error) {
	root, err := avm.ToChunkPayload(data, chunk.TypeNormal)
	if err != nil {
		return "", err
	}
	return chunk.RenderSource(root), nil
}

func writeArtifactJSON(root *os.Root, name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return root.WriteFile(name, data, 0o644)
}
