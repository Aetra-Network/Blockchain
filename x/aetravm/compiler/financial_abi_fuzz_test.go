package compiler

import "testing"

// Stage 7(d): ABI round-trip fuzz/property coverage for the Ratio256
// Stage-3 financial struct type (financialCodec/ratio256Fixture are defined
// in financial_abi_codec_test.go, Stage 4's example-based coverage for the
// same six types). The property under test: for ANY (num, den) pair the
// fuzzer supplies, encode -> decode -> re-encode must (a) never panic and
// (b) be byte-identical, and the decoded value must equal the original.

func FuzzRatio256ABIRoundTrip(f *testing.F) {
	f.Add(uint64(3), uint64(7))
	f.Add(uint64(0), uint64(0))
	f.Add(uint64(0), uint64(1))
	f.Add(uint64(1), uint64(0))
	f.Add(^uint64(0), ^uint64(0)) // max uint64 in both fields
	f.Add(^uint64(0), uint64(1))
	f.Add(uint64(1), ^uint64(0))

	f.Fuzz(func(t *testing.T, num, den uint64) {
		codec := financialCodec("Ratio256")
		type fixture struct {
			Value ratio256Fixture
		}
		original := fixture{Value: ratio256Fixture{Num: num, Den: den}}

		var encoded []byte
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Encode(%+v) panicked: %v", original, r)
				}
			}()
			encoded, err = codec.Encode(original)
		}()
		if err != nil {
			t.Fatalf("Encode(%+v) failed: %v", original, err)
		}

		var decoded fixture
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Decode(%q) panicked: %v", encoded, r)
				}
			}()
			err = codec.Decode(encoded, &decoded)
		}()
		if err != nil {
			t.Fatalf("Decode(%q) failed: %v", encoded, err)
		}
		if decoded != original {
			t.Fatalf("round trip mismatch: original %+v, decoded %+v", original, decoded)
		}

		var reencoded []byte
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("re-Encode(%+v) panicked: %v", decoded, r)
				}
			}()
			reencoded, err = codec.Encode(decoded)
		}()
		if err != nil {
			t.Fatalf("re-Encode(%+v) failed: %v", decoded, err)
		}
		if string(encoded) != string(reencoded) {
			t.Fatalf("encode->decode->re-encode not byte-identical: %q vs %q", encoded, reencoded)
		}
	})
}

// FuzzRatio256ABIDecodeNeverPanics feeds arbitrary bytes directly into
// Decode -- not just well-formed encodings of valid fixtures -- to prove the
// malformed-payload path (missing field / extra field / non-object value /
// garbage JSON) always returns an error rather than panicking, matching the
// financial_abi_codec_test.go deterministic-rejection tests but at fuzz
// scale.
func FuzzRatio256ABIDecodeNeverPanics(f *testing.F) {
	f.Add([]byte(`[{"name":"value","type":"Ratio256","value":{"num":3,"den":7}}]`))
	f.Add([]byte(`[{"name":"value","type":"Ratio256","value":{"num":3}}]`))
	f.Add([]byte(`[{"name":"value","type":"Ratio256","value":{"num":3,"den":7,"extra":1}}]`))
	f.Add([]byte(`[{"name":"value","type":"Ratio256","value":42}]`))
	f.Add([]byte(`not json`))
	f.Add([]byte(``))
	f.Add([]byte(`[]`))
	f.Add([]byte(`null`))

	f.Fuzz(func(t *testing.T, payload []byte) {
		codec := financialCodec("Ratio256")
		type fixture struct {
			Value ratio256Fixture
		}
		var out fixture
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Decode(%q) panicked: %v", payload, r)
			}
		}()
		_ = codec.Decode(payload, &out)
	})
}
