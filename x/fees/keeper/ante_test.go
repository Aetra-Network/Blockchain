package keeper_test

import (
	"errors"
	"testing"

	sdkmath "cosmossdk.io/math"
	protov2 "google.golang.org/protobuf/proto"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sigtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	l1app "github.com/sovereign-l1/l1/app"
	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	l1testutil "github.com/sovereign-l1/l1/tests/testutil"
	"github.com/sovereign-l1/l1/x/fees/types"
)

type feeTx struct {
	fees	sdk.Coins
	payer	sdk.AccAddress
	msgs	[]sdk.Msg
	gas	uint64	// 0 → defaults to 100_000 for backward compat
}

func (tx feeTx) GetMsgs() []sdk.Msg {
	return tx.msgs
}

func (tx feeTx) GetMsgsV2() ([]protov2.Message, error) {
	return nil, nil
}

func (tx feeTx) GetGas() uint64 {
	if tx.gas == 0 {
		return 100_000
	}
	return tx.gas
}

func (tx feeTx) GetFee() sdk.Coins {
	return tx.fees
}

func (tx feeTx) FeePayer() []byte {
	return tx.payer
}

func (tx feeTx) FeeGranter() []byte {
	return nil
}

type sigFeeTx struct {
	feeTx
	signers	[][]byte
}

func (tx sigFeeTx) GetSigners() ([][]byte, error) {
	return tx.signers, nil
}

func (tx sigFeeTx) GetPubKeys() ([]cryptotypes.PubKey, error) {
	return nil, nil
}

func (tx sigFeeTx) GetSignaturesV2() ([]sigtypes.SignatureV2, error) {
	return nil, nil
}

type noFeeTx struct{}

func (tx noFeeTx) GetMsgs() []sdk.Msg {
	return nil
}

func (tx noFeeTx) GetMsgsV2() ([]protov2.Message, error) {
	return nil, nil
}

func validRawAddress(fill byte) string {
	return aetraaddress.FormatAccAddress(sdk.AccAddress{
		fill, fill, fill, fill, fill,
		fill, fill, fill, fill, fill,
		fill, fill, fill, fill, fill,
		fill, fill, fill, fill, fill,
	})
}

func reservedAddress(t *testing.T, name string) aetraaddress.SystemAddress {
	t.Helper()
	address, found := aetraaddress.SystemAddressByName(name)
	require.True(t, found)
	return address
}

func reservedBytes(t *testing.T, name string) []byte {
	t.Helper()
	address := reservedAddress(t, name)
	bz, err := aetraaddress.Parse(address.Raw)
	require.NoError(t, err)
	return bz
}

// requiredFullFee mirrors keeper.AdmitTx: the deterministic full-formula fee
// for a tx under default params with no tx bytes, no congestion, unknown
// reputation, and no storage rent side effects.
func requiredFullFee(t *testing.T, gas, msgCount uint64) sdkmath.Int {
	t.Helper()
	required, err := types.ComputeFullTransferFee(
		types.DefaultParams(),
		types.DefaultFeeFormulaParams(),
		gas,
		0,
		msgCount,
		0,
		types.ReputationNeutralScore,
		false,
		sdkmath.ZeroInt(),
	)
	require.NoError(t, err)
	return required
}

func TestAnteHandlerDecoratorFeePolicy(t *testing.T) {
	validSender := validRawAddress(1)
	validRecipient := validRawAddress(2)
	burn := reservedAddress(t, "AETBurn")

	fee := sdk.NewCoins(sdk.NewInt64Coin(types.BondDenom, 1))

	// Covers the full-formula requirement for every accepted tx here
	// (default 100k gas, at most one message).
	sufficientFee := sdk.NewCoins(sdk.NewCoin(types.BondDenom, requiredFullFee(t, 100_000, 1)))
	require.True(t, burn.CanReceiveUserFunds)

	tests := []struct {
		name		string
		tx		sdk.Tx
		wantErr		string
		wantNextCall	bool
		maxFeeAmount	string	// if non-empty, overrides MaxFeeAmount param before running
	}{
		{
			name:		"accepts native fee denom",
			tx:		feeTx{fees: sufficientFee},
			wantNextCall:	true,
			maxFeeAmount:	"1000000000000000000",
		},
		{
			name:		"rejects zero fee payer",
			tx:		feeTx{fees: fee, payer: sdk.AccAddress(make([]byte, 20))},
			wantErr:	"fee payer must not be zero address",
		},
		{
			name:		"rejects zero signer",
			tx:		sigFeeTx{feeTx: feeTx{fees: fee}, signers: [][]byte{make([]byte, 20)}},
			wantErr:	"signer 0 must not be zero address",
		},
		{
			name:		"rejects reserved core signer",
			tx:		sigFeeTx{feeTx: feeTx{fees: fee}, signers: [][]byte{reservedBytes(t, "AETElector")}},
			wantErr:	"signer 0 is reserved system address AETElector",
		},
		{
			name:		"rejects reserved module signer",
			tx:		sigFeeTx{feeTx: feeTx{fees: fee}, signers: [][]byte{reservedBytes(t, "AETMint")}},
			wantErr:	"signer 0 is reserved system address AETMint",
		},
		{
			name:		"rejects reserved fee payer",
			tx:		feeTx{fees: fee, payer: reservedBytes(t, "AETTreasury")},
			wantErr:	"fee payer is reserved system address AETTreasury",
		},
		{
			name:	"rejects zero bank send recipient",
			tx: feeTx{
				fees:	fee,
				msgs: []sdk.Msg{&banktypes.MsgSend{
					FromAddress:	validSender,
					ToAddress:	aetraaddress.ZeroUserFriendly,
					Amount:		fee,
				}},
			},
			wantErr:	"bank send recipient must not be zero address",
		},
		{
			name:	"rejects bank send to mint system address",
			tx: feeTx{
				fees:	fee,
				msgs: []sdk.Msg{&banktypes.MsgSend{
					FromAddress:	validSender,
					ToAddress:	reservedAddress(t, "AETMint").UserFriendly,
					Amount:		fee,
				}},
			},
			wantErr:	"bank send recipient is reserved system address AETMint and cannot receive user funds",
		},
		{
			name:	"rejects bank send to core system address",
			tx: feeTx{
				fees:	fee,
				msgs: []sdk.Msg{&banktypes.MsgSend{
					FromAddress:	validSender,
					ToAddress:	reservedAddress(t, "AETConfig").UserFriendly,
					Amount:		fee,
				}},
			},
			wantErr:	"bank send recipient is reserved system address AETConfig and cannot receive user funds",
		},
		{
			name:	"rejects direct user funds to treasury",
			tx: feeTx{
				fees:	fee,
				msgs: []sdk.Msg{&banktypes.MsgSend{
					FromAddress:	validSender,
					ToAddress:	reservedAddress(t, "AETTreasury").UserFriendly,
					Amount:		fee,
				}},
			},
			wantErr:	"bank send recipient is reserved system address AETTreasury and cannot receive user funds",
		},
		{
			name:	"rejects direct user funds to fee collector",
			tx: feeTx{
				fees:	fee,
				msgs: []sdk.Msg{&banktypes.MsgSend{
					FromAddress:	validSender,
					ToAddress:	reservedAddress(t, "AETFeeCollector").UserFriendly,
					Amount:		fee,
				}},
			},
			wantErr:	"bank send recipient is reserved system address AETFeeCollector and cannot receive user funds",
		},
		{
			name:	"allows bank send to burn when policy permits",
			tx: feeTx{
				fees:	sufficientFee,
				msgs: []sdk.Msg{&banktypes.MsgSend{
					FromAddress:	validSender,
					ToAddress:	burn.UserFriendly,
					Amount:		fee,
				}},
			},
			wantNextCall:	true,
			maxFeeAmount:	"1000000000000000000",
		},
		{
			name:	"accepts bank send between user addresses",
			tx: feeTx{
				fees:	sufficientFee,
				msgs: []sdk.Msg{&banktypes.MsgSend{
					FromAddress:	validSender,
					ToAddress:	validRecipient,
					Amount:		fee,
				}},
			},
			wantNextCall:	true,
			maxFeeAmount:	"1000000000000000000",
		},
		{
			name:	"rejects zero bank multisend output",
			tx: feeTx{
				fees:	fee,
				msgs: []sdk.Msg{&banktypes.MsgMultiSend{
					Inputs:		[]banktypes.Input{{Address: validSender, Coins: fee}},
					Outputs:	[]banktypes.Output{{Address: aetraaddress.ZeroUserFriendly, Coins: fee}},
				}},
			},
			wantErr:	"output 0: bank multisend output must not be zero address",
		},
		{
			name:	"rejects zero distribution withdraw address",
			tx: feeTx{
				fees:	fee,
				msgs: []sdk.Msg{&distrtypes.MsgSetWithdrawAddress{
					DelegatorAddress:	validSender,
					WithdrawAddress:	aetraaddress.ZeroUserFriendly,
				}},
			},
			wantErr:	"distribution withdraw address must not be zero address",
		},
		{
			name:		"rejects empty fee list",
			tx:		feeTx{fees: sdk.Coins{}},
			wantErr:	"fee must be positive",
		},
		{
			name:		"rejects nil fee list",
			tx:		feeTx{},
			wantErr:	"fee must be positive",
		},
		{
			name:		"rejects zero native fee coin",
			tx:		feeTx{fees: sdk.Coins{sdk.NewInt64Coin(types.BondDenom, 0)}},
			wantErr:	"invalid fee coins",
		},
		{
			name:		"rejects non native fee denom",
			tx:		feeTx{fees: sdk.NewCoins(sdk.NewInt64Coin("uatom", 1))},
			wantErr:	"fee denom uatom not accepted; use naet",
		},
		{
			name:		"rejects mixed native and non native fee denoms",
			tx:		feeTx{fees: sdk.Coins{sdk.NewInt64Coin(types.BondDenom, 1), sdk.NewInt64Coin(l1testutil.TestAssetDenom, 1)}},
			wantErr:	"fee denom testtoken not accepted; use naet",
		},
		{
			name:		"rejects malformed fee coin",
			tx:		feeTx{fees: sdk.Coins{{Denom: "!", Amount: sdkmath.NewInt(1)}}},
			wantErr:	"invalid fee coins",
		},
		{
			name:	"rejects duplicate fee denom entries",
			tx: feeTx{fees: sdk.Coins{
				sdk.NewInt64Coin(types.BondDenom, 1),
				sdk.NewInt64Coin(types.BondDenom, 2),
			}},
			wantErr:	"invalid fee coins",
		},
		{
			name:		"rejects transaction without fee interface",
			tx:		noFeeTx{},
			wantErr:	"transaction must expose fees",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := l1app.Setup(t, false)
			ctx := app.NewContext(false)

			if tc.maxFeeAmount != "" {
				p := types.DefaultParams()
				p.MaxFeeAmount = tc.maxFeeAmount
				require.NoError(t, app.FeesKeeper.SetParams(ctx, p))
			}

			called := false
			next := func(ctx sdk.Context, tx sdk.Tx, _ bool) (sdk.Context, error) {
				called = true
				if feeTx, ok := tx.(sdk.FeeTx); ok && !feeTx.GetFee().Empty() {
					require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, feeTx.GetFee()))
					require.NoError(t, app.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, authtypes.FeeCollectorName, feeTx.GetFee()))
				}
				return ctx, nil
			}

			_, err := app.FeesKeeper.AnteHandlerDecorator(next)(ctx, tc.tx, false)
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, types.ErrInvalidFee)
				require.Contains(t, err.Error(), tc.wantErr)
			}
			require.Equal(t, tc.wantNextCall, called)
		})
	}
}

func TestAnteHandlerDecoratorPropagatesNextError(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false)
	nextErr := errors.New("next failed")

	p := types.DefaultParams()
	p.MaxFeeAmount = "1000000000000000000"
	require.NoError(t, app.FeesKeeper.SetParams(ctx, p))

	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nextErr
	}

	_, err := app.FeesKeeper.AnteHandlerDecorator(next)(ctx, feeTx{fees: sdk.NewCoins(sdk.NewCoin(types.BondDenom, requiredFullFee(t, 100_000, 0)))}, false)
	require.ErrorIs(t, err, nextErr)
}

func TestAnteHandlerDecoratorRejectsAddressPolicyBeforeFeesAndNext(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(1)

	called := false
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		called = true
		return ctx, nil
	}

	_, err := app.FeesKeeper.AnteHandlerDecorator(next)(ctx, feeTx{
		fees:	sdk.Coins{},
		msgs: []sdk.Msg{&banktypes.MsgSend{
			FromAddress:	validRawAddress(1),
			ToAddress:	aetraaddress.ZeroUserFriendly,
			Amount:		sdk.NewCoins(sdk.NewInt64Coin(types.BondDenom, 1)),
		}},
	}, false)

	require.ErrorIs(t, err, types.ErrInvalidFee)
	require.Contains(t, err.Error(), "bank send recipient must not be zero address")
	require.False(t, called)
}

func TestAnteHandlerDecoratorRejectsZeroFee(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(1)

	called := false
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		called = true
		return ctx, nil
	}

	_, err := app.FeesKeeper.AnteHandlerDecorator(next)(ctx, feeTx{fees: sdk.Coins{}}, false)
	require.ErrorIs(t, err, types.ErrInvalidFee)
	require.False(t, called)
}

func TestAnteHandlerDecoratorAllowsGenesisCreateValidatorWithoutFee(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(0)

	called := false
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		called = true
		return ctx, nil
	}

	_, err := app.FeesKeeper.AnteHandlerDecorator(next)(ctx, feeTx{
		fees:	sdk.Coins{},
		msgs:	[]sdk.Msg{&stakingtypes.MsgCreateValidator{}},
	}, false)
	require.NoError(t, err)
	require.True(t, called)
}

func TestAnteHandlerDecoratorRejectsBelowMinimumFee(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(1)
	params := types.DefaultParams()
	params.MinFeeAmount = "100"
	params.BaseFeeAmount = "100"
	params.MaxFeeAmount = "1000"
	require.NoError(t, app.FeesKeeper.SetParams(ctx, params))

	called := false
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		called = true
		return ctx, nil
	}

	_, err := app.FeesKeeper.AnteHandlerDecorator(next)(ctx, feeTx{fees: sdk.NewCoins(sdk.NewInt64Coin(types.BondDenom, 99))}, false)
	require.ErrorIs(t, err, types.ErrInvalidFee)
	require.False(t, called)
}

func TestAnteHandlerDecoratorRejectsOverHardFeeCap(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(1)

	maxFee, err := types.DefaultParams().MaxFeeInt()
	require.NoError(t, err)

	called := false
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		called = true
		return ctx, nil
	}

	// One naet above the default 5 AET hard cap must be rejected.
	_, err = app.FeesKeeper.AnteHandlerDecorator(next)(ctx, feeTx{fees: sdk.NewCoins(sdk.NewCoin(types.BondDenom, maxFee.AddRaw(1))), gas: 1}, false)
	require.ErrorIs(t, err, types.ErrInvalidFee)
	require.Contains(t, err.Error(), "fee must not exceed hard cap")
	require.False(t, called)
}

func TestAnteHandlerDecoratorEnforcesSenderRateLimit(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(1)
	params := types.DefaultParams()
	params.MaxSenderTxsPerBlock = 2
	params.MaxSenderTxsPerBlockWithStake = 2

	params.MaxFeeAmount = "1000000000000000000"
	require.NoError(t, app.FeesKeeper.SetParams(ctx, params))

	payer := sdk.AccAddress{1, 2, 3}
	next := func(ctx sdk.Context, tx sdk.Tx, _ bool) (sdk.Context, error) {
		feeTx := tx.(sdk.FeeTx)
		require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, feeTx.GetFee()))
		require.NoError(t, app.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, authtypes.FeeCollectorName, feeTx.GetFee()))
		return ctx, nil
	}
	handler := app.FeesKeeper.AnteHandlerDecorator(next)

	tx := feeTx{fees: sdk.NewCoins(sdk.NewCoin(types.BondDenom, requiredFullFee(t, 100_000, 0))), payer: payer}

	_, err := handler(ctx, tx, false)
	require.NoError(t, err)
	_, err = handler(ctx, tx, false)
	require.NoError(t, err)
	_, err = handler(ctx, tx, false)
	require.ErrorIs(t, err, types.ErrInvalidFee)
	require.Contains(t, err.Error(), "sender rate limit")
}

func TestAnteHandlerDecoratorResetsRateLimitByBlockHeight(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(1)
	params := types.DefaultParams()
	params.MaxSenderTxsPerBlock = 1
	params.MaxSenderTxsPerBlockWithStake = 1

	params.MaxFeeAmount = "1000000000000000000"
	require.NoError(t, app.FeesKeeper.SetParams(ctx, params))

	payer := sdk.AccAddress{9, 9, 9}
	next := func(ctx sdk.Context, tx sdk.Tx, _ bool) (sdk.Context, error) {
		feeTx := tx.(sdk.FeeTx)
		require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, feeTx.GetFee()))
		require.NoError(t, app.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, authtypes.FeeCollectorName, feeTx.GetFee()))
		return ctx, nil
	}
	handler := app.FeesKeeper.AnteHandlerDecorator(next)

	tx := feeTx{fees: sdk.NewCoins(sdk.NewCoin(types.BondDenom, requiredFullFee(t, 100_000, 0))), payer: payer}

	_, err := handler(ctx, tx, false)
	require.NoError(t, err)
	_, err = handler(ctx, tx, false)
	require.ErrorIs(t, err, types.ErrInvalidFee)

	nextBlock := ctx.WithBlockHeight(2)
	_, err = handler(nextBlock, tx, false)
	require.NoError(t, err)
}

func TestAnteHandlerDecoratorRecordsFeesAfterDeduction(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(1)

	params := types.DefaultParams()
	params.MaxFeeAmount = "1000000000000000000"
	require.NoError(t, app.FeesKeeper.SetParams(ctx, params))

	payer := l1app.AddTestAddrsIncremental(app, ctx, 1, sdkmath.NewInt(1_000_000_000))[0]

	// 0.6 AET: comfortably above the ~0.425 AET full-formula requirement for a
	// message-less 100k-gas tx under the 0.5 AET transfer-fee policy.
	feeAmount := int64(600_000_000)
	fee := sdk.NewCoins(sdk.NewInt64Coin(types.BondDenom, feeAmount))
	feeCollector := app.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
	before := app.BankKeeper.GetBalance(ctx, feeCollector, types.BondDenom)

	next := func(ctx sdk.Context, tx sdk.Tx, _ bool) (sdk.Context, error) {
		feeTx := tx.(sdk.FeeTx)
		if err := app.BankKeeper.SendCoinsFromAccountToModule(ctx, sdk.AccAddress(feeTx.FeePayer()), authtypes.FeeCollectorName, feeTx.GetFee()); err != nil {
			return ctx, err
		}
		return ctx, nil
	}

	newCtx, err := app.FeesKeeper.AnteHandlerDecorator(next)(ctx, feeTx{fees: fee, payer: payer}, false)
	require.NoError(t, err)
	require.Equal(t, before, app.BankKeeper.GetBalance(newCtx, feeCollector, types.BondDenom))

	state, err := app.FeesKeeper.GetProtocolFeeState(newCtx)
	require.NoError(t, err)

	validatorExpected := sdk.NewCoins(sdk.NewInt64Coin(types.BondDenom, 588_000_000))	// 98%
	communityExpected := sdk.NewCoins(sdk.NewInt64Coin(types.BondDenom, 12_000_000))	// 2%
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin(types.BondDenom, feeAmount)), state.TotalCollected)
	require.Equal(t, validatorExpected, state.ValidatorRewards)
	require.Equal(t, communityExpected, state.CommunityPool)
	require.NoError(t, state.Validate())
}

// TestAnteHandlerDecoratorAcceptsLargeTxPayingExactlyHardCap is the
// end-to-end regression test for FINDING-011. Before the fix, AdmitTx
// required paidAmount >= requiredFull (the uncapped full-formula
// requirement) AND paidAmount <= maxFee; a large-but-envelope-legal tx could
// push requiredFull above maxFee, making those two conditions mutually
// exclusive so the tx was rejected no matter what fee was attached -- even
// the maximum legal fee. This converts the finding's PoC sketch into a
// positive assertion: the tx must now be admitted.
func TestAnteHandlerDecoratorAcceptsLargeTxPayingExactlyHardCap(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(1)

	maxFee, err := types.DefaultParams().MaxFeeInt()
	require.NoError(t, err)

	// ~60 KB tx body: comfortably under the 256 KB envelope limit, but large
	// enough that the uncapped full-formula requirement (byte component
	// alone: 100_000 naet/byte * 60_000 bytes = 6 AET) exceeds the default
	// 5 AET hard cap before the base/gas/message components are even added.
	ctx = ctx.WithTxBytes(make([]byte, 60_000))

	called := false
	next := func(ctx sdk.Context, tx sdk.Tx, _ bool) (sdk.Context, error) {
		called = true
		feeTx := tx.(sdk.FeeTx)
		require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, feeTx.GetFee()))
		require.NoError(t, app.BankKeeper.SendCoinsFromModuleToModule(ctx, minttypes.ModuleName, authtypes.FeeCollectorName, feeTx.GetFee()))
		return ctx, nil
	}

	tx := feeTx{fees: sdk.NewCoins(sdk.NewCoin(types.BondDenom, maxFee))}
	_, err = app.FeesKeeper.AnteHandlerDecorator(next)(ctx, tx, false)
	require.NoError(t, err, "a tx paying the maximum legal fee must never be rejected as unpayable")
	require.True(t, called)
}

// TestAnteHandlerDecoratorCountsNestedAuthzMessagesTowardEnvelopeCap is the
// end-to-end regression test for FINDING-013. Before the fix,
// validateTxEnvelope computed MsgCount as len(tx.GetMsgs()); a single
// top-level authz.MsgExec wrapping any number of inner messages counted as
// 1, sailing past MaxMessagesPerTx=16 regardless of how many messages it
// actually carried -- 100 flat top-level MsgSends would have been rejected
// at that same limit.
func TestAnteHandlerDecoratorCountsNestedAuthzMessagesTowardEnvelopeCap(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(1)

	validSender := validRawAddress(11)
	validRecipient := validRawAddress(12)
	grantee, err := aetraaddress.Parse(validRawAddress(13))
	require.NoError(t, err)

	const innerCount = 100
	inner := make([]sdk.Msg, innerCount)
	for i := range inner {
		inner[i] = &banktypes.MsgSend{
			FromAddress:	validSender,
			ToAddress:	validRecipient,
			Amount:		sdk.NewCoins(sdk.NewInt64Coin(types.BondDenom, 1)),
		}
	}
	execMsg := authz.NewMsgExec(grantee, inner)

	called := false
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		called = true
		return ctx, nil
	}

	tx := feeTx{
		fees:	sdk.NewCoins(sdk.NewInt64Coin(types.BondDenom, 1)),
		msgs:	[]sdk.Msg{&execMsg},
	}

	_, err = app.FeesKeeper.AnteHandlerDecorator(next)(ctx, tx, false)
	require.ErrorIs(t, err, types.ErrInvalidFee)
	// innerCount nested MsgSends + the MsgExec wrapper itself = 101, not 1.
	require.Contains(t, err.Error(), "message count 101 exceeds max_messages_per_tx 16")
	require.False(t, called)
}

// TestAnteHandlerDecoratorRejectsZeroAddressNestedInAuthzExec is the
// end-to-end regression test for FINDING-014. Before the fix,
// validateNoZeroTxAddresses (via validateNoZeroMsgAddresses) only inspected
// tx.GetMsgs() directly; a MsgSend to the zero address wrapped inside an
// authz.MsgExec never matched the type switch (MsgExec itself isn't a
// MsgSend/MsgMultiSend/MsgSetWithdrawAddress) and was never unwrapped, so
// the same send that is rejected at the top level sailed through when
// nested.
func TestAnteHandlerDecoratorRejectsZeroAddressNestedInAuthzExec(t *testing.T) {
	app := l1app.Setup(t, false)
	ctx := app.NewContext(false).WithBlockHeight(1)

	validSender := validRawAddress(21)
	grantee, err := aetraaddress.Parse(validRawAddress(22))
	require.NoError(t, err)

	inner := &banktypes.MsgSend{
		FromAddress:	validSender,
		ToAddress:	aetraaddress.ZeroUserFriendly,
		Amount:		sdk.NewCoins(sdk.NewInt64Coin(types.BondDenom, 1)),
	}
	execMsg := authz.NewMsgExec(grantee, []sdk.Msg{inner})

	called := false
	next := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		called = true
		return ctx, nil
	}

	tx := feeTx{
		fees:	sdk.NewCoins(sdk.NewInt64Coin(types.BondDenom, 1)),
		msgs:	[]sdk.Msg{&execMsg},
	}

	_, err = app.FeesKeeper.AnteHandlerDecorator(next)(ctx, tx, false)
	require.ErrorIs(t, err, types.ErrInvalidFee)
	require.Contains(t, err.Error(), "bank send recipient must not be zero address")
	require.False(t, called)
}
