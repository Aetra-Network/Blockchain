package app

import (
	"github.com/cosmos/cosmos-sdk/client"

	"github.com/sovereign-l1/l1/app/txhandlers"
)

func (app *L1App) setAnteHandler(txConfig client.TxConfig) {
	// NewAnteHandler assembles a single flat decorator chain with SetUpContext
	// first, followed by StorageRentDecorator, RejectDirectUserStakingDecorator
	// and FeesKeeper.AnteHandlerDecorator (in that relative order), followed by
	// the rest of the standard SDK chain. See app/txhandlers/handlers.go for
	// why these three must run after SetUpContext rather than wrapping the SDK
	// ante handler from the outside.
	anteHandler := txhandlers.NewAnteHandler(
		txConfig,
		app.AccountKeeper,
		app.BankKeeper,
		app.FeeGrantKeeper,
		app.NativeAccountKeeper,
		app.FeesKeeper.AnteHandlerDecorator,
	)
	app.SetAnteHandler(anteHandler)
}

func (app *L1App) setPostHandler() {
	app.SetPostHandler(txhandlers.NewPostHandler())
}
