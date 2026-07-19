package types

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// This file round-trip tests the hand-written wire-format methods added in
// query_marshal.go/query_marshal_size.go/query_marshal_unmarshal.go for
// every type in the closure reachable from the 7 x/contracts query RPCs
// (Params, Code, Codes, Contract, Contracts, ContractStorage,
// ContractReceipts). Each test constructs a fully non-zero-value instance
// (every field populated, including nested messages and repeated slices
// with at least 2 distinct elements so ordering/off-by-one bugs surface),
// marshals it, unmarshals into a fresh zero value, and asserts the two are
// equal. A passing round trip is the actual evidence the wire format is
// correct -- not just that the code compiles.

func fixturePageRequestForTest(limit uint32) PageRequest {
	return PageRequest{Limit: limit}
}

func fixtureCodeDependencyForTest(suffix string) CodeDependency {
	return CodeDependency{
		CodeID:   "dep-code-id-" + suffix,
		CodeHash: strings.Repeat("a", 60) + suffix,
	}
}

func fixtureStateInitForTest(suffix string) StateInit {
	return StateInit{
		ABIVersion:         3,
		CodeID:             "state-init-code-" + suffix,
		CodeHash:           strings.Repeat("b", 60) + suffix,
		InitData:           []byte("init-data-" + suffix),
		Salt:               "salt-" + suffix,
		SaltBytes:          []byte("salt-bytes-" + suffix),
		Owner:              "owner-" + suffix,
		Libraries:          []CodeDependency{fixtureCodeDependencyForTest("1" + suffix), fixtureCodeDependencyForTest("2" + suffix)},
		InitialStorageRoot: strings.Repeat("c", 60) + suffix,
		InitialBalanceNAET: 123456,
		Capabilities:       []string{"cap-a-" + suffix, "cap-b-" + suffix},
	}
}

func fixtureCodeRecordForTest(suffix string) CodeRecord {
	return CodeRecord{
		CodeID:    "code-id-" + suffix,
		CodeHash:  strings.Repeat("d", 60) + suffix,
		CodeBytes: 4096,
		Bytecode:  []byte("AVM1 bytecode payload " + suffix),
		Owner:     "owner-" + suffix,
	}
}

func fixtureContractForTest(suffix string) Contract {
	return Contract{
		AddressUser:                    "user-address-" + suffix,
		AddressRaw:                     "raw-address-" + suffix,
		CodeID:                         "contract-code-id-" + suffix,
		CodeHash:                       strings.Repeat("e", 60) + suffix,
		StateInitHash:                  strings.Repeat("f", 60) + suffix,
		StateInit:                      fixtureStateInitForTest("nested-" + suffix),
		Creator:                        "creator-" + suffix,
		Owner:                          "owner-" + suffix,
		Admin:                          "admin-" + suffix,
		Upgradeable:                    true,
		UpgradesDisabled:               true,
		SystemOwned:                    true,
		StorageSchemaVersion:           7,
		InitMsg:                        []byte("init-msg-" + suffix),
		Data:                           []byte("data-" + suffix),
		Balance:                        99999,
		StateRoot:                      strings.Repeat("1", 60) + suffix,
		Status:                         ContractStatusFrozen,
		StorageBytes:                   2048,
		LastStorageChargeHeight:        55,
		StorageRentDebt:                77,
		LogicalTime:                    88,
		CreatedHeight:                  10,
		UpdatedHeight:                  20,
		PendingUpgradeCodeID:           "pending-code-id-" + suffix,
		PendingUpgradeMigrationHandler: "pending-handler-" + suffix,
		PendingUpgradeScheduledHeight:  30,
		PendingUpgradeEarliestHeight:   40,
	}
}

func fixtureContractStorageEntryForTest(suffix string) ContractStorageEntry {
	return ContractStorageEntry{
		ContractAddress: "contract-address-" + suffix,
		Key:             []byte("key-" + suffix),
		Value:           []byte("value-" + suffix),
	}
}

func fixtureContractReceiptForTest(suffix string) ContractReceipt {
	return ContractReceipt{
		ReceiptID:       strings.Repeat("2", 60) + suffix,
		ContractAddress: "contract-address-" + suffix,
		Actor:           "actor-" + suffix,
		Operation:       "operation-" + suffix,
		ExitCode:        7,
		Amount:          321,
		GasUsed:         654,
		LogicalTime:     987,
		Height:          15,
	}
}

func fixtureParamsForTest() Params {
	return Params{
		Authority:                     "authority-address",
		Enabled:                       true,
		MaxCodeBytes:                  111,
		MaxContractStorageBytes:       222,
		MaxGasPerExecution:            333,
		StorageRentPerByteBlock:       444,
		MaxInitDataBytes:              555,
		MaxStateInitSaltBytes:         666,
		MaxStateInitDependencies:      7,
		MaxInternalMessageGasPerBlock: 888,
		MinUpgradeDelay:               999,
	}
}

func TestQueryMarshalRoundTrip_QueryParamsRequest(t *testing.T) {
	original := QueryParamsRequest{}
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded QueryParamsRequest
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_QueryParamsResponse(t *testing.T) {
	original := QueryParamsResponse{Params: fixtureParamsForTest()}
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded QueryParamsResponse
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_Params(t *testing.T) {
	original := fixtureParamsForTest()
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded Params
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_QueryCodeRequest(t *testing.T) {
	original := QueryCodeRequest{CodeID: "code-request-id"}
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded QueryCodeRequest
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_QueryCodeResponse(t *testing.T) {
	original := QueryCodeResponse{Code: fixtureCodeRecordForTest("resp"), Found: true}
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded QueryCodeResponse
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_CodeRecord(t *testing.T) {
	original := fixtureCodeRecordForTest("standalone")
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded CodeRecord
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_QueryCodesRequest(t *testing.T) {
	original := QueryCodesRequest{Pagination: fixturePageRequestForTest(42)}
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded QueryCodesRequest
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_QueryCodesResponse(t *testing.T) {
	original := QueryCodesResponse{Codes: []CodeRecord{fixtureCodeRecordForTest("one"), fixtureCodeRecordForTest("two")}}
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded QueryCodesResponse
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_PageRequest(t *testing.T) {
	original := fixturePageRequestForTest(99)
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded PageRequest
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_QueryContractRequest(t *testing.T) {
	stateInit := fixtureStateInitForTest("req")
	original := QueryContractRequest{
		ContractAddress: "contract-address-req",
		ChainID:         "chain-id-req",
		Namespace:       "namespace-req",
		Deployer:        "deployer-req",
		StateInit:       &stateInit,
	}
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded QueryContractRequest
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_CodeDependency(t *testing.T) {
	original := fixtureCodeDependencyForTest("standalone")
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded CodeDependency
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_StateInit(t *testing.T) {
	original := fixtureStateInitForTest("standalone")
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded StateInit
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_Contract(t *testing.T) {
	original := fixtureContractForTest("standalone")
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded Contract
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_QueryContractResponse(t *testing.T) {
	original := QueryContractResponse{
		ContractAddress: "contract-address-resp",
		StateRoot:       strings.Repeat("9", 60) + "resp",
		Found:           true,
		Virtual:         true,
		Contract:        fixtureContractForTest("resp"),
		Status:          ContractStatusActive,
	}
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded QueryContractResponse
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_QueryContractsRequest(t *testing.T) {
	original := QueryContractsRequest{Pagination: fixturePageRequestForTest(13)}
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded QueryContractsRequest
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_QueryContractsResponse(t *testing.T) {
	original := QueryContractsResponse{Contracts: []Contract{fixtureContractForTest("one"), fixtureContractForTest("two")}}
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded QueryContractsResponse
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_QueryContractStorageRequest(t *testing.T) {
	original := QueryContractStorageRequest{
		ContractAddress: "contract-address-storage-req",
		KeyPrefix:       []byte("key-prefix-req"),
		Pagination:      fixturePageRequestForTest(21),
	}
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded QueryContractStorageRequest
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_ContractStorageEntry(t *testing.T) {
	original := fixtureContractStorageEntryForTest("standalone")
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded ContractStorageEntry
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_QueryContractStorageResponse(t *testing.T) {
	original := QueryContractStorageResponse{
		Entries: []ContractStorageEntry{
			fixtureContractStorageEntryForTest("one"),
			fixtureContractStorageEntryForTest("two"),
		},
	}
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded QueryContractStorageResponse
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_QueryContractReceiptsRequest(t *testing.T) {
	original := QueryContractReceiptsRequest{
		ContractAddress: "contract-address-receipts-req",
		Pagination:      fixturePageRequestForTest(34),
	}
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded QueryContractReceiptsRequest
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_ContractReceipt(t *testing.T) {
	original := fixtureContractReceiptForTest("standalone")
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded ContractReceipt
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryMarshalRoundTrip_QueryContractReceiptsResponse(t *testing.T) {
	original := QueryContractReceiptsResponse{
		Receipts: []ContractReceipt{
			fixtureContractReceiptForTest("one"),
			fixtureContractReceiptForTest("two"),
		},
	}
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded QueryContractReceiptsResponse
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

// TestQueryMarshalSizeMatchesMarshaledLength cross-checks Size() against the
// actual Marshal() output length for a representative sample of the
// closure -- Size() is used to pre-allocate the MarshalToSizedBuffer
// destination buffer, so any mismatch would corrupt every call, not just
// show up as a cosmetic inefficiency.
func TestQueryMarshalSizeMatchesMarshaledLength(t *testing.T) {
	contract := fixtureContractForTest("size-check")
	data, err := contract.Marshal()
	require.NoError(t, err)
	require.Equal(t, contract.Size(), len(data))

	stateInit := fixtureStateInitForTest("size-check")
	data, err = stateInit.Marshal()
	require.NoError(t, err)
	require.Equal(t, stateInit.Size(), len(data))

	receiptsResp := QueryContractReceiptsResponse{Receipts: []ContractReceipt{
		fixtureContractReceiptForTest("a"), fixtureContractReceiptForTest("b"),
	}}
	data, err = receiptsResp.Marshal()
	require.NoError(t, err)
	require.Equal(t, receiptsResp.Size(), len(data))
}
