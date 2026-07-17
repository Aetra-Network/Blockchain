package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
	loadkeeper "github.com/sovereign-l1/l1/x/load/keeper"
	loadtypes "github.com/sovereign-l1/l1/x/load/types"
	meshkeeper "github.com/sovereign-l1/l1/x/mesh/keeper"
	meshtypes "github.com/sovereign-l1/l1/x/mesh/types"
	routingkeeper "github.com/sovereign-l1/l1/x/routing/keeper"
	routingtypes "github.com/sovereign-l1/l1/x/routing/types"
)

const (
	executionOSProfileBase		= "base"
	executionOSProfileSim		= "execution-os-sim"
	executionOSProfileAEZPrototype	= "aez-prototype"
	executionOSProfileMeshPrototype	= "mesh-prototype"
)

var executionOSProfiles = []string{
	executionOSProfileBase,
	executionOSProfileSim,
	executionOSProfileAEZPrototype,
	executionOSProfileMeshPrototype,
}

type executionOSReport struct {
	Profile		string			`json:"profile"`
	Load		executionOSLoadReport	`json:"load"`
	Routing		executionOSRouteReport	`json:"routing"`
	AEZ		executionOSAEZReport	`json:"aez"`
	Mesh		executionOSMeshReport	`json:"mesh"`
	RestartSafe	bool			`json:"restart_safe"`
	FeatureGated	bool			`json:"feature_gated"`
	ProductionLive	bool			`json:"production_live"`
}

type executionOSLoadReport struct {
	ScoreBps	uint32	`json:"score_bps"`
	Band		string	`json:"band"`
	WindowHeight	uint64	`json:"window_height"`
}

type executionOSRouteReport struct {
	MsgType		string	`json:"msg_type"`
	TxClass		string	`json:"tx_class"`
	ZoneID		string	`json:"zone_id"`
	ShardID		uint32	`json:"shard_id"`
	ActiveShards	uint32	`json:"active_shards"`
}

// executionOSAEZReport replaces the deleted x/zones report. It reports the AEZ
// routing table rather than a list of application-typed zones: under AEZ a zone
// is a state+execution CONTAINER, so what an operator needs to see is the
// bucket->zone map, not a zone taxonomy.
type executionOSAEZReport struct {
	TableVersion	uint64	`json:"table_version"`
	BucketCount	int	`json:"bucket_count"`
	// CoreOnly reports the Phase 1 invariant: every bucket maps to zone 0,
	// so no entity can route anywhere else.
	CoreOnly	bool	`json:"core_only"`
	TableHash	string	`json:"table_hash"`
}

type executionOSMeshReport struct {
	MessageID		string	`json:"message_id"`
	ReceiptStatus		string	`json:"receipt_status"`
	ReceiptHash		string	`json:"receipt_hash"`
	ReplayMarkerCount	int	`json:"replay_marker_count"`
	PendingMessages		int	`json:"pending_messages"`
}

type executionOSDiagnostics struct {
	Profile			string			`json:"profile"`
	Source			string			`json:"source"`
	FeatureGates		map[string]featureGate	`json:"feature_gates"`
	CurrentLoadScoreBps	uint32			`json:"current_load_score_bps"`
	LoadWindowHeight	uint64			`json:"load_window_height"`
	ActiveShards		[]zoneShardSummary	`json:"active_shards"`
	PendingMeshMessages	int			`json:"pending_mesh_messages"`
	ReplayMarkerCount	int			`json:"replay_marker_count"`
	MeshReceiptCount	int			`json:"mesh_receipt_count"`
	AEZTableVersion		uint64			`json:"aez_table_version"`
	AEZCoreOnly		bool			`json:"aez_core_only"`
	ProductionLive		bool			`json:"production_live"`
}

type featureGate struct {
	Enabled		bool	`json:"enabled"`
	TestnetProfile	bool	`json:"testnet_profile"`
}

type zoneShardSummary struct {
	ZoneID		string	`json:"zone_id"`
	ActiveShards	uint32	`json:"active_shards"`
}

func NewExecutionOSCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:	"execution-os",
		Short:	"Aetra modular execution OS operator tools",
	}
	cmd.AddCommand(
		newExecutionOSProfilesCmd(),
		newExecutionOSSmokeCmd(),
		newExecutionOSDiagnosticsCmd(),
	)
	return cmd
}

func newExecutionOSProfilesCmd() *cobra.Command {
	return &cobra.Command{
		Use:	"profiles",
		Short:	"List supported local execution OS profiles",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeJSON(cmd, struct {
				Profiles []string `json:"profiles"`
			}{Profiles: append([]string(nil), executionOSProfiles...)})
		},
	}
}

func newExecutionOSSmokeCmd() *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:	"smoke",
		Short:	"Run a deterministic execution OS simulator smoke scenario",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateExecutionOSProfile(profile); err != nil {
				return err
			}
			report, err := buildExecutionOSSmokeReport(profile)
			if err != nil {
				return err
			}
			return writeJSON(cmd, report)
		},
	}
	cmd.Flags().StringVar(&profile, "profile", executionOSProfileSim, "execution OS profile to simulate")
	return cmd
}

func newExecutionOSDiagnosticsCmd() *cobra.Command {
	var profile string
	var genesisPath string
	cmd := &cobra.Command{
		Use:	"diagnostics",
		Short:	"Inspect execution OS prototype state from genesis or simulator defaults",
		Args:	cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateExecutionOSProfile(profile); err != nil {
				return err
			}
			diagnostics, err := buildExecutionOSDiagnostics(profile, genesisPath)
			if err != nil {
				return err
			}
			return writeJSON(cmd, diagnostics)
		},
	}
	cmd.Flags().StringVar(&profile, "profile", executionOSProfileBase, "localnet execution OS profile")
	cmd.Flags().StringVar(&genesisPath, "genesis", "", "optional genesis.json path to inspect")
	return cmd
}

func buildExecutionOSSmokeReport(profile string) (executionOSReport, error) {
	loadResult, err := runLoadSmoke()
	if err != nil {
		return executionOSReport{}, err
	}
	route, err := routingtypes.Route(routingtypes.RouteInput{
		MsgType:		routingtypes.MsgTypeBankSend,
		FeeDenom:		routingtypes.NativeFeeDenom,
		FeeClass:		99,
		ReputationClass:	99,
		AdmissionHeight:	12,
		TxHash:			hashBytes("operator-smoke-tx"),
		RoutingEpoch:		1,
		ActiveShards: map[routingtypes.ZoneID]uint32{
			routingtypes.ZoneFinancial: 2,
		},
		Locality: routingtypes.Locality{
			AccountKey:	[]byte("operator-account"),
			AssetDenom:	"naet",
		},
	})
	if err != nil {
		return executionOSReport{}, err
	}
	aezReport := buildAEZSmokeReport()
	meshState, meshMsg, meshReceipt, err := runMeshSmoke()
	if err != nil {
		return executionOSReport{}, err
	}
	return executionOSReport{
		Profile:	profile,
		Load: executionOSLoadReport{
			ScoreBps:	loadResult.LoadScoreBps,
			Band:		string(loadResult.Band),
			WindowHeight:	loadResult.EMA.WindowHeight,
		},
		Routing: executionOSRouteReport{
			MsgType:	routingtypes.MsgTypeBankSend,
			TxClass:	string(route.TxClass),
			ZoneID:		string(route.ZoneID),
			ShardID:	uint32(route.ShardID),
			ActiveShards:	route.ActiveShards,
		},
		AEZ:	aezReport,
		Mesh: executionOSMeshReport{
			MessageID:		meshMsg.MessageID,
			ReceiptStatus:		string(meshReceipt.Status),
			ReceiptHash:		meshReceipt.ReceiptHash,
			ReplayMarkerCount:	len(meshState.ReplayMarkers),
			PendingMessages:	0,
		},
		RestartSafe:	true,
		FeatureGated:	profile != executionOSProfileBase,
		ProductionLive:	false,
	}, nil
}

func buildExecutionOSDiagnostics(profile, genesisPath string) (executionOSDiagnostics, error) {
	diag := executionOSDiagnostics{
		Profile:	profile,
		Source:		"simulator-defaults",
		FeatureGates: map[string]featureGate{
			"load":		{},
			"routing":	{},
			"aez":		{},
			"mesh":		{},
		},
		ProductionLive: false,
	}
	if genesisPath == "" {
		return diag, nil
	}
	bz, err := os.ReadFile(genesisPath)
	if err != nil {
		return executionOSDiagnostics{}, err
	}
	var genesis struct {
		AppState map[string]json.RawMessage `json:"app_state"`
	}
	if err := json.Unmarshal(bz, &genesis); err != nil {
		return executionOSDiagnostics{}, err
	}
	diag.Source = genesisPath
	if raw := genesis.AppState["load"]; len(raw) > 0 {
		var gs loadkeeper.GenesisState
		if err := json.Unmarshal(raw, &gs); err != nil {
			return executionOSDiagnostics{}, err
		}
		if err := gs.Validate(); err != nil {
			return executionOSDiagnostics{}, err
		}
		diag.FeatureGates["load"] = featureGate{Enabled: gs.Params.Enabled, TestnetProfile: gs.Params.TestnetProfile}
		diag.CurrentLoadScoreBps = gs.EMA.LoadScoreBps
		diag.LoadWindowHeight = gs.EMA.WindowHeight
	}
	if raw := genesis.AppState["routing"]; len(raw) > 0 {
		var gs routingkeeper.GenesisState
		if err := json.Unmarshal(raw, &gs); err != nil {
			return executionOSDiagnostics{}, err
		}
		if err := gs.Validate(); err != nil {
			return executionOSDiagnostics{}, err
		}
		diag.FeatureGates["routing"] = featureGate{Enabled: gs.Params.Enabled, TestnetProfile: gs.Params.TestnetProfile}
		diag.ActiveShards = make([]zoneShardSummary, len(gs.Shards))
		for i, shard := range gs.Shards {
			diag.ActiveShards[i] = zoneShardSummary{ZoneID: string(shard.ZoneID), ActiveShards: shard.ActiveShards}
		}
	}
	if raw := genesis.AppState["aez"]; len(raw) > 0 {
		var gs aeztypes.GenesisState
		if err := json.Unmarshal(raw, &gs); err != nil {
			return executionOSDiagnostics{}, err
		}
		if err := gs.Validate(); err != nil {
			return executionOSDiagnostics{}, err
		}
		diag.FeatureGates["aez"] = featureGate{Enabled: gs.Params.Prototype.Enabled, TestnetProfile: gs.Params.Prototype.TestnetProfile}
		diag.AEZTableVersion = gs.RoutingTable.Version
		diag.AEZCoreOnly = gs.IsCoreOnly()
	}
	if raw := genesis.AppState["mesh"]; len(raw) > 0 {
		var gs meshkeeper.GenesisState
		if err := json.Unmarshal(raw, &gs); err != nil {
			return executionOSDiagnostics{}, err
		}
		if err := gs.Validate(); err != nil {
			return executionOSDiagnostics{}, err
		}
		diag.FeatureGates["mesh"] = featureGate{Enabled: gs.Params.Enabled, TestnetProfile: gs.Params.TestnetProfile}
		diag.ReplayMarkerCount = len(gs.State.ReplayMarkers)
		diag.MeshReceiptCount = len(gs.State.Receipts)
	}
	sort.SliceStable(diag.ActiveShards, func(i, j int) bool {
		return diag.ActiveShards[i].ZoneID < diag.ActiveShards[j].ZoneID
	})
	return diag, nil
}

func runLoadSmoke() (loadtypes.Result, error) {
	params := loadtypes.DefaultParams()
	params.AlphaNumerator = 1
	params.AlphaDenominator = 1
	params.MaxDeltaBps = loadtypes.BasisPoints
	return loadtypes.ComputeLoadScore(params, loadtypes.EMAState{}, loadtypes.Metrics{
		CanonicalMempoolSize:		params.TargetMempoolSize,
		UsedBlockGas:			params.TargetBlockGas,
		AverageInclusionDelayBlocks:	params.TargetLatencyBlocks,
		FailedTxCount:			1,
		TotalTxCount:			1,
		ExecutionStepCount:		params.TargetExecutionSteps,
	})
}

// buildAEZSmokeReport reports the genesis AEZ routing table. It cannot fail:
// the table is a compile-time constant shape (all BucketCount buckets on the
// Core Zone), not a state machine to be driven like the old zones registry.
func buildAEZSmokeReport() executionOSAEZReport {
	gs := aeztypes.DefaultGenesis()
	return executionOSAEZReport{
		TableVersion:	gs.RoutingTable.Version,
		BucketCount:	aeztypes.BucketCount,
		CoreOnly:	gs.IsCoreOnly(),
		TableHash:	hex.EncodeToString(gs.RoutingTable.TableHash),
	}
}

func runMeshSmoke() (meshtypes.MeshState, meshtypes.MeshMessage, meshtypes.MeshReceipt, error) {
	state := meshtypes.EmptyState(meshtypes.DefaultParams())
	var err error
	state, err = meshtypes.RegisterDestination(state, meshtypes.MeshDestination{ZoneID: "CONTRACT_ZONE", ShardID: "0:1", Active: true})
	if err != nil {
		return meshtypes.MeshState{}, meshtypes.MeshMessage{}, meshtypes.MeshReceipt{}, err
	}
	state, err = meshtypes.RegisterDestination(state, meshtypes.MeshDestination{ZoneID: "FINANCIAL_ZONE", ShardID: "0:0", Active: true})
	if err != nil {
		return meshtypes.MeshState{}, meshtypes.MeshMessage{}, meshtypes.MeshReceipt{}, err
	}
	commitment := meshtypes.FinalizedCommitment{
		ZoneID:		"FINANCIAL_ZONE",
		ShardID:	"0:0",
		Height:		90,
		CommitmentHash:	meshtypes.HashParts("source-commitment", "financial", "0:0", "90"),
		MessageRoot:	meshtypes.HashParts("message-root", "financial", "90"),
		ReceiptRoot:	meshtypes.HashParts("receipt-root", "financial", "90"),
	}
	state, err = meshtypes.AddFinalizedCommitment(state, commitment)
	if err != nil {
		return meshtypes.MeshState{}, meshtypes.MeshMessage{}, meshtypes.MeshReceipt{}, err
	}
	msg, err := meshtypes.NewMessage(meshtypes.MeshMessage{
		SourceZone:		"FINANCIAL_ZONE",
		SourceShard:		"0:0",
		DestinationZone:	"CONTRACT_ZONE",
		DestinationShard:	"0:1",
		Nonce:			7,
		Sender:			[]byte("operator-sender"),
		Recipient:		[]byte("operator-contract"),
		AssetCommitment:	meshtypes.HashParts("asset", "100naet"),
		PayloadHash:		meshtypes.HashParts("payload", "execute"),
		TimeoutHeight:		150,
		Finality:		meshtypes.FinalityReference{Height: commitment.Height, CommitmentHash: commitment.CommitmentHash},
		Sequence:		3,
		SourceLogicalTime:	88,
	})
	if err != nil {
		return meshtypes.MeshState{}, meshtypes.MeshMessage{}, meshtypes.MeshReceipt{}, err
	}
	msg.Proof = meshtypes.BuildProof(msg, commitment)
	next, receipt, err := meshtypes.ApplyMessage(state, msg, meshtypes.ExecutionResult{
		Success:	true,
		Code:		0,
		ResultHash:	meshtypes.HashParts("execution", "success"),
	}, 100)
	if err != nil {
		return meshtypes.MeshState{}, meshtypes.MeshMessage{}, meshtypes.MeshReceipt{}, err
	}
	return next, msg, receipt, nil
}

func hashBytes(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

func hashString(value string) string {
	return hex.EncodeToString(hashBytes(value))
}

func validateExecutionOSProfile(profile string) error {
	for _, allowed := range executionOSProfiles {
		if profile == allowed {
			return nil
		}
	}
	return fmt.Errorf("unknown execution OS profile %q", profile)
}

func writeJSON(cmd *cobra.Command, value any) error {
	bz, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if len(bz) == 0 {
		return errors.New("empty JSON output")
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(bz))
	return err
}
