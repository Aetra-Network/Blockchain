package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/spf13/cobra"

	"cosmossdk.io/log/v2"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client/flags"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	l1app "github.com/sovereign-l1/l1/app"
	"github.com/sovereign-l1/l1/app/accounts"
	appparams "github.com/sovereign-l1/l1/app/params"
)

type invariantAppOptions map[string]interface{}

func (o invariantAppOptions) Get(key string) interface{} {
	return o[key]
}

var _ servertypes.AppOptions = invariantAppOptions{}

type invariantCheckReport struct {
	Command		string				`json:"command"`
	Mode		string				`json:"mode"`
	Passed		bool				`json:"passed"`
	Routes		[]string			`json:"routes"`
	Failures	[]l1app.AppInvariantFailure	`json:"failures,omitempty"`
}

func NewInvariantsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"invariants",
		Short:	"Run critical Aetra app invariant checks",
	}
	cmd.AddCommand(newInvariantListCmd(), newInvariantCheckCmd())
	return cmd
}

func newInvariantListCmd() *cobra.Command {
	return &cobra.Command{
		Use:	"list",
		Short:	"List registered critical invariant routes",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeCommandJSON(cmd, struct {
				Command	string		`json:"command"`
				Routes	[]string	`json:"routes"`
			}{
				Command:	"invariants list",
				Routes:		l1app.CriticalAppInvariantRoutes(),
			})
		},
	}
}

func newInvariantCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:	"check",
		Short:	"Run critical invariants against deterministic default genesis",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := runDefaultGenesisInvariantCheck()
			if err != nil {
				return err
			}
			if writeErr := writeCommandJSON(cmd, report); writeErr != nil {
				return writeErr
			}
			if !report.Passed {
				return fmt.Errorf("critical invariants failed: %d", len(report.Failures))
			}
			return nil
		},
	}
}

func runDefaultGenesisInvariantCheck() (invariantCheckReport, error) {
	db := dbm.NewMemDB()
	appOpts := invariantAppOptions{flags.FlagHome: l1app.DefaultNodeHome}
	app := l1app.NewL1App(log.NewNopLogger(), db, true, appOpts)
	genesis, err := invariantDefaultGenesisWithValidator(app)
	if err != nil {
		return invariantCheckReport{}, err
	}
	stateBytes, err := cmtjson.MarshalIndent(genesis, "", " ")
	if err != nil {
		return invariantCheckReport{}, err
	}
	consensusParams := cmttypes.DefaultConsensusParams().ToProto()
	if _, err := app.InitChain(&abci.RequestInitChain{AppStateBytes: stateBytes, ConsensusParams: &consensusParams}); err != nil {
		return invariantCheckReport{}, err
	}
	// InitChain's writes land in the finalize-block state, not the committed
	// multistore, so they must be finalized and committed before the
	// invariants below can observe them. Without this the check ran against
	// an EMPTY committed store: most invariants passed vacuously, and
	// AppInvariantGenesisExport failed and was swept into "skipped".
	//
	// It went unnoticed because the blob-genesis keepers hold their whole
	// state in a k.genesis field in RAM and serve it regardless of what the
	// store contains -- the exact F-17 masking documented at
	// x/contracts/keeper/keeper.go:2184-2196. x/aez keeps no state in RAM, so
	// it reported the empty store honestly instead of hiding it.
	if _, err := app.FinalizeBlock(&abci.RequestFinalizeBlock{Height: app.LastBlockHeight() + 1}); err != nil {
		return invariantCheckReport{}, err
	}
	if _, err := app.Commit(); err != nil {
		return invariantCheckReport{}, err
	}
	// NewUncachedContext, not NewContext: Commit clears the finalize-block
	// state that NewContext branches from, and reading committed state is
	// the whole point of committing above.
	ctx := app.NewUncachedContext(false, cmtproto.Header{Height: app.LastBlockHeight()})
	failures := app.RunCriticalInvariants(ctx)
	report := invariantCheckReport{
		Command:	"invariants check",
		Mode:		"default-genesis",
		Passed:		len(failures) == 0,
		Routes:		app.CriticalInvariantRoutes(),
		Failures:	failures,
	}
	if _, err := json.Marshal(report); err != nil {
		return invariantCheckReport{}, err
	}
	return report, nil
}

func invariantDefaultGenesisWithValidator(app *l1app.L1App) (l1app.GenesisState, error) {
	genesis := app.DefaultGenesis()
	priv := &secp256k1.PrivKey{Key: bytes.Repeat([]byte{0x42}, 32)}
	pub := priv.PubKey()
	addr := sdk.AccAddress(pub.Address())
	account := authtypes.NewBaseAccount(addr, pub, 0, 0)
	genAccounts := []authtypes.GenesisAccount{account}
	for moduleName, permissions := range accounts.ModuleAccountPermissions() {
		genAccounts = append(genAccounts, authtypes.NewEmptyModuleAccount(moduleName, permissions...))
	}
	authGenesis := authtypes.NewGenesisState(authtypes.DefaultParams(), genAccounts)
	genesis[authtypes.ModuleName] = app.AppCodec().MustMarshalJSON(authGenesis)

	pubAny, err := codectypes.NewAnyWithValue(pub)
	if err != nil {
		return nil, err
	}
	bondAmt := sdk.TokensFromConsensusPower(1, sdk.DefaultPowerReduction)
	valAddr := sdk.ValAddress(addr)
	validator := stakingtypes.Validator{
		OperatorAddress:	valAddr.String(),
		ConsensusPubkey:	pubAny,
		Jailed:			false,
		Status:			stakingtypes.Bonded,
		Tokens:			bondAmt,
		DelegatorShares:	sdkmath.LegacyOneDec(),
		Description:		stakingtypes.Description{},
		UnbondingHeight:	0,
		UnbondingTime:		time.Unix(1, 0).UTC(),
		Commission:		stakingtypes.NewCommission(sdkmath.LegacyZeroDec(), sdkmath.LegacyZeroDec(), sdkmath.LegacyZeroDec()),
		MinSelfDelegation:	sdkmath.OneInt(),
	}
	stakingGenesis := stakingtypes.NewGenesisState(stakingtypes.DefaultParams(), []stakingtypes.Validator{validator}, []stakingtypes.Delegation{
		stakingtypes.NewDelegation(addr.String(), valAddr.String(), sdkmath.LegacyOneDec()),
	})
	stakingGenesis.Params.BondDenom = appparams.BaseDenom
	genesis[stakingtypes.ModuleName] = app.AppCodec().MustMarshalJSON(stakingGenesis)

	accountBalance := sdk.NewCoins(sdk.NewCoin(appparams.BaseDenom, bondAmt))
	bondedBalance := sdk.NewCoins(sdk.NewCoin(appparams.BaseDenom, bondAmt))
	totalSupply := accountBalance.Add(bondedBalance...)
	bankGenesis := banktypes.DefaultGenesisState()
	bankGenesis.Balances = []banktypes.Balance{
		{Address: addr.String(), Coins: accountBalance},
		{Address: authtypes.NewModuleAddress(stakingtypes.BondedPoolName).String(), Coins: bondedBalance},
	}
	bankGenesis.Supply = totalSupply
	bankGenesis.DenomMetadata = []banktypes.Metadata{appparams.NativeTokenMetadata()}
	genesis[banktypes.ModuleName] = app.AppCodec().MustMarshalJSON(bankGenesis)
	return genesis, nil
}
