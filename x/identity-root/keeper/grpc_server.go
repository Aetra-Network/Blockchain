package keeper

import (
	"context"
	"errors"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/x/identity-root/types"
)

var (
	_	types.MsgServer		= grpcMsgServer{}
	_	types.QueryServer	= grpcQueryServer{}
)

type grpcMsgServer struct {
	types.UnimplementedMsgServer
	keeper *Keeper
}

type grpcQueryServer struct {
	keeper *Keeper
}

func NewGRPCMsgServer(k *Keeper) types.MsgServer	{ return grpcMsgServer{keeper: k} }
func NewGRPCQueryServer(k *Keeper) types.QueryServer	{ return grpcQueryServer{keeper: k} }

// --- Msg server. Every handler reloads committed state before mutating; the
// keeper method takes the write lock and persists the diff internally. ---

// blockHeight returns the consensus block height for the current context. Auction
// timing (OpenedHeight/DeadlineHeight/CreatedHeight) must be block-driven, never
// user-driven: the wire message carries a Height field that a caller controls, so
// every handler overwrites msg.Height with this value before the keeper runs.
// The overwrite is UNCONDITIONAL (unlike nominator-pool's defaultHeight, which
// only fills a zero value) so a non-zero attacker height can never pick its own
// deadline. Floored at 1 so the keeper's Height==0 guard is never tripped
// spuriously (e.g. a genesis/height-0 context).
func blockHeight(ctx context.Context) uint64 {
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if height <= 0 {
		return 1
	}
	return uint64(height)
}

func (m grpcMsgServer) SendToNameCollection(ctx context.Context, msg *types.MsgSendToNameCollection) (*types.MsgSendToNameCollectionResponse, error) {
	if msg == nil {
		return nil, errors.New("empty identity collection message")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	msg.Height = blockHeight(ctx)
	res, err := m.keeper.SendToNameCollection(*msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgSendToNameCollectionResponse{
		Outcome:	res.Outcome,
		Name:		res.Name,
		RefundNaet:	res.RefundNaet,
		FeeKeptNaet:	res.FeeKeptNaet,
		AuctionOpened:	res.AuctionOpened,
		DeadlineHeight:	res.DeadlineHeight,
	}, nil
}

func (m grpcMsgServer) PlaceBid(ctx context.Context, msg *types.MsgPlaceBid) (*types.MsgPlaceBidResponse, error) {
	if msg == nil {
		return nil, errors.New("empty identity bid message")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	msg.Height = blockHeight(ctx)
	res, err := m.keeper.PlaceBid(*msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgPlaceBidResponse{
		Name:			res.Name,
		HighBidNaet:		res.HighBidNaet,
		HighBidder:		res.HighBidder,
		RefundedPreviousNaet:	res.RefundedPreviousNaet,
		DeadlineHeight:		res.DeadlineHeight,
	}, nil
}

func (m grpcMsgServer) StartAuction(ctx context.Context, msg *types.MsgStartAuction) (*types.MsgStartAuctionResponse, error) {
	if msg == nil {
		return nil, errors.New("empty identity start-auction message")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	msg.Height = blockHeight(ctx)
	res, err := m.keeper.StartAuction(*msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgStartAuctionResponse{Name: res.Name, DeadlineHeight: res.DeadlineHeight}, nil
}

func (m grpcMsgServer) UpdatePriceTable(ctx context.Context, msg *types.MsgUpdatePriceTable) (*types.MsgUpdatePriceTableResponse, error) {
	if msg == nil {
		return nil, errors.New("empty identity price-table update message")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	tiers, err := m.keeper.UpdatePriceTable(*msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgUpdatePriceTableResponse{Tiers: uint32(tiers)}, nil
}

func (m grpcMsgServer) ListForSale(ctx context.Context, msg *types.MsgListForSale) (*types.MsgListForSaleResponse, error) {
	if msg == nil {
		return nil, errors.New("empty identity list-for-sale message")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	msg.Height = blockHeight(ctx)
	listing, err := m.keeper.ListForSale(*msg)
	if err != nil {
		return nil, err
	}
	price, err := listing.Price()
	if err != nil {
		return nil, err
	}
	return &types.MsgListForSaleResponse{Name: listing.Name, PriceNaet: uintFromInt(price)}, nil
}

func (m grpcMsgServer) DelistName(ctx context.Context, msg *types.MsgDelistName) (*types.MsgDelistNameResponse, error) {
	if msg == nil {
		return nil, errors.New("empty identity delist message")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	msg.Height = blockHeight(ctx)
	listing, err := m.keeper.DelistName(*msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgDelistNameResponse{Name: listing.Name}, nil
}

func (m grpcMsgServer) BuyListedName(ctx context.Context, msg *types.MsgBuyListedName) (*types.MsgBuyListedNameResponse, error) {
	if msg == nil {
		return nil, errors.New("empty identity buy-listed-name message")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	msg.Height = blockHeight(ctx)
	outcome, err := m.keeper.BuyListedName(*msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgBuyListedNameResponse{Name: outcome.Name, Owner: outcome.Owner, PriceNaet: outcome.PriceNaet}, nil
}

func (m grpcMsgServer) AttachDomain(ctx context.Context, msg *types.MsgAttachDomain) (*types.MsgAttachDomainResponse, error) {
	if msg == nil {
		return nil, errors.New("empty identity attach message")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	msg.Height = blockHeight(ctx)
	attachment, err := m.keeper.AttachDomain(*msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgAttachDomainResponse{Fqdn: attachment.Fqdn, Target: attachment.Target}, nil
}

func (m grpcMsgServer) DetachDomain(ctx context.Context, msg *types.MsgDetachDomain) (*types.MsgDetachDomainResponse, error) {
	if msg == nil {
		return nil, errors.New("empty identity detach message")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	msg.Height = blockHeight(ctx)
	attachment, err := m.keeper.DetachDomain(*msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgDetachDomainResponse{Fqdn: attachment.Fqdn}, nil
}

func (m grpcMsgServer) DisownAttachment(ctx context.Context, msg *types.MsgDisownAttachment) (*types.MsgDisownAttachmentResponse, error) {
	if msg == nil {
		return nil, errors.New("empty identity disown message")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	msg.Height = blockHeight(ctx)
	attachment, err := m.keeper.DisownAttachment(*msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgDisownAttachmentResponse{Fqdn: attachment.Fqdn, Target: attachment.Target}, nil
}

func (m grpcMsgServer) CreateSubdomain(ctx context.Context, msg *types.MsgCreateSubdomain) (*types.MsgCreateSubdomainResponse, error) {
	if msg == nil {
		return nil, errors.New("empty identity create-subdomain message")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	msg.Height = blockHeight(ctx)
	record, err := m.keeper.CreateSubdomain(*msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgCreateSubdomainResponse{Name: record.Name, ExpiryHeight: record.ExpiryHeight}, nil
}

func (m grpcMsgServer) RenewName(ctx context.Context, msg *types.MsgRenewName) (*types.MsgRenewNameResponse, error) {
	if msg == nil {
		return nil, errors.New("empty identity renew message")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	msg.Height = blockHeight(ctx)
	record, err := m.keeper.RenewName(*msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgRenewNameResponse{Name: record.Name, ExpiryHeight: record.ExpiryHeight}, nil
}

func (m grpcMsgServer) TransferName(ctx context.Context, msg *types.MsgTransferName) (*types.MsgTransferNameResponse, error) {
	if msg == nil {
		return nil, errors.New("empty identity transfer message")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	msg.Height = blockHeight(ctx)
	record, err := m.keeper.TransferName(*msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgTransferNameResponse{Name: record.Name, Owner: record.Owner}, nil
}

func (m grpcMsgServer) SetResolver(ctx context.Context, msg *types.MsgSetResolver) (*types.MsgSetResolverResponse, error) {
	if msg == nil {
		return nil, errors.New("empty identity set-resolver message")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	msg.Height = blockHeight(ctx)
	resolver, err := m.keeper.SetResolver(*msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgSetResolverResponse{Name: resolver.Name, ResolverRoot: resolver.ResolverRoot}, nil
}

func (m grpcMsgServer) SetReverseRecord(ctx context.Context, msg *types.MsgSetReverseRecord) (*types.MsgSetReverseRecordResponse, error) {
	if msg == nil {
		return nil, errors.New("empty identity set-reverse-record message")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	msg.Height = blockHeight(ctx)
	reverse, err := m.keeper.SetReverseRecord(*msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgSetReverseRecordResponse{Address: reverse.Address, Name: reverse.Name}, nil
}

// ReserveName and ReleaseReservedName carry no Height field: the keeper methods
// they call are governance-authority actions (requireAuthority), not
// block-timed domain operations, so there is no msg.Height to overwrite with
// blockHeight(ctx) here -- unlike every owner-signed handler above.

func (m grpcMsgServer) ReserveName(ctx context.Context, msg *types.MsgReserveName) (*types.MsgReserveNameResponse, error) {
	if msg == nil {
		return nil, errors.New("empty identity reserve message")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	reserved, err := m.keeper.ReserveName(*msg)
	if err != nil {
		return nil, err
	}
	return &types.MsgReserveNameResponse{Name: reserved.Name, Authority: reserved.Authority}, nil
}

func (m grpcMsgServer) ReleaseReservedName(ctx context.Context, msg *types.MsgReleaseReservedName) (*types.MsgReleaseReservedNameResponse, error) {
	if msg == nil {
		return nil, errors.New("empty identity release-reserved message")
	}
	if err := m.keeper.loadForBlock(ctx); err != nil {
		return nil, err
	}
	if err := m.keeper.ReleaseReservedName(*msg); err != nil {
		return nil, err
	}
	return &types.MsgReleaseReservedNameResponse{Name: msg.Name}, nil
}

// --- Query server. Read-only; the keeper accessors take the read lock. ---

func (q grpcQueryServer) CollectionParams(ctx context.Context, req *types.QueryCollectionParamsRequest) (*types.QueryCollectionParamsResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity collection params query")
	}
	params, enabled, err := q.keeper.collectionParamsView(ctx)
	if err != nil {
		return nil, err
	}
	resp := &types.QueryCollectionParamsResponse{
		RootNamespace:			params.RootNamespace,
		Enabled:			enabled,
		CollectionFeeNaet:		params.CollectionFeeNaet,
		RegistrationPeriodBlocks:	params.RegistrationPeriod,
		RenewalWindowBlocks:		params.RenewalWindowBlocks,
		IssuanceAuctionDurationBlocks:	params.IssuanceAuctionDurationBlocks,
		MinBidRaisePctBps:		params.MinBidRaisePctBps,
		SweepIntervalBlocks:		params.SweepIntervalBlocks,
		SweepFloorNaet:			params.SweepFloorNaet,
		TreasuryModuleName:		params.TreasuryModuleName,
	}
	for _, tier := range params.PriceTable {
		resp.MinLabelLens = append(resp.MinLabelLens, tier.MinLabelLen)
		resp.PricesNaet = append(resp.PricesNaet, tier.PriceNaet)
	}
	return resp, nil
}

func (q grpcQueryServer) CollectionBalance(ctx context.Context, req *types.QueryCollectionBalanceRequest) (*types.QueryCollectionBalanceResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity collection balance query")
	}
	balance, escrow, retained, err := q.keeper.collectionBalanceView(ctx)
	if err != nil {
		return nil, err
	}
	return &types.QueryCollectionBalanceResponse{BalanceNaet: balance, EscrowedNaet: escrow, RetainedNaet: retained}, nil
}

func (q grpcQueryServer) PriceForLabel(ctx context.Context, req *types.QueryPriceForLabelRequest) (*types.QueryPriceForLabelResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity price query")
	}
	price, found, err := q.keeper.priceForLabelView(ctx, req.Label)
	if err != nil {
		return nil, err
	}
	if !found {
		return &types.QueryPriceForLabelResponse{Found: false}, nil
	}
	return &types.QueryPriceForLabelResponse{Found: true, PriceNaet: price.String()}, nil
}

// Auctions returns every open auction, unpaginated.
//
// Known, deferred gap (audit: DoS resource-limits pass, LOW/informational):
// neither this nor Subdomains below takes a
// cosmos.base.query.v1beta1.PageRequest, so a large result set (Auctions is
// now bounded by MaxAuctions -- see collection.go's capacity guards and
// types/state.go's MaxAuctions field, but a busy chain could still have many
// thousands open; Subdomains has no cap at all) is returned in one response.
// Correctly scoped as low severity: gRPC/REST query paths run outside
// consensus gas metering and are node-operator-rate-limitable, unlike a Msg
// handler. Not implemented in this pass because both request/response types
// are hand-rolled (no protoc/buf toolchain wired to this tree -- see
// types/query.go's own doc comment), so adding PageRequest/PageResponse
// fields means hand-editing the gogoproto field-number descriptors those
// types carry; doing that correctly needs its own focused pass, not a
// drive-by addition alongside unrelated fixes. Deferred as a scoped
// follow-up: add PageRequest/PageResponse to QueryAuctionsRequest/Response
// and QuerySubdomainsRequest/Response with a server-side default+max page
// size clamp in both handlers.
func (q grpcQueryServer) Auctions(ctx context.Context, req *types.QueryAuctionsRequest) (*types.QueryAuctionsResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity auctions query")
	}
	auctions, err := q.keeper.auctionsView(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]types.QueryAuction, 0, len(auctions))
	for _, auction := range auctions {
		out = append(out, types.AuctionView(auction))
	}
	return &types.QueryAuctionsResponse{Auctions: out}, nil
}

func (q grpcQueryServer) Auction(ctx context.Context, req *types.QueryAuctionRequest) (*types.QueryAuctionResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity auction query")
	}
	auction, found, err := q.keeper.auctionView(ctx, req.Name)
	if err != nil {
		return nil, err
	}
	return &types.QueryAuctionResponse{Found: found, Auction: types.AuctionView(auction)}, nil
}

func (q grpcQueryServer) Listing(ctx context.Context, req *types.QueryListingRequest) (*types.QueryListingResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity listing query")
	}
	listing, found, err := q.keeper.listingView(ctx, req.Name)
	if err != nil {
		return nil, err
	}
	if !found {
		return &types.QueryListingResponse{Found: false}, nil
	}
	return &types.QueryListingResponse{Found: true, Listing: types.ListingView(listing)}, nil
}

func (q grpcQueryServer) DomainStatus(ctx context.Context, req *types.QueryDomainStatusRequest) (*types.QueryDomainStatusResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity domain status query")
	}
	return q.keeper.domainStatusView(ctx, req.Name, req.Height)
}

func (q grpcQueryServer) NameRecord(ctx context.Context, req *types.QueryNameRecordRequest) (*types.QueryNameRecordResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity name record query")
	}
	record, found, err := q.keeper.NameRecord(ctx, req.Name)
	if err != nil {
		return nil, err
	}
	if !found {
		return &types.QueryNameRecordResponse{Found: false}, nil
	}
	return &types.QueryNameRecordResponse{
		Found:		true,
		Name:		record.Name,
		Owner:		record.Owner,
		ResolverRoot:	record.ResolverRoot,
		ExpiryHeight:	record.ExpiryHeight,
		ParentName:	record.ParentName,
	}, nil
}

func (q grpcQueryServer) ResolveName(ctx context.Context, req *types.QueryResolveNameRequest) (*types.QueryResolveNameResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity resolve query")
	}
	record, resolver, active, err := q.keeper.ResolveName(ctx, req.Name, req.Height)
	if err != nil {
		return nil, err
	}
	if !active {
		return &types.QueryResolveNameResponse{Found: false, Active: false}, nil
	}
	return &types.QueryResolveNameResponse{
		Found:		true,
		Active:		true,
		Name:		record.Name,
		ResolverRoot:	resolver.ResolverRoot,
	}, nil
}

func (q grpcQueryServer) ReverseRecord(ctx context.Context, req *types.QueryReverseRecordRequest) (*types.QueryReverseRecordResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity reverse record query")
	}
	reverse, found, err := q.keeper.ReverseRecord(ctx, req.Address)
	if err != nil {
		return nil, err
	}
	if !found {
		return &types.QueryReverseRecordResponse{Found: false}, nil
	}
	return &types.QueryReverseRecordResponse{Found: true, Name: reverse.Name, Owner: reverse.Owner}, nil
}

func (q grpcQueryServer) NameZone(ctx context.Context, req *types.QueryNameZoneRequest) (*types.QueryNameZoneResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity name zone query")
	}
	nz, err := q.keeper.NameZone(ctx, req.Name)
	if err != nil {
		return nil, err
	}
	return &types.QueryNameZoneResponse{Resolved: nz.Resolved, Zone: nz.Zone, Bucket: nz.Bucket}, nil
}

func (q grpcQueryServer) Subdomains(ctx context.Context, req *types.QuerySubdomainsRequest) (*types.QuerySubdomainsResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity subdomains query")
	}
	records, err := q.keeper.Subdomains(ctx, req.ParentName)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(records))
	for _, record := range records {
		names = append(names, record.Name)
	}
	return &types.QuerySubdomainsResponse{Names: names}, nil
}

// --- keeper read accessors used only by the query server (lockR). ---

func (k *Keeper) collectionParamsView(ctx context.Context) (types.IdentityRootParams, bool, error) {
	gs, err := k.viewGenesis(ctx)
	if err != nil {
		return types.IdentityRootParams{}, false, err
	}
	if err := gs.IdentityParams.Validate(); err != nil {
		return types.IdentityRootParams{}, false, err
	}
	return gs.IdentityParams, gs.Params.Enabled, nil
}

func (k *Keeper) collectionBalanceView(ctx context.Context) (uint64, uint64, uint64, error) {
	gs, err := k.viewGenesis(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	escrow := openEscrowTotal(gs.State.Auctions)
	if !k.hasCustody() {
		return 0, uintFromInt(escrow), 0, nil
	}
	balance := k.bankKeeper.SpendableCoins(ctx, CollectionModuleAddress()).AmountOf(types.CollectionDenom)
	retained := balance.Sub(escrow)
	if retained.IsNegative() {
		retained = sdkmath.ZeroInt()
	}
	return uintFromInt(balance), uintFromInt(escrow), uintFromInt(retained), nil
}

func (k *Keeper) priceForLabelView(ctx context.Context, label string) (sdkmath.Int, bool, error) {
	gs, err := k.viewGenesis(ctx)
	if err != nil {
		return sdkmath.ZeroInt(), false, err
	}
	price, err := gs.IdentityParams.PriceForLabel(label)
	if err != nil {
		return sdkmath.ZeroInt(), false, nil
	}
	return price, true, nil
}

func (k *Keeper) auctionsView(ctx context.Context) ([]types.Auction, error) {
	gs, err := k.viewGenesis(ctx)
	if err != nil {
		return nil, err
	}
	// gs is already a cloneGenesis()-produced snapshot (viewGenesis), so
	// gs.State.Auctions is already exported/sorted; no need to Export() again.
	return gs.State.Auctions, nil
}

func (k *Keeper) auctionView(ctx context.Context, name string) (types.Auction, bool, error) {
	gs, err := k.viewGenesis(ctx)
	if err != nil {
		return types.Auction{}, false, err
	}
	if err := gs.Params.Validate(); err != nil {
		return types.Auction{}, false, err
	}
	normalized, err := types.NormalizeName(name, gs.IdentityParams.RootNamespace)
	if err != nil {
		return types.Auction{}, false, err
	}
	_, auction, found := auctionIndex(gs.State.Auctions, normalized)
	return auction, found, nil
}

func (k *Keeper) domainStatusView(ctx context.Context, name string, height uint64) (*types.QueryDomainStatusResponse, error) {
	gs, err := k.viewGenesis(ctx)
	if err != nil {
		return nil, err
	}
	if err := gs.Params.Validate(); err != nil {
		return nil, err
	}
	normalized, err := types.NormalizeName(name, gs.IdentityParams.RootNamespace)
	if err != nil {
		return nil, err
	}
	_, inAuction := indexOfAuction(gs.State.Auctions, normalized)
	_, record, found := recordIndex(gs.State.Records, normalized)
	if !found {
		return &types.QueryDomainStatusResponse{Found: false, InAuction: inAuction}, nil
	}
	return &types.QueryDomainStatusResponse{
		Found:		true,
		Active:		types.IsActive(record, height),
		InAuction:	inAuction,
		Owner:		record.Owner,
		ExpiryHeight:	record.ExpiryHeight,
	}, nil
}

func indexOfAuction(auctions []types.Auction, name string) (int, bool) {
	i, _, found := auctionIndex(auctions, name)
	return i, found
}
