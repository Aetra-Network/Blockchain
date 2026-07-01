package cmd

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	appparams "github.com/sovereign-l1/l1/app/params"
)

func TestFaucetCommandBuildsLocalOnlyFundingPlan(t *testing.T) {
	recipient := aeAddressForCLI(0x41)
	out, err := executeAVMCommand(
		NewFaucetCmd(),
		"send", recipient,
		"--chain-id", "aetra-local-1",
		"--amount", "123"+appparams.BaseDenom,
		"--fees", "7"+appparams.BaseDenom,
		"--node", "tcp://127.0.0.1:26657",
	)
	require.NoError(t, err)

	var plan operatorCommandPlan
	require.NoError(t, json.Unmarshal([]byte(out), &plan), out)
	require.Equal(t, "faucet send", plan.Command)
	require.Equal(t, appparams.BaseDenom, plan.Denom)
	require.Contains(t, plan.Equivalent, "scripts/localnet/fund.ps1")
	require.Contains(t, plan.Equivalent, recipient)
	require.Contains(t, plan.Equivalent, "123"+appparams.BaseDenom)
	require.NotNil(t, plan.RecipientAddress)
	require.Equal(t, recipient, plan.RecipientAddress.UserFriendly)
	requireAddressWarning(t, plan.RecipientAddress.Warnings, addressing.AddressWarningNewAddress)
	require.Contains(t, plan.Notes, "does not mint and does not edit genesis")
}

func TestFaucetRejectsNonLocalOrNonNaetFunding(t *testing.T) {
	recipient := aeAddressForCLI(0x42)
	_, err := executeAVMCommand(NewFaucetCmd(), "send", recipient, "--chain-id", "aetra-mainnet-1")
	require.ErrorContains(t, err, "non-local")

	_, err = executeAVMCommand(NewFaucetCmd(), "send", recipient, "--fees", "1uatom")
	require.ErrorContains(t, err, "naet")
}

func TestConvenienceQueriesBuildStablePlans(t *testing.T) {
	address := aeAddressForCLI(0x43)
	out, err := executeAVMCommand(NewBalancesCmd(), address)
	require.NoError(t, err)
	require.Contains(t, out, "query")
	require.Contains(t, out, "bank")
	require.Contains(t, out, appparams.BaseDenom)

	out, err = executeAVMCommand(NewValidatorsCmd())
	require.NoError(t, err)
	require.Contains(t, out, "staking")
	require.Contains(t, out, "validators")
}

func TestSystemAddressesCommandReturnsAEAndRawCatalog(t *testing.T) {
	out, err := executeAVMCommand(NewSystemAddressesCmd())
	require.NoError(t, err)

	var res struct {
		Command   string                     `json:"command"`
		Count     int                        `json:"count"`
		Addresses []addressing.SystemAddress `json:"addresses"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &res), out)
	require.Equal(t, "system-addresses", res.Command)
	require.Equal(t, len(addressing.AllSystemAddresses()), res.Count)
	require.NotEmpty(t, res.Addresses)
	for _, addr := range res.Addresses {
		require.Contains(t, addr.UserFriendly, "AE")
		require.NotEmpty(t, addr.Raw)
	}
}

func TestSystemModuleCommandSurfaceBuildsPlans(t *testing.T) {
	out, err := executeAVMCommand(NewSystemQueryCmd(), "config", "params")
	require.NoError(t, err)
	require.Contains(t, out, "query system config params")

	out, err = executeAVMCommand(NewSystemTxCmd(), "validator-registry", "register-validator", "--help")
	require.NoError(t, err)
	require.Contains(t, out, "register-validator")
}

func TestAddressInspectUsesBookAndVerifiedLabelSources(t *testing.T) {
	recipient := aeAddressForCLI(0x44)
	entry, err := addressing.NewAddressBookEntry("Treasury Ops", recipient, addressing.LabelSourceSignedAttestation, "sig:example", 90, 100)
	require.NoError(t, err)
	book := addressing.AddressBook{ChainID: "aetra-local-1", Entries: []addressing.AddressBookEntry{entry}}
	bookJSON, err := json.Marshal(book)
	require.NoError(t, err)
	bookFile := t.TempDir() + "/book.json"
	require.NoError(t, os.WriteFile(bookFile, bookJSON, 0o600))

	out, err := executeAVMCommand(NewAddressCmd(), "inspect", recipient, "--chain-id", "aetra-local-1", "--book-file", bookFile, "--current-height", "95", "--recent-window", "10")
	require.NoError(t, err)

	var display addressing.AddressDisplay
	require.NoError(t, json.Unmarshal([]byte(out), &display), out)
	require.True(t, display.Known)
	require.Equal(t, "Treasury Ops", display.VerifiedLabel)
	requireAddressWarning(t, display.Warnings, addressing.AddressWarningRecentlyCreated)
}

func TestAddressBookAddRejectsUnsignedVerifiedClaim(t *testing.T) {
	recipient := aeAddressForCLI(0x45)
	_, err := executeAVMCommand(NewAddressCmd(), "book", "add", "Ops", recipient, "--source", addressing.LabelSourceSignedAttestation)
	require.ErrorContains(t, err, "attestation")

	out, err := executeAVMCommand(NewAddressCmd(), "book", "add", "Ops", recipient, "--source", addressing.LabelSourceSignedAttestation, "--attestation", "sig:example")
	require.NoError(t, err)
	require.Contains(t, out, addressing.LabelSourceSignedAttestation)
}

func requireAddressWarning(t *testing.T, warnings []addressing.AddressWarning, code string) {
	t.Helper()
	for _, warning := range warnings {
		if warning.Code == code {
			return
		}
	}
	t.Fatalf("missing warning %s in %#v", code, warnings)
}
