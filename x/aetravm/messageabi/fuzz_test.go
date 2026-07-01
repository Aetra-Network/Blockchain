package messageabi

import (
	"bytes"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sovereign-l1/l1/app/addressing"
)

func FuzzDecodeRejectsMalformedWithoutPanic(f *testing.F) {
	valid := Message{
		Kind:          KindInternal,
		Opcode:        0x1020_3040_5060_7080,
		QueryID:       42,
		Sender:        mustPair(bytes.Repeat([]byte{0x11}, 20)),
		Destination:   mustPair(bytes.Repeat([]byte{0x22}, 20)),
		ValueNAET:     300_000_000,
		Bounce:        true,
		DeadlineBlock: 100,
		GasLimit:      1_000_000,
		Body:          []byte{1, 2, 3},
		StateInit:     []byte("state-init-canonical"),
		Metadata:      []byte("debug-metadata"),
		Signature:     bytes.Repeat([]byte{0x33}, 64),
	}
	encoded, err := Encode(valid, DefaultParams())
	if err == nil {
		f.Add(encoded)
	}
	f.Add([]byte("bad"))
	f.Add([]byte{0x00, 0x01, 0x02, 0x03})

	f.Fuzz(func(t *testing.T, bz []byte) {
		msg, err := Decode(bz, DefaultParams(), 100)
		if err != nil {
			return
		}
		if _, err := CanonicalBytes(msg, DefaultParams()); err != nil {
			t.Fatalf("canonical encode failed: %v", err)
		}
		if _, err := DebugJSON(msg, DefaultParams()); err != nil {
			t.Fatalf("debug json failed: %v", err)
		}
	})
}

func mustPair(raw []byte) AddressPair {
	user, err := addressing.FormatUserFriendly(raw)
	if err != nil {
		panic(err)
	}
	return AddressPair{User: user, Raw: addressing.Format(sdk.AccAddress(raw))}
}
