package keeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strings"
	"sync"
	"time"

	corestore "cosmossdk.io/core/store"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/internal/prototype"
	"github.com/sovereign-l1/l1/x/nominator-pool/types"
)

// BankKeeper is the narrow bank interface the pool needs to actually custody
// deposits and pay out withdrawals/rewards -- see #2/SA2-N01: before this,
// the module had no bankKeeper at all and every "deposit" was a pure ledger
// entry with no matching bank debit.
type BankKeeper interface {
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins
}

// StakingKeeper is the narrow x/staking interface the pool needs to actually
// delegate deposited funds to pool.ValidatorTarget and undelegate on
// withdrawal, instead of tracking TotalBondedStake as a number with no real
// stake behind it.
type StakingKeeper interface {
	GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
	Delegate(ctx context.Context, delAddr sdk.AccAddress, bondAmt sdkmath.Int, tokenSrc stakingtypes.BondStatus, validator stakingtypes.Validator, subtractAccount bool) (sdkmath.LegacyDec, error)
	Undelegate(ctx context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress, sharesAmount sdkmath.LegacyDec) (time.Time, sdkmath.Int, error)
	ValidateUnbondAmount(ctx context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress, amt sdkmath.Int) (sdkmath.LegacyDec, error)
	// GetUnbondingDelegation lets settlement see whether x/staking still holds
	// money in flight for the pool. It is the only honest answer to "has
	// everything this cohort is ever going to receive already arrived?" --
	// x/staking's CompleteUnbonding removes an entry the moment it credits the
	// pool account, so "no entry older than us remains" is exactly that
	// question. Returns stakingtypes.ErrNoUnbondingDelegation when the pool
	// has nothing unbonding from this validator at all.
	GetUnbondingDelegation(ctx context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress) (stakingtypes.UnbondingDelegation, error)
}

// DistrKeeper is the narrow x/distribution interface the pool needs to pull
// real staking rewards earned by its delegation into the pool's module
// account before distributing them to depositors proportionally.
type DistrKeeper interface {
	WithdrawDelegationRewards(ctx context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress) (sdk.Coins, error)
}

// PoolModuleAddress is the pool's real, bank-custodied cosmos module account
// -- distinct from the reserved catalog ("vanity") address AETNominatorPool,
// which stays unfunded per the two-layer address model (see
// app/accounts/module_accounts.go). This is the address that actually holds
// deposits and delegates to validators.
func PoolModuleAddress() sdk.AccAddress {
	return authtypes.NewModuleAddress(types.ModuleName)
}

var genesisKey = []byte{0x01}

type GenesisState struct {
	Version uint64
	Params  types.Params
	State   types.State
}

type OperationCounters struct {
	PoolLookups              uint64
	DelegatorLookups         uint64
	DelegatorRewardUpdates   uint64
	ValidatorAllocationReads uint64
	ProofQueries             uint64
}

const (
	accountStatusActive   = "active"
	accountStatusInactive = "inactive"
	accountStatusFrozen   = "frozen"
)

type AccountStatusReader interface {
	AccountStatus(address string) (string, bool)
}

type poolIndexEntry struct {
	index     int
	delegator map[string]int
}

type Keeper struct {
	genesis GenesisState
	// written is exactly what this keeper last read from, or wrote to, the
	// store -- the baseline writeDiff compares against to decide which
	// per-entity records actually need rewriting. It is deliberately
	// separate from genesis: several helpers mutate genesis in place, and a
	// baseline that moved with them would silently skip real writes.
	// loadGenesisState re-establishes it from committed state at the top of
	// every consensus entry point, which is what keeps the write set (and
	// therefore gas) a pure function of committed state plus message. See
	// persistence.go.
	written             GenesisState
	storeService        corestore.KVStoreService
	runtimeCtx          context.Context
	accountStatusReader AccountStatusReader
	bankKeeper          BankKeeper
	stakingKeeper       StakingKeeper
	distrKeeper         DistrKeeper
	indexes             map[string]poolIndexEntry
	counters            OperationCounters
	// mu guards genesis, indexes, and counters against the concurrent
	// gRPC/REST query goroutines AND the Simulate RPC path racing the
	// (single-threaded, ABCI-serialized) msg-handler write path. baseapp
	// runs message execution for execModeSimulate (the
	// /cosmos.tx.v1beta1.Service/Simulate endpoint, served on a query
	// goroutine) through the SAME msg-handler code that execModeFinalize
	// uses. That means a client hammering the public Simulate RPC with
	// nominator-pool messages runs rebuildIndexes() (which reassigns
	// k.indexes -- a WRITE to a shared map) concurrently with a real
	// block's FinalizeBlock executing the same code on the consensus
	// goroutine. Without this lock that is a concurrent Go map
	// read/write, which is an unrecoverable runtime.throw (recover()
	// cannot catch it) and crashes the validator process outright. See
	// F-14 / FINDING-014-nominator-pool-concurrent-map-crash.md.
	//
	// It is a *sync.RWMutex, not a value, deliberately -- mirroring the
	// pattern x/contracts/keeper uses for the same class of bug
	// (FINDING-008): several methods below are value receivers (pure
	// read-only helpers invoked only while a caller already holds this
	// lock) that operate on copies of Keeper, and a pointer field lets
	// every copy keep sharing the SAME lock instead of `go vet` flagging
	// a copied sync.Mutex, and instead of every value-receiver copy
	// silently getting its own independent (i.e. useless) lock.
	//
	// Locking convention for this file: every EXPORTED method that reads
	// or writes genesis/indexes/counters acquires mu itself (Lock for
	// anything that mutates OR that can reach ensureIndexes/rebuildIndexes
	// -- see below; RLock for the handful of pure getters that cannot)
	// and holds it for the method's ENTIRE body, not just around
	// individual field assignments -- a load-check-use sequence has to be
	// atomic as a whole or a concurrent rebuildIndexes can still land
	// in the middle of it. Every unexported (lowercase) helper --
	// loadForBlock's callees, rebuildIndexes, ensureIndexes, lookupPool,
	// lookupDelegator, saveGenesis, savePool(Only), the upsertXxx
	// helpers, accrueOfficialPoolRent -- assumes the caller already
	// holds the appropriate lock and never locks itself, to avoid
	// non-reentrant self-deadlock. A handful of exported methods call
	// another exported method internally (e.g. DepositToStakingPool ->
	// DepositToOfficialLiquidStaking); those inner methods are split into
	// a thin locking wrapper plus an unexported *Locked implementation,
	// and the outer caller invokes the *Locked form directly so the lock
	// is only ever acquired once per external entry point.
	//
	// ensureIndexes conditionally calls rebuildIndexes (a write) even
	// from what looks like a pure lookup, so every path that can reach
	// lookupPool/lookupDelegator -- including the read-only NominatorPool/
	// PoolDelegator/PoolRewards/PoolShare/PoolAllocations queries and
	// StakingProof/ClaimPoolRewards/SyncPoolRewards -- takes the WRITE
	// lock, not RLock: two callers each holding only RLock could both
	// decide a rebuild is needed and both call rebuildIndexes
	// concurrently, reproducing the exact crash this field exists to
	// prevent.
	mu *sync.RWMutex
}

func NewKeeper() Keeper {
	k := Keeper{genesis: DefaultGenesis(), mu: &sync.RWMutex{}}
	k.rebuildIndexes()
	return k
}

func NewKeeperWithAccountStatus(reader AccountStatusReader) Keeper {
	k := NewKeeper()
	k.accountStatusReader = reader
	return k
}

func (k Keeper) WithAccountStatusReader(reader AccountStatusReader) Keeper {
	k.accountStatusReader = reader
	return k
}

// WithBankKeeper wires real bank custody (#2/SA2-N01): without it, deposit
// and withdrawal handlers below no-op the bank movement (see hasCustody)
// and behave exactly as before -- a ledger-only pool, safe for tests that
// don't construct one.
func (k Keeper) WithBankKeeper(bk BankKeeper) Keeper {
	k.bankKeeper = bk
	return k
}

// WithStakingKeeper wires real delegation of pooled deposits to
// pool.ValidatorTarget. Requires WithBankKeeper too -- see hasCustody.
func (k Keeper) WithStakingKeeper(sk StakingKeeper) Keeper {
	k.stakingKeeper = sk
	return k
}

// WithDistrKeeper wires real reward withdrawal from the pool's actual
// x/staking delegation. Requires WithBankKeeper and WithStakingKeeper too.
func (k Keeper) WithDistrKeeper(dk DistrKeeper) Keeper {
	k.distrKeeper = dk
	return k
}

// hasCustody reports whether this keeper was wired with real
// bank+staking+distribution custody. Every keeper constructed without
// WithBankKeeper/WithStakingKeeper/WithDistrKeeper (every existing unit
// test, and any future NewKeeper()-based caller) keeps the pre-custody,
// ledger-only behavior instead of nil-pointer-panicking -- intentionally, so
// this change doesn't force every test in this large existing suite to also
// wire a bank+staking+distribution double. The three are always wired
// together in production (app/keepers.go), so gating on all three keeps
// claimRewardCustody's distrKeeper access safe without a separate check.
func (k Keeper) hasCustody() bool {
	return k.bankKeeper != nil && k.stakingKeeper != nil && k.distrKeeper != nil
}

// parsePoolAccAddress converts one of this module's raw address strings
// (DelegatorShare.Delegator, NominatorPool.ValidatorTarget when used as an
// account) into a real sdk.AccAddress.
func parsePoolAccAddress(field, raw string) (sdk.AccAddress, error) {
	bz, err := addressing.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", field, err)
	}
	return sdk.AccAddress(bz), nil
}

// depositCustody collects a real deposit from the delegator into the pool's
// module account and delegates it to the pool's validator target, so
// TotalBondedStake is backed by an actual x/staking delegation instead of
// being a number with no real stake behind it. A no-op when the keeper
// wasn't wired with WithBankKeeper/WithStakingKeeper (see hasCustody), so
// existing ledger-only tests are unaffected. Assumes k.mu is already held by
// the caller.
func (k *Keeper) depositCustody(rawDelegator, rawValidatorTarget string, amountNaet uint64) error {
	if !k.hasCustody() {
		return nil
	}
	if strings.TrimSpace(rawValidatorTarget) == "" {
		return errors.New("nominator pool deposit requires a validator target to delegate to")
	}
	delegatorAddr, err := parsePoolAccAddress("nominator pool deposit delegator", rawDelegator)
	if err != nil {
		return err
	}
	validatorAddr, err := parsePoolAccAddress("nominator pool validator target", rawValidatorTarget)
	if err != nil {
		return err
	}
	valAddr := sdk.ValAddress(validatorAddr.Bytes())
	coins := sdk.NewCoins(sdk.NewInt64Coin(k.genesis.Params.BaseDenom, int64(amountNaet)))
	if err := k.bankKeeper.SendCoinsFromAccountToModule(k.runtimeCtx, delegatorAddr, types.ModuleName, coins); err != nil {
		return fmt.Errorf("collecting nominator pool deposit: %w", err)
	}
	validator, err := k.stakingKeeper.GetValidator(k.runtimeCtx, valAddr)
	if err != nil {
		return fmt.Errorf("nominator pool validator target lookup: %w", err)
	}
	if _, err := k.stakingKeeper.Delegate(k.runtimeCtx, PoolModuleAddress(), sdkmath.NewIntFromUint64(amountNaet), stakingtypes.Unbonded, validator, true); err != nil {
		return fmt.Errorf("delegating nominator pool deposit: %w", err)
	}
	return nil
}

// withdrawalCustody starts a real x/staking unbonding of amountNaet from the
// pool's delegation to rawValidatorTarget. x/staking's own EndBlocker
// completes the unbonding automatically after the real UnbondingTime elapses
// and credits the pool module account's spendable balance -- no separate
// pool-side completion call is needed for that half. The pool's own
// settlePendingWithdrawals EndBlocker (below) later pays the depositor once
// those tokens actually land back in the pool account. A no-op when custody
// isn't wired (see hasCustody), matching depositCustody. Assumes k.mu is
// already held by the caller.
//
// Returns the block height x/staking stamped on the unbonding entry it just
// created (SetUnbondingDelegationEntry uses sdkCtx.BlockHeight()), so the
// caller can record it on the withdrawal. Cohort settlement needs that real
// height to tell its own in-flight entries apart from later withdrawals' --
// see PendingWithdrawal.UnbondHeight for why the ledger's RequestHeight cannot
// be trusted for this. Returns 0 when custody isn't wired, which is also the
// only case where no entry was created.
func (k *Keeper) withdrawalCustody(rawValidatorTarget string, amountNaet uint64) (uint64, error) {
	if !k.hasCustody() {
		return 0, nil
	}
	if strings.TrimSpace(rawValidatorTarget) == "" {
		return 0, errors.New("nominator pool withdrawal requires a validator target to undelegate from")
	}
	validatorAddr, err := parsePoolAccAddress("nominator pool validator target", rawValidatorTarget)
	if err != nil {
		return 0, err
	}
	valAddr := sdk.ValAddress(validatorAddr.Bytes())
	poolAddr := PoolModuleAddress()
	shares, err := k.stakingKeeper.ValidateUnbondAmount(k.runtimeCtx, poolAddr, valAddr, sdkmath.NewIntFromUint64(amountNaet))
	if err != nil {
		return 0, fmt.Errorf("nominator pool withdrawal share conversion: %w", err)
	}
	if _, _, err := k.stakingKeeper.Undelegate(k.runtimeCtx, poolAddr, valAddr, shares); err != nil {
		return 0, fmt.Errorf("undelegating nominator pool withdrawal: %w", err)
	}
	unbondHeight := sdk.UnwrapSDKContext(k.runtimeCtx).BlockHeight()
	if unbondHeight < 0 {
		unbondHeight = 0
	}
	return uint64(unbondHeight), nil
}

func NewPersistentKeeper(storeService corestore.KVStoreService) Keeper {
	k := Keeper{genesis: DefaultGenesis(), storeService: storeService, mu: &sync.RWMutex{}}
	k.rebuildIndexes()
	return k
}

func DefaultGenesis() GenesisState {
	params := types.DefaultParams()
	return GenesisState{Version: prototype.CurrentGenesisVersion, Params: params, State: types.State{}.Normalize(params)}
}

func (gs GenesisState) Validate() error {
	if gs.Version != prototype.CurrentGenesisVersion {
		return errors.New("nominator pool unsupported genesis version")
	}
	return gs.State.Validate(gs.Params)
}

func (k *Keeper) InitGenesis(gs GenesisState) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := gs.Validate(); err != nil {
		return err
	}
	k.genesis = cloneGenesis(gs)
	k.rebuildIndexes()
	return nil
}

func (k *Keeper) InitGenesisState(ctx context.Context, gs GenesisState) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := gs.Validate(); err != nil {
		return err
	}
	k.genesis = cloneGenesis(gs)
	k.runtimeCtx = ctx
	k.rebuildIndexes()
	if k.storeService == nil {
		return nil
	}
	return k.writeReplacingState(ctx, k.genesis)
}

func (k Keeper) ExportGenesis() GenesisState {
	return cloneGenesis(k.genesis)
}

func (k Keeper) ExportGenesisState(ctx context.Context) (GenesisState, error) {
	if k.storeService == nil {
		return k.ExportGenesis(), nil
	}
	gs, err := k.readGenesisState(ctx)
	if err != nil {
		return GenesisState{}, err
	}
	if err := gs.Validate(); err != nil {
		return GenesisState{}, err
	}
	return cloneGenesis(gs), nil
}

// loadForBlock refreshes the in-memory genesis from the committed store using
// the live block context and points runtimeCtx at that same context. It MUST
// be called at the start of every consensus entry point (each Msg handler) so
// a restarted or state-synced node -- where InitChain/InitGenesis is not
// re-run -- operates on the same committed state as a continuously running
// node, and so writes persist through the current block rather than a stale
// InitChain-era context. Reading through the block context observes writes
// made earlier in the same block. Mirrors the fix applied to
// x/validator-election (SEC-HIGH). See security-audit/05-findings/
// FINDING-006-inmemory-genesis-not-rehydrated-consensus-halt.md.
// loadForBlock is called directly (never nested inside another locking
// keeper method) from msg_server.go at the top of every Msg handler, so it
// takes the write lock itself and holds it for its whole body.
func (k *Keeper) loadForBlock(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.runtimeCtx = ctx
	if k.storeService == nil {
		return nil
	}
	gs, err := k.readGenesisState(ctx)
	if err != nil {
		return err
	}
	if err := gs.Validate(); err != nil {
		return err
	}
	k.genesis = cloneGenesis(gs)
	// The committed store is now, by definition, what we just read, so this
	// is the baseline writeDiff must compare against. Re-establishing it here
	// on every entry point is what lets a mutation write only the records it
	// changed while staying deterministic across nodes -- see persistence.go.
	k.written = cloneGenesis(gs)
	k.rebuildIndexes()
	return nil
}

// saveGenesis is an internal helper invoked from many exported methods that
// already hold k.mu (write-locked) by the time they call it. It deliberately
// does NOT lock -- see the Keeper.mu doc comment.
func (k *Keeper) saveGenesis(next GenesisState) error {
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return err
	}
	k.genesis = cloneGenesis(next)
	k.rebuildIndexes()
	if k.storeService == nil || k.runtimeCtx == nil {
		return nil
	}
	return k.writeDiff(k.runtimeCtx, k.genesis)
}

func (k *Keeper) OperationCounters() OperationCounters {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.counters
}

func (k *Keeper) ResetOperationCounters() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.counters = OperationCounters{}
}

func (k *Keeper) UpdateParams(msg types.MsgUpdateParams) (types.Params, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.updateParamsLocked(msg)
}

// updateParamsLocked assumes k.mu is already held (write) by the caller. Do
// not call this from outside the package or without holding the lock.
func (k *Keeper) updateParamsLocked(msg types.MsgUpdateParams) (types.Params, error) {
	if msg.Height == 0 {
		return types.Params{}, errors.New("nominator pool params update height must be positive")
	}
	next := msg.Params
	if err := k.genesis.Params.ValidateParamsUpdate(msg.Authority, next); err != nil {
		return types.Params{}, err
	}
	next.Authority = k.genesis.Params.Authority
	updated := cloneGenesis(k.genesis)
	updated.Params = next
	if err := k.saveGenesis(updated); err != nil {
		return types.Params{}, err
	}
	return k.genesis.Params, nil
}

func (k *Keeper) UpdateStakingParams(msg types.MsgUpdateStakingParams) (types.Params, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.updateParamsLocked(types.MsgUpdateParams{
		Authority: msg.Authority,
		Params:    msg.Params,
		Height:    msg.Height,
	})
}

func (k *Keeper) RegisterValidator(msg types.MsgRegisterValidator) (types.ValidatorRegistrationReceipt, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := types.ValidateUserFacingAEAddress("validator registration signer", msg.SignerAddress); err != nil {
		return types.ValidatorRegistrationReceipt{}, err
	}
	if err := types.ValidateUserFacingAEAddress("validator registration validator", msg.ValidatorAddress); err != nil {
		return types.ValidatorRegistrationReceipt{}, err
	}
	if msg.Height == 0 {
		return types.ValidatorRegistrationReceipt{}, errors.New("validator registration height must be positive")
	}
	if err := k.ensureActiveWallet(msg.SignerAddress, "validator registration"); err != nil {
		return types.ValidatorRegistrationReceipt{}, err
	}
	if _, _, found := findValidator(k.genesis.State.Validators, msg.ValidatorAddress); found {
		return types.ValidatorRegistrationReceipt{}, errors.New("staking validator already registered")
	}
	mode := types.ValidatorFundingPoolBacked
	if msg.NominatorStake == 0 {
		mode = types.ValidatorFundingSolo
	}
	if err := k.genesis.Params.ValidateValidatorFunding(types.ValidatorFunding{Mode: mode, SelfStake: msg.SelfStake, NominatorStake: msg.NominatorStake}); err != nil {
		return types.ValidatorRegistrationReceipt{}, err
	}
	if err := k.genesis.Params.ValidateCommission(msg.CommissionBps, k.genesis.Params.DefaultValidatorCommissionBps, 0); err != nil {
		return types.ValidatorRegistrationReceipt{}, err
	}
	validator := types.Validator{
		Address:            msg.ValidatorAddress,
		SelfStake:          msg.SelfStake,
		NominatorStake:     msg.NominatorStake,
		Status:             types.StateValidatorStatusActive,
		PerformanceScore:   types.MaxBasisPoints,
		CommissionBps:      msg.CommissionBps,
		SlashingRiskBps:    0,
		AllocationLimitBps: k.genesis.Params.MaxPoolValidatorAllocationBps,
		UpdatedHeight:      msg.Height,
	}
	next := cloneGenesis(k.genesis)
	next.State.Validators = append(next.State.Validators, validator)
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return types.ValidatorRegistrationReceipt{}, err
	}
	if err := k.saveGenesis(next); err != nil {
		return types.ValidatorRegistrationReceipt{}, err
	}
	return types.ValidatorRegistrationReceipt{
		Validator:   msg.ValidatorAddress,
		Status:      validator.Status,
		SelfStake:   validator.SelfStake,
		PoolStake:   validator.NominatorStake,
		TouchedKeys: []string{string(types.ValidatorKey(msg.ValidatorAddress))},
	}, nil
}

func (k *Keeper) UpdateValidator(msg types.MsgUpdateValidator) (types.ValidatorRegistrationReceipt, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := types.ValidateUserFacingAEAddress("validator update signer", msg.SignerAddress); err != nil {
		return types.ValidatorRegistrationReceipt{}, err
	}
	if err := types.ValidateUserFacingAEAddress("validator update validator", msg.ValidatorAddress); err != nil {
		return types.ValidatorRegistrationReceipt{}, err
	}
	if msg.Height == 0 {
		return types.ValidatorRegistrationReceipt{}, errors.New("validator update height must be positive")
	}
	if msg.SignerAddress != msg.ValidatorAddress {
		return types.ValidatorRegistrationReceipt{}, errors.New("validator update signer must match validator address")
	}
	if err := k.ensureActiveWallet(msg.SignerAddress, "validator update"); err != nil {
		return types.ValidatorRegistrationReceipt{}, err
	}
	idx, validator, found := findValidator(k.genesis.State.Validators, msg.ValidatorAddress)
	if !found {
		return types.ValidatorRegistrationReceipt{}, errors.New("staking validator not found")
	}
	if msg.SelfStake > 0 {
		validator.SelfStake = msg.SelfStake
	}
	if msg.NominatorStake > 0 || validator.NominatorStake > 0 {
		validator.NominatorStake = msg.NominatorStake
	}
	if msg.PerformanceScore > 0 {
		validator.PerformanceScore = msg.PerformanceScore
	}
	if msg.CommissionBps > 0 {
		dailyChange := validator.CommissionBps - msg.CommissionBps
		if msg.CommissionBps > validator.CommissionBps {
			dailyChange = msg.CommissionBps - validator.CommissionBps
		}
		if err := k.genesis.Params.ValidateCommission(msg.CommissionBps, validator.CommissionBps, dailyChange); err != nil {
			return types.ValidatorRegistrationReceipt{}, err
		}
		validator.CommissionBps = msg.CommissionBps
	}
	validator.SlashingRiskBps = msg.SlashingRiskBps
	if msg.AllocationLimitBps > 0 {
		validator.AllocationLimitBps = msg.AllocationLimitBps
	}
	if msg.Status != "" {
		validator.Status = msg.Status
	}
	validator.UpdatedHeight = msg.Height
	next := cloneGenesis(k.genesis)
	next.State.Validators[idx] = validator
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return types.ValidatorRegistrationReceipt{}, err
	}
	if err := k.saveGenesis(next); err != nil {
		return types.ValidatorRegistrationReceipt{}, err
	}
	return types.ValidatorRegistrationReceipt{
		Validator:   validator.Address,
		Status:      validator.Status,
		SelfStake:   validator.SelfStake,
		PoolStake:   validator.NominatorStake,
		TouchedKeys: []string{string(types.ValidatorKey(validator.Address))},
	}, nil
}

// rebuildIndexes indexes k.genesis.State by pool ID and delegator address. It
// normalizes state first so the index positions it records always match the
// order cloneGenesis/saveGenesis produce -- callers that build indexes off
// not-yet-normalized state would otherwise compute delegatorIdx values that
// point at the wrong entry once a later cloneGenesis call re-sorts
// DelegatorShares (SortDelegators orders by the address STRING, which for
// bech32 raw addresses does not track insertion/numeric order the way the
// legacy zero-padded-hex format coincidentally did).
//
// Assumes k.mu is already held (write) by the caller; it never locks itself.
// This is the method whose unsynchronized k.indexes reassignment IS the F-14
// crash (concurrent map read/write) when called without the lock.
func (k *Keeper) rebuildIndexes() {
	k.genesis.State = k.genesis.State.Normalize(k.genesis.Params)
	k.indexes = make(map[string]poolIndexEntry, len(k.genesis.State.Pools))
	for poolIdx, pool := range k.genesis.State.Pools {
		entry := poolIndexEntry{
			index:     poolIdx,
			delegator: make(map[string]int, len(pool.DelegatorShares)),
		}
		for delegatorIdx, share := range pool.DelegatorShares {
			entry.delegator[share.Delegator] = delegatorIdx
		}
		k.indexes[pool.PoolID] = entry
	}
}

// ensureIndexes assumes k.mu is already held (write) by the caller. It can
// itself mutate k.indexes via rebuildIndexes, which is why every caller that
// can reach it -- directly or via lookupPool/lookupDelegator -- must hold the
// WRITE lock, never just RLock (see the Keeper.mu doc comment).
func (k *Keeper) ensureIndexes() {
	if k.indexes == nil || len(k.indexes) != len(k.genesis.State.Pools) {
		k.rebuildIndexes()
	}
}

// lookupPool assumes k.mu is already held (write) by the caller.
func (k *Keeper) lookupPool(poolID string) (int, types.NominatorPool, bool) {
	k.ensureIndexes()
	k.counters.PoolLookups++
	entry, found := k.indexes[poolID]
	if !found || entry.index < 0 || entry.index >= len(k.genesis.State.Pools) {
		return -1, types.NominatorPool{}, false
	}
	return entry.index, k.genesis.State.Pools[entry.index], true
}

// lookupDelegator assumes k.mu is already held (write) by the caller.
func (k *Keeper) lookupDelegator(poolID string, delegator string) (int, types.DelegatorShare, bool) {
	k.ensureIndexes()
	k.counters.DelegatorLookups++
	entry, found := k.indexes[poolID]
	if !found {
		return -1, types.DelegatorShare{}, false
	}
	pool := k.genesis.State.Pools[entry.index]
	delegatorIdx, found := entry.delegator[delegator]
	if !found || delegatorIdx < 0 || delegatorIdx >= len(pool.DelegatorShares) {
		return -1, types.DelegatorShare{}, false
	}
	return delegatorIdx, pool.DelegatorShares[delegatorIdx], true
}

func (k *Keeper) CreateNominatorPool(msg types.MsgCreateNominatorPool) (types.NominatorPool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.genesis.Params.Authorize(msg.Authority); err != nil {
		return types.NominatorPool{}, err
	}
	if msg.Height == 0 {
		return types.NominatorPool{}, errors.New("nominator pool creation height must be positive")
	}
	if types.IsJailedValidatorStatus(msg.ValidatorStatus) {
		return types.NominatorPool{}, errors.New("nominator pool cannot delegate to jailed validator")
	}
	if _, _, found := findPool(k.genesis.State.Pools, msg.PoolID); found {
		return types.NominatorPool{}, errors.New("nominator pool already exists")
	}
	pool := types.NominatorPool{
		PoolID:            msg.PoolID,
		PoolOperator:      msg.PoolOperator,
		ValidatorTarget:   msg.ValidatorTarget,
		PoolCommissionBps: msg.PoolCommissionBps,
		Status:            types.PoolStatusActive,
	}
	if err := pool.Validate(k.genesis.Params); err != nil {
		return types.NominatorPool{}, err
	}
	next := cloneGenesis(k.genesis)
	next.State.Pools = append(next.State.Pools, pool)
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return types.NominatorPool{}, err
	}
	if err := k.saveGenesis(next); err != nil {
		return types.NominatorPool{}, err
	}
	return pool, nil
}

func (k *Keeper) CreateOfficialLiquidStakingPool(msg types.MsgCreateOfficialLiquidStakingPool) (types.NominatorPool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.genesis.Params.Authorize(msg.Authority); err != nil {
		return types.NominatorPool{}, err
	}
	if msg.Height == 0 {
		return types.NominatorPool{}, errors.New("official liquid staking pool creation height must be positive")
	}
	if _, _, found := findPool(k.genesis.State.Pools, msg.PoolID); found {
		return types.NominatorPool{}, errors.New("official liquid staking pool already exists")
	}
	// An official pool must name the validator it delegates to. Without it the
	// pool can accept deposits it can never stake -- depositCustody rejects an
	// empty target -- so it would hand out shares backed by nothing.
	if strings.TrimSpace(msg.ValidatorTarget) == "" {
		return types.NominatorPool{}, errors.New("official liquid staking pool requires a validator target to delegate to")
	}
	pool := types.NominatorPool{
		PoolID:                msg.PoolID,
		ContractAddressUser:   msg.ContractAddressUser,
		ContractAddressRaw:    msg.ContractAddressRaw,
		OfficialLiquidStaking: true,
		PoolOperator:          msg.PoolOperator,
		PoolCommissionBps:     msg.PoolCommissionBps,
		ValidatorTarget:       msg.ValidatorTarget,
		Status:                types.PoolStatusActive,
	}
	if err := pool.Validate(k.genesis.Params); err != nil {
		return types.NominatorPool{}, err
	}
	next := cloneGenesis(k.genesis)
	next.State.Pools = append(next.State.Pools, pool)
	next.State.LiquidStakingPools = append(next.State.LiquidStakingPools, types.LiquidStakingPool{
		PoolID:                  msg.PoolID,
		ContractAddressUser:     msg.ContractAddressUser,
		ContractAddressRaw:      msg.ContractAddressRaw,
		ReceiptToken:            next.Params.PoolReceiptDenomOrCodeID,
		RentPayerPolicy:         types.RentPayerPolicyPoolReserve,
		Status:                  types.PoolStatusActive,
		LastStorageChargeHeight: msg.Height,
	})
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return types.NominatorPool{}, err
	}
	if err := k.saveGenesis(next); err != nil {
		return types.NominatorPool{}, err
	}
	return pool, nil
}

func (k *Keeper) DepositToPool(msg types.MsgDepositToPool) (types.DelegatorShare, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.genesis.Params.Authorize(msg.Authority); err != nil {
		return types.DelegatorShare{}, err
	}
	if msg.Amount == 0 || msg.Height == 0 {
		return types.DelegatorShare{}, errors.New("nominator pool deposit amount and height must be positive")
	}
	idx, pool, found := findPool(k.genesis.State.Pools, msg.PoolID)
	if !found {
		return types.DelegatorShare{}, errors.New("nominator pool not found")
	}
	if pool.Status != types.PoolStatusActive {
		return types.DelegatorShare{}, errors.New("nominator pool must be active for deposits")
	}
	shareAmount, err := types.SharesForDepositChecked(pool, msg.Amount)
	if err != nil {
		return types.DelegatorShare{}, err
	}
	// #2/SA2-N01: collect the real deposit and delegate it to the pool's
	// validator target BEFORE crediting any ledger share, mirroring
	// x/contracts' PayContractStorageDebt collect-before-mutate ordering.
	// All pure validation above already ran, so the only way this can fail
	// after real money moves is the final Validate()/persist below finding
	// something unexpected -- the same small residual risk every
	// collect-then-mutate handler in this codebase already accepts.
	if err := k.depositCustody(msg.Delegator, pool.ValidatorTarget, msg.Amount); err != nil {
		return types.DelegatorShare{}, err
	}
	delegatorIdx, delegator, found := findDelegator(pool.DelegatorShares, msg.Delegator)
	if found {
		accrued, err := types.AccruedReward(delegator, pool.RewardIndex)
		if err != nil {
			return types.DelegatorShare{}, err
		}
		delegator.PendingRewards = accrued
		newShares, err := types.CheckedAddUint64(delegator.Shares, shareAmount)
		if err != nil {
			return types.DelegatorShare{}, err
		}
		delegator.Shares = newShares
		delegator.RewardIndexCheckpoint = pool.RewardIndex
		delegator.SlashIndexCheckpoint = pool.SlashIndex
		pool.DelegatorShares[delegatorIdx] = delegator
	} else {
		delegator = types.DelegatorShare{
			Delegator:             msg.Delegator,
			Shares:                shareAmount,
			RewardIndexCheckpoint: pool.RewardIndex,
			SlashIndexCheckpoint:  pool.SlashIndex,
		}
		pool.DelegatorShares = append(pool.DelegatorShares, delegator)
	}
	newTotalShares, err := types.CheckedAddUint64(pool.TotalShares, shareAmount)
	if err != nil {
		return types.DelegatorShare{}, err
	}
	newTotalBondedStake, err := types.CheckedAddUint64(pool.TotalBondedStake, msg.Amount)
	if err != nil {
		return types.DelegatorShare{}, err
	}
	pool.TotalShares = newTotalShares
	pool.TotalBondedStake = newTotalBondedStake
	// Nothing is appended to pool.PendingDeposits -- see the note on
	// DepositToOfficialLiquidStaking's append site below.
	return k.savePool(idx, pool, delegator)
}

func (k *Keeper) DepositToOfficialLiquidStaking(msg types.MsgDepositToOfficialLiquidStaking) (types.DelegatorShare, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.depositToOfficialLiquidStakingLocked(msg)
}

// depositToOfficialLiquidStakingLocked assumes k.mu is already held (write)
// by the caller.
func (k *Keeper) depositToOfficialLiquidStakingLocked(msg types.MsgDepositToOfficialLiquidStaking) (types.DelegatorShare, error) {
	if err := types.ValidateOfficialLiquidStakingDeposit(msg, k.genesis.Params); err != nil {
		return types.DelegatorShare{}, err
	}
	rawUserAddress, err := types.RawAddressForUserAddress(msg.UserAddress)
	if err != nil {
		return types.DelegatorShare{}, err
	}
	idx, pool, found := findPool(k.genesis.State.Pools, msg.PoolID)
	if !found {
		return types.DelegatorShare{}, errors.New("official liquid staking pool not found")
	}
	if !pool.OfficialLiquidStaking {
		return types.DelegatorShare{}, errors.New("pool is not an official liquid staking pool")
	}
	if pool.Status != types.PoolStatusActive {
		return types.DelegatorShare{}, errors.New("official liquid staking pool must be active for deposits")
	}
	shareAmount, err := types.SharesForDepositChecked(pool, msg.Amount)
	if err != nil {
		return types.DelegatorShare{}, err
	}
	// Collect the real deposit and delegate it to the pool's validator target
	// before crediting any ledger share, exactly as DepositToPool does.
	//
	// This is the path an ordinary wallet actually uses: MsgDepositToStakingPool
	// lands here, and direct x/staking delegation is disabled for users
	// (app/stakingpolicy/msg_server.go), so this is the ONLY way user funds can
	// become real stake. It previously credited shares and TotalBondedStake
	// without moving a coin or delegating anything -- the custody wiring only
	// ever covered the authority-signed DepositToPool, so liquid staking looked
	// funded while no stake existed behind it.
	if err := k.depositCustody(rawUserAddress, pool.ValidatorTarget, msg.Amount); err != nil {
		return types.DelegatorShare{}, err
	}
	delegatorIdx, delegator, found := findDelegator(pool.DelegatorShares, rawUserAddress)
	if found {
		accrued, err := types.AccruedReward(delegator, pool.RewardIndex)
		if err != nil {
			return types.DelegatorShare{}, err
		}
		delegator.PendingRewards = accrued
		newShares, err := types.CheckedAddUint64(delegator.Shares, shareAmount)
		if err != nil {
			return types.DelegatorShare{}, err
		}
		delegator.Shares = newShares
		delegator.RewardIndexCheckpoint = pool.RewardIndex
		delegator.SlashIndexCheckpoint = pool.SlashIndex
		pool.DelegatorShares[delegatorIdx] = delegator
	} else {
		delegator = types.DelegatorShare{
			Delegator:             rawUserAddress,
			Shares:                shareAmount,
			RewardIndexCheckpoint: pool.RewardIndex,
			SlashIndexCheckpoint:  pool.SlashIndex,
		}
		pool.DelegatorShares = append(pool.DelegatorShares, delegator)
	}
	newTotalShares, err := types.CheckedAddUint64(pool.TotalShares, shareAmount)
	if err != nil {
		return types.DelegatorShare{}, err
	}
	newTotalBondedStake, err := types.CheckedAddUint64(pool.TotalBondedStake, msg.Amount)
	if err != nil {
		return types.DelegatorShare{}, err
	}
	pool.TotalShares = newTotalShares
	pool.TotalBondedStake = newTotalBondedStake
	// pool.PendingDeposits is deliberately NOT appended to.
	//
	// Nothing pends. By this line the deposit is already fully applied: the
	// coins have left the wallet, x/staking holds the delegation, and the
	// share is credited above. The field is a leftover from the pre-custody
	// design where a deposit really did queue, and it has no consumer left --
	// only Validate (a length cap) and Normalize (a sort) touch it, and its
	// generated protobuf type is imported by nothing.
	//
	// Appending here made it an unbounded, append-only log inside the pool
	// record -- the hottest value this module writes. Since gas is charged per
	// byte written, every historical entry was re-paid for on every future
	// deposit, so the cost grew with the number of DEPOSITS, not with the
	// number of depositors: measured, one single wallet depositing repeatedly
	// drove its own deposit from 231,521 gas to 560,473 by deposit #100 and
	// would have crossed MaxTxGas (1,000,000) around #250 -- at which point
	// NO further deposit or unbond fits in a block for ANY user of that pool
	// and every depositor's principal is trapped. One wallet paying ordinary
	// fees could do that to a pool deliberately.
	//
	// The field itself is left in place (so the record shape the off-chain
	// indexer reads is unchanged) but is now provably dead; removing it from
	// the Go struct, the .proto and the indexer is a separate coordinated
	// cleanup.
	return k.savePool(idx, pool, delegator)
}

func (k *Keeper) DepositToStakingPool(msg types.MsgDepositToStakingPool) (types.StakingPoolDepositReceipt, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := types.ValidateUserFacingAEAddress("staking pool depositor", msg.WalletAddress); err != nil {
		return types.StakingPoolDepositReceipt{}, err
	}
	if msg.ReservedRouting != "" {
		return types.StakingPoolDepositReceipt{}, errors.New("staking pool deposit must not include a routing field")
	}
	if err := k.ensureActiveWallet(msg.WalletAddress, "staking pool deposit"); err != nil {
		return types.StakingPoolDepositReceipt{}, err
	}

	poolID := msg.PoolID

	if poolID != "" {
		if _, err := addressing.Parse(poolID); err == nil {
			return types.StakingPoolDepositReceipt{}, errors.New("pool id must not be an address")
		} else {
		}
	}
	if msg.OfficialContract != "" {
		found := false
		resolvedID := ""

		for _, p := range k.genesis.State.Pools {
			if p.ContractAddressUser == msg.OfficialContract {
				resolvedID = p.PoolID
				found = true
				break
			}
		}

		if !found {
			for _, lp := range k.genesis.State.LiquidStakingPools {
				if lp.ContractAddressUser == msg.OfficialContract {
					resolvedID = lp.PoolID
					found = true
					break
				}
			}
		}
		if !found {
			return types.StakingPoolDepositReceipt{}, errors.New("official liquid staking pool not found")
		}

		if msg.PoolID != "" && msg.PoolID != resolvedID {
			return types.StakingPoolDepositReceipt{}, errors.New("pool id does not match official contract")
		}
		poolID = resolvedID
	}
	share, err := k.depositToOfficialLiquidStakingLocked(types.MsgDepositToOfficialLiquidStaking{
		Authority:   k.genesis.Params.Authority,
		PoolID:      poolID,
		UserAddress: msg.WalletAddress,
		Amount:      msg.Amount,
		Height:      msg.Height,
	})
	if err != nil {
		return types.StakingPoolDepositReceipt{}, err
	}
	rawUserAddress, err := types.RawAddressForUserAddress(msg.WalletAddress)
	if err != nil {
		return types.StakingPoolDepositReceipt{}, err
	}
	_, pool, found := findPool(k.genesis.State.Pools, poolID)
	if !found {
		return types.StakingPoolDepositReceipt{}, errors.New("official liquid staking pool not found")
	}
	if err := k.upsertLiquidPoolAfterPoolMutation(pool, msg.Height); err != nil {
		return types.StakingPoolDepositReceipt{}, err
	}
	if err := k.upsertPoolShare(poolID, msg.WalletAddress, share, msg.Amount, msg.Height); err != nil {
		return types.StakingPoolDepositReceipt{}, err
	}
	return types.StakingPoolDepositReceipt{
		PoolID:                  poolID,
		OwnerAddress:            msg.WalletAddress,
		PoolContractAddressUser: pool.ContractAddressUser,
		ReceiptToken:            k.genesis.Params.PoolReceiptDenomOrCodeID,
		Amount:                  msg.Amount,
		Shares:                  share.Shares,
		Height:                  msg.Height,
		InternalMetadata: types.PoolStateMetadata{
			OwnerRaw:               rawUserAddress,
			PoolContractAddressRaw: pool.ContractAddressRaw,
			TouchedKeys: []string{
				string(types.PoolKey(poolID)),
				string(types.PoolShareKey(poolID, msg.WalletAddress)),
			},
		},
	}, nil
}

func (k *Keeper) DelegateUserToValidator(msg types.MsgDelegateToValidator) error {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return types.ValidateDirectUserDelegation(msg, k.genesis.Params)
}

// ParamsAuthority returns the module's configured governance authority
// address. It exists so callers outside this file (msg_server.go's
// DelegateToValidator handler defaults an empty msg.Authority to it) never
// need to read k.genesis directly and unsynchronized.
func (k *Keeper) ParamsAuthority() string {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.genesis.Params.Authority
}

func (k *Keeper) InjectPooledStake(msg types.MsgInjectPooledStake) (types.NominatorPool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.injectPooledStakeLocked(msg)
}

// injectPooledStakeLocked assumes k.mu is already held (write) by the caller.
func (k *Keeper) injectPooledStakeLocked(msg types.MsgInjectPooledStake) (types.NominatorPool, error) {
	if err := types.ValidateUserFacingAEAddress("pooled stake caller contract", msg.CallerContractUser); err != nil {
		return types.NominatorPool{}, err
	}
	if err := types.ValidateUserFacingAEAddress("pooled stake validator address", msg.ValidatorAddress); err != nil {
		return types.NominatorPool{}, err
	}
	if msg.Amount == 0 || msg.Height == 0 {
		return types.NominatorPool{}, errors.New("pooled stake injection amount and height must be positive")
	}
	idx, pool, found := findPool(k.genesis.State.Pools, msg.PoolID)
	if !found {
		return types.NominatorPool{}, errors.New("official liquid staking pool not found")
	}
	if !pool.OfficialLiquidStaking || pool.ContractAddressUser != msg.CallerContractUser {
		return types.NominatorPool{}, errors.New("pooled stake injection requires official liquid staking contract")
	}
	if pool.Status != types.PoolStatusActive {
		return types.NominatorPool{}, errors.New("official liquid staking pool must be active for stake injection")
	}
	currentAllocated := totalAllocated(pool.Allocations)
	if msg.Amount > pool.TotalBondedStake-currentAllocated {
		return types.NominatorPool{}, errors.New("pooled stake injection exceeds unallocated pool stake")
	}
	allocationIdx, allocation, found := findAllocation(pool.Allocations, msg.ValidatorAddress)
	if found {
		allocation.Amount += msg.Amount
		allocation.Height = msg.Height
		pool.Allocations[allocationIdx] = allocation
	} else {
		pool.Allocations = append(pool.Allocations, types.PoolAllocation{
			ValidatorAddress: msg.ValidatorAddress,
			Amount:           msg.Amount,
			Height:           msg.Height,
		})
	}
	savedPool, err := k.savePoolOnly(idx, pool)
	if err != nil {
		return types.NominatorPool{}, err
	}
	if savedPool.OfficialLiquidStaking {
		if err := k.upsertLiquidPoolAfterPoolMutation(savedPool, msg.Height); err != nil {
			return types.NominatorPool{}, err
		}
	}
	return savedPool, nil
}

func (k *Keeper) InjectPoolStake(msg types.MsgInjectPoolStake) (types.PoolRebalanceReceipt, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(msg.Allocations) == 0 {
		return types.PoolRebalanceReceipt{}, errors.New("pool stake injection requires allocations")
	}
	var updated types.NominatorPool
	for _, allocation := range types.SortAllocations(msg.Allocations) {
		pool, err := k.injectPooledStakeLocked(types.MsgInjectPooledStake{
			CallerContractUser: msg.CallerContractUser,
			PoolID:             msg.PoolID,
			ValidatorAddress:   allocation.ValidatorAddress,
			Amount:             allocation.Amount,
			Height:             msg.Height,
		})
		if err != nil {
			return types.PoolRebalanceReceipt{}, err
		}
		updated = pool
		if err := k.upsertPoolValidatorAllocation(msg.PoolID, allocation.ValidatorAddress, allocation.Amount, msg.Height); err != nil {
			return types.PoolRebalanceReceipt{}, err
		}
	}
	return k.poolAllocationReceipt(updated, 0, msg.Height)
}

func (k *Keeper) RebalancePoolAllocations(msg types.MsgRebalancePoolAllocations) (types.PoolRebalanceReceipt, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := types.ValidateUserFacingAEAddress("pool rebalance caller contract", msg.CallerContractUser); err != nil {
		return types.PoolRebalanceReceipt{}, err
	}
	if msg.Epoch == 0 || msg.Height == 0 {
		return types.PoolRebalanceReceipt{}, errors.New("pool rebalance epoch and height must be positive")
	}
	idx, pool, found := findPool(k.genesis.State.Pools, msg.PoolID)
	if !found {
		return types.PoolRebalanceReceipt{}, errors.New("official liquid staking pool not found")
	}
	if !pool.OfficialLiquidStaking || pool.ContractAddressUser != msg.CallerContractUser {
		return types.PoolRebalanceReceipt{}, errors.New("pool rebalance requires official liquid staking contract")
	}
	if pool.Status != types.PoolStatusActive {
		return types.PoolRebalanceReceipt{}, errors.New("official liquid staking pool must be active for rebalance")
	}
	weights, err := k.genesis.Params.AllocationWeights(msg.Candidates)
	if err != nil {
		return types.PoolRebalanceReceipt{}, err
	}
	nextAllocations := make([]types.PoolAllocation, 0, len(weights))
	allocated := uint64(0)
	lastPositive := -1
	for idx := range weights {
		if weights[idx].WeightBps > 0 {
			lastPositive = idx
		}
	}
	for idx, weight := range weights {
		if weight.WeightBps == 0 {
			continue
		}
		amount, err := types.MulDivUint64(pool.TotalBondedStake, uint64(weight.WeightBps), uint64(types.MaxBasisPoints))
		if err != nil {
			return types.PoolRebalanceReceipt{}, err
		}
		if idx == lastPositive {
			amount = pool.TotalBondedStake - allocated
		}
		allocated += amount
		nextAllocations = append(nextAllocations, types.PoolAllocation{
			ValidatorAddress: weight.ValidatorAddress,
			Amount:           amount,
			Height:           msg.Height,
		})
		if err := k.upsertPoolValidatorAllocation(msg.PoolID, weight.ValidatorAddress, amount, msg.Height); err != nil {
			return types.PoolRebalanceReceipt{}, err
		}
	}
	pool.Allocations = types.SortAllocations(nextAllocations)
	next := cloneGenesis(k.genesis)
	next.State.Pools[idx] = pool
	for allocationIdx := range next.State.PoolValidatorAllocations {
		if next.State.PoolValidatorAllocations[allocationIdx].PoolID == msg.PoolID {
			next.State.PoolValidatorAllocations[allocationIdx].UpdatedHeight = msg.Height
		}
	}
	if liquidIdx, liquid, found := findLiquidPool(next.State.LiquidStakingPools, msg.PoolID); found {
		if err := k.accrueOfficialPoolRent(&liquid, pool, msg.Height); err != nil {
			return types.PoolRebalanceReceipt{}, err
		}
		liquid.TotalActiveStake = allocated
		liquid.AllocationEpoch = msg.Epoch
		next.State.LiquidStakingPools[liquidIdx] = liquid
	}
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return types.PoolRebalanceReceipt{}, err
	}
	if err := k.saveGenesis(next); err != nil {
		return types.PoolRebalanceReceipt{}, err
	}
	return k.poolAllocationReceipt(pool, msg.Epoch, msg.Height)
}

func (k *Keeper) SetOfficialLiquidStakingContract(msg types.MsgSetOfficialLiquidStakingContract) (types.NominatorPool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.genesis.Params.Authorize(msg.Authority); err != nil {
		return types.NominatorPool{}, err
	}
	if msg.Height == 0 {
		return types.NominatorPool{}, errors.New("official liquid staking contract update height must be positive")
	}
	if err := types.ValidateAddressPair("official liquid staking contract", msg.ContractAddressUser, msg.ContractAddressRaw); err != nil {
		return types.NominatorPool{}, err
	}
	idx, pool, found := findPool(k.genesis.State.Pools, msg.PoolID)
	if !found {
		return types.NominatorPool{}, errors.New("official liquid staking pool not found")
	}
	pool.ContractAddressUser = msg.ContractAddressUser
	pool.ContractAddressRaw = msg.ContractAddressRaw
	pool.OfficialLiquidStaking = true
	next := cloneGenesis(k.genesis)
	next.State.Pools[idx] = pool
	if liquidIdx, liquid, found := findLiquidPool(next.State.LiquidStakingPools, msg.PoolID); found {
		liquid.ContractAddressUser = msg.ContractAddressUser
		liquid.ContractAddressRaw = msg.ContractAddressRaw
		liquid.LastStorageChargeHeight = msg.Height
		next.State.LiquidStakingPools[liquidIdx] = liquid
	} else {
		next.State.LiquidStakingPools = append(next.State.LiquidStakingPools, types.LiquidStakingPool{
			PoolID:                  msg.PoolID,
			ContractAddressUser:     msg.ContractAddressUser,
			ContractAddressRaw:      msg.ContractAddressRaw,
			ReceiptToken:            next.Params.PoolReceiptDenomOrCodeID,
			RentPayerPolicy:         types.RentPayerPolicyPoolReserve,
			Status:                  pool.Status,
			LastStorageChargeHeight: msg.Height,
		})
	}
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return types.NominatorPool{}, err
	}
	if err := k.saveGenesis(next); err != nil {
		return types.NominatorPool{}, err
	}
	return pool, nil
}

func (k *Keeper) RequestPoolWithdrawal(msg types.MsgRequestPoolWithdrawal) (types.PendingWithdrawal, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.requestPoolWithdrawalLocked(msg)
}

// requestPoolWithdrawalLocked assumes k.mu is already held (write) by the
// caller.
func (k *Keeper) requestPoolWithdrawalLocked(msg types.MsgRequestPoolWithdrawal) (types.PendingWithdrawal, error) {
	if err := k.genesis.Params.Authorize(msg.Authority); err != nil {
		return types.PendingWithdrawal{}, err
	}
	if msg.Shares == 0 || msg.Height == 0 {
		return types.PendingWithdrawal{}, errors.New("nominator pool withdrawal shares and height must be positive")
	}
	idx, pool, found := findPool(k.genesis.State.Pools, msg.PoolID)
	if !found {
		return types.PendingWithdrawal{}, errors.New("nominator pool not found")
	}
	if _, _, found := findWithdrawal(pool.PendingWithdrawals, msg.WithdrawalID); found {
		return types.PendingWithdrawal{}, errors.New("nominator pool withdrawal already exists")
	}
	delegatorIdx, delegator, found := findDelegator(pool.DelegatorShares, msg.Delegator)
	if !found {
		return types.PendingWithdrawal{}, errors.New("nominator pool delegator not found")
	}
	if msg.Shares > delegator.Shares || msg.Shares > pool.TotalShares {
		return types.PendingWithdrawal{}, errors.New("nominator pool cannot withdraw more than total stake")
	}
	reward, err := types.AccruedReward(delegator, pool.RewardIndex)
	if err != nil {
		return types.PendingWithdrawal{}, err
	}
	amount, err := types.ShareValue(pool, msg.Shares)
	if err != nil {
		return types.PendingWithdrawal{}, err
	}
	if amount == 0 || amount > pool.TotalBondedStake {
		return types.PendingWithdrawal{}, errors.New("nominator pool withdrawal amount exceeds bonded stake")
	}
	// #2/SA2-N01: start the real unbonding before touching the ledger, same
	// collect/undelegate-before-mutate ordering as depositCustody.
	//
	// This must stay symmetric with depositCustody for EVERY pool kind. It
	// used to be skipped for an OfficialLiquidStaking pool on the grounds that
	// only DepositToPool delegated real funds, so there was "nothing real to
	// undelegate". Official deposits now delegate for real, so skipping here
	// would burn the depositor: the ledger drops their shares, no undelegation
	// starts, settleWithdrawal finds no spendable coins and EndBlocker leaves
	// the withdrawal Pending forever -- money in, nothing out.
	unbondHeight, err := k.withdrawalCustody(pool.ValidatorTarget, amount)
	if err != nil {
		return types.PendingWithdrawal{}, err
	}
	delegator.Shares -= msg.Shares
	delegator.PendingRewards = reward
	delegator.RewardIndexCheckpoint = pool.RewardIndex
	delegator.SlashIndexCheckpoint = pool.SlashIndex
	pool.TotalShares -= msg.Shares
	pool.TotalBondedStake -= amount
	if delegator.Shares == 0 && delegator.PendingRewards == 0 {
		pool.DelegatorShares = append(pool.DelegatorShares[:delegatorIdx], pool.DelegatorShares[delegatorIdx+1:]...)
	} else {
		pool.DelegatorShares[delegatorIdx] = delegator
	}
	withdrawal := types.PendingWithdrawal{
		WithdrawalID:    msg.WithdrawalID,
		Delegator:       msg.Delegator,
		Shares:          msg.Shares,
		Amount:          amount,
		RequestHeight:   msg.Height,
		CompleteHeight:  msg.Height + k.genesis.Params.UnbondingBlocks,
		UnbondHeight:    unbondHeight,
		UnbondValidator: pool.ValidatorTarget,
		Status:          types.WithdrawalStatusPending,
	}
	pool.PendingWithdrawals = append(pool.PendingWithdrawals, withdrawal)
	pool.UnbondingQueue = append(pool.UnbondingQueue, types.UnbondingEntry{
		WithdrawalID:   withdrawal.WithdrawalID,
		Delegator:      withdrawal.Delegator,
		Amount:         withdrawal.Amount,
		CompleteHeight: withdrawal.CompleteHeight,
		Status:         withdrawal.Status,
	})
	next := cloneGenesis(k.genesis)
	next.State.Pools[idx] = pool
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return types.PendingWithdrawal{}, err
	}
	if err := k.saveGenesis(next); err != nil {
		return types.PendingWithdrawal{}, err
	}
	return withdrawal, nil
}

func (k *Keeper) RequestPoolUnbond(msg types.MsgRequestPoolUnbond) (types.PoolUnbondReceipt, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := types.ValidateUserFacingAEAddress("pool unbond owner", msg.OwnerAddress); err != nil {
		return types.PoolUnbondReceipt{}, err
	}
	if err := k.ensureActiveWallet(msg.OwnerAddress, "pool unbond request"); err != nil {
		return types.PoolUnbondReceipt{}, err
	}
	rawOwner, err := types.RawAddressForUserAddress(msg.OwnerAddress)
	if err != nil {
		return types.PoolUnbondReceipt{}, err
	}
	withdrawal, err := k.requestPoolWithdrawalLocked(types.MsgRequestPoolWithdrawal{
		Authority:    k.genesis.Params.Authority,
		PoolID:       msg.PoolID,
		WithdrawalID: msg.RequestID,
		Delegator:    rawOwner,
		Shares:       msg.Shares,
		Height:       msg.Height,
	})
	if err != nil {
		return types.PoolUnbondReceipt{}, err
	}
	_, pool, found := findPool(k.genesis.State.Pools, msg.PoolID)
	if !found {
		return types.PoolUnbondReceipt{}, errors.New("nominator pool not found")
	}
	if err := k.upsertLiquidPoolAfterPoolMutation(pool, msg.Height); err != nil {
		return types.PoolUnbondReceipt{}, err
	}
	if err := k.upsertPoolUnbonding(msg.PoolID, msg.OwnerAddress, withdrawal); err != nil {
		return types.PoolUnbondReceipt{}, err
	}
	if err := k.updatePoolShareAfterUnbond(msg.PoolID, msg.OwnerAddress, withdrawal, msg.Height); err != nil {
		return types.PoolUnbondReceipt{}, err
	}
	return types.PoolUnbondReceipt{
		PoolID:         msg.PoolID,
		OwnerAddress:   msg.OwnerAddress,
		RequestID:      msg.RequestID,
		Shares:         withdrawal.Shares,
		Amount:         withdrawal.Amount,
		RequestHeight:  withdrawal.RequestHeight,
		CompleteHeight: withdrawal.CompleteHeight,
		InternalMetadata: types.PoolStateMetadata{
			OwnerRaw:               rawOwner,
			PoolContractAddressRaw: pool.ContractAddressRaw,
			TouchedKeys: []string{
				string(types.PoolKey(msg.PoolID)),
				string(types.PoolShareKey(msg.PoolID, msg.OwnerAddress)),
				string(types.PoolUnbondingKey(msg.PoolID, msg.OwnerAddress, msg.RequestID)),
			},
		},
	}, nil
}

func (k *Keeper) WithdrawPoolStake(msg types.MsgWithdrawPoolStake) (types.PoolWithdrawalReceipt, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := types.ValidateUserFacingAEAddress("pool withdrawal caller contract", msg.CallerContractUser); err != nil {
		return types.PoolWithdrawalReceipt{}, err
	}
	if err := types.ValidateUserFacingAEAddress("pool withdrawal owner", msg.OwnerAddress); err != nil {
		return types.PoolWithdrawalReceipt{}, err
	}
	rawOwner, err := types.RawAddressForUserAddress(msg.OwnerAddress)
	if err != nil {
		return types.PoolWithdrawalReceipt{}, err
	}
	idx, pool, found := findPool(k.genesis.State.Pools, msg.PoolID)
	if !found {
		return types.PoolWithdrawalReceipt{}, errors.New("official liquid staking pool not found")
	}
	if !pool.OfficialLiquidStaking || pool.ContractAddressUser != msg.CallerContractUser {
		return types.PoolWithdrawalReceipt{}, errors.New("pool withdrawal requires official liquid staking contract")
	}
	withdrawalIdx, withdrawal, found := findWithdrawal(pool.PendingWithdrawals, msg.RequestID)
	if !found {
		return types.PoolWithdrawalReceipt{}, errors.New("pool withdrawal request not found")
	}
	if withdrawal.Delegator != rawOwner {
		return types.PoolWithdrawalReceipt{}, errors.New("pool withdrawal owner mismatch")
	}
	if withdrawal.Status != types.WithdrawalStatusPending {
		return types.PoolWithdrawalReceipt{}, errors.New("pool withdrawal is not pending")
	}
	if msg.Height < withdrawal.CompleteHeight {
		return types.PoolWithdrawalReceipt{}, errors.New("pool withdrawal cannot release before unbonding period")
	}
	// Pay the withdrawal for real before marking it completed. This used to just
	// flip Status to Completed and save, moving no coins at all -- and
	// settlePendingWithdrawals (the EndBlocker) only ever settles withdrawals
	// that are still Pending, so a withdrawal "completed" here was paid by
	// nobody, ever: the owner's shares were already burned at RequestPoolUnbond
	// and their principal sat in the pool module account permanently.
	//
	// That was unreachable only by accident: msg_server rewrote unbond's
	// owner_address to the account's v2 identity while this path's owner stayed
	// plain, so withdrawal.Delegator != rawOwner above always tripped first. Now
	// that both paths carry the plain address that check passes, which makes
	// this reachable -- so it has to actually settle.
	//
	// Failing when the pool cannot cover it yet is the safe half of the trade:
	// the tx errors, nothing mutates, the withdrawal stays Pending, and either
	// the EndBlocker or a later retry settles it once the real x/staking
	// unbonding matures. Marking it Completed unpaid is the unsafe half -- it
	// permanently strands the principal by making the EndBlocker skip it.
	// A no-op when custody isn't wired (see hasCustody), matching the EndBlocker.
	//
	// This goes through the SAME cohort settlement the EndBlocker uses rather
	// than paying this one withdrawal its full Amount on demand. A per-caller
	// full-Amount payout here would be exactly the drain the cohort rule exists
	// to prevent: whoever calls first would take their whole claim out of a
	// balance that, after an in-flight slash, is short for everybody -- leaving
	// the shortfall to whoever exits last instead of sharing it pro-rata. So
	// this settles the whole matured cohort and then reports what THIS
	// withdrawal actually got.
	if k.hasCustody() {
		settled, _, err := k.settlePoolWithdrawals(k.genesis.Params.BaseDenom, pool, msg.Height)
		if err != nil {
			return types.PoolWithdrawalReceipt{}, err
		}
		pool = settled
		if _, current, ok := findWithdrawal(pool.PendingWithdrawals, msg.RequestID); ok {
			withdrawal = current
		}
		if withdrawal.Status != types.WithdrawalStatusCompleted {
			return types.PoolWithdrawalReceipt{}, errors.New("nominator pool withdrawal is not funded yet: the real x/staking unbonding has not completed, so the pool cannot pay it -- retry once it matures, or let the pool's EndBlocker settle it automatically")
		}
	} else {
		// Ledger-only pool: no coins exist to move, so "settled" is the claim
		// by definition. Keeps the receipt below uniform across both modes.
		withdrawal.SettledAmount = withdrawal.Amount
		withdrawal.Status = types.WithdrawalStatusCompleted
		pool.PendingWithdrawals[withdrawalIdx] = withdrawal
		for entryIdx, entry := range pool.UnbondingQueue {
			if entry.WithdrawalID == msg.RequestID {
				entry.Status = types.WithdrawalStatusCompleted
				pool.UnbondingQueue[entryIdx] = entry
			}
		}
	}
	next := cloneGenesis(k.genesis)
	next.State.Pools[idx] = pool
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return types.PoolWithdrawalReceipt{}, err
	}
	if err := k.saveGenesis(next); err != nil {
		return types.PoolWithdrawalReceipt{}, err
	}
	if err := k.upsertLiquidPoolAfterPoolMutation(pool, msg.Height); err != nil {
		return types.PoolWithdrawalReceipt{}, err
	}
	if err := k.upsertPoolUnbonding(msg.PoolID, msg.OwnerAddress, withdrawal); err != nil {
		return types.PoolWithdrawalReceipt{}, err
	}
	return types.PoolWithdrawalReceipt{
		PoolID:       msg.PoolID,
		OwnerAddress: msg.OwnerAddress,
		RequestID:    msg.RequestID,
		// What the caller actually received, not what they claimed -- an
		// in-flight slash really can make these differ, and a receipt that
		// reports the claim would be reporting a payout that never happened.
		Amount:       withdrawal.SettledAmount,
		Height:       msg.Height,
		InternalMetadata: types.PoolStateMetadata{
			OwnerRaw:               rawOwner,
			PoolContractAddressRaw: pool.ContractAddressRaw,
			TouchedKeys: []string{
				string(types.PoolKey(msg.PoolID)),
				string(types.PoolUnbondingKey(msg.PoolID, msg.OwnerAddress, msg.RequestID)),
			},
		},
	}, nil
}

func (k *Keeper) TopUpPoolReserve(msg types.MsgTopUpPoolReserve) (types.PoolTopUpReceipt, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := types.ValidateUserFacingAEAddress("pool top-up payer", msg.PayerAddress); err != nil {
		return types.PoolTopUpReceipt{}, err
	}
	if msg.Amount == 0 || msg.Height == 0 {
		return types.PoolTopUpReceipt{}, errors.New("pool top-up amount and height must be positive")
	}
	if err := k.ensureActiveWallet(msg.PayerAddress, "pool top-up"); err != nil {
		return types.PoolTopUpReceipt{}, err
	}
	rawPayer, err := types.RawAddressForUserAddress(msg.PayerAddress)
	if err != nil {
		return types.PoolTopUpReceipt{}, err
	}
	next := cloneGenesis(k.genesis)
	_, pool, found := findPool(next.State.Pools, msg.PoolID)
	if !found {
		return types.PoolTopUpReceipt{}, errors.New("official liquid staking pool not found")
	}
	if !pool.OfficialLiquidStaking {
		return types.PoolTopUpReceipt{}, errors.New("pool top-up requires official liquid staking pool")
	}
	if pool.Status == types.PoolStatusClosed {
		return types.PoolTopUpReceipt{}, errors.New("closed pool reserve cannot be topped up")
	}
	liquidIdx, liquid, found := findLiquidPool(next.State.LiquidStakingPools, msg.PoolID)
	if !found {
		return types.PoolTopUpReceipt{}, errors.New("liquid staking pool state not found")
	}
	if err := k.accrueOfficialPoolRent(&liquid, pool, msg.Height); err != nil {
		return types.PoolTopUpReceipt{}, err
	}
	debtPaid := msg.Amount
	if debtPaid > liquid.StorageRentDebt {
		debtPaid = liquid.StorageRentDebt
	}
	liquid.StorageRentDebt -= debtPaid
	liquid.ContractAddressUser = pool.ContractAddressUser
	liquid.ContractAddressRaw = pool.ContractAddressRaw
	liquid.Status = pool.Status
	if liquid.StorageRentDebt > 0 && pool.Status == types.PoolStatusActive {
		liquid.Status = types.PoolStatusFrozenLimited
	}
	next.State.LiquidStakingPools[liquidIdx] = liquid
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return types.PoolTopUpReceipt{}, err
	}
	if err := k.saveGenesis(next); err != nil {
		return types.PoolTopUpReceipt{}, err
	}
	return types.PoolTopUpReceipt{
		PoolID:          msg.PoolID,
		PayerAddress:    msg.PayerAddress,
		Amount:          msg.Amount,
		StorageDebtPaid: debtPaid,
		Height:          msg.Height,
		InternalMetadata: types.PoolStateMetadata{
			OwnerRaw:               rawPayer,
			PoolContractAddressRaw: pool.ContractAddressRaw,
			TouchedKeys: []string{
				string(types.PoolKey(msg.PoolID)),
				string(types.PoolStorageDebtKey(msg.PoolID)),
			},
		},
	}, nil
}

func (k *Keeper) CancelPoolWithdrawal(msg types.MsgCancelPoolWithdrawal) (types.PendingWithdrawal, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.genesis.Params.Authorize(msg.Authority); err != nil {
		return types.PendingWithdrawal{}, err
	}
	idx, pool, found := findPool(k.genesis.State.Pools, msg.PoolID)
	if !found {
		return types.PendingWithdrawal{}, errors.New("nominator pool not found")
	}
	withdrawalIdx, withdrawal, found := findWithdrawal(pool.PendingWithdrawals, msg.WithdrawalID)
	if !found {
		return types.PendingWithdrawal{}, errors.New("nominator pool withdrawal not found")
	}
	if withdrawal.Delegator != msg.Delegator {
		return types.PendingWithdrawal{}, errors.New("nominator pool withdrawal delegator mismatch")
	}
	if withdrawal.Status != types.WithdrawalStatusPending {
		return types.PendingWithdrawal{}, errors.New("nominator pool withdrawal is not pending")
	}
	withdrawal.Status = types.WithdrawalStatusCancelled
	pool.PendingWithdrawals[withdrawalIdx] = withdrawal
	for entryIdx, entry := range pool.UnbondingQueue {
		if entry.WithdrawalID == msg.WithdrawalID {
			entry.Status = types.WithdrawalStatusCancelled
			pool.UnbondingQueue[entryIdx] = entry
		}
	}
	shares, err := types.SharesForDepositChecked(pool, withdrawal.Amount)
	if err != nil {
		return types.PendingWithdrawal{}, err
	}
	if shares < withdrawal.Shares {
		shares = withdrawal.Shares
	}
	delegatorIdx, delegator, found := findDelegator(pool.DelegatorShares, msg.Delegator)
	if found {
		accrued, err := types.AccruedReward(delegator, pool.RewardIndex)
		if err != nil {
			return types.PendingWithdrawal{}, err
		}
		delegator.PendingRewards = accrued
		newShares, err := types.CheckedAddUint64(delegator.Shares, shares)
		if err != nil {
			return types.PendingWithdrawal{}, err
		}
		delegator.Shares = newShares
		delegator.RewardIndexCheckpoint = pool.RewardIndex
		pool.DelegatorShares[delegatorIdx] = delegator
	} else {
		pool.DelegatorShares = append(pool.DelegatorShares, types.DelegatorShare{
			Delegator:             msg.Delegator,
			Shares:                shares,
			RewardIndexCheckpoint: pool.RewardIndex,
			SlashIndexCheckpoint:  pool.SlashIndex,
		})
	}
	newTotalShares, err := types.CheckedAddUint64(pool.TotalShares, shares)
	if err != nil {
		return types.PendingWithdrawal{}, err
	}
	newTotalBondedStake, err := types.CheckedAddUint64(pool.TotalBondedStake, withdrawal.Amount)
	if err != nil {
		return types.PendingWithdrawal{}, err
	}
	pool.TotalShares = newTotalShares
	pool.TotalBondedStake = newTotalBondedStake
	next := cloneGenesis(k.genesis)
	next.State.Pools[idx] = pool
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return types.PendingWithdrawal{}, err
	}
	if err := k.saveGenesis(next); err != nil {
		return types.PendingWithdrawal{}, err
	}
	return withdrawal, nil
}

func (k *Keeper) ClaimPoolRewards(msg types.MsgClaimPoolRewards) (uint64, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.claimPoolRewardsLocked(msg)
}

// claimPoolRewardsLocked assumes k.mu is already held (write) by the caller.
func (k *Keeper) claimPoolRewardsLocked(msg types.MsgClaimPoolRewards) (uint64, error) {
	ownerAddress := msg.OwnerAddress
	delegator := msg.Delegator
	if ownerAddress != "" {
		if msg.Height == 0 {
			return 0, errors.New("pool reward claim height must be positive")
		}
		if err := types.ValidateUserFacingAEAddress("pool reward claim owner", ownerAddress); err != nil {
			return 0, err
		}
		if err := k.ensureActiveWallet(ownerAddress, "pool reward claim"); err != nil {
			return 0, err
		}
		if msg.Authority == "" {
			msg.Authority = ownerAddress
		}
		if msg.Authority != ownerAddress {
			return 0, errors.New("pool reward claim signer must match owner address")
		}
		rawOwner, err := types.RawAddressForUserAddress(ownerAddress)
		if err != nil {
			return 0, err
		}
		delegator = rawOwner
	} else if err := k.genesis.Params.Authorize(msg.Authority); err != nil {
		return 0, err
	}
	idx, pool, found := k.lookupPool(msg.PoolID)
	if !found {
		return 0, errors.New("nominator pool not found")
	}
	delegatorIdx, share, found := k.lookupDelegator(msg.PoolID, delegator)
	if !found {
		return 0, errors.New("nominator pool delegator not found")
	}
	reward, err := types.AccruedReward(share, pool.RewardIndex)
	if err != nil {
		return 0, err
	}
	// #2/SA2-N01: pay the claimed reward for real before touching the
	// ledger. claimRewardCustody opportunistically pulls the pool's actual
	// accrued x/staking distribution rewards into the pool account first
	// (best-effort -- there may be nothing to pull if SyncPoolRewards' own
	// externally-injected reward numbers are running ahead of real
	// distribution income), then pays out only if the pool account actually
	// has the funds. A no-op when custody isn't wired (see hasCustody).
	//
	// This used to be skipped for an OfficialLiquidStaking pool because
	// DepositToStakingPool was ledger-only, so there was no real income to pay
	// a claim from. Official deposits now delegate for real and earn real
	// distribution rewards, so skipping here would zero the depositor's
	// PendingRewards without ever paying them.
	//
	// The pool's ledger reward and its real income are still two different
	// numbers: SyncPoolRewards injects the ledger number externally rather than
	// deriving it from x/distribution accrual. Where they disagree this
	// deliberately FAILS the claim rather than reconciling it. Failing is the
	// safe half of the trade -- it errors before PendingRewards is zeroed, so
	// the claim stays on the ledger and succeeds later once real income catches
	// up, costing liveness and nothing else. Paying regardless is the unsafe
	// half: the pool's spendable balance is other people's un-delegated money,
	// so an over-stated reward would be paid out of the next depositor's
	// principal. Deriving the ledger number from real accrual is the actual fix
	// and a separate change.
	if reward > 0 {
		if err := k.claimRewardCustody(delegator, pool.ValidatorTarget, reward); err != nil {
			return 0, err
		}
	}
	share.PendingRewards = 0
	share.RewardIndexCheckpoint = pool.RewardIndex
	if err := share.Validate(); err != nil {
		return 0, err
	}
	next := cloneGenesis(k.genesis)
	next.State.Pools[idx].DelegatorShares[delegatorIdx] = share
	if err := k.saveGenesis(next); err != nil {
		return 0, err
	}
	if pool.OfficialLiquidStaking && msg.Height > 0 {
		if err := k.upsertLiquidPoolAfterPoolMutation(next.State.Pools[idx], msg.Height); err != nil {
			return 0, err
		}
	}
	k.counters.DelegatorRewardUpdates++
	if ownerAddress != "" {
		if err := k.upsertRewardClaim(msg.PoolID, ownerAddress, pool.RewardEpoch, reward); err != nil {
			return 0, err
		}
		if err := k.upsertPoolShare(msg.PoolID, ownerAddress, share, 0, msg.Height); err != nil {
			return 0, err
		}
	}
	return reward, nil
}

func (k *Keeper) ClaimPoolRewardsWithReceipt(msg types.MsgClaimPoolRewards) (types.PoolRewardClaimReceipt, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if msg.OwnerAddress == "" {
		return types.PoolRewardClaimReceipt{}, errors.New("pool reward claim requires AE owner address")
	}
	amount, err := k.claimPoolRewardsLocked(msg)
	if err != nil {
		return types.PoolRewardClaimReceipt{}, err
	}
	rawOwner, err := types.RawAddressForUserAddress(msg.OwnerAddress)
	if err != nil {
		return types.PoolRewardClaimReceipt{}, err
	}
	_, pool, found := findPool(k.genesis.State.Pools, msg.PoolID)
	if !found {
		return types.PoolRewardClaimReceipt{}, errors.New("nominator pool not found")
	}
	return types.PoolRewardClaimReceipt{
		PoolID:       msg.PoolID,
		OwnerAddress: msg.OwnerAddress,
		Amount:       amount,
		Epoch:        pool.RewardEpoch,
		Height:       msg.Height,
		InternalMetadata: types.PoolStateMetadata{
			OwnerRaw:               rawOwner,
			PoolContractAddressRaw: pool.ContractAddressRaw,
			TouchedKeys: []string{
				string(types.PoolShareKey(msg.PoolID, msg.OwnerAddress)),
				string(types.RewardClaimKey(msg.PoolID, msg.OwnerAddress, pool.RewardEpoch)),
			},
		},
	}, nil
}

func (k *Keeper) ClaimStakeReputation(msg types.MsgClaimStakeReputation) (types.StakeReputationClaimReceipt, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := types.ValidateUserFacingAEAddress("stake reputation claim owner", msg.OwnerAddress); err != nil {
		return types.StakeReputationClaimReceipt{}, err
	}
	if msg.Height == 0 {
		return types.StakeReputationClaimReceipt{}, errors.New("stake reputation claim height must be positive")
	}
	if err := k.ensureActiveWallet(msg.OwnerAddress, "stake reputation claim"); err != nil {
		return types.StakeReputationClaimReceipt{}, err
	}
	next := cloneGenesis(k.genesis)
	_, pool, found := findPool(next.State.Pools, msg.PoolID)
	if !found {
		return types.StakeReputationClaimReceipt{}, errors.New("nominator pool not found")
	}
	shareIdx, share, found := findPoolShare(next.State.PoolShares, msg.PoolID, msg.OwnerAddress)
	if !found {
		return types.StakeReputationClaimReceipt{}, errors.New("pool share not found for stake reputation claim")
	}
	if msg.Height < share.LastReputationUpdate {
		return types.StakeReputationClaimReceipt{}, errors.New("stake reputation claim height precedes previous update")
	}
	elapsed := msg.Height - share.LastReputationUpdate
	if share.LastReputationUpdate == 0 {
		elapsed = msg.Height - share.CreatedHeight
	}
	effectiveStake, err := poolShareActiveStakeExposure(next.State, pool, share)
	if err != nil {
		return types.StakeReputationClaimReceipt{}, err
	}
	delta, err := types.MulDivUint64(effectiveStake, elapsed, 1)
	if err != nil {
		return types.StakeReputationClaimReceipt{}, err
	}
	share.StakeWeightedSeconds, err = types.CheckedAddUint64(share.StakeWeightedSeconds, delta)
	if err != nil {
		return types.StakeReputationClaimReceipt{}, err
	}
	share.LastReputationUpdate = msg.Height
	share.UpdatedHeight = msg.Height
	next.State.PoolShares[shareIdx] = share

	scoreDelta, err := types.MulDivUint64(delta, uint64(k.genesis.Params.ReputationStakeWeightBps), uint64(types.MaxBasisPoints))
	if err != nil {
		return types.StakeReputationClaimReceipt{}, err
	}
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return types.StakeReputationClaimReceipt{}, err
	}
	if err := k.saveGenesis(next); err != nil {
		return types.StakeReputationClaimReceipt{}, err
	}
	rawOwner, err := types.RawAddressForUserAddress(msg.OwnerAddress)
	if err != nil {
		return types.StakeReputationClaimReceipt{}, err
	}
	return types.StakeReputationClaimReceipt{
		Account:         msg.OwnerAddress,
		PoolID:          msg.PoolID,
		ReputationDelta: scoreDelta,
		Height:          msg.Height,
		InternalMetadata: types.PoolStateMetadata{
			OwnerRaw:    rawOwner,
			TouchedKeys: []string{string(types.PoolShareKey(msg.PoolID, msg.OwnerAddress))},
		},
	}, nil
}

func (k *Keeper) SyncPoolRewards(msg types.MsgSyncPoolRewards) (types.PoolRewardSummary, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	idx, pool, found := k.lookupPool(msg.PoolID)
	if !found {
		return types.PoolRewardSummary{}, errors.New("nominator pool not found")
	}
	nextPool, summary, err := types.SyncPoolRewards(k.genesis.Params, pool, msg)
	if err != nil {
		return types.PoolRewardSummary{}, err
	}
	k.counters.ValidatorAllocationReads += summary.AllocationsTouched
	next := cloneGenesis(k.genesis)
	next.State.Pools[idx] = nextPool
	if err := k.saveGenesis(next); err != nil {
		return types.PoolRewardSummary{}, err
	}
	if nextPool.OfficialLiquidStaking {
		if err := k.upsertLiquidPoolAfterPoolMutation(nextPool, msg.Height); err != nil {
			return types.PoolRewardSummary{}, err
		}
	}
	return summary, nil
}

func (k *Keeper) ClaimStakingRewards(msg types.MsgClaimStakingRewards) (uint64, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if err := k.genesis.Params.Authorize(msg.Authority); err != nil {
		return 0, err
	}
	if !msg.InternalMigration {
		return 0, errors.New("direct staking reward claims are internal migration only; use pool reward claims")
	}
	if msg.Height == 0 {
		return 0, errors.New("staking reward claim height must be positive")
	}
	if err := types.ValidateRawAddress("staking reward delegator", msg.Delegator); err != nil {
		return 0, err
	}
	if err := types.ValidateRawAddress("staking reward validator", msg.Validator); err != nil {
		return 0, err
	}
	return 0, nil
}

func (k *Keeper) StakingProof(req types.StakingProofRequest) (types.StakingProofMetadata, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.counters.ProofQueries++
	return types.BuildStakingProofMetadata(req)
}

func (k *Keeper) UpdatePoolCommission(msg types.MsgUpdatePoolCommission) (types.NominatorPool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.genesis.Params.Authorize(msg.Authority); err != nil {
		return types.NominatorPool{}, err
	}
	idx, pool, found := findPool(k.genesis.State.Pools, msg.PoolID)
	if !found {
		return types.NominatorPool{}, errors.New("nominator pool not found")
	}
	if pool.PoolOperator != msg.PoolOperator {
		return types.NominatorPool{}, errors.New("nominator pool operator mismatch")
	}
	pool.PoolCommissionBps = msg.PoolCommissionBps
	return k.savePoolOnly(idx, pool)
}

func (k *Keeper) ChangePoolValidator(msg types.MsgChangePoolValidator) (types.NominatorPool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.genesis.Params.Authorize(msg.Authority); err != nil {
		return types.NominatorPool{}, err
	}
	if types.IsJailedValidatorStatus(msg.ValidatorStatus) {
		return types.NominatorPool{}, errors.New("nominator pool cannot delegate to jailed validator")
	}
	idx, pool, found := findPool(k.genesis.State.Pools, msg.PoolID)
	if !found {
		return types.NominatorPool{}, errors.New("nominator pool not found")
	}
	if pool.PoolOperator != msg.PoolOperator {
		return types.NominatorPool{}, errors.New("nominator pool operator mismatch")
	}
	if msg.Height == 0 {
		return types.NominatorPool{}, errors.New("nominator pool validator change height must be positive")
	}
	if pool.PendingValidatorTarget == msg.ValidatorTarget && msg.Height >= pool.ValidatorChangeHeight {
		pool.ValidatorTarget = msg.ValidatorTarget
		pool.PendingValidatorTarget = ""
		pool.ValidatorChangeHeight = 0
	} else if pool.PendingValidatorTarget == msg.ValidatorTarget {

	} else {
		pool.PendingValidatorTarget = msg.ValidatorTarget
		pool.ValidatorChangeHeight = msg.Height + k.genesis.Params.ValidatorChangeDelay
	}
	return k.savePoolOnly(idx, pool)
}

func (k *Keeper) ApplyPoolReward(poolID string, rewardAmount uint64) (types.NominatorPool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	idx, pool, found := findPool(k.genesis.State.Pools, poolID)
	if !found {
		return types.NominatorPool{}, errors.New("nominator pool not found")
	}
	if rewardAmount == 0 {
		return pool, nil
	}
	// SA2 #19: use the checked big.Int helper (as the sibling reward/slash paths
	// do) so a large reward * bps multiply cannot overflow uint64 and corrupt the
	// commission split.
	commission, err := types.MulDivUint64(rewardAmount, uint64(pool.PoolCommissionBps), uint64(types.MaxBasisPoints))
	if err != nil {
		return types.NominatorPool{}, err
	}
	netReward := rewardAmount - commission
	// Credit the reward only to the per-share RewardIndex, not to
	// TotalBondedStake (the principal/exchange-rate pool). Crediting both
	// double-counts every reward and makes the pool insolvent. See SEC-CRIT:
	// pool reward double-credit.
	delta, err := types.RewardDelta(netReward, pool.TotalShares)
	if err != nil {
		return types.NominatorPool{}, err
	}
	pool.RewardIndex += delta
	return k.savePoolOnly(idx, pool)
}

func (k *Keeper) ApplyPoolSlash(poolID string, slashAmount uint64) (types.NominatorPool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	idx, pool, found := findPool(k.genesis.State.Pools, poolID)
	if !found {
		return types.NominatorPool{}, errors.New("nominator pool not found")
	}
	if slashAmount == 0 {
		return pool, nil
	}
	if slashAmount > pool.TotalBondedStake {
		slashAmount = pool.TotalBondedStake
	}
	pool.TotalBondedStake -= slashAmount
	slashDelta, err := types.RewardDelta(slashAmount, pool.TotalShares)
	if err != nil {
		return types.NominatorPool{}, err
	}
	pool.SlashIndex += slashDelta
	return k.savePoolOnly(idx, pool)
}

// ApplyValidatorSlash reduces pooled stake and advances the pool slash index
// when a validator a pool delegates to is slashed.
//
// It is intentionally not reachable in production: it has no msg-server route
// and no caller. It is exercised only by tests. Do not delete it as dead code,
// and do not wire it into consensus slashing on its own.
//
// This used to be justified by there being no real pooled stake to slash --
// deposits were pure keeper accounting with no module account, no token custody
// and no cosmos delegation. That is no longer true: both DepositToPool and
// DepositToStakingPool now collect real coins into the pool module account and
// delegate them to the pool's ValidatorTarget (see depositCustody).
//
// So the gap this comment describes has inverted, and is worth stating plainly:
// pooled stake is now REAL and can really be slashed by cosmos x/slashing, but
// the pool's own ledger is not told. A fault burns the pool module account's
// delegation in x/staking while pool.TotalBondedStake and every delegator's
// share value keep their pre-slash numbers, so the ledger over-states what the
// pool can actually pay. The loss surfaces at the exit: withdrawalCustody's
// ValidateUnbondAmount reads real delegation tokens, so once slashing has eaten
// enough of them, unbonding the ledger's stated amount fails -- and the last
// delegators out cannot withdraw. Nothing over-pays (settleWithdrawal only pays
// what the pool really holds), but the shortfall lands on whoever exits last
// instead of being shared pro-rata at slash time, which is what this function
// exists to do.
//
// Wiring it into the x/evidence pipeline (runSlashingHooks, which today bridges
// faults to validator-registry, reputation and validator-insurance but
// deliberately NOT to nominator-pool) is the intended fix. It is a real change
// with real consensus consequences, not a comment edit, so it is deliberately
// left out of the custody pass that made pooled stake real.
//
// WHEN WIRING IT: this is an entry point, so like every Msg handler and the
// EndBlocker it must call loadForBlock(ctx) first, before it touches state.
// That is not only the FINDING-006 rehydration rule -- loadForBlock is also
// what re-establishes the baseline the storage layer diffs writes against, so
// an entry point that skips it can persist against a stale baseline and drop a
// write it should have made. See persistence.go.
func (k *Keeper) ApplyValidatorSlash(msg types.MsgApplyValidatorSlash) ([]types.ValidatorSlashEvent, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.genesis.Params.Authorize(msg.Authority); err != nil {
		return nil, err
	}

	var slashBps uint32
	switch msg.Fault {
	case types.SlashingFaultDowntime:
		slashBps = k.genesis.Params.DowntimeSlashBps
	case types.SlashingFaultDoubleSign:
		slashBps = k.genesis.Params.DoubleSignSlashBps
	default:
		return nil, errors.New("unknown slashing fault")
	}

	// Find and update the validator
	var validatorIdx int
	var validator types.Validator
	var found bool
	for i, v := range k.genesis.State.Validators {
		if v.Address == msg.ValidatorAddress {
			validatorIdx = i
			validator = v
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("validator not found")
	}

	// Determine new status
	var newStatus string
	var tombstoned bool
	switch msg.Fault {
	case types.SlashingFaultDowntime:
		newStatus = types.StateValidatorStatusJailed
	case types.SlashingFaultDoubleSign:
		newStatus = types.StateValidatorStatusSlashed
		if k.genesis.Params.DoubleSignTombstone {
			tombstoned = true
		}
	default:
		newStatus = validator.Status
	}

	validator.Status = newStatus
	validator.SlashingRiskBps = slashBps
	if msg.Fault == types.SlashingFaultDowntime {
		validator.Jailed = true
	}
	if tombstoned {
		validator.Tombstoned = true
	}
	k.genesis.State.Validators[validatorIdx] = validator

	// Create slash events for affected pools
	var events []types.ValidatorSlashEvent

	for poolIdx, pool := range k.genesis.State.Pools {
		slashAmount := uint64(0)

		for allocIdx, alloc := range pool.Allocations {
			if alloc.ValidatorAddress == msg.ValidatorAddress {
				loss, err := types.MulDivUint64(alloc.Amount, uint64(slashBps), uint64(types.MaxBasisPoints))
				if err != nil {
					return nil, err
				}
				// slashBps is bounds-checked <= MaxBasisPoints by
				// Params.Validate(), so loss <= alloc.Amount should already
				// hold; clamp defensively so a subtraction can never
				// underflow if that invariant is ever violated.
				if loss > alloc.Amount {
					loss = alloc.Amount
				}
				slashAmount += loss
				pool.Allocations[allocIdx].Amount -= loss
			}
		}

		if slashAmount > 0 {

			if pool.TotalBondedStake >= slashAmount {
				pool.TotalBondedStake -= slashAmount
			} else {
				pool.TotalBondedStake = 0
			}

			slashDelta, err := types.RewardDelta(slashAmount, pool.TotalShares)
			if err != nil {
				return nil, err
			}
			pool.SlashIndex += slashDelta

			k.genesis.State.Pools[poolIdx] = pool

			event := types.ValidatorSlashEvent{
				Height:              msg.Height,
				Validator:           msg.ValidatorAddress,
				PoolID:              pool.PoolID,
				Fault:               msg.Fault,
				Epoch:               msg.Epoch,
				SlashingLoss:        slashAmount,
				ValidatorStatus:     newStatus,
				Tombstoned:          tombstoned,
				PoolSlashIndexAfter: pool.SlashIndex,
			}
			events = append(events, event)

			for pvIdx, pv := range k.genesis.State.PoolValidatorAllocations {
				if pv.PoolID == pool.PoolID && pv.Validator == msg.ValidatorAddress {

					loss, err := types.MulDivUint64(pv.ActiveStake, uint64(slashBps), uint64(types.MaxBasisPoints))
					if err != nil {
						return nil, err
					}
					if pv.ActiveStake >= loss {
						k.genesis.State.PoolValidatorAllocations[pvIdx].ActiveStake -= loss
					} else {
						k.genesis.State.PoolValidatorAllocations[pvIdx].ActiveStake = 0
					}

					if newStatus != types.StateValidatorStatusActive {
						k.genesis.State.PoolValidatorAllocations[pvIdx].TargetWeightBps = 0
					}
				}
			}
		}
	}

	for _, event := range events {
		k.genesis.State.ValidatorSlashEvents = append(k.genesis.State.ValidatorSlashEvents, event)
	}

	if k.storeService != nil {
		if k.runtimeCtx == nil {
			k.runtimeCtx = context.Background()
		}

		// This mutates k.genesis in place rather than building a `next`, so
		// it must persist through the same diffing writer as every other
		// path: writing the genesis blob directly here would put Pools and
		// PoolShares back inside it and shadow their authoritative
		// per-entity records (see persistence.go).
		if err := k.writeDiff(k.runtimeCtx, k.genesis); err != nil {
			return nil, err
		}

		store := k.storeService.OpenKVStore(k.runtimeCtx)
		for _, alloc := range k.genesis.State.PoolValidatorAllocations {
			bz, err := json.Marshal(alloc)
			if err != nil {
				return nil, err
			}
			if err := store.Set(types.PoolAllocationKey(alloc.PoolID, alloc.Validator), bz); err != nil {
				return nil, err
			}
		}
	}

	return events, nil
}

// NominatorPool and the other query-surface methods below are reachable
// concurrently with block execution via the ordinary query_server.go gRPC
// handlers (not just the Simulate abuse path), so they need the lock too.
// They take the WRITE lock rather than RLock because they go through
// lookupPool/lookupDelegator, which can themselves mutate k.indexes via
// ensureIndexes -- see the Keeper.mu doc comment.
func (k *Keeper) NominatorPool(poolID string) (types.NominatorPool, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	_, pool, found := k.lookupPool(poolID)
	return pool, found
}

func (k *Keeper) NominatorPools() []types.NominatorPool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return types.SortPools(k.genesis.State.Pools)
}

func (k *Keeper) PoolDelegator(poolID string, delegator string) (types.DelegatorShare, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	_, _, found := k.lookupPool(poolID)
	if !found {
		return types.DelegatorShare{}, false
	}
	_, share, found := k.lookupDelegator(poolID, delegator)
	return share, found
}

func (k *Keeper) PoolRewards(poolID string, delegator string) (uint64, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	_, pool, found := k.lookupPool(poolID)
	if !found {
		return 0, false
	}
	_, share, found := k.lookupDelegator(poolID, delegator)
	if !found {
		return 0, false
	}
	reward, err := types.AccruedReward(share, pool.RewardIndex)
	if err != nil {
		return 0, false
	}
	return reward, true
}

func (k *Keeper) PoolShare(req types.QueryPoolShareRequest) (types.QueryPoolShareResponse, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	_, pool, found := k.lookupPool(req.PoolID)
	if !found {
		return types.QueryPoolShareResponse{}, false
	}
	_, share, found := k.lookupDelegator(req.PoolID, req.Delegator)
	if !found {
		return types.QueryPoolShareResponse{}, false
	}
	reward, err := types.AccruedReward(share, pool.RewardIndex)
	if err != nil {
		return types.QueryPoolShareResponse{}, false
	}
	return types.QueryPoolShareResponse{
		Share:          share,
		PendingRewards: reward,
	}, true
}

func (k *Keeper) PoolAllocations(req types.QueryPoolAllocationsRequest) (types.QueryPoolAllocationsResponse, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	_, pool, found := k.lookupPool(req.PoolID)
	if !found {
		return types.QueryPoolAllocationsResponse{}, false
	}
	return types.QueryPoolAllocationsResponse{Allocations: types.SortValidatorRewardAllocations(pool.ValidatorAllocations)}, true
}

func (k *Keeper) StakingRewards(req types.QueryStakingRewardsRequest) (types.QueryStakingRewardsResponse, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if !req.InternalMigration {
		return types.QueryStakingRewardsResponse{}, errors.New("staking rewards query is internal migration only; use pool rewards")
	}
	return types.QueryStakingRewardsResponse{RewardAmount: 0}, nil
}

func (k *Keeper) PoolUnbondingQueue(poolID string) []types.UnbondingEntry {
	k.mu.RLock()
	defer k.mu.RUnlock()
	_, pool, found := findPool(k.genesis.State.Pools, poolID)
	if !found {
		return []types.UnbondingEntry{}
	}
	return types.SortUnbonding(pool.UnbondingQueue)
}

type Migrator struct{ keeper *Keeper }

func NewMigrator(k *Keeper) Migrator  { return Migrator{keeper: k} }
func (m Migrator) Migrate1to2() error { return m.keeper.ExportGenesis().Validate() }
func (k Keeper) Migrate1to2State(ctx context.Context) error {
	_, err := k.ExportGenesisState(ctx)
	return err
}

func (k *Keeper) savePool(idx int, pool types.NominatorPool, delegator types.DelegatorShare) (types.DelegatorShare, error) {
	next := cloneGenesis(k.genesis)
	next.State.Pools[idx] = pool
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return types.DelegatorShare{}, err
	}
	if err := k.saveGenesis(next); err != nil {
		return types.DelegatorShare{}, err
	}
	return delegator, nil
}

func (k *Keeper) savePoolOnly(idx int, pool types.NominatorPool) (types.NominatorPool, error) {
	next := cloneGenesis(k.genesis)
	next.State.Pools[idx] = pool
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return types.NominatorPool{}, err
	}
	if err := k.saveGenesis(next); err != nil {
		return types.NominatorPool{}, err
	}
	return pool, nil
}

// normalizeAccountIdentity maps a caller's PLAIN wallet address — the "AE..."
// address a bank send / signature verifies against, and what wallets now put in
// staking message address fields so standard signer resolution can verify the tx
// (see types/signing.go) — to the account's canonical v2 identity: the "AE..."
// form of addressing.DeriveAccountAddress's output, which is the identity
// native-account records activation under.
//
// Its ONLY caller is ensureActiveWallet, and that is deliberate. The derivation
// is a one-way sha256 (see addressing.deriveV2RawAddress): the identity is a
// 32-byte address, no pubkey's 20-byte pk.Address() can ever equal it, so no
// signature can move coins out of it and it can never be inverted back to the
// plain address. That makes it usable ONLY as a lookup key for the activation
// check -- never as an account to debit, credit, or key a share by. The msg
// server used to rewrite wallet_address/owner_address to this identity before
// calling the keeper; that debited an empty derived address on deposit (every
// live deposit failed) and would have paid withdrawals and rewards into one
// permanently.
//
// It is idempotent: an address that is already a v2 identity is returned
// unchanged (NormalizeV2RawAddress passes v2/system-class addresses through), so
// callers and tests that already pass the v2 identity keep working untouched.
func normalizeAccountIdentity(userAddress string) (string, error) {
	seed, err := addressing.Parse(userAddress)
	if err != nil {
		return "", err
	}
	identity, err := addressing.NormalizeToAccountIdentity(seed)
	if err != nil {
		return "", err
	}
	return addressing.FormatUserFriendly(identity)
}

// ensureActiveWallet gates an action on the signer's account being activated
// and unfrozen. It takes the caller's PLAIN address -- the one that signed --
// and derives the account's v2 identity itself, because native-account (the
// real AccountStatusReader) keys its records by that identity: an activation
// stores the account under DeriveAccountAddress(pubKey).User, never under the
// plain address the pubkey hashes to.
//
// Deriving here, rather than rewriting the message in msg_server, is what keeps
// the identity confined to this check. Every other keeper use of the address --
// the bank debit, the payout recipient, the share/ledger key, the receipt --
// stays on the plain address, so money and bookkeeping agree and both entry
// points (msgServer and the direct keeper API) pass the same address in.
func (k *Keeper) ensureActiveWallet(address string, action string) error {
	if k.accountStatusReader == nil {
		return nil
	}
	identity, err := normalizeAccountIdentity(address)
	if err != nil {
		return err
	}
	status, found := k.accountStatusReader.AccountStatus(identity)
	if !found || status == accountStatusInactive {
		return errors.New(action + " requires active wallet")
	}
	if status == accountStatusFrozen {
		return errors.New(action + " rejected for frozen wallet; pay storage debt and unfreeze first")
	}
	if status != accountStatusActive {
		return errors.New(action + " requires active wallet")
	}
	return nil
}

func (k *Keeper) accrueOfficialPoolRent(liquid *types.LiquidStakingPool, pool types.NominatorPool, height uint64) error {
	if liquid == nil {
		return errors.New("official pool storage rent state is required")
	}
	if height == 0 {
		return errors.New("official pool storage rent height must be positive")
	}
	if liquid.LastStorageChargeHeight == 0 {
		liquid.LastStorageChargeHeight = height
		return nil
	}
	if height < liquid.LastStorageChargeHeight {
		return errors.New("official pool storage rent height must be monotonic")
	}
	if height == liquid.LastStorageChargeHeight {
		return nil
	}
	rate := k.genesis.Params.StorageRentRatePerByteSecond
	if rate == 0 {
		liquid.LastStorageChargeHeight = height
		return nil
	}
	elapsed := height - liquid.LastStorageChargeHeight
	footprint := officialPoolStorageFootprintBytes(pool, *liquid)
	charge, err := multiplyUint64Checked(elapsed, footprint, rate)
	if err != nil {
		return err
	}
	if liquid.StorageRentReserve >= charge {
		liquid.StorageRentReserve -= charge
	} else {
		unpaid := charge - liquid.StorageRentReserve
		liquid.StorageRentReserve = 0
		liquid.StorageRentDebt, err = types.CheckedAddUint64(liquid.StorageRentDebt, unpaid)
		if err != nil {
			return err
		}
	}
	liquid.LastStorageChargeHeight = height
	liquid.Status = pool.Status
	if liquid.StorageRentDebt > 0 && pool.Status == types.PoolStatusActive {
		liquid.Status = types.PoolStatusFrozenLimited
	}
	return nil
}

func officialPoolStorageFootprintBytes(pool types.NominatorPool, liquid types.LiquidStakingPool) uint64 {
	base := uint64(160)
	base += uint64(len(pool.PoolID) + len(pool.ContractAddressUser) + len(pool.ContractAddressRaw))
	base += uint64(len(liquid.ReceiptToken) + len(liquid.RentPayerPolicy) + len(liquid.Status))
	base += uint64(len(pool.DelegatorShares)) * 48
	base += uint64(len(pool.PendingWithdrawals)) * 56
	base += uint64(len(pool.Allocations)) * 40
	if base == 0 {
		return 1
	}
	return base
}

func multiplyUint64Checked(factors ...uint64) (uint64, error) {
	acc := big.NewInt(1)
	limit := new(big.Int).SetUint64(math.MaxUint64)
	for _, factor := range factors {
		acc.Mul(acc, new(big.Int).SetUint64(factor))
		if acc.Cmp(limit) > 0 {
			return 0, errors.New("nominator pool uint64 accounting overflow")
		}
	}
	return acc.Uint64(), nil
}

func (k *Keeper) upsertLiquidPoolAfterPoolMutation(pool types.NominatorPool, height uint64) error {
	idx, liquid, found := findLiquidPool(k.genesis.State.LiquidStakingPools, pool.PoolID)
	if !found {
		liquid = types.LiquidStakingPool{
			PoolID:              pool.PoolID,
			ContractAddressUser: pool.ContractAddressUser,
			ContractAddressRaw:  pool.ContractAddressRaw,
			ReceiptToken:        k.genesis.Params.PoolReceiptDenomOrCodeID,
			RentPayerPolicy:     types.RentPayerPolicyPoolReserve,
			Status:              pool.Status,
		}
	}
	liquid.ContractAddressUser = pool.ContractAddressUser
	liquid.ContractAddressRaw = pool.ContractAddressRaw
	liquid.TotalDeposited = pool.TotalBondedStake + totalPendingWithdrawalAmount(pool.PendingWithdrawals)
	liquid.TotalActiveStake = totalAllocated(pool.Allocations)
	liquid.TotalUnbonding = totalPendingWithdrawalAmount(pool.PendingWithdrawals)
	liquid.TotalShares = pool.TotalShares
	liquid.RewardIndex = pool.RewardIndex
	if err := k.accrueOfficialPoolRent(&liquid, pool, height); err != nil {
		return err
	}
	next := cloneGenesis(k.genesis)
	if found {
		next.State.LiquidStakingPools[idx] = liquid
	} else {
		next.State.LiquidStakingPools = append(next.State.LiquidStakingPools, liquid)
	}
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return err
	}
	return k.saveGenesis(next)
}

func (k *Keeper) upsertPoolShare(poolID string, owner string, delegator types.DelegatorShare, principalDelta uint64, height uint64) error {
	idx, share, found := findPoolShare(k.genesis.State.PoolShares, poolID, owner)
	if !found {
		share = types.PoolShare{
			Owner:                owner,
			PoolID:               poolID,
			CreatedHeight:        height,
			LastReputationUpdate: height,
		}
	}
	share.Shares = delegator.Shares
	share.PrincipalAmount += principalDelta
	share.UpdatedHeight = height
	share.LastRewardIndex = delegator.RewardIndexCheckpoint
	share.PendingRewards = delegator.PendingRewards
	next := cloneGenesis(k.genesis)
	if found {
		next.State.PoolShares[idx] = share
	} else {
		next.State.PoolShares = append(next.State.PoolShares, share)
	}
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return err
	}
	return k.saveGenesis(next)
}

func (k *Keeper) upsertPoolUnbonding(poolID string, owner string, withdrawal types.PendingWithdrawal) error {
	idx, request, found := findPoolUnbonding(k.genesis.State.PoolUnbondingRequests, poolID, owner, withdrawal.WithdrawalID)
	if !found {
		request = types.PoolUnbondingRequest{PoolID: poolID, Owner: owner, RequestID: withdrawal.WithdrawalID}
	}
	request.Shares = withdrawal.Shares
	request.Amount = withdrawal.Amount
	request.RequestHeight = withdrawal.RequestHeight
	request.CompleteHeight = withdrawal.CompleteHeight
	request.SettledAmount = withdrawal.SettledAmount
	request.Status = withdrawal.Status
	next := cloneGenesis(k.genesis)
	if found {
		next.State.PoolUnbondingRequests[idx] = request
	} else {
		next.State.PoolUnbondingRequests = append(next.State.PoolUnbondingRequests, request)
	}
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return err
	}
	return k.saveGenesis(next)
}

func (k *Keeper) updatePoolShareAfterUnbond(poolID string, owner string, withdrawal types.PendingWithdrawal, height uint64) error {
	idx, share, found := findPoolShare(k.genesis.State.PoolShares, poolID, owner)
	if !found {
		return nil
	}
	next := cloneGenesis(k.genesis)
	if withdrawal.Shares >= share.Shares {
		next.State.PoolShares = append(next.State.PoolShares[:idx], next.State.PoolShares[idx+1:]...)
	} else {
		share.Shares -= withdrawal.Shares
		if withdrawal.Amount >= share.PrincipalAmount {
			share.PrincipalAmount = 1
		} else {
			share.PrincipalAmount -= withdrawal.Amount
		}
		share.UpdatedHeight = height
		next.State.PoolShares[idx] = share
	}
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return err
	}
	return k.saveGenesis(next)
}

func (k *Keeper) upsertRewardClaim(poolID string, owner string, epoch uint64, amount uint64) error {
	if epoch == 0 {
		epoch = 1
	}
	idx, claim, found := findRewardClaim(k.genesis.State.RewardClaims, poolID, owner, epoch)
	if !found {
		claim = types.RewardClaim{PoolID: poolID, Owner: owner, Epoch: epoch}
	}
	var err error
	claim.Amount, err = types.CheckedAddUint64(claim.Amount, amount)
	if err != nil {
		return err
	}
	next := cloneGenesis(k.genesis)
	if found {
		next.State.RewardClaims[idx] = claim
	} else {
		next.State.RewardClaims = append(next.State.RewardClaims, claim)
	}
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return err
	}
	return k.saveGenesis(next)
}

func (k *Keeper) upsertPoolValidatorAllocation(poolID string, validatorAddress string, amount uint64, height uint64) error {
	// perform manual lookup with verbose debugging
	var validator types.Validator
	foundValidator := false
	for _, v := range k.genesis.State.Validators {
		if v.Address == validatorAddress {
			validator = v
			foundValidator = true
			break
		}
	}
	if !foundValidator || validator.Status != types.StateValidatorStatusActive {
		return errors.New("pool allocation requires registered active validator")
	}
	idx, allocation, found := findPoolValidatorAllocation(k.genesis.State.PoolValidatorAllocations, poolID, validatorAddress)
	if !found {
		allocation = types.PoolValidatorAllocation{PoolID: poolID, Validator: validatorAddress}
	}
	_, pool, poolFound := findPool(k.genesis.State.Pools, poolID)
	if !poolFound {
		return errors.New("nominator pool not found")
	}
	targetWeight := uint32(0)
	if pool.TotalBondedStake > 0 {
		weight, err := types.MulDivUint64(amount, uint64(types.MaxBasisPoints), pool.TotalBondedStake)
		if err != nil {
			return err
		}
		targetWeight = uint32(weight)
	}
	allocation.TargetWeightBps = targetWeight
	allocation.ActiveStake = amount
	allocation.PerformanceScore = validator.PerformanceScore
	allocation.CommissionBps = validator.CommissionBps
	allocation.SlashingRiskBps = validator.SlashingRiskBps
	allocation.UpdatedHeight = height

	if found {
		k.genesis.State.PoolValidatorAllocations[idx] = allocation
	} else {
		k.genesis.State.PoolValidatorAllocations = append(k.genesis.State.PoolValidatorAllocations, allocation)
	}
	k.genesis.State = k.genesis.State.Normalize(k.genesis.Params)
	k.rebuildIndexes()

	if k.storeService != nil {
		if k.runtimeCtx == nil {
			k.runtimeCtx = context.Background()
		}
		store := k.storeService.OpenKVStore(k.runtimeCtx)
		bz, err := json.Marshal(allocation)
		if err != nil {
			return err
		}
		if err := store.Set(types.PoolAllocationKey(poolID, validatorAddress), bz); err != nil {
			return err
		}
	}
	return nil
}

func (k Keeper) poolAllocationReceipt(pool types.NominatorPool, epoch uint64, height uint64) (types.PoolRebalanceReceipt, error) {
	allocations := []types.PoolValidatorAllocation{}
	touched := []string{string(types.PoolKey(pool.PoolID))}
	for _, allocation := range types.SortPoolValidatorAllocations(k.genesis.State.PoolValidatorAllocations) {
		if allocation.PoolID != pool.PoolID {
			continue
		}
		allocations = append(allocations, allocation)
		touched = append(touched, string(types.PoolAllocationKey(pool.PoolID, allocation.Validator)))
	}
	return types.PoolRebalanceReceipt{
		PoolID:      pool.PoolID,
		Epoch:       epoch,
		Height:      height,
		Allocations: allocations,
		InternalMetadata: types.PoolStateMetadata{
			PoolContractAddressRaw: pool.ContractAddressRaw,
			TouchedKeys:            touched,
		},
	}, nil
}

func cloneGenesis(gs GenesisState) GenesisState {
	gs.State = gs.State.Normalize(gs.Params)
	return gs
}

func findPool(pools []types.NominatorPool, poolID string) (int, types.NominatorPool, bool) {
	for idx, pool := range pools {
		if pool.PoolID == poolID {
			return idx, pool, true
		}
	}
	return -1, types.NominatorPool{}, false
}

func findDelegator(delegators []types.DelegatorShare, delegator string) (int, types.DelegatorShare, bool) {
	for idx, share := range delegators {
		if share.Delegator == delegator {
			return idx, share, true
		}
	}
	return -1, types.DelegatorShare{}, false
}

func findWithdrawal(withdrawals []types.PendingWithdrawal, withdrawalID string) (int, types.PendingWithdrawal, bool) {
	for idx, withdrawal := range withdrawals {
		if withdrawal.WithdrawalID == withdrawalID {
			return idx, withdrawal, true
		}
	}
	return -1, types.PendingWithdrawal{}, false
}

func findAllocation(allocations []types.PoolAllocation, validatorAddress string) (int, types.PoolAllocation, bool) {
	for idx, allocation := range allocations {
		if allocation.ValidatorAddress == validatorAddress {
			return idx, allocation, true
		}
	}
	return -1, types.PoolAllocation{}, false
}

func findValidator(validators []types.Validator, validatorAddress string) (int, types.Validator, bool) {
	for idx, validator := range validators {
		if validator.Address == validatorAddress {
			return idx, validator, true
		}
	}
	return -1, types.Validator{}, false
}

func findLiquidPool(pools []types.LiquidStakingPool, poolID string) (int, types.LiquidStakingPool, bool) {
	for idx, pool := range pools {
		if pool.PoolID == poolID {
			return idx, pool, true
		}
	}
	return -1, types.LiquidStakingPool{}, false
}

func findPoolShare(shares []types.PoolShare, poolID string, owner string) (int, types.PoolShare, bool) {
	for idx, share := range shares {
		if share.PoolID == poolID && share.Owner == owner {
			return idx, share, true
		}
	}
	return -1, types.PoolShare{}, false
}

func findPoolUnbonding(requests []types.PoolUnbondingRequest, poolID string, owner string, requestID string) (int, types.PoolUnbondingRequest, bool) {
	for idx, request := range requests {
		if request.PoolID == poolID && request.Owner == owner && request.RequestID == requestID {
			return idx, request, true
		}
	}
	return -1, types.PoolUnbondingRequest{}, false
}

func findPoolValidatorAllocation(allocations []types.PoolValidatorAllocation, poolID string, validator string) (int, types.PoolValidatorAllocation, bool) {
	for idx, allocation := range allocations {
		if allocation.PoolID == poolID && allocation.Validator == validator {
			return idx, allocation, true
		}
	}
	return -1, types.PoolValidatorAllocation{}, false
}

func findRewardClaim(claims []types.RewardClaim, poolID string, owner string, epoch uint64) (int, types.RewardClaim, bool) {
	for idx, claim := range claims {
		if claim.PoolID == poolID && claim.Owner == owner && claim.Epoch == epoch {
			return idx, claim, true
		}
	}
	return -1, types.RewardClaim{}, false
}

func poolShareActiveStakeExposure(state types.State, pool types.NominatorPool, share types.PoolShare) (uint64, error) {
	if share.Shares == 0 || pool.TotalShares == 0 {
		return 0, nil
	}
	activeStake := totalAllocated(pool.Allocations)
	if _, liquid, found := findLiquidPool(state.LiquidStakingPools, pool.PoolID); found {
		activeStake = liquid.TotalActiveStake
	}
	if activeStake == 0 {
		return 0, nil
	}
	return types.MulDivUint64(activeStake, share.Shares, pool.TotalShares)
}

func totalAllocated(allocations []types.PoolAllocation) uint64 {
	total := uint64(0)
	for _, allocation := range allocations {
		total += allocation.Amount
	}
	return total
}

func totalPendingWithdrawalAmount(withdrawals []types.PendingWithdrawal) uint64 {
	total := uint64(0)
	for _, withdrawal := range withdrawals {
		if withdrawal.Status == types.WithdrawalStatusPending {
			total += withdrawal.Amount
		}
	}
	return total
}

// EndBlocker settles each pool's matured withdrawal cohort: once the
// withdrawals' CompleteHeight has passed AND x/staking has finished delivering
// everything those withdrawals will ever receive (see hasUnbondingInFlight),
// it pays the depositors pro-rata out of what actually came back and marks the
// withdrawals completed. Until then it retries next block rather than paying
// out money that has not arrived or that belongs to a later withdrawal.
//
// The settlement rule, and why an in-flight slash no longer strands a
// depositor's principal forever, is documented on settlePoolWithdrawals.
//
// A no-op when custody isn't wired (see hasCustody) -- there is nothing to
// settle for a ledger-only pool, and every existing test constructs a keeper
// this way.
func (k *Keeper) EndBlocker(ctx context.Context) error {
	if err := k.loadForBlock(ctx); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.hasCustody() {
		return nil
	}
	height := uint64(sdk.UnwrapSDKContext(ctx).BlockHeight())
	next := cloneGenesis(k.genesis)
	changed := false
	for poolIdx, pool := range next.State.Pools {
		settled, poolChanged, err := k.settlePoolWithdrawals(next.Params.BaseDenom, pool, height)
		if err != nil {
			return err
		}
		if poolChanged {
			next.State.Pools[poolIdx] = settled
			changed = true
		}
	}
	if !changed {
		return nil
	}
	next.State = next.State.Normalize(next.Params)
	if err := next.Validate(); err != nil {
		return err
	}
	return k.saveGenesis(next)
}

// claimRewardCustody opportunistically pulls the pool's real, actually
// accrued x/staking distribution rewards into the pool module account
// (ignoring "nothing to withdraw" -- the pool's ledger-computed reward may be
// smaller than, larger than, or claimed on a different cadence than what
// x/distribution has accrued; reconciling SyncPoolRewards' external
// injection with real distribution income exactly is a deliberate follow-up,
// not attempted here), then pays the claimant their ledger-computed reward
// ONLY if the pool account actually has the funds -- erroring cleanly rather
// than silently paying a partial amount if it doesn't. A no-op when custody
// isn't wired (see hasCustody). Assumes k.mu is already held by the caller.
func (k *Keeper) claimRewardCustody(rawDelegator, rawValidatorTarget string, rewardNaet uint64) error {
	if !k.hasCustody() {
		return nil
	}
	recipient, err := parsePoolAccAddress("nominator pool reward claimant", rawDelegator)
	if err != nil {
		return err
	}
	if strings.TrimSpace(rawValidatorTarget) != "" {
		if validatorAddr, err := parsePoolAccAddress("nominator pool validator target", rawValidatorTarget); err == nil {
			valAddr := sdk.ValAddress(validatorAddr.Bytes())
			// Best-effort: a pool with no live delegation yet, or with
			// nothing currently accrued, returns an error here that we
			// deliberately ignore -- the payout below still proceeds against
			// whatever the pool account already holds.
			_, _ = k.distrKeeper.WithdrawDelegationRewards(k.runtimeCtx, PoolModuleAddress(), valAddr)
		}
	}
	coins := sdk.NewCoins(sdk.NewInt64Coin(k.genesis.Params.BaseDenom, int64(rewardNaet)))
	spendable := k.bankKeeper.SpendableCoins(k.runtimeCtx, PoolModuleAddress())
	if !spendable.IsAllGTE(coins) {
		return fmt.Errorf("nominator pool account does not yet hold the claimed reward (has %s, needs %s) -- retry once real distribution income catches up", spendable.String(), coins.String())
	}
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(k.runtimeCtx, types.ModuleName, recipient, coins); err != nil {
		return fmt.Errorf("paying out nominator pool reward: %w", err)
	}
	return nil
}

// withdrawalUnbondHeight is the real x/staking height a withdrawal's unbonding
// entry was created at, falling back to the ledger's RequestHeight for rows
// written before UnbondHeight existed (and for the height-0 genesis contexts
// unit tests build, where withdrawalCustody legitimately reports 0). The
// fallback is only ever conservative in the direction that matters: a
// too-small height makes the cohort gate below wait longer, never settle
// earlier. See PendingWithdrawal.UnbondHeight.
func withdrawalUnbondHeight(withdrawal types.PendingWithdrawal) uint64 {
	if withdrawal.UnbondHeight != 0 {
		return withdrawal.UnbondHeight
	}
	return withdrawal.RequestHeight
}

// withdrawalUnbondValidator is the validator a withdrawal's principal is
// actually unbonding from, falling back to the pool's current target for rows
// written before UnbondValidator existed. See PendingWithdrawal.UnbondValidator
// for why the pool's live target is the wrong thing to gate on.
func withdrawalUnbondValidator(pool types.NominatorPool, withdrawal types.PendingWithdrawal) string {
	if strings.TrimSpace(withdrawal.UnbondValidator) != "" {
		return withdrawal.UnbondValidator
	}
	return pool.ValidatorTarget
}

// hasUnbondingInFlight reports whether x/staking still holds an unbonding
// entry at rawValidatorTarget that could be carrying money belonging to a
// cohort whose latest unbond started at unbondHeight.
//
// This is the race gate, and the reasoning is worth stating because it is the
// whole reason cohort settlement is safe:
//
//   - Every entry that holds cohort money was created by one of the cohort's
//     own Undelegate calls, so its CreationHeight is <= unbondHeight.
//   - x/staking's CompleteUnbonding removes an entry from the delegation at
//     the exact moment it credits the pool account (and removes the whole
//     UnbondingDelegation once the last entry goes). So once no entry with
//     CreationHeight <= unbondHeight remains, every coin the cohort will ever
//     receive has already landed.
//   - An entry with CreationHeight > unbondHeight belongs to a strictly later
//     withdrawal. It is still in the delegation, so its money has NOT arrived
//     and therefore cannot be in the balance we are about to split.
//
// Assumes k.mu is already held by the caller and that hasCustody() is true.
func (k *Keeper) hasUnbondingInFlight(rawValidatorTarget string, unbondHeight uint64) (bool, error) {
	validatorAddr, err := parsePoolAccAddress("nominator pool validator target", rawValidatorTarget)
	if err != nil {
		return false, err
	}
	valAddr := sdk.ValAddress(validatorAddr.Bytes())
	ubd, err := k.stakingKeeper.GetUnbondingDelegation(k.runtimeCtx, PoolModuleAddress(), valAddr)
	if err != nil {
		// Nothing unbonding from this validator at all -- every entry the
		// cohort created has already been completed and paid in.
		if errors.Is(err, stakingtypes.ErrNoUnbondingDelegation) {
			return false, nil
		}
		return false, fmt.Errorf("reading nominator pool unbonding delegation: %w", err)
	}
	for _, entry := range ubd.Entries {
		if entry.CreationHeight >= 0 && uint64(entry.CreationHeight) <= unbondHeight {
			return true, nil
		}
	}
	return false, nil
}

// settlePoolWithdrawals settles the pool's matured withdrawal cohort and
// reports whether it changed the pool. Assumes k.mu is already held (write) by
// the caller and that hasCustody() is true.
//
// It replaces a per-withdrawal "pay Amount in full or pay nothing" settle,
// which was an unconditional fund-loss bug (F-1). Shares are burned at unbond
// time, before the money comes back, and x/staking slashes an IN-FLIGHT
// unbonding entry for infractions committed before the unbond began -- that is
// the entire purpose of the unbonding period. So after any such slash strictly
// LESS than withdrawal.Amount ever arrives, an all-or-nothing settle can never
// succeed, and the EndBlocker retries it forever while the depositor's shares
// are already gone. A routine ~0.01% downtime slash stranded 100% of the
// principal. Even on a perfectly healthy chain, Unbond's TruncateInt can
// return Amount-1 and trigger the same permanent strand.
//
// The rule: settle a matured cohort together, pro-rata, once x/staking has
// finished delivering everything that cohort will ever get.
//
//	C         = matured, still-Pending withdrawals
//	expected  = sum of C's Amount claims
//	available = min(pool spendable balance, expected)
//	payout_w  = w.Amount * available / expected     (truncated)
//
// Why each piece is what it is:
//
//   - Cohort, not per-withdrawal, because a withdrawal cannot be linked to its
//     own proceeds even in principle. x/staking MERGES entries created in the
//     same block into one summed Balance (types.UnbondingDelegation.AddEntry),
//     so per-withdrawal attribution is destroyed inside x/staking itself, and
//     CompleteUnbonding pays a lump sum into a module account already
//     commingled with reward income. Any design keyed on per-withdrawal
//     proceeds is unimplementable.
//   - Gated on hasUnbondingInFlight, not on "pay whoever has coins now",
//     because SortWithdrawals orders PendingWithdrawals by WithdrawalID
//     LEXICOGRAPHICALLY, which is arbitrary with respect to maturity. Paying
//     greedily in iteration order would let a not-yet-funded withdrawal drain
//     coins that belong to a different one that already matured.
//   - Pro-rata, so a slash lands proportionally on the cohort that was in
//     flight when it happened, instead of being handed in full to whoever
//     x/staking's map iteration happened to reach first.
//   - Capped at expected, which is invariant I2: the pool can only ever pay a
//     cohort what that cohort claims. Reward dust sitting in the same module
//     account can top a slashed cohort back up (the pool self-heals in the
//     delegators' favour and the cap bounds it), but it can never be used to
//     pay out more than was claimed. Removing that top-up entirely needs a
//     BeforeValidatorSlashed staking hook and is a separate change.
//
// Amount is deliberately NOT overwritten with the payout: Validate rejects a
// zero Amount, so a heavily slashed withdrawal would fail next.Validate() and
// halt the EndBlocker -- turning a fund-loss bug into a chain-halt bug. The
// truth goes in SettledAmount, and a zero payout still reaches the terminal
// Completed status rather than staying Pending forever.
func (k *Keeper) settlePoolWithdrawals(baseDenom string, pool types.NominatorPool, height uint64) (types.NominatorPool, bool, error) {
	cohort := make([]int, 0, len(pool.PendingWithdrawals))
	unbondHeight := uint64(0)
	expected := uint64(0)
	targets := make([]string, 0, len(pool.PendingWithdrawals))
	for idx, withdrawal := range pool.PendingWithdrawals {
		if withdrawal.Status != types.WithdrawalStatusPending || height < withdrawal.CompleteHeight {
			continue
		}
		cohort = append(cohort, idx)
		if h := withdrawalUnbondHeight(withdrawal); h > unbondHeight {
			unbondHeight = h
		}
		if target := withdrawalUnbondValidator(pool, withdrawal); !slices.Contains(targets, target) {
			targets = append(targets, target)
		}
		total, err := types.CheckedAddUint64(expected, withdrawal.Amount)
		if err != nil {
			return pool, false, err
		}
		expected = total
	}
	if len(cohort) == 0 || expected == 0 {
		return pool, false, nil
	}
	// A cohort can span validators once ChangePoolValidator has moved the
	// pool's target, so every validator any member unbonded from has to be
	// clear. Sorted so the order of an error return cannot depend on map
	// iteration -- this runs in consensus.
	slices.Sort(targets)
	for _, target := range targets {
		inFlight, err := k.hasUnbondingInFlight(target, unbondHeight)
		if err != nil {
			return pool, false, err
		}
		if inFlight {
			// The cohort's own money is still unbonding: retry next block
			// rather than paying it out of somebody else's balance.
			return pool, false, nil
		}
	}
	spendable := k.bankKeeper.SpendableCoins(k.runtimeCtx, PoolModuleAddress()).AmountOf(baseDenom)
	available := expected
	if expectedInt := sdkmath.NewIntFromUint64(expected); spendable.LT(expectedInt) {
		available = spendable.Uint64()
	}
	changed := false
	for _, idx := range cohort {
		withdrawal := pool.PendingWithdrawals[idx]
		payout := uint64(0)
		if available > 0 {
			share, err := types.MulDivUint64(withdrawal.Amount, available, expected)
			if err != nil {
				return pool, false, err
			}
			payout = share
		}
		if payout > 0 {
			recipient, err := parsePoolAccAddress("nominator pool withdrawal recipient", withdrawal.Delegator)
			if err != nil {
				return pool, false, err
			}
			coins := sdk.NewCoins(sdk.NewCoin(baseDenom, sdkmath.NewIntFromUint64(payout)))
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(k.runtimeCtx, types.ModuleName, recipient, coins); err != nil {
				return pool, false, fmt.Errorf("paying out nominator pool withdrawal: %w", err)
			}
		}
		withdrawal.SettledAmount = payout
		withdrawal.Status = types.WithdrawalStatusCompleted
		pool.PendingWithdrawals[idx] = withdrawal
		for entryIdx, entry := range pool.UnbondingQueue {
			if entry.WithdrawalID == withdrawal.WithdrawalID {
				entry.Status = types.WithdrawalStatusCompleted
				pool.UnbondingQueue[entryIdx] = entry
			}
		}
		changed = true
	}
	return pool, changed, nil
}
