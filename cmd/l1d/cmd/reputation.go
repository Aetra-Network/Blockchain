package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"

	"github.com/sovereign-l1/l1/x/reputation/types/reputationpb"
)

// NewReputationQueryCmd exposes the reputation module's live query surface
// (backed by reputationpb.QueryServer, registered in x/reputation/module.go)
// over the CLI. Before this command existed there was no way to drive these
// queries from l1d at all -- see RESULTS_V1-live-testnet-exercise.md section
// 5 ("Репутация кошельков: query НЕ реализован"). ReporterReputation and
// ReputationHistory are intentionally omitted: both are documented as
// deprecated in x/reputation/keeper/query_server.go (they always return
// NotFound / empty results respectively, by design, not a bug).
func NewReputationQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reputation",
		Short: "Reputation query helpers",
	}
	cmd.AddCommand(
		newReputationValidatorScoreQueryCmd(),
		newReputationParamsQueryCmd(),
	)
	return cmd
}

func newReputationValidatorScoreQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validator-score [address]",
		Short: "Query l1.reputation.v1.Query/ValidatorReputation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			res, err := reputationpb.NewQueryClient(clientCtx).ValidatorReputation(cmd.Context(), &reputationpb.QueryValidatorReputationRequest{
				Address: strings.TrimSpace(args[0]),
			})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func newReputationParamsQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "params",
		Short: "Query l1.reputation.v1.Query/ReputationParams",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			res, err := reputationpb.NewQueryClient(clientCtx).ReputationParams(cmd.Context(), &reputationpb.QueryReputationParamsRequest{})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
