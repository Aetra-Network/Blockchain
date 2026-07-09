package conformance

import (
	"math/big"
	"path/filepath"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/async"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/compiler"
)

// TestAcceptanceDnsFamilyExample drives the DNS registry through a full
// lifecycle: register with bounce-revert, record deploy, claim, resolve,
// update, and non-owner rejection.
func TestAcceptanceDnsFamilyExample(t *testing.T) {
	deployer := testAddress(0x61)
	registrant := testAddress(0x62)
	target := testAddress(0x63)
	newTarget := testAddress(0x64)
	opts := compiler.Options{DeployerAddress: addressing.FormatAccAddress(deployer)}

	recordRes := compileExampleFile(t, filepath.Join("dns", "dns_record.atlx"), opts)
	registryRes := compileExampleFile(t, filepath.Join("dns", "dns_registry.atlx"), opts)
	require.NoError(t, avm.VerifyInterface(recordRes.Module, recordRes.Manifest))
	require.NoError(t, avm.VerifyInterface(registryRes.Module, registryRes.Manifest))

	registryStorage := mustAVMStorage(t, map[string]any{
		"owner":       addressing.FormatAccAddress(deployer),
		"recordCount": uint64(0),
		"recordCode":  recordRes.CodeChunk,
		"paused":      uint32(0),
	})
	registrySnapshot := avm.EncodeSnapshot(registryStorage)

	executor := mustExecutor(t)
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	registryAddr, err := executor.DeployContract(deployer, registryRes.ModuleHash[:], []byte("dns-registry"), registrySnapshot, sdkmath.NewInt(1_000_000))
	require.NoError(t, err)
	require.NoError(t, executor.RegisterHandler(registryAddr, runner.AsyncHandler(registryRes.Module, nil, avm.RuntimeContext{})))

	// Register before the record child exists: seed bounces, count reverts.
	registerBody := mustCodecBody(t, registryRes.MessageBodies["RegisterName"], map[string]any{
		"name":   "alice.aet",
		"target": addressing.FormatAccAddress(target),
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      registrant,
		Destination: registryAddr,
		Value:       acceptanceZeroCoin(),
		Opcode:      registryRes.MessageBodyOpcodes["RegisterName"],
		QueryID:     61,
		Body:        registerBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	_, err = executor.ProcessBlock(1)
	require.NoError(t, err)
	require.Equal(t, uint64(0), runGetterUint64(t, runner, registryRes, contractState(t, executor, registryAddr), "recordCount", registryAddr))

	// Deploy the record child at its deterministic address.
	recordStorage := mustAVMStorage(t, map[string]any{
		"registry":  addressing.FormatAccAddress(registryAddr),
		"name":      "alice.aet",
		"owner":     addressing.FormatAccAddress(registryAddr),
		"target":    nil,
		"updatedAt": int64(0),
	})
	recordSnapshot := avm.EncodeSnapshot(recordStorage)
	recordInit, recordAddr := deriveChildInit(t, registryAddr, recordRes.ModuleHash[:], recordSnapshot)

	seedBody := mustCodecBody(t, recordRes.MessageBodies["RecordSeed"], map[string]any{
		"name":   "alice.aet",
		"owner":  addressing.FormatAccAddress(registrant),
		"target": addressing.FormatAccAddress(target),
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      registryAddr,
		Destination: recordAddr,
		Value:       acceptanceCoin(1_000_000),
		Opcode:      recordRes.MessageBodyOpcodes["RecordSeed"],
		QueryID:     62,
		Body:        seedBody,
		StateInit:   &recordInit,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	_, err = executor.ProcessBlock(2)
	require.NoError(t, err)
	_, ok := executor.Contract(recordAddr)
	require.True(t, ok)
	require.NoError(t, executor.RegisterHandler(recordAddr, runner.AsyncHandler(recordRes.Module, nil, avm.RuntimeContext{})))

	// Claim the record via the seed now that the handler exists.
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      registryAddr,
		Destination: recordAddr,
		Value:       acceptanceCoin(1_000_000),
		Opcode:      recordRes.MessageBodyOpcodes["RecordSeed"],
		QueryID:     63,
		Body:        seedBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err := executor.ProcessBlock(3)
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	require.Equalf(t, async.ResultOK, receipts[0].ResultCode, "seed receipt error: %s", receipts[0].Error)

	recordState := contractState(t, executor, recordAddr)
	require.Equal(t, addressing.FormatAccAddress(target), runGetterAddress(t, runner, recordRes, recordState, "resolve", recordAddr))
	require.Equal(t, addressing.FormatAccAddress(registrant), runGetterAddress(t, runner, recordRes, recordState, "ownerOf", recordAddr))

	// A second seed must be rejected: the record is already claimed.
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      registryAddr,
		Destination: recordAddr,
		Value:       acceptanceZeroCoin(),
		Opcode:      recordRes.MessageBodyOpcodes["RecordSeed"],
		QueryID:     64,
		Body:        seedBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err = executor.ProcessBlock(4)
	require.NoError(t, err)
	require.NotEmpty(t, receipts)
	require.NotEqual(t, async.ResultOK, receipts[0].ResultCode)

	// Owner updates the target.
	updateBody := mustCodecBody(t, recordRes.MessageBodies["UpdateTarget"], map[string]any{
		"target": addressing.FormatAccAddress(newTarget),
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      registrant,
		Destination: recordAddr,
		Value:       acceptanceZeroCoin(),
		Opcode:      recordRes.MessageBodyOpcodes["UpdateTarget"],
		QueryID:     65,
		Body:        updateBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err = executor.ProcessBlock(5)
	require.NoError(t, err)
	require.Equalf(t, async.ResultOK, receipts[0].ResultCode, "update receipt error: %s", receipts[0].Error)
	recordState = contractState(t, executor, recordAddr)
	require.Equal(t, addressing.FormatAccAddress(newTarget), runGetterAddress(t, runner, recordRes, recordState, "resolve", recordAddr))

	// Non-owner update must be rejected.
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      testAddress(0x69),
		Destination: recordAddr,
		Value:       acceptanceZeroCoin(),
		Opcode:      recordRes.MessageBodyOpcodes["UpdateTarget"],
		QueryID:     66,
		Body:        updateBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err = executor.ProcessBlock(6)
	require.NoError(t, err)
	require.NotEqual(t, async.ResultOK, receipts[0].ResultCode)
}

// TestAcceptanceNftFamilyExample drives the NFT collection through a full
// lifecycle: mint with bounce-revert of the index, item deploy, seed,
// transfer by owner, and rejection of a non-owner transfer.
func TestAcceptanceNftFamilyExample(t *testing.T) {
	deployer := testAddress(0x71)
	holder := testAddress(0x72)
	buyer := testAddress(0x73)
	opts := compiler.Options{DeployerAddress: addressing.FormatAccAddress(deployer)}

	itemRes := compileExampleFile(t, filepath.Join("nft", "nft_item.atlx"), opts)
	collectionRes := compileExampleFile(t, filepath.Join("nft", "nft_collection.atlx"), opts)
	require.NoError(t, avm.VerifyInterface(itemRes.Module, itemRes.Manifest))
	require.NoError(t, avm.VerifyInterface(collectionRes.Module, collectionRes.Manifest))

	collectionStorage := mustAVMStorage(t, map[string]any{
		"owner":       addressing.FormatAccAddress(deployer),
		"nextIndex":   uint64(0),
		"burnedCount": uint64(0),
		"itemCode":    itemRes.CodeChunk,
		"mintOpen":    uint32(1),
	})
	collectionSnapshot := avm.EncodeSnapshot(collectionStorage)

	executor := mustExecutor(t)
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	collectionAddr, err := executor.DeployContract(deployer, collectionRes.ModuleHash[:], []byte("nft-collection"), collectionSnapshot, sdkmath.NewInt(1_000_000))
	require.NoError(t, err)
	require.NoError(t, executor.RegisterHandler(collectionAddr, runner.AsyncHandler(collectionRes.Module, nil, avm.RuntimeContext{})))

	// Mint before the item child exists: the collection computes the item's
	// address itself (autoDeployItemAddress) and auto-attaches its StateInit
	// to the outgoing seed — but delivery still fails because no handler is
	// registered for that address yet in this test harness, so it bounces
	// and the collection must revert its optimistic index counter. The
	// bounced seed's Destination in the receipt is the collection's OWN
	// computed item address — using it (rather than re-deriving it
	// independently via a hand-built StateInit) is what keeps this test
	// honest about what the runtime actually computed.
	mintBody := mustCodecBody(t, collectionRes.MessageBodies["MintItem"], map[string]any{
		"to":      addressing.FormatAccAddress(holder),
		"content": "aetralis://item/0",
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      deployer,
		Destination: collectionAddr,
		Value:       acceptanceZeroCoin(),
		Opcode:      collectionRes.MessageBodyOpcodes["MintItem"],
		QueryID:     71,
		Body:        mintBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err := executor.ProcessBlock(1)
	require.NoError(t, err)
	require.Len(t, receipts, 3, "expected the primary mint, the failed seed cascade, and its bounce")
	require.Equalf(t, async.ResultOK, receipts[0].ResultCode, "primary mint failed: %s", receipts[0].Error)
	require.NotEqual(t, async.ResultOK, receipts[1].ResultCode, "seed cascade should fail: no handler registered yet")
	itemAddr := receipts[1].Destination
	require.NotEmpty(t, itemAddr)
	require.Equal(t, uint64(0), runGetterUint64(t, runner, collectionRes, contractState(t, executor, collectionAddr), "nextIndex", collectionAddr))

	// Register the item's handler now that its runtime-computed address is
	// known, then resend the SAME mint. This time the collection's cascade
	// (with its auto-attached StateInit) reaches a live handler, so the
	// child deploys and seeds itself in one real, unmodified flow.
	require.NoError(t, executor.RegisterHandler(itemAddr, runner.AsyncHandler(itemRes.Module, nil, avm.RuntimeContext{})))

	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      deployer,
		Destination: collectionAddr,
		Value:       acceptanceZeroCoin(),
		Opcode:      collectionRes.MessageBodyOpcodes["MintItem"],
		QueryID:     72,
		Body:        mintBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err = executor.ProcessBlock(2)
	require.NoError(t, err)
	require.Len(t, receipts, 2, "expected the primary mint and the successful seed cascade")
	require.Equalf(t, async.ResultOK, receipts[0].ResultCode, "primary mint failed: %s", receipts[0].Error)
	require.Equalf(t, async.ResultOK, receipts[1].ResultCode, "seed cascade failed: %s", receipts[1].Error)
	require.Equal(t, itemAddr, receipts[1].Destination, "the collection must compute the same item address across calls")

	require.Equal(t, uint64(1), runGetterUint64(t, runner, collectionRes, contractState(t, executor, collectionAddr), "nextIndex", collectionAddr))

	itemState := contractState(t, executor, itemAddr)
	require.Equal(t, addressing.FormatAccAddress(holder), runGetterAddress(t, runner, itemRes, itemState, "ownerOf", itemAddr))
	require.Equal(t, uint64(1), runGetterUint64(t, runner, itemRes, itemState, "isInitialized", itemAddr))

	// Owner transfers the item.
	transferBody := mustCodecBody(t, itemRes.MessageBodies["TransferItem"], map[string]any{
		"to":         addressing.FormatAccAddress(buyer),
		"responseTo": nil,
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      holder,
		Destination: itemAddr,
		Value:       acceptanceZeroCoin(),
		Opcode:      itemRes.MessageBodyOpcodes["TransferItem"],
		QueryID:     74,
		Body:        transferBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err = executor.ProcessBlock(4)
	require.NoError(t, err)
	require.Equalf(t, async.ResultOK, receipts[0].ResultCode, "transfer receipt error: %s", receipts[0].Error)

	itemState = contractState(t, executor, itemAddr)
	require.Equal(t, addressing.FormatAccAddress(buyer), runGetterAddress(t, runner, itemRes, itemState, "ownerOf", itemAddr))

	// The previous owner can no longer transfer.
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      holder,
		Destination: itemAddr,
		Value:       acceptanceZeroCoin(),
		Opcode:      itemRes.MessageBodyOpcodes["TransferItem"],
		QueryID:     75,
		Body:        transferBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err = executor.ProcessBlock(5)
	require.NoError(t, err)
	require.NotEqual(t, async.ResultOK, receipts[0].ResultCode)

	// Burn: the current owner destroys the item; the burn notice cascades to
	// the collection which counts it.
	burnBody := mustCodecBody(t, itemRes.MessageBodies["BurnItem"], map[string]any{})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      buyer,
		Destination: itemAddr,
		Value:       acceptanceZeroCoin(),
		Opcode:      itemRes.MessageBodyOpcodes["BurnItem"],
		QueryID:     76,
		Body:        burnBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err = executor.ProcessBlock(6)
	require.NoError(t, err)
	require.NotEmpty(t, receipts)
	require.Equalf(t, async.ResultOK, receipts[0].ResultCode, "burn receipt error: %s", receipts[0].Error)

	itemState = contractState(t, executor, itemAddr)
	require.Equal(t, uint64(2), runGetterUint64(t, runner, itemRes, itemState, "isInitialized", itemAddr), "burned item must be tombstoned")
	require.Equal(t, uint64(1), runGetterUint64(t, runner, collectionRes, contractState(t, executor, collectionAddr), "burnedCount", collectionAddr))

	// A burned item rejects every further operation.
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      buyer,
		Destination: itemAddr,
		Value:       acceptanceZeroCoin(),
		Opcode:      itemRes.MessageBodyOpcodes["BurnItem"],
		QueryID:     77,
		Body:        burnBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err = executor.ProcessBlock(7)
	require.NoError(t, err)
	require.NotEqual(t, async.ResultOK, receipts[0].ResultCode)
}

// TestAcceptanceDaoFamilyExample drives the DAO through a full lifecycle:
// proposal creation with bounce-revert, proposal deploy and seed, voting to
// the threshold, and the pass report cascading back to the core treasury.
func TestAcceptanceDaoFamilyExample(t *testing.T) {
	deployer := testAddress(0x81)
	author := testAddress(0x82)
	voterA := testAddress(0x83)
	voterB := testAddress(0x84)
	beneficiary := testAddress(0x85)
	opts := compiler.Options{DeployerAddress: addressing.FormatAccAddress(deployer)}

	proposalRes := compileExampleFile(t, filepath.Join("dao", "dao_proposal.atlx"), opts)
	coreRes := compileExampleFile(t, filepath.Join("dao", "dao_core.atlx"), opts)
	require.NoError(t, avm.VerifyInterface(proposalRes.Module, proposalRes.Manifest))
	require.NoError(t, avm.VerifyInterface(coreRes.Module, coreRes.Manifest))

	coreStorage := mustAVMStorage(t, map[string]any{
		"owner":          addressing.FormatAccAddress(deployer),
		"nextProposalId": uint64(0),
		"proposalCode":   proposalRes.CodeChunk,
		"voteThreshold":  uint64(2),
		"paused":         uint32(0),
	})
	coreSnapshot := avm.EncodeSnapshot(coreStorage)

	executor := mustExecutor(t)
	runner, err := avm.NewRunner(avm.DefaultParams())
	require.NoError(t, err)
	coreAddr, err := executor.DeployContract(deployer, coreRes.ModuleHash[:], []byte("dao-core"), coreSnapshot, sdkmath.NewInt(1_000_000))
	require.NoError(t, err)
	require.NoError(t, executor.RegisterHandler(coreAddr, runner.AsyncHandler(coreRes.Module, nil, avm.RuntimeContext{})))

	// Create before the proposal child exists: the core computes the
	// proposal's address itself (autoDeployProposalAddress) and auto-attaches
	// its StateInit to the outgoing seed — but delivery still fails because
	// no handler is registered for that address yet in this test harness, so
	// it bounces and the core must revert its optimistic id counter. The
	// bounced seed's Destination in the receipt is the core's OWN computed
	// proposal address — using it (rather than re-deriving it independently
	// via a hand-built StateInit) is what keeps this test honest about what
	// the runtime actually computed.
	createBody := mustCodecBody(t, coreRes.MessageBodies["CreateProposal"], map[string]any{
		"beneficiary": addressing.FormatAccAddress(beneficiary),
		"amount":      big.NewInt(300),
		"description": "fund the tooling team",
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      author,
		Destination: coreAddr,
		Value:       acceptanceZeroCoin(),
		Opcode:      coreRes.MessageBodyOpcodes["CreateProposal"],
		QueryID:     81,
		Body:        createBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err := executor.ProcessBlock(1)
	require.NoError(t, err)
	require.Len(t, receipts, 3, "expected the primary create, the failed seed cascade, and its bounce")
	require.Equalf(t, async.ResultOK, receipts[0].ResultCode, "primary create failed: %s", receipts[0].Error)
	require.NotEqual(t, async.ResultOK, receipts[1].ResultCode, "seed cascade should fail: no handler registered yet")
	proposalAddr := receipts[1].Destination
	require.NotEmpty(t, proposalAddr)
	require.Equal(t, uint64(0), runGetterUint64(t, runner, coreRes, contractState(t, executor, coreAddr), "nextProposalId", coreAddr))

	// Register the proposal's handler now that its runtime-computed address
	// is known, then resend the SAME create. This time the core's cascade
	// (with its auto-attached StateInit) reaches a live handler, so the
	// child deploys and seeds itself in one real, unmodified flow.
	require.NoError(t, executor.RegisterHandler(proposalAddr, runner.AsyncHandler(proposalRes.Module, nil, avm.RuntimeContext{})))

	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      author,
		Destination: coreAddr,
		Value:       acceptanceZeroCoin(),
		Opcode:      coreRes.MessageBodyOpcodes["CreateProposal"],
		QueryID:     82,
		Body:        createBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err = executor.ProcessBlock(2)
	require.NoError(t, err)
	require.Len(t, receipts, 2, "expected the primary create and the successful seed cascade")
	require.Equalf(t, async.ResultOK, receipts[0].ResultCode, "primary create failed: %s", receipts[0].Error)
	require.Equalf(t, async.ResultOK, receipts[1].ResultCode, "seed cascade failed: %s", receipts[1].Error)
	require.Equal(t, proposalAddr, receipts[1].Destination, "the core must compute the same proposal address across calls")

	require.Equal(t, uint64(1), runGetterUint64(t, runner, coreRes, contractState(t, executor, coreAddr), "nextProposalId", coreAddr))

	proposalState := contractState(t, executor, proposalAddr)
	require.Equal(t, uint64(1), runGetterUint64(t, runner, proposalRes, proposalState, "isOpen", proposalAddr))

	// First vote: proposal stays open below the threshold.
	voteBody := mustCodecBody(t, proposalRes.MessageBodies["CastVote"], map[string]any{
		"support": uint32(1),
	})
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      voterA,
		Destination: proposalAddr,
		Value:       acceptanceZeroCoin(),
		Opcode:      proposalRes.MessageBodyOpcodes["CastVote"],
		QueryID:     84,
		Body:        voteBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err = executor.ProcessBlock(4)
	require.NoError(t, err)
	require.Equalf(t, async.ResultOK, receipts[0].ResultCode, "vote receipt error: %s", receipts[0].Error)
	proposalState = contractState(t, executor, proposalAddr)
	require.Equal(t, uint64(1), runGetterUint64(t, runner, proposalRes, proposalState, "votes", proposalAddr))
	require.Equal(t, uint64(1), runGetterUint64(t, runner, proposalRes, proposalState, "isOpen", proposalAddr))

	// Second vote reaches the threshold: the proposal closes and reports
	// ProposalPassed back to the core, which pays the beneficiary.
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      voterB,
		Destination: proposalAddr,
		Value:       acceptanceZeroCoin(),
		Opcode:      proposalRes.MessageBodyOpcodes["CastVote"],
		QueryID:     85,
		Body:        voteBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err = executor.ProcessBlock(5)
	require.NoError(t, err)
	require.NotEmpty(t, receipts)

	proposalState = contractState(t, executor, proposalAddr)
	require.Equal(t, uint64(2), runGetterUint64(t, runner, proposalRes, proposalState, "votes", proposalAddr))
	require.Equal(t, uint64(0), runGetterUint64(t, runner, proposalRes, proposalState, "isOpen", proposalAddr))

	// Votes after close must be rejected.
	require.NoError(t, executor.EnqueueTxMessages([]async.MessageEnvelope{{
		Source:      voterA,
		Destination: proposalAddr,
		Value:       acceptanceZeroCoin(),
		Opcode:      proposalRes.MessageBodyOpcodes["CastVote"],
		QueryID:     86,
		Body:        voteBody,
		Bounce:      false,
		GasLimit:    100_000,
		ForwardFee:  acceptanceZeroCoin(),
	}}))
	receipts, err = executor.ProcessBlock(6)
	require.NoError(t, err)
	require.NotEqual(t, async.ResultOK, receipts[0].ResultCode)
}
