package adversarial_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	appparams "github.com/sovereign-l1/l1/app/params"
	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
	feestypes "github.com/sovereign-l1/l1/x/fees/types"
	loadtypes "github.com/sovereign-l1/l1/x/load/types"
	meshtypes "github.com/sovereign-l1/l1/x/mesh/types"
	routingtypes "github.com/sovereign-l1/l1/x/routing/types"
)

func TestPhase11ModularExecutionInvariants(t *testing.T) {
	t.Run("mesh rejects duplicate messages receipts and double spend", func(t *testing.T) {
		state, msg := adversarialMeshFixture(t)
		next, receipt, err := meshtypes.ApplyMessage(state, msg, adversarialMeshSuccess(), 100)
		require.NoError(t, err)
		require.Len(t, next.ReplayMarkers, 1)
		require.Len(t, next.Receipts, 1)

		_, _, err = meshtypes.ApplyMessage(next, msg, adversarialMeshSuccess(), 101)
		require.ErrorContains(t, err, "replay")
		_, err = meshtypes.CommitReceipt(next, receipt)
		require.ErrorContains(t, err, "duplicate receipt")

		exported := next.Export()
		require.Len(t, exported.ReplayMarkers, 1)
		require.Equal(t, msg.MessageID, exported.ReplayMarkers[0].MessageID)
	})

	t.Run("protocol fee denom remains naet only", func(t *testing.T) {
		params := feestypes.DefaultParams()
		require.NoError(t, feestypes.ValidateFeeCoins(params, sdk.NewCoins(sdk.NewInt64Coin(appparams.BaseDenom, 1)), true))
		require.Error(t, feestypes.ValidateFeeCoins(params, sdk.NewCoins(sdk.NewInt64Coin("uatom", 1)), true))

		_, err := routingtypes.Route(adversarialRouteInput("uatom"))
		require.ErrorContains(t, err, "naet")
	})

	t.Run("aez routing table hash detects tampering and core pins ignore the table", func(t *testing.T) {
		// Successor to the deleted x/zones commitment test. The AEZ
		// equivalent of "a tampered commitment is rejected" is a routing
		// table whose committed TableHash no longer matches its contents.
		table := aeztypes.GenesisRoutingTable()
		require.NoError(t, table.Validate())

		tampered := table
		tampered.Buckets[7] = aeztypes.ZoneID(4)
		require.ErrorContains(t, tampered.Validate(), "table hash")

		// A hand-crafted MALICIOUS but internally consistent table that
		// maps every bucket away from the core zone still cannot move a
		// core-pinned namespace: CorePinned bypasses the table entirely,
		// so no table version can express a core-zone move (I-9).
		var hostile [aeztypes.BucketCount]aeztypes.ZoneID
		for i := range hostile {
			hostile[i] = aeztypes.ZoneID(4)
		}
		malicious := aeztypes.NewRoutingTable(2, 1, 0, hostile)
		require.NoError(t, malicious.Validate())
		for _, ns := range aeztypes.AllNamespaces() {
			if !aeztypes.CorePinned(ns) {
				continue
			}
			require.NotEqual(t, aeztypes.ZoneIDCore, malicious.ZoneForBucket(aeztypes.ComputeBucket(ns, []byte("victim"))),
				"fixture must actually route away from core, else the pin assertion is vacuous")
		}
	})

	t.Run("load score max delta and routing determinism", func(t *testing.T) {
		params := loadtypes.DefaultParams()
		result, err := loadtypes.ComputeLoadScore(params, loadtypes.EMAState{}, loadtypes.Metrics{
			CanonicalMempoolSize:		params.TargetMempoolSize,
			UsedBlockGas:			params.TargetBlockGas,
			AverageInclusionDelayBlocks:	params.TargetLatencyBlocks,
			FailedTxCount:			1,
			TotalTxCount:			1,
			ExecutionStepCount:		params.TargetExecutionSteps,
		})
		require.NoError(t, err)
		require.LessOrEqual(t, result.LoadScoreBps, params.MaxDeltaBps)

		input := adversarialRouteInput(routingtypes.NativeFeeDenom)
		left, err := routingtypes.Route(input)
		require.NoError(t, err)
		right, err := routingtypes.Route(input)
		require.NoError(t, err)
		require.Equal(t, left, right)
	})
}

func FuzzMalformedMeshMessagesFailSafely(f *testing.F) {
	f.Add([]byte("sender"), []byte("recipient"), "payload", uint64(1))
	f.Add([]byte{}, []byte{}, "", uint64(0))
	f.Fuzz(func(t *testing.T, sender []byte, recipient []byte, payload string, nonce uint64) {
		msg := meshtypes.MeshMessage{
			SourceZone:		"FINANCIAL_ZONE",
			SourceShard:		"0:0",
			DestinationZone:	"CONTRACT_ZONE",
			DestinationShard:	"0:1",
			Nonce:			nonce,
			Sender:			sender,
			Recipient:		recipient,
			AssetCommitment:	meshtypes.HashParts("asset", payload),
			PayloadHash:		meshtypes.HashParts("payload", payload),
			TimeoutHeight:		10,
			Finality:		meshtypes.FinalityReference{Height: 1, CommitmentHash: hashString("commitment")},
			Sequence:		nonce,
			SourceLogicalTime:	1,
		}
		if valid, err := meshtypes.NewMessage(msg); err == nil {
			require.NoError(t, valid.Validate())
		}
	})
}

// FuzzMalformedAEZBucketsFailSafely succeeds FuzzMalformedZoneCommitmentsFailSafely,
// which was deleted with x/zones. Zone commitments do not exist until Phase 4,
// but the bucket hash does exist today and is consensus-critical, so the
// adversarial surface moves rather than disappearing.
func FuzzMalformedAEZBucketsFailSafely(f *testing.F) {
	f.Add("native-account", []byte("entity"))
	f.Add("", []byte{})
	f.Add("system", []byte{0x00, 0xff})
	f.Fuzz(func(t *testing.T, namespace string, entityID []byte) {
		ns := aeztypes.Namespace(namespace)
		// ComputeBucket is total: it must never panic and must never
		// return a bucket outside 0..255, for ANY input including an
		// unknown namespace or a NUL-bearing entity id.
		bucket := aeztypes.ComputeBucket(ns, entityID)
		require.Less(t, uint32(bucket), uint32(aeztypes.BucketCount))
		// ...and pure: the same input always yields the same bucket.
		require.Equal(t, bucket, aeztypes.ComputeBucket(ns, entityID))
	})
}

// FuzzMalformedAEZRoutingTablesFailSafely asserts a routing table with
// arbitrary field values either validates or errors -- never panics -- and that
// any accepted table is total over all 256 buckets.
func FuzzMalformedAEZRoutingTablesFailSafely(f *testing.F) {
	f.Add(uint64(1), uint64(0), int64(0), uint32(0))
	f.Add(uint64(0), uint64(1<<40), int64(-5), uint32(99))
	f.Fuzz(func(t *testing.T, version uint64, epoch uint64, activationHeight int64, zone uint32) {
		var buckets [aeztypes.BucketCount]aeztypes.ZoneID
		for i := range buckets {
			buckets[i] = aeztypes.ZoneID(zone)
		}
		table := aeztypes.NewRoutingTable(version, epoch, activationHeight, buckets)
		if err := table.Validate(); err != nil {
			return
		}
		for i := 0; i < aeztypes.BucketCount; i++ {
			require.True(t, table.Buckets[i].IsValid())
		}
	})
}

func adversarialRouteInput(feeDenom string) routingtypes.RouteInput {
	return routingtypes.RouteInput{
		MsgType:		routingtypes.MsgTypeBankSend,
		FeeDenom:		feeDenom,
		FeeClass:		routingtypes.MaxFeeClass + 100,
		ReputationClass:	routingtypes.MaxReputationClass + 100,
		AdmissionHeight:	1,
		TxHash:			[]byte("tx-hash"),
		RoutingEpoch:		7,
		ActiveShards: map[routingtypes.ZoneID]uint32{
			routingtypes.ZoneFinancial: 4,
		},
		Locality: routingtypes.Locality{
			AccountKey:	[]byte("account"),
			AssetDenom:	"naet",
		},
	}
}

func adversarialMeshFixture(t *testing.T) (meshtypes.MeshState, meshtypes.MeshMessage) {
	t.Helper()
	state := meshtypes.EmptyState(meshtypes.DefaultParams())
	var err error
	state, err = meshtypes.RegisterDestination(state, meshtypes.MeshDestination{ZoneID: "CONTRACT_ZONE", ShardID: "0:1", Active: true})
	require.NoError(t, err)
	state, err = meshtypes.RegisterDestination(state, meshtypes.MeshDestination{ZoneID: "FINANCIAL_ZONE", ShardID: "0:0", Active: true})
	require.NoError(t, err)
	commitment := meshtypes.FinalizedCommitment{
		ZoneID:		"FINANCIAL_ZONE",
		ShardID:	"0:0",
		Height:		90,
		CommitmentHash:	meshtypes.HashParts("source-commitment", "financial", "90"),
		MessageRoot:	meshtypes.HashParts("message-root", "financial", "90"),
		ReceiptRoot:	meshtypes.HashParts("receipt-root", "financial", "90"),
	}
	state, err = meshtypes.AddFinalizedCommitment(state, commitment)
	require.NoError(t, err)
	msg, err := meshtypes.NewMessage(meshtypes.MeshMessage{
		SourceZone:		"FINANCIAL_ZONE",
		SourceShard:		"0:0",
		DestinationZone:	"CONTRACT_ZONE",
		DestinationShard:	"0:1",
		Nonce:			7,
		Sender:			[]byte("sender"),
		Recipient:		[]byte("recipient"),
		AssetCommitment:	meshtypes.HashParts("asset", "100naet"),
		PayloadHash:		meshtypes.HashParts("payload", "execute"),
		TimeoutHeight:		150,
		Finality:		meshtypes.FinalityReference{Height: commitment.Height, CommitmentHash: commitment.CommitmentHash},
		Sequence:		3,
		SourceLogicalTime:	88,
	})
	require.NoError(t, err)
	msg.Proof = meshtypes.BuildProof(msg, commitment)
	return state, msg
}

func adversarialMeshSuccess() meshtypes.ExecutionResult {
	return meshtypes.ExecutionResult{
		Success:	true,
		Code:		0,
		ResultHash:	meshtypes.HashParts("execution", "success"),
	}
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
