package cmd

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	appparams "github.com/sovereign-l1/l1/app/params"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

func TestAVMCLICommandConstruction(t *testing.T) {
	for _, tc := range []struct {
		name string
		root *cobraCommandShim
		path []string
	}{
		{name: "tx store-code", root: shimCommand(NewAVMTxCmd()), path: []string{"store-code"}},
		{name: "tx deploy", root: shimCommand(NewAVMTxCmd()), path: []string{"deploy"}},
		{name: "tx execute", root: shimCommand(NewAVMTxCmd()), path: []string{"execute"}},
		{name: "avm compile", root: shimCommand(NewAVMCmd()), path: []string{"compile"}},
		{name: "avm fmt", root: shimCommand(NewAVMCmd()), path: []string{"fmt"}},
		{name: "avm lint", root: shimCommand(NewAVMCmd()), path: []string{"lint"}},
		{name: "avm disasm", root: shimCommand(NewAVMCmd()), path: []string{"disasm"}},
		{name: "avm gas", root: shimCommand(NewAVMCmd()), path: []string{"gas"}},
		{name: "avm inspect", root: shimCommand(NewAVMCmd()), path: []string{"inspect"}},
		{name: "avm test", root: shimCommand(NewAVMCmd()), path: []string{"test"}},
		{name: "avm selectors", root: shimCommand(NewAVMCmd()), path: []string{"selectors"}},
		{name: "avm lsp", root: shimCommand(NewAVMCmd()), path: []string{"lsp"}},
		{name: "query code", root: shimCommand(NewAVMQueryCmd()), path: []string{"code"}},
		{name: "query contract", root: shimCommand(NewAVMQueryCmd()), path: []string{"contract"}},
		{name: "query storage", root: shimCommand(NewAVMQueryCmd()), path: []string{"storage"}},
		{name: "query receipts", root: shimCommand(NewAVMQueryCmd()), path: []string{"receipts"}},
		{name: "debug encode-message", root: shimCommand(NewAVMDebugCmd()), path: []string{"encode-message"}},
		{name: "debug decode-receipt", root: shimCommand(NewAVMDebugCmd()), path: []string{"decode-receipt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.root.Find(t, tc.path...)
			require.NotEmpty(t, cmd.Short)
		})
	}
}

func TestAVMCLIFeeValidationRejectsMissingOrNonNaetFees(t *testing.T) {
	_, err := executeAVMCommand(NewAVMTxCmd(), "deploy", testHash("code"), "--from", aeAddressForCLI(0x11), "--height", "1")
	require.ErrorContains(t, err, "requires --fees")

	_, err = executeAVMCommand(NewAVMTxCmd(), "deploy", testHash("code"), "--from", aeAddressForCLI(0x11), "--height", "1", "--fees", "1uatom")
	require.ErrorContains(t, err, "must use naet denom")

	out, err := executeAVMCommand(NewAVMTxCmd(), "deploy", testHash("code"), "--from", aeAddressForCLI(0x11), "--height", "1", "--fees", "1"+appparams.BaseDenom)
	require.NoError(t, err)
	require.Contains(t, out, "DeployContract")
}

func TestAVMCLIE2ESmokeDeployExecuteQuery(t *testing.T) {
	codeID := testHash("smoke-code")
	creator := aeAddressForCLI(0x11)
	contract := aeAddressForCLI(0x22)

	deployOut, err := executeAVMCommand(
		NewAVMTxCmd(),
		"deploy", codeID,
		"--from", creator,
		"--height", "9",
		"--fees", "7"+appparams.BaseDenom,
		"--body-json", `{"symbol":"TST","decimals":9}`,
		"--salt", "token-master",
	)
	require.NoError(t, err)
	var deploy struct {
		Service string `json:"service"`
		Method  string `json:"method"`
		TypeURL string `json:"type_url"`
		Request struct {
			Creator     string `json:"creator"`
			CodeID      string `json:"code_id"`
			InitPayload string `json:"init_payload_base64"`
			Height      uint64 `json:"height"`
		} `json:"request"`
		Expected struct {
			ContractAddressUser string `json:"contract_address_user"`
			ContractAddressRaw  string `json:"contract_address_raw"`
		} `json:"expected_response_fields"`
	}
	require.NoError(t, json.Unmarshal([]byte(deployOut), &deploy), deployOut)
	require.Equal(t, "l1.contracts.v1.Msg", deploy.Service)
	require.Equal(t, "DeployContract", deploy.Method)
	require.Equal(t, codeID, deploy.Request.CodeID)
	require.Equal(t, creator, deploy.Request.Creator)
	require.Equal(t, uint64(9), deploy.Request.Height)
	require.Equal(t, "AE...", deploy.Expected.ContractAddressUser)
	require.Equal(t, "4:...", deploy.Expected.ContractAddressRaw)
	body, err := base64.StdEncoding.DecodeString(deploy.Request.InitPayload)
	require.NoError(t, err)
	require.Equal(t, `{"decimals":9,"symbol":"TST"}`, string(body))

	execOut, err := executeAVMCommand(
		NewAVMTxCmd(),
		"execute", contract,
		"--from", creator,
		"--height", "10",
		"--gas-limit", "500000",
		"--fees", "3"+appparams.BaseDenom,
		"--body-json", `{"op":"mint","amount":"100"}`,
	)
	require.NoError(t, err)
	require.Contains(t, execOut, "ExecuteExternal")
	require.Contains(t, execOut, "receipt_id")
	require.Contains(t, execOut, `"exit_code": 0`)

	queryOut, err := executeAVMCommand(NewAVMQueryCmd(), "contract", contract)
	require.NoError(t, err)
	require.Contains(t, queryOut, "l1.contracts.v1.Query")
	require.Contains(t, queryOut, "Contract")
	require.Contains(t, queryOut, contract)

	storageOut, err := executeAVMCommand(NewAVMQueryCmd(), "storage", contract, "--key-prefix-hex", "01", "--limit", "5")
	require.NoError(t, err)
	require.Contains(t, storageOut, "ContractStorage")
	require.Contains(t, storageOut, `"limit": 5`)
}

func TestAVMCLIToolingArtifactsAndSmokeTest(t *testing.T) {
	sourcePath := filepath.Clean(filepath.Join("..", "..", "..", "examples", "avm", "token", "token_master.atlx"))
	compileDir := t.TempDir()

	compileOut, err := executeAVMCommand(NewAVMCmd(), "compile", sourcePath, "--out", compileDir)
	require.NoError(t, err, compileOut)
	requireJSONKeys(t, compileOut,
		"source",
		"out_dir",
		"contract",
		"module_hash",
		"manifest_hash",
		"state_init_hash",
		"selector_count",
		"warnings",
		"bytecode_hex",
		"bytecode_base64",
		"bytecode_chunks",
		"raw_data_hex",
		"raw_data_base64",
		"raw_data_hex_hash",
		"raw_data_chunks",
	)
	for _, name := range []string{
		"module.bin",
		"module.chunk",
		"interface.json",
		"stateinit.json",
		"storage-layout.json",
		"selector-registry.json",
		"codecs.json",
		"diagnostics.json",
		"ir.json",
		"dependency-lock.json",
	} {
		_, statErr := os.Stat(filepath.Join(compileDir, name))
		require.NoErrorf(t, statErr, "missing compile artifact %s", name)
	}

	lintOut, err := executeAVMCommand(NewAVMCmd(), "lint", sourcePath)
	require.NoError(t, err, lintOut)
	requireJSONKeys(t, lintOut, "ok", "contract", "module_hash", "manifest_hash", "state_init_hash", "selector_count", "warnings")

	disasmOut, err := executeAVMCommand(NewAVMCmd(), "disasm", filepath.Join(compileDir, "module.bin"))
	require.NoError(t, err, disasmOut)
	requireJSONKeys(t, disasmOut, "module_hash", "exports", "code")

	gasOut, err := executeAVMCommand(NewAVMCmd(), "gas", filepath.Join(compileDir, "module.bin"))
	require.NoError(t, err, gasOut)
	requireJSONKeys(t, gasOut, "contract", "module_hash", "gas_total", "gas_breakdown", "gas_schedule", "instruction_count")

	inspectOut, err := executeAVMCommand(NewAVMCmd(), "inspect", sourcePath)
	require.NoError(t, err, inspectOut)
	requireJSONKeys(t, inspectOut, "contract", "package", "module_hash", "manifest_hash", "state_init_hash", "storage_layout", "selectors", "wallet_actions", "code_chunk_hash", "bytecode_hex", "bytecode_base64", "bytecode_chunks", "raw_data_hex", "raw_data_base64", "raw_data_hex_hash", "raw_data_chunks", "dependency_lock", "warnings")

	testDir := t.TempDir()
	testOut, err := executeAVMCommand(NewAVMCmd(), "test", sourcePath, "--out", testDir)
	require.NoError(t, err, testOut)
	requireJSONKeys(t, testOut, "contract", "package", "module_hash", "manifest_hash", "state_init_hash", "artifact_dir", "passed", "executions")
	_, statErr := os.Stat(filepath.Join(testDir, "test-report.json"))
	require.NoError(t, statErr)

	selectorsOut, err := executeAVMCommand(NewAVMCmd(), "selectors", sourcePath)
	require.NoError(t, err, selectorsOut)
	requireJSONKeys(t, selectorsOut, "Contract", "Entries", "RegistryHash")
}

func TestAVMCLIToolingOutputsAreStableForSameSourceTree(t *testing.T) {
	sourcePath := filepath.Clean(filepath.Join("..", "..", "..", "examples", "avm", "token", "token_master.atlx"))

	compileA := t.TempDir()
	compileB := t.TempDir()
	compileOutA, err := executeAVMCommand(NewAVMCmd(), "compile", sourcePath, "--out", compileA)
	require.NoError(t, err)
	compileOutB, err := executeAVMCommand(NewAVMCmd(), "compile", sourcePath, "--out", compileB)
	require.NoError(t, err)
	require.Equal(t, normalizeToolJSON(t, compileOutA, "out_dir"), normalizeToolJSON(t, compileOutB, "out_dir"))
	requireArtifactHashesEqual(t, compileA, compileB, []string{
		"module.bin",
		"module.chunk",
		"interface.json",
		"stateinit.json",
		"storage-layout.json",
		"selector-registry.json",
		"codecs.json",
		"diagnostics.json",
		"ir.json",
		"dependency-lock.json",
	})

	inspectOutA, err := executeAVMCommand(NewAVMCmd(), "inspect", sourcePath)
	require.NoError(t, err)
	inspectOutB, err := executeAVMCommand(NewAVMCmd(), "inspect", sourcePath)
	require.NoError(t, err)
	require.Equal(t, inspectOutA, inspectOutB)

	disasmOutA, err := executeAVMCommand(NewAVMCmd(), "disasm", filepath.Join(compileA, "module.bin"))
	require.NoError(t, err)
	disasmOutB, err := executeAVMCommand(NewAVMCmd(), "disasm", filepath.Join(compileA, "module.bin"))
	require.NoError(t, err)
	require.Equal(t, disasmOutA, disasmOutB)

	gasOutA, err := executeAVMCommand(NewAVMCmd(), "gas", filepath.Join(compileA, "module.bin"))
	require.NoError(t, err)
	gasOutB, err := executeAVMCommand(NewAVMCmd(), "gas", filepath.Join(compileA, "module.bin"))
	require.NoError(t, err)
	require.Equal(t, gasOutA, gasOutB)

	testA := t.TempDir()
	testB := t.TempDir()
	testOutA, err := executeAVMCommand(NewAVMCmd(), "test", sourcePath, "--out", testA)
	require.NoError(t, err)
	testOutB, err := executeAVMCommand(NewAVMCmd(), "test", sourcePath, "--out", testB)
	require.NoError(t, err)
	require.Equal(t, normalizeToolJSON(t, testOutA, "artifact_dir"), normalizeToolJSON(t, testOutB, "artifact_dir"))
	require.Equal(t, readNormalizedJSONFile(t, filepath.Join(testA, "test-report.json"), "artifact_dir"), readNormalizedJSONFile(t, filepath.Join(testB, "test-report.json"), "artifact_dir"))

	selectorsOutA, err := executeAVMCommand(NewAVMCmd(), "selectors", sourcePath)
	require.NoError(t, err)
	selectorsOutB, err := executeAVMCommand(NewAVMCmd(), "selectors", sourcePath)
	require.NoError(t, err)
	require.Equal(t, selectorsOutA, selectorsOutB)
}

func TestAVMLSPInitializeAdvertisesStableCapabilities(t *testing.T) {
	resp := runAVMLSPInitialize(t)
	require.Equal(t, "2.0", resp["jsonrpc"])
	require.NotNil(t, resp["id"])

	result, ok := resp["result"].(map[string]any)
	require.True(t, ok, "%v", resp["result"])
	capabilities, ok := result["capabilities"].(map[string]any)
	require.True(t, ok, "%v", result)
	require.EqualValues(t, 1, capabilities["textDocumentSync"])
	diagnostics, ok := capabilities["diagnosticProvider"].(map[string]any)
	require.True(t, ok, "%v", capabilities)
	require.Equal(t, false, diagnostics["interFileDependencies"])
	require.Equal(t, false, diagnostics["workspaceDiagnostics"])
}

func TestAVMLSPInitializeIsStableAcrossFreshServerInstances(t *testing.T) {
	first := runAVMLSPInitialize(t)
	second := runAVMLSPInitialize(t)
	require.Equal(t, first, second)
}

func TestAVMArtifactWritingUsesOutputDirectoryRoot(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, writeAVMArtifacts(testAVMCompileResult(), dir))

	for _, name := range []string{
		"module.bin",
		"interface.json",
		"stateinit.json",
		"storage-layout.json",
		"selector-registry.json",
		"codecs.json",
		"diagnostics.json",
		"ir.json",
		"dependency-lock.json",
	} {
		_, err := os.Stat(filepath.Join(dir, name))
		require.NoErrorf(t, err, "missing artifact %s", name)
	}
	_, err := os.Stat(filepath.Join(dir, "module.chunk"))
	require.Error(t, err)
}

func TestLoadAVMSourcesReadsNestedDirectoriesDeterministically(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.atlx"), []byte("package b"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "nested", "a.atlx"), []byte("package a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "nested", "ignore.txt"), []byte("ignored"), 0o644))

	sources, err := loadAVMSources([]string{dir})
	require.NoError(t, err)
	require.Len(t, sources, 2)
	require.Equal(t, filepath.Join(dir, "b.atlx"), sources[0].Name)
	require.Equal(t, []byte("package b"), sources[0].Data)
	require.Equal(t, filepath.Join(dir, "nested", "a.atlx"), sources[1].Name)
	require.Equal(t, []byte("package a"), sources[1].Data)
}

func TestAVMCLIDecodeReceiptStableJSON(t *testing.T) {
	receipt := async.ExecutionReceipt{
		Sequence:       7,
		Opcode:         99,
		QueryID:        42,
		ResultCode:     async.ResultOK,
		GasUsed:        1234,
		StorageFeeNaet: sdkmath.NewInt(5),
		ForwardFeeNaet: sdkmath.NewInt(3),
	}
	bz, err := json.Marshal(receipt)
	require.NoError(t, err)

	first, err := executeAVMCommand(NewAVMDebugCmd(), "decode-receipt", "--receipt-json", string(bz))
	require.NoError(t, err)
	second, err := executeAVMCommand(NewAVMDebugCmd(), "decode-receipt", "--receipt-json", string(bz))
	require.NoError(t, err)
	require.Equal(t, first, second)

	var decoded struct {
		ReceiptID      string `json:"receipt_id"`
		ExitCode       uint32 `json:"exit_code"`
		GasUsed        uint64 `json:"gas_used"`
		StorageFeeNaet string `json:"storage_fee_naet"`
		ForwardFeeNaet string `json:"forward_fee_naet"`
		RetryScheduled bool   `json:"retry_scheduled"`
	}
	require.NoError(t, json.Unmarshal([]byte(first), &decoded), first)
	require.NotEmpty(t, decoded.ReceiptID)
	require.Equal(t, uint32(0), decoded.ExitCode)
	require.Equal(t, uint64(1234), decoded.GasUsed)
	require.Equal(t, "5", decoded.StorageFeeNaet)
	require.Equal(t, "3", decoded.ForwardFeeNaet)
	require.False(t, decoded.RetryScheduled)
}

func TestAVMCLIEncodeMessageCanonicalizesJSON(t *testing.T) {
	out, err := executeAVMCommand(NewAVMDebugCmd(), "encode-message", "--opcode", "12", "--query-id", "77", "--body-json", `{"b":2,"a":1}`)
	require.NoError(t, err)

	var decoded struct {
		BodyBase64 string `json:"body_base64"`
		Opcode     uint32 `json:"opcode"`
		QueryID    uint64 `json:"query_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &decoded), out)
	body, err := base64.StdEncoding.DecodeString(decoded.BodyBase64)
	require.NoError(t, err)
	require.Equal(t, `{"a":1,"b":2}`, string(body))
	require.Equal(t, uint32(12), decoded.Opcode)
	require.Equal(t, uint64(77), decoded.QueryID)
}

type cobraCommandShim struct {
	cmd interface {
		Find([]string) (*cobra.Command, []string, error)
	}
}

func shimCommand(cmd *cobra.Command) *cobraCommandShim {
	return &cobraCommandShim{cmd: cmd}
}

func (s *cobraCommandShim) Find(t *testing.T, path ...string) *cobra.Command {
	t.Helper()
	cmd, _, err := s.cmd.Find(path)
	require.NoError(t, err)
	require.NotNil(t, cmd)
	require.Equal(t, path[len(path)-1], cmd.Name())
	return cmd
}

func executeAVMCommand(cmd *cobra.Command, args ...string) (string, error) {
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func runAVMLSPInitialize(t *testing.T) map[string]any {
	t.Helper()

	originalStdin := os.Stdin
	originalStdout := os.Stdout
	defer func() {
		os.Stdin = originalStdin
		os.Stdout = originalStdout
	}()

	inR, inW, err := os.Pipe()
	require.NoError(t, err)
	defer inR.Close()
	defer inW.Close()

	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	defer outR.Close()
	defer outW.Close()

	os.Stdin = inR
	os.Stdout = outW

	done := make(chan error, 1)
	go func() {
		done <- runAVMLanguageServer()
	}()

	request := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	_, err = fmt.Fprintf(inW, "Content-Length: %d\r\n\r\n%s", len(request), request)
	require.NoError(t, err)
	require.NoError(t, inW.Close())

	resp := readLSPMessage(t, outR)
	require.NoError(t, outW.Close())
	require.NoError(t, <-done)
	return resp
}

func normalizeToolJSON(t *testing.T, out string, dropKeys ...string) map[string]any {
	t.Helper()
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &parsed), out)
	for _, key := range dropKeys {
		delete(parsed, key)
	}
	return parsed
}

func readNormalizedJSONFile(t *testing.T, path string, dropKeys ...string) map[string]any {
	t.Helper()
	bz, err := os.ReadFile(path)
	require.NoError(t, err)
	return normalizeToolJSON(t, string(bz), dropKeys...)
}

func requireArtifactHashesEqual(t *testing.T, leftDir, rightDir string, names []string) {
	t.Helper()
	for _, name := range names {
		require.Equal(t, hashFile(t, filepath.Join(leftDir, name)), hashFile(t, filepath.Join(rightDir, name)), name)
	}
}

func hashFile(t *testing.T, path string) string {
	t.Helper()
	bz, err := os.ReadFile(path)
	require.NoError(t, err)
	sum := sha256.Sum256(bz)
	return hex.EncodeToString(sum[:])
}

func requireJSONKeys(t *testing.T, out string, want ...string) {
	t.Helper()
	var obj map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(out), &obj), out)
	got := make([]string, 0, len(obj))
	for key := range obj {
		got = append(got, key)
	}
	require.ElementsMatch(t, want, got, out)
}

func readLSPMessage(t *testing.T, r *os.File) map[string]any {
	t.Helper()
	reader := bufio.NewReader(r)
	var contentLength int
	for {
		line, err := reader.ReadString('\n')
		require.NoError(t, err)
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			_, err := fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(strings.ToLower(line), "content-length:")), "%d", &contentLength)
			require.NoError(t, err)
		}
	}
	require.Greater(t, contentLength, 0)
	body := make([]byte, contentLength)
	_, err := io.ReadFull(reader, body)
	require.NoError(t, err)
	var msg map[string]any
	require.NoError(t, json.Unmarshal(body, &msg))
	return msg
}

func aeAddressForCLI(fill byte) string {
	bz := make([]byte, 20)
	for i := range bz {
		bz[i] = fill
	}
	return addressing.FormatAccAddress(bz)
}

func testHash(seed string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("avm-cli/%s", seed)))
	return hex.EncodeToString(sum[:])
}

func testAVMCompileResult() *compiler.Result {
	return &compiler.Result{
		ModuleBytes: []byte{0x01, 0x02, 0x03},
		ModuleHash:  [32]byte{0x04},
		Manifest: avm.InterfaceManifest{
			Name:    "demo",
			Version: 1,
		},
		StateInit: &avm.StateInit{
			ABIVersion:      avm.StateInitABIVersion,
			CodeHash:        [32]byte{0x05},
			InitData:        []byte("init"),
			Salt:            []byte("salt"),
			DeployerAddress: "AEtest",
			ChainID:         "chain",
			Namespace:       "namespace",
			InitialBalance:  7,
			Capabilities:    avm.DeployCapabilityMask{Flags: 3},
		},
		StateInitHash: [32]byte{0x06},
		StorageLayout: compiler.StorageLayout{Name: "storage"},
		SelectorRegistry: compiler.SelectorRegistry{
			Contract: "demo",
		},
		Diagnostics: []compiler.Diagnostic{
			{Severity: compiler.SeverityWarning, Code: "W001", Message: "warn"},
		},
		DependencyLock: compiler.DependencyLock{
			Package: "demo",
		},
	}
}
