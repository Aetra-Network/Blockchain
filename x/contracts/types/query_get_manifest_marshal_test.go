package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Round-trip tests for query_get_manifest_marshal.go's hand-written wire
// format, matching query_marshal_test.go's own convention: a passing
// Marshal-then-Unmarshal round trip is the actual evidence the wire format
// is correct, not just that the code compiles. Guards the exact defect
// class app/identity_root_msg_wire_format_test.go documents elsewhere in
// this codebase -- a type with Reset()/String()/ProtoMessage() but no real
// Marshal/Unmarshal compiles fine and satisfies proto.Message, but panics
// or silently mis-decodes the moment a real gRPC call tries to use it.

func TestQueryGetManifestMarshalRoundTrip_QueryContractManifestRequest(t *testing.T) {
	original := QueryContractManifestRequest{CodeID: "manifest-code-id"}
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded QueryContractManifestRequest
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryGetManifestMarshalRoundTrip_QueryContractManifestRequest_Empty(t *testing.T) {
	original := QueryContractManifestRequest{}
	data, err := original.Marshal()
	require.NoError(t, err)
	require.Empty(t, data)
	var decoded QueryContractManifestRequest
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryGetManifestMarshalRoundTrip_QueryContractManifestResponse_Found(t *testing.T) {
	original := QueryContractManifestResponse{
		Found:         true,
		ManifestBytes: []byte(`{"version":1,"getters":["price"]}`),
	}
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded QueryContractManifestResponse
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

// NotFound is the zero-value shape (Found=false, no ManifestBytes) -- both
// fields omit from the wire, so this also proves Unmarshal correctly leaves
// a fresh QueryContractManifestResponse at its zero value rather than, say,
// mistaking an empty buffer for a decode error.
func TestQueryGetManifestMarshalRoundTrip_QueryContractManifestResponse_NotFound(t *testing.T) {
	original := QueryContractManifestResponse{Found: false}
	data, err := original.Marshal()
	require.NoError(t, err)
	require.Empty(t, data)
	var decoded QueryContractManifestResponse
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

// A contract stored without a manifest (Found=true, but ManifestBytes
// empty) is a real, distinct case from NotFound -- see
// QueryContractManifestResponse's own doc comment in types.go.
func TestQueryGetManifestMarshalRoundTrip_QueryContractManifestResponse_FoundNoManifest(t *testing.T) {
	original := QueryContractManifestResponse{Found: true}
	data, err := original.Marshal()
	require.NoError(t, err)
	var decoded QueryContractManifestResponse
	require.NoError(t, decoded.Unmarshal(data))
	require.Equal(t, original, decoded)
}

func TestQueryGetManifestMarshalSize_MatchesActualMarshaledLength(t *testing.T) {
	original := QueryContractManifestResponse{
		Found:         true,
		ManifestBytes: []byte("a manifest payload of some real length"),
	}
	data, err := original.Marshal()
	require.NoError(t, err)
	require.Equal(t, original.Size(), len(data))
}
