package conformance

import (
	"encoding/hex"
	"math/big"
	"path/filepath"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	appparams "github.com/sovereign-l1/l1/app/params"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
	contracttypes "github.com/sovereign-l1/l1/x/contracts/types"
)

func acceptanceCoin(amount int64) sdk.Coin {
	return sdk.NewCoin(appparams.BaseDenom, sdkmath.NewInt(amount))
}

func acceptanceZeroCoin() sdk.Coin {
	return sdk.NewCoin(appparams.BaseDenom, sdkmath.ZeroInt())
}

func deriveChildInit(t *testing.T, parent sdk.AccAddress, moduleHash []byte, snapshot []byte) (contracttypes.StateInit, sdk.AccAddress) {
	t.Helper()
	init := contracttypes.NewStateInit(
		addressing.FormatAccAddress(parent),
		hex.EncodeToString(moduleHash),
		snapshot,
		"",
		0,
	)
	derived, _, err := contracttypes.DeriveContractAddressFromStateInit(
		contracttypes.DefaultContractChainID,
		contracttypes.DefaultContractNamespace,
		addressing.FormatAccAddress(parent),
		init,
		contracttypes.DefaultParams(),
	)
	require.NoError(t, err)
	addr, err := addressing.ParseAccAddress(derived)
	require.NoError(t, err)
	return init, addr
}

func runGetterUint64(t *testing.T, runner *avm.Runner, res *compiler.Result, state avm.Storage, name string, self sdk.AccAddress) uint64 {
	t.Helper()
	op := opcodeForGetter(t, res, name)
	exec, err := runner.Run(res.Module, state, avm.RuntimeContext{
		Entry:           avm.EntryQuery,
		ContractAddress: self,
		Message:         async.MessageEnvelope{Opcode: op, GasLimit: 100_000},
		GasLimit:        100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	big, err := exec.ReturnValue.AsBigInt()
	require.NoError(t, err)
	return big.Uint64()
}

func runGetterAddress(t *testing.T, runner *avm.Runner, res *compiler.Result, state avm.Storage, name string, self sdk.AccAddress) string {
	t.Helper()
	op := opcodeForGetter(t, res, name)
	exec, err := runner.Run(res.Module, state, avm.RuntimeContext{
		Entry:           avm.EntryQuery,
		ContractAddress: self,
		Message:         async.MessageEnvelope{Opcode: op, GasLimit: 100_000},
		GasLimit:        100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	addr, err := exec.ReturnValue.AsAddress()
	require.NoError(t, err)
	return addr
}

func contractState(t *testing.T, executor *async.Executor, addr sdk.AccAddress) avm.Storage {
	t.Helper()
	c, ok := executor.Contract(addr)
	require.True(t, ok)
	return mustDecodeSnapshot(t, c.State)
}

// TestAcceptanceStakeFamilyExample drives the staking pool through a full
// lifecycle: deploy, stake with bounce-revert, child account deploy, a
// successful stake credit, and an unstake that cascades account -> pool ->
// staker payout.
func TestAcceptanceStakeFamilyExample(t *testing.T) {
	deployer := testAddress(0x51)
	staker := testAddress(0x52)
	opts := compiler.Options{DeployerAddress: addressing.FormatAccAddress(deployer)}

	accountRes := compileExampleFile(t, filepath.Join("stake", "stake_account.atlx"), opts)
	poolRes := compileExampleFile(t, filepath.Join("stake", "stake_pool.atlx"), opts)
	require.NoError(t, avm.VerifyInterface(accountRes.Module, accountRes.Manifest))
	require.NoError(t, avm.VerifyInterface(poolRes.Module, poolRes.Manifest))

	poolStorage := mustAVMStorage(t, map[string]any{
		"owner":       addressing.FormatAccAddress(deployer),
		"totalStaked": big.NewInt(0),
		"stakerCount": uint64(0),
		"accountCode": accountRes.CodeChunk,
		"paused":      uint32(0),
	})
	poolSnapshot := avm.EncodeSnapshot(poolStorage)

	executor := mustExecutor(t)
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	poolAddr, err := executor.DeployContract(deployer, poolRes.ModuleHash[:], []byte("stake-pool"), poolSnapshot, sdkmath.NewInt(1_000_000))
	require.NoError(t, err)
	require.NoError(t, executor.RegisterHandler(poolAddr, runner.AsyncHandler(poolRes.Module, nil, avm.RuntimeContext{})))

	require.Equal(t, uint64(0), runGetterUint64(t, runner, poolRes, mustDecodeSnapshot(t, poolSnapshot), "totalStaked", poolAddr))

	// Stake before the account child exists: the pool computes the child's
	// address itself (autoDeployAccountAddress) and auto-attaches its
	// StateInit to the outgoing credit — but delivery still fails because no
	// handler is registered for that address yet in this test harness, so it
	// bounces and the pool must revert its optimistic totals. The bounced
	// credit's Destination in the receipt is the pool's OWN computed account
	// address — using it (rather than re-deriving it independently) is what
	// keeps this test honest about what the runtime actually computed.
	stakeBody := mustCodecBody(t, poolRes.MessageBodies["Stake"], map[string]any{
		"responseTo": nil,
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      staker,
		Destination: poolAddr,
		Value:       acceptanceCoin(500),
		Opcode:      poolRes.MessageBodyOpcodes["Stake"],
		QueryID:     51,
		Body:        stakeBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err := executor.ProcessBlock(1)
	require.NoError(t, err)
	require.Len(t, receipts, 3, "expected the primary stake, the failed credit cascade, and its bounce")
	require.Equalf(t, async.ResultOK, receipts[0].ResultCode, "primary stake failed: %s", receipts[0].Error)
	require.NotEqual(t, async.ResultOK, receipts[1].ResultCode, "credit cascade should fail: no handler registered yet")
	accountAddr := receipts[1].Destination
	require.NotEmpty(t, accountAddr)

	poolState := contractState(t, executor, poolAddr)
	afterBounceTotal := runGetterUint64(t, runner, poolRes, poolState, "totalStaked", poolAddr)
	require.Equal(t, uint64(0), afterBounceTotal, "bounced credit must revert pool totals")

	// Register the account's handler now that its runtime-computed address
	// is known, then resend the SAME stake. This time the pool's cascade
	// (with its auto-attached StateInit) reaches a live handler, so the
	// child deploys and credits itself in one real, unmodified flow — no
	// hand-built StateInit or message body stands in for the runtime here.
	require.NoError(t, executor.RegisterHandler(accountAddr, runner.AsyncHandler(accountRes.Module, nil, avm.RuntimeContext{})))

	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      staker,
		Destination: poolAddr,
		Value:       acceptanceCoin(500),
		Opcode:      poolRes.MessageBodyOpcodes["Stake"],
		QueryID:     52,
		Body:        stakeBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err = executor.ProcessBlock(2)
	require.NoError(t, err)
	require.Len(t, receipts, 2, "expected the primary stake and the successful credit cascade")
	require.Equalf(t, async.ResultOK, receipts[0].ResultCode, "primary stake failed: %s", receipts[0].Error)
	require.Equalf(t, async.ResultOK, receipts[1].ResultCode, "credit cascade failed: %s", receipts[1].Error)
	require.Equal(t, accountAddr, receipts[1].Destination, "the pool must compute the same account address across calls")

	poolState = contractState(t, executor, poolAddr)
	require.Equal(t, uint64(500), runGetterUint64(t, runner, poolRes, poolState, "totalStaked", poolAddr))

	accountState := contractState(t, executor, accountAddr)
	require.Equal(t, uint64(500), runGetterUint64(t, runner, accountRes, accountState, "staked", accountAddr))
	require.Equal(t, addressing.FormatAccAddress(staker), runGetterAddress(t, runner, accountRes, accountState, "ownerOf", accountAddr))
	require.Equal(t, addressing.FormatAccAddress(poolAddr), runGetterAddress(t, runner, accountRes, accountState, "poolOf", accountAddr))

	// Unstake: staker -> account; the account debits the pool, and the pool
	// pays the staker out. The debit cascade is processed by the executor.
	unstakeBody := mustCodecBody(t, accountRes.MessageBodies["Unstake"], map[string]any{
		"amount": big.NewInt(200),
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      staker,
		Destination: accountAddr,
		Value:       acceptanceZeroCoin(),
		Opcode:      accountRes.MessageBodyOpcodes["Unstake"],
		QueryID:     54,
		Body:        unstakeBody,
		Bounce:      true,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err = executor.ProcessBlock(4)
	require.NoError(t, err)
	// Unstake and the account -> pool debit both succeed for real: the
	// account reduces its local position and the pool's own totalStaked
	// (asserted below) drops in lockstep — no bounce, no manual envelope
	// stand-in for either step. The pool's final payout targets a plain
	// staker wallet address; this test harness's async.Executor only
	// delivers messages to registered contracts (a real chain would credit
	// the wallet balance directly, e.g. via a bank-module send, bypassing
	// AVM dispatch entirely), so that last hop bounces here — that bounce
	// is expected and does not affect the debit that already committed.
	require.Len(t, receipts, 4)
	require.Equalf(t, async.ResultOK, receipts[0].ResultCode, "unstake failed: %s", receipts[0].Error)
	require.Equalf(t, async.ResultOK, receipts[1].ResultCode, "debit failed: %s", receipts[1].Error)
	require.NotEqual(t, async.ResultOK, receipts[2].ResultCode, "payout to a plain wallet is expected to fail in this harness")

	poolState = contractState(t, executor, poolAddr)
	require.Equal(t, uint64(300), runGetterUint64(t, runner, poolRes, poolState, "totalStaked", poolAddr), "the pool's own ledger must reflect the debit")

	accountState = contractState(t, executor, accountAddr)
	require.Equal(t, uint64(300), runGetterUint64(t, runner, accountRes, accountState, "staked", accountAddr), "unstake must reduce the local position")

	// Wrong-owner unstake must be rejected and change nothing.
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      testAddress(0x59),
		Destination: accountAddr,
		Value:       acceptanceZeroCoin(),
		Opcode:      accountRes.MessageBodyOpcodes["Unstake"],
		QueryID:     55,
		Body:        unstakeBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err = executor.ProcessBlock(5)
	require.NoError(t, err)
	require.NotEmpty(t, receipts)
	require.NotEqual(t, async.ResultOK, receipts[0].ResultCode)

	accountState = contractState(t, executor, accountAddr)
	require.Equal(t, uint64(300), runGetterUint64(t, runner, accountRes, accountState, "staked", accountAddr))
}
