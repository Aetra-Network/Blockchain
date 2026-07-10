// Package chainquery implements the explorer's live module-state reads over
// the node's gRPC endpoint. Aetra's x/contracts messages are hand-rolled
// gogoproto types, so the client forces a gogoproto codec (rawCodec) rather
// than grpc-go's default protov2 codec; the same codec serves standard
// cosmos-sdk bank/staking queries too.
package chainquery

import (
	"context"
	"fmt"

	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	contractstypes "github.com/sovereign-l1/l1/x/contracts/types"
)

// Client is a gRPC ChainQuerier for the explorer api.ChainQuerier interface.
type Client struct {
	conn *grpc.ClientConn
}

// Dial opens a gRPC connection to the node (e.g. "127.0.0.1:9090").
func Dial(endpoint string) (*Client, error) {
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial gRPC %s: %w", endpoint, err)
	}
	return &Client{conn: conn}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) invoke(ctx context.Context, method string, req, resp gogoproto.Message) error {
	return c.conn.Invoke(ctx, method, req, resp, grpc.ForceCodec(rawCodec{}))
}

// Contracts lists deployed AVM contracts (address, code id, status, balance,
// creator) — the core of a contract explorer index page.
func (c *Client) Contracts(ctx context.Context, limit uint32) (any, error) {
	if limit == 0 || limit > 200 {
		limit = 50
	}
	req := &contractstypes.QueryContractsRequest{Pagination: contractstypes.PageRequest{Limit: limit}}
	resp := &contractstypes.QueryContractsResponse{}
	if err := c.invoke(ctx, "/l1.contracts.v1.Query/Contracts", req, resp); err != nil {
		return nil, fmt.Errorf("query contracts: %w", err)
	}
	items := make([]map[string]any, 0, len(resp.Contracts))
	for _, ct := range resp.Contracts {
		items = append(items, contractSummary(ct))
	}
	return map[string]any{"contracts": items, "count": len(items)}, nil
}

// Contract returns a single contract's detail plus its committed receipts.
func (c *Client) Contract(ctx context.Context, address string) (any, error) {
	req := &contractstypes.QueryContractRequest{ContractAddress: address}
	resp := &contractstypes.QueryContractResponse{}
	if err := c.invoke(ctx, "/l1.contracts.v1.Query/Contract", req, resp); err != nil {
		return nil, fmt.Errorf("query contract: %w", err)
	}
	if !resp.Found {
		return map[string]any{"found": false, "address": address}, nil
	}
	out := contractSummary(resp.Contract)
	out["found"] = true
	out["state_root"] = resp.Contract.StateRoot
	out["code_hash"] = resp.Contract.CodeHash
	out["admin"] = resp.Contract.Admin
	out["storage_bytes"] = resp.Contract.StorageBytes
	out["logical_time"] = resp.Contract.LogicalTime
	out["created_height"] = resp.Contract.CreatedHeight
	out["updated_height"] = resp.Contract.UpdatedHeight
	return out, nil
}

func contractSummary(ct contractstypes.Contract) map[string]any {
	return map[string]any{
		"address": ct.AddressUser,
		"code_id": ct.CodeID,
		"status":  ct.Status,
		"balance": ct.Balance,
		"creator": ct.Creator,
	}
}

// Validators lists the bonded validator set (operator, moniker, tokens,
// commission, jailed/status) for the validators page.
func (c *Client) Validators(ctx context.Context) (any, error) {
	req := &stakingtypes.QueryValidatorsRequest{}
	resp := &stakingtypes.QueryValidatorsResponse{}
	if err := c.invoke(ctx, "/cosmos.staking.v1beta1.Query/Validators", req, resp); err != nil {
		return nil, fmt.Errorf("query validators: %w", err)
	}
	items := make([]map[string]any, 0, len(resp.Validators))
	for _, v := range resp.Validators {
		items = append(items, map[string]any{
			"operator_address": v.OperatorAddress,
			"moniker":          v.Description.Moniker,
			"status":           v.Status.String(),
			"jailed":           v.Jailed,
			"tokens":           v.Tokens.String(),
			"delegator_shares": v.DelegatorShares.String(),
			"commission_rate":  v.Commission.CommissionRates.Rate.String(),
		})
	}
	return map[string]any{"validators": items, "count": len(items)}, nil
}

// Supply returns the total on-chain token supply.
func (c *Client) Supply(ctx context.Context) (any, error) {
	req := &banktypes.QueryTotalSupplyRequest{}
	resp := &banktypes.QueryTotalSupplyResponse{}
	if err := c.invoke(ctx, "/cosmos.bank.v1beta1.Query/TotalSupply", req, resp); err != nil {
		return nil, fmt.Errorf("query supply: %w", err)
	}
	coins := make([]map[string]string, 0, len(resp.Supply))
	for _, c := range resp.Supply {
		coins = append(coins, map[string]string{"denom": c.Denom, "amount": c.Amount.String()})
	}
	return map[string]any{"supply": coins}, nil
}

// rawCodec marshals/unmarshals via gogoproto reflection, the mechanism the
// node uses for the hand-rolled x/contracts types (and which works for the
// standard cosmos-sdk gogoproto query types too).
type rawCodec struct{}

func (rawCodec) Marshal(v any) ([]byte, error) {
	msg, ok := v.(gogoproto.Message)
	if !ok {
		return nil, fmt.Errorf("not a gogoproto.Message: %T", v)
	}
	return gogoproto.Marshal(msg)
}

func (rawCodec) Unmarshal(data []byte, v any) error {
	msg, ok := v.(gogoproto.Message)
	if !ok {
		return fmt.Errorf("not a gogoproto.Message: %T", v)
	}
	return gogoproto.Unmarshal(data, msg)
}

func (rawCodec) Name() string { return "explorer-raw-gogoproto" }
