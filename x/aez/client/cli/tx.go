package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdktx "github.com/cosmos/cosmos-sdk/client/tx"

	"github.com/sovereign-l1/l1/x/aez/types"
)

// GetTxCmd wires the aez transaction subcommands, mirroring the parent-command
// conventions used by x/fees/client/cli (DisableFlagParsing + ValidateCmd on
// the group node, real flag parsing on each leaf).
func GetTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:				types.ModuleName,
		Short:				"AEZ transactions",
		DisableFlagParsing:		true,
		SuggestionsMinimumDistance:	2,
		RunE:				client.ValidateCmd,
	}
	cmd.AddCommand(NewUpdateRoutingTableCmd())
	return cmd
}

const (
	flagRoutingTableFile	= "routing-table-file"
	flagAuthority		= "authority"
)

// routingTableFile is the on-disk shape --routing-table-file reads.
//
// It carries the message's FULL 256-bucket body. MsgUpdateRoutingTable never
// accepts a delta (x/aez/types/tx.go's own doc comment: "The whole map
// travels, never a delta ... so the proposal bytes ARE the resulting
// layout"), and 256 individual flags would be unusable, so the table body is
// a JSON file instead. See NewUpdateRoutingTableCmd's Long text for the exact
// schema and a worked example of producing one.
type routingTableFile struct {
	Version			uint64		`json:"version"`
	Epoch			uint64		`json:"epoch"`
	ActivationHeight	int64		`json:"activation_height"`
	Buckets			[]uint32	`json:"buckets"`
}

func loadRoutingTableFile(path string) (routingTableFile, error) {
	var body routingTableFile
	raw, err := os.ReadFile(path)
	if err != nil {
		return body, fmt.Errorf("read --%s: %w", flagRoutingTableFile, err)
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return body, fmt.Errorf("parse --%s: %w", flagRoutingTableFile, err)
	}
	if len(body.Buckets) != types.BucketCount {
		return body, fmt.Errorf("--%s must carry exactly %d buckets (index-ordered, one zone id each), got %d", flagRoutingTableFile, types.BucketCount, len(body.Buckets))
	}
	return body, nil
}

// NewUpdateRoutingTableCmd builds the sole aez Msg: MsgUpdateRoutingTable.
func NewUpdateRoutingTableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"update-routing-table",
		Short:	"Stage a new AEZ bucket->zone routing table for a future routing-epoch boundary",
		Long: `Stage a new AEZ bucket->zone routing table (MsgUpdateRoutingTable).

This does NOT apply the table immediately. The swap happens automatically,
inside the BeginBlocker, at the table's activation_height -- which must be an
exact routing-epoch boundary, strictly in the future.

MsgUpdateRoutingTable is governance-gated: the message's authority field must
equal the chain's current aez Params.Prototype.Authority (the gov module
account by default), and that same address must sign the transaction --
SigVerificationDecorator checks the two independently. In practice this means
the usual way to use this command is with --generate-only, embedding the
resulting message JSON as one entry in a "tx gov submit-proposal" messages
array, rather than signing it directly with an arbitrary --from key.

Because the message carries the FULL 256-entry bucket->zone map (never a
delta -- every bucket must be listed, index 0 through 255, so the proposal's
bytes ARE the resulting layout), the table body is supplied as a JSON file via
--routing-table-file rather than one flag per bucket:

  {
    "version": 2,
    "epoch": 1,
    "activation_height": 20000,
    "buckets": [0, 0, 0, 1, 1, 2, 0, ...]
  }

"buckets" must have exactly 256 entries; buckets[i] is the zone id (0=Core,
1..4=elastic) that bucket i resolves to.

A convenient way to produce a starting file is to dump the CURRENT table and
edit it, e.g. with jq:

  l1d query aez routing-table --output json \
    | jq '{version: ((.version|tonumber)+1), epoch: (.epoch|tonumber), activation_height: 0, buckets: (.buckets | map(tonumber))}' \
    > table.json

then set activation_height to the next routing-epoch boundary and edit
whichever bucket entries should move zones.
`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			filePath, err := cmd.Flags().GetString(flagRoutingTableFile)
			if err != nil {
				return err
			}
			if filePath == "" {
				return fmt.Errorf("--%s is required", flagRoutingTableFile)
			}
			body, err := loadRoutingTableFile(filePath)
			if err != nil {
				return err
			}

			authority, err := cmd.Flags().GetString(flagAuthority)
			if err != nil {
				return err
			}

			msg := &types.MsgUpdateRoutingTable{
				Authority:		authority,
				Version:		body.Version,
				Epoch:			body.Epoch,
				ActivationHeight:	body.ActivationHeight,
				Buckets:		body.Buckets,
			}
			return sdktx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
		},
	}
	flags.AddTxFlagsToCmd(cmd)
	cmd.Flags().String(flagRoutingTableFile, "", "path to a JSON file with {version, epoch, activation_height, buckets[256]} (required, see command help)")
	cmd.Flags().String(flagAuthority, types.GovAuthority(), "authority address carried by the message; must equal the chain's current aez Params.Prototype.Authority and match the tx signer")
	if err := cmd.MarkFlagRequired(flagRoutingTableFile); err != nil {
		panic(err)
	}
	return cmd
}
