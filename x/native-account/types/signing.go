package types

import (
	"fmt"

	"google.golang.org/protobuf/proto"
)

// MsgActivateAccountSigners is a signing.GetSignersFunc for MsgActivateAccount
// (registered via signing.Options.CustomGetSigners in app/keeperconfig/tx.go).
//
// It deliberately does NOT use the tx_proto.go descriptor's declared
// "address_user" signer field. AddressUser is required by ValidateBasic to
// equal addressing.DeriveAccountAddress(pubKey)'s output -- a
// domain-separated "v2" hash of the plain address (see
// app/addressing/raw_policy.go's NormalizeV2RawAddress), NOT the address a
// signature over this pubkey naturally verifies against
// (pubKey.Address(), what SigVerificationDecorator independently derives
// from AuthInfo). No pubkey's signature can ever match its own v2-derived
// hash, so resolving the signer from AddressUser makes this message
// permanently unsignable by anyone. The message already carries the pubkey
// being activated (PublicKeyHex/PublicKeyType) -- deriving the required
// signer from THAT (the tautologically correct rule: whoever holds this
// pubkey's private key may activate it) is what actually works, and leaves
// the v2 address-derivation policy itself untouched for whatever its
// original purpose is elsewhere (validator/consensus roles).
//
// msg arrives here as a protoreflect-hybrid wrapper, not the concrete
// *MsgActivateAccount Go struct (it does not implement ProtoReflect() --
// confirmed at compile time), so fields are read by descriptor name rather
// than direct struct-field access.
func MsgActivateAccountSigners(msg proto.Message) ([][]byte, error) {
	reflectMsg := msg.ProtoReflect()
	fields := reflectMsg.Descriptor().Fields()
	pubKeyTypeField := fields.ByName("public_key_type")
	pubKeyHexField := fields.ByName("public_key_hex")
	if pubKeyTypeField == nil || pubKeyHexField == nil {
		return nil, fmt.Errorf("MsgActivateAccount descriptor missing public_key_type/public_key_hex fields")
	}

	m := MsgActivateAccount{
		PublicKeyType: reflectMsg.Get(pubKeyTypeField).String(),
		PublicKeyHex:  reflectMsg.Get(pubKeyHexField).String(),
	}
	pubKey, err := m.EffectivePublicKey()
	if err != nil {
		return nil, err
	}
	return [][]byte{pubKey.Address().Bytes()}, nil
}
