package txhandlers

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	"github.com/cosmos/cosmos-sdk/x/auth/posthandler"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	feegrantkeeper "github.com/cosmos/cosmos-sdk/x/feegrant/keeper"
)

// wrapAnteDecorator adapts the "func(next sdk.AnteHandler) sdk.AnteHandler"
// wrapping style used by the L1-specific ante decorators (StorageRentDecorator,
// RejectDirectUserStakingDecorator, the fees keeper's AnteHandlerDecorator) to
// the sdk.AnteDecorator interface, so they can be spliced into a single flat
// sdk.ChainAnteDecorators chain alongside the SDK's own decorators instead of
// wrapping the SDK's already-assembled handler from the outside.
type wrapAnteDecorator func(next sdk.AnteHandler) sdk.AnteHandler

func (w wrapAnteDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	return w(next)(ctx, tx, simulate)
}

// NewAnteHandler assembles the L1 ante decorator chain as a single flat list.
//
// ante.NewSetUpContextDecorator() MUST run first (CONTRACT enforced by the
// SDK, see x/auth/ante/setup.go): it replaces baseapp's infinite gas meter
// with the tx's real bounded gas meter and installs OutOfGas panic recovery,
// before any other decorator does any work. storageRentDecorator,
// RejectDirectUserStakingDecorator and feesDecorator are therefore spliced in
// immediately after SetUpContext -- so they still run before signature
// verification and fee deduction as intended -- rather than outside the SDK
// chain, where they would previously execute on the unmetered infinite gas
// meter and before any signature check.
func NewAnteHandler(
	txConfig client.TxConfig,
	accountKeeper authkeeper.AccountKeeper,
	bankKeeper bankkeeper.BaseKeeper,
	feeGrantKeeper feegrantkeeper.Keeper,
	nativeAccountKeeper NativeAccountStatusReader,
	feesDecorator func(sdk.AnteHandler) sdk.AnteHandler,
) sdk.AnteHandler {
	signModeHandler := txConfig.SignModeHandler()
	if signModeHandler == nil {
		panic("sign mode handler is required for ante builder")
	}

	decorators := []sdk.AnteDecorator{
		ante.NewSetUpContextDecorator(), // outermost decorator: SetUpContext must be called first
		wrapAnteDecorator(func(next sdk.AnteHandler) sdk.AnteHandler {
			return StorageRentDecorator(nativeAccountKeeper, next)
		}),
		wrapAnteDecorator(RejectDirectUserStakingDecorator),
		wrapAnteDecorator(feesDecorator),
		ante.NewExtensionOptionsDecorator(nil),
		ante.NewValidateBasicDecorator(),
		ante.NewTxTimeoutHeightDecorator(),
		ante.NewValidateMemoDecorator(accountKeeper),
		ante.NewConsumeGasForTxSizeDecorator(accountKeeper),
		ante.NewDeductFeeDecorator(accountKeeper, bankKeeper, feeGrantKeeper, nil),
		ante.NewSetPubKeyDecorator(accountKeeper), // SetPubKeyDecorator must be called before all signature verification decorators
		ante.NewValidateSigCountDecorator(accountKeeper),
		ante.NewSigGasConsumeDecorator(accountKeeper, ante.DefaultSigVerificationGasConsumer),
		ante.NewSigVerificationDecorator(
			accountKeeper,
			signModeHandler,
			ante.WithUnorderedTxGasCost(ante.DefaultUnorderedTxGasCost),
			ante.WithMaxUnorderedTxTimeoutDuration(ante.DefaultMaxTimeoutDuration),
		),
		ante.NewIncrementSequenceDecorator(accountKeeper),
	}

	return sdk.ChainAnteDecorators(decorators...)
}

func NewPostHandler() sdk.PostHandler {
	postHandler, err := posthandler.NewPostHandler(posthandler.HandlerOptions{})
	if err != nil {
		panic(err)
	}
	return postHandler
}
