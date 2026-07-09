package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/cosmos/cosmos-sdk/client"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// signAndBroadcast builds, signs, and broadcasts msgs using the same
// Factory.Prepare/BuildUnsignedTx/Sign/BroadcastTx pipeline the standard
// cosmos-sdk CLI tx commands use (client/tx.GenerateOrBroadcastTxWithFactory),
// stopping short of that helper's final clientCtx.PrintProto so the caller
// gets the raw *sdk.TxResponse (tx hash, result code, raw log) back directly
// instead of it only being printed to stdout. Shared by the faucet service
// and any CLI command's opt-in --broadcast path (see addBroadcastFlag).
func signAndBroadcast(ctx context.Context, clientCtx client.Context, flagSet *pflag.FlagSet, msgs ...sdk.Msg) (*sdk.TxResponse, error) {
	clientCtx = clientCtx.WithCmdContext(ctx)

	txf, err := clienttx.NewFactoryCLI(clientCtx, flagSet)
	if err != nil {
		return nil, err
	}
	txf, err = txf.Prepare(clientCtx)
	if err != nil {
		return nil, err
	}
	if txf.SimulateAndExecute() {
		_, adjusted, gasErr := clienttx.CalculateGas(clientCtx, txf, msgs...)
		if gasErr != nil {
			return nil, gasErr
		}
		txf = txf.WithGas(adjusted)
	}

	unsignedTx, err := txf.BuildUnsignedTx(msgs...)
	if err != nil {
		return nil, err
	}
	if err := clienttx.Sign(ctx, txf, clientCtx.FromName, unsignedTx, true); err != nil {
		return nil, err
	}
	txBytes, err := clientCtx.TxConfig.TxEncoder()(unsignedTx.GetTx())
	if err != nil {
		return nil, err
	}
	res, err := clientCtx.BroadcastTx(txBytes)
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return res, fmt.Errorf("tx rejected: code=%d raw_log=%s", res.Code, res.RawLog)
	}
	return res, nil
}

const flagBroadcast = "broadcast"

// addBroadcastFlag registers the opt-in --broadcast flag a "build a request
// plan" command can offer alongside its default dry-run JSON-plan output: set,
// it signs and sends the same message for real via signAndBroadcast instead of
// only printing what the equivalent RPC call would look like.
func addBroadcastFlag(cmd *cobra.Command) {
	cmd.Flags().Bool(flagBroadcast, false, "sign and broadcast this message for real instead of only printing the request plan")
}
