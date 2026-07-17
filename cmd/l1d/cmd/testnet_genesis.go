package cmd

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	cmtconfig "github.com/cometbft/cometbft/config"
	cmttypes "github.com/cometbft/cometbft/types"
	cmttime "github.com/cometbft/cometbft/types/time"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	appparams "github.com/sovereign-l1/l1/app/params"
	contractstypes "github.com/sovereign-l1/l1/x/contracts/types"
	feestypes "github.com/sovereign-l1/l1/x/fees/types"
	identityroottypes "github.com/sovereign-l1/l1/x/identity-root/types"
	nativeaccounttypes "github.com/sovereign-l1/l1/x/native-account/types"
	nominatorpooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
)

// simGenesisOverrides are opt-in genesis changes a local load/simulation net
// needs and a public testnet must not get. The zero value changes nothing, so
// the public launch genesis keeps its exact shape.
type simGenesisOverrides struct {
	// enableContracts turns the AVM on. Off by default: the public testnet
	// ships contracts disabled behind the keeper gate (see below).
	enableContracts bool
	// poolAuthority replaces x/nominator-pool's default all-zero authority
	// address. Without it MsgCreateNominatorPool can never be authorized, so
	// liquid staking cannot be exercised at all.
	poolAuthority string
	// unbondingTime shortens x/staking's 21-day default so a run can actually
	// observe a pool withdrawal settle.
	unbondingTime time.Duration
}

func initGenFiles(
	clientCtx client.Context, mm module.BasicManager, chainID string,
	genAccounts []authtypes.GenesisAccount, genBalances []banktypes.Balance,
	nativeAccounts []nativeaccounttypes.Account,
	genFiles []string, numValidators int, sim simGenesisOverrides,
) error {
	appGenState := mm.DefaultGenesis(clientCtx.Codec)

	var authGenState authtypes.GenesisState
	clientCtx.Codec.MustUnmarshalJSON(appGenState[authtypes.ModuleName], &authGenState)

	accounts, err := authtypes.PackAccounts(genAccounts)
	if err != nil {
		return err
	}

	authGenState.Accounts = accounts
	appGenState[authtypes.ModuleName] = clientCtx.Codec.MustMarshalJSON(&authGenState)

	var bankGenState banktypes.GenesisState
	clientCtx.Codec.MustUnmarshalJSON(appGenState[banktypes.ModuleName], &bankGenState)

	bankGenState.Balances = banktypes.SanitizeGenesisBalances(genBalances)
	for _, bal := range bankGenState.Balances {
		bankGenState.Supply = bankGenState.Supply.Add(bal.Coins...)
	}
	bankGenState.DenomMetadata = appparams.EnsureNativeTokenMetadata(bankGenState.DenomMetadata)
	appGenState[banktypes.ModuleName] = clientCtx.Codec.MustMarshalJSON(&bankGenState)
	appGenState[minttypes.ModuleName] = clientCtx.Codec.MustMarshalJSON(appparams.AetraMintGenesisState())

	// The cosmos-sdk staking default MaxValidators (100) is below the Aetra
	// genesis-phase ceiling and would silently block the documented validator
	// growth plan (100/128 genesis -> 150/200 growth -> 250/300 mature; see
	// app/params/network_profile.go). Genesis starts at the genesis-phase
	// ceiling; later phases raise it via governance MsgUpdateParams.
	//
	// MinCommissionRate is left at the cosmos-sdk stock default of 0% by
	// InitCmd/this codegen path, which permits a 0%-commission validator (a
	// live-verified decentralization defect: nothing stops the race to the
	// bottom that eventually starves smaller operators of any margin). Set
	// the documented 3% floor (app/params StakingCommissionFloorBps) here so
	// every genesis this codepath produces actually carries it, rather than
	// leaving it to a policy struct (x/dynamic-commission,
	// x/aetra-staking-policy) that nothing on the live create-validator path
	// consults.
	var stakingGenState stakingtypes.GenesisState
	clientCtx.Codec.MustUnmarshalJSON(appGenState[stakingtypes.ModuleName], &stakingGenState)
	stakingGenState.Params.MaxValidators = appparams.AetraValidatorSetGenesisMax
	stakingGenState.Params.MinCommissionRate = appparams.BpsToLegacyDec(appparams.StakingCommissionFloorBps)
	if sim.unbondingTime > 0 {
		stakingGenState.Params.UnbondingTime = sim.unbondingTime
	}
	appGenState[stakingtypes.ModuleName] = clientCtx.Codec.MustMarshalJSON(&stakingGenState)

	// The cosmos-sdk slashing stock defaults are NOT what Aetra's own policy
	// constants specify (app/params AetraSlashingParams) -- and this codegen
	// path is the one place slashing genesis is actually assembled for a real
	// network (app.DefaultGenesis()/ApplyCoreModuleDefaults, which DOES apply
	// AetraSlashingParams, is bypassed here in favor of mm.DefaultGenesis
	// directly, mirroring how mint/MaxValidators above already have to be
	// re-applied by hand). Live-verified drift before this fix: downtime slash
	// 1% instead of the intended 0.05% (20x harsher than designed), downtime
	// jail 10 minutes instead of 60.
	var slashingGenState slashingtypes.GenesisState
	clientCtx.Codec.MustUnmarshalJSON(appGenState[slashingtypes.ModuleName], &slashingGenState)
	slashingGenState.Params = appparams.AetraSlashingParams()
	appGenState[slashingtypes.ModuleName] = clientCtx.Codec.MustMarshalJSON(&slashingGenState)

	// AVM smart-contract execution ships DISABLED for the public-testnet launch
	// profile. The on-chain contract runtime (StoreCode bytecode verification,
	// inter-contract async delivery, and live-node execution evidence) is still
	// behind the keeper gate per docs/public-testnet-production-gates.md, which
	// explicitly allows launching the base chain without contracts. A
	// validator-liveness testnet therefore runs base-chain modules only;
	// contracts are turned on later via governance MsgUpdateContractParams once
	// the AVM hardening + adversarial + audit gates are green.
	//
	// x/contracts marshals its genesis with plain encoding/json (see
	// x/contracts/module.go mustMarshalGenesis), not the proto JSONCodec. The
	// genesis state root commits to Params.Enabled (ComputeContractsStateRoot),
	// so it must be recomputed after flipping the flag or Validate() rejects the
	// state-root mismatch.
	var contractsGenState contractstypes.GenesisState
	if err := json.Unmarshal(appGenState[contractstypes.ModuleName], &contractsGenState); err != nil {
		return fmt.Errorf("unmarshal contracts default genesis: %w", err)
	}
	contractsGenState.Params.Enabled = sim.enableContracts
	contractsGenState.StateRoot = contractstypes.ComputeContractsStateRoot(contractsGenState)
	if err := contractsGenState.Validate(); err != nil {
		return fmt.Errorf("invalid contracts launch genesis (enabled=%t): %w", sim.enableContracts, err)
	}
	contractsGenStateJSON, err := json.Marshal(contractsGenState)
	if err != nil {
		return fmt.Errorf("marshal contracts genesis: %w", err)
	}
	appGenState[contractstypes.ModuleName] = contractsGenStateJSON

	// x/nominator-pool ships with Params.Authority set to prototype's all-zero
	// address, which nobody holds a key for, so MsgCreateNominatorPool always
	// fails Params.Authorize and the official liquid-staking pool can never be
	// registered. On a real network that authority is the gov module account;
	// on a localnet it has to be an operator key that can actually sign (see
	// x/nominator-pool/types/signing.go:92-97).
	//
	// Patched as raw JSON on purpose: the module marshals its genesis with plain
	// encoding/json and untagged Go field names (Version/Params/State), not the
	// proto codec, and commits no state root -- so a keyed edit is both correct
	// and cheaper than pulling the keeper's GenesisState type into cmd.
	if sim.poolAuthority != "" {
		poolRaw, ok := appGenState[nominatorpooltypes.ModuleName]
		if !ok {
			return fmt.Errorf("nominator-pool default genesis missing")
		}
		var poolGen map[string]json.RawMessage
		if err := json.Unmarshal(poolRaw, &poolGen); err != nil {
			return fmt.Errorf("unmarshal nominator-pool genesis: %w", err)
		}
		var poolParams map[string]json.RawMessage
		if err := json.Unmarshal(poolGen["Params"], &poolParams); err != nil {
			return fmt.Errorf("unmarshal nominator-pool params: %w", err)
		}
		authorityJSON, err := json.Marshal(sim.poolAuthority)
		if err != nil {
			return err
		}
		poolParams["Authority"] = authorityJSON
		poolParamsJSON, err := json.Marshal(poolParams)
		if err != nil {
			return err
		}
		poolGen["Params"] = poolParamsJSON
		poolGenJSON, err := json.Marshal(poolGen)
		if err != nil {
			return err
		}
		appGenState[nominatorpooltypes.ModuleName] = poolGenJSON
	}

	// x/native-account uses plain encoding/json for its genesis (see
	// x/native-account/module.go mustMarshalGenesis/unmarshalGenesis), not
	// the proto JSONCodec used above.
	if len(nativeAccounts) > 0 {
		var nativeAccountGenState nativeaccounttypes.GenesisState
		if err := json.Unmarshal(appGenState[nativeaccounttypes.ModuleName], &nativeAccountGenState); err != nil {
			return fmt.Errorf("unmarshal native account default genesis: %w", err)
		}
		nativeAccountGenState.Accounts = append(nativeAccountGenState.Accounts, nativeAccounts...)
		if err := nativeAccountGenState.Validate(); err != nil {
			return fmt.Errorf("invalid bootstrap native account genesis: %w", err)
		}
		nativeAccountGenStateJSON, err := json.Marshal(nativeAccountGenState)
		if err != nil {
			return fmt.Errorf("marshal native account genesis: %w", err)
		}
		appGenState[nativeaccounttypes.ModuleName] = nativeAccountGenStateJSON
	}

	// x/identity-root (the .aet collection) ships DISABLED with the full mainnet
	// price table, exactly like x/contracts ships contracts-off. A testnet /
	// localnet genesis turns it on with the TESTNET profile, divides every price
	// tier by 10, and shortens the issuance auction to ~1 min (12 blocks). This
	// is a GENESIS choice, never a runtime chain-id branch in consensus code
	// (docs/architecture/ans.md): the keeper reads whatever the param says.
	//
	// Patched as raw JSON: the module marshals its genesis with plain
	// encoding/json and untagged Go field names (Version/Params/IdentityParams/
	// State), not the proto codec, and commits no state root.
	if irRaw, ok := appGenState[identityroottypes.ModuleName]; ok {
		var irGen map[string]json.RawMessage
		if err := json.Unmarshal(irRaw, &irGen); err != nil {
			return fmt.Errorf("unmarshal identity-root genesis: %w", err)
		}
		var params map[string]json.RawMessage
		if err := json.Unmarshal(irGen["Params"], &params); err != nil {
			return fmt.Errorf("unmarshal identity-root params: %w", err)
		}
		params["Enabled"] = json.RawMessage("true")
		params["TestnetProfile"] = json.RawMessage("true")
		paramsJSON, err := json.Marshal(params)
		if err != nil {
			return err
		}
		irGen["Params"] = paramsJSON

		var identityParams map[string]json.RawMessage
		if err := json.Unmarshal(irGen["IdentityParams"], &identityParams); err != nil {
			return fmt.Errorf("unmarshal identity-root identity params: %w", err)
		}
		var tiers []identityroottypes.PriceTier
		if err := json.Unmarshal(identityParams["PriceTable"], &tiers); err != nil {
			return fmt.Errorf("unmarshal identity-root price table: %w", err)
		}
		for i := range tiers {
			price, ok := new(big.Int).SetString(tiers[i].PriceNaet, 10)
			if !ok {
				return fmt.Errorf("invalid identity-root price %q", tiers[i].PriceNaet)
			}
			tiers[i].PriceNaet = price.Div(price, big.NewInt(10)).String()
		}
		tiersJSON, err := json.Marshal(tiers)
		if err != nil {
			return err
		}
		identityParams["PriceTable"] = tiersJSON
		identityParams["IssuanceAuctionDurationBlocks"] = json.RawMessage(fmt.Sprintf("%d", identityroottypes.TestnetIssuanceAuctionDurationBlocks))
		identityParamsJSON, err := json.Marshal(identityParams)
		if err != nil {
			return err
		}
		irGen["IdentityParams"] = identityParamsJSON

		irGenJSON, err := json.Marshal(irGen)
		if err != nil {
			return err
		}
		appGenState[identityroottypes.ModuleName] = irGenJSON
	}

	appGenStateJSON, err := json.MarshalIndent(appGenState, "", "  ")
	if err != nil {
		return err
	}

	appGenesis := genutiltypes.NewAppGenesisWithVersion(chainID, appGenStateJSON)
	for i := 0; i < numValidators; i++ {
		if err := appGenesis.SaveAs(genFiles[i]); err != nil {
			return err
		}
	}
	return nil
}

// applyConsensusBlockGasCap patches each already-written genesis file's
// consensus_params.block.max_gas from CometBFT's stock -1 (unlimited) to
// x/fees' own declared block gas budget, so there is a real, structural
// ceiling at the consensus layer independent of the in-app (block-gas-meter
// backed) admission check.
//
// This must run AFTER collectGenFiles, not as part of initGenFiles: the
// "collect" step's genutil.ExportGenesisFileWithTime rebuilds each node's
// AppGenesis from scratch via NewAppGenesisWithVersion (Consensus.Params
// nil), discarding whatever consensus params an earlier write set. Live
// symptom before this fix: a block was accepted carrying 21,014,289 gas
// against x/fees' own MaxBlockGas of 20,000,000, because nothing at the
// consensus layer enforced a cap at all.
func applyConsensusBlockGasCap(genFiles []string) error {
	for _, genFile := range genFiles {
		appGenesis, err := genutiltypes.AppGenesisFromFile(genFile)
		if err != nil {
			return err
		}
		if appGenesis.Consensus == nil {
			appGenesis.Consensus = &genutiltypes.ConsensusGenesis{}
		}
		consensusParams := appGenesis.Consensus.Params
		if consensusParams == nil {
			consensusParams = cmttypes.DefaultConsensusParams()
		}
		consensusParams.Block.MaxGas = int64(feestypes.DefaultMaxBlockGas)
		appGenesis.Consensus.Params = consensusParams
		if err := appGenesis.SaveAs(genFile); err != nil {
			return err
		}
	}
	return nil
}

func collectGenFiles(
	clientCtx client.Context,
	nodeConfig *cmtconfig.Config,
	chainID string,
	nodeIDs []string,
	valPubKeys []types.PubKey,
	numValidators int,
	outputDir, nodeDirPrefix, nodeDaemonHome string,
	genBalIterator banktypes.GenesisBalancesIterator,
	rpcPortStart, p2pPortStart int,
	singleMachine bool,
) error {
	var appState json.RawMessage
	genTime := cmttime.Now()

	for i := 0; i < numValidators; i++ {
		if singleMachine {
			portOffset := i
			nodeConfig.RPC.ListenAddress = fmt.Sprintf("tcp://0.0.0.0:%d", rpcPortStart+portOffset)
			nodeConfig.P2P.ListenAddress = fmt.Sprintf("tcp://0.0.0.0:%d", p2pPortStart+portOffset)
		}

		nodeDirName := fmt.Sprintf("%s%d", nodeDirPrefix, i)
		nodeDir := filepath.Join(outputDir, nodeDirName, nodeDaemonHome)
		gentxsDir := filepath.Join(outputDir, "gentxs")
		nodeConfig.Moniker = nodeDirName
		nodeConfig.SetRoot(nodeDir)

		initCfg := genutiltypes.NewInitConfig(chainID, gentxsDir, nodeIDs[i], valPubKeys[i])
		appGenesis, err := genutiltypes.AppGenesisFromFile(nodeConfig.GenesisFile())
		if err != nil {
			return err
		}

		nodeAppState, err := genutil.GenAppStateFromConfig(
			clientCtx.Codec,
			clientCtx.TxConfig,
			nodeConfig,
			initCfg,
			appGenesis,
			genBalIterator,
			genutiltypes.DefaultMessageValidator,
			clientCtx.TxConfig.SigningContext().ValidatorAddressCodec(),
		)
		if err != nil {
			return err
		}

		if appState == nil {
			appState = nodeAppState
		}

		if err := genutil.ExportGenesisFileWithTime(nodeConfig.GenesisFile(), chainID, nil, appState, genTime); err != nil {
			return err
		}
	}

	return nil
}

func getIP(i int, startingIPAddr string) (ip string, err error) {
	if len(startingIPAddr) == 0 {
		ip, err = server.ExternalIP()
		if err != nil {
			return "", err
		}
		return ip, nil
	}
	return calculateIP(startingIPAddr, i)
}

func calculateIP(ip string, i int) (string, error) {
	ipv4 := net.ParseIP(ip).To4()
	if ipv4 == nil {
		return "", fmt.Errorf("%v: non ipv4 address", ip)
	}

	for j := 0; j < i; j++ {
		ipv4[3]++
	}

	return ipv4.String(), nil
}

func writeFile(name, dir string, contents []byte) error {
	file := filepath.Join(dir, name)

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("could not create directory %q: %w", dir, err)
	}

	if err := os.WriteFile(file, contents, 0o600); err != nil {
		return err
	}

	return nil
}
