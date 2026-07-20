package keeperconfig

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sigtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	txmodule "github.com/cosmos/cosmos-sdk/x/auth/tx/config"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	"github.com/cosmos/cosmos-sdk/x/tx/signing"
	"google.golang.org/protobuf/reflect/protoreflect"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	aeztypes "github.com/sovereign-l1/l1/x/aez/types"
	identityroottypes "github.com/sovereign-l1/l1/x/identity-root/types"
	nativeaccounttypes "github.com/sovereign-l1/l1/x/native-account/types"
	nominatorpooltypes "github.com/sovereign-l1/l1/x/nominator-pool/types"
)

// CustomGetSigners holds signer-resolution overrides for hand-rolled message
// types whose declared cosmos.msg.v1.signer field can't be verified against a
// normal signature -- see native-account/types/signing.go's doc comment for
// MsgActivateAccount's case. Shared by both TxConfig constructions in this
// binary (this file's, used for app.TxConfig()/CLI/query tooling, and
// app.go's early-bootstrap one, which baseapp actually uses to decode every
// live transaction) so they can't silently drift apart.
func CustomGetSigners() map[protoreflect.FullName]signing.GetSignersFunc {
	return map[protoreflect.FullName]signing.GetSignersFunc{
		"l1.nativeaccount.v1.MsgActivateAccount": nativeaccounttypes.MsgActivateAccountSigners,
		// x/nominator-pool's hand-rolled tx types carried no signer option and no
		// fields, so the signing context could not resolve a signer for any of
		// them ("no cosmos.msg.v1.signer option found") -- see
		// nominator-pool/types/signing.go. The three user-facing messages resolve
		// to the caller's plain wallet address; the official-pool creation
		// resolves to the governance authority address.
		"l1.nominatorpool.v1.MsgDepositToStakingPool":         nominatorpooltypes.MsgDepositToStakingPoolSigners,
		"l1.nominatorpool.v1.MsgRequestPoolUnbond":            nominatorpooltypes.MsgRequestPoolUnbondSigners,
		"l1.nominatorpool.v1.MsgClaimPoolRewards":             nominatorpooltypes.MsgClaimPoolRewardsSigners,
		"l1.nominatorpool.v1.MsgCreateOfficialLiquidStakingPool": nominatorpooltypes.MsgCreateOfficialLiquidStakingPoolSigners,
		// #2/SA2-N01: the three plain-pool messages real bank+staking custody
		// depends on had the identical missing-descriptor bug as the four
		// above (live-verified: broadcasting one crashed gogoproto's
		// Unmarshal on every receiving node) -- see the struct doc comments
		// on these types in x/nominator-pool/types/state.go.
		"l1.nominatorpool.v1.MsgCreateNominatorPool":    nominatorpooltypes.MsgCreateNominatorPoolSigners,
		"l1.nominatorpool.v1.MsgDepositToPool":          nominatorpooltypes.MsgDepositToPoolSigners,
		"l1.nominatorpool.v1.MsgRequestPoolWithdrawal":  nominatorpooltypes.MsgRequestPoolWithdrawalSigners,
		// AEZ Phase 2. x/aez's hand-rolled MsgUpdateRoutingTable has the
		// same shape as the entries above -- no cosmos.msg.v1.signer option
		// -- so it needs the same explicit resolver. Unlike them it declared
		// its descriptor fields from the start rather than after a live
		// crash; see x/aez/types/tx.go. The signer is the governance
		// authority (the gov module account on a real network).
		"l1.aez.v1.MsgUpdateRoutingTable": aeztypes.MsgUpdateRoutingTableSigners,
		// ANS Phase A. x/identity-root's hand-rolled collection messages carry no
		// cosmos.msg.v1.signer option (same shape as the entries above), so each
		// needs an explicit resolver. The first three resolve to the caller's
		// plain wallet address; the price-table update resolves to the governance
		// authority. See x/identity-root/types/signing.go.
		"l1.identityroot.v1.MsgSendToNameCollection":	identityroottypes.MsgSendToNameCollectionSigners,
		"l1.identityroot.v1.MsgPlaceBid":		identityroottypes.MsgPlaceBidSigners,
		"l1.identityroot.v1.MsgStartAuction":		identityroottypes.MsgStartAuctionSigners,
		"l1.identityroot.v1.MsgUpdatePriceTable":	identityroottypes.MsgUpdatePriceTableSigners,
		// ANS Phase B. The attach/detach/subdomain messages are the same
		// hand-rolled shape (no cosmos.msg.v1.signer option); each resolves to
		// the caller's plain wallet address in its "owner" field.
		"l1.identityroot.v1.MsgAttachDomain":		identityroottypes.MsgAttachDomainSigners,
		"l1.identityroot.v1.MsgDetachDomain":		identityroottypes.MsgDetachDomainSigners,
		// Anti-griefing self-detach: the signer is the attachment's TARGET, not
		// the FQDN owner -- the target authorizes clearing an attachment aimed at
		// its own wallet. See x/identity-root/types/signing.go.
		"l1.identityroot.v1.MsgDisownAttachment":	identityroottypes.MsgDisownAttachmentSigners,
		"l1.identityroot.v1.MsgCreateSubdomain":	identityroottypes.MsgCreateSubdomainSigners,
		// ANS Phase C. Renew/transfer/resolver/reverse-record are ordinary
		// owned-domain operations, each resolving to the caller's plain "owner"
		// field like the Phase B messages above. Reserve/release are
		// governance-gated (the keeper's requireAuthority check enforces this,
		// not the signer resolver) and resolve to "authority", like
		// MsgUpdatePriceTable. RegisterName is DELIBERATELY not wired here: it
		// has no fee/payment check of its own and would let any caller register
		// an unreserved name for free, bypassing the .aet collection's auction
		// pricing entirely -- domains are meant to be acquired only through
		// MsgSendToNameCollection's auction flow. See
		// x/identity-root/keeper/keeper.go's RegisterName doc.
		"l1.identityroot.v1.MsgRenewName":		identityroottypes.MsgRenewNameSigners,
		"l1.identityroot.v1.MsgTransferName":		identityroottypes.MsgTransferNameSigners,
		"l1.identityroot.v1.MsgSetResolver":		identityroottypes.MsgSetResolverSigners,
		"l1.identityroot.v1.MsgSetReverseRecord":	identityroottypes.MsgSetReverseRecordSigners,
		"l1.identityroot.v1.MsgReserveName":		identityroottypes.MsgReserveNameSigners,
		"l1.identityroot.v1.MsgReleaseReservedName":	identityroottypes.MsgReleaseReservedNameSigners,
		// ANS owner fixed-price sale. List/delist resolve to the listing
		// owner; buy resolves to the buyer. Same hand-rolled shape as the
		// Phase A/B/C messages above. See x/identity-root/types/signing.go.
		"l1.identityroot.v1.MsgListForSale":		identityroottypes.MsgListForSaleSigners,
		"l1.identityroot.v1.MsgDelistName":		identityroottypes.MsgDelistNameSigners,
		"l1.identityroot.v1.MsgBuyListedName":		identityroottypes.MsgBuyListedNameSigners,
	}
}

func NewTxConfig(appCodec codec.Codec, bankKeeper bankkeeper.BaseKeeper) client.TxConfig {
	enabledSignModes := append(authtx.DefaultSignModes, sigtypes.SignMode_SIGN_MODE_TEXTUAL)
	txConfig, err := authtx.NewTxConfigWithOptions(
		appCodec,
		authtx.ConfigOptions{
			EnabledSignModes:	enabledSignModes,
			SigningOptions: &signing.Options{
				AddressCodec:		aetraaddress.Codec{},
				ValidatorAddressCodec:	aetraaddress.Codec{},
				CustomGetSigners:	CustomGetSigners(),
			},
			TextualCoinMetadataQueryFn:	txmodule.NewBankKeeperCoinMetadataQueryFn(bankKeeper),
		},
	)
	if err != nil {
		panic(err)
	}
	return txConfig
}
