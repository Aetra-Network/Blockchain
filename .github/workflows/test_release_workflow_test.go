package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot() string {
	if dir := os.Getenv("REPO_ROOT"); dir != "" {
		return dir
	}
	cwd, _ := os.Getwd()
	normalized := strings.ReplaceAll(cwd, "\\", "/")

	if strings.HasSuffix(normalized, ".github/workflows") {
		return filepath.Dir(filepath.Dir(cwd))
	}
	return cwd
}

// TestReleaseWorkflowFileExists verifies testnet-readiness.yml exists
func TestReleaseWorkflowFileExists(t *testing.T) {
	workflowPath := filepath.Join(repoRoot(), ".github", "workflows", "testnet-readiness.yml")
	if _, err := os.Stat(workflowPath); os.IsNotExist(err) {
		t.Error("testnet-readiness.yml not found - required for release workflow")
	}
}

// TestReleaseWorkflowHasGoTest verifies workflow has go test job
func TestReleaseWorkflowHasGoTest(t *testing.T) {
	workflowPath := filepath.Join(repoRoot(), ".github", "workflows", "testnet-readiness.yml")
	content, err := os.ReadFile(workflowPath)
	if os.IsNotExist(err) {
		t.Skip("testnet-readiness.yml not found")
	}
	if err != nil {
		t.Fatalf("error reading testnet-readiness.yml: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "go test ./...") {
		t.Error("testnet-readiness.yml should run 'go test ./...'")
	}
}

func TestReleaseWorkflowHasLaunchAndAVMGates(t *testing.T) {
	workflowPath := filepath.Join(repoRoot(), ".github", "workflows", "testnet-readiness.yml")
	content, err := os.ReadFile(workflowPath)
	if os.IsNotExist(err) {
		t.Skip("testnet-readiness.yml not found")
	}
	if err != nil {
		t.Fatalf("error reading testnet-readiness.yml: %v", err)
	}

	text := string(content)
	for _, want := range []string{
		"avm-gates",
		"tests/e2e/avm_contract_smoke.ps1",
		"genesis-validate",
		"localnet-smoke",
		"export-import-roundtrip",
		"launch-evidence-bundle.ps1",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("testnet-readiness.yml should contain %q", want)
		}
	}
}

func TestSecurityWorkflowReadsGovulncheckTriageFromProtectedBaseBranch(t *testing.T) {
	workflowPath := filepath.Join(repoRoot(), ".github", "workflows", "security.yml")
	content, err := os.ReadFile(workflowPath)
	if os.IsNotExist(err) {
		t.Skip("security.yml not found")
	}
	if err != nil {
		t.Fatalf("error reading security.yml: %v", err)
	}

	text := string(content)
	for _, want := range []string{
		`GITHUB_EVENT_NAME`,
		`pull_request`,
		`GITHUB_BASE_REF`,
		`git fetch --no-tags --depth=1 origin "${GITHUB_BASE_REF}"`,
		`triage_file=".github/security/govulncheck-triage.txt"`,
		`git show "origin/${GITHUB_BASE_REF}:${triage_file}"`,
		`triage_source`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("security.yml should contain %q", want)
		}
	}
	if strings.Contains(text, `grep -E '^GO-[0-9]+-[0-9]+' .github/security/govulncheck-triage.txt`) {
		t.Error("security.yml should not read govulncheck triage from the checkout tree on PRs")
	}
}

func TestSecurityWorkflowRunsDeterminismGateOnPullRequests(t *testing.T) {
	workflowPath := filepath.Join(repoRoot(), ".github", "workflows", "security.yml")
	content, err := os.ReadFile(workflowPath)
	if os.IsNotExist(err) {
		t.Skip("security.yml not found")
	}
	if err != nil {
		t.Fatalf("error reading security.yml: %v", err)
	}

	text := string(content)
	// The Security Gate runs on pull_request and push-to-main; the determinism
	// gate job must be part of it so nondeterministic AVM/contracts changes are
	// blocked before merge (TESTNET-P0 #19).
	for _, want := range []string{
		"determinism-gate:",
		"go test ./tests/avm_determinism_gate/...",
		"TestAVMRuntimeDeterminismGateRejectsNondeterminismAndKeepsStableRoots",
		"TestConsensusCriticalSourceRejectsNondeterminismAndExternalNetworkCalls",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("security.yml should contain %q so the determinism gate runs on PRs", want)
		}
	}
}

func TestSecurityWorkflowUsesProtectedGitleaksConfig(t *testing.T) {
	workflowPath := filepath.Join(repoRoot(), ".github", "workflows", "security.yml")
	content, err := os.ReadFile(workflowPath)
	if os.IsNotExist(err) {
		t.Skip("security.yml not found")
	}
	if err != nil {
		t.Fatalf("error reading security.yml: %v", err)
	}

	text := string(content)
	for _, want := range []string{
		`config_ref="${{ github.event.repository.default_branch }}"`,
		`if [[ "${GITHUB_EVENT_NAME}" == "pull_request" ]]`,
		`git show "origin/${config_ref}:.gitleaks.toml"`,
		`--config "${gitleaks_config}"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("security.yml should contain %q", want)
		}
	}
	if strings.Contains(text, `--config .gitleaks.toml`) {
		t.Error("security.yml should not pass the checkout-tree gitleaks config directly")
	}
}

func TestPrototypeReleaseUsesProtectedGitleaksConfig(t *testing.T) {
	workflowPath := filepath.Join(repoRoot(), ".github", "workflows", "prototype-release.yml")
	content, err := os.ReadFile(workflowPath)
	if os.IsNotExist(err) {
		t.Skip("prototype-release.yml not found")
	}
	if err != nil {
		t.Fatalf("error reading prototype-release.yml: %v", err)
	}

	text := string(content)
	for _, want := range []string{
		`fetch-depth: 0`,
		`Resolve gitleaks config`,
		`$ConfigRef = "${{ github.event.repository.default_branch }}"`,
		`git show "origin/$ConfigRef:.gitleaks.toml"`,
		`GITLEAKS_CONFIG=$GitleaksConfig`,
		`--config $env:GITLEAKS_CONFIG`,
		`-GitleaksConfig $env:GITLEAKS_CONFIG`,
		`scripts\tooling\ensure-buf.ps1`,
		`BUF_VERSION`,
		`-Strict`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("prototype-release.yml should contain %q", want)
		}
	}
	if strings.Contains(text, `--config .gitleaks.toml`) {
		t.Error("prototype-release.yml should not pass the checkout-tree gitleaks config directly")
	}
	if strings.Contains(text, `-SkipGitleaksHistory`) {
		t.Error("prototype-release.yml should not skip gitleaks history scanning")
	}

	scriptPath := filepath.Join(repoRoot(), "scripts", "security", "prototype-audit.ps1")
	scriptContent, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("error reading prototype-audit.ps1: %v", err)
	}
	scriptText := string(scriptContent)
	for _, want := range []string{
		`[string]$GitleaksConfig = ""`,
		`Resolve-GitleaksConfig -Path $GitleaksConfig`,
		`-AllowFailure`,
		`gosec source issues`,
		`throw "gosec reported $count source findings"`,
		`--config", $GitleaksConfig`,
	} {
		if !strings.Contains(scriptText, want) {
			t.Fatalf("prototype-audit.ps1 should contain %q", want)
		}
	}
	if strings.Contains(scriptText, `--config", ".gitleaks.toml"`) {
		t.Error("prototype-audit.ps1 should not hardcode the checkout-tree gitleaks config")
	}
}

// TestReleaseWorkflowHasGoVet verifies workflow has go vet job
func TestReleaseWorkflowHasGoVet(t *testing.T) {
	workflowPath := filepath.Join(repoRoot(), ".github", "workflows", "testnet-readiness.yml")
	content, err := os.ReadFile(workflowPath)
	if os.IsNotExist(err) {
		t.Skip("testnet-readiness.yml not found")
	}
	if err != nil {
		t.Fatalf("error reading testnet-readiness.yml: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "go vet") {
		t.Error("testnet-readiness.yml should run 'go vet ./...'")
	}
}

// TestReleaseWorkflowHasBufLint verifies workflow has buf lint job
func TestReleaseWorkflowHasBufLint(t *testing.T) {
	workflowPath := filepath.Join(repoRoot(), ".github", "workflows", "testnet-readiness.yml")
	content, err := os.ReadFile(workflowPath)
	if os.IsNotExist(err) {
		t.Skip("testnet-readiness.yml not found")
	}
	if err != nil {
		t.Fatalf("error reading testnet-readiness.yml: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "buf lint") {
		t.Error("testnet-readiness.yml should run 'buf lint'")
	}
	if !strings.Contains(text, "scripts\\tooling\\ensure-buf.ps1") {
		t.Error("testnet-readiness.yml should install buf through the pinned helper")
	}
	if !strings.Contains(text, "BUF_VERSION") {
		t.Error("testnet-readiness.yml should pin buf version")
	}
}

// TestReleaseWorkflowHasGenesisValidate verifies workflow has genesis validation
func TestReleaseWorkflowHasGenesisValidate(t *testing.T) {
	workflowPath := filepath.Join(repoRoot(), ".github", "workflows", "testnet-readiness.yml")
	content, err := os.ReadFile(workflowPath)
	if os.IsNotExist(err) {
		t.Skip("testnet-readiness.yml not found")
	}
	if err != nil {
		t.Fatalf("error reading testnet-readiness.yml: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "genesis-validate") && !strings.Contains(text, "validate-genesis") {
		t.Error("testnet-readiness.yml should run genesis validation")
	}
}

// TestReleaseWorkflowHasLocalnetSmoke verifies workflow has localnet smoke test
func TestReleaseWorkflowHasLocalnetSmoke(t *testing.T) {
	workflowPath := filepath.Join(repoRoot(), ".github", "workflows", "testnet-readiness.yml")
	content, err := os.ReadFile(workflowPath)
	if os.IsNotExist(err) {
		t.Skip("testnet-readiness.yml not found")
	}
	if err != nil {
		t.Fatalf("error reading testnet-readiness.yml: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "localnet-smoke") && !strings.Contains(text, "localnet") {
		t.Error("testnet-readiness.yml should run localnet smoke test")
	}
}

// TestReleaseWorkflowHasExportImportSmoke verifies workflow has export/import smoke
func TestReleaseWorkflowHasExportImportSmoke(t *testing.T) {
	workflowPath := filepath.Join(repoRoot(), ".github", "workflows", "testnet-readiness.yml")
	content, err := os.ReadFile(workflowPath)
	if os.IsNotExist(err) {
		t.Skip("testnet-readiness.yml not found")
	}
	if err != nil {
		t.Fatalf("error reading testnet-readiness.yml: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "export-import") && !strings.Contains(text, "export") {
		t.Error("testnet-readiness.yml should run export/import smoke test")
	}
}

// TestReleaseWorkflowHasInvariants verifies workflow has invariants test
func TestReleaseWorkflowHasInvariants(t *testing.T) {
	workflowPath := filepath.Join(repoRoot(), ".github", "workflows", "testnet-readiness.yml")
	content, err := os.ReadFile(workflowPath)
	if os.IsNotExist(err) {
		t.Skip("testnet-readiness.yml not found")
	}
	if err != nil {
		t.Fatalf("error reading testnet-readiness.yml: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "invariants") {
		t.Error("testnet-readiness.yml should run invariants test")
	}
}

// TestReleaseWorkflowHasReleaseArtifactBuild verifies workflow has release artifact build
func TestReleaseWorkflowHasReleaseArtifactBuild(t *testing.T) {
	workflowPath := filepath.Join(repoRoot(), ".github", "workflows", "testnet-readiness.yml")
	content, err := os.ReadFile(workflowPath)
	if os.IsNotExist(err) {
		t.Skip("testnet-readiness.yml not found")
	}
	if err != nil {
		t.Fatalf("error reading testnet-readiness.yml: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "release-artifact") && !strings.Contains(text, "artifact") {
		t.Error("testnet-readiness.yml should build release artifact")
	}
}

// TestReleaseWorkflowHasVersionCommand verifies workflow has version command test
func TestReleaseWorkflowHasVersionCommand(t *testing.T) {
	workflowPath := filepath.Join(repoRoot(), ".github", "workflows", "testnet-readiness.yml")
	content, err := os.ReadFile(workflowPath)
	if os.IsNotExist(err) {
		t.Skip("testnet-readiness.yml not found")
	}
	if err != nil {
		t.Fatalf("error reading testnet-readiness.yml: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "version") {
		t.Error("testnet-readiness.yml should test version command")
	}
}

// TestReleaseWorkflowHasDockerBuild verifies workflow has Docker build
func TestReleaseWorkflowHasDockerBuild(t *testing.T) {
	workflowPath := filepath.Join(repoRoot(), ".github", "workflows", "testnet-readiness.yml")
	content, err := os.ReadFile(workflowPath)
	if os.IsNotExist(err) {
		t.Skip("testnet-readiness.yml not found")
	}
	if err != nil {
		t.Fatalf("error reading testnet-readiness.yml: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "docker") && !strings.Contains(text, "Docker") {
		t.Log("warning: testnet-readiness.yml may not include Docker build - check if Dockerfile exists")
	}
}

// TestReleaseWorkflowUploadsArtifacts verifies workflow uploads artifacts
func TestReleaseWorkflowUploadsArtifacts(t *testing.T) {
	workflowPath := filepath.Join(repoRoot(), ".github", "workflows", "testnet-readiness.yml")
	content, err := os.ReadFile(workflowPath)
	if os.IsNotExist(err) {
		t.Skip("testnet-readiness.yml not found")
	}
	if err != nil {
		t.Fatalf("error reading testnet-readiness.yml: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "upload-artifact") {
		t.Error("testnet-readiness.yml should upload release artifacts")
	}
}

// TestReleasePackageScriptExists verifies release package script exists
func TestReleasePackageScriptExists(t *testing.T) {
	scriptPath := filepath.Join(repoRoot(), "scripts", "release", "prototype-package.ps1")
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Error("scripts/release/prototype-package.ps1 not found - required for release packaging")
	}
}

// TestReleasePackageScriptBuildsBinary verifies release script builds binary
func TestReleasePackageScriptBuildsBinary(t *testing.T) {
	scriptPath := filepath.Join(repoRoot(), "scripts", "release", "prototype-package.ps1")
	content, err := os.ReadFile(scriptPath)
	if os.IsNotExist(err) {
		t.Skip("prototype-package.ps1 not found")
	}
	if err != nil {
		t.Fatalf("error reading prototype-package.ps1: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "go build") && !strings.Contains(text, "build-aetrad") {
		t.Error("prototype-package.ps1 should build aetrad binary")
	}
}

// TestReleasePackageScriptGeneratesChecksums verifies release script generates checksums
func TestReleasePackageScriptGeneratesChecksums(t *testing.T) {
	scriptPath := filepath.Join(repoRoot(), "scripts", "release", "prototype-package.ps1")
	content, err := os.ReadFile(scriptPath)
	if os.IsNotExist(err) {
		t.Skip("prototype-package.ps1 not found")
	}
	if err != nil {
		t.Fatalf("error reading prototype-package.ps1: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "checksum") && !strings.Contains(text, "sha256") {
		t.Error("prototype-package.ps1 should generate checksums")
	}
}

// TestReleasePackageScriptIncludesDocs verifies release script includes docs
func TestReleasePackageScriptIncludesDocs(t *testing.T) {
	scriptPath := filepath.Join(repoRoot(), "scripts", "release", "prototype-package.ps1")
	content, err := os.ReadFile(scriptPath)
	if os.IsNotExist(err) {
		t.Skip("prototype-package.ps1 not found")
	}
	if err != nil {
		t.Fatalf("error reading prototype-package.ps1: %v", err)
	}

	text := strings.ToLower(string(content))
	if !strings.Contains(text, "docs") && !strings.Contains(text, "readme") {
		t.Error("prototype-package.ps1 should include docs in release package")
	}
}

// TestReleasePackageScriptHasVersionArg verifies release script has version argument
func TestReleasePackageScriptHasVersionArg(t *testing.T) {
	scriptPath := filepath.Join(repoRoot(), "scripts", "release", "prototype-package.ps1")
	content, err := os.ReadFile(scriptPath)
	if os.IsNotExist(err) {
		t.Skip("prototype-package.ps1 not found")
	}
	if err != nil {
		t.Fatalf("error reading prototype-package.ps1: %v", err)
	}

	text := string(content)
	if !strings.Contains(text, "[string]$Version") {
		t.Error("prototype-package.ps1 should have Version parameter")
	}
}
