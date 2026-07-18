package conformance

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

// mintRequestOpcode is the literal opcode declared on
// examples/avm/capability/mint_capability.atlx's @message(0x9101) MintRequest.
const mintRequestOpcode = 0x9101

// mustEncodeMintRequest encodes a MintRequest message body (tokenId, amount)
// through the compiler-generated message-body codec, the same convention
// used for every @message struct's wire body.
func mustEncodeMintRequest(t *testing.T, res *compiler.Result, tokenID, amount uint64) []byte {
	t.Helper()
	codec, ok := res.MessageBodies["MintRequest"]
	require.True(t, ok, "compiled example is missing the MintRequest message body codec")
	body, err := codec.Encode(map[string]any{"tokenId": tokenID, "amount": amount})
	require.NoError(t, err)
	return body
}

// mintVaultStorage builds the initial VaultStorage snapshot for the
// mint-capability example: an owner address, the one tokenId this vault's
// capability recognizes, and a starting totalSupply.
func mintVaultStorage(t *testing.T, owner string, allowedTokenID, totalSupply uint64) avm.Storage {
	t.Helper()
	return mustAVMStorage(t, map[string]any{
		"owner":          owner,
		"allowedTokenId": allowedTokenID,
		"totalSupply":    totalSupply,
	})
}

// TestMintCapabilityGrantsMintWhenTokenMatches proves the capability-pattern
// reference contract (examples/avm/capability/mint_capability.atlx) actually
// executes: presenting a mint request for the vault's own allowedTokenId
// succeeds and increases totalSupply. The gate (`canMint`) is a @pure helper
// that reads a field off a `cap: MintCapability` struct value constructed
// from the vault's own storage -- "authorization by holding a typed object",
// not by comparing the caller's address.
func TestMintCapabilityGrantsMintWhenTokenMatches(t *testing.T) {
	owner := testAddress(0x21)
	res := compileExampleFile(t, "capability/mint_capability.atlx", compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(owner),
	})

	storage := mintVaultStorage(t, addressing.FormatAccAddress(owner), 7, 100)

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	body := mustEncodeMintRequest(t, res, 7, 25)
	exec, err := runner.Run(res.Module, storage, avm.RuntimeContext{
		Entry:    avm.EntryReceiveInternal,
		Message:  async.MessageEnvelope{Opcode: mintRequestOpcode, Body: body, GasLimit: 200_000},
		GasLimit: 200_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	require.Equal(t, uint64(125), avm.DecodeU64(exec.State["totalSupply"]))

	getExec, err := runner.Run(res.Module, exec.State, avm.RuntimeContext{
		Entry:    avm.EntryQuery,
		Message:  async.MessageEnvelope{Opcode: opcodeForGetter(t, res, "totalSupply"), GasLimit: 100_000},
		GasLimit: 100_000,
	})
	require.NoError(t, err)
	got, err := getExec.ReturnValue.AsUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(125), got)
}

// TestMintCapabilityRejectsMintWhenTokenMismatches proves the negative case:
// presenting a mint request for a DIFFERENT tokenId than the vault's
// capability recognizes traps (deterministic rollback via `assert ... throw
// ERR_BAD_CAPABILITY`), leaving totalSupply untouched -- the capability gate
// actually gates, it isn't a no-op.
func TestMintCapabilityRejectsMintWhenTokenMismatches(t *testing.T) {
	owner := testAddress(0x22)
	res := compileExampleFile(t, "capability/mint_capability.atlx", compiler.Options{
		DeployerAddress: addressing.FormatAccAddress(owner),
	})

	storage := mintVaultStorage(t, addressing.FormatAccAddress(owner), 7, 100)

	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)

	body := mustEncodeMintRequest(t, res, 8, 25) // wrong tokenId: vault only recognizes 7
	exec, err := runner.Run(res.Module, storage, avm.RuntimeContext{
		Entry:    avm.EntryReceiveInternal,
		Message:  async.MessageEnvelope{Opcode: mintRequestOpcode, Body: body, GasLimit: 200_000},
		GasLimit: 200_000,
	})
	// A trap is a deterministic rollback: Run returns a non-OK ResultCode
	// (and, for an assert/throw abort, a non-nil error) -- never a Go panic.
	require.NotEqualf(t, async.ResultOK, exec.ResultCode, "mint with a mismatched capability token must trap, not succeed (err=%v)", err)
	require.Equal(t, storage, exec.State, "a trapped mint must leave storage untouched (deterministic rollback)")
}
