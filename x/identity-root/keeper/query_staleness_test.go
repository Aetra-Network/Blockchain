package keeper

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/x/identity-root/types"
	"github.com/sovereign-l1/l1/x/internal/kvtest"
	"github.com/sovereign-l1/l1/x/internal/prototype"
)

// TestQueryAccessorsReadCommittedStoreNotStaleCache is the regression guard for
// the audit's CONFIRMED consensus-safety finding: every query accessor
// (NameRecord, ResolveName, ReverseRecord, Subdomains, IdentityRootParams, and
// the grpc_server.go *View helpers) used to read k.genesis directly, and
// NOTHING ever refreshed k.genesis except a Msg handler or the EndBlocker
// (loadForBlock). NewPersistentKeeper(store) initializes k.genesis to
// DefaultGenesis() in RAM -- so a freshly restarted or state-synced node's
// queries answered with DefaultGenesis() (e.g. "record not found" for a name
// that is very much registered in the committed store) until the next Msg or
// EndBlocker call happened to run first.
//
// This test builds one keeper, commits a registration through it, then builds
// a SECOND, completely fresh keeper over the SAME store -- exactly the shape
// of a restarted/state-synced node -- and queries it WITHOUT ever calling
// loadForBlock, a Msg handler, or EndBlocker on it first. Before the fix
// (viewGenesis routing every query accessor through the committed store when
// one is wired), this fails: found comes back false. After the fix, it must
// see the committed record.
func TestQueryAccessorsReadCommittedStoreNotStaleCache(t *testing.T) {
	svc := kvtest.NewStoreService()
	ctx := blockCtx(50)

	writer := NewPersistentKeeper(svc)
	gs := DefaultGenesis()
	gs.Params = prototype.TestnetParams()
	gs.IdentityParams.RegistrationPeriod = 100
	require.NoError(t, writer.InitGenesisState(ctx, gs))
	require.NoError(t, writer.loadForBlock(ctx))

	_, err := writer.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 10})
	require.NoError(t, err)
	_, err = writer.SetReverseRecord(types.MsgSetReverseRecord{Owner: ownerA, Address: ownerA, Name: "alice", Height: 10})
	require.NoError(t, err)
	_, err = writer.CreateSubdomain(types.MsgCreateSubdomain{Owner: ownerA, ParentName: "alice", Label: "app", Height: 10})
	require.NoError(t, err)

	// A brand-new keeper over the SAME store -- no InitGenesisState, no prior
	// loadForBlock, no Msg/EndBlocker call. This is exactly
	// NewPersistentKeeper(existingStore) from the audit's reproduction.
	restarted := NewPersistentKeeper(svc)

	record, found, err := restarted.NameRecord(ctx, "alice.aet")
	require.NoError(t, err)
	require.True(t, found, "a freshly restarted keeper's NameRecord query must see the committed record, not DefaultGenesis()")
	require.Equal(t, ownerA, record.Owner)

	_, _, active, err := restarted.ResolveName(ctx, "alice", 50)
	require.NoError(t, err)
	require.True(t, active, "ResolveName must see the committed record on a fresh keeper")

	reverse, found, err := restarted.ReverseRecord(ctx, ownerA)
	require.NoError(t, err)
	require.True(t, found, "ReverseRecord must see the committed reverse record on a fresh keeper")
	require.Equal(t, "alice.aet", reverse.Name)

	subs, err := restarted.Subdomains(ctx, "alice")
	require.NoError(t, err)
	require.Len(t, subs, 1, "Subdomains must see the committed record on a fresh keeper")
	require.Equal(t, "app.alice.aet", subs[0].Name)

	params, err := restarted.IdentityRootParams(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(100), params.RegistrationPeriod, "IdentityRootParams must read the committed params, not DefaultGenesis()'s")
}

// TestBareKeeperQueriesStillWorkWithoutAStore proves viewGenesis's fallback
// path: a keeper built with NewKeeper() (no storeService -- every existing
// unit test in this package) must keep answering queries from the in-memory
// cache exactly as before, since it has no committed store to read.
func TestBareKeeperQueriesStillWorkWithoutAStore(t *testing.T) {
	k := setupKeeper(t)
	_, err := k.RegisterName(types.MsgRegisterName{Owner: ownerA, Name: "alice", Height: 10})
	require.NoError(t, err)

	record, found, err := k.NameRecord(context.Background(), "alice.aet")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, ownerA, record.Owner)
}
