package adversarial_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	appparams "github.com/sovereign-l1/l1/app/params"
	feestypes "github.com/sovereign-l1/l1/x/fees/types"
	loadtypes "github.com/sovereign-l1/l1/x/load/types"
	meshtypes "github.com/sovereign-l1/l1/x/mesh/types"
	routingtypes "github.com/sovereign-l1/l1/x/routing/types"
	zonestypes "github.com/sovereign-l1/l1/x/zones/types"
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

		zone := adversarialZone(zonestypes.ZoneIDFinancial, zonestypes.ZoneKindFinancial, zonestypes.VMPolicyNativeModule)
		zone.FeePolicy = "uatom"
		_, err = zonestypes.RegisterZone(zonestypes.EmptyState(), zone)
		require.ErrorContains(t, err, "naet")
	})

	t.Run("zone roots match exported state commitment", func(t *testing.T) {
		state := zonestypes.EmptyState()
		var err error
		state, err = zonestypes.RegisterZone(state, adversarialZone(zonestypes.ZoneIDFinancial, zonestypes.ZoneKindFinancial, zonestypes.VMPolicyNativeModule))
		require.NoError(t, err)
		exported := state.Export()
		stateRoot := hashJSON(t, exported.Zones)
		commitment, err := zonestypes.NewZoneCommitment(
			zonestypes.ZoneIDFinancial,
			1,
			stateRoot,
			hashString("receipt-root"),
			hashString("message-root"),
			hashString("execution-root"),
			"",
		)
		require.NoError(t, err)
		next, err := zonestypes.AppendCommitment(exported, commitment)
		require.NoError(t, err)
		require.NoError(t, next.Export().Commitments[0].ValidateHash())

		tampered := next.Export()
		tampered.Commitments[0].StateRoot = hashString("tampered")
		require.ErrorContains(t, tampered.Validate(), "hash mismatch")
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

func FuzzMalformedZoneCommitmentsFailSafely(f *testing.F) {
	f.Add("FINANCIAL_ZONE", uint64(1), hashString("state"), hashString("receipt"), hashString("message"), hashString("execution"))
	f.Add("", uint64(0), "bad", "bad", "bad", "bad")
	f.Fuzz(func(t *testing.T, zone string, height uint64, stateRoot string, receiptRoot string, messageRoot string, executionRoot string) {
		commitment, err := zonestypes.NewZoneCommitment(zonestypes.ZoneID(zone), height, stateRoot, receiptRoot, messageRoot, executionRoot, "")
		if err != nil {
			return
		}
		require.NoError(t, commitment.ValidateHash())
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

func adversarialZone(id zonestypes.ZoneID, kind zonestypes.ZoneKind, vm zonestypes.VMPolicy) zonestypes.Zone {
	return zonestypes.Zone{
		ID:			id,
		Kind:			kind,
		VMPolicy:		vm,
		FeePolicy:		zonestypes.FeePolicyNaet,
		GenesisStateHash:	hashString(string(id) + "-genesis"),
		StateTransitionID:	"transition-" + string(id),
		UpgradePolicy:		zonestypes.UpgradePolicyGovernance,
		DataAvailabilityPolicy:	zonestypes.DataAvailabilityCoreCommitment,
		AuditStatus:		zonestypes.AuditStatusExperimental,
		ActivationHeight:	1,
	}
}

func hashJSON(t *testing.T, value any) string {
	t.Helper()
	bz, err := json.Marshal(value)
	require.NoError(t, err)
	return hashString(string(bz))
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
