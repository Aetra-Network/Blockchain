package types

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/sovereign-l1/l1/app/addressing"
)

// The x/nominator-pool user-facing messages (deposit / unbond / claim) and the
// authority-gated official-pool creation are hand-rolled gogo types whose
// tx.proto descriptor historically declared NEITHER fields NOR a
// cosmos.msg.v1.signer option -- so the x/tx signing context could not resolve a
// signer for them at all ("no cosmos.msg.v1.signer option found") and no such tx
// could ever be broadcast. These signing.GetSignersFunc's (registered via
// signing.Options.CustomGetSigners in app/keeperconfig/tx.go, mirroring
// native-account's MsgActivateAccount fix) resolve the required signer directly
// from the message's own address field.
//
// Unlike MsgActivateAccount -- whose declared signer field carries a
// domain-separated "v2" identity that no signature can ever verify against,
// forcing a pubkey-derived resolver -- these messages carry the signer's PLAIN
// account address (the very same "AE..." address a bank MsgSend uses as its
// from_address). So the signer is simply that address parsed to bytes: exactly
// what the standard AddressCodec-based resolver produces for a MsgSend, and
// therefore guaranteed to match what SigVerificationDecorator independently
// derives from the tx's own AuthInfo pubkey. The keeper separately normalizes
// this plain address to the account's v2 identity for its activation check and
// share bookkeeping (see keeper/msg_server.go's normalizeAccountIdentity), so
// signing stays standard while the v2 derivation lives server-side.
//
// The message arrives here as a protoreflect-hybrid wrapper (the concrete gogo
// struct does not implement ProtoReflect()), so the address field is read by
// descriptor name rather than by struct-field access -- which is why tx.go's
// descriptor for each of these messages now declares its fields.
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

// MsgDepositToStakingPoolSigners resolves the signer to the depositor's plain
// wallet address.
func MsgDepositToStakingPoolSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "wallet_address")
}

// MsgRequestPoolUnbondSigners resolves the signer to the unbonding owner's plain
// wallet address.
func MsgRequestPoolUnbondSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "owner_address")
}

// MsgClaimPoolRewardsSigners resolves the signer to the reward owner's plain
// wallet address. (owner_address is field 4 -- the keeper reads this, not the
// authority/delegator fields, for a user claim.)
func MsgClaimPoolRewardsSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "owner_address")
}

// MsgCreateOfficialLiquidStakingPoolSigners resolves the signer to the pool's
// governance authority address. On mainnet that is the gov module account
// (the message then only executes inside a passed proposal); on a localnet /
// testnet it is whatever operator key the network's nominator-pool
// Params.Authority is set to, so chain-ops can register the official pool with
// a normal signed tx.
func MsgCreateOfficialLiquidStakingPoolSigners(msg proto.Message) ([][]byte, error) {
	return msgSignerFromAddressField(msg, "authority")
}
