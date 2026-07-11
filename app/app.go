package app

import (
	"fmt"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/gogoproto/proto"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/std"
	"github.com/cosmos/cosmos-sdk/version"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/cosmos/cosmos-sdk/x/tx/signing"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/app/keeperconfig"
)

// NewL1App returns a reference to an initialized L1App.
func NewL1App(
	logger log.Logger,
	db dbm.DB,
	loadLatest bool,
	appOpts servertypes.AppOptions,
	baseAppOptions ...func(*baseapp.BaseApp),
) *L1App {
	interfaceRegistry, _ := types.NewInterfaceRegistryWithOptions(types.InterfaceRegistryOptions{
		ProtoFiles:	proto.HybridResolver,
		SigningOptions: signing.Options{
			AddressCodec:		aetraaddress.Codec{},
			ValidatorAddressCodec:	aetraaddress.Codec{},
			CustomGetSigners:	keeperconfig.CustomGetSigners(),
		},
	})
	appCodec := codec.NewProtoCodec(interfaceRegistry)
	legacyAmino := codec.NewLegacyAmino()
	// NewTxConfigWithOptions, not the bare NewTxConfig(codec, signModes): the
	// 2-arg form builds its OWN independent signing.Context from scratch
	// (x/auth/tx/config.go's NewDefaultSigningOptions) rather than reusing
	// interfaceRegistry's SigningContext above -- so the CustomGetSigners set
	// on interfaceRegistry, a few lines up, would otherwise be silently
	// ignored by exactly the TxConfig baseapp uses below to decode every live
	// transaction (SetTxDecoder/SetTxEncoder, right after this).
	txConfig, err := authtx.NewTxConfigWithOptions(appCodec, authtx.ConfigOptions{
		EnabledSignModes: authtx.DefaultSignModes,
		SigningOptions: &signing.Options{
			AddressCodec:		aetraaddress.Codec{},
			ValidatorAddressCodec:	aetraaddress.Codec{},
			CustomGetSigners:	keeperconfig.CustomGetSigners(),
		},
	})
	if err != nil {
		panic(err)
	}

	if err := interfaceRegistry.SigningContext().Validate(); err != nil {
		panic(err)
	}

	std.RegisterLegacyAminoCodec(legacyAmino)
	std.RegisterInterfaces(interfaceRegistry)

	baseAppOptions = append(baseAppOptions, baseapp.SetOptimisticExecution())

	bApp := baseapp.NewBaseApp(appName, logger, db, txConfig.TxDecoder(), baseAppOptions...)
	bApp.SetVersion(version.Version)
	bApp.SetInterfaceRegistry(interfaceRegistry)
	bApp.SetTxEncoder(txConfig.TxEncoder())

	keys := newKVStoreKeys()

	if err := bApp.RegisterStreamingServices(appOpts, keys); err != nil {
		panic(err)
	}

	app := &L1App{
		BaseApp:		bApp,
		legacyAmino:		legacyAmino,
		appCodec:		appCodec,
		txConfig:		txConfig,
		interfaceRegistry:	interfaceRegistry,
		keys:			keys,
	}

	txConfig = app.initKeepers(appCodec, legacyAmino, logger, appOpts, keys)
	app.initModules(appCodec, legacyAmino, interfaceRegistry, txConfig)
	app.registerCriticalInvariantRoutes()
	if err := app.ValidateAetraCoreWiringGate(); err != nil {
		panic(err)
	}
	if err := ValidateReservedSystemModuleAccountWiring(BlockedAddresses()); err != nil {
		panic(err)
	}

	app.MountKVStores(keys)

	app.SetInitChainer(app.InitChainer)
	app.SetPreBlocker(app.PreBlocker)
	app.SetBeginBlocker(app.BeginBlocker)
	app.SetEndBlocker(app.EndBlocker)
	app.setAnteHandler(txConfig)

	app.setPostHandler()

	if loadLatest {
		if err := app.LoadLatestVersion(); err != nil {
			panic(fmt.Errorf("error loading last version: %w", err))
		}
	}

	return app
}
