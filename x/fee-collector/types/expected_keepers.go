package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type AccountKeeper interface {
	GetModuleAddress(moduleName string) sdk.AccAddress
}

type BankKeeper interface {
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	// GetSupply is required by the fee burn cap: the cap is expressed as a
	// fraction of total supply per year, so the burn has to become supply-aware.
	// A pure bps split of fees cannot be bounded relative to supply, which is the
	// mechanism gap that let burn reach 74% of supply/yr at 7.63 TPS.
	GetSupply(ctx context.Context, denom string) sdk.Coin
}
