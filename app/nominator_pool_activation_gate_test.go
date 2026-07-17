package app

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	nominatorpoolkeeper "github.com/sovereign-l1/l1/x/nominator-pool/keeper"
	nominatorpooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
	nativeaccounttypes "github.com/sovereign-l1/l1/x/native-account/types"
)

// poolWalletIdentity is the v2 identity a plain wallet address activates under
// -- exactly what the pool's ensureActiveWallet derives internally, computed
// here through the same addressing calls (Parse then NormalizeToAccountIdentity)
// so a test cannot silently drift onto a different address form than the code
// under test.
//
// It is the identity, NOT the plain address, because that is what
// native-account keys an activation by; the plain address stays the money
// address (see commit 4fad3ae2, which had to undo a rewrite that confused the
// two and made every live deposit debit an empty account).
func poolWalletIdentity(t *testing.T, walletAE string) addressing.AddressPair {
	t.Helper()

	seed, err := addressing.Parse(walletAE)
	require.NoError(t, err)
	identity, err := addressing.NormalizeToAccountIdentity(seed)
	require.NoError(t, err)
	user, err := addressing.FormatUserFriendly(identity)
	require.NoError(t, err)
	return addressing.AddressPair{Role: addressing.AddressRoleAccount, User: user, Raw: addressing.Format(identity)}
}

// activatePoolWallet gives the plain wallet address `wallet` (an sdk.AccAddress)
// a native-account record with the given status. See activatePoolWalletAE.
func activatePoolWallet(t *testing.T, app *L1App, ctx sdk.Context, wallet sdk.AccAddress, accountNumber uint64, status string) nativeaccounttypes.Account {
	t.Helper()
	return activatePoolWalletAE(t, app, ctx, addressing.FormatAccAddress(wallet), accountNumber, status)
}

// activatePoolWalletAE gives the plain wallet address walletAE a
// native-account record with the given status, stored under the v2 identity
// that wallet derives to. accountNumber must be unique per account within one
// app -- native-account keeps a uniqueness index on it.
//
// This is what an ordinary user does with `l1d tx native-account activate`
// before touching the chain's gated surface; SetAccount is the test-side
// shortcut for it (the same one app/avm_runtime_system_test.go uses to drive
// x/contracts' identical gate).
//
// Every app-level pool test that deposits now has to call this, and that is the
// point: before D3 the pool's activation gate was wired to nothing, so all of
// them deposited from wallets the chain had never heard of and passed. The
// calls are the diff that shows how much of the gate was dead.
func activatePoolWalletAE(t *testing.T, app *L1App, ctx sdk.Context, walletAE string, accountNumber uint64, status string) nativeaccounttypes.Account {
	t.Helper()

	pair := poolWalletIdentity(t, walletAE)
	account := nativeaccounttypes.Account{
		Version:                 nativeaccounttypes.CurrentAccountVersion,
		AddressUser:             pair.User,
		AddressRaw:              pair.Raw,
		AccountNumber:           accountNumber,
		Status:                  status,
		AuthPolicy:              nativeaccounttypes.AuthPolicy{Version: 1, Mode: nativeaccounttypes.AuthModeSingleKey},
		CreatedHeight:           1,
		LastActiveHeight:        1,
		LastStorageChargeHeight: 1,
	}
	require.NoError(t, app.NativeAccountKeeper.SetAccount(ctx, account))
	return account
}

// TestNominatorPoolDepositRejectsFrozenAndInactiveWallets is the D3 regression.
//
// x/nominator-pool has always declared an activation gate -- ensureActiveWallet,
// called from the deposit, unbond and claim paths -- and it has never once run.
// Two independent faults kept it dead, and BOTH had to be fixed:
//
//  1. app/keepers.go wired bank, staking and distribution custody onto the
//     keeper but never called WithAccountStatusReader, so accountStatusReader
//     was nil and ensureActiveWallet's first line returned nil for everybody.
//  2. The reader could not have been wired even deliberately: the keeper
//     declared AccountStatus(string) (string, bool) while native-account's real
//     method is AccountStatus(ctx, string) (string, bool, error). No type in
//     this binary satisfied the interface. (x/contracts, which enforces the very
//     same gate, has always declared the real signature -- see its
//     AccountStatusReader. nominator-pool's was simply written wrong.)
//
// Fault 2 is why the module's own keeper unit tests never caught fault 1: they
// wire a hand-written fixture that matched the WRONG signature, so the gate was
// exercised in tests and dead in production. That is exactly why this
// regression lives at the app level, against the real wiring, and asserts on
// the real native-account keeper. Against the pre-fix tree the frozen and
// never-activated deposits below both SUCCEED.
func TestNominatorPoolDepositRejectsFrozenAndInactiveWallets(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(10)

	validator := GetBondedTestValidator(t, app, ctx)
	genesis, err := app.NominatorPoolKeeper.ExportGenesisState(ctx)
	require.NoError(t, err)
	authority := genesis.Params.Authority

	srv := nominatorpoolkeeper.NewMsgServerImpl(&app.NominatorPoolKeeper)
	contractUser, contractRaw := nominatorPoolAddressPair(t, "61")
	const poolID = "activation-gate-pool"
	_, err = srv.CreateOfficialLiquidStakingPool(ctx, &nominatorpooltypes.MsgCreateOfficialLiquidStakingPool{
		Authority:           authority,
		PoolID:              poolID,
		ContractAddressUser: contractUser,
		ContractAddressRaw:  contractRaw,
		PoolOperator:        nominatorPoolRawAddress("62"),
		ValidatorTarget:     validator.OperatorAddress,
		PoolCommissionBps:   500,
		Height:              10,
	})
	require.NoError(t, err)

	const funded = int64(100_000_000_000) // 100 AET
	wallets := AddTestAddrsIncremental(app, ctx, 3, sdkmath.NewInt(funded))
	frozen, never, active := wallets[0], wallets[1], wallets[2]

	// The AE user-facing form of the wallet's own PLAIN address: what it signs
	// with and holds its balance at (see commit 4fad3ae2). NOT its identity.
	deposit := func(wallet sdk.AccAddress) error {
		_, err := srv.DepositToStakingPool(ctx, &nominatorpooltypes.MsgDepositToStakingPool{
			PoolID:        poolID,
			WalletAddress: addressing.FormatAccAddress(wallet),
			Amount:        nominatorpooltypes.DefaultMinPoolDeposit,
			Height:        10,
		})
		return err
	}

	// A frozen wallet: activated once, then frozen for storage debt. The pool
	// must refuse it and say why.
	activatePoolWallet(t, app, ctx, frozen, 9001, nativeaccounttypes.AccountStatusFrozen)
	err = deposit(frozen)
	require.Error(t, err, "a frozen wallet must not be able to deposit into a staking pool")
	require.ErrorContains(t, err, "frozen wallet")

	// A wallet that never activated at all has no native-account record, so
	// AccountStatus reports not-found. Funded and able to sign, but not a
	// participant in the chain's account model yet.
	err = deposit(never)
	require.Error(t, err, "a never-activated wallet must not be able to deposit into a staking pool")
	require.ErrorContains(t, err, "requires active wallet")

	// The gate must not be a blanket denial: the same call from an active
	// wallet still works. Without this the two assertions above would pass on
	// a keeper that simply rejected everything.
	activatePoolWallet(t, app, ctx, active, 9003, nativeaccounttypes.AccountStatusActive)
	require.NoError(t, deposit(active), "an active wallet must still be able to deposit")

	// And the money really moved for the active wallet only: proof the gate
	// rejected BEFORE depositCustody's bank debit rather than after it.
	require.Equal(t, funded, app.BankKeeper.GetBalance(ctx, frozen, "naet").Amount.Int64(),
		"a rejected deposit must not have debited the frozen wallet")
	require.Equal(t, funded, app.BankKeeper.GetBalance(ctx, never, "naet").Amount.Int64(),
		"a rejected deposit must not have debited the never-activated wallet")
	require.Equal(t, funded-int64(nominatorpooltypes.DefaultMinPoolDeposit),
		app.BankKeeper.GetBalance(ctx, active, "naet").Amount.Int64(),
		"the accepted deposit must have debited the active wallet")
}

// TestNominatorPoolKeeperHasAccountStatusReaderWired pins fault 1 on its own,
// independently of behaviour. A future refactor that drops the
// WithAccountStatusReader call from app/keepers.go would restore the silent
// nil-reader return -- every gated path would go back to accepting everybody,
// and every behavioural test above would still pass if it were also changed to
// stop activating its wallets. This asserts the wire itself.
func TestNominatorPoolKeeperHasAccountStatusReaderWired(t *testing.T) {
	app := Setup(t, false)
	require.True(t, app.NominatorPoolKeeper.HasAccountStatusReader(),
		"app/keepers.go must wire WithAccountStatusReader onto the nominator pool keeper; "+
			"without it ensureActiveWallet returns nil for every wallet and the activation gate is dead code")
}
