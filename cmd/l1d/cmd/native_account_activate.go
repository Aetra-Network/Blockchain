package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"

	nativeaccounttypes "github.com/sovereign-l1/l1/x/native-account/types"
)

// newNativeAccountActivateCmd replaces the generic dry-run-only
// "system native-account activate-account" leaf (see systemModuleSpecs in
// operator.go) with a command that can actually sign and broadcast
// MsgActivateAccount: every AVM contract keeper entrypoint (StoreCode,
// DeployContract, ExecuteExternal, ...) requires the caller's native-account
// record to already be active (see x/contracts/keeper/keeper.go's
// ensureActiveWallet), and genesis starts with zero activated accounts, so
// without this an operator has no working path to ever deploy a contract.
func newNativeAccountActivateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "activate-account",
		Short: "Sign and broadcast l1.nativeaccount.v1.Msg/ActivateAccount for --from's key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			if clientCtx.FromName == "" {
				return errors.New("activate-account requires --from naming a keyring account")
			}
			record, err := clientCtx.Keyring.Key(clientCtx.FromName)
			if err != nil {
				return fmt.Errorf("look up keyring key %q: %w", clientCtx.FromName, err)
			}
			pubKey, err := record.GetPubKey()
			if err != nil {
				return fmt.Errorf("read public key for %q: %w", clientCtx.FromName, err)
			}
			msg, err := nativeaccounttypes.NewMsgActivateAccountFromPubKey(pubKey, 0)
			if err != nil {
				return fmt.Errorf("build activation message: %w", err)
			}
			res, err := signAndBroadcast(cmd.Context(), clientCtx, cmd.Flags(), &msg)
			if err != nil && res == nil {
				return err
			}
			if writeErr := writeCommandJSON(cmd, avmBroadcastResult{TxHash: res.TxHash, Code: res.Code, RawLog: res.RawLog}); writeErr != nil {
				return writeErr
			}
			return err
		},
	}
	addAVMTxFlags(cmd)
	return cmd
}
