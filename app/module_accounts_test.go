package app

import (
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	protocolpooltypes "github.com/cosmos/cosmos-sdk/x/protocolpool/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	burntypes "github.com/sovereign-l1/l1/x/burn/types"
	configtypes "github.com/sovereign-l1/l1/x/config/types"
	feecollectortypes "github.com/sovereign-l1/l1/x/fee-collector/types"
	feestypes "github.com/sovereign-l1/l1/x/fees/types"
	mintauthoritytypes "github.com/sovereign-l1/l1/x/mint-authority/types"
	nominatorpooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
	systemregistrytypes "github.com/sovereign-l1/l1/x/system-registry/types"
	validatorelectiontypes "github.com/sovereign-l1/l1/x/validator-election/types"
)

func TestPrototypeModuleAccountPermissionsAreNarrow(t *testing.T) {
	expected := map[string][]string{
		authtypes.FeeCollectorName:			nil,
		distrtypes.ModuleName:				nil,
		minttypes.ModuleName:				{authtypes.Minter},
		stakingtypes.BondedPoolName:			{authtypes.Burner, authtypes.Staking},
		stakingtypes.NotBondedPoolName:			{authtypes.Burner, authtypes.Staking},
		govtypes.ModuleName:				{authtypes.Burner},
		protocolpooltypes.ModuleName:			nil,
		protocolpooltypes.ProtocolPoolEscrowAccount:	nil,
		burntypes.ModuleName:				{authtypes.Burner},
		feecollectortypes.CollectorModuleName:		{authtypes.Burner},
		feecollectortypes.TreasuryModuleName:		nil,
		feecollectortypes.ProtectionModuleName:		nil,
		feecollectortypes.ValidatorInsuranceModuleName:	nil,
		feecollectortypes.EcosystemGrantsModuleName:	nil,
		feecollectortypes.StorageRentReserveModuleName:	nil,
		feecollectortypes.BurnModuleName:		nil,
		feecollectortypes.ReporterRewardsModuleName:	nil,
		mintauthoritytypes.ModuleName:			{authtypes.Minter},
		// storage-rent, delegator-protection and validator-insurance
		// intentionally have no module account: their reserves are the
		// feecollector_* buckets listed above, exactly as the reporter module
		// custodies via feecollector_reporter_rewards rather than an account of
		// its own.
		configtypes.ModuleName:				nil,
		systemregistrytypes.ModuleName:			nil,
		validatorelectiontypes.ModuleName:		nil,
		feestypes.ModuleName:				nil,
		// nominator-pool now custodies real deposits and delegates them to
		// validators directly -- it is its own custodian, unlike
		// storage-rent/delegator-protection/validator-insurance above.
		nominatorpooltypes.ModuleName:			nil,
	}
	require.Equal(t, expected, GetMaccPerms())

	blocked := BlockedAddresses()
	for moduleName := range expected {
		addr := authtypes.NewModuleAddress(moduleName).String()
		switch moduleName {
		case govtypes.ModuleName:
			require.False(t, blocked[addr])
			continue
		case nominatorpooltypes.ModuleName:
			// nominator-pool's real module account is a genuine x/staking
			// delegator (see PoolModuleAddress in x/nominator-pool/keeper).
			// x/staking's BeforeDelegationSharesModified hook and
			// CompleteUnbonding both pay straight into the delegator address
			// via bankKeeper.SendCoinsFromModuleToAccount, which errors on a
			// blocked recipient -- live-verified broadcasting a real
			// withdrawal against a testnet build that still blocked it.
			require.False(t, blocked[addr])
			continue
		}
		require.True(t, blocked[addr], moduleName)
	}
}
