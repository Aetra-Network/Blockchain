package cli

import (
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdktx "github.com/cosmos/cosmos-sdk/client/tx"

	"github.com/sovereign-l1/l1/x/identity-root/types"
)

// GetTxCmd wires every real x/identity-root Msg RPC (see types/tx.go's
// MsgServer interface) into a cobra command tree, mirroring x/fees's
// client/cli/tx.go structural conventions (Use=types.ModuleName,
// DisableFlagParsing on the parent, SuggestionsMinimumDistance, RunE:
// client.ValidateCmd) -- x/fees itself has no Msg service to demonstrate a
// leaf command, so every leaf below follows the universal cosmos-sdk tx CLI
// idiom instead: client.GetClientTxContext + flags.AddTxFlagsToCmd +
// sdktx.GenerateOrBroadcastTxCLI, exactly as bank/staking/gov etc. do.
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:				types.ModuleName,
		Short:				"Identity root (ANS) transactions",
		DisableFlagParsing:		true,
		SuggestionsMinimumDistance:	2,
		RunE:				client.ValidateCmd,
	}
	cmd.AddCommand(
		NewSendToNameCollectionCmd(),
		NewPlaceBidCmd(),
		NewStartAuctionCmd(),
		NewUpdatePriceTableCmd(),
		NewAttachDomainCmd(),
		NewDetachDomainCmd(),
		NewDisownAttachmentCmd(),
		NewCreateSubdomainCmd(),
		NewRenewNameCmd(),
		NewTransferNameCmd(),
		NewSetResolverCmd(),
		NewSetReverseRecordCmd(),
		NewReserveNameCmd(),
		NewReleaseReservedNameCmd(),
		NewListForSaleCmd(),
		NewDelistNameCmd(),
		NewBuyListedNameCmd(),
	)
	return cmd
}

// NewSendToNameCollectionCmd is the message-driven entry point to the .aet
// collection: --opcode 1 (TOPUP) moves --amount-naet into the collection
// module account; --opcode 2 (REGISTER) parses the label to register from
// --comment.
func NewSendToNameCollectionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"send-to-collection",
		Short:	"Send a TOPUP or REGISTER message to the .aet name collection",
		Long:	"Send a message-driven request to the name collection module account. --opcode 1 is TOPUP (escrows --amount-naet for a future REGISTER); --opcode 2 is REGISTER (parses the label to register from --comment, funded by --amount-naet).",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			opcode, err := cmd.Flags().GetUint32(flagOpcode)
			if err != nil {
				return err
			}
			comment, err := cmd.Flags().GetString(flagComment)
			if err != nil {
				return err
			}
			amountNaet, err := cmd.Flags().GetUint64(flagAmountNaet)
			if err != nil {
				return err
			}
			msg := &types.MsgSendToNameCollection{
				Sender:		clientCtx.GetFromAddress().String(),
				Opcode:		opcode,
				Comment:	comment,
				AmountNaet:	amountNaet,
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().Uint32(flagOpcode, 0, "collection message opcode: 1=TOPUP, 2=REGISTER")
	cmd.Flags().String(flagComment, "", "REGISTER label to claim (ignored for TOPUP)")
	cmd.Flags().Uint64(flagAmountNaet, 0, "amount to send, in naet (1 AET = 1e9 naet)")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewPlaceBidCmd escrows --amount-naet as a bid on the open auction for
// --name. The prior high bid, if any, is refunded automatically.
func NewPlaceBidCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"place-bid",
		Short:	"Place a bid on an open name auction",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			name, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return err
			}
			amountNaet, err := cmd.Flags().GetUint64(flagAmountNaet)
			if err != nil {
				return err
			}
			msg := &types.MsgPlaceBid{
				Bidder:		clientCtx.GetFromAddress().String(),
				Name:		name,
				AmountNaet:	amountNaet,
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagName, "", "name (FQDN) being auctioned")
	cmd.Flags().Uint64(flagAmountNaet, 0, "bid amount, in naet")
	_ = cmd.MarkFlagRequired(flagName)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewStartAuctionCmd lists a domain the caller owns for an owner-listed
// auction of --duration-days (7..365) at a custom --start-price-naet.
func NewStartAuctionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"start-auction",
		Short:	"List an owned name for an owner-listed auction",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			name, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return err
			}
			startPriceNaet, err := cmd.Flags().GetUint64(flagStartPriceNaet)
			if err != nil {
				return err
			}
			durationDays, err := cmd.Flags().GetUint32(flagDurationDays)
			if err != nil {
				return err
			}
			msg := &types.MsgStartAuction{
				Owner:		clientCtx.GetFromAddress().String(),
				Name:		name,
				StartPriceNaet:	startPriceNaet,
				DurationDays:	durationDays,
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagName, "", "owned name (FQDN) to list")
	cmd.Flags().Uint64(flagStartPriceNaet, 0, "opening/reserve price, in naet")
	cmd.Flags().Uint32(flagDurationDays, 0, "auction duration in days (7..365)")
	_ = cmd.MarkFlagRequired(flagName)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewUpdatePriceTableCmd replaces the governance-owned price table. The
// --min-label-lens and --prices-naet slices are parallel arrays: entry i of
// each is one price tier.
func NewUpdatePriceTableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"update-price-table",
		Short:	"Replace the governance-owned label-length price table",
		Long:	"Governance-gated: the tx signer (--from) must be the module's configured authority. --min-label-lens and --prices-naet are parallel arrays -- the i-th min-label-lens entry prices labels of at least that length at the i-th prices-naet entry.",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			minLabelLens, err := cmd.Flags().GetUintSlice(flagMinLabelLens)
			if err != nil {
				return err
			}
			pricesNaet, err := cmd.Flags().GetStringSlice(flagPricesNaet)
			if err != nil {
				return err
			}
			lens := make([]uint32, len(minLabelLens))
			for i, v := range minLabelLens {
				lens[i] = uint32(v)
			}
			msg := &types.MsgUpdatePriceTable{
				Authority:	clientCtx.GetFromAddress().String(),
				MinLabelLens:	lens,
				PricesNaet:	pricesNaet,
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().UintSlice(flagMinLabelLens, nil, "minimum label lengths for each price tier, comma-separated (parallel to --prices-naet)")
	cmd.Flags().StringSlice(flagPricesNaet, nil, "naet price for each tier, comma-separated (parallel to --min-label-lens)")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewAttachDomainCmd attaches an owned FQDN to --target, per the
// one-domain-per-wallet index.
func NewAttachDomainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"attach-domain",
		Short:	"Attach an owned FQDN to a target wallet",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			fqdn, err := cmd.Flags().GetString(flagFqdn)
			if err != nil {
				return err
			}
			target, err := cmd.Flags().GetString(flagTarget)
			if err != nil {
				return err
			}
			msg := &types.MsgAttachDomain{
				Owner:	clientCtx.GetFromAddress().String(),
				Fqdn:	fqdn,
				Target:	target,
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagFqdn, "", "owned FQDN to attach")
	cmd.Flags().String(flagTarget, "", "target wallet address the FQDN is attached to")
	_ = cmd.MarkFlagRequired(flagFqdn)
	_ = cmd.MarkFlagRequired(flagTarget)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewDetachDomainCmd clears the attachment for an owned FQDN.
func NewDetachDomainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"detach-domain",
		Short:	"Clear the attachment for an owned FQDN",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			fqdn, err := cmd.Flags().GetString(flagFqdn)
			if err != nil {
				return err
			}
			msg := &types.MsgDetachDomain{
				Owner:	clientCtx.GetFromAddress().String(),
				Fqdn:	fqdn,
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagFqdn, "", "owned FQDN to detach")
	_ = cmd.MarkFlagRequired(flagFqdn)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewDisownAttachmentCmd lets the tx signer, as the TARGET of an attachment,
// clear it without the FQDN owner's cooperation.
func NewDisownAttachmentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"disown-attachment",
		Short:	"Clear the domain attachment pointed at your own wallet",
		Long:	"Anti-griefing action: the tx signer (--from) is the TARGET of the attachment, not the FQDN owner. No owned-name check is performed -- the target need not own the FQDN.",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			msg := &types.MsgDisownAttachment{
				Target: clientCtx.GetFromAddress().String(),
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewCreateSubdomainCmd creates a subdomain of an owned --parent-name.
func NewCreateSubdomainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"create-subdomain",
		Short:	"Create a subdomain under an owned parent name",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			parentName, err := cmd.Flags().GetString(flagParentName)
			if err != nil {
				return err
			}
			label, err := cmd.Flags().GetString(flagLabel)
			if err != nil {
				return err
			}
			subdomainOwner, err := cmd.Flags().GetString(flagSubdomainOwner)
			if err != nil {
				return err
			}
			resolverRoot, err := cmd.Flags().GetString(flagResolverRoot)
			if err != nil {
				return err
			}
			subdomainPolicy, err := cmd.Flags().GetString(flagSubdomainPolicy)
			if err != nil {
				return err
			}
			msg := &types.MsgCreateSubdomain{
				Owner:			clientCtx.GetFromAddress().String(),
				ParentName:		parentName,
				Label:			label,
				SubdomainOwner:		subdomainOwner,
				ResolverRoot:		resolverRoot,
				SubdomainPolicy:	subdomainPolicy,
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagParentName, "", "owned parent name (FQDN)")
	cmd.Flags().String(flagLabel, "", "subdomain label to create under the parent")
	cmd.Flags().String(flagSubdomainOwner, "", "owner address for the new subdomain")
	cmd.Flags().String(flagResolverRoot, "", "resolver root for the new subdomain (optional)")
	cmd.Flags().String(flagSubdomainPolicy, "", "subdomain delegation policy for the new subdomain (optional)")
	_ = cmd.MarkFlagRequired(flagParentName)
	_ = cmd.MarkFlagRequired(flagLabel)
	_ = cmd.MarkFlagRequired(flagSubdomainOwner)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewRenewNameCmd extends the registration term of an owned --name.
func NewRenewNameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"renew-name",
		Short:	"Renew the registration term of an owned name",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			name, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return err
			}
			msg := &types.MsgRenewName{
				Owner:	clientCtx.GetFromAddress().String(),
				Name:	name,
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagName, "", "owned name (FQDN) to renew")
	_ = cmd.MarkFlagRequired(flagName)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewTransferNameCmd transfers ownership of an owned --name to --new-owner.
func NewTransferNameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"transfer-name",
		Short:	"Transfer ownership of an owned name",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			name, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return err
			}
			newOwner, err := cmd.Flags().GetString(flagNewOwner)
			if err != nil {
				return err
			}
			msg := &types.MsgTransferName{
				Owner:		clientCtx.GetFromAddress().String(),
				Name:		name,
				NewOwner:	newOwner,
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagName, "", "owned name (FQDN) to transfer")
	cmd.Flags().String(flagNewOwner, "", "recipient address")
	_ = cmd.MarkFlagRequired(flagName)
	_ = cmd.MarkFlagRequired(flagNewOwner)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewSetResolverCmd sets the resolver root for an owned --name.
func NewSetResolverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"set-resolver",
		Short:	"Set the resolver root for an owned name",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			name, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return err
			}
			resolverRoot, err := cmd.Flags().GetString(flagResolverRoot)
			if err != nil {
				return err
			}
			msg := &types.MsgSetResolver{
				Owner:		clientCtx.GetFromAddress().String(),
				Name:		name,
				ResolverRoot:	resolverRoot,
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagName, "", "owned name (FQDN)")
	cmd.Flags().String(flagResolverRoot, "", "new resolver root")
	_ = cmd.MarkFlagRequired(flagName)
	_ = cmd.MarkFlagRequired(flagResolverRoot)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewSetReverseRecordCmd points --address's reverse record at an owned
// --name.
func NewSetReverseRecordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"set-reverse-record",
		Short:	"Set the reverse record for an address to an owned name",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			address, err := cmd.Flags().GetString(flagAddress)
			if err != nil {
				return err
			}
			name, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return err
			}
			msg := &types.MsgSetReverseRecord{
				Owner:		clientCtx.GetFromAddress().String(),
				Address:	address,
				Name:		name,
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagAddress, "", "address the reverse record is set for")
	cmd.Flags().String(flagName, "", "owned name (FQDN) the address resolves back to")
	_ = cmd.MarkFlagRequired(flagAddress)
	_ = cmd.MarkFlagRequired(flagName)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewReserveNameCmd governance-reserves --name so it cannot be registered.
func NewReserveNameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"reserve-name",
		Short:	"Governance-reserve a name to block registration",
		Long:	"Governance-gated: the tx signer (--from) must be the module's configured authority.",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			name, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return err
			}
			reason, err := cmd.Flags().GetString(flagReason)
			if err != nil {
				return err
			}
			msg := &types.MsgReserveName{
				Authority:	clientCtx.GetFromAddress().String(),
				Name:		name,
				Reason:		reason,
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagName, "", "name (FQDN) to reserve")
	cmd.Flags().String(flagReason, "", "human-readable reservation reason")
	_ = cmd.MarkFlagRequired(flagName)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewReleaseReservedNameCmd releases a governance reservation on --name.
func NewReleaseReservedNameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"release-reserved-name",
		Short:	"Release a governance reservation on a name",
		Long:	"Governance-gated: the tx signer (--from) must be the module's configured authority.",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			name, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return err
			}
			msg := &types.MsgReleaseReservedName{
				Authority:	clientCtx.GetFromAddress().String(),
				Name:		name,
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagName, "", "reserved name (FQDN) to release")
	_ = cmd.MarkFlagRequired(flagName)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewListForSaleCmd lists an owned --name for sale at a fixed --price-naet.
// Mutually exclusive with an open auction on the same name (the keeper
// rejects one while the other is active).
func NewListForSaleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"list-for-sale",
		Short:	"List an owned name for sale at a fixed price",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			name, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return err
			}
			priceNaet, err := cmd.Flags().GetUint64(flagPriceNaet)
			if err != nil {
				return err
			}
			msg := &types.MsgListForSale{
				Owner:		clientCtx.GetFromAddress().String(),
				Name:		name,
				PriceNaet:	priceNaet,
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagName, "", "owned name (FQDN) to list for sale")
	cmd.Flags().Uint64(flagPriceNaet, 0, "fixed sale price, in naet")
	_ = cmd.MarkFlagRequired(flagName)
	_ = cmd.MarkFlagRequired(flagPriceNaet)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewDelistNameCmd clears an owned --name's fixed-price listing.
func NewDelistNameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"delist-name",
		Short:	"Clear the fixed-price listing on an owned name",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			name, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return err
			}
			msg := &types.MsgDelistName{
				Owner:	clientCtx.GetFromAddress().String(),
				Name:	name,
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagName, "", "listed name (FQDN) to delist")
	_ = cmd.MarkFlagRequired(flagName)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewBuyListedNameCmd buys --name at its live, on-chain listing price. There
// is no amount flag -- the buyer always pays the current listing price, not
// an offer.
func NewBuyListedNameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"buy-listed-name",
		Short:	"Buy a name at its listed fixed price",
		Long:	"Buys --name at its current on-chain listing price. There is no amount flag: the buyer always pays the live listing price, never an offer.",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			name, err := cmd.Flags().GetString(flagName)
			if err != nil {
				return err
			}
			msg := &types.MsgBuyListedName{
				Buyer:	clientCtx.GetFromAddress().String(),
				Name:	name,
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	cmd.Flags().String(flagName, "", "listed name (FQDN) to buy")
	_ = cmd.MarkFlagRequired(flagName)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

const (
	flagOpcode		= "opcode"
	flagComment		= "comment"
	flagAmountNaet		= "amount-naet"
	flagName		= "name"
	flagStartPriceNaet	= "start-price-naet"
	flagDurationDays	= "duration-days"
	flagMinLabelLens	= "min-label-lens"
	flagPricesNaet		= "prices-naet"
	flagFqdn		= "fqdn"
	flagTarget		= "target"
	flagParentName		= "parent-name"
	flagLabel		= "label"
	flagSubdomainOwner	= "subdomain-owner"
	flagResolverRoot	= "resolver-root"
	flagSubdomainPolicy	= "subdomain-policy"
	flagNewOwner		= "new-owner"
	flagAddress		= "address"
	flagReason		= "reason"
	flagPriceNaet		= "price-naet"
)
