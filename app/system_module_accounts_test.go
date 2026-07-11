package app

import (
	"bytes"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	appparams "github.com/sovereign-l1/l1/app/params"
	mintauthoritytypes "github.com/sovereign-l1/l1/x/mint-authority/types"
)

func TestAppBootsWithReservedSystemModuleAccounts(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)

	require.NoError(t, app.ValidateAetraCoreWiringGate())
	require.NoError(t, ValidateReservedSystemModuleAccountWiring(BlockedAddresses()))

	permissions := GetMaccPerms()
	for _, account := range ReservedSystemModuleAccounts() {
		require.Contains(t, permissions, account.ModuleAccountName, account.Name)

		addr, found, err := ReservedSystemModuleAccountAddress(account.ModuleAccountName)
		require.NoError(t, err)
		require.True(t, found, account.ModuleAccountName)

		storedAccount := app.AccountKeeper.GetAccount(ctx, addr)
		if storedAccount != nil {
			require.Nil(t, storedAccount.GetPubKey(), account.Name)
		}
	}
}

func TestReservedSystemModuleAccountAddressesMatchConstants(t *testing.T) {
	for _, account := range ReservedSystemModuleAccounts() {
		addr, found, err := ReservedSystemModuleAccountAddress(account.ModuleAccountName)
		require.NoError(t, err)
		require.True(t, found, account.ModuleAccountName)

		rawBytes, err := aetraaddress.Parse(account.Raw)
		require.NoError(t, err)
		require.Equal(t, rawBytes, []byte(addr), account.Name)

		catalogAddress, found := aetraaddress.SystemAddressByName(account.Name)
		require.True(t, found, account.Name)
		require.Equal(t, catalogAddress.Raw, account.Raw, account.Name)
		require.Equal(t, catalogAddress.UserFriendly, account.UserFriendly, account.Name)
	}

	mint, found := ReservedSystemModuleAccountByName("AETMint")
	require.True(t, found)
	require.Equal(t, mintauthoritytypes.DefaultMintAuthorityModuleAccount, mint.ModuleAccountName)

	burn, found := ReservedSystemModuleAccountByName("AETBurn")
	require.True(t, found)
	require.Equal(t, "burn", burn.ModuleAccountName)
}

func TestBankBlockedAddressesIncludeNonReceivableSystemAccounts(t *testing.T) {
	blocked := BlockedAddresses()

	for _, address := range aetraaddress.AllSystemAddresses() {
		bz, err := aetraaddress.Parse(address.Raw)
		require.NoError(t, err)

		key := sdk.AccAddress(bz).String()
		require.Equal(t, !address.CanReceiveUserFunds, blocked[key], address.Name)
	}

	for _, account := range ReservedSystemModuleAccounts() {
		bz, err := aetraaddress.Parse(account.Raw)
		require.NoError(t, err)

		require.Equal(t, !account.CanReceiveUserFunds, blocked[sdk.AccAddress(bz).String()], account.Name)
	}
}

func TestReservedSystemBankSendPolicy(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)
	sender := AddTestAddrsWithCoins(t, app, ctx, 1, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 100)))[0]
	msgServer := bankkeeper.NewMsgServerImpl(app.BankKeeper)

	mint, found := ReservedSystemModuleAccountByName("AETMint")
	require.True(t, found)
	mintAddr, found, err := ReservedSystemModuleAccountAddress(mint.ModuleAccountName)
	require.NoError(t, err)
	require.True(t, found)
	_, err = msgServer.Send(ctx, &banktypes.MsgSend{
		FromAddress:	sender.String(),
		ToAddress:	mint.Raw,
		Amount:		sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 1)),
	})
	require.ErrorContains(t, err, "not allowed to receive funds")

	burn, found := ReservedSystemModuleAccountByName("AETBurn")
	require.True(t, found)
	_, err = msgServer.Send(ctx, &banktypes.MsgSend{
		FromAddress:	sender.String(),
		ToAddress:	burn.Raw,
		Amount:		sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 2)),
	})
	require.NoError(t, err)

	burnAddr, found, err := ReservedSystemModuleAccountAddress(burn.ModuleAccountName)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, sdk.NewInt64Coin(appparams.BaseDenom, 2), app.BankKeeper.GetBalance(ctx, burnAddr, appparams.BaseDenom))

	require.True(t, app.BankKeeper.BlockedAddr(mintAddr))
	require.False(t, app.BankKeeper.BlockedAddr(burnAddr))
}

// TestReservedSystemModuleAccountsBackedByLiveModuleAccounts verifies — against a
// booted app, not the catalog comparing fields to itself — that every reserved
// system entity claiming a module account is genuinely backed by a live
// cosmos-sdk module account. This is the non-circular counterpart to
// TestReservedSystemModuleAccountAddressesMatchConstants (which only re-parses
// catalog.Raw and compares it to catalog.Raw). It is the invariant that actually
// catches a regression in the module-account wiring: a fund-relevant entity whose
// backing module account disappears, loses a permission, drifts to a different
// address, or stops being bank-blocked.
//
// It also locks in the chain's deliberate two-layer address model: the reserved
// catalog ("vanity") address is a distinct, bank-reserved public identifier, while
// funds are custodied by a separate cosmos module account whose address the account
// keeper derives with authtypes.NewModuleAddress. The two must never silently
// collapse into one address (they cannot by construction — a hand-picked vanity
// pattern vs. a hash — and this test guards that they never do).
func TestReservedSystemModuleAccountsBackedByLiveModuleAccounts(t *testing.T) {
	app := Setup(t, false)
	ctx := app.NewContext(false)

	for _, account := range ReservedSystemModuleAccounts() {
		// 1. A live, deterministic module-account address exists for the claimed module.
		realAddr := app.AccountKeeper.GetModuleAddress(account.ModuleAccountName)
		require.NotNil(t, realAddr, account.Name)

		// 2. The stored account is a real module account with the declared name + permissions,
		//    stored exactly at that deterministic address.
		macc := app.AccountKeeper.GetModuleAccount(ctx, account.ModuleAccountName)
		require.NotNil(t, macc, account.Name)
		require.Equal(t, account.ModuleAccountName, macc.GetName(), account.Name)
		require.ElementsMatch(t, account.Permissions, macc.GetPermissions(), account.Name)
		require.Equal(t, realAddr.Bytes(), macc.GetAddress().Bytes(), account.Name)

		// 3. The reserved catalog (vanity) address is a DISTINCT reserved identifier from
		//    the cosmos module account that custodies funds (two-layer model).
		vanityBz, err := aetraaddress.Parse(account.Raw)
		require.NoError(t, err, account.Name)
		require.False(t, bytes.Equal(vanityBz, realAddr.Bytes()),
			"%s: reserved catalog address must stay distinct from its cosmos module account", account.Name)

		// 4. The reserved catalog address is bank-blocked for user receives iff the entity
		//    is not allowed to receive user funds.
		require.Equal(t, !account.CanReceiveUserFunds,
			app.BankKeeper.BlockedAddr(sdk.AccAddress(vanityBz)), account.Name)
	}

	// The economically load-bearing permissions must be present on the live accounts.
	mint, found := ReservedSystemModuleAccountByName("AETMint")
	require.True(t, found)
	require.Contains(t, app.AccountKeeper.GetModuleAccount(ctx, mint.ModuleAccountName).GetPermissions(), authtypes.Minter)

	burn, found := ReservedSystemModuleAccountByName("AETBurn")
	require.True(t, found)
	require.Contains(t, app.AccountKeeper.GetModuleAccount(ctx, burn.ModuleAccountName).GetPermissions(), authtypes.Burner)

	feeCollector, found := ReservedSystemModuleAccountByName("AETFeeCollector")
	require.True(t, found)
	require.Contains(t, app.AccountKeeper.GetModuleAccount(ctx, feeCollector.ModuleAccountName).GetPermissions(), authtypes.Burner)
}
