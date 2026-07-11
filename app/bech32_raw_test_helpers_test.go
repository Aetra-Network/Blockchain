package app

import (
	"encoding/hex"
	"strings"

	"github.com/sovereign-l1/l1/app/addressing"
)

// legacyByteRawAddress builds the bech32 (ae1…) raw form of the 20-byte account
// whose bytes are hexByte repeated 20 times. It replaces the old
// "4:0000…0000<hexByte×20>" legacy-padded raw literals these system tests used
// before the raw address form migrated to bech32.
func legacyByteRawAddress(hexByte string) string {
	bz, err := hex.DecodeString(strings.Repeat(hexByte, 20))
	if err != nil {
		panic(err)
	}
	return addressing.Format(bz)
}
