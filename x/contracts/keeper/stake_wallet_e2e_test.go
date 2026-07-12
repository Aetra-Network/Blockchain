package keeper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
	"github.com/sovereign-l1/l1/x/contracts/types"
)

// payoutWalletSource is a minimal on-chain wallet contract: it accepts any
// internal message (payouts, refunds) without asserting on the opcode, so
// value sent to it is always credited. It stands in for the user-facing
// wallet product in the liquid-staking round trip below.
const payoutWalletSource = `
@storage
struct WalletStorage {
    marker: uint64
}

@message(0x7001)
struct Noop {}

type WalletMsg = Noop

contract PayoutWallet {
    storage: WalletStorage
    incomingMessages: WalletMsg

    @store
    func WalletStorage.load() {
        return WalletStorage.fromChunk(contract.getData())
    }

    @store
    func WalletStorage.save(self) {
        contract.setData(self.toChunk())
    }

    @internal
    func onInternalMessage(in: InMessage) {
        const msg = lazy WalletMsg.fromSegment(in.body)
        match (msg) {
            Noop => {
            }
            else => {
            }
        }
    }
}
`

// TestStakeWalletLiquidStakingRoundTripE2E proves the complete wallet ->
// liquid staking pool product flow through the REAL on-chain contract path
// (x/contracts keeper + autonomous EndBlock drain), using the canonical
// examples/avm/stake contracts unmodified:
//
//  1. wallet stakes value into the pool (queued internal message, delivered
//     by the EndBlock drain, no manual MsgReceiveInternalMessage);
//  2. the pool auto-deploys the staker's per-owner StakeAccount child from
//     its own StateInit derivation and credits the position;
//  3. the wallet unstakes; the account debits the pool and the pool pays the
//     value back out to the wallet;
//  4. totals, per-position storage, and wallet balance all reconcile.
func TestStakeWalletLiquidStakingRoundTripE2E(t *testing.T) {
	owner := aeAddress("11")
	k := NewKeeperWithAccountStatus(testAccountStatus{owner: accountStatusActive})

	// Turn the autonomous internal-message drain on (default is off).
	gs := k.ExportGenesis()
	gs.Params.MaxInternalMessageGasPerBlock = 1_000_000_000
	require.NoError(t, k.InitGenesis(gs))

	accountSrc, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "avm", "stake", "stake_account.atlx"))
	require.NoError(t, err)
	poolSrc, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "avm", "stake", "stake_pool.atlx"))
	require.NoError(t, err)

	c := mustFamilyCompiler(t)
	accountRes, err := c.Compile(accountSrc)
	require.NoError(t, err)
	poolRes, err := c.Compile(poolSrc)
	require.NoError(t, err)
	walletRes, err := c.Compile([]byte(payoutWalletSource))
	require.NoError(t, err)

	accountCodeID := storeCompiledCode(t, &k, owner, accountRes)
	poolCodeID := storeCompiledCode(t, &k, owner, poolRes)
	walletCodeID := storeCompiledCode(t, &k, owner, walletRes)

	poolInit, err := poolRes.StorageCodec.Encode(map[string]any{
		"owner":       owner,
		"totalStaked": uint64(0),
		"stakerCount": uint64(0),
		"accountCode": accountRes.CodeChunk,
		"paused":      uint32(0),
	})
	require.NoError(t, err)
	pool, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: owner,
		CodeID:  poolCodeID,
		InitMsg: poolInit,
		Funds:   10_000_000_000,
		Admin:   owner,
		Salt:    "stake-pool-e2e",
		Height:  10,
	})
	require.NoError(t, err)

	walletInit, err := walletRes.StorageCodec.Encode(map[string]any{"marker": uint64(0)})
	require.NoError(t, err)
	const walletStartBalance = uint64(5_000_000_000)
	wallet, err := k.InstantiateContract(types.MsgInstantiateContract{
		Creator: owner,
		CodeID:  walletCodeID,
		InitMsg: walletInit,
		Funds:   walletStartBalance,
		Admin:   owner,
		Salt:    "staker-wallet-e2e",
		Height:  11,
	})
	require.NoError(t, err)

	// --- Stake: the wallet sends 1 AET into the pool. ---
	const stakeAmount = uint64(1_000_000_000)
	stakeBody, err := poolRes.MessageBodies["Stake"].Encode(map[string]any{"responseTo": nil})
	require.NoError(t, err)
	enqueueInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: wallet.ContractAddressUser,
		DestinationAccount: pool.ContractAddressUser,
		Funds:              stakeAmount,
		Opcode:             poolRes.MessageBodyOpcodes["Stake"],
		QueryID:            1,
		Body:               stakeBody,
		GasLimit:           500_000,
		LogicalTime:        21,
		Height:             21,
	})

	ctx := sdk.NewContext(nil, cmtproto.Header{Height: 22}, false, log.NewNopLogger())
	drainQueue(t, &k, ctx)

	// The pool auto-deployed the staker's account: exactly one new contract
	// with the account code exists.
	accountAddr := findContractByCode(t, &k, accountCodeID)
	require.NotEqual(t, pool.ContractAddressUser, accountAddr)
	require.NotEqual(t, wallet.ContractAddressUser, accountAddr)

	require.Equal(t, stakeAmount, runKeeperGetterUint64(t, &k, poolRes, pool.ContractAddressUser, "totalStaked"))
	require.Equal(t, uint64(1), runKeeperGetterUint64(t, &k, poolRes, pool.ContractAddressUser, "stakerCount"))
	require.Equal(t, stakeAmount, runKeeperGetterUint64(t, &k, accountRes, accountAddr, "staked"))

	walletAfterStake := contractBalance(t, &k, wallet.ContractAddressUser)
	require.Less(t, walletAfterStake, walletStartBalance)

	// --- Unstake: the wallet (the position owner) asks its account for the
	// full amount back; account debits the pool; pool pays the wallet. ---
	unstakeBody, err := accountRes.MessageBodies["Unstake"].Encode(map[string]any{"amount": stakeAmount})
	require.NoError(t, err)
	enqueueInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: wallet.ContractAddressUser,
		DestinationAccount: accountAddr,
		Funds:              100_000,
		Opcode:             accountRes.MessageBodyOpcodes["Unstake"],
		QueryID:            2,
		Body:               unstakeBody,
		GasLimit:           500_000,
		LogicalTime:        31,
		Height:             31,
	})
	drainQueue(t, &k, ctx)

	require.Equal(t, uint64(0), runKeeperGetterUint64(t, &k, poolRes, pool.ContractAddressUser, "totalStaked"))
	require.Equal(t, uint64(0), runKeeperGetterUint64(t, &k, accountRes, accountAddr, "staked"))

	// The payout landed back in the wallet: its balance recovered by the
	// staked amount, minus only the small Unstake forward value and the
	// storage rent charged across the flow's blocks (both orders of
	// magnitude below the 1 AET stake).
	walletFinal := contractBalance(t, &k, wallet.ContractAddressUser)
	require.Greater(t, walletFinal, walletAfterStake)
	const feeAndRentTolerance = uint64(1_000_000) // 0.001 AET
	require.GreaterOrEqual(t, walletFinal+feeAndRentTolerance, walletAfterStake+stakeAmount)
}

// drainQueue runs the autonomous EndBlock drain until the internal-message
// queue is empty, failing if it does not converge (a message that can never
// deliver would otherwise loop forever).
func drainQueue(t *testing.T, k *Keeper, ctx sdk.Context) {
	t.Helper()
	for i := 0; i < 8; i++ {
		if len(k.ExportGenesis().State.InternalMessages) == 0 {
			return
		}
		require.NoError(t, k.EndBlocker(ctx))
	}
	require.Empty(t, k.ExportGenesis().State.InternalMessages, "internal message queue did not drain")
}

// findContractByCode returns the single contract instantiated from codeID.
func findContractByCode(t *testing.T, k *Keeper, codeID string) string {
	t.Helper()
	var found []string
	for _, contract := range k.ExportGenesis().State.Contracts {
		if contract.CodeID == codeID {
			found = append(found, contract.AddressUser)
		}
	}
	require.Len(t, found, 1, "expected exactly one contract for code %s", codeID)
	return found[0]
}

func contractBalance(t *testing.T, k *Keeper, address string) uint64 {
	t.Helper()
	query, err := k.Contract(types.QueryContractRequest{ContractAddress: address})
	require.NoError(t, err)
	require.True(t, query.Found)
	return query.Contract.Balance
}

// runKeeperGetterUint64 executes a @get entrypoint against the contract's
// committed storage snapshot from the keeper, mirroring how a read-only query
// node serves getters.
func runKeeperGetterUint64(t *testing.T, k *Keeper, res *compiler.Result, address string, getter string) uint64 {
	t.Helper()
	query, err := k.Contract(types.QueryContractRequest{ContractAddress: address})
	require.NoError(t, err)
	require.True(t, query.Found)
	storage, err := avm.DecodeSnapshot(query.Contract.Data)
	require.NoError(t, err)

	var opcode uint32
	foundGetter := false
	for _, method := range res.Manifest.Methods {
		if method.Name == getter {
			opcode = method.Opcode
			foundGetter = true
			break
		}
	}
	if !foundGetter {
		for _, entry := range res.SelectorRegistry.Entries {
			if entry.Name == getter {
				opcode = entry.Selector
				foundGetter = true
				break
			}
		}
	}
	require.True(t, foundGetter, "getter %q not found in manifest or selector registry", getter)

	self, err := addressing.ParseAccAddress(address)
	require.NoError(t, err)
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	exec, err := runner.Run(res.Module, storage, avm.RuntimeContext{
		Entry:           avm.EntryQuery,
		ContractAddress: self,
		Message:         async.MessageEnvelope{Opcode: opcode, GasLimit: 100_000},
		GasLimit:        100_000,
	})
	require.NoError(t, err)
	require.Equal(t, async.ResultOK, exec.ResultCode)
	value, err := exec.ReturnValue.AsBigInt()
	require.NoError(t, err)
	return value.Uint64()
}
