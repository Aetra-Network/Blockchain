package keeper

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/identity-root/types"
)

// This file closes the integration-completeness audit's CONFIRMED test-coverage
// gap: DetachDomain, DisownAttachment, CreateSubdomain and UpdatePriceTable had
// no wrapper-level test reaching them through the real grpcMsgServer (only the
// keeper method was ever called directly in tests), and 10 of the 11 query
// methods (all but NameZone) had no wrapper-level test reaching them through
// grpcQueryServer either. Neither gap is a live bug -- every handler compiles
// against the MsgServer/QueryServer interfaces (var _ assertions in
// grpc_server.go) -- but an interface assertion only proves the method exists
// with the right signature, not that routing it through the real wrapper (which
// does its own pre-work: loadForBlock, the blockHeight(ctx) override) behaves
// correctly.

// TestDetachDisownCreateSubdomainReachableThroughGRPC covers the three
// owner-signed Msg handlers the audit found reachable only via direct keeper
// calls in every existing test.
func TestDetachDisownCreateSubdomainReachableThroughGRPC(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 10})
	require.NoError(t, err)
	srv := NewGRPCMsgServer(&k)

	t.Run("DetachDomain", func(t *testing.T) {
		_, err := k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "alice", Target: targetUser, Height: 11})
		require.NoError(t, err)

		ctx := sdk.Context{}.WithBlockHeight(12)
		// msg.Height is deliberately left at its zero value: if the wrapper did
		// NOT overwrite it with blockHeight(ctx) before calling the keeper, the
		// keeper's own "identity detach height must be positive" guard would
		// reject this -- so success here proves the wrapper's override actually
		// ran, not just that the method compiles.
		res, err := srv.DetachDomain(ctx, &types.MsgDetachDomain{Owner: ownerA, Fqdn: "alice"})
		require.NoError(t, err, "the wrapper must overwrite msg.Height from the block context before calling the keeper")
		require.Equal(t, "alice.aet", res.Fqdn)
	})

	t.Run("DisownAttachment", func(t *testing.T) {
		_, err := k.AttachDomain(types.MsgAttachDomain{Owner: ownerA, Fqdn: "alice", Target: targetUser, Height: 13})
		require.NoError(t, err)

		ctx := sdk.Context{}.WithBlockHeight(14)
		res, err := srv.DisownAttachment(ctx, &types.MsgDisownAttachment{Target: targetUser})
		require.NoError(t, err, "the wrapper must overwrite msg.Height from the block context before calling the keeper")
		require.Equal(t, "alice.aet", res.Fqdn)
	})

	t.Run("CreateSubdomain", func(t *testing.T) {
		const (
			blockHeightAt  = int64(90)
			attackerHeight = uint64(900_000)
		)
		ctx := sdk.Context{}.WithBlockHeight(blockHeightAt)
		res, err := srv.CreateSubdomain(ctx, &types.MsgCreateSubdomain{Owner: ownerA, ParentName: "alice", Label: "app", Height: attackerHeight})
		require.NoError(t, err)
		require.Equal(t, "app.alice.aet", res.Name)

		record, found, err := k.NameRecord(context.Background(), "app.alice.aet")
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, uint64(blockHeightAt), record.CreatedHeight,
			"a subdomain's CreatedHeight must be the block height, not the caller's attacker-supplied msg.Height")
	})
}

// TestUpdatePriceTableReachableThroughGRPC covers the one remaining Msg
// handler the audit flagged: it carries no Height field (a governance action,
// not a block-timed one), so it only needed a basic reachability check.
func TestUpdatePriceTableReachableThroughGRPC(t *testing.T) {
	k := setupKeeper(t)
	srv := NewGRPCMsgServer(&k)
	ctx := sdk.Context{}.WithBlockHeight(1)

	res, err := srv.UpdatePriceTable(ctx, &types.MsgUpdatePriceTable{
		Authority:    authority,
		MinLabelLens: []uint32{3, 9},
		PricesNaet:   []string{"5000", "1000"},
	})
	require.NoError(t, err)
	require.Equal(t, uint32(2), res.Tiers)

	params, err := k.IdentityRootParams(context.Background())
	require.NoError(t, err)
	require.Len(t, params.PriceTable, 2)
	require.Equal(t, "5000", params.PriceTable[0].PriceNaet)
}

// TestQueryServerViewMethodsReachableThroughGRPC covers the 10 query methods
// the audit found reachable only via direct keeper calls (collectionParamsView
// / collectionBalanceView / priceForLabelView / auctionsView / auctionView /
// domainStatusView are private, reachable ONLY through the wrapper; NameRecord
// / ResolveName / ReverseRecord / Subdomains are public but were only ever
// called directly on the keeper in tests, never via grpcQueryServer). It also
// doubles as a second, wrapper-level exercise of the viewGenesis stale-cache
// fix (query_staleness_test.go covers the keeper-level accessors directly).
func TestQueryServerViewMethodsReachableThroughGRPC(t *testing.T) {
	k, bank := setupCollectionKeeper(t)
	fund(bank, accAddr(t, ownerA), 1_000_000)
	querySrv := NewGRPCQueryServer(k)
	msgSrv := NewGRPCMsgServer(k)

	regRes, err := msgSrv.SendToNameCollection(sdk.Context{}.WithBlockHeight(1), &types.MsgSendToNameCollection{
		Sender: ownerA, Opcode: types.OpcodeRegister, Comment: "alice", AmountNaet: 5000,
	})
	require.NoError(t, err)
	require.True(t, regRes.AuctionOpened)

	ctx := context.Background()

	t.Run("CollectionParams", func(t *testing.T) {
		res, err := querySrv.CollectionParams(ctx, &types.QueryCollectionParamsRequest{})
		require.NoError(t, err)
		require.Equal(t, "aet", res.RootNamespace)
		require.NotEmpty(t, res.MinLabelLens)
		require.NotEmpty(t, res.PricesNaet)
	})

	t.Run("CollectionBalance", func(t *testing.T) {
		res, err := querySrv.CollectionBalance(ctx, &types.QueryCollectionBalanceRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(5000), res.EscrowedNaet)
	})

	t.Run("PriceForLabel", func(t *testing.T) {
		res, err := querySrv.PriceForLabel(ctx, &types.QueryPriceForLabelRequest{Label: "alice"})
		require.NoError(t, err)
		require.True(t, res.Found)
		require.Equal(t, "5000", res.PriceNaet)
	})

	t.Run("Auctions", func(t *testing.T) {
		res, err := querySrv.Auctions(ctx, &types.QueryAuctionsRequest{})
		require.NoError(t, err)
		require.Len(t, res.Auctions, 1)
		require.Equal(t, "alice.aet", res.Auctions[0].Name)
	})

	t.Run("Auction", func(t *testing.T) {
		res, err := querySrv.Auction(ctx, &types.QueryAuctionRequest{Name: "alice"})
		require.NoError(t, err)
		require.True(t, res.Found)
		require.Equal(t, types.AuctionKindIssuance, res.Auction.Kind)
	})

	t.Run("DomainStatus (in auction, not yet granted)", func(t *testing.T) {
		res, err := querySrv.DomainStatus(ctx, &types.QueryDomainStatusRequest{Name: "alice", Height: 1})
		require.NoError(t, err)
		require.False(t, res.Found)
		require.True(t, res.InAuction)
	})

	runEndBlock(t, k, 11)

	t.Run("DomainStatus (granted)", func(t *testing.T) {
		res, err := querySrv.DomainStatus(ctx, &types.QueryDomainStatusRequest{Name: "alice", Height: 11})
		require.NoError(t, err)
		require.True(t, res.Found)
		require.True(t, res.Active)
		require.Equal(t, ownerA, res.Owner)
	})

	t.Run("NameRecord", func(t *testing.T) {
		res, err := querySrv.NameRecord(ctx, &types.QueryNameRecordRequest{Name: "alice"})
		require.NoError(t, err)
		require.True(t, res.Found)
		require.Equal(t, ownerA, res.Owner)
	})

	t.Run("ResolveName", func(t *testing.T) {
		res, err := querySrv.ResolveName(ctx, &types.QueryResolveNameRequest{Name: "alice", Height: 11})
		require.NoError(t, err)
		require.True(t, res.Found)
		require.True(t, res.Active)
	})

	_, err = k.SetReverseRecord(types.MsgSetReverseRecord{Owner: ownerA, Address: ownerA, Name: "alice", Height: 12})
	require.NoError(t, err)

	t.Run("ReverseRecord", func(t *testing.T) {
		res, err := querySrv.ReverseRecord(ctx, &types.QueryReverseRecordRequest{Address: ownerA})
		require.NoError(t, err)
		require.True(t, res.Found)
		require.Equal(t, "alice.aet", res.Name)
	})

	_, err = k.CreateSubdomain(types.MsgCreateSubdomain{Owner: ownerA, ParentName: "alice", Label: "app", Height: 12})
	require.NoError(t, err)

	t.Run("Subdomains", func(t *testing.T) {
		res, err := querySrv.Subdomains(ctx, &types.QuerySubdomainsRequest{ParentName: "alice"})
		require.NoError(t, err)
		require.Equal(t, []string{"app.alice.aet"}, res.Names)
	})
}
