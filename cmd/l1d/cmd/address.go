package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
)

const (
	flagAddressBookFile       = "book-file"
	flagAddressChainID        = "chain-id"
	flagAddressCurrentHeight  = "current-height"
	flagAddressRecentWindow   = "recent-window"
	flagAddressLabelSource    = "source"
	flagAddressAttestation    = "attestation"
	flagAddressCreatedHeight  = "created-height"
	flagAddressLastSeenHeight = "last-seen-height"
)

func NewAddressCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "address",
		Short: "Address utilities",
	}
	cmd.AddCommand(NewAddressConvertCmd(), NewAddressInspectCmd(), NewAddressBookCmd())
	return cmd
}

func NewAddressConvertCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "convert [address]",
		Short: "Convert an address to Aetra raw and userfriendly forms",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bz, err := aetraaddress.Parse(args[0])
			if err != nil {
				return err
			}
			raw := aetraaddress.Format(bz)
			userFriendly, err := aetraaddress.FormatUserFriendly(bz)
			if err != nil {
				return err
			}
			out := struct {
				Raw          string `json:"raw"`
				UserFriendly string `json:"user_friendly"`
			}{
				Raw:          raw,
				UserFriendly: userFriendly,
			}
			bzJSON, err := json.MarshalIndent(out, "", "  ")
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(bzJSON))
			return err
		},
	}
}

func NewAddressInspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect [address]",
		Short: "Inspect address display, checksum, verified label, and impersonation warnings",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := addressInspectionContextFromCmd(cmd)
			if err != nil {
				return err
			}
			display, err := aetraaddress.InspectAddress(args[0], ctx)
			if err != nil {
				return err
			}
			return writeCommandJSON(cmd, display)
		},
	}
	addAddressInspectFlags(cmd)
	return cmd
}

func NewAddressBookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "book",
		Short: "Local address book helpers",
	}
	cmd.AddCommand(newAddressBookAddCmd())
	return cmd
}

func newAddressBookAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [label] [address]",
		Short: "Build a local address book entry",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			source, _ := cmd.Flags().GetString(flagAddressLabelSource)
			attestation, _ := cmd.Flags().GetString(flagAddressAttestation)
			createdHeight, _ := cmd.Flags().GetUint64(flagAddressCreatedHeight)
			lastSeenHeight, _ := cmd.Flags().GetUint64(flagAddressLastSeenHeight)
			entry, err := aetraaddress.NewAddressBookEntry(args[0], args[1], source, attestation, createdHeight, lastSeenHeight)
			if err != nil {
				return err
			}
			return writeCommandJSON(cmd, entry)
		},
	}
	cmd.Flags().String(flagAddressLabelSource, aetraaddress.LabelSourceLocal, "label source: local, on_chain, or signed_attestation")
	cmd.Flags().String(flagAddressAttestation, "", "signed label attestation envelope or hash")
	cmd.Flags().Uint64(flagAddressCreatedHeight, 0, "known account creation height")
	cmd.Flags().Uint64(flagAddressLastSeenHeight, 0, "last height where the address was observed")
	return cmd
}

func addAddressInspectFlags(cmd *cobra.Command) {
	cmd.Flags().String(flagAddressChainID, "", "chain id for chain-bound checksum display")
	addAddressRiskFlags(cmd)
}

func addAddressRiskFlags(cmd *cobra.Command) {
	cmd.Flags().String(flagAddressBookFile, "", "local address book JSON file")
	cmd.Flags().Uint64(flagAddressCurrentHeight, 0, "current chain height for recent-address warnings")
	cmd.Flags().Uint64(flagAddressRecentWindow, aetraaddress.DefaultRecentAddressWindow, "recent-address warning window in blocks")
}

func addressInspectionContextFromCmd(cmd *cobra.Command) (aetraaddress.AddressInspectionContext, error) {
	chainID, _ := cmd.Flags().GetString(flagAddressChainID)
	bookFile, _ := cmd.Flags().GetString(flagAddressBookFile)
	currentHeight, _ := cmd.Flags().GetUint64(flagAddressCurrentHeight)
	recentWindow, _ := cmd.Flags().GetUint64(flagAddressRecentWindow)
	book, err := readAddressBookFile(bookFile)
	if err != nil {
		return aetraaddress.AddressInspectionContext{}, err
	}
	return aetraaddress.AddressInspectionContext{
		ChainID:               chainID,
		CurrentHeight:         currentHeight,
		RecentAddressWindow:   recentWindow,
		AddressBook:           book,
		IncludeSystemContacts: true,
	}, nil
}

func readAddressBookFile(fileName string) ([]aetraaddress.AddressBookEntry, error) {
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(fileName)
	if err != nil {
		return nil, err
	}
	var book aetraaddress.AddressBook
	if err := json.Unmarshal(raw, &book); err == nil {
		return book.Entries, nil
	}
	var entries []aetraaddress.AddressBookEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("address book must be a JSON object with entries or an array: %w", err)
	}
	return entries, nil
}
