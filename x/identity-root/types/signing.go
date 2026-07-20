package types

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/sovereign-l1/l1/app/addressing"
)

// The x/identity-root Phase-A messages are hand-rolled gogo types whose tx.proto
// descriptor carries no cosmos.msg.v1.signer option, so the x/tx signing context
// cannot resolve a signer on its own ("no cosmos.msg.v1.signer option found").
// These GetSignersFunc's (registered via signing.Options.CustomGetSigners in
// app/keeperconfig/tx.go, mirroring x/aez and x/nominator-pool) resolve the
// signer directly from the message's own address field.
//
// Each signer field carries a PLAIN account address (the same "AE..." address a
// bank MsgSend uses), so the resolved signer is exactly what the standard
// AddressCodec resolver produces and is guaranteed to match what
// SigVerificationDecorator derives from the tx's AuthInfo pubkey -- which makes
// the field cryptographically load-bearing. The message arrives as a
// protoreflect-hybrid wrapper (the concrete gogo struct has no ProtoReflect), so
// the field is read BY DESCRIPTOR NAME, which is why tx.go's descriptor declares
// real fields.
func msgSignerFromAddressField(msg proto.Message, fieldName string) ([][]byte, error) {
	reflectMsg := msg.ProtoReflect()
	field := reflectMsg.Descriptor().Fields().ByName(protoreflect.Name(fieldName))
	if field == nil {
		return nil, fmt.Errorf("%s descriptor missing %q signer field", reflectMsg.Descriptor().FullName(), fieldName)
	}
	addr := reflectMsg.Get(field).String()
	bz, err := addressing.Parse(addr)
	if err != nil {
		return nil, fmt.Errorf("resolve %s signer from %q=%q: %w", reflectMsg.Descriptor().FullName(), fieldName, addr, err)
	}
	return [][]byte{bz}, nil
}

// MsgSendToNameCollectionSigners resolves the signer to the sender.
func MsgSendToNameCollectionSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "sender")
}

// MsgPlaceBidSigners resolves the signer to the bidder.
func MsgPlaceBidSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "bidder")
}

// MsgStartAuctionSigners resolves the signer to the listing owner.
func MsgStartAuctionSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "owner")
}

// MsgUpdatePriceTableSigners resolves the signer to the governance authority.
func MsgUpdatePriceTableSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "authority")
}

// MsgListForSaleSigners resolves the signer to the listing owner (ANS Phase B
// owner fixed-price sale). Must be registered alongside the other three
// hand-rolled Phase-B/C entries in app/keeperconfig/tx.go's CustomGetSigners map
// -- that file is outside this session's edit scope, so this resolver exists
// ready to be wired but is NOT yet reachable through the real signing context
// until that registration lands. See the final report for this gap.
func MsgListForSaleSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "owner")
}

// MsgDelistNameSigners resolves the signer to the listing owner. Same
// app/keeperconfig/tx.go wiring caveat as MsgListForSaleSigners above.
func MsgDelistNameSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "owner")
}

// MsgBuyListedNameSigners resolves the signer to the buyer. Same
// app/keeperconfig/tx.go wiring caveat as MsgListForSaleSigners above.
func MsgBuyListedNameSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "buyer")
}

// MsgAttachDomainSigners resolves the signer to the FQDN owner.
func MsgAttachDomainSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "owner")
}

// MsgDetachDomainSigners resolves the signer to the FQDN owner.
func MsgDetachDomainSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "owner")
}

// MsgDisownAttachmentSigners resolves the signer to the attachment's TARGET
// wallet: the target authorizes clearing the attachment that points at its own
// account (ANS Phase B anti-griefing self-detach). The target field carries a
// plain AE address, exactly like the owner fields above.
func MsgDisownAttachmentSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "target")
}

// MsgCreateSubdomainSigners resolves the signer to the parent-domain owner.
func MsgCreateSubdomainSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "owner")
}

// MsgRenewNameSigners resolves the signer to the domain owner (ANS Phase C).
func MsgRenewNameSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "owner")
}

// MsgTransferNameSigners resolves the signer to the CURRENT owner (the party
// giving the domain away), not the new owner -- matches keeper.TransferName's
// requireOwnedName(msg.Owner, ...) check (ANS Phase C).
func MsgTransferNameSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "owner")
}

// MsgSetResolverSigners resolves the signer to the domain owner (ANS Phase C).
func MsgSetResolverSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "owner")
}

// MsgSetReverseRecordSigners resolves the signer to the domain owner (ANS Phase
// C).
func MsgSetReverseRecordSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "owner")
}

// MsgReserveNameSigners resolves the signer to the governance authority.
// ReserveName is a governance-gated action -- keeper.ReserveName's
// requireAuthority(msg.Authority) call is the actual safety check; this signer
// resolver only ensures the tx is signed by whatever address the message
// claims as authority, exactly like MsgUpdatePriceTableSigners (ANS Phase C).
func MsgReserveNameSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "authority")
}

// MsgReleaseReservedNameSigners resolves the signer to the governance
// authority, mirroring MsgReserveNameSigners (ANS Phase C).
func MsgReleaseReservedNameSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "authority")
}
