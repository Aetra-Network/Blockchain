package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"

	appparams "github.com/sovereign-l1/l1/app/params"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
	contractstypes "github.com/sovereign-l1/l1/x/contracts/types"
)

const (
	avmMsgService   = "l1.contracts.v1.Msg"
	avmQueryService = "l1.contracts.v1.Query"

	flagAVMAuthority      = "authority"
	flagAVMCreator        = "creator"
	flagAVMSender         = "sender"
	flagAVMBytecodeFile   = "bytecode-file"
	flagAVMBytecodeHex    = "bytecode-hex"
	flagAVMCodeHash       = "code-hash"
	flagAVMCodeBytes      = "code-bytes"
	flagAVMBodyJSON       = "body-json"
	flagAVMBodyFile       = "body-file"
	flagAVMBodyHex        = "body-hex"
	flagAVMSource         = "source"
	flagAVMMessage        = "message"
	flagAVMFields         = "fields"
	flagAVMInitialBalance = "initial-balance"
	flagAVMAdmin          = "admin"
	flagAVMHeight         = "height"
	flagAVMNamespace      = "namespace"
	flagAVMSalt           = "salt"
	flagAVMFunds          = "funds"
	flagAVMGasLimit       = "gas-limit"
	flagAVMKeyPrefixHex   = "key-prefix-hex"
	flagAVMLimit          = "limit"
	flagAVMOpcode         = "opcode"
	flagAVMQueryID        = "query-id"
	flagAVMReceiptJSON    = "receipt-json"
	flagAVMReceiptFile    = "receipt-file"
	flagAVMAmount         = "amount"

	flagAVMActor             = "actor"
	flagAVMNewCodeID         = "new-code-id"
	flagAVMMigrationHandler  = "migration-handler"
	flagAVMFromSchemaVersion = "from-schema-version"
	flagAVMToSchemaVersion   = "to-schema-version"
	flagAVMPayloadHex        = "payload-hex"
	flagAVMNewAdmin          = "new-admin"
)

const (
	avmMsgStoreCodeTypeURL       = "/l1.contracts.v1.MsgStoreCode"
	avmMsgDeployContractTypeURL  = "/l1.contracts.v1.MsgDeployContract"
	avmMsgExecuteExternalTypeURL = "/l1.contracts.v1.MsgExecuteExternal"

	// avmMsgDeployContractResponseTypeURL is the registered wire name for the
	// deploy Msg response (see the gogoproto.RegisterType((*InstantiateContractResponse)(nil),
	// "l1.contracts.v1.MsgDeployContractResponse") call in
	// x/contracts/types/service.go) -- used to pick MsgDeployContractResponse
	// back out of a confirmed tx's packed MsgResponses.
	avmMsgDeployContractResponseTypeURL = "/l1.contracts.v1.MsgDeployContractResponse"

	avmMsgTopUpContractTypeURL          = "/l1.contracts.v1.MsgTopUpContract"
	avmMsgPayContractStorageDebtTypeURL = "/l1.contracts.v1.MsgPayContractStorageDebt"
	avmMsgUnfreezeContractTypeURL       = "/l1.contracts.v1.MsgUnfreezeContract"

	avmMsgUpgradeContractCodeTypeURL     = "/l1.contracts.v1.MsgUpgradeContractCode"
	avmMsgMigrateContractStateTypeURL    = "/l1.contracts.v1.MsgMigrateContractState"
	avmMsgSetContractAdminTypeURL        = "/l1.contracts.v1.MsgSetContractAdmin"
	avmMsgDisableContractUpgradesTypeURL = "/l1.contracts.v1.MsgDisableContractUpgrades"
)

// avmTxConfirmPollInterval and avmTxConfirmMaxAttempts bound how long
// runAVMBroadcast waits for a synchronously-broadcast AVM tx to actually be
// included in a block before falling back to reporting the raw (CheckTx-only)
// result.
//
// This matters because in this SDK version --broadcast-mode only supports
// "sync" and "async" (BroadcastTxCommit was removed), and "sync" returns as
// soon as CheckTx admits the tx to the mempool -- see
// client.Context.BroadcastTxSync's own doc comment ("returns after CheckTx
// execution"). CheckTx itself never runs message handlers: baseapp's runMsgs
// skips execution entirely unless mode is execModeFinalize or
// execModeSimulate. So a freshly-broadcast res.Code==0 only means "fees,
// signature, and sequence checked out and the tx was admitted" -- it does NOT
// mean the AVM message executed successfully, and res.Data/res.Events are
// empty at that point because the msg handler (and any contract-not-found
// style rejection it returns) only runs once the tx is actually included in
// a block. This is exactly what made a live-testnet `tx avm execute` against
// a non-contract address look like code=0 "success" even though the keeper
// correctly rejects it (see RESULTS_V1-live-testnet-exercise.md section 3).
// Polling for the committed tx result turns that CheckTx-only code=0 into the
// real FinalizeBlock outcome.
const (
	avmTxConfirmPollInterval = 1 * time.Second
	avmTxConfirmMaxAttempts  = 20
)

type avmServicePayload struct {
	Service    string `json:"service"`
	Method     string `json:"method"`
	FullMethod string `json:"full_method"`
	TypeURL    string `json:"type_url,omitempty"`
	Request    any    `json:"request"`
}

type avmStoreCodeRequest struct {
	Authority string `json:"authority"`
	CodeHash  string `json:"code_hash,omitempty"`
	CodeBytes uint64 `json:"code_bytes,omitempty"`
	Bytecode  string `json:"bytecode_base64,omitempty"`
}

type avmDeployRequest struct {
	Creator        string `json:"creator"`
	CodeID         string `json:"code_id"`
	ChainID        string `json:"chain_id,omitempty"`
	Namespace      string `json:"namespace,omitempty"`
	Salt           string `json:"salt,omitempty"`
	InitPayload    string `json:"init_payload_base64,omitempty"`
	InitialBalance uint64 `json:"initial_balance,omitempty"`
	Admin          string `json:"admin,omitempty"`
	Height         uint64 `json:"height"`
}

type avmExecuteRequest struct {
	Sender          string `json:"sender"`
	ContractAddress string `json:"contract_address"`
	Payload         string `json:"payload_base64,omitempty"`
	Funds           uint64 `json:"funds,omitempty"`
	GasLimit        uint64 `json:"gas_limit"`
	Height          uint64 `json:"height"`
	Opcode          uint32 `json:"opcode,omitempty"`
}

type avmTopUpRequest struct {
	Sender          string `json:"sender"`
	ContractAddress string `json:"contract_address"`
	Amount          uint64 `json:"amount"`
	Height          uint64 `json:"height"`
}

type avmPayDebtRequest struct {
	Sender          string `json:"sender"`
	ContractAddress string `json:"contract_address"`
	Amount          uint64 `json:"amount"`
	Height          uint64 `json:"height"`
}

type avmUnfreezeRequest struct {
	Sender          string `json:"sender"`
	ContractAddress string `json:"contract_address"`
	Height          uint64 `json:"height"`
}

type avmUpgradeCodeRequest struct {
	Actor            string `json:"actor"`
	ContractAddress  string `json:"contract_address"`
	NewCodeID        string `json:"new_code_id"`
	MigrationHandler string `json:"migration_handler,omitempty"`
	Height           uint64 `json:"height"`
}

type avmMigrateStateRequest struct {
	Actor             string `json:"actor"`
	ContractAddress   string `json:"contract_address"`
	FromSchemaVersion uint64 `json:"from_schema_version"`
	ToSchemaVersion   uint64 `json:"to_schema_version"`
	MigrationHandler  string `json:"migration_handler"`
	Payload           string `json:"payload_hex,omitempty"`
	Height            uint64 `json:"height"`
}

type avmSetAdminRequest struct {
	Actor           string `json:"actor"`
	ContractAddress string `json:"contract_address"`
	NewAdmin        string `json:"new_admin"`
	Height          uint64 `json:"height"`
}

type avmDisableUpgradesRequest struct {
	Actor           string `json:"actor"`
	ContractAddress string `json:"contract_address"`
	Height          uint64 `json:"height"`
}

type avmQueryRequest struct {
	ContractAddress string `json:"contract_address,omitempty"`
	CodeID          string `json:"code_id,omitempty"`
	KeyPrefix       string `json:"key_prefix_base64,omitempty"`
	Limit           uint32 `json:"limit,omitempty"`
}

type avmDeployHint struct {
	ContractAddressUser string `json:"contract_address_user"`
	ContractAddressRaw  string `json:"contract_address_raw"`
}

type avmExecuteHint struct {
	ExitCode  uint32 `json:"exit_code"`
	ReceiptID string `json:"receipt_id"`
}

func NewAVMTxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "avm",
		Short: "AVM contract transaction helpers",
	}
	cmd.AddCommand(
		newAVMStoreCodeCmd(),
		newAVMDeployCmd(),
		newAVMExecuteCmd(),
		newAVMTopUpCmd(),
		newAVMPayDebtCmd(),
		newAVMUnfreezeCmd(),
		newAVMUpgradeCodeCmd(),
		newAVMMigrateStateCmd(),
		newAVMSetAdminCmd(),
		newAVMDisableUpgradesCmd(),
	)
	return cmd
}

func NewAVMQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "avm",
		Short: "AVM contract query helpers",
	}
	cmd.AddCommand(
		newAVMCodeQueryCmd(),
		newAVMContractQueryCmd(),
		newAVMContractsQueryCmd(),
		newAVMStorageQueryCmd(),
		newAVMReceiptsQueryCmd(),
	)
	return cmd
}

func NewAVMDebugCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "avm",
		Short: "AVM developer debug helpers",
	}
	cmd.AddCommand(
		newAVMEncodeMessageCmd(),
		newAVMDecodeReceiptCmd(),
	)
	return cmd
}

func newAVMStoreCodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store-code",
		Short: "Build l1.contracts.v1.Msg/StoreCode request",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateAVMTxFees(cmd); err != nil {
				return err
			}
			authority, err := avmAuthority(cmd)
			if err != nil {
				return err
			}
			bytecode, err := readOptionalBytes(cmd, flagAVMBytecodeFile, flagAVMBytecodeHex)
			if err != nil {
				return err
			}
			codeHash, _ := cmd.Flags().GetString(flagAVMCodeHash)
			codeBytes, _ := cmd.Flags().GetUint64(flagAVMCodeBytes)
			if len(bytecode) == 0 && (strings.TrimSpace(codeHash) == "" || codeBytes == 0) {
				return errors.New("store-code requires bytecode or code-hash plus code-bytes")
			}
			req := struct {
				Authority string `json:"authority"`
				CodeHash  string `json:"code_hash,omitempty"`
				CodeBytes uint64 `json:"code_bytes,omitempty"`
				Bytecode  []byte `json:"bytecode"`
			}{
				Authority: authority,
				CodeHash:  strings.TrimSpace(codeHash),
				CodeBytes: codeBytes,
				Bytecode:  append([]byte(nil), bytecode...),
			}
			if len(req.Bytecode) > 0 {
				req.CodeBytes = uint64(len(req.Bytecode))
				if req.CodeHash == "" {
					req.CodeHash = canonicalCodeHash(req.Bytecode)
				}
			}
			broadcast, _ := cmd.Flags().GetBool(flagBroadcast)
			if broadcast {
				return runAVMBroadcast(cmd, &contractstypes.MsgStoreCode{
					Authority: req.Authority,
					CodeHash:  req.CodeHash,
					CodeBytes: req.CodeBytes,
					Bytecode:  req.Bytecode,
				})
			}
			return writeCommandJSON(cmd, avmServicePayload{
				Service:    avmMsgService,
				Method:     "StoreCode",
				FullMethod: "/" + avmMsgService + "/StoreCode",
				TypeURL:    avmMsgStoreCodeTypeURL,
				Request: avmStoreCodeRequest{
					Authority: req.Authority,
					CodeHash:  req.CodeHash,
					CodeBytes: req.CodeBytes,
					Bytecode:  base64OrEmpty(req.Bytecode),
				},
			})
		},
	}
	addAVMTxFlags(cmd)
	addBroadcastFlag(cmd)
	cmd.Flags().String(flagAVMAuthority, "", "governance/system authority; defaults to --from")
	cmd.Flags().String(flagAVMBytecodeFile, "", "AVM bytecode file")
	cmd.Flags().String(flagAVMBytecodeHex, "", "hex-encoded AVM bytecode")
	cmd.Flags().String(flagAVMCodeHash, "", "known AVM code hash")
	cmd.Flags().Uint64(flagAVMCodeBytes, 0, "known AVM code size in bytes")
	return cmd
}

func newAVMDeployCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy [code-id]",
		Short: "Build l1.contracts.v1.Msg/DeployContract request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateAVMTxFees(cmd); err != nil {
				return err
			}
			creator, err := avmAddressOverride(cmd, flagAVMCreator, "deploy creator")
			if err != nil {
				return err
			}
			body, err := readBodyBytes(cmd)
			if err != nil {
				return err
			}
			broadcast, _ := cmd.Flags().GetBool(flagBroadcast)
			var clientCtx client.Context
			height, _ := cmd.Flags().GetUint64(flagAVMHeight)
			if broadcast {
				clientCtx, err = client.GetClientTxContext(cmd)
				if err != nil {
					return err
				}
				height, err = avmBroadcastHeight(cmd, clientCtx)
				if err != nil {
					return err
				}
			}
			chainID, _ := cmd.Flags().GetString(flags.FlagChainID)
			namespace, _ := cmd.Flags().GetString(flagAVMNamespace)
			salt, _ := cmd.Flags().GetString(flagAVMSalt)
			initialBalance, _ := cmd.Flags().GetUint64(flagAVMInitialBalance)
			admin, _ := cmd.Flags().GetString(flagAVMAdmin)
			req := avmDeployRequest{
				Creator:        creator,
				CodeID:         strings.TrimSpace(args[0]),
				ChainID:        strings.TrimSpace(chainID),
				Namespace:      strings.TrimSpace(namespace),
				Salt:           strings.TrimSpace(salt),
				InitPayload:    base64OrEmpty(body),
				InitialBalance: initialBalance,
				Admin:          strings.TrimSpace(admin),
				Height:         height,
			}
			if req.CodeID == "" {
				return errors.New("deploy code id is required")
			}
			if req.Height == 0 {
				return errors.New("deploy height must be positive")
			}
			if broadcast {
				return runAVMBroadcast(cmd, &contractstypes.MsgDeployContract{
					Creator:        req.Creator,
					CodeID:         req.CodeID,
					ChainID:        req.ChainID,
					Namespace:      req.Namespace,
					Salt:           req.Salt,
					InitPayload:    body,
					InitialBalance: req.InitialBalance,
					Admin:          req.Admin,
					Height:         req.Height,
				})
			}
			return writeCommandJSON(cmd, struct {
				avmServicePayload
				ContractAddressUser string        `json:"contract_address_user"`
				ContractAddressRaw  string        `json:"contract_address_raw"`
				Expected            avmDeployHint `json:"expected_response_fields"`
			}{
				avmServicePayload: avmServicePayload{
					Service:    avmMsgService,
					Method:     "DeployContract",
					FullMethod: "/" + avmMsgService + "/DeployContract",
					TypeURL:    avmMsgDeployContractTypeURL,
					Request:    req,
				},
				ContractAddressUser: "AE...",
				ContractAddressRaw:  "4:...",
				Expected: avmDeployHint{
					ContractAddressUser: "AE...",
					ContractAddressRaw:  "4:...",
				},
			})
		},
	}
	addAVMTxFlags(cmd)
	addBroadcastFlag(cmd)
	addAVMBodyFlags(cmd)
	cmd.Flags().String(flagAVMCreator, "", "contract creator AE address; defaults to --from (only usable if --from is itself a valid AE address)")
	cmd.Flags().String(flagAVMNamespace, "", "contract namespace")
	cmd.Flags().String(flagAVMSalt, "", "contract salt")
	cmd.Flags().Uint64(flagAVMInitialBalance, 0, "initial native balance in naet")
	cmd.Flags().String(flagAVMAdmin, "", "contract admin AE address")
	cmd.Flags().Uint64(flagAVMHeight, 1, "execution height (with --broadcast, defaults to the chain's current height when not set explicitly)")
	return cmd
}

func newAVMExecuteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "execute [contract-address]",
		Short: "Build l1.contracts.v1.Msg/ExecuteExternal request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateAVMTxFees(cmd); err != nil {
				return err
			}
			sender, err := avmAddressOverride(cmd, flagAVMSender, "execute sender")
			if err != nil {
				return err
			}
			body, abiOpcode, abiOpcodeKnown, err := resolveAVMBody(cmd)
			if err != nil {
				return err
			}
			// Route a union-typed external message: prefer the compiled
			// opcode from --source/--message; else an explicit --opcode; else
			// 0 (valid for a single-variant external union).
			opcode, _ := cmd.Flags().GetUint32(flagAVMOpcode)
			if abiOpcodeKnown {
				if cmd.Flags().Changed(flagAVMOpcode) && opcode != abiOpcode {
					return fmt.Errorf("--opcode %d conflicts with the compiled opcode %d for --message", opcode, abiOpcode)
				}
				opcode = abiOpcode
			}
			broadcast, _ := cmd.Flags().GetBool(flagBroadcast)
			var clientCtx client.Context
			height, _ := cmd.Flags().GetUint64(flagAVMHeight)
			if broadcast {
				clientCtx, err = client.GetClientTxContext(cmd)
				if err != nil {
					return err
				}
				height, err = avmBroadcastHeight(cmd, clientCtx)
				if err != nil {
					return err
				}
			}
			funds, _ := cmd.Flags().GetUint64(flagAVMFunds)
			gasLimit, _ := cmd.Flags().GetUint64(flagAVMGasLimit)
			req := avmExecuteRequest{
				Sender:          sender,
				ContractAddress: strings.TrimSpace(args[0]),
				Payload:         base64OrEmpty(body),
				Funds:           funds,
				GasLimit:        gasLimit,
				Height:          height,
				Opcode:          opcode,
			}
			if req.ContractAddress == "" {
				return errors.New("execute contract address is required")
			}
			if req.GasLimit == 0 {
				return errors.New("execute gas limit must be positive")
			}
			if req.Height == 0 {
				return errors.New("execute height must be positive")
			}
			if broadcast {
				return runAVMBroadcast(cmd, &contractstypes.MsgExecuteExternal{
					Sender:          req.Sender,
					ContractAddress: req.ContractAddress,
					Payload:         body,
					Funds:           req.Funds,
					GasLimit:        req.GasLimit,
					Height:          req.Height,
					Opcode:          req.Opcode,
				})
			}
			return writeCommandJSON(cmd, struct {
				avmServicePayload
				ExitCode  uint32         `json:"exit_code"`
				ReceiptID string         `json:"receipt_id"`
				Expected  avmExecuteHint `json:"expected_response_fields"`
			}{
				avmServicePayload: avmServicePayload{
					Service:    avmMsgService,
					Method:     "ExecuteExternal",
					FullMethod: "/" + avmMsgService + "/ExecuteExternal",
					TypeURL:    avmMsgExecuteExternalTypeURL,
					Request:    req,
				},
				ExitCode:  0,
				ReceiptID: "receipt_id",
				Expected: avmExecuteHint{
					ExitCode:  0,
					ReceiptID: "receipt_id",
				},
			})
		},
	}
	addAVMTxFlags(cmd)
	addBroadcastFlag(cmd)
	addAVMBodyFlags(cmd)
	cmd.Flags().String(flagAVMSender, "", "execute sender AE address; defaults to --from (only usable if --from is itself a valid AE address)")
	cmd.Flags().Uint64(flagAVMFunds, 0, "native funds in naet")
	cmd.Flags().Uint64(flagAVMGasLimit, 100_000, "AVM gas limit")
	cmd.Flags().Uint64(flagAVMHeight, 1, "execution height (with --broadcast, defaults to the chain's current height when not set explicitly)")
	cmd.Flags().Uint32(flagAVMOpcode, 0, "external message @message opcode to route a union-typed incomingExternal; auto-filled from --source/--message")
	return cmd
}

func newAVMTopUpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "top-up [contract-address]",
		Short: "Build l1.contracts.v1.Msg/TopUpContract request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateAVMTxFees(cmd); err != nil {
				return err
			}
			sender, err := avmAddressOverride(cmd, flagAVMSender, "top-up sender")
			if err != nil {
				return err
			}
			broadcast, _ := cmd.Flags().GetBool(flagBroadcast)
			var clientCtx client.Context
			height, _ := cmd.Flags().GetUint64(flagAVMHeight)
			if broadcast {
				clientCtx, err = client.GetClientTxContext(cmd)
				if err != nil {
					return err
				}
				height, err = avmBroadcastHeight(cmd, clientCtx)
				if err != nil {
					return err
				}
			}
			amount, _ := cmd.Flags().GetUint64(flagAVMAmount)
			req := avmTopUpRequest{
				Sender:          sender,
				ContractAddress: strings.TrimSpace(args[0]),
				Amount:          amount,
				Height:          height,
			}
			if req.ContractAddress == "" {
				return errors.New("top-up contract address is required")
			}
			if req.Amount == 0 {
				return errors.New("top-up amount must be positive")
			}
			if req.Height == 0 {
				return errors.New("top-up height must be positive")
			}
			if broadcast {
				return runAVMBroadcast(cmd, &contractstypes.MsgTopUpContract{
					Sender:          req.Sender,
					ContractAddress: req.ContractAddress,
					Amount:          req.Amount,
					Height:          req.Height,
				})
			}
			return writeCommandJSON(cmd, avmServicePayload{
				Service:    avmMsgService,
				Method:     "TopUpContract",
				FullMethod: "/" + avmMsgService + "/TopUpContract",
				TypeURL:    avmMsgTopUpContractTypeURL,
				Request:    req,
			})
		},
	}
	addAVMTxFlags(cmd)
	addBroadcastFlag(cmd)
	cmd.Flags().String(flagAVMSender, "", "top-up sender AE address; defaults to --from (only usable if --from is itself a valid AE address)")
	cmd.Flags().Uint64(flagAVMAmount, 0, "amount in naet to add to the contract's balance")
	cmd.Flags().Uint64(flagAVMHeight, 1, "execution height (with --broadcast, defaults to the chain's current height when not set explicitly)")
	return cmd
}

func newAVMPayDebtCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pay-debt [contract-address]",
		Short: "Build l1.contracts.v1.Msg/PayContractStorageDebt request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateAVMTxFees(cmd); err != nil {
				return err
			}
			sender, err := avmAddressOverride(cmd, flagAVMSender, "storage debt payer")
			if err != nil {
				return err
			}
			broadcast, _ := cmd.Flags().GetBool(flagBroadcast)
			var clientCtx client.Context
			height, _ := cmd.Flags().GetUint64(flagAVMHeight)
			if broadcast {
				clientCtx, err = client.GetClientTxContext(cmd)
				if err != nil {
					return err
				}
				height, err = avmBroadcastHeight(cmd, clientCtx)
				if err != nil {
					return err
				}
			}
			amount, _ := cmd.Flags().GetUint64(flagAVMAmount)
			req := avmPayDebtRequest{
				Sender:          sender,
				ContractAddress: strings.TrimSpace(args[0]),
				Amount:          amount,
				Height:          height,
			}
			if req.ContractAddress == "" {
				return errors.New("pay-debt contract address is required")
			}
			if req.Amount == 0 {
				return errors.New("pay-debt amount must be positive")
			}
			if req.Height == 0 {
				return errors.New("pay-debt height must be positive")
			}
			if broadcast {
				return runAVMBroadcast(cmd, &contractstypes.MsgPayContractStorageDebt{
					Sender:          req.Sender,
					ContractAddress: req.ContractAddress,
					Amount:          req.Amount,
					Height:          req.Height,
				})
			}
			return writeCommandJSON(cmd, avmServicePayload{
				Service:    avmMsgService,
				Method:     "PayContractStorageDebt",
				FullMethod: "/" + avmMsgService + "/PayContractStorageDebt",
				TypeURL:    avmMsgPayContractStorageDebtTypeURL,
				Request:    req,
			})
		},
	}
	addAVMTxFlags(cmd)
	addBroadcastFlag(cmd)
	cmd.Flags().String(flagAVMSender, "", "storage debt payer AE address; defaults to --from (only usable if --from is itself a valid AE address)")
	cmd.Flags().Uint64(flagAVMAmount, 0, "amount in naet to pay against the contract's storage rent debt")
	cmd.Flags().Uint64(flagAVMHeight, 1, "execution height (with --broadcast, defaults to the chain's current height when not set explicitly)")
	return cmd
}

func newAVMUnfreezeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unfreeze [contract-address]",
		Short: "Build l1.contracts.v1.Msg/UnfreezeContract request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateAVMTxFees(cmd); err != nil {
				return err
			}
			sender, err := avmAddressOverride(cmd, flagAVMSender, "unfreeze sender")
			if err != nil {
				return err
			}
			broadcast, _ := cmd.Flags().GetBool(flagBroadcast)
			var clientCtx client.Context
			height, _ := cmd.Flags().GetUint64(flagAVMHeight)
			if broadcast {
				clientCtx, err = client.GetClientTxContext(cmd)
				if err != nil {
					return err
				}
				height, err = avmBroadcastHeight(cmd, clientCtx)
				if err != nil {
					return err
				}
			}
			req := avmUnfreezeRequest{
				Sender:          sender,
				ContractAddress: strings.TrimSpace(args[0]),
				Height:          height,
			}
			if req.ContractAddress == "" {
				return errors.New("unfreeze contract address is required")
			}
			if req.Height == 0 {
				return errors.New("unfreeze height must be positive")
			}
			if broadcast {
				return runAVMBroadcast(cmd, &contractstypes.MsgUnfreezeContract{
					Sender:          req.Sender,
					ContractAddress: req.ContractAddress,
					Height:          req.Height,
				})
			}
			return writeCommandJSON(cmd, avmServicePayload{
				Service:    avmMsgService,
				Method:     "UnfreezeContract",
				FullMethod: "/" + avmMsgService + "/UnfreezeContract",
				TypeURL:    avmMsgUnfreezeContractTypeURL,
				Request:    req,
			})
		},
	}
	addAVMTxFlags(cmd)
	addBroadcastFlag(cmd)
	cmd.Flags().String(flagAVMSender, "", "unfreeze sender AE address; defaults to --from (only usable if --from is itself a valid AE address)")
	cmd.Flags().Uint64(flagAVMHeight, 1, "execution height (with --broadcast, defaults to the chain's current height when not set explicitly)")
	return cmd
}

func newAVMUpgradeCodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade-code [contract-address]",
		Short: "Build l1.contracts.v1.Msg/UpgradeContractCode request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateAVMTxFees(cmd); err != nil {
				return err
			}
			actor, err := avmAddressOverride(cmd, flagAVMActor, "upgrade actor")
			if err != nil {
				return err
			}
			broadcast, _ := cmd.Flags().GetBool(flagBroadcast)
			var clientCtx client.Context
			height, _ := cmd.Flags().GetUint64(flagAVMHeight)
			if broadcast {
				clientCtx, err = client.GetClientTxContext(cmd)
				if err != nil {
					return err
				}
				height, err = avmBroadcastHeight(cmd, clientCtx)
				if err != nil {
					return err
				}
			}
			newCodeID, _ := cmd.Flags().GetString(flagAVMNewCodeID)
			migrationHandler, _ := cmd.Flags().GetString(flagAVMMigrationHandler)
			req := avmUpgradeCodeRequest{
				Actor:            actor,
				ContractAddress:  strings.TrimSpace(args[0]),
				NewCodeID:        strings.TrimSpace(newCodeID),
				MigrationHandler: strings.TrimSpace(migrationHandler),
				Height:           height,
			}
			if req.ContractAddress == "" {
				return errors.New("upgrade-code contract address is required")
			}
			if req.NewCodeID == "" {
				return errors.New("upgrade-code new code id is required")
			}
			if req.Height == 0 {
				return errors.New("upgrade-code height must be positive")
			}
			if broadcast {
				return runAVMBroadcast(cmd, &contractstypes.MsgUpgradeContractCode{
					Actor:            req.Actor,
					ContractAddress:  req.ContractAddress,
					NewCodeID:        req.NewCodeID,
					MigrationHandler: req.MigrationHandler,
					Height:           req.Height,
				})
			}
			return writeCommandJSON(cmd, avmServicePayload{
				Service:    avmMsgService,
				Method:     "UpgradeContractCode",
				FullMethod: "/" + avmMsgService + "/UpgradeContractCode",
				TypeURL:    avmMsgUpgradeContractCodeTypeURL,
				Request:    req,
			})
		},
	}
	addAVMTxFlags(cmd)
	addBroadcastFlag(cmd)
	cmd.Flags().String(flagAVMActor, "", "upgrade actor AE address (must be the contract's admin, or the governance authority for a system-owned contract); defaults to --from")
	cmd.Flags().String(flagAVMNewCodeID, "", "code id of the previously stored code to upgrade to")
	cmd.Flags().String(flagAVMMigrationHandler, "", "migration handler name; required if the new code's hash differs from the contract's current code hash")
	cmd.Flags().Uint64(flagAVMHeight, 1, "execution height (with --broadcast, defaults to the chain's current height when not set explicitly)")
	return cmd
}

func newAVMMigrateStateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate-state [contract-address]",
		Short: "Build l1.contracts.v1.Msg/MigrateContractState request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateAVMTxFees(cmd); err != nil {
				return err
			}
			actor, err := avmAddressOverride(cmd, flagAVMActor, "migration actor")
			if err != nil {
				return err
			}
			broadcast, _ := cmd.Flags().GetBool(flagBroadcast)
			var clientCtx client.Context
			height, _ := cmd.Flags().GetUint64(flagAVMHeight)
			if broadcast {
				clientCtx, err = client.GetClientTxContext(cmd)
				if err != nil {
					return err
				}
				height, err = avmBroadcastHeight(cmd, clientCtx)
				if err != nil {
					return err
				}
			}
			fromVersion, _ := cmd.Flags().GetUint64(flagAVMFromSchemaVersion)
			toVersion, _ := cmd.Flags().GetUint64(flagAVMToSchemaVersion)
			migrationHandler, _ := cmd.Flags().GetString(flagAVMMigrationHandler)
			payload, err := optionalHexFlag(cmd, flagAVMPayloadHex)
			if err != nil {
				return fmt.Errorf("decode migration payload: %w", err)
			}
			req := avmMigrateStateRequest{
				Actor:             actor,
				ContractAddress:   strings.TrimSpace(args[0]),
				FromSchemaVersion: fromVersion,
				ToSchemaVersion:   toVersion,
				MigrationHandler:  strings.TrimSpace(migrationHandler),
				Payload:           base64OrEmpty(payload),
				Height:            height,
			}
			if req.ContractAddress == "" {
				return errors.New("migrate-state contract address is required")
			}
			if req.MigrationHandler == "" {
				return errors.New("migrate-state migration handler is required")
			}
			if req.Height == 0 {
				return errors.New("migrate-state height must be positive")
			}
			if broadcast {
				return runAVMBroadcast(cmd, &contractstypes.MsgMigrateContractState{
					Actor:             req.Actor,
					ContractAddress:   req.ContractAddress,
					FromSchemaVersion: req.FromSchemaVersion,
					ToSchemaVersion:   req.ToSchemaVersion,
					MigrationHandler:  req.MigrationHandler,
					Payload:           payload,
					Height:            req.Height,
				})
			}
			return writeCommandJSON(cmd, avmServicePayload{
				Service:    avmMsgService,
				Method:     "MigrateContractState",
				FullMethod: "/" + avmMsgService + "/MigrateContractState",
				TypeURL:    avmMsgMigrateContractStateTypeURL,
				Request:    req,
			})
		},
	}
	addAVMTxFlags(cmd)
	addBroadcastFlag(cmd)
	cmd.Flags().String(flagAVMActor, "", "migration actor AE address (must be the contract's admin, or the governance authority for a system-owned contract); defaults to --from")
	cmd.Flags().Uint64(flagAVMFromSchemaVersion, 0, "contract's current storage schema version")
	cmd.Flags().Uint64(flagAVMToSchemaVersion, 0, "target storage schema version (must exceed from-schema-version)")
	cmd.Flags().String(flagAVMMigrationHandler, "", "migration handler: schema_only, replace, or append")
	cmd.Flags().String(flagAVMPayloadHex, "", "hex-encoded migration payload")
	cmd.Flags().Uint64(flagAVMHeight, 1, "execution height (with --broadcast, defaults to the chain's current height when not set explicitly)")
	return cmd
}

func newAVMSetAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-admin [contract-address]",
		Short: "Build l1.contracts.v1.Msg/SetContractAdmin request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateAVMTxFees(cmd); err != nil {
				return err
			}
			actor, err := avmAddressOverride(cmd, flagAVMActor, "set-admin actor")
			if err != nil {
				return err
			}
			broadcast, _ := cmd.Flags().GetBool(flagBroadcast)
			var clientCtx client.Context
			height, _ := cmd.Flags().GetUint64(flagAVMHeight)
			if broadcast {
				clientCtx, err = client.GetClientTxContext(cmd)
				if err != nil {
					return err
				}
				height, err = avmBroadcastHeight(cmd, clientCtx)
				if err != nil {
					return err
				}
			}
			newAdmin, _ := cmd.Flags().GetString(flagAVMNewAdmin)
			req := avmSetAdminRequest{
				Actor:           actor,
				ContractAddress: strings.TrimSpace(args[0]),
				NewAdmin:        strings.TrimSpace(newAdmin),
				Height:          height,
			}
			if req.ContractAddress == "" {
				return errors.New("set-admin contract address is required")
			}
			if req.NewAdmin == "" {
				return errors.New("set-admin new admin address is required")
			}
			if req.Height == 0 {
				return errors.New("set-admin height must be positive")
			}
			if broadcast {
				return runAVMBroadcast(cmd, &contractstypes.MsgSetContractAdmin{
					Actor:           req.Actor,
					ContractAddress: req.ContractAddress,
					NewAdmin:        req.NewAdmin,
					Height:          req.Height,
				})
			}
			return writeCommandJSON(cmd, avmServicePayload{
				Service:    avmMsgService,
				Method:     "SetContractAdmin",
				FullMethod: "/" + avmMsgService + "/SetContractAdmin",
				TypeURL:    avmMsgSetContractAdminTypeURL,
				Request:    req,
			})
		},
	}
	addAVMTxFlags(cmd)
	addBroadcastFlag(cmd)
	cmd.Flags().String(flagAVMActor, "", "set-admin actor AE address (must be the contract's current admin, or the governance authority for a system-owned contract); defaults to --from")
	cmd.Flags().String(flagAVMNewAdmin, "", "new contract admin AE address")
	cmd.Flags().Uint64(flagAVMHeight, 1, "execution height (with --broadcast, defaults to the chain's current height when not set explicitly)")
	return cmd
}

func newAVMDisableUpgradesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable-upgrades [contract-address]",
		Short: "Build l1.contracts.v1.Msg/DisableContractUpgrades request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateAVMTxFees(cmd); err != nil {
				return err
			}
			actor, err := avmAddressOverride(cmd, flagAVMActor, "disable-upgrades actor")
			if err != nil {
				return err
			}
			broadcast, _ := cmd.Flags().GetBool(flagBroadcast)
			var clientCtx client.Context
			height, _ := cmd.Flags().GetUint64(flagAVMHeight)
			if broadcast {
				clientCtx, err = client.GetClientTxContext(cmd)
				if err != nil {
					return err
				}
				height, err = avmBroadcastHeight(cmd, clientCtx)
				if err != nil {
					return err
				}
			}
			req := avmDisableUpgradesRequest{
				Actor:           actor,
				ContractAddress: strings.TrimSpace(args[0]),
				Height:          height,
			}
			if req.ContractAddress == "" {
				return errors.New("disable-upgrades contract address is required")
			}
			if req.Height == 0 {
				return errors.New("disable-upgrades height must be positive")
			}
			if broadcast {
				return runAVMBroadcast(cmd, &contractstypes.MsgDisableContractUpgrades{
					Actor:           req.Actor,
					ContractAddress: req.ContractAddress,
					Height:          req.Height,
				})
			}
			return writeCommandJSON(cmd, avmServicePayload{
				Service:    avmMsgService,
				Method:     "DisableContractUpgrades",
				FullMethod: "/" + avmMsgService + "/DisableContractUpgrades",
				TypeURL:    avmMsgDisableContractUpgradesTypeURL,
				Request:    req,
			})
		},
	}
	addAVMTxFlags(cmd)
	addBroadcastFlag(cmd)
	cmd.Flags().String(flagAVMActor, "", "disable-upgrades actor AE address (must be the contract's admin, or the governance authority for a system-owned contract); defaults to --from")
	cmd.Flags().Uint64(flagAVMHeight, 1, "execution height (with --broadcast, defaults to the chain's current height when not set explicitly)")
	return cmd
}

func newAVMCodeQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "code [code-id]",
		Short: "Query l1.contracts.v1.Query/Code",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			res, err := contractstypes.NewQueryClient(clientCtx).Code(cmd.Context(), &contractstypes.QueryCodeRequest{
				CodeID: strings.TrimSpace(args[0]),
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

func newAVMContractQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contract [contract-address]",
		Short: "Query l1.contracts.v1.Query/Contract",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			res, err := contractstypes.NewQueryClient(clientCtx).Contract(cmd.Context(), &contractstypes.QueryContractRequest{
				ContractAddress: strings.TrimSpace(args[0]),
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

// newAVMContractsQueryCmd lists deployed contracts via the already-implemented
// keeper method (x/contracts/keeper/grpc_server.go Contracts, backed by
// types.QueryContractsRequest/Response with Offset/Limit pagination -- see
// x/contracts/keeper/keeper.go Keeper.Contracts). Without this, finding a
// deployed contract's address operationally required scraping deploy tx logs
// (see RESULTS_V1-live-testnet-exercise.md section 3, item 3).
func newAVMContractsQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contracts",
		Short: "Query l1.contracts.v1.Query/Contracts (list deployed contracts)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			limit, _ := cmd.Flags().GetUint32(flagAVMLimit)
			if limit == 0 {
				return errors.New("contracts query limit must be positive")
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			res, err := contractstypes.NewQueryClient(clientCtx).Contracts(cmd.Context(), &contractstypes.QueryContractsRequest{
				Pagination: contractstypes.PageRequest{Limit: limit},
			})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	cmd.Flags().Uint32(flagAVMLimit, 50, "bounded contracts list query limit")
	return cmd
}

func newAVMStorageQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "storage [contract-address]",
		Short: "Query l1.contracts.v1.Query/ContractStorage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keyPrefix, err := optionalHexFlag(cmd, flagAVMKeyPrefixHex)
			if err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetUint32(flagAVMLimit)
			if limit == 0 {
				return errors.New("storage query limit must be positive")
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			res, err := contractstypes.NewQueryClient(clientCtx).ContractStorage(cmd.Context(), &contractstypes.QueryContractStorageRequest{
				ContractAddress: strings.TrimSpace(args[0]),
				KeyPrefix:       keyPrefix,
				Pagination:      contractstypes.PageRequest{Limit: limit},
			})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	cmd.Flags().String(flagAVMKeyPrefixHex, "", "hex-encoded storage key prefix")
	cmd.Flags().Uint32(flagAVMLimit, 50, "bounded storage query limit")
	return cmd
}

func newAVMReceiptsQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "receipts [contract-address]",
		Short: "Query l1.contracts.v1.Query/ContractReceipts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			limit, _ := cmd.Flags().GetUint32(flagAVMLimit)
			if limit == 0 {
				return errors.New("receipts query limit must be positive")
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			res, err := contractstypes.NewQueryClient(clientCtx).ContractReceipts(cmd.Context(), &contractstypes.QueryContractReceiptsRequest{
				ContractAddress: strings.TrimSpace(args[0]),
				Pagination:      contractstypes.PageRequest{Limit: limit},
			})
			if err != nil {
				return err
			}
			return clientCtx.PrintProto(res)
		},
	}
	flags.AddQueryFlagsToCmd(cmd)
	cmd.Flags().Uint32(flagAVMLimit, 50, "bounded receipts query limit")
	return cmd
}

func newAVMEncodeMessageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "encode-message",
		Short: "Encode an AVM message body from JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, abiOpcode, abiOpcodeKnown, err := resolveAVMBody(cmd)
			if err != nil {
				return err
			}
			opcode, _ := cmd.Flags().GetUint32(flagAVMOpcode)
			if abiOpcodeKnown {
				if cmd.Flags().Changed(flagAVMOpcode) && opcode != abiOpcode {
					return fmt.Errorf("--opcode %d conflicts with the compiled opcode %d for this message", opcode, abiOpcode)
				}
				opcode = abiOpcode
			}
			queryID, _ := cmd.Flags().GetUint64(flagAVMQueryID)
			sum := sha256.Sum256(body)
			return writeCommandJSON(cmd, map[string]any{
				"body_base64": base64.StdEncoding.EncodeToString(body),
				"body_hex":    hex.EncodeToString(body),
				"body_sha256": hex.EncodeToString(sum[:]),
				"opcode":      opcode,
				"query_id":    queryID,
			})
		},
	}
	addAVMBodyFlags(cmd)
	cmd.Flags().Uint32(flagAVMOpcode, 0, "AVM opcode")
	cmd.Flags().Uint64(flagAVMQueryID, 0, "AVM query id")
	return cmd
}

func newAVMDecodeReceiptCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "decode-receipt",
		Short: "Normalize an AVM execution receipt into stable JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			raw, err := readReceiptJSON(cmd)
			if err != nil {
				return err
			}
			var receipt async.ExecutionReceipt
			if err := json.Unmarshal(raw, &receipt); err != nil {
				return fmt.Errorf("decode receipt JSON: %w", err)
			}
			id := avmReceiptID(receipt)
			return writeCommandJSON(cmd, map[string]any{
				"receipt_id":       id,
				"sequence":         receipt.Sequence,
				"source":           receipt.Source.String(),
				"destination":      receipt.Destination.String(),
				"opcode":           receipt.Opcode,
				"query_id":         receipt.QueryID,
				"exit_code":        receipt.ResultCode,
				"gas_used":         receipt.GasUsed,
				"storage_fee_naet": receipt.StorageFeeNaet.String(),
				"forward_fee_naet": receipt.ForwardFeeNaet.String(),
				"bounced":          receipt.Bounced,
				"retry_count":      receipt.RetryCount,
				"retry_scheduled":  receipt.RetryScheduled,
				"error":            receipt.Error,
			})
		},
	}
	cmd.Flags().String(flagAVMReceiptJSON, "", "receipt JSON")
	cmd.Flags().String(flagAVMReceiptFile, "", "receipt JSON file")
	return cmd
}

func addAVMTxFlags(cmd *cobra.Command) {
	flags.AddTxFlagsToCmd(cmd)
}

func addAVMBodyFlags(cmd *cobra.Command) {
	cmd.Flags().String(flagAVMBodyJSON, "", "JSON message body (raw bytes; NOT the ATLX wire format)")
	cmd.Flags().String(flagAVMBodyFile, "", "file containing JSON message body (raw bytes)")
	cmd.Flags().String(flagAVMBodyHex, "", "hex-encoded message body (raw bytes)")
	cmd.Flags().String(flagAVMSource, "", "contract .atlx source file; with --message, ABI-encodes the body with the contract's canonical codec")
	cmd.Flags().String(flagAVMMessage, "", "@message struct name to encode (requires --source)")
	cmd.Flags().String(flagAVMFields, "", "JSON object of message field values for --message (default {})")
}

// avmBroadcastHeight resolves the height an AVM tx message should carry: the
// contracts keeper stores this value verbatim as the contract's
// created/updated/last-rent-charge height (see x/contracts/keeper/keeper.go),
// so submitting a stale or placeholder height (e.g. the CLI's dry-run default
// of 1) against a chain that is actually thousands of blocks in means the
// very first rent charge sees a huge elapsed block span and can freeze the
// contract immediately. When broadcasting for real and the caller did not
// explicitly override --height, fetch the chain's current height instead.
func avmBroadcastHeight(cmd *cobra.Command, clientCtx client.Context) (uint64, error) {
	if cmd.Flags().Changed(flagAVMHeight) {
		return cmd.Flags().GetUint64(flagAVMHeight)
	}
	if clientCtx.Client == nil {
		return cmd.Flags().GetUint64(flagAVMHeight)
	}
	status, err := clientCtx.Client.Status(cmd.Context())
	if err != nil {
		return 0, fmt.Errorf("query current chain height: %w", err)
	}
	height := status.SyncInfo.LatestBlockHeight
	if height <= 0 {
		return 0, errors.New("chain reports non-positive latest block height")
	}
	return uint64(height), nil
}

type avmBroadcastResult struct {
	TxHash string `json:"txhash"`
	Code   uint32 `json:"code"`
	RawLog string `json:"raw_log,omitempty"`
	// Confirmed is true once Code/RawLog reflect the tx's actual committed
	// (FinalizeBlock/DeliverTx) result rather than just CheckTx/mempool
	// admission. See awaitAVMTxConfirmation.
	Confirmed bool `json:"confirmed"`
	// Note explains why Confirmed is false when it is: the tx was accepted by
	// CheckTx but did not show up in a block within the poll window, so
	// Code/RawLog above are CheckTx-only and do not reflect execution.
	Note string `json:"note,omitempty"`
	// ContractAddressUser/ContractAddressRaw are populated for a confirmed,
	// successful MsgDeployContract broadcast: the deployed contract's address,
	// decoded from the confirmed tx's MsgDeployContractResponse so the caller
	// does not have to separately look it up (see decodeDeployContractAddress).
	ContractAddressUser string `json:"contract_address_user,omitempty"`
	ContractAddressRaw  string `json:"contract_address_raw,omitempty"`
}

func runAVMBroadcast(cmd *cobra.Command, msg sdk.Msg) error {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}
	res, broadcastErr := signAndBroadcast(context.Background(), clientCtx, cmd.Flags(), msg)
	if broadcastErr != nil && res == nil {
		return broadcastErr
	}

	result := avmBroadcastResult{TxHash: res.TxHash, Code: res.Code, RawLog: res.RawLog}
	finalErr := broadcastErr

	// res.Code == 0 at this point only means CheckTx admitted the tx to the
	// mempool (see the avmTxConfirmPollInterval doc comment above) -- confirm
	// the real outcome before reporting success or extracting a deploy
	// response. A nonzero res.Code here is a genuine CheckTx-level rejection
	// (bad signature, insufficient fee, sequence mismatch, ...); there is
	// nothing to wait for in that case since the tx never entered the mempool.
	if res.Code == 0 {
		if confirmed, ok := awaitAVMTxConfirmation(cmd, clientCtx, res.TxHash); ok {
			result.Confirmed = true
			result.Code = confirmed.Code
			result.RawLog = confirmed.RawLog
			if confirmed.Code != 0 {
				finalErr = fmt.Errorf("tx rejected: code=%d raw_log=%s", confirmed.Code, confirmed.RawLog)
			} else {
				finalErr = nil
				if _, isDeploy := msg.(*contractstypes.MsgDeployContract); isDeploy {
					if user, raw, found := decodeDeployContractAddress(clientCtx, confirmed); found {
						result.ContractAddressUser = user
						result.ContractAddressRaw = raw
					}
				}
			}
		} else {
			result.Note = fmt.Sprintf("tx admitted by CheckTx (mempool) but not yet confirmed in a block within %s; code/raw_log above reflect mempool admission only, not execution -- re-check with `l1d query tx %s`", avmTxConfirmPollInterval*avmTxConfirmMaxAttempts, res.TxHash)
		}
	}

	if writeErr := writeCommandJSON(cmd, result); writeErr != nil {
		return writeErr
	}
	return finalErr
}

// awaitAVMTxConfirmation polls for txHash's committed (post-block) result and
// returns it with ok=true once found. It returns ok=false (not an error) if
// the tx is not yet visible within the poll budget: callers should fall back
// to the CheckTx-only result rather than fail the whole command, since the tx
// may simply still be in flight rather than lost.
func awaitAVMTxConfirmation(cmd *cobra.Command, clientCtx client.Context, txHash string) (*sdk.TxResponse, bool) {
	clientCtx = clientCtx.WithCmdContext(cmd.Context())
	waitCtx := clientCtx.GetCmdContextWithFallback()
	for attempt := 0; attempt < avmTxConfirmMaxAttempts; attempt++ {
		select {
		case <-waitCtx.Done():
			return nil, false
		case <-time.After(avmTxConfirmPollInterval):
		}
		confirmed, err := authtx.QueryTx(clientCtx, txHash)
		if err == nil && confirmed != nil {
			return confirmed, true
		}
	}
	return nil, false
}

// decodeDeployContractAddress extracts the deployed contract's address from a
// confirmed deploy tx. The deploy keeper method (x/contracts/keeper/keeper.go
// instantiateContract) does not emit a separate SDK/ABCI event carrying the
// address the way execute's avm_execute event does, so the only place this is
// recoverable from is the tx's packed Msg response: res.Data decodes as
// sdk.TxMsgData{MsgResponses: []*Any}, and the deploy response
// (MsgDeployContractResponse, wire-aliased to
// contractstypes.InstantiateContractResponse) is one of those Anys.
func decodeDeployContractAddress(clientCtx client.Context, res *sdk.TxResponse) (userAddr, rawAddr string, found bool) {
	if res == nil || res.Data == "" || clientCtx.Codec == nil {
		return "", "", false
	}
	dataBytes, err := hex.DecodeString(res.Data)
	if err != nil {
		return "", "", false
	}
	var txMsgData sdk.TxMsgData
	if err := clientCtx.Codec.Unmarshal(dataBytes, &txMsgData); err != nil {
		return "", "", false
	}
	for _, msgResponse := range txMsgData.MsgResponses {
		if msgResponse == nil || msgResponse.TypeUrl != avmMsgDeployContractResponseTypeURL {
			continue
		}
		var deployResp contractstypes.InstantiateContractResponse
		if err := clientCtx.Codec.Unmarshal(msgResponse.Value, &deployResp); err != nil {
			return "", "", false
		}
		return deployResp.ContractAddressUser, deployResp.ContractAddressRaw, true
	}
	return "", "", false
}

func validateAVMTxFees(cmd *cobra.Command) error {
	feeText, _ := cmd.Flags().GetString(flags.FlagFees)
	feeText = strings.TrimSpace(feeText)
	if feeText == "" {
		return errors.New("AVM tx requires --fees with naet denom")
	}
	coins, err := sdk.ParseCoinsNormalized(feeText)
	if err != nil {
		return fmt.Errorf("invalid AVM tx fees: %w", err)
	}
	if coins.Empty() {
		return errors.New("AVM tx fees must not be empty")
	}
	for _, coin := range coins {
		if coin.Denom != appparams.BaseDenom {
			return fmt.Errorf("AVM tx fees must use %s denom", appparams.BaseDenom)
		}
	}
	return nil
}

func avmAuthority(cmd *cobra.Command) (string, error) {
	authority, _ := cmd.Flags().GetString(flagAVMAuthority)
	authority = strings.TrimSpace(authority)
	if authority != "" {
		return authority, nil
	}
	return requiredFlag(cmd, flags.FlagFrom, "store code authority")
}

// avmAddressOverride resolves a message-content address for commands where
// --from must name a keyring key (not an AE address string, which isn't
// valid bech32 and can't be resolved back to a signing key) but the message
// itself needs the caller's real AE address: --from's keyring account may
// have an AE identity that differs from its key name, so callers pass it
// explicitly via flagName instead of relying on --from's raw string value.
func avmAddressOverride(cmd *cobra.Command, flagName, label string) (string, error) {
	override, _ := cmd.Flags().GetString(flagName)
	override = strings.TrimSpace(override)
	if override != "" {
		return override, nil
	}
	return requiredFlag(cmd, flags.FlagFrom, label)
}

func requiredFlag(cmd *cobra.Command, name string, label string) (string, error) {
	value, _ := cmd.Flags().GetString(name)
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	return value, nil
}

func readBodyBytes(cmd *cobra.Command) ([]byte, error) {
	body, _, _, err := resolveAVMBody(cmd)
	return body, err
}

// resolveAVMBody resolves the message body from the CLI flags. Two mutually
// exclusive paths exist:
//
//   - raw bytes via --body-json/--body-file/--body-hex (the operator already
//     has wire bytes);
//   - ABI encoding via --source + --message [+ --fields]: the contract's
//     .atlx source is compiled and the named @message struct's canonical
//     body codec encodes the JSON field map. This is the only way to produce
//     a body the deployed module can actually decode — hand-written JSON
//     bytes are NOT the ATLX wire format and abort execution.
//
// When the ABI path is used, the message's opcode from the compiled selector
// registry is returned as well (opcodeKnown=true) so callers can surface it.
func resolveAVMBody(cmd *cobra.Command) (body []byte, opcode uint32, opcodeKnown bool, err error) {
	bodyJSON, _ := cmd.Flags().GetString(flagAVMBodyJSON)
	bodyFile, _ := cmd.Flags().GetString(flagAVMBodyFile)
	bodyHex, _ := cmd.Flags().GetString(flagAVMBodyHex)
	sourceFile, _ := cmd.Flags().GetString(flagAVMSource)
	messageName, _ := cmd.Flags().GetString(flagAVMMessage)
	fieldsJSON, _ := cmd.Flags().GetString(flagAVMFields)

	abiSet := strings.TrimSpace(sourceFile) != "" || strings.TrimSpace(messageName) != ""
	rawSet := countSet(bodyJSON, bodyFile, bodyHex)

	if abiSet && rawSet > 0 {
		return nil, 0, false, errors.New("set either raw body flags (body-json/body-file/body-hex) or ABI flags (source/message/fields), not both")
	}
	if abiSet {
		if strings.TrimSpace(sourceFile) == "" || strings.TrimSpace(messageName) == "" {
			return nil, 0, false, errors.New("ABI encoding requires both --source and --message")
		}
		body, opcode, err = encodeAVMMessageBody(strings.TrimSpace(sourceFile), strings.TrimSpace(messageName), fieldsJSON)
		if err != nil {
			return nil, 0, false, err
		}
		return body, opcode, true, nil
	}
	if rawSet == 0 {
		return nil, 0, false, nil
	}
	if rawSet > 1 {
		return nil, 0, false, errors.New("set only one of body-json, body-file, or body-hex")
	}
	if strings.TrimSpace(bodyHex) != "" {
		body, err = hex.DecodeString(strings.TrimSpace(bodyHex))
		return body, 0, false, err
	}
	if strings.TrimSpace(bodyFile) != "" {
		bz, err := os.ReadFile(strings.TrimSpace(bodyFile))
		if err != nil {
			return nil, 0, false, err
		}
		body, err = canonicalJSONBytes(bz)
		return body, 0, false, err
	}
	body, err = canonicalJSONBytes([]byte(bodyJSON))
	return body, 0, false, err
}

// encodeAVMMessageBody compiles the contract source and encodes the named
// @message struct's fields with the contract's own canonical body codec.
func encodeAVMMessageBody(sourceFile, messageName, fieldsJSON string) ([]byte, uint32, error) {
	src, err := os.ReadFile(sourceFile)
	if err != nil {
		return nil, 0, fmt.Errorf("read contract source: %w", err)
	}
	c, err := compiler.New(compiler.DefaultOptions())
	if err != nil {
		return nil, 0, err
	}
	compiled, err := c.Compile(src)
	if err != nil {
		return nil, 0, fmt.Errorf("compile %s: %w", sourceFile, err)
	}
	codec, ok := compiled.MessageBodies[messageName]
	if !ok {
		known := make([]string, 0, len(compiled.MessageBodies))
		for name := range compiled.MessageBodies {
			known = append(known, name)
		}
		sort.Strings(known)
		return nil, 0, fmt.Errorf("message %q is not declared in %s (declared: %s)", messageName, sourceFile, strings.Join(known, ", "))
	}
	fields := map[string]any{}
	if strings.TrimSpace(fieldsJSON) != "" {
		decoder := json.NewDecoder(strings.NewReader(fieldsJSON))
		decoder.UseNumber()
		if err := decoder.Decode(&fields); err != nil {
			return nil, 0, fmt.Errorf("parse --fields JSON: %w", err)
		}
	}
	body, err := codec.Encode(normalizeJSONNumbers(fields).(map[string]any))
	if err != nil {
		return nil, 0, fmt.Errorf("encode %s body: %w", messageName, err)
	}
	return body, compiled.MessageBodyOpcodes[messageName], nil
}

// normalizeJSONNumbers converts json.Number values (from UseNumber decoding,
// which preserves full uint64 precision) into native Go integers the message
// codec's reflection path understands. Values outside int64/uint64 range stay
// as decimal strings for the codec's big-integer handling.
func normalizeJSONNumbers(value any) any {
	switch v := value.(type) {
	case json.Number:
		if u, err := strconv.ParseUint(v.String(), 10, 64); err == nil {
			return u
		}
		if i, err := strconv.ParseInt(v.String(), 10, 64); err == nil {
			return i
		}
		return v.String()
	case map[string]any:
		for key, item := range v {
			v[key] = normalizeJSONNumbers(item)
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = normalizeJSONNumbers(item)
		}
		return v
	default:
		return value
	}
}

func readOptionalBytes(cmd *cobra.Command, fileFlag string, hexFlag string) ([]byte, error) {
	fileName, _ := cmd.Flags().GetString(fileFlag)
	hexText, _ := cmd.Flags().GetString(hexFlag)
	if strings.TrimSpace(fileName) != "" && strings.TrimSpace(hexText) != "" {
		return nil, fmt.Errorf("set only one of %s or %s", fileFlag, hexFlag)
	}
	if strings.TrimSpace(fileName) != "" {
		return os.ReadFile(strings.TrimSpace(fileName))
	}
	if strings.TrimSpace(hexText) != "" {
		return hex.DecodeString(strings.TrimSpace(hexText))
	}
	return nil, nil
}

func optionalHexFlag(cmd *cobra.Command, name string) ([]byte, error) {
	value, _ := cmd.Flags().GetString(name)
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	return hex.DecodeString(value)
}

func canonicalJSONBytes(raw []byte) ([]byte, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, errors.New("JSON body is empty")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("invalid JSON body: %w", err)
	}
	return json.Marshal(value)
}

func readReceiptJSON(cmd *cobra.Command) ([]byte, error) {
	text, _ := cmd.Flags().GetString(flagAVMReceiptJSON)
	fileName, _ := cmd.Flags().GetString(flagAVMReceiptFile)
	if strings.TrimSpace(text) != "" && strings.TrimSpace(fileName) != "" {
		return nil, errors.New("set only one of receipt-json or receipt-file")
	}
	if strings.TrimSpace(fileName) != "" {
		return os.ReadFile(strings.TrimSpace(fileName))
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("receipt JSON is required")
	}
	return []byte(text), nil
}

func avmReceiptID(receipt async.ExecutionReceipt) string {
	buf := bytes.NewBuffer(nil)
	_, _ = fmt.Fprintf(buf, "%020d/%s/%s/%010d/%020d/%010d/%020d/%s",
		receipt.Sequence,
		receipt.Source.String(),
		receipt.Destination.String(),
		receipt.Opcode,
		receipt.QueryID,
		receipt.ResultCode,
		receipt.GasUsed,
		receipt.Error,
	)
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:])
}

func writeCommandJSON(cmd *cobra.Command, value any) error {
	bz, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(bz))
	return err
}

func base64OrEmpty(bz []byte) string {
	if len(bz) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(bz)
}

func countSet(values ...string) int {
	var count int
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}

func canonicalCodeHash(bytecode []byte) string {
	sum := sha256.Sum256(append([]byte("aetra-avm-code-v1/"), bytecode...))
	return hex.EncodeToString(sum[:])
}
