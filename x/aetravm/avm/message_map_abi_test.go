package avm

import (
	"encoding/json"
	"testing"
)

// TestSplitMapKindParsesDeclaredMapTypes covers the type-string parser that
// gates map decoding, including a nested map value (whose inner comma must
// not be mistaken for the outer separator).
func TestSplitMapKindParsesDeclaredMapTypes(t *testing.T) {
	cases := []struct {
		kind      string
		key       string
		value     string
		recognize bool
	}{
		{kind: "map<uint64,address>", key: "uint64", value: "address", recognize: true},
		{kind: "map<uint64, string>", key: "uint64", value: "string", recognize: true},
		{kind: "map<address,map<uint64,uint256>>", key: "address", value: "map<uint64,uint256>", recognize: true},
		{kind: "uint64", recognize: false},
		{kind: "map<uint64>", recognize: false},
		{kind: "map<,address>", recognize: false},
		{kind: "map<uint64,>", recognize: false},
	}
	for _, tc := range cases {
		key, value, ok := splitMapKind(tc.kind)
		if ok != tc.recognize {
			t.Fatalf("splitMapKind(%q) recognized = %v, want %v", tc.kind, ok, tc.recognize)
		}
		if !tc.recognize {
			continue
		}
		if key != tc.key || value != tc.value {
			t.Fatalf("splitMapKind(%q) = (%q, %q), want (%q, %q)", tc.kind, key, value, tc.key, tc.value)
		}
	}
}

// TestMessageBodyMapFieldDecodesTyped is the regression test for map-typed
// message-body fields. Before this support existed, runtimeValueFromJSONField
// had no map case, so a declared Map<K,V> body field fell through to the
// default branch and arrived in the contract as raw bytes — every map builtin
// on it then trapped with "expected map, got bytes", which is what made a
// dictionary-carrying message (e.g. the NFT family's batch mint) impossible.
func TestMessageBodyMapFieldDecodesTyped(t *testing.T) {
	raw := json.RawMessage(`{"7":"ae1qqqq","3":"ae1zzzz"}`)
	value, err := runtimeValueFromJSONField("map<uint64,address>", raw)
	if err != nil {
		t.Fatalf("decode map field: %v", err)
	}
	if value.Tag != TagMap {
		t.Fatalf("decoded tag = %v, want TagMap", value.Tag)
	}
	entries, err := value.AsMap()
	if err != nil {
		t.Fatalf("AsMap: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entry count = %d, want 2", len(entries))
	}

	// Entries must come back sorted by canonical key, independent of the
	// JSON object's textual order and of Go's randomized map iteration —
	// otherwise two validators could decode the same body into two different
	// values.
	first, err := entries[0].Key.AsUint64()
	if err != nil {
		t.Fatalf("first key: %v", err)
	}
	second, err := entries[1].Key.AsUint64()
	if err != nil {
		t.Fatalf("second key: %v", err)
	}
	if first != 3 || second != 7 {
		t.Fatalf("keys = (%d, %d), want (3, 7) in canonical order", first, second)
	}

	// Lookup by key must find the value the wire named for it.
	found, ok, err := runtimeMapLookup(entries, ValueUint64(7))
	if err != nil || !ok {
		t.Fatalf("lookup key 7: ok=%v err=%v", ok, err)
	}
	addr, err := found.AsAddress()
	if err != nil {
		t.Fatalf("value as address: %v", err)
	}
	if addr != "ae1qqqq" {
		t.Fatalf("value for key 7 = %q, want %q", addr, "ae1qqqq")
	}
}

// TestMessageBodyMapFieldRejectsAmbiguousAndOversizedInput covers the two
// ways a map body field can be malformed in a consensus-relevant way.
func TestMessageBodyMapFieldRejectsAmbiguousAndOversizedInput(t *testing.T) {
	// "1" and "01" are distinct JSON keys that canonicalize to the same
	// uint64 map key. Accepting both would make the decoded map depend on
	// which one Go's randomized iteration happened to write last.
	if _, err := runtimeValueFromJSONField("map<uint64,address>", json.RawMessage(`{"1":"ae1a","01":"ae1b"}`)); err == nil {
		t.Fatal("duplicate canonical key must be rejected")
	}

	// A map body field may not exceed the runtime's map entry ceiling.
	oversized := make(map[string]string, MaxTupleElements+1)
	for i := uint32(0); i < MaxTupleElements+1; i++ {
		oversized[itoa(i)] = "ae1a"
	}
	encoded, err := json.Marshal(oversized)
	if err != nil {
		t.Fatalf("marshal oversized map: %v", err)
	}
	if _, err := runtimeValueFromJSONField("map<uint64,address>", encoded); err == nil {
		t.Fatalf("a map above %d entries must be rejected", MaxTupleElements)
	}

	// A non-object payload for a map-typed field is a decode error, not a
	// silent fallback to bytes.
	if _, err := runtimeValueFromJSONField("map<uint64,address>", json.RawMessage(`"not-a-map"`)); err == nil {
		t.Fatal("a non-object payload for a map field must be rejected")
	}
}

func itoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
