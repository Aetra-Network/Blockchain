package keeper

import (
	"context"
	"errors"
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/contracts/types"
)

// payoutMockBank is an in-memory bank ledger scoped to storageRentBaseDenom,
// the same shape as x/identity-root/keeper/collection_test.go's mockBank. It
// ENFORCES non-negativity by erroring on any transfer that would overdraw an
// account, so a bug that tried to pay out more than the reserve module holds
// fails the test immediately rather than silently minting value.
type payoutMockBank struct {
	balances map[string]sdkmath.Int
}

func newPayoutMockBank() *payoutMockBank { return &payoutMockBank{balances: map[string]sdkmath.Int{}} }

func (b *payoutMockBank) get(addr string) sdkmath.Int {
	if v, ok := b.balances[addr]; ok {
		return v
	}
	return sdkmath.ZeroInt()
}

func (b *payoutMockBank) set(addr string, v sdkmath.Int) { b.balances[addr] = v }

func (b *payoutMockBank) move(from, to string, amount sdkmath.Int) error {
	if b.get(from).LT(amount) {
		return errors.New("payout mock bank: insufficient balance (would go negative)")
	}
	b.set(from, b.get(from).Sub(amount))
	b.set(to, b.get(to).Add(amount))
	return nil
}

func (b *payoutMockBank) SendCoinsFromAccountToModule(_ context.Context, sender sdk.AccAddress, module string, amt sdk.Coins) error {
	return b.move(sender.String(), authtypes.NewModuleAddress(module).String(), amt.AmountOf(storageRentBaseDenom))
}

func (b *payoutMockBank) SendCoinsFromModuleToAccount(_ context.Context, module string, recipient sdk.AccAddress, amt sdk.Coins) error {
	return b.move(authtypes.NewModuleAddress(module).String(), recipient.String(), amt.AmountOf(storageRentBaseDenom))
}

func (b *payoutMockBank) SendCoinsFromModuleToModule(_ context.Context, senderModule, recipientModule string, amt sdk.Coins) error {
	return b.move(authtypes.NewModuleAddress(senderModule).String(), authtypes.NewModuleAddress(recipientModule).String(), amt.AmountOf(storageRentBaseDenom))
}

// newWalletPayoutTestKeeper builds a Keeper wired with a payoutMockBank, with
// the reserve module pre-funded to reserveBalance naet so SEND_PAYOUT_TO_WALLET
// deliveries have real (mock) coins to move. k.runtimeCtx is stamped directly
// (same-package field access) since these tests never go through loadForBlock;
// the mock bank ignores its context argument entirely, matching the other
// bank-keeper mocks in this repo (see x/identity-root/keeper/collection_test.go).
//
// owner is also pre-funded generously: with a real (mock, here) bank keeper
// wired in, StoreCode/InstantiateContract's own initial-storage-rent
// collection (k.collectRentPayment, a no-op when bankKeeper is nil, as it is
// in every other keeper_test.go case) now actually executes a bank transfer,
// so the test's contract owner needs spendable balance for those unrelated
// setup calls to succeed before a wallet-payout delivery is ever attempted.
func newWalletPayoutTestKeeper(t *testing.T, owner string, reserveBalance uint64) (Keeper, *payoutMockBank) {
	t.Helper()
	status := testAccountStatus{owner: accountStatusActive}
	k := NewKeeperWithAccountStatus(status)
	bank := newPayoutMockBank()
	bank.set(authtypes.NewModuleAddress(storageRentReserveModule).String(), sdkmath.NewIntFromUint64(reserveBalance))
	ownerAddr, err := aetraaddress.ParseAccAddress(owner)
	require.NoError(t, err)
	bank.set(ownerAddr.String(), sdkmath.NewIntFromUint64(1_000_000_000))
	k = k.WithBankKeeper(bank)
	k.runtimeCtx = context.Background()
	return k, bank
}

// TestWalletPayoutMovesFundsToRealWalletAndDebitsContractBalance is the
// acceptance case for Phase 0's SEND_PAYOUT_TO_WALLET primitive: a contract
// emitting an internal message whose destination is a plain wallet address
// (not a registered contract) and whose Mode carries
// async.SendModeWalletPayout must (a) debit its own ledger Balance by
// record.Funds and (b) move the SAME amount of real naet out of the
// contracts module's storage-rent reserve into the destination wallet's real
// bank balance.
func TestWalletPayoutMovesFundsToRealWalletAndDebitsContractBalance(t *testing.T) {
	owner := aeAddress("11")
	recipient := aeAddress("aa")
	const reserveBalance = uint64(10_000)
	const payout = uint64(500)

	k, bank := newWalletPayoutTestKeeper(t, owner, reserveBalance)

	codeHash := storeContractCode(t, &k, owner)
	source := instantiateContract(t, &k, owner, codeHash, "payout-source", 5, 1_000, 0)

	recipientAddr, err := aetraaddress.ParseAccAddress(recipient)
	require.NoError(t, err)

	before := k.ExportGenesis()
	sumBefore := sumContractBalances(before.State.Contracts)
	// Captured AFTER instantiateContract (which already collected its own
	// initial-storage-rent fee into the reserve module) so this delivery's
	// effect on the reserve module can be isolated below.
	reserveBefore := bank.get(authtypes.NewModuleAddress(storageRentReserveModule).String())

	delivered, err := deliverInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: source.ContractAddressUser,
		DestinationAccount: recipient,
		Funds:              payout,
		Mode:               async.SendModeWalletPayout,
		GasLimit:           100_000,
		LogicalTime:        5,
		Height:             5,
	})
	require.NoError(t, err)
	require.Equal(t, recipient, delivered.DestinationAccount)

	after := k.ExportGenesis()

	// The source contract's ledger Balance is debited by exactly the payout.
	sourceAfter, found := findContract(after.State.Contracts, source.ContractAddressUser)
	require.True(t, found)
	require.Equal(t, uint64(1_000-payout), sourceAfter.Balance)

	// No contract was created at the recipient address -- it is a plain
	// wallet, never a ledger entry.
	_, recipientIsContract := findContract(after.State.Contracts, recipient)
	require.False(t, recipientIsContract)

	// Value conservation across the CONTRACT ledger: total contract balances
	// drop by exactly the payout amount (the value legitimately left the
	// contract system for a real wallet, it did not evaporate or duplicate).
	sumAfter := sumContractBalances(after.State.Contracts)
	require.Equal(t, sumBefore-payout, sumAfter)

	// The real (mock) bank balance moved: reserve module down by payout,
	// recipient up by payout.
	require.True(t, reserveBefore.Sub(sdkmath.NewIntFromUint64(payout)).Equal(bank.get(authtypes.NewModuleAddress(storageRentReserveModule).String())))
	require.Equal(t, sdkmath.NewIntFromUint64(payout), bank.get(recipientAddr.String()))

	// The receipt records the dedicated wallet-payout operation, distinct
	// from an ordinary contract-to-contract delivery.
	foundReceipt := false
	for _, r := range after.State.Receipts {
		if r.ContractAddress == source.ContractAddressUser && r.Operation == "internal_message_wallet_payout" && r.Amount == payout {
			foundReceipt = true
		}
	}
	require.True(t, foundReceipt, "expected an internal_message_wallet_payout receipt")
}

// TestWalletPayoutRejectsInsufficientContractBalanceWithoutPartialEffect
// proves an underfunded SEND_PAYOUT_TO_WALLET delivery fails atomically: no
// ledger debit, no bank transfer, and the message stays queued for retry --
// exactly the same shape TestReceiveInternalMessageAutoDeployFailureIsAtomic
// already establishes for the auto-deploy branch.
func TestWalletPayoutRejectsInsufficientContractBalanceWithoutPartialEffect(t *testing.T) {
	owner := aeAddress("22")
	recipient := aeAddress("bb")
	const reserveBalance = uint64(10_000)

	k, bank := newWalletPayoutTestKeeper(t, owner, reserveBalance)

	codeHash := storeContractCode(t, &k, owner)
	source := instantiateContract(t, &k, owner, codeHash, "payout-underfunded", 5, 100, 0)

	before := k.ExportGenesis()
	sumBefore := sumContractBalances(before.State.Contracts)
	reserveBefore := bank.get(authtypes.NewModuleAddress(storageRentReserveModule).String())

	_, err := deliverInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: source.ContractAddressUser,
		DestinationAccount: recipient,
		Funds:              1_000, // exceeds the source's 100 balance
		Mode:               async.SendModeWalletPayout,
		GasLimit:           100_000,
		LogicalTime:        5,
		Height:             5,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient balance")

	after := k.ExportGenesis()
	sumAfter := sumContractBalances(after.State.Contracts)
	require.Equal(t, sumBefore, sumAfter, "a rejected payout must not change any contract's ledger balance")

	sourceAfter, found := findContract(after.State.Contracts, source.ContractAddressUser)
	require.True(t, found)
	require.Equal(t, uint64(100), sourceAfter.Balance, "source balance must be unchanged after a rejected payout")

	reserveAfter := bank.get(authtypes.NewModuleAddress(storageRentReserveModule).String())
	require.True(t, reserveBefore.Equal(reserveAfter), "a rejected payout must not move any real bank funds")

	foundQueued := false
	for _, m := range after.State.InternalMessages {
		if m.DestinationAccount == recipient {
			foundQueued = true
		}
	}
	require.True(t, foundQueued, "a rejected payout must leave the message queued for retry")
}

// TestWalletPayoutModeIsIgnoredWhenDestinationIsARealContract proves the
// destination-lookup precedence explicitly required by the plan: a message
// whose destination IS a registered contract is delivered through the
// ordinary contract-to-contract path even if (by mistake, or a compromised
// sender) its Mode also carries async.SendModeWalletPayout -- the wallet
// payout branch is reached only once findContractWithIndex has already
// missed.
func TestWalletPayoutModeIsIgnoredWhenDestinationIsARealContract(t *testing.T) {
	owner := aeAddress("33")
	const reserveBalance = uint64(10_000)

	k, _ := newWalletPayoutTestKeeper(t, owner, reserveBalance)

	codeHash := storeContractCode(t, &k, owner)
	source := instantiateContract(t, &k, owner, codeHash, "payout-precedence-source", 5, 1_000, 0)
	destination := instantiateContract(t, &k, owner, codeHash, "payout-precedence-dest", 5, 0, 0)

	const funds = uint64(250)
	_, err := deliverInternalForTest(t, &k, types.InternalMessage{
		SourceContractUser: source.ContractAddressUser,
		DestinationAccount: destination.ContractAddressUser,
		Funds:              funds,
		// Nonsensical combination (destination is a real contract, but the
		// wallet-payout bit is set anyway) -- must be ignored, not honored.
		Mode:        async.SendModeWalletPayout,
		GasLimit:    100_000,
		LogicalTime: 5,
		Height:      5,
	})
	require.NoError(t, err)

	after := k.ExportGenesis()
	sourceAfter, found := findContract(after.State.Contracts, source.ContractAddressUser)
	require.True(t, found)
	require.Equal(t, uint64(1_000-funds), sourceAfter.Balance)

	destAfter, found := findContract(after.State.Contracts, destination.ContractAddressUser)
	require.True(t, found)
	require.Equal(t, funds, destAfter.Balance, "funds must land in the destination CONTRACT's ledger balance, not a wallet")

	foundOrdinaryReceipt := false
	foundPayoutReceipt := false
	for _, r := range after.State.Receipts {
		if r.ContractAddress != source.ContractAddressUser {
			continue
		}
		switch r.Operation {
		case "internal_message_wallet_payout":
			foundPayoutReceipt = true
		case "internal_message_delivered", "internal_message_executed":
			foundOrdinaryReceipt = true
		}
	}
	require.True(t, foundOrdinaryReceipt, "a real-contract destination must take the ordinary delivery path")
	require.False(t, foundPayoutReceipt, "a real-contract destination must never be treated as a wallet payout")
}
