package async

import (
	"sort"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func FuzzQueueExportImportMatchesReferenceOrder(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4})
	f.Add([]byte("queue-seed"))

	f.Fuzz(func(t *testing.T, seed []byte) {
		executor := newTestExecutor(t)
		deployer := testAddr(seedByte(seed, 0))
		dest := deployTestContract(t, executor, deployer, []byte{seedByte(seed, 1)})
		require.NoError(t, executor.RegisterHandler(dest, func(contract ContractAccount, msg MessageEnvelope) ExecutionResult {
			return ExecutionResult{NewState: append([]byte(nil), contract.State...), ResultCode: ResultOK}
		}))

		messages := seededQueueMessages(seed, deployer, dest)
		require.NoError(t, executor.EnqueueTxMessages(messages))

		exported := executor.ExportState()
		expected := append([]QueuedMessage(nil), exported.Queue...)
		sort.SliceStable(expected, func(i, j int) bool {
			return queuedMessageLess(expected[i], expected[j])
		})
		require.Equal(t, queueQueryIDs(expected), queueQueryIDs(exported.Queue))

		imported, err := ImportState(exported)
		require.NoError(t, err)
		require.Equal(t, exported, imported.ExportState())
	})
}

func TestQueueCorpusMatchesReferenceOrder(t *testing.T) {
	cases := [][]byte{
		[]byte{1, 2, 3, 4},
		[]byte("queue-seed"),
		[]byte{0xff, 0x00, 0x7f},
	}

	for i, seed := range cases {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			executor := newTestExecutor(t)
			deployer := testAddr(seedByte(seed, 0))
			dest := deployTestContract(t, executor, deployer, []byte{seedByte(seed, 1)})
			require.NoError(t, executor.RegisterHandler(dest, func(contract ContractAccount, msg MessageEnvelope) ExecutionResult {
				return ExecutionResult{NewState: append([]byte(nil), contract.State...), ResultCode: ResultOK}
			}))

			messages := seededQueueMessages(seed, deployer, dest)
			require.NoError(t, executor.EnqueueTxMessages(messages))

			exported := executor.ExportState()
			expected := append([]QueuedMessage(nil), exported.Queue...)
			sort.SliceStable(expected, func(i, j int) bool {
				return queuedMessageLess(expected[i], expected[j])
			})
			require.Equal(t, queueQueryIDs(expected), queueQueryIDs(exported.Queue))

			imported, err := ImportState(exported)
			require.NoError(t, err)
			require.Equal(t, exported, imported.ExportState())
		})
	}
}

func seededQueueMessages(seed []byte, source, dest sdk.AccAddress) []MessageEnvelope {
	return []MessageEnvelope{
		seededMessage(seed, source, dest, 0),
		seededMessage(seed, source, dest, 1),
		seededMessage(seed, source, dest, 2),
	}
}

func seededMessage(seed []byte, source, dest sdk.AccAddress, index int) MessageEnvelope {
	msg := testMessage(testAddr(seedByte(seed, index+3)), testAddr(seedByte(seed, index+9)), uint64(seedByte(seed, index+6)))
	msg.Source = sdk.AccAddress(append([]byte(nil), source...))
	msg.Destination = sdk.AccAddress(append([]byte(nil), dest...))
	msg.QueryID = uint64(seedByte(seed, index+12))
	msg.DeliverAtBlock = uint64(seedByte(seed, index+15) % 4)
	msg.CreatedLogicalTime = uint64(index + 1)
	if seedByte(seed, index+18)%2 == 0 {
		msg.Bounce = false
	}
	if seedByte(seed, index+21)%3 == 0 {
		msg.Value = naetCoin(0)
	}
	return msg
}

func seedByte(seed []byte, index int) byte {
	if len(seed) == 0 {
		return byte(index + 1)
	}
	return seed[index%len(seed)]
}
