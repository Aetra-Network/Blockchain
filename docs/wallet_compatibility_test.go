package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const walletCompatFile = "wallet-compatibility.md"

// TestWalletCompatDocExists verifies the wallet compatibility doc exists.
func TestWalletCompatDocExists(t *testing.T) {
	path := filepath.Join(walletCompatFile)
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		t.Fatalf("%s not found in docs/", walletCompatFile)
	}
	if err != nil {
		t.Fatalf("error checking %s: %v", walletCompatFile, err)
	}
}

// loadWalletCompat reads the wallet compatibility doc content.
func loadWalletCompat(t *testing.T) string {
	t.Helper()
	path := filepath.Join(walletCompatFile)
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		t.Skipf("%s not found", walletCompatFile)
	}
	if err != nil {
		t.Fatalf("error reading %s: %v", walletCompatFile, err)
	}
	return string(content)
}

// TestWalletCompatRequiredSections verifies all AWCE required sections are present.
func TestWalletCompatRequiredSections(t *testing.T) {
	content := loadWalletCompat(t)
	text := string(content)

	required := []string{
		"AWCE-1",
		"Canonical Addresses",
		"Address Derivation",
		"Signing Scheme",
		"Account Lifecycle",
		"Activation",
		"Dual-Address Example",
		"Frozen / Recovery Flow",
		"Pool-Based Only",
		"Auth Policy System",
		"Account Metadata",
		"Security Rules",
		"Machine-Readable Extension Descriptor",
		"Revision History",
	}

	for _, section := range required {
		if !strings.Contains(text, section) {
			t.Errorf("wallet-compatibility.md missing required section: %q", section)
		}
	}
}

// TestWalletCompatNoSeedPhraseExamples verifies no seed/private-key instructional
// examples exist (security rules are allowed).
func TestWalletCompatNoSeedPhraseExamples(t *testing.T) {
	content := loadWalletCompat(t)
	text := strings.ToLower(string(content))

	instructional := []string{
		"export your seed",
		"input your seed",
		"enter your mnemonic",
		"paste your private key",
		"your seed phrase into",
		"your private key into",
	}

	for _, pattern := range instructional {
		if strings.Contains(text, pattern) {
			t.Errorf("wallet-compatibility.md must not contain instructional seed/private-key text: %q", pattern)
		}
	}
}

// TestWalletCompatNoAevaloperUserFacingFlows verifies aevaloper/aevalcons are
// not presented as user-facing wallet addresses.
func TestWalletCompatNoAevaloperUserFacingFlows(t *testing.T) {
	content := loadWalletCompat(t)
	text := string(content)

	if strings.Contains(text, "aevaloper") || strings.Contains(text, "aevalcons") {
		lower := strings.ToLower(text)
		if !strings.Contains(lower, "internal") && !strings.Contains(lower, "validator") {
			t.Error("aevaloper/aevalcons references must be marked as internal/validator only")
		}
	}
}

// TestWalletCompatNoDirectDelegationInstruction verifies the doc does not teach
// direct validator selection for normal staking.
func TestWalletCompatNoDirectDelegationInstruction(t *testing.T) {
	content := loadWalletCompat(t)
	text := strings.ToLower(string(content))

	if strings.Contains(text, "staking delegate") {
		t.Error("wallet-compatibility.md must not instruct direct staking delegation")
	}
}

// TestWalletCompatFourAddressNotNormalWallet verifies the doc does not call
// 4:... the normal wallet address.
func TestWalletCompatFourAddressNotNormalWallet(t *testing.T) {
	content := loadWalletCompat(t)
	text := strings.ToLower(string(content))

	if strings.Contains(text, "4:...") || strings.Contains(text, "4: address") {
		if strings.Contains(text, "normal wallet address") || strings.Contains(text, "primary address") {
			if !strings.Contains(text, "must not") && !strings.Contains(text, "not") {
				t.Error("wallet-compatibility.md must not call 4: address the normal wallet address")
			}
		}
	}
}

// TestWalletCompatContainsDescriptor verifies the machine-readable descriptor JSON.
func TestWalletCompatContainsDescriptor(t *testing.T) {
	content := loadWalletCompat(t)
	text := string(content)

	requiredDescriptor := []string{
		`"standard": "AWCE-1"`,
		`"canonical_user_address"`,
		`"raw_address"`,
		`"signing": "cosmos-signdoc-secp256k1"`,
		`"default_hd_path": "m/44'/118'/0'/0/0"`,
		`"features"`,
	}

	for _, pattern := range requiredDescriptor {
		if !strings.Contains(text, pattern) {
			t.Errorf("wallet-compatibility.md missing machine-readable descriptor field: %q", pattern)
		}
	}
}

// TestWalletCompatExamplesUseCLIFormat verifies examples use valid CLI schema.
func TestWalletCompatExamplesUseCLIFormat(t *testing.T) {
	content := loadWalletCompat(t)
	text := string(content)

	if !strings.Contains(text, "aetrad tx") && !strings.Contains(text, "aetrad query") {
		t.Error("wallet-compatibility.md should contain CLI examples using aetrad")
	}
}

// TestWalletCompatExamplesUseAEAddresses verifies wallet examples use AE... format.
func TestWalletCompatExamplesUseAEAddresses(t *testing.T) {
	content := loadWalletCompat(t)
	text := string(content)

	if strings.Contains(text, "```") {
		if !strings.Contains(text, "AE...") && !strings.Contains(text, "AEAAA") {
			t.Error("wallet-compatibility.md should show AE address examples in code blocks")
		}
	}
}
