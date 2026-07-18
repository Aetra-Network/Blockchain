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

func (q grpcQueryServer) CollectionParams(_ context.Context, req *types.QueryCollectionParamsRequest) (*types.QueryCollectionParamsResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity collection params query")
	}
	params, enabled, err := q.keeper.collectionParamsView()
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
	balance, escrow, retained := q.keeper.collectionBalanceView(ctx)
	return &types.QueryCollectionBalanceResponse{BalanceNaet: balance, EscrowedNaet: escrow, RetainedNaet: retained}, nil
}

func (q grpcQueryServer) PriceForLabel(_ context.Context, req *types.QueryPriceForLabelRequest) (*types.QueryPriceForLabelResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity price query")
	}
	price, found := q.keeper.priceForLabelView(req.Label)
	if !found {
		return &types.QueryPriceForLabelResponse{Found: false}, nil
	}
	return &types.QueryPriceForLabelResponse{Found: true, PriceNaet: price.String()}, nil
}

func (q grpcQueryServer) Auctions(_ context.Context, req *types.QueryAuctionsRequest) (*types.QueryAuctionsResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity auctions query")
	}
	auctions := q.keeper.auctionsView()
	out := make([]types.QueryAuction, 0, len(auctions))
	for _, auction := range auctions {
		out = append(out, types.AuctionView(auction))
	}
	return &types.QueryAuctionsResponse{Auctions: out}, nil
}

func (q grpcQueryServer) Auction(_ context.Context, req *types.QueryAuctionRequest) (*types.QueryAuctionResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity auction query")
	}
	auction, found, err := q.keeper.auctionView(req.Name)
	if err != nil {
		return nil, err
	}
	return &types.QueryAuctionResponse{Found: found, Auction: types.AuctionView(auction)}, nil
}

func (q grpcQueryServer) DomainStatus(_ context.Context, req *types.QueryDomainStatusRequest) (*types.QueryDomainStatusResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity domain status query")
	}
	return q.keeper.domainStatusView(req.Name, req.Height)
}

func (q grpcQueryServer) NameRecord(_ context.Context, req *types.QueryNameRecordRequest) (*types.QueryNameRecordResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity name record query")
	}
	record, found, err := q.keeper.NameRecord(req.Name)
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

func (q grpcQueryServer) ResolveName(_ context.Context, req *types.QueryResolveNameRequest) (*types.QueryResolveNameResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity resolve query")
	}
	record, resolver, active, err := q.keeper.ResolveName(req.Name, req.Height)
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

func (q grpcQueryServer) ReverseRecord(_ context.Context, req *types.QueryReverseRecordRequest) (*types.QueryReverseRecordResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity reverse record query")
	}
	reverse, found, err := q.keeper.ReverseRecord(req.Address)
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

func (q grpcQueryServer) Subdomains(_ context.Context, req *types.QuerySubdomainsRequest) (*types.QuerySubdomainsResponse, error) {
	if req == nil {
		return nil, errors.New("empty identity subdomains query")
	}
	records, err := q.keeper.Subdomains(req.ParentName)
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

func (k *Keeper) collectionParamsView() (types.IdentityRootParams, bool, error) {
	k.lockR()
	defer k.unlockR()
	if err := k.genesis.IdentityParams.Validate(); err != nil {
		return types.IdentityRootParams{}, false, err
	}
	return k.genesis.IdentityParams, k.genesis.Params.Enabled, nil
}

func (k *Keeper) collectionBalanceView(ctx context.Context) (uint64, uint64, uint64) {
	k.lockR()
	defer k.unlockR()
	escrow := openEscrowTotal(k.genesis.State.Auctions)
	if !k.hasCustody() {
		return 0, uintFromInt(escrow), 0
	}
	balance := k.bankKeeper.SpendableCoins(ctx, CollectionModuleAddress()).AmountOf(types.CollectionDenom)
	retained := balance.Sub(escrow)
	if retained.IsNegative() {
		retained = sdkmath.ZeroInt()
	}
	return uintFromInt(balance), uintFromInt(escrow), uintFromInt(retained)
}

func (k *Keeper) priceForLabelView(label string) (sdkmath.Int, bool) {
	k.lockR()
	defer k.unlockR()
	price, err := k.genesis.IdentityParams.PriceForLabel(label)
	if err != nil {
		return sdkmath.ZeroInt(), false
	}
	return price, true
}

func (k *Keeper) auctionsView() []types.Auction {
	k.lockR()
	defer k.unlockR()
	return k.genesis.State.Export().Auctions
}

func (k *Keeper) auctionView(name string) (types.Auction, bool, error) {
	k.lockR()
	defer k.unlockR()
	if err := k.genesis.Params.Validate(); err != nil {
		return types.Auction{}, false, err
	}
	normalized, err := types.NormalizeName(name, k.genesis.IdentityParams.RootNamespace)
	if err != nil {
		return types.Auction{}, false, err
	}
	_, auction, found := auctionIndex(k.genesis.State.Auctions, normalized)
	return auction, found, nil
}

func (k *Keeper) domainStatusView(name string, height uint64) (*types.QueryDomainStatusResponse, error) {
	k.lockR()
	defer k.unlockR()
	if err := k.genesis.Params.Validate(); err != nil {
		return nil, err
	}
	normalized, err := types.NormalizeName(name, k.genesis.IdentityParams.RootNamespace)
	if err != nil {
		return nil, err
	}
	_, inAuction := indexOfAuction(k.genesis.State.Auctions, normalized)
	_, record, found := recordIndex(k.genesis.State.Records, normalized)
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
