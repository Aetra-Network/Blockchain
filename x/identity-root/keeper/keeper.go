package keeper

import (
	"context"
	"errors"
	"math"
	"sync"

	corestore "cosmossdk.io/core/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/sovereign-l1/l1/x/identity-root/types"
	"github.com/sovereign-l1/l1/x/internal/prototype"
)

var genesisKey = []byte{0x01}

type GenesisState struct {
	Version		uint64
	Params		prototype.Params
	IdentityParams	types.IdentityRootParams
	State		types.IdentityRootState
}

// BankKeeper is the narrow bank interface the collection needs to custody
// deposits (SendCoinsFromAccountToModule), refund losing/underfunded bids
// (SendCoinsFromModuleToAccount), sweep to the treasury
// (SendCoinsFromModuleToModule) and measure its own balance (SpendableCoins).
// Funds enter ONLY via the message Amount -> SendCoinsFromAccountToModule; the
// reserved catalog address stays unfunded (CanReceiveUserFunds=false), the
// FINDING-017 stranding guard.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins
}

type Keeper struct {
	genesis		GenesisState
	storeService	corestore.KVStoreService
	// written and writtenResidual are this keeper's model of what the store
	// currently holds: the exact committed bytes of every per-record key, and
	// of the residual blob. writeDiff writes only what differs from them. Both
	// are re-established from the committed store by loadForBlock at the top of
	// every consensus entry point. See persistence.go.
	written		hotRecords
	writtenResidual	[]byte
	bankKeeper	BankKeeper
	// zoneResolver resolves the AEZ zone of a registered name (see
	// x/identity-root/keeper/zone.go's NameZoneResolver and WithZoneResolver).
	// A nil value is the default for every existing test keeper and degrades
	// NameZone to Resolved=false rather than failing, the same rule
	// x/native-account's zoneResolver follows.
	zoneResolver	NameZoneResolver
	runtimeCtx	context.Context
	// mu guards genesis (and the written baseline) against the concurrent
	// gRPC/REST query goroutines AND the Simulate RPC path racing the
	// single-threaded, ABCI-serialized msg-handler write path (FINDING-008).
	// It is a *sync.RWMutex, not a value: several methods are value receivers
	// that return a modified Keeper copy during wiring (WithBankKeeper), and a
	// pointer field lets every copy keep sharing the SAME lock. Every exported
	// mutator holds Lock for its whole body and persists inside it; every
	// exported reader holds RLock. The lock helpers are nil-safe so a
	// zero-value Keeper (var x Keeper) built by a test still works.
	mu *sync.RWMutex
}

func NewKeeper() Keeper {
	return Keeper{genesis: DefaultGenesis(), mu: &sync.RWMutex{}}
}

func NewPersistentKeeper(storeService corestore.KVStoreService) Keeper {
	return Keeper{genesis: DefaultGenesis(), storeService: storeService, mu: &sync.RWMutex{}}
}

// WithBankKeeper wires real bank custody. Without it the collection handlers
// and EndBlocker no-op every money movement (see hasCustody) and behave as a
// pure ledger, safe for the existing unit tests that don't construct a bank.
func (k Keeper) WithBankKeeper(bk BankKeeper) Keeper {
	k.bankKeeper = bk
	return k
}

// CollectionModuleAddress is the collection's real, bank-custodied cosmos
// module account -- distinct from the reserved catalog ("vanity") address
// AETIdentityRoot, which stays unfunded per the two-layer address model. This
// is the address that actually holds deposits and auction escrows.
func CollectionModuleAddress() sdk.AccAddress {
	return authtypes.NewModuleAddress(types.ModuleName)
}

// hasCustody reports whether real bank custody is wired. A keeper built without
// WithBankKeeper stays ledger-only (no bank movement, no sweep) -- every
// existing unit test constructs one this way.
func (k Keeper) hasCustody() bool {
	return k.bankKeeper != nil
}

func (k *Keeper) lockW() {
	if k.mu != nil {
		k.mu.Lock()
	}
}

func (k *Keeper) unlockW() {
	if k.mu != nil {
		k.mu.Unlock()
	}
}

func (k *Keeper) lockR() {
	if k.mu != nil {
		k.mu.RLock()
	}
}

func (k *Keeper) unlockR() {
	if k.mu != nil {
		k.mu.RUnlock()
	}
}

func DefaultGenesis() GenesisState {
	state := types.EmptyIdentityRootState()
	state.RootAuthorities = append(state.RootAuthorities, types.RootAuthority{Authority: prototype.DefaultAuthority, Role: "root"})
	return GenesisState{
		Version:	prototype.CurrentGenesisVersion,
		Params:		prototype.DefaultParams(),
		IdentityParams:	types.DefaultIdentityRootParams(),
		State:		state,
	}
}

func (gs GenesisState) Validate() error {
	if gs.Version != prototype.CurrentGenesisVersion {
		return errors.New("identity root prototype unsupported genesis version")
	}
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	return gs.State.Validate(gs.IdentityParams)
}

func (k *Keeper) InitGenesis(gs GenesisState) error {
	if err := gs.Validate(); err != nil {
		return err
	}
	if k.mu == nil {
		k.mu = &sync.RWMutex{}
	}
	k.lockW()
	defer k.unlockW()
	k.genesis = cloneGenesis(gs)
	return nil
}

func (k *Keeper) InitGenesisState(ctx context.Context, gs GenesisState) error {
	if err := gs.Validate(); err != nil {
		return err
	}
	if k.mu == nil {
		k.mu = &sync.RWMutex{}
	}
	k.lockW()
	defer k.unlockW()
	k.runtimeCtx = ctx
	k.genesis = cloneGenesis(gs)
	if k.storeService == nil {
		return nil
	}
	// writeReplacingState makes the store hold exactly gs, removing any records a
	// prior state held that this genesis does not mention. See persistence.go.
	return k.writeReplacingState(ctx, cloneGenesis(gs))
}

func (k *Keeper) ExportGenesis() GenesisState {
	k.lockR()
	defer k.unlockR()
	return cloneGenesis(k.genesis)
}

func (k *Keeper) ExportGenesisState(ctx context.Context) (GenesisState, error) {
	if k.storeService == nil {
		return k.ExportGenesis(), nil
	}
	gs, _, found, err := k.readGenesisState(ctx)
	if err != nil {
		return GenesisState{}, err
	}
	if !found {
		return k.ExportGenesis(), nil
	}
	gs = cloneGenesis(gs)
	if err := gs.Validate(); err != nil {
		return GenesisState{}, err
	}
	return gs, nil
}

// loadForBlock rehydrates the in-memory genesis and the write baseline from the
// committed store at the top of every consensus entry point (each Msg handler
// and the EndBlocker), and points runtimeCtx at the live block context. It MUST
// run before any mutation so a restarted or state-synced node -- where
// InitGenesis is not re-run -- operates on committed state instead of the empty
// default (the FINDING-006 consensus-halt class).
func (k *Keeper) loadForBlock(ctx context.Context) error {
	k.lockW()
	defer k.unlockW()
	k.runtimeCtx = ctx
	if k.storeService == nil {
		return nil
	}
	gs, baseline, found, err := k.readGenesisState(ctx)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	k.written = baseline.records
	k.writtenResidual = baseline.residual
	gs = cloneGenesis(gs)
	// FD-02: this used to be a full gs.Validate() -- an O(state) re-check of
	// bytes that were already fully validated when they were WRITTEN (every
	// write path either goes through the per-message incremental validators in
	// incremental_validate.go, or through a genesis/import boundary that runs
	// the full validator). Re-running it here on every single message/
	// EndBlocker was a SECOND full-state pass additive to the per-handler one.
	// Keep only the O(1) params check; committed record content is trusted.
	if err := gs.validateParamsOnly(); err != nil {
		return err
	}
	k.genesis = gs
	return nil
}

// validateParamsOnly is loadForBlock's lightweight substitute for the full
// GenesisState.Validate(): it checks the genesis version and both O(1) params
// structs but does NOT walk (or Export) the module's collections. Safe only
// because loadForBlock reads state that was already fully validated at write
// time (see incremental_validate.go); it must NOT be used at a genesis/import
// boundary where the state may be arbitrary/external.
func (gs GenesisState) validateParamsOnly() error {
	if gs.Version != prototype.CurrentGenesisVersion {
		return errors.New("identity root prototype unsupported genesis version")
	}
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	return gs.IdentityParams.Validate()
}

// persistLocked assigns next as the new in-memory genesis and, when custody is
// wired, writes the per-record diff to the store. Assumes k.mu is held (write)
// by the caller.
func (k *Keeper) persistLocked(next GenesisState) error {
	k.genesis = next
	if k.storeService == nil || k.runtimeCtx == nil {
		return nil
	}
	return k.writeDiff(k.runtimeCtx, k.genesis)
}

func (k *Keeper) RegisterName(msg types.MsgRegisterName) (types.NameRecord, error) {
	k.lockW()
	defer k.unlockW()
	if err := k.requireEnabled(); err != nil {
		return types.NameRecord{}, err
	}
	if msg.Height == 0 {
		return types.NameRecord{}, errors.New("identity registration height must be positive")
	}
	name, err := types.NormalizeName(msg.Name, k.genesis.IdentityParams.RootNamespace)
	if err != nil {
		return types.NameRecord{}, err
	}
	root, _ := types.NormalizeRootNamespace(k.genesis.IdentityParams.RootNamespace)
	if name == root {
		return types.NameRecord{}, errors.New("identity root namespace cannot be registered")
	}
	if err := types.ValidateUserFacingAEAddress("identity owner", msg.Owner); err != nil {
		return types.NameRecord{}, err
	}
	if _, _, found := recordIndex(k.genesis.State.Records, name); found {
		return types.NameRecord{}, errors.New("identity name already registered")
	}
	if isReserved(k.genesis.State.ReservedNames, name) && !isRootAuthority(k.genesis.State.RootAuthorities, msg.Owner) {
		return types.NameRecord{}, errors.New("identity reserved name cannot be registered by normal user")
	}
	expiry, err := addHeight(msg.Height, k.genesis.IdentityParams.RegistrationPeriod)
	if err != nil {
		return types.NameRecord{}, err
	}
	parent, err := types.ParentName(name, k.genesis.IdentityParams.RootNamespace)
	if err != nil {
		return types.NameRecord{}, err
	}
	// Restore the cross-record parent invariant the old full Validate()
	// enforced for every record with a non-empty ParentName (state.go:374-385):
	// RegisterName can mint an arbitrary-depth dotted name via types.ParentName
	// without ever going through CreateSubdomain's owned-parent check, so it
	// needs its own read-only lookup against the parent that already exists in
	// k.genesis (nothing has been mutated yet). See incremental_validate.go.
	if err := requireParentPolicySatisfied(k.genesis.State.Records, parent, name, msg.Owner); err != nil {
		return types.NameRecord{}, err
	}
	binding := prepareBinding(name, msg.Owner, msg.NFTBinding, k.genesis.IdentityParams)
	record := types.NameRecord{
		Name:				name,
		ParentName:			parent,
		Owner:				msg.Owner,
		ResolverRoot:			msg.ResolverRoot,
		ExpiryHeight:			expiry,
		RenewalHeight:			msg.Height,
		SubdomainPolicy:		msg.SubdomainPolicy,
		NFTBinding:			binding,
		LastStorageChargeHeight:	msg.Height,
		RentPayerPolicy:		nextDefaultRentPayerPolicy(k.genesis.IdentityParams),
		CreatedHeight:			msg.Height,
		UpdatedHeight:			msg.Height,
	}.Normalize(k.genesis.IdentityParams)
	next := cloneGenesisUnsorted(k.genesis)
	next.State.Records = append(next.State.Records, record)
	var resolver types.ResolverRecord
	hasResolver := record.ResolverRoot != types.DefaultResolverRoot
	if hasResolver {
		resolver = types.ResolverRecord{Name: name, ResolverRoot: record.ResolverRoot, UpdatedHeight: msg.Height}.Normalize(next.IdentityParams)
		next.State.Resolvers = upsertResolver(next.State.Resolvers, resolver, next.IdentityParams)
	}
	if binding.Enabled {
		next.State.NFTBindings = upsertBinding(next.State.NFTBindings, binding, next.IdentityParams)
	}
	// Incremental validation (FD-02): only what THIS message touched, not the
	// whole module state. See incremental_validate.go. Name uniqueness and the
	// reserved-name x owner cross-check are already enforced above (:257,:260)
	// before any state was built, so they need no re-check here.
	if err := validateGlobal(next); err != nil {
		return types.NameRecord{}, err
	}
	if err := record.Validate(next.IdentityParams); err != nil {
		return types.NameRecord{}, err
	}
	if hasResolver {
		if err := resolver.Validate(next.IdentityParams); err != nil {
			return types.NameRecord{}, err
		}
	}
	if binding.Enabled {
		if err := binding.Validate(next.IdentityParams); err != nil {
			return types.NameRecord{}, err
		}
	}
	if err := k.persistLocked(next); err != nil {
		return types.NameRecord{}, err
	}
	return record, nil
}

// RenewName extends a domain's term, but only inside the trailing renewal
// window and only while it is still active. A PURCHASE (auction win) resets the
// term to a fresh period elsewhere; renewal extends from the CURRENT
// ExpiryHeight (not from height) by RegistrationPeriod, so an early-but-in-window
// renewal loses no time. An expired domain cannot be renewed -- it must be
// re-acquired via the collection auction (REGISTER).
func (k *Keeper) RenewName(msg types.MsgRenewName) (types.NameRecord, error) {
	k.lockW()
	defer k.unlockW()
	if err := k.requireEnabled(); err != nil {
		return types.NameRecord{}, err
	}
	_, record, err := k.requireOwnedName(msg.Name, msg.Owner, msg.Height, false)
	if err != nil {
		return types.NameRecord{}, err
	}
	if msg.Height >= record.ExpiryHeight {
		return types.NameRecord{}, errors.New("identity expired name cannot be renewed; re-acquire it via the collection auction")
	}
	if record.ExpiryHeight-msg.Height > k.genesis.IdentityParams.RenewalWindowBlocks {
		return types.NameRecord{}, errors.New("identity name can only be renewed inside the renewal window before expiry")
	}
	expiry, err := addHeight(record.ExpiryHeight, k.genesis.IdentityParams.RegistrationPeriod)
	if err != nil {
		return types.NameRecord{}, err
	}
	record, err = accrueDomainRent(record, k.genesis.IdentityParams, msg.Height)
	if err != nil {
		return types.NameRecord{}, err
	}
	record.ExpiryHeight = expiry
	record.RenewalHeight = msg.Height
	record.UpdatedHeight = msg.Height
	next := cloneGenesisUnsorted(k.genesis)
	// next.State.Records is an independent deep copy (cloneGenesisUnsorted
	// still clones every collection; it only skips the sort -- see
	// types.IdentityRootState.ExportUnsortedHot), so an index captured against
	// k.genesis.State.Records before the clone is not guaranteed to still point
	// at the same slot in next. Re-resolve the index against next itself.
	idx, _, found := recordIndex(next.State.Records, record.Name)
	if !found {
		return types.NameRecord{}, errors.New("identity name vanished during renewal")
	}
	next.State.Records[idx] = record.Normalize(next.IdentityParams)
	if err := validateGlobal(next); err != nil {
		return types.NameRecord{}, err
	}
	if err := next.State.Records[idx].Validate(next.IdentityParams); err != nil {
		return types.NameRecord{}, err
	}
	if err := k.persistLocked(next); err != nil {
		return types.NameRecord{}, err
	}
	return record.Normalize(next.IdentityParams), nil
}

func (k *Keeper) TransferName(msg types.MsgTransferName) (types.NameRecord, error) {
	k.lockW()
	defer k.unlockW()
	if err := k.requireEnabled(); err != nil {
		return types.NameRecord{}, err
	}
	_, record, err := k.requireOwnedName(msg.Name, msg.Owner, msg.Height, true)
	if err != nil {
		return types.NameRecord{}, err
	}
	if err := types.ValidateUserFacingAEAddress("identity new owner", msg.NewOwner); err != nil {
		return types.NameRecord{}, err
	}
	record, err = accrueDomainRent(record, k.genesis.IdentityParams, msg.Height)
	if err != nil {
		return types.NameRecord{}, err
	}
	// MsgTransferName carries no NewNFTBinding field on the wire (see its doc
	// comment in types/state.go); prepareBinding degrades an empty reference
	// safely and is a no-op while NFTBindingEnabled stays false (the default).
	binding := prepareBinding(record.Name, msg.NewOwner, types.IdentityNFTBindingReference{}, k.genesis.IdentityParams)
	record.Owner = msg.NewOwner
	record.NFTBinding = binding
	record.UpdatedHeight = msg.Height
	next := cloneGenesisUnsorted(k.genesis)
	// See RenewName: the index must be resolved against next.State.Records (an
	// independent copy), not against any index captured before the clone.
	idx, _, found := recordIndex(next.State.Records, record.Name)
	if !found {
		return types.NameRecord{}, errors.New("identity name vanished during transfer")
	}
	next.State.Records[idx] = record.Normalize(next.IdentityParams)
	next.State.ReverseRecords = removeReverseByName(next.State.ReverseRecords, record.Name)
	// A domain SALE must not carry the reputation fee discount to the seller: the
	// attachment is exactly what AccountHoldsDomain (the ante fee gate) reads, so
	// clearing it on transfer drops the old target's discount and forces the new
	// owner to re-attach to gain it. removeAttachmentByName deletes the AttachKey
	// store entry via the same diff mechanism as the reverse-record clear above.
	// (Audit: reputation must be gated on live ownership, not carried on sale.)
	next.State.Attachments = removeAttachmentByName(next.State.Attachments, record.Name)
	if next.IdentityParams.NFTBindingEnabled {
		next.State.NFTBindings = upsertBinding(next.State.NFTBindings, binding, next.IdentityParams)
	}
	updated := next.State.Records[idx]
	// Incremental validation (FD-02): the reserved-owner and subdomain-policy
	// cross-checks below are the ONLY-caught-by-full-Validate invariants a
	// TransferName can newly break (a reserved name transferred to a
	// non-authority; an owner_only child moved off its parent's owner; a
	// parent moved out from under its owner_only children).
	if err := validateGlobal(next); err != nil {
		return types.NameRecord{}, err
	}
	if err := updated.Validate(next.IdentityParams); err != nil {
		return types.NameRecord{}, err
	}
	if err := checkReservedOwnership(next, updated.Name); err != nil {
		return types.NameRecord{}, err
	}
	if err := transferPreservesSubdomainOwnershipPolicy(next, updated.Name); err != nil {
		return types.NameRecord{}, err
	}
	if next.IdentityParams.NFTBindingEnabled {
		if err := binding.Validate(next.IdentityParams); err != nil {
			return types.NameRecord{}, err
		}
	}
	if err := k.persistLocked(next); err != nil {
		return types.NameRecord{}, err
	}
	return record.Normalize(next.IdentityParams), nil
}

func (k *Keeper) SetResolver(msg types.MsgSetResolver) (types.ResolverRecord, error) {
	k.lockW()
	defer k.unlockW()
	if err := k.requireEnabled(); err != nil {
		return types.ResolverRecord{}, err
	}
	_, record, err := k.requireOwnedName(msg.Name, msg.Owner, msg.Height, true)
	if err != nil {
		return types.ResolverRecord{}, err
	}
	record, err = accrueDomainRent(record, k.genesis.IdentityParams, msg.Height)
	if err != nil {
		return types.ResolverRecord{}, err
	}
	record.ResolverRoot = msg.ResolverRoot
	record.UpdatedHeight = msg.Height
	resolver := types.ResolverRecord{Name: record.Name, ResolverRoot: msg.ResolverRoot, UpdatedHeight: msg.Height}.Normalize(k.genesis.IdentityParams)
	next := cloneGenesisUnsorted(k.genesis)
	// See RenewName: resolve the index against next, not a pre-clone index.
	idx, _, found := recordIndex(next.State.Records, record.Name)
	if !found {
		return types.ResolverRecord{}, errors.New("identity name vanished during resolver update")
	}
	next.State.Records[idx] = record.Normalize(next.IdentityParams)
	next.State.Resolvers = upsertResolver(next.State.Resolvers, resolver, next.IdentityParams)
	if err := validateGlobal(next); err != nil {
		return types.ResolverRecord{}, err
	}
	if err := next.State.Records[idx].Validate(next.IdentityParams); err != nil {
		return types.ResolverRecord{}, err
	}
	if err := resolver.Validate(next.IdentityParams); err != nil {
		return types.ResolverRecord{}, err
	}
	if err := k.persistLocked(next); err != nil {
		return types.ResolverRecord{}, err
	}
	return resolver, nil
}

func (k *Keeper) SetReverseRecord(msg types.MsgSetReverseRecord) (types.ReverseRecord, error) {
	k.lockW()
	defer k.unlockW()
	if err := k.requireEnabled(); err != nil {
		return types.ReverseRecord{}, err
	}
	_, record, err := k.requireOwnedName(msg.Name, msg.Owner, msg.Height, true)
	if err != nil {
		return types.ReverseRecord{}, err
	}
	if err := types.ValidateUserFacingAEAddress("identity reverse address", msg.Address); err != nil {
		return types.ReverseRecord{}, err
	}
	reverse := types.ReverseRecord{Address: msg.Address, Name: record.Name, Owner: record.Owner, UpdatedHeight: msg.Height}.Normalize(k.genesis.IdentityParams)
	next := cloneGenesisUnsorted(k.genesis)
	next.State.ReverseRecords = upsertReverse(next.State.ReverseRecords, reverse, next.IdentityParams)
	if err := validateGlobal(next); err != nil {
		return types.ReverseRecord{}, err
	}
	if err := reverse.Validate(next.IdentityParams); err != nil {
		return types.ReverseRecord{}, err
	}
	if err := k.persistLocked(next); err != nil {
		return types.ReverseRecord{}, err
	}
	return reverse, nil
}

func (k *Keeper) CreateSubdomain(msg types.MsgCreateSubdomain) (types.NameRecord, error) {
	k.lockW()
	defer k.unlockW()
	if err := k.requireEnabled(); err != nil {
		return types.NameRecord{}, err
	}
	_, parent, err := k.requireOwnedName(msg.ParentName, msg.Owner, msg.Height, true)
	if err != nil {
		return types.NameRecord{}, err
	}
	if parent.SubdomainPolicy == types.SubdomainPolicyDisabled {
		return types.NameRecord{}, errors.New("identity parent disables subdomains")
	}
	subOwner := msg.SubdomainOwner
	if subOwner == "" {
		subOwner = msg.Owner
	}
	if err := types.ValidateUserFacingAEAddress("identity subdomain owner", subOwner); err != nil {
		return types.NameRecord{}, err
	}
	if parent.SubdomainPolicy == types.SubdomainPolicyOwnerOnly && subOwner != parent.Owner {
		return types.NameRecord{}, errors.New("identity subdomain ownership must follow parent policy")
	}
	if parent.SubdomainPolicy == types.SubdomainPolicyPublic && !k.genesis.IdentityParams.AllowPublicSubdomains && subOwner != parent.Owner {
		return types.NameRecord{}, errors.New("identity public subdomains are disabled")
	}
	name, err := types.ChildName(msg.Label, parent.Name, k.genesis.IdentityParams.RootNamespace)
	if err != nil {
		return types.NameRecord{}, err
	}
	if _, _, found := recordIndex(k.genesis.State.Records, name); found {
		return types.NameRecord{}, errors.New("identity subdomain already registered")
	}
	// ANS Phase B made MsgCreateSubdomain a flat wire type with no NFTBinding
	// field (a nested message would need its own descriptor and panics the
	// gogoproto table unmarshaler). A subdomain therefore never carries its own
	// binding over the wire; prepareBinding degrades an empty reference safely.
	binding := prepareBinding(name, subOwner, types.IdentityNFTBindingReference{}, k.genesis.IdentityParams)
	record := types.NameRecord{
		Name:				name,
		ParentName:			parent.Name,
		Owner:				subOwner,
		ResolverRoot:			msg.ResolverRoot,
		ExpiryHeight:			parent.ExpiryHeight,
		RenewalHeight:			msg.Height,
		SubdomainPolicy:		msg.SubdomainPolicy,
		NFTBinding:			binding,
		LastStorageChargeHeight:	msg.Height,
		RentPayerPolicy:		nextDefaultRentPayerPolicy(k.genesis.IdentityParams),
		CreatedHeight:			msg.Height,
		UpdatedHeight:			msg.Height,
	}.Normalize(k.genesis.IdentityParams)
	next := cloneGenesisUnsorted(k.genesis)
	next.State.Records = append(next.State.Records, record)
	if binding.Enabled {
		next.State.NFTBindings = upsertBinding(next.State.NFTBindings, binding, next.IdentityParams)
	}
	// Incremental validation (FD-02): the parent-policy match for THIS child
	// was already checked above (:469-474) before any state was built. The
	// reserved-owner cross-check is scoped here because CreateSubdomain can
	// mint a child whose FQDN happens to already be reserved.
	if err := validateGlobal(next); err != nil {
		return types.NameRecord{}, err
	}
	if err := record.Validate(next.IdentityParams); err != nil {
		return types.NameRecord{}, err
	}
	if err := checkReservedOwnership(next, record.Name); err != nil {
		return types.NameRecord{}, err
	}
	if binding.Enabled {
		if err := binding.Validate(next.IdentityParams); err != nil {
			return types.NameRecord{}, err
		}
	}
	if err := k.persistLocked(next); err != nil {
		return types.NameRecord{}, err
	}
	return record, nil
}

func (k *Keeper) ReserveName(msg types.MsgReserveName) (types.ReservedName, error) {
	k.lockW()
	defer k.unlockW()
	if err := k.requireAuthority(msg.Authority); err != nil {
		return types.ReservedName{}, err
	}
	reserved := types.ReservedName{Name: msg.Name, Authority: msg.Authority, Reason: msg.Reason}.Normalize(k.genesis.IdentityParams)
	if isReserved(k.genesis.State.ReservedNames, reserved.Name) {
		return types.ReservedName{}, errors.New("identity name already reserved")
	}
	next := cloneGenesisUnsorted(k.genesis)
	next.State.ReservedNames = append(next.State.ReservedNames, reserved)
	// Incremental validation (FD-02): reservation uniqueness is already
	// enforced above (isReserved guard); the ONLY-caught-by-full-Validate
	// invariant here is that this name may already be a registered record
	// owned by a normal user (see checkReservedOwnership).
	if err := validateGlobal(next); err != nil {
		return types.ReservedName{}, err
	}
	if err := reserved.Validate(next.IdentityParams); err != nil {
		return types.ReservedName{}, err
	}
	if err := checkReservedOwnership(next, reserved.Name); err != nil {
		return types.ReservedName{}, err
	}
	if err := k.persistLocked(next); err != nil {
		return types.ReservedName{}, err
	}
	return reserved, nil
}

func (k *Keeper) ReleaseReservedName(msg types.MsgReleaseReservedName) error {
	k.lockW()
	defer k.unlockW()
	if err := k.requireAuthority(msg.Authority); err != nil {
		return err
	}
	name, err := types.NormalizeName(msg.Name, k.genesis.IdentityParams.RootNamespace)
	if err != nil {
		return err
	}
	next := cloneGenesisUnsorted(k.genesis)
	var removed bool
	next.State.ReservedNames, removed = removeReserved(next.State.ReservedNames, name)
	if !removed {
		return errors.New("identity reserved name not found")
	}
	// Removal-only mutation: releasing a reserved name cannot newly violate any
	// invariant (see incremental_validate.go), so only the O(1) global guard
	// applies.
	if err := validateGlobal(next); err != nil {
		return err
	}
	if err := k.persistLocked(next); err != nil {
		return err
	}
	return nil
}

func (k *Keeper) NameRecord(name string) (types.NameRecord, bool, error) {
	k.lockR()
	defer k.unlockR()
	if err := k.genesis.Params.Validate(); err != nil {
		return types.NameRecord{}, false, err
	}
	name, err := types.NormalizeName(name, k.genesis.IdentityParams.RootNamespace)
	if err != nil {
		return types.NameRecord{}, false, err
	}
	_, record, found := recordIndex(k.genesis.State.Records, name)
	return record, found, nil
}

func (k *Keeper) ResolveName(name string, height uint64) (types.NameRecord, types.ResolverRecord, bool, error) {
	k.lockR()
	defer k.unlockR()
	if err := k.genesis.Params.Validate(); err != nil {
		return types.NameRecord{}, types.ResolverRecord{}, false, err
	}
	normalized, err := types.NormalizeName(name, k.genesis.IdentityParams.RootNamespace)
	if err != nil {
		return types.NameRecord{}, types.ResolverRecord{}, false, err
	}
	_, record, found := recordIndex(k.genesis.State.Records, normalized)
	if !found {
		return types.NameRecord{}, types.ResolverRecord{}, false, nil
	}
	if !types.IsActive(record, height) {
		return types.NameRecord{}, types.ResolverRecord{}, false, nil
	}
	_, resolver, resolverFound := resolverIndex(k.genesis.State.Resolvers, record.Name)
	if !resolverFound {
		resolver = types.ResolverRecord{Name: record.Name, ResolverRoot: record.ResolverRoot, UpdatedHeight: record.UpdatedHeight}
	}
	return record, resolver.Normalize(k.genesis.IdentityParams), true, nil
}

func (k *Keeper) ReverseRecord(address string) (types.ReverseRecord, bool, error) {
	k.lockR()
	defer k.unlockR()
	if err := k.genesis.Params.Validate(); err != nil {
		return types.ReverseRecord{}, false, err
	}
	_, reverse, found := reverseIndex(k.genesis.State.ReverseRecords, address)
	return reverse, found, nil
}

func (k *Keeper) Subdomains(parentName string) ([]types.NameRecord, error) {
	k.lockR()
	defer k.unlockR()
	if err := k.genesis.Params.Validate(); err != nil {
		return nil, err
	}
	parentName, err := types.NormalizeName(parentName, k.genesis.IdentityParams.RootNamespace)
	if err != nil {
		return nil, err
	}
	out := make([]types.NameRecord, 0)
	for _, record := range k.genesis.State.Export().Records {
		if record.ParentName == parentName {
			out = append(out, record)
		}
	}
	types.SortRecords(out)
	return out, nil
}

func (k *Keeper) IdentityRootParams() (types.IdentityRootParams, error) {
	k.lockR()
	defer k.unlockR()
	if err := k.genesis.IdentityParams.Validate(); err != nil {
		return types.IdentityRootParams{}, err
	}
	return k.genesis.IdentityParams, nil
}

type Migrator struct {
	keeper *Keeper
}

func NewMigrator(k *Keeper) Migrator {
	return Migrator{keeper: k}
}

func (m Migrator) Migrate1to2() error {
	return m.keeper.ExportGenesis().Validate()
}

func (k *Keeper) Migrate1to2State(ctx context.Context) error {
	_, err := k.ExportGenesisState(ctx)
	return err
}

// Migrate2to3State fans the whole-state genesis blob (the v2 layout) out into
// the per-record KV keys the graduated module uses (see persistence.go).
// readGenesisState prefers the blob's copy when no per-record keys exist yet,
// so this eagerly rewrites the residual without the hot collections and Sets one
// record per domain/resolver/reverse/auction. Idempotent: a store already in the
// per-record layout reads back identically and writeReplacingState finds no diff.
func (k *Keeper) Migrate2to3State(ctx context.Context) error {
	if k.storeService == nil {
		return nil
	}
	if k.mu == nil {
		k.mu = &sync.RWMutex{}
	}
	k.lockW()
	defer k.unlockW()
	k.runtimeCtx = ctx
	gs, _, found, err := k.readGenesisState(ctx)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	gs = cloneGenesis(gs)
	if err := gs.Validate(); err != nil {
		return err
	}
	k.genesis = gs
	return k.writeReplacingState(ctx, gs)
}

func (k Keeper) requireEnabled() error {
	return k.genesis.Params.RequireEnabled()
}

func (k Keeper) requireAuthority(authority string) error {
	if err := k.genesis.Params.RequireEnabled(); err != nil {
		return err
	}
	return k.genesis.Params.Authorize(authority)
}

func (k Keeper) requireOwnedName(name, owner string, height uint64, requireActive bool) (int, types.NameRecord, error) {
	if height == 0 {
		return -1, types.NameRecord{}, errors.New("identity message height must be positive")
	}
	name, err := types.NormalizeName(name, k.genesis.IdentityParams.RootNamespace)
	if err != nil {
		return -1, types.NameRecord{}, err
	}
	index, record, found := recordIndex(k.genesis.State.Records, name)
	if !found {
		return -1, types.NameRecord{}, errors.New("identity name not found")
	}
	if record.Owner != owner {
		return -1, types.NameRecord{}, errors.New("identity name operation requires owner")
	}
	if requireActive && !types.IsActive(record, height) {
		return -1, types.NameRecord{}, errors.New("identity expired name cannot be used as active")
	}
	return index, record, nil
}

func prepareBinding(name, owner string, binding types.IdentityNFTBindingReference, params types.IdentityRootParams) types.IdentityNFTBindingReference {
	if !params.NFTBindingEnabled {
		return types.IdentityNFTBindingReference{Name: name}
	}
	binding.Name = name
	binding.Owner = owner
	return binding.Normalize(params)
}

func addHeight(base, delta uint64) (uint64, error) {
	if base > math.MaxUint64-delta {
		return 0, errors.New("identity height overflow")
	}
	return base + delta, nil
}

func nextDefaultRentPayerPolicy(params types.IdentityRootParams) string {
	if types.IsDomainRentPayerPolicy(params.DefaultDomainRentPayerPolicy) {
		return params.DefaultDomainRentPayerPolicy
	}
	return types.DomainRentPayerOwner
}

func accrueDomainRent(record types.NameRecord, params types.IdentityRootParams, height uint64) (types.NameRecord, error) {
	record = record.Normalize(params)
	delta, err := types.DomainStorageRentDelta(record, params, height)
	if err != nil {
		return types.NameRecord{}, err
	}
	if record.RentPayerPolicy == types.DomainRentPayerOwner {
		if record.StorageRentDebt > math.MaxUint64-delta {
			return types.NameRecord{}, errors.New("identity domain storage rent overflow")
		}
		record.StorageRentDebt += delta
	}
	record.LastStorageChargeHeight = height
	return record, nil
}

func recordIndex(records []types.NameRecord, name string) (int, types.NameRecord, bool) {
	for i, record := range records {
		if record.Name == name {
			return i, record, true
		}
	}
	return -1, types.NameRecord{}, false
}

func resolverIndex(records []types.ResolverRecord, name string) (int, types.ResolverRecord, bool) {
	for i, record := range records {
		if record.Name == name {
			return i, record, true
		}
	}
	return -1, types.ResolverRecord{}, false
}

func reverseIndex(records []types.ReverseRecord, address string) (int, types.ReverseRecord, bool) {
	for i, record := range records {
		if record.Address == address {
			return i, record, true
		}
	}
	return -1, types.ReverseRecord{}, false
}

func upsertResolver(records []types.ResolverRecord, resolver types.ResolverRecord, params types.IdentityRootParams) []types.ResolverRecord {
	resolver = resolver.Normalize(params)
	out := append([]types.ResolverRecord(nil), records...)
	if i, _, found := resolverIndex(out, resolver.Name); found {
		out[i] = resolver
	} else {
		out = append(out, resolver)
	}
	types.SortResolvers(out)
	return out
}

func upsertReverse(records []types.ReverseRecord, reverse types.ReverseRecord, params types.IdentityRootParams) []types.ReverseRecord {
	reverse = reverse.Normalize(params)
	out := append([]types.ReverseRecord(nil), records...)
	if i, _, found := reverseIndex(out, reverse.Address); found {
		out[i] = reverse
	} else {
		out = append(out, reverse)
	}
	types.SortReverseRecords(out)
	return out
}

func upsertBinding(bindings []types.IdentityNFTBindingReference, binding types.IdentityNFTBindingReference, params types.IdentityRootParams) []types.IdentityNFTBindingReference {
	binding = binding.Normalize(params)
	out := append([]types.IdentityNFTBindingReference(nil), bindings...)
	for i := range out {
		if out[i].Name == binding.Name {
			out[i] = binding
			types.SortBindings(out)
			return out
		}
	}
	out = append(out, binding)
	types.SortBindings(out)
	return out
}

func removeReverseByName(records []types.ReverseRecord, name string) []types.ReverseRecord {
	out := records[:0]
	for _, record := range records {
		if record.Name != name {
			out = append(out, record)
		}
	}
	return append([]types.ReverseRecord(nil), out...)
}

func isReserved(names []types.ReservedName, name string) bool {
	for _, reserved := range names {
		if reserved.Name == name {
			return true
		}
	}
	return false
}

func isRootAuthority(authorities []types.RootAuthority, authority string) bool {
	for _, root := range authorities {
		if root.Authority == authority {
			return true
		}
	}
	return false
}

func removeReserved(names []types.ReservedName, name string) ([]types.ReservedName, bool) {
	out := make([]types.ReservedName, 0, len(names))
	var removed bool
	for _, reserved := range names {
		if reserved.Name == name {
			removed = true
			continue
		}
		out = append(out, reserved)
	}
	return out, removed
}

func cloneGenesis(gs GenesisState) GenesisState {
	gs.State = gs.State.Export()
	return gs
}

// cloneGenesisUnsorted is cloneGenesis's cheap variant (FD-02 perf follow-up):
// used ONLY at a hot mutation handler's entry point (the `next :=
// cloneGenesisUnsorted(k.genesis)` line at the top of every RegisterName /
// RenewName / ... / AttachDomain / ... / SendToNameCollection / ... handler in
// this package). It still deep-clones all 8 collections -- preserving the
// discard-the-clone-on-error rollback safety net every handler relies on when
// it returns an error after building `next` but before persistLocked -- but
// skips the sort.SliceStable pass over the five collections whose in-memory
// order is not wire-significant (see
// types.IdentityRootState.ExportUnsortedHot's doc comment for why: each is
// stored as its own per-name/per-address KV key by persistence.go's writeDiff,
// which re-derives its bytes from ITS OWN cloneGenesis(next).Export() call
// independent of whatever order the handler produced).
//
// Do NOT use this at a genesis/import/migration boundary (InitGenesis,
// InitGenesisState, ExportGenesis(State), Migrate1to2/Migrate2to3State,
// GenesisState.Validate, module.ValidateGenesis) or in loadForBlock -- those
// remain on the fully-sorted cloneGenesis, where correctness against
// arbitrary/external state (not speed) is what matters.
func cloneGenesisUnsorted(gs GenesisState) GenesisState {
	gs.State = gs.State.ExportUnsortedHot()
	return gs
}
