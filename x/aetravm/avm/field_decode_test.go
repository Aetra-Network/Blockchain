package avm

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

// field encodes a {name,type,value} entry the way the compiler's Codec does
// (Type: field.Type.String(), the canonical LONG-FORM name), building a
// message-body JSON blob runtimeMessageFieldValue can decode.
func fieldBody(t *testing.T, name, typ string, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	body, err := json.Marshal([]map[string]json.RawMessage{
		{"name": mustJSON(t, name), "type": mustJSON(t, typ), "value": raw},
	})
	require.NoError(t, err)
	return body
}

func mustJSON(t *testing.T, v string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}

// TestRuntimeMessageFieldValueCanonicalLongFormTypes pins the fix for a
// wire-format mismatch: the compiler's Codec emits the CANONICAL long-form
// type name (TypeRef.String(), e.g. "uint64", "int256") into every encoded
// {name,type,value} field, but runtimeValueFromJSONField's switch only
// matched the short form ("u64"). Anything else silently fell through to a
// generic-numeric-or-raw-bytes fallback — correct by accident for small
// unsigned values, wrong for negative signed values and silently truncating
// for 128/256-bit values. This is the exact path both ordinary message
// fields (msg.by) and the new multi-argument get-method calling convention
// depend on.
func TestRuntimeMessageFieldValueCanonicalLongFormTypes(t *testing.T) {
	cases := []struct {
		name    string
		typ     string
		value   any
		wantTag ValueTag
		wantStr string
	}{
		{"long uint64", "uint64", 9, TagUint64, "9"},
		{"short u64 (legacy)", "u64", 9, TagUint64, "9"},
		{"long int64 negative", "int64", -42, TagInt64, "-42"},
		{"short i64 negative (legacy)", "i64", -42, TagInt64, "-42"},
		{"long uint8", "uint8", 200, TagUint8, "200"},
		{"long int32", "int32", -70000, TagInt32, "-70000"},
		{"uint128 large value as string", "uint128", "340282366920938463463374607431768211455", TagUint128, "340282366920938463463374607431768211455"},
		{"int256 large negative as string", "int256", "-1000000000000000000000000000000", TagInt256, "-1000000000000000000000000000000"},
		{"address", "address", "AEfakeaddress", TagAddress, ""},
		{"string", "string", "hello", TagString, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := fieldBody(t, "arg0", tc.typ, tc.value)
			v, err := runtimeMessageFieldValue(body, "arg0")
			require.NoError(t, err)
			require.Equal(t, tc.wantTag, v.Tag)
			if tc.wantStr != "" {
				n, err := v.AsBigInt()
				require.NoError(t, err)
				want, ok := new(big.Int).SetString(tc.wantStr, 10)
				require.True(t, ok)
				require.Equal(t, 0, n.Cmp(want), "got %s want %s", n, want)
			}
		})
	}
}

// TestRuntimeMessageFieldValueRejectsOutOfRange guards the range checks that
// now apply uniformly to every width/signedness combination.
func TestRuntimeMessageFieldValueRejectsOutOfRange(t *testing.T) {
	// uint8 max is 255.
	body := fieldBody(t, "arg0", "uint8", 256)
	_, err := runtimeMessageFieldValue(body, "arg0")
	require.Error(t, err)

	// uint64 rejects a negative value.
	body = fieldBody(t, "arg0", "uint64", -1)
	_, err = runtimeMessageFieldValue(body, "arg0")
	require.Error(t, err)

	// int8 range is [-128, 127].
	body = fieldBody(t, "arg0", "int8", 128)
	_, err = runtimeMessageFieldValue(body, "arg0")
	require.Error(t, err)
}

// TestMultiArgGetterFieldNames pins the positional wire convention the
// compiler now uses for getter/entrypoint scalar parameters: each parameter
// binds to a message-body field named "arg<index>" (see IRExprMsgField in
// compile.go), so a getter can accept more than the previous one-argument
// limit — every argument is read with its own declared type.
func TestMultiArgGetterFieldNames(t *testing.T) {
	body, err := json.Marshal([]map[string]any{
		{"name": "arg0", "type": "uint64", "value": 9},
		{"name": "arg1", "type": "address", "value": "AEsecond"},
		{"name": "arg2", "type": "bool", "value": true},
	})
	require.NoError(t, err)

	v0, err := runtimeMessageFieldValue(body, "arg0")
	require.NoError(t, err)
	require.Equal(t, TagUint64, v0.Tag)
	n, _ := v0.AsBigInt()
	require.Equal(t, "9", n.String())

	v1, err := runtimeMessageFieldValue(body, "arg1")
	require.NoError(t, err)
	require.Equal(t, TagAddress, v1.Tag)
	a, _ := v1.AsAddress()
	require.Equal(t, "AEsecond", a)

	v2, err := runtimeMessageFieldValue(body, "arg2")
	require.NoError(t, err)
	require.Equal(t, TagBool, v2.Tag)
	b, _ := v2.AsBool()
	require.True(t, b)
}
