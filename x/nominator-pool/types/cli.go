package types

import "github.com/spf13/cobra"

func NewTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:				ModuleName,
		Short:				"Nominator pool transaction commands",
		DisableFlagParsing:		true,
		SuggestionsMinimumDistance:	2,
		RunE:				cobra.NoArgs,
	}
	// D4: "withdraw" (MsgWithdrawPoolStake) is deliberately NOT here.
	//
	// It advertised a transaction no user can send and no user needs:
	//
	//   - It cannot be signed. It has no CustomGetSigners entry, so the signing
	//     context answers "no cosmos.msg.v1.signer option found" and the tx is
	//     rejected before it reaches a block. That is not an oversight to fix by
	//     adding one: MsgWithdrawPoolStake is authenticated by CallerContractUser,
	//     which the keeper requires to equal pool.ContractAddressUser (keeper.go).
	//     A contract address is public, so a user-signed withdraw would let anyone
	//     supply that value and walk straight through the "requires official liquid
	//     staking contract" check -- turning the pool's only contract gate into a
	//     comparison against a published constant. Wiring a signer here would be a
	//     downgrade, not a fix. tx.go's nominatorPoolMessageFields says the same
	//     thing from the other side: these messages are contract-internal and
	//     intentionally carry no on-wire descriptor.
	//   - It is not needed. A depositor exits with request-unbond (which IS signed)
	//     and the EndBlocker settles the matured cohort and pays them
	//     automatically -- see settlePoolWithdrawals. WithdrawPoolStake is only an
	//     on-demand settle for the official contract path, and nothing dispatches
	//     it: no module in this repo calls the keeper method.
	//
	// If the official liquid-staking contract ever does need on-demand settlement,
	// the correct wiring is a contract-dispatch path that proves the CALLER is the
	// contract -- not a user signer. Pinned by
	// app/nominator_pool_signing_test.go.
	for _, use := range []string{
		"create-pool",
		"deposit-to-pool",
		"request-withdrawal",
		"cancel-withdrawal",
		"deposit",
		"request-unbond",
		"claim-rewards",
		"sync-rewards",
		"claim-staking-rewards",
		"claim-reputation",
		"top-up-reserve",
		"update-pool-commission",
		"change-pool-validator",
		"register-validator",
		"update-validator",
		"update-staking-params",
		"create-official-pool",
	} {
		cmd.AddCommand(&cobra.Command{Use: use, Short: "Build " + use + " transaction", RunE: cobra.NoArgs})
	}
	return cmd
}

func NewQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:				ModuleName,
		Short:				"Nominator pool query commands",
		DisableFlagParsing:		true,
		SuggestionsMinimumDistance:	2,
		RunE:				cobra.NoArgs,
	}
	for _, use := range []string{
		"pool",
		"pools",
		"pool-delegator",
		"pool-rewards",
		"pool-share",
		"pool-allocations",
		"stake-reputation",
		"account-reputation",
		"staking-rewards",
		"staking-proof",
		"pool-unbonding-queue",
	} {
		cmd.AddCommand(&cobra.Command{Use: use, Short: "Run " + use + " query", RunE: cobra.NoArgs})
	}
	return cmd
}
