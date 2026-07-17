package types

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/sovereign-l1/l1/app/addressing"
)

// MsgUpdateRoutingTable is a hand-rolled gogo type. Like x/nominator-pool's, its
// descriptor carries no cosmos.msg.v1.signer option, so the x/tx signing context
// cannot resolve a signer for it on its own ("no cosmos.msg.v1.signer option
// found") and no such transaction could ever be broadcast. This resolver is
// registered through signing.Options.CustomGetSigners in app/keeperconfig/tx.go
// and reads the signer straight out of the message's own authority field.
//
// The message arrives here as a protoreflect-hybrid wrapper -- the concrete gogo
// struct does not implement ProtoReflect() -- so the field is read BY DESCRIPTOR
// NAME rather than by struct-field access. That is precisely why tx.go's
// descriptor declares real fields instead of following x/config's field-less
// builder: with no declared "authority" field there is nothing here to read.
//
// The authority is a plain account address (the gov module account on a real
// network), so the resolved signer is exactly what the standard AddressCodec
// resolver would produce and is guaranteed to match what SigVerificationDecorator
// independently derives from the tx's AuthInfo pubkey. That equality is what
// makes the field cryptographically load-bearing rather than advisory: a tx
// whose authority is anything but the signing key's own address cannot be
// broadcast at all.
func MsgUpdateRoutingTableSigners(msg proto.Message) ([][]byte, error) {
	reflectMsg := msg.ProtoReflect()
	field := reflectMsg.Descriptor().Fields().ByName(protoreflect.Name("authority"))
	if field == nil {
		return nil, fmt.Errorf("%s descriptor missing %q signer field", reflectMsg.Descriptor().FullName(), "authority")
	}
	addr := reflectMsg.Get(field).String()
	bz, err := addressing.Parse(addr)
	if err != nil {
		return nil, fmt.Errorf("resolve %s signer from %q=%q: %w", reflectMsg.Descriptor().FullName(), "authority", addr, err)
	}
	return [][]byte{bz}, nil
}
