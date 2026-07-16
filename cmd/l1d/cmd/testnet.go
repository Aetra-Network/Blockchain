package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	cmtconfig "github.com/cometbft/cometbft/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"cosmossdk.io/math"
	"cosmossdk.io/math/unsafe"

	mathrand "math/rand"
	l1app "github.com/sovereign-l1/l1/app"
	appparams "github.com/sovereign-l1/l1/app/params"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/server"
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/testutil"
	"github.com/cosmos/cosmos-sdk/testutil/network"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/version"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	nativeaccounttypes "github.com/sovereign-l1/l1/x/native-account/types"
)

// bootstrapNativeAccount builds an already-active x/native-account record
// for a genesis bootstrap key, using the SAME legacy-padded address the
// account already uses for its funded bank/staking identity (via
// aetraaddress.Format/FormatUserFriendly on the raw pubkey hash) rather than
// the domain-hashed v2 address MsgActivateAccount would derive for a brand
// new account. See the call site in newTestnetCmd for why this matters.
func bootstrapNativeAccount(pubKey cryptotypes.PubKey, accountNumber uint64) nativeaccounttypes.Account {
	rawBytes := pubKey.Address()
	addressUser, err := aetraaddress.FormatUserFriendly(rawBytes)
	if err != nil {
		panic(fmt.Errorf("format bootstrap native account address: %w", err))
	}
	addressRaw := aetraaddress.Format(rawBytes)
	return nativeaccounttypes.Account{
		Version:		nativeaccounttypes.CurrentAccountVersion,
		AddressUser:		addressUser,
		AddressRaw:		addressRaw,
		PubKeys:		[]string{nativeaccounttypes.PublicKeyText(pubKey)},
		AccountNumber:		accountNumber,
		Status:			nativeaccounttypes.AccountStatusActive,
		AuthPolicy:		nativeaccounttypes.AuthPolicy{Version: 1, Mode: nativeaccounttypes.AuthModeSingleKey},
		CreatedHeight:		1,
		LastActiveHeight:	1,
		LastStorageChargeHeight: 1,
	}
}

var (
	flagNodeDirPrefix	= "node-dir-prefix"
	flagNumValidators	= "validator-count"
	flagOutputDir		= "output-dir"
	flagNodeDaemonHome	= "node-daemon-home"
	flagStartingIPAddress	= "starting-ip-address"
	flagListenIPAddress	= "listen-ip-address"
	flagEnableLogging	= "enable-logging"
	flagGRPCAddress		= "grpc.address"
	flagRPCAddress		= "rpc.address"
	flagAPIAddress		= "api.address"
	flagPrintMnemonic	= "print-mnemonic"
	flagStakingDenom	= "staking-denom"
	flagCommitTimeout	= "commit-timeout"
	flagSingleHost		= "single-host"

	// Genesis sizing. The defaults below reproduce the historical hardcoded
	// figures exactly (1,000 AET funded, 100 AET self-bonded, no extra
	// accounts), so an unflagged init-files is byte-identical to before.
	//
	// They exist because those figures are far too small to test economics on:
	// a 4-validator net genesis-mints 4,000 AET total, and at the 0.5 AET
	// average fee with a 50% burn that supply measurably collapses under load
	// (third audit 2.2 -- 1.48% of all money burned in 22 minutes). The audit's
	// conclusion was that genesis supply has to be 10^4-10^6 times larger to
	// measure inflation against burn honestly.
	flagValidatorStakeMinAet	= "validator-stake-aet-min"
	flagValidatorStakeMaxAet	= "validator-stake-aet-max"
	flagValidatorExtraAet		= "validator-extra-aet"
	flagExtraAccounts		= "extra-accounts"
	flagExtraAccountMinAet		= "extra-account-aet-min"
	flagExtraAccountMaxAet		= "extra-account-aet-max"
	flagSimSeed			= "sim-seed"
	flagEnableContracts		= "enable-contracts"
	flagPoolAuthority		= "nominator-pool-authority"
	flagUnbondingTime		= "unbonding-time"
)

// naetPerAet is the base-unit scale: 1 AET = 1e9 naet (app/params/token.go).
const naetPerAet = int64(1_000_000_000)

// aetToNaet converts a whole-AET figure to base units.
func aetToNaet(aet int64) math.Int {
	return math.NewInt(aet).MulRaw(naetPerAet)
}

// pickAet returns a deterministic pseudo-random whole-AET amount in
// [minAet, maxAet]. maxAet <= minAet pins it to minAet, which is what keeps the
// default flag values reproducing the old fixed behaviour.
func pickAet(rng *mathrand.Rand, minAet, maxAet int64) int64 {
	if maxAet <= minAet {
		return minAet
	}
	return minAet + rng.Int63n(maxAet-minAet+1)
}

type initArgs struct {
	algo			string
	chainID			string
	keyringBackend		string
	minGasPrices		string
	nodeDaemonHome		string
	nodeDirPrefix		string
	numValidators		int
	outputDir		string
	startingIPAddress	string
	listenIPAddress		string
	singleMachine		bool
	bondTokenDenom		string

	valStakeMinAet		int64
	valStakeMaxAet		int64
	valExtraAet		int64
	extraAccounts		int
	extraAccountMinAet	int64
	extraAccountMaxAet	int64
	simSeed			int64
	enableContracts		bool
	poolAuthority		string
	unbondingTime		time.Duration
}

type startArgs struct {
	algo		string
	apiAddress	string
	chainID		string
	enableLogging	bool
	grpcAddress	string
	minGasPrices	string
	numValidators	int
	outputDir	string
	printMnemonic	bool
	rpcAddress	string
	timeoutCommit	time.Duration
}

func addTestnetFlagsToCmd(cmd *cobra.Command) {
	cmd.Flags().IntP(flagNumValidators, "v", 4, "Number of validators to initialize the testnet with")
	cmd.Flags().StringP(flagOutputDir, "o", "./.testnets", "Directory to store initialization data for the testnet")
	cmd.Flags().String(flags.FlagChainID, "", "genesis file chain-id, if left blank will be randomly created")
	cmd.Flags().String(
		server.FlagMinGasPrices,
		fmt.Sprintf("0.000006%s", sdk.DefaultBondDenom),
		fmt.Sprintf("Minimum gas prices to accept for transactions; all prototype fees should use %s (e.g. 0.000006%s)", appparams.BaseDenom, appparams.BaseDenom),
	)
	cmd.Flags().String(flags.FlagKeyType, string(hd.Secp256k1Type), "Key signing algorithm to generate keys for")

	cmd.Flags().SetNormalizeFunc(func(f *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == flags.FlagKeyAlgorithm {
			name = flags.FlagKeyType
		}

		return pflag.NormalizedName(name)
	})
}

// NewTestnetCmd creates a root testnet command with subcommands to run an in-process testnet or initialize
// validator configuration files for running a multi-validator testnet in a separate process
func NewTestnetCmd(mm module.BasicManager, genBalIterator banktypes.GenesisBalancesIterator) *cobra.Command {
	testnetCmd := &cobra.Command{
		Use:				"testnet",
		Short:				"subcommands for starting or configuring local testnets",
		DisableFlagParsing:		true,
		SuggestionsMinimumDistance:	2,
		RunE:				client.ValidateCmd,
	}

	testnetCmd.AddCommand(testnetStartCmd())
	testnetCmd.AddCommand(testnetInitFilesCmd(mm, genBalIterator))

	return testnetCmd
}

// NewInitLocalnetCmd initializes a runnable single-host localnet using Aetra defaults.
func NewInitLocalnetCmd(mm module.BasicManager, genBalIterator banktypes.GenesisBalancesIterator) *cobra.Command {
	cmd := &cobra.Command{
		Use:	"init-localnet",
		Short:	"Initialize a single-host Aetra localnet with naet genesis balances and gentxs",
		Long: fmt.Sprintf(`init-localnet is a convenience wrapper around testnet init-files.
It writes node directories, genesis accounts, gentxs, and collected genesis files
for a single-host localnet. Use --validator-count 1 for a one-node devnet or
--validator-count 4 for a local multi-validator devnet.

Example:
	%s init-localnet --validator-count 4 --output-dir ./.localnet --chain-id aetra-local-1
`, version.AppName),
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			serverCtx := server.GetServerContextFromCmd(cmd)
			config := serverCtx.Config

			args := initArgs{}
			args.outputDir, _ = cmd.Flags().GetString(flagOutputDir)
			args.keyringBackend, _ = cmd.Flags().GetString(flags.FlagKeyringBackend)
			args.chainID, _ = cmd.Flags().GetString(flags.FlagChainID)
			args.minGasPrices, _ = cmd.Flags().GetString(server.FlagMinGasPrices)
			args.nodeDirPrefix, _ = cmd.Flags().GetString(flagNodeDirPrefix)
			args.nodeDaemonHome, _ = cmd.Flags().GetString(flagNodeDaemonHome)
			args.startingIPAddress, _ = cmd.Flags().GetString(flagStartingIPAddress)
			args.listenIPAddress, _ = cmd.Flags().GetString(flagListenIPAddress)
			args.numValidators, _ = cmd.Flags().GetInt(flagNumValidators)
			args.algo, _ = cmd.Flags().GetString(flags.FlagKeyType)
			args.bondTokenDenom, _ = cmd.Flags().GetString(flagStakingDenom)
			args.singleMachine = true
			config.Consensus.TimeoutCommit, err = cmd.Flags().GetDuration(flagCommitTimeout)
			if err != nil {
				return err
			}
			if args.chainID != "" {
				if err := appparams.ValidateAetraTestnetChainID(args.chainID); err != nil {
					return err
				}
			}

			return initTestnetFiles(clientCtx, cmd, config, mm, genBalIterator, args)
		},
	}

	addTestnetFlagsToCmd(cmd)
	cmd.Flags().String(flagNodeDirPrefix, "node", "Prefix for per-validator directories")
	cmd.Flags().String(flagNodeDaemonHome, "aetrad", "Home directory under each node directory")
	cmd.Flags().String(flagStartingIPAddress, "127.0.0.1", "Starting IP address for generated peers")
	cmd.Flags().String(flagListenIPAddress, "0.0.0.0", "RPC listen IP")
	cmd.Flags().String(flags.FlagKeyringBackend, "test", "Keyring backend for generated localnet keys")
	cmd.Flags().Duration(flagCommitTimeout, time.Second, "Time to wait after a block commit")
	cmd.Flags().String(flagStakingDenom, appparams.BaseDenom, "Staking token denominator")
	cmd.Flags().Lookup(flagNumValidators).DefValue = "1"
	cmd.Flags().Lookup(server.FlagMinGasPrices).DefValue = "0" + appparams.BaseDenom
	_ = cmd.Flags().Set(flagNumValidators, "1")
	_ = cmd.Flags().Set(flagStakingDenom, appparams.BaseDenom)
	_ = cmd.Flags().Set(server.FlagMinGasPrices, "0"+appparams.BaseDenom)
	return cmd
}

// testnetInitFilesCmd returns a cmd to initialize all files for CometBFT testnet and application
func testnetInitFilesCmd(mm module.BasicManager, genBalIterator banktypes.GenesisBalancesIterator) *cobra.Command {
	cmd := &cobra.Command{
		Use:	"init-files",
		Short:	"Initialize config directories & files for a multi-validator testnet running locally via separate processes (e.g. Docker Compose or similar)",
		Long: fmt.Sprintf(`init-files will setup one directory per validator and populate each with
necessary files (private validator, genesis, config, etc.) for running validator nodes.

Booting up a network with these validator folders is intended to be used with Docker Compose,
or a similar setup where each node has a manually configurable IP address.

Note, strict routability for addresses is turned off in the config file.

Example:
	%s testnet init-files --validator-count 4 --output-dir ./.testnets --starting-ip-address 192.168.10.2
	`, version.AppName),
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			serverCtx := server.GetServerContextFromCmd(cmd)
			config := serverCtx.Config

			args := initArgs{}
			args.outputDir, _ = cmd.Flags().GetString(flagOutputDir)
			args.keyringBackend, _ = cmd.Flags().GetString(flags.FlagKeyringBackend)
			args.chainID, _ = cmd.Flags().GetString(flags.FlagChainID)
			args.minGasPrices, _ = cmd.Flags().GetString(server.FlagMinGasPrices)
			args.nodeDirPrefix, _ = cmd.Flags().GetString(flagNodeDirPrefix)
			args.nodeDaemonHome, _ = cmd.Flags().GetString(flagNodeDaemonHome)
			args.startingIPAddress, _ = cmd.Flags().GetString(flagStartingIPAddress)
			args.listenIPAddress, _ = cmd.Flags().GetString(flagListenIPAddress)
			args.numValidators, _ = cmd.Flags().GetInt(flagNumValidators)
			args.algo, _ = cmd.Flags().GetString(flags.FlagKeyType)
			args.bondTokenDenom, _ = cmd.Flags().GetString(flagStakingDenom)
			args.singleMachine, _ = cmd.Flags().GetBool(flagSingleHost)
			args.valStakeMinAet, _ = cmd.Flags().GetInt64(flagValidatorStakeMinAet)
			args.valStakeMaxAet, _ = cmd.Flags().GetInt64(flagValidatorStakeMaxAet)
			args.valExtraAet, _ = cmd.Flags().GetInt64(flagValidatorExtraAet)
			args.extraAccounts, _ = cmd.Flags().GetInt(flagExtraAccounts)
			args.extraAccountMinAet, _ = cmd.Flags().GetInt64(flagExtraAccountMinAet)
			args.extraAccountMaxAet, _ = cmd.Flags().GetInt64(flagExtraAccountMaxAet)
			args.simSeed, _ = cmd.Flags().GetInt64(flagSimSeed)
			args.enableContracts, _ = cmd.Flags().GetBool(flagEnableContracts)
			args.poolAuthority, _ = cmd.Flags().GetString(flagPoolAuthority)
			args.unbondingTime, err = cmd.Flags().GetDuration(flagUnbondingTime)
			if err != nil {
				return err
			}
			if args.valStakeMinAet <= 0 {
				return fmt.Errorf("--%s must be positive", flagValidatorStakeMinAet)
			}
			if args.valExtraAet < 0 {
				return fmt.Errorf("--%s cannot be negative", flagValidatorExtraAet)
			}
			if args.extraAccounts < 0 {
				return fmt.Errorf("--%s cannot be negative", flagExtraAccounts)
			}
			if args.extraAccounts > 0 && args.extraAccountMinAet <= 0 {
				return fmt.Errorf("--%s must be positive", flagExtraAccountMinAet)
			}
			config.Consensus.TimeoutCommit, err = cmd.Flags().GetDuration(flagCommitTimeout)
			if err != nil {
				return err
			}
			if args.chainID != "" {
				if err := appparams.ValidateAetraTestnetChainID(args.chainID); err != nil {
					return err
				}
			}

			return initTestnetFiles(clientCtx, cmd, config, mm, genBalIterator, args)
		},
	}

	addTestnetFlagsToCmd(cmd)
	cmd.Flags().String(flagNodeDirPrefix, "node", "Prefix for the name of per-validator subdirectories (to be number-suffixed like node0, node1, ...)")
	cmd.Flags().String(flagNodeDaemonHome, "aetrad", "Home directory of the node's daemon configuration")
	cmd.Flags().String(flagStartingIPAddress, "192.168.0.1", "Starting IP address (192.168.0.1 results in persistent peers list ID0@192.168.0.1:46656, ID1@192.168.0.2:46656, ...)")
	cmd.Flags().String(flagListenIPAddress, "0.0.0.0", "TCP or UNIX socket IP address for the RPC server to listen on")
	cmd.Flags().String(flags.FlagKeyringBackend, flags.DefaultKeyringBackend, "Select keyring's backend (os|file|test)")
	cmd.Flags().Duration(flagCommitTimeout, 5*time.Second, "Time to wait after a block commit before starting on the new height")
	cmd.Flags().Bool(flagSingleHost, false, "Cluster runs on a single host machine with different ports")
	cmd.Flags().String(flagStakingDenom, sdk.DefaultBondDenom, "Default staking token denominator")
	cmd.Flags().Int64(flagValidatorStakeMinAet, 100, "Minimum whole-AET self-bond per validator")
	cmd.Flags().Int64(flagValidatorStakeMaxAet, 0, "Maximum whole-AET self-bond per validator; <= min pins every validator to min (uniform, the historical behaviour)")
	cmd.Flags().Int64(flagValidatorExtraAet, 900, "Whole AET funded to each validator on top of its self-bond, to pay fees")
	cmd.Flags().Int(flagExtraAccounts, 0, "Number of extra funded genesis wallets to create beyond the validators (keys land in <output-dir>/wallets)")
	cmd.Flags().Int64(flagExtraAccountMinAet, 20_000, "Minimum whole-AET balance per extra wallet")
	cmd.Flags().Int64(flagExtraAccountMaxAet, 220_000, "Maximum whole-AET balance per extra wallet; <= min pins every wallet to min")
	cmd.Flags().Int64(flagSimSeed, 1, "Seed for the stake/balance randomisation, so a given genesis is reproducible")
	cmd.Flags().Bool(flagEnableContracts, false, "Enable x/contracts (the AVM) in genesis; off by default because the public testnet ships it gated behind governance")
	cmd.Flags().String(flagPoolAuthority, "", "Address to install as x/nominator-pool Params.Authority; empty keeps the default all-zero address, which no key can sign for so no liquid-staking pool can ever be created")
	cmd.Flags().Duration(flagUnbondingTime, 0, "Override x/staking unbonding time; 0 keeps the 21-day default, which is far too long for a load run to observe a pool withdrawal settle")

	return cmd
}

// testnetStartCmd returns a cmd to start multi validator in-process testnet
func testnetStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"start",
		Short:	"Launch an in-process multi-validator testnet",
		Long: fmt.Sprintf(`testnet will launch an in-process multi-validator testnet,
and generate a directory for each validator populated with necessary
configuration files (private validator, genesis, config, etc.).

Example:
	%s testnet --validator-count 4 --output-dir ./.testnets
	`, version.AppName),
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			args := startArgs{}
			args.outputDir, _ = cmd.Flags().GetString(flagOutputDir)
			args.chainID, _ = cmd.Flags().GetString(flags.FlagChainID)
			args.minGasPrices, _ = cmd.Flags().GetString(server.FlagMinGasPrices)
			args.numValidators, _ = cmd.Flags().GetInt(flagNumValidators)
			args.algo, _ = cmd.Flags().GetString(flags.FlagKeyType)
			args.enableLogging, _ = cmd.Flags().GetBool(flagEnableLogging)
			args.rpcAddress, _ = cmd.Flags().GetString(flagRPCAddress)
			args.apiAddress, _ = cmd.Flags().GetString(flagAPIAddress)
			args.grpcAddress, _ = cmd.Flags().GetString(flagGRPCAddress)
			args.printMnemonic, _ = cmd.Flags().GetBool(flagPrintMnemonic)
			args.timeoutCommit, _ = cmd.Flags().GetDuration(flagCommitTimeout)
			if args.chainID != "" {
				if err := appparams.ValidateAetraTestnetChainID(args.chainID); err != nil {
					return err
				}
			}

			return startTestnet(cmd, args)
		},
	}

	addTestnetFlagsToCmd(cmd)
	cmd.Flags().Bool(flagEnableLogging, false, "Enable INFO logging of CometBFT validator nodes")
	cmd.Flags().String(flagRPCAddress, "tcp://0.0.0.0:26657", "the RPC address to listen on")
	cmd.Flags().String(flagAPIAddress, "tcp://0.0.0.0:1317", "the address to listen on for REST API")
	cmd.Flags().String(flagGRPCAddress, "0.0.0.0:9090", "the gRPC server address to listen on")
	cmd.Flags().Bool(flagPrintMnemonic, false, "Print mnemonic of first validator to stdout; use only for local manual testing")
	return cmd
}

const nodeDirPerm = 0o755

const bootstrapTestAssetDenom = "testtoken"

// initTestnetFiles initializes testnet files for a testnet to be run in a separate process
func initTestnetFiles(
	clientCtx client.Context,
	cmd *cobra.Command,
	nodeConfig *cmtconfig.Config,
	mm module.BasicManager,
	genBalIterator banktypes.GenesisBalancesIterator,
	args initArgs,
) error {
	if args.chainID == "" {
		args.chainID = "chain-" + unsafe.Str(6)
	}
	nodeIDs := make([]string, args.numValidators)
	valPubKeys := make([]cryptotypes.PubKey, args.numValidators)

	appConfig := srvconfig.DefaultConfig()
	appConfig.MinGasPrices = args.minGasPrices
	appConfig.API.Enable = true
	appConfig.Telemetry.Enabled = true
	appConfig.Telemetry.PrometheusRetentionTime = 60
	appConfig.Telemetry.EnableHostnameLabel = false
	appConfig.Telemetry.GlobalLabels = [][]string{{"chain_id", args.chainID}}

	var (
		genAccounts	[]authtypes.GenesisAccount
		genBalances	[]banktypes.Balance
		genFiles	[]string
		nativeAccounts	[]nativeaccounttypes.Account
	)
	const (
		rpcPort			= 26657
		apiPort			= 1317
		grpcPort		= 9090
		pprofListen		= 6060
		prometheusListen	= 27780
	)
	p2pPortStart := 26656

	inBuf := bufio.NewReader(cmd.InOrStdin())

	// Seeded so a given (--sim-seed, --validator-count, --extra-accounts) always
	// produces the same genesis: a load run whose supply distribution shifts
	// between runs cannot be compared against a previous one.
	rng := mathrand.New(mathrand.NewSource(args.simSeed))

	// Validator key name -> funded address, so --nominator-pool-authority can
	// name a node ("node0") instead of an address: the keys are generated in
	// this loop, so a caller cannot know the address beforehand.
	nodeAddrs := make(map[string]string, args.numValidators)

	for i := 0; i < args.numValidators; i++ {
		var portOffset int
		if args.singleMachine {
			portOffset = i
			p2pPortStart = 16656
			nodeConfig.P2P.AddrBookStrict = false
			nodeConfig.P2P.PexReactor = false
			nodeConfig.P2P.AllowDuplicateIP = true
			nodeConfig.Instrumentation.PrometheusListenAddr = fmt.Sprintf(":%d", prometheusListen+portOffset)
			nodeConfig.RPC.PprofListenAddress = fmt.Sprintf("localhost:%d", pprofListen+portOffset)
			appConfig.API.Address = fmt.Sprintf("tcp://0.0.0.0:%d", apiPort+portOffset)
			appConfig.GRPC.Address = fmt.Sprintf("0.0.0.0:%d", grpcPort+portOffset)
		}

		nodeDirName := fmt.Sprintf("%s%d", args.nodeDirPrefix, i)
		nodeDir := filepath.Join(args.outputDir, nodeDirName, args.nodeDaemonHome)
		gentxsDir := filepath.Join(args.outputDir, "gentxs")

		nodeConfig.SetRoot(nodeDir)
		nodeConfig.Moniker = nodeDirName
		nodeConfig.RPC.ListenAddress = fmt.Sprintf("tcp://%s:%d", args.listenIPAddress, rpcPort+portOffset)

		if err := os.MkdirAll(filepath.Join(nodeDir, "config"), nodeDirPerm); err != nil {
			_ = os.RemoveAll(args.outputDir)
			return err
		}
		var (
			err	error
			ip	string
		)
		if args.singleMachine {
			ip = "127.0.0.1"
		} else {
			ip, err = getIP(i, args.startingIPAddress)
			if err != nil {
				_ = os.RemoveAll(args.outputDir)
				return err
			}
		}

		nodeIDs[i], valPubKeys[i], err = genutil.InitializeNodeValidatorFiles(nodeConfig)
		if err != nil {
			_ = os.RemoveAll(args.outputDir)
			return err
		}

		memo := fmt.Sprintf("%s@%s:%d", nodeIDs[i], ip, p2pPortStart+portOffset)
		genFiles = append(genFiles, nodeConfig.GenesisFile())

		kb, err := keyring.New(sdk.KeyringServiceName(), args.keyringBackend, nodeDir, inBuf, clientCtx.Codec)
		if err != nil {
			return err
		}

		keyringAlgos, _ := kb.SupportedAlgorithms()
		algo, err := keyring.NewSigningAlgoFromString(args.algo, keyringAlgos)
		if err != nil {
			return err
		}

		addr, secret, err := testutil.GenerateSaveCoinKey(kb, nodeDirName, "", true, algo)
		if err != nil {
			_ = os.RemoveAll(args.outputDir)
			return err
		}

		// x/contracts and other AVM entrypoints require an active
		// native-account record (ensureActiveWallet) for the caller. A brand
		// new MsgActivateAccount always derives a domain-hashed v2 address
		// from the pubkey (see native-account/types.DeriveAccountAddress),
		// which never equals this bootstrap account's legacy-padded bech32
		// address — so a genesis validator could never activate its OWN
		// already-funded identity. Pre-populate genesis with an already
		// -active native-account record under the account's real, funded
		// address, so it can call AVM entrypoints without first sending
		// itself a self-activation transaction under a different address.
		keyRecord, err := kb.Key(nodeDirName)
		if err != nil {
			_ = os.RemoveAll(args.outputDir)
			return err
		}
		nodePubKey, err := keyRecord.GetPubKey()
		if err != nil {
			_ = os.RemoveAll(args.outputDir)
			return err
		}
		nativeAccounts = append(nativeAccounts, bootstrapNativeAccount(nodePubKey, uint64(i+1)))

		info := map[string]string{"secret": secret}

		cliPrint, err := json.Marshal(info)
		if err != nil {
			return err
		}

		if err := writeFile(fmt.Sprintf("%v.json", "key_seed"), nodeDir, cliPrint); err != nil {
			return err
		}

		// Self-bond is drawn per validator from [--validator-stake-aet-min,
		// --validator-stake-aet-max] and the account is funded with that plus
		// --validator-extra-aet to pay fees; at the 0.5 AET average transfer fee
		// a validator funded only to its self-bond could not send a single
		// transaction. One consensus-power unit is 1e6 naet (0.001 AET).
		// Defaults (100 / 100 / 900) reproduce the historical fixed 100 AET
		// self-bond funded with 1,000 AET exactly.
		stakeAet := pickAet(rng, args.valStakeMinAet, args.valStakeMaxAet)
		valTokens := aetToNaet(stakeAet)
		accTokens := sdk.TokensFromConsensusPower(1000, sdk.DefaultPowerReduction)
		coins := sdk.Coins{
			sdk.NewCoin(bootstrapTestAssetDenom, accTokens),
			sdk.NewCoin(args.bondTokenDenom, aetToNaet(stakeAet+args.valExtraAet)),
		}

		genBalances = append(genBalances, banktypes.Balance{Address: addr.String(), Coins: coins.Sort()})
		genAccounts = append(genAccounts, authtypes.NewBaseAccount(addr, nil, 0, 0))
		nodeAddrs[nodeDirName] = addr.String()

		valAddr := sdk.ValAddress(addr)
		valStr, err := clientCtx.TxConfig.SigningContext().ValidatorAddressCodec().BytesToString(valAddr)
		if err != nil {
			return err
		}
		createValMsg, err := stakingtypes.NewMsgCreateValidator(
			valStr,
			valPubKeys[i],
			sdk.NewCoin(args.bondTokenDenom, valTokens),
			stakingtypes.NewDescription(nodeDirName, "", "", "", ""),
			// SA2 #32: default validators to a realistic 5% rate / 20% max / 1%
			// max daily change (was 100%/100%/100%), consistent with the
			// app/params commission floor and ceiling.
			stakingtypes.NewCommissionRates(math.LegacyNewDecWithPrec(5, 2), math.LegacyNewDecWithPrec(20, 2), math.LegacyNewDecWithPrec(1, 2)),
			math.OneInt(),
		)
		if err != nil {
			return err
		}

		txBuilder := clientCtx.TxConfig.NewTxBuilder()
		if err := txBuilder.SetMsgs(createValMsg); err != nil {
			return err
		}

		txBuilder.SetMemo(memo)

		txFactory := tx.Factory{}
		txFactory = txFactory.
			WithChainID(args.chainID).
			WithMemo(memo).
			WithKeybase(kb).
			WithTxConfig(clientCtx.TxConfig)

		if err := tx.Sign(cmd.Context(), txFactory, nodeDirName, txBuilder, true); err != nil {
			return err
		}

		txBz, err := clientCtx.TxConfig.TxJSONEncoder()(txBuilder.GetTx())
		if err != nil {
			return err
		}

		if err := writeFile(fmt.Sprintf("%v.json", nodeDirName), gentxsDir, txBz); err != nil {
			return err
		}

		srvconfig.SetConfigTemplate(srvconfig.DefaultConfigTemplate)

		srvconfig.WriteConfigFile(filepath.Join(nodeDir, "config", "app.toml"), appConfig)
	}

	// Ordinary funded wallets beyond the validator set, keyed into
	// <output-dir>/wallets. A load run needs users who are not also the block
	// producers: a validator's balance moves with rewards and its own fees, so
	// measuring transfer/staking behaviour on validator accounts confounds the
	// two.
	//
	// Each wallet also gets an active native-account record, for the same reason
	// the validators do (see bootstrapNativeAccount): MsgActivateAccount derives
	// a domain-hashed v2 address that never equals the funded bootstrap address,
	// so a wallet without a pre-seeded record can never call an AVM entrypoint
	// under the identity it was actually funded at.
	if args.extraAccounts > 0 {
		walletDir := filepath.Join(args.outputDir, "wallets")
		if err := os.MkdirAll(walletDir, nodeDirPerm); err != nil {
			_ = os.RemoveAll(args.outputDir)
			return err
		}

		walletKb, err := keyring.New(sdk.KeyringServiceName(), args.keyringBackend, walletDir, inBuf, clientCtx.Codec)
		if err != nil {
			return err
		}
		walletAlgos, _ := walletKb.SupportedAlgorithms()
		walletAlgo, err := keyring.NewSigningAlgoFromString(args.algo, walletAlgos)
		if err != nil {
			return err
		}

		for i := 0; i < args.extraAccounts; i++ {
			walletName := fmt.Sprintf("wallet%d", i)
			walletAddr, _, err := testutil.GenerateSaveCoinKey(walletKb, walletName, "", true, walletAlgo)
			if err != nil {
				_ = os.RemoveAll(args.outputDir)
				return err
			}

			balanceAet := pickAet(rng, args.extraAccountMinAet, args.extraAccountMaxAet)
			genBalances = append(genBalances, banktypes.Balance{
				Address:	walletAddr.String(),
				Coins:		sdk.Coins{sdk.NewCoin(args.bondTokenDenom, aetToNaet(balanceAet))}.Sort(),
			})
			genAccounts = append(genAccounts, authtypes.NewBaseAccount(walletAddr, nil, 0, 0))

			walletRecord, err := walletKb.Key(walletName)
			if err != nil {
				_ = os.RemoveAll(args.outputDir)
				return err
			}
			walletPubKey, err := walletRecord.GetPubKey()
			if err != nil {
				_ = os.RemoveAll(args.outputDir)
				return err
			}
			nativeAccounts = append(nativeAccounts, bootstrapNativeAccount(walletPubKey, uint64(args.numValidators+i+1)))
		}
	}

	// A validator name resolves to that validator's address; anything else is
	// passed through as a literal address. Node names are never valid addresses,
	// so this cannot be ambiguous.
	poolAuthority := args.poolAuthority
	if resolved, ok := nodeAddrs[poolAuthority]; ok {
		poolAuthority = resolved
	}

	if err := initGenFiles(clientCtx, mm, args.chainID, genAccounts, genBalances, nativeAccounts, genFiles, args.numValidators, simGenesisOverrides{
		enableContracts:	args.enableContracts,
		poolAuthority:		poolAuthority,
		unbondingTime:		args.unbondingTime,
	}); err != nil {
		return err
	}

	err := collectGenFiles(
		clientCtx, nodeConfig, args.chainID, nodeIDs, valPubKeys, args.numValidators,
		args.outputDir, args.nodeDirPrefix, args.nodeDaemonHome, genBalIterator,
		rpcPort, p2pPortStart, args.singleMachine,
	)
	if err != nil {
		return err
	}

	if err := applyConsensusBlockGasCap(genFiles); err != nil {
		return err
	}

	cmd.PrintErrf("Successfully initialized %d node directories\n", args.numValidators)
	return nil
}

// startTestnet starts an in-process testnet
func startTestnet(cmd *cobra.Command, args startArgs) error {
	networkConfig := network.DefaultConfig(l1app.NewTestNetworkFixture)

	if args.chainID != "" {
		networkConfig.ChainID = args.chainID
	}
	networkConfig.SigningAlgo = args.algo
	networkConfig.MinGasPrices = args.minGasPrices
	networkConfig.NumValidators = args.numValidators
	networkConfig.EnableLogging = args.enableLogging
	networkConfig.RPCAddress = args.rpcAddress
	networkConfig.APIAddress = args.apiAddress
	networkConfig.GRPCAddress = args.grpcAddress
	networkConfig.PrintMnemonic = args.printMnemonic
	networkConfig.TimeoutCommit = args.timeoutCommit
	networkLogger := network.NewCLILogger(cmd)

	baseDir := fmt.Sprintf("%s/%s", args.outputDir, networkConfig.ChainID)
	if _, err := os.Stat(baseDir); !os.IsNotExist(err) {
		return fmt.Errorf(
			"testnets directory already exists for chain-id '%s': %s, please remove or select a new --chain-id",
			networkConfig.ChainID, baseDir)
	}

	testnet, err := network.New(networkLogger, baseDir, networkConfig)
	if err != nil {
		return err
	}

	if _, err := testnet.WaitForHeight(1); err != nil {
		return err
	}
	cmd.Println("press the Enter Key to terminate")
	if _, err := fmt.Scanln(); err != nil {
		return err
	}
	testnet.Cleanup()

	return nil
}
