package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"

	"github.com/sovereign-l1/l1/x/aez/types"
)

// GetQueryCmd wires the aez query subcommands, mirroring x/fees/client/cli's
// conventions exactly: a DisableFlagParsing group node delegating to
// client.ValidateCmd, with real flag parsing on each leaf command.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:				types.ModuleName,
		Short:				"AEZ queries",
		DisableFlagParsing:		true,
		SuggestionsMinimumDistance:	2,
		RunE:				client.ValidateCmd,
	}
	cmd.AddCommand(
		NewParamsCmd(),
		NewRoutingTableCmd(),
		NewPendingRoutingTableCmd(),
		NewZonesCmd(),
		NewZoneOfCmd(),
	)
	return cmd
}

func NewParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"params",
		Short:	"Query AEZ module params",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).Params(cmd.Context(), &types.QueryParamsRequest{})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func NewRoutingTableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"routing-table [version]",
		Short:	"Query the AEZ bucket->zone routing table",
		Long:	"Query the AEZ bucket->zone routing table. With no argument, returns the CURRENT active table. With [version], returns that specific historical table version if it is still retained.",
		Args:	cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var version uint64
			if len(args) == 1 {
				v, err := strconv.ParseUint(args[0], 10, 64)
				if err != nil {
					return fmt.Errorf("invalid version %q: %w", args[0], err)
				}
				version = v
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).RoutingTable(cmd.Context(), &types.QueryRoutingTableRequest{Version: version})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func NewPendingRoutingTableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"pending-routing-table",
		Short:	"Query the AEZ routing table scheduled to activate, if any",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).PendingRoutingTable(cmd.Context(), &types.QueryPendingRoutingTableRequest{})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func NewZonesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"zones",
		Short:	"List all AEZ zones",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).Zones(cmd.Context(), &types.QueryZonesRequest{})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

func NewZoneOfCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"zone-of [kind] [entity]",
		Short:	"Query which AEZ zone an entity resolves to",
		Long:	`Query which AEZ zone an entity resolves to.

kind is one of "address", "contract", or "name". entity is the address
(either "AE..." or "ae1..." encoding), or, for kind=name, a normalized FQDN.`,
		Args:	cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).ZoneOf(cmd.Context(), &types.QueryZoneOfRequest{Kind: args[0], Entity: args[1]})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}
