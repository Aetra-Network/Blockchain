package cli

import (
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"

	"github.com/sovereign-l1/l1/x/identity-root/types"
)

// GetQueryCmd wires every real x/identity-root Query RPC (see
// types/query.go's QueryServer interface) into a cobra command tree,
// mirroring x/fees's client/cli/query.go conventions exactly: Use =
// types.ModuleName, DisableFlagParsing on the parent,
// SuggestionsMinimumDistance, RunE: client.ValidateCmd, and every leaf using
// client.GetClientQueryContext + flags.AddQueryFlagsToCmd +
// clientCtx.PrintProto.
func GetQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:				types.ModuleName,
		Short:				"Identity root (ANS) queries",
		DisableFlagParsing:		true,
		SuggestionsMinimumDistance:	2,
		RunE:				client.ValidateCmd,
	}
	cmd.AddCommand(
		NewCollectionParamsCmd(),
		NewCollectionBalanceCmd(),
		NewPriceForLabelCmd(),
		NewAuctionsCmd(),
		NewAuctionCmd(),
		NewDomainStatusCmd(),
		NewNameRecordCmd(),
		NewResolveNameCmd(),
		NewReverseRecordCmd(),
		NewSubdomainsCmd(),
		NewNameZoneCmd(),
		NewListingCmd(),
	)
	return cmd
}

// NewCollectionParamsCmd queries the .aet collection's governance params
// and price table.
func NewCollectionParamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"collection-params",
		Short:	"Query the name collection's governance params and price table",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).CollectionParams(cmd.Context(), &types.QueryCollectionParamsRequest{})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewCollectionBalanceCmd queries the name collection module account's
// balance, escrow, and retained totals.
func NewCollectionBalanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"collection-balance",
		Short:	"Query the name collection module account's balance",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).CollectionBalance(cmd.Context(), &types.QueryCollectionBalanceRequest{})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewPriceForLabelCmd queries the current price tier for a label of the
// given length.
func NewPriceForLabelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"price-for-label",
		Short:	"Query the current REGISTER price for a label",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			label, err := cmd.Flags().GetString(flagLabel)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).PriceForLabel(cmd.Context(), &types.QueryPriceForLabelRequest{Label: label})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	cmd.Flags().String(flagLabel, "", "label to price (unqualified, no root namespace suffix)")
	_ = cmd.MarkFlagRequired(flagLabel)
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewAuctionsCmd lists every open auction.
func NewAuctionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"auctions",
		Short:	"List every open name auction",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).Auctions(cmd.Context(), &types.QueryAuctionsRequest{})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewAuctionCmd queries the open auction for a specific name, if any.
func NewAuctionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"auction",
		Short:	"Query the open auction for a name",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			name, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).Auction(cmd.Context(), &types.QueryAuctionRequest{Name: name})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	cmd.Flags().String(flagName, "", "name (FQDN) to look up")
	_ = cmd.MarkFlagRequired(flagName)
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewDomainStatusCmd queries a name's registration/auction status as of an
// optional height.
func NewDomainStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"domain-status",
		Short:	"Query a name's registration and auction status",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			name, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return err
			}
			height, err := cmd.Flags().GetUint64(flagEvalHeight)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).DomainStatus(cmd.Context(), &types.QueryDomainStatusRequest{Name: name, Height: height})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	cmd.Flags().String(flagName, "", "name (FQDN) to look up")
	// eval-height is the request's own Height field (which height to evaluate
	// expiry at); it is deliberately NOT named --height, since
	// flags.AddQueryFlagsToCmd below already registers a standard --height
	// flag for the unrelated concept of which ABCI query height to run the
	// RPC against, and colliding names panics with "flag redefined".
	cmd.Flags().Uint64(flagEvalHeight, 0, "block height to evaluate expiry at (0 = current)")
	_ = cmd.MarkFlagRequired(flagName)
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewNameRecordCmd queries the raw stored record for a name.
func NewNameRecordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"name-record",
		Short:	"Query the stored record for a name",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			name, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).NameRecord(cmd.Context(), &types.QueryNameRecordRequest{Name: name})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	cmd.Flags().String(flagName, "", "name (FQDN) to look up")
	_ = cmd.MarkFlagRequired(flagName)
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewResolveNameCmd resolves a name to its resolver root as of an optional
// height, honoring expiry/activity.
func NewResolveNameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"resolve-name",
		Short:	"Resolve a name to its resolver root",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			name, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return err
			}
			height, err := cmd.Flags().GetUint64(flagEvalHeight)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).ResolveName(cmd.Context(), &types.QueryResolveNameRequest{Name: name, Height: height})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	cmd.Flags().String(flagName, "", "name (FQDN) to resolve")
	// eval-height: see NewDomainStatusCmd's comment on flagEvalHeight for why
	// this is not named --height.
	cmd.Flags().Uint64(flagEvalHeight, 0, "block height to evaluate activity at (0 = current)")
	_ = cmd.MarkFlagRequired(flagName)
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewReverseRecordCmd queries the reverse record for an address.
func NewReverseRecordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"reverse-record",
		Short:	"Query the reverse record for an address",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			address, err := cmd.Flags().GetString(flagAddress)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).ReverseRecord(cmd.Context(), &types.QueryReverseRecordRequest{Address: address})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	cmd.Flags().String(flagAddress, "", "address to look up")
	_ = cmd.MarkFlagRequired(flagAddress)
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewSubdomainsCmd lists every subdomain registered under a parent name.
func NewSubdomainsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"subdomains",
		Short:	"List every subdomain under a parent name",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			parentName, err := cmd.Flags().GetString(flagParentName)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).Subdomains(cmd.Context(), &types.QuerySubdomainsRequest{ParentName: parentName})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	cmd.Flags().String(flagParentName, "", "parent name (FQDN) to list subdomains under")
	_ = cmd.MarkFlagRequired(flagParentName)
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewNameZoneCmd queries the AEZ zone/bucket a name's canonical entity id
// resolves to.
func NewNameZoneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"name-zone",
		Short:	"Query the AEZ zone and bucket a name resolves to",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			name, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).NameZone(cmd.Context(), &types.QueryNameZoneRequest{Name: name})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	cmd.Flags().String(flagName, "", "name (FQDN) to resolve a zone/bucket for; need not be registered")
	_ = cmd.MarkFlagRequired(flagName)
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

// NewListingCmd queries whether a name is currently listed for a fixed-price
// sale, and at what price.
func NewListingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"listing",
		Short:	"Query the fixed-price sale listing for a name",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			name, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return err
			}
			res, err := types.NewQueryClient(clientCtx).Listing(cmd.Context(), &types.QueryListingRequest{Name: name})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	cmd.Flags().String(flagName, "", "name (FQDN) to look up")
	_ = cmd.MarkFlagRequired(flagName)
	flags.AddQueryFlagsToCmd(cmd)
	return cmd
}

const flagEvalHeight = "eval-height"
