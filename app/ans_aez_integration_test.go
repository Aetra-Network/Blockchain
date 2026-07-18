package app

import (
	"testing"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	appparams "github.com/sovereign-l1/l1/app/params"
	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
	feestypes "github.com/sovereign-l1/l1/x/fees/types"
	identityrootkeeper "github.com/sovereign-l1/l1/x/identity-root/keeper"
	identityroottypes "github.com/sovereign-l1/l1/x/identity-root/types"
)

// TestANSAEZFullAppIntegration is the cross-module proof that x/identity-root
// (ANS) and x/aez work as ONE WHOLE against a single REAL app instance -- not
// isolated bare keepers over separate in-memory stores (that style of proof
// lives in x/identity-root/keeper/zone_test.go and is a genuine but much
// weaker guarantee; this test is the "do these two modules actually cooperate
// inside the blockchain" version).
//
// It drives, in sequence, against the SAME app.L1App:
//
//  1. A real .aet REGISTER via the live auction path (MsgSendToNameCollection
//     through the actual gRPC msg server -> EndBlocker grants it), not a
//     direct keeper call that skips the auction machinery.
//  2. The domain's computed AEZ zone (the Task 1 capability added in
//     x/identity-root/keeper/zone.go), asserted against x/aez's own frozen
//     "name/alice.aet" golden bucket vector (220) AND against the Core-pin
//     invariant (I-9) that vector's OWN namespace carries.
//  3. A real MsgAttachDomain, and AccountHoldsDomain (the reputation fee
//     gate's reader) read back through the SAME app instance.
//  4. A real ordinary transaction whose ante-time admission runs BOTH the
//     ANS Phase B reputation-multiplier fee discount and the AEZ Phase 6
//     per-zone gas-quota check in one pass (app.FeesKeeper.
//     AnteHandlerDecorator, the exact decorator wired into the production
//     ante chain in app/handlers.go) -- proving neither breaks the other and
//     the discount is real, not just present in isolated unit tests.
//  5. A live governance-style routing-table update that remaps alice.aet's
//     OWN bucket (220) to a non-Core zone, followed by a re-query proving the
//     zone assignment is Core NOW for the same reason it was Core before:
//     x/aez/types.CorePinned(NamespaceName) short-circuits before the routing
//     table is ever read (see zone.go's NameZone doc). This is the honest
//     answer to "is the zone live-computed or snapshotted at registration" --
//     neither: it is permanently pinned, and this step proves that pin
//     survives a real routing-table mutation on this exact app instance.
func TestANSAEZFullAppIntegration(t *testing.T) {
	testApp := Setup(t, false)
	ctx := testApp.NewContext(false).WithBlockHeight(1)

	// Three distinct funded wallets in one call -- AddTestAddrsIncremental
	// always starts numbering at 0, so calling it more than once would hand
	// back the SAME address twice.
	addrs := AddTestAddrsIncremental(testApp, ctx, 3, sdkmath.NewInt(3_000_000_000)) // 3 AET each
	registrant, walletB, walletC := addrs[0], addrs[1], addrs[2]

	registrantAE, err := addressing.FormatUserFriendly(registrant)
	require.NoError(t, err)
	walletBAE, err := addressing.FormatUserFriendly(walletB)
	require.NoError(t, err)

	// Enable identity-root with fast-cycle, cheap test params on the SAME app
	// instance x/aez already genesis-initialized through the real InitChain
	// above (Setup -> SetupWithGenesisValSet -> InitChain + FinalizeBlock).
	// InitGenesisState is the real chain-upgrade genesis API, not a test
	// backdoor; it leaves every other keeper (including app.AEZKeeper, wired
	// as this keeper's zone resolver in app/keepers.go) untouched.
	gs := identityrootkeeper.DefaultGenesis()
	// gs.Params is x/internal/prototype.Params, a Go internal package this
	// app-level test cannot import directly -- set the field it needs
	// (Enabled) without naming the type, exactly as every other field below
	// is set on gs.IdentityParams without this file importing
	// identityroottypes.IdentityRootParams by value construction.
	gs.Params.Enabled = true
	gs.Params.TestnetProfile = true
	gs.IdentityParams.IssuanceAuctionDurationBlocks = 5
	gs.IdentityParams.PriceTable = []identityroottypes.PriceTier{
		{MinLabelLen: 3, PriceNaet: "1000000000"}, // flat 1 AET for any registrable label
	}
	require.NoError(t, gs.Validate())
	require.NoError(t, testApp.IdentityRootKeeper.InitGenesisState(ctx, gs))

	// --- 1. Register "alice.aet" via the real live auction flow. ---
	msgServer := identityrootkeeper.NewGRPCMsgServer(&testApp.IdentityRootKeeper)
	querySrv := identityrootkeeper.NewGRPCQueryServer(&testApp.IdentityRootKeeper)

	regRes, err := msgServer.SendToNameCollection(ctx, &identityroottypes.MsgSendToNameCollection{
		Sender:		registrantAE,
		Opcode:		identityroottypes.OpcodeRegister,
		Comment:	"alice",
		AmountNaet:	1_000_000_000,
	})
	require.NoError(t, err)
	require.True(t, regRes.AuctionOpened, "REGISTER on a free label must open a real issuance auction, not grant instantly")
	require.NotZero(t, regRes.DeadlineHeight)

	grantCtx := ctx.WithBlockHeight(int64(regRes.DeadlineHeight))
	require.NoError(t, testApp.IdentityRootKeeper.EndBlocker(grantCtx))

	record, found, err := testApp.IdentityRootKeeper.NameRecord("alice")
	require.NoError(t, err)
	require.True(t, found, "the EndBlocker must have granted the auction to its sole bidder")
	require.Equal(t, registrantAE, record.Owner, "the granted owner must be the registrant who won the auction")

	// --- 2. Query the domain's computed AEZ zone; match the golden vector. ---
	zoneResp, err := querySrv.NameZone(grantCtx, &identityroottypes.QueryNameZoneRequest{Name: "alice"})
	require.NoError(t, err)
	require.True(t, zoneResp.Resolved)
	require.Equal(t, uint32(220), zoneResp.Bucket,
		"must match x/aez/types/bucket_test.go's frozen name/alice.aet golden vector (bucket 220)")
	require.Equal(t, uint32(aeztypes.ZoneIDCore), zoneResp.Zone,
		"x/aez's NamespaceName is Core-pinned (I-9): every name lives in the Core Zone")

	// --- 3. Attach the domain to walletB; confirm the reputation fee-gate
	// reader (AccountHoldsDomain) reads correctly through the SAME app
	// instance, for the attached wallet AND for an unrelated one. ---
	_, err = msgServer.AttachDomain(grantCtx, &identityroottypes.MsgAttachDomain{
		Owner:	registrantAE,
		Fqdn:	"alice",
		Target:	walletBAE,
	})
	require.NoError(t, err)

	holdsB, err := testApp.IdentityRootKeeper.AccountHoldsDomain(grantCtx, walletB)
	require.NoError(t, err)
	require.True(t, holdsB, "AccountHoldsDomain must read the just-attached domain through the same app instance")

	holdsC, err := testApp.IdentityRootKeeper.AccountHoldsDomain(grantCtx, walletC)
	require.NoError(t, err)
	require.False(t, holdsC, "an unrelated wallet must not read as holding the domain")

	// --- 4. An ordinary transaction from the attached wallet: BOTH the
	// reputation-multiplier fee discount and the AEZ per-zone gas-quota
	// check run in the SAME ante pass (app.FeesKeeper.AnteHandlerDecorator --
	// the exact decorator app/handlers.go wires into the production ante
	// chain), and neither rejects a normal-sized tx. ---
	scoreB, foundB, err := testApp.FeesKeeper.GetReputationScore(grantCtx, walletB)
	require.NoError(t, err)
	require.True(t, foundB, "a wallet holding an attached domain must be reputation-GATED (ANS Phase B)")

	scoreC, foundC, err := testApp.FeesKeeper.GetReputationScore(grantCtx, walletC)
	require.NoError(t, err)
	require.False(t, foundC, "a plain wallet holding no domain must not be reputation-gated")

	feeParams, err := testApp.FeesKeeper.GetParams(grantCtx)
	require.NoError(t, err)
	feeFormulaParams, err := testApp.FeesKeeper.GetFeeFormulaParams(grantCtx)
	require.NoError(t, err)

	const gasLimit = uint64(200_000)
	const probeTxSizeBytes = uint64(250) // representative single-MsgSend size; identical for both

	// The exact production formula (x/fees/keeper/fee_policy.go's AdmitTx
	// calls this same function), called directly with each wallet's REAL
	// reputation-gate status, at otherwise-identical tx shape.
	reqB, err := feestypes.ComputeFullTransferFee(feeParams, feeFormulaParams, gasLimit, probeTxSizeBytes, 1, 0, scoreB, foundB, sdkmath.ZeroInt())
	require.NoError(t, err)
	reqC, err := feestypes.ComputeFullTransferFee(feeParams, feeFormulaParams, gasLimit, probeTxSizeBytes, 1, 0, scoreC, foundC, sdkmath.ZeroInt())
	require.NoError(t, err)
	require.True(t, reqB.LT(reqC),
		"the attached wallet's required fee must be strictly lower than an identical-shaped tx from a plain wallet -- the reputation discount must actually reduce the computed fee")

	// A fee strictly between the two requirements: clears the attached
	// wallet's discounted requirement but falls short of the plain wallet's
	// full requirement.
	mid := reqB.Add(reqC).QuoRaw(2)
	require.True(t, mid.GTE(reqB) && mid.LT(reqC), "test setup: midpoint fee must separate the two requirements")

	txConfig := testApp.TxConfig()
	buildFeeTx := func(sender sdk.AccAddress, fee sdkmath.Int) (sdk.Tx, []byte) {
		msg := banktypes.NewMsgSend(sender, registrant, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 1)))
		builder := txConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(msg))
		builder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(feestypes.BondDenom, fee)))
		builder.SetGasLimit(gasLimit)
		tx := builder.GetTx()
		txBytes, encErr := txConfig.TxEncoder()(tx)
		require.NoError(t, encErr)
		return tx, txBytes
	}
	// fakeDeductAndExecuteNext stands in for "the rest of the standard SDK
	// chain" app/handlers.go wires AFTER FeesKeeper.AnteHandlerDecorator --
	// the real DeductFeeDecorator, which deposits the tx's declared fee into
	// the standard auth fee-collector module account before the tx executes.
	// AnteHandlerDecorator's OWN AdmitTx never moves money (it only
	// validates); a bare no-op next would leave the fee collector empty and
	// make the decorator's own post-next RecordCollectedFees fail with
	// insufficient funds -- not a bug in the code under test, just this
	// probe skipping the one accounting step it doesn't need to re-verify.
	fakeDeductAndExecuteNext := func(c sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		feeTx, ok := tx.(sdk.FeeTx)
		if !ok {
			return c, nil
		}
		payer := sdk.AccAddress(feeTx.FeePayer())
		if err := testApp.BankKeeper.SendCoinsFromAccountToModule(c, payer, authtypes.FeeCollectorName, feeTx.GetFee()); err != nil {
			return c, err
		}
		return c, nil
	}

	// At the midpoint fee: the attached wallet's tx must be admitted...
	txB, txBytesB := buildFeeTx(walletB, mid)
	_, errB := testApp.FeesKeeper.AnteHandlerDecorator(fakeDeductAndExecuteNext)(grantCtx.WithTxBytes(txBytesB), txB, false)
	require.NoError(t, errB, "the attached wallet's identically-shaped tx must be admitted at the discounted fee")

	// ...while the SAME fee, on the SAME tx shape, from an unattached wallet
	// must not -- isolating the reputation discount as the reason the
	// attached wallet's tx cleared admission.
	txC, txBytesC := buildFeeTx(walletC, mid)
	_, errC := testApp.FeesKeeper.AnteHandlerDecorator(fakeDeductAndExecuteNext)(grantCtx.WithTxBytes(txBytesC), txC, false)
	require.Error(t, errC, "the identical fee must be rejected for a plain (non-gated) wallet")

	// A generously-fee'd normal-sized tx from the attached wallet -- clearing
	// even the FULL (non-discounted) requirement -- must not be rejected by
	// the AEZ per-zone gas-quota check either: both checks ran in the SAME
	// AdmitTx pass above and neither conflicted with the other.
	txBFull, txBytesBFull := buildFeeTx(walletB, reqC.AddRaw(1))
	_, errBFull := testApp.FeesKeeper.AnteHandlerDecorator(fakeDeductAndExecuteNext)(grantCtx.WithTxBytes(txBytesBFull), txBFull, false)
	require.NoError(t, errBFull, "a generously-fee'd normal-sized tx from the attached wallet must clear both the fee check and the AEZ zone-quota check")

	// --- 5. A live routing-table update remaps alice.aet's OWN bucket (220)
	// to a non-Core zone; re-query and confirm the zone assignment is STILL
	// Core, for the honest reason (the Core pin, not a stale snapshot). ---
	current, err := testApp.AEZKeeper.GetRoutingTable(ctx)
	require.NoError(t, err)
	buckets := current.Buckets
	buckets[220] = aeztypes.ZoneID(2)
	newTable := aeztypes.NewRoutingTable(current.Version+1, current.Epoch+1, 10000, buckets)
	require.NoError(t, newTable.Validate())
	require.NoError(t, testApp.AEZKeeper.SetPendingRoutingTable(ctx, newTable))

	activateCtx := ctx.WithBlockHeight(10000)
	activated, err := testApp.AEZKeeper.MaybeActivatePendingRoutingTable(activateCtx)
	require.NoError(t, err)
	require.True(t, activated, "the staged table must activate at its ActivationHeight")

	// Sanity: an ORDINARY entity hashing into bucket 220 really does follow
	// the new table -- proving the swap is real, not a no-op, before the
	// name-specific pin assertion below. Searched deterministically (not left
	// to chance) so this assertion is never vacuous.
	movedEntityID := mustFindBucketEntity(t, 220)
	moved, zerr := testApp.AEZKeeper.ResolveZone(activateCtx, aeztypes.NamespaceNativeAccount, movedEntityID)
	require.NoError(t, zerr)
	require.Equal(t, aeztypes.ZoneID(2), moved.Zone, "an ordinary entity actually in bucket 220 must follow the new table")

	zoneRespAfter, err := querySrv.NameZone(activateCtx, &identityroottypes.QueryNameZoneRequest{Name: "alice"})
	require.NoError(t, err)
	require.True(t, zoneRespAfter.Resolved)
	require.Equal(t, uint32(220), zoneRespAfter.Bucket, "the bucket assignment itself never depends on the routing table")
	require.Equal(t, uint32(aeztypes.ZoneIDCore), zoneRespAfter.Zone,
		"NamespaceName is Core-pinned (I-9): the zone must stay Core even after a routing-table update remaps its own bucket elsewhere -- proving this is a permanent pin, not a registration-time snapshot that merely hasn't been refreshed")
}

// mustFindBucketEntity deterministically searches trivially-derived 20-byte
// addresses for one whose normalized native-account identity hashes into the
// requested bucket, so a routing-table test can prove a REAL entity's zone
// actually moves using only exported, deterministic functions -- no test-only
// hash bypass, and no reliance on a randomly-seeded address happening to land
// there.
func mustFindBucketEntity(t *testing.T, bucket aeztypes.BucketID) []byte {
	t.Helper()
	raw := make([]byte, 20)
	for counter := 0; counter < 100000; counter++ {
		raw[18] = byte(counter >> 8)
		raw[19] = byte(counter)
		identity, err := addressing.NormalizeToAccountIdentity(raw)
		require.NoError(t, err)
		if aeztypes.ComputeBucket(aeztypes.NamespaceNativeAccount, identity) == bucket {
			return identity
		}
	}
	t.Fatalf("no address found in bucket %d after exhausting search", bucket)
	return nil
}
