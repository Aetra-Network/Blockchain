package types

const (
	AccountMessageKindExternal	= "external"
	AccountMessageKindInternal	= "internal"

	AuthModeSingleKey	= "single_key"

	InternalMessageSourceModule	= "module"
	InternalMessageSourceContract	= "contract"
	InternalMessageSourceSystem	= "system"
)

type ExternalMessage struct {
	AccountUser	string
	Sequence	uint64
	Signers		[]string
	// CoSignatures carry cryptographic proofs from additional auth keys; only
	// proven keys (plus the transaction's verified account_user key) count
	// toward multi-key policy thresholds. See auth_cosignature.go.
	CoSignatures	[]AuthCoSignature
	// PayloadHash binds co-signatures to the operation-specific payload
	// (CoSignaturePayloadHash of e.g. the new auth policy), so a co-signature
	// cannot be replayed onto a different payload at the same sequence.
	PayloadHash	[]byte
	Operation	string
	Amount		uint64
	CurrentHeight	uint64
}

type InternalMessage struct {
	AccountUser		string
	Source			string
	Feature			string
	Operation		string
	WhitelistedWhileFrozen	bool
}

type InternalMessagePolicy struct {
	Version		uint64
	EnabledFeature	string
}

type MsgUpdateAuthPolicy struct {
	AccountUser	string			`protobuf:"bytes,1,opt,name=account_user,json=accountUser,proto3" json:"account_user,omitempty"`
	NewAuthPolicy	AuthPolicy		`protobuf:"bytes,2,opt,name=new_auth_policy,json=newAuthPolicy,proto3" json:"new_auth_policy"`
	Signers		[]string		`protobuf:"bytes,3,rep,name=signers,proto3" json:"signers,omitempty"`
	CurrentHeight	uint64			`protobuf:"varint,4,opt,name=current_height,json=currentHeight,proto3" json:"current_height,omitempty"`
	CoSignatures	[]AuthCoSignature	`protobuf:"bytes,5,rep,name=co_signatures,json=coSignatures,proto3" json:"co_signatures,omitempty"`
}

type MsgRotateKey struct {
	AccountUser	string			`protobuf:"bytes,1,opt,name=account_user,json=accountUser,proto3" json:"account_user,omitempty"`
	OldKeyID	string			`protobuf:"bytes,2,opt,name=old_key_id,json=oldKeyID,proto3" json:"old_key_id,omitempty"`
	NewKey		AuthKey			`protobuf:"bytes,3,opt,name=new_key,json=newKey,proto3" json:"new_key"`
	Signers		[]string		`protobuf:"bytes,4,rep,name=signers,proto3" json:"signers,omitempty"`
	CurrentHeight	uint64			`protobuf:"varint,5,opt,name=current_height,json=currentHeight,proto3" json:"current_height,omitempty"`
	CoSignatures	[]AuthCoSignature	`protobuf:"bytes,6,rep,name=co_signatures,json=coSignatures,proto3" json:"co_signatures,omitempty"`
}

type MsgRecoverAccount struct {
	AccountUser	string			`protobuf:"bytes,1,opt,name=account_user,json=accountUser,proto3" json:"account_user,omitempty"`
	Signers		[]string		`protobuf:"bytes,2,rep,name=signers,proto3" json:"signers,omitempty"`
	CurrentHeight	uint64			`protobuf:"varint,3,opt,name=current_height,json=currentHeight,proto3" json:"current_height,omitempty"`
	CoSignatures	[]AuthCoSignature	`protobuf:"bytes,4,rep,name=co_signatures,json=coSignatures,proto3" json:"co_signatures,omitempty"`
}

type MsgFreezeAccount struct {
	AccountUser	string			`protobuf:"bytes,1,opt,name=account_user,json=accountUser,proto3" json:"account_user,omitempty"`
	Signers		[]string		`protobuf:"bytes,2,rep,name=signers,proto3" json:"signers,omitempty"`
	CurrentHeight	uint64			`protobuf:"varint,3,opt,name=current_height,json=currentHeight,proto3" json:"current_height,omitempty"`
	CoSignatures	[]AuthCoSignature	`protobuf:"bytes,4,rep,name=co_signatures,json=coSignatures,proto3" json:"co_signatures,omitempty"`
}

type MsgUnfreezeAccount struct {
	AccountUser		string			`protobuf:"bytes,1,opt,name=account_user,json=accountUser,proto3" json:"account_user,omitempty"`
	Signers			[]string		`protobuf:"bytes,2,rep,name=signers,proto3" json:"signers,omitempty"`
	CurrentHeight		uint64			`protobuf:"varint,3,opt,name=current_height,json=currentHeight,proto3" json:"current_height,omitempty"`
	StorageDebtPaid		bool			`protobuf:"varint,4,opt,name=storage_debt_paid,json=storageDebtPaid,proto3" json:"storage_debt_paid,omitempty"`
	OtherFreezeReason	bool			`protobuf:"varint,5,opt,name=other_freeze_reason,json=otherFreezeReason,proto3" json:"other_freeze_reason,omitempty"`
	CoSignatures		[]AuthCoSignature	`protobuf:"bytes,6,rep,name=co_signatures,json=coSignatures,proto3" json:"co_signatures,omitempty"`
}

type MsgPayStorageDebt struct {
	AccountUser	string			`protobuf:"bytes,1,opt,name=account_user,json=accountUser,proto3" json:"account_user,omitempty"`
	Amount		uint64			`protobuf:"varint,2,opt,name=amount,proto3" json:"amount,omitempty"`
	Signers		[]string		`protobuf:"bytes,3,rep,name=signers,proto3" json:"signers,omitempty"`
	CurrentHeight	uint64			`protobuf:"varint,4,opt,name=current_height,json=currentHeight,proto3" json:"current_height,omitempty"`
	CoSignatures	[]AuthCoSignature	`protobuf:"bytes,5,rep,name=co_signatures,json=coSignatures,proto3" json:"co_signatures,omitempty"`
}

type MsgUpdateAccountMetadata struct {
	AccountUser	string			`protobuf:"bytes,1,opt,name=account_user,json=accountUser,proto3" json:"account_user,omitempty"`
	Metadata	AccountMetadata		`protobuf:"bytes,2,opt,name=metadata,proto3" json:"metadata"`
	Signers		[]string		`protobuf:"bytes,3,rep,name=signers,proto3" json:"signers,omitempty"`
	CurrentHeight	uint64			`protobuf:"varint,4,opt,name=current_height,json=currentHeight,proto3" json:"current_height,omitempty"`
	CoSignatures	[]AuthCoSignature	`protobuf:"bytes,5,rep,name=co_signatures,json=coSignatures,proto3" json:"co_signatures,omitempty"`
}

type MsgUpdateAccountParams struct {
	AccountUser	string
	FeatureFlags	[]string
	Signers		[]string
	CurrentHeight	uint64
	CoSignatures	[]AuthCoSignature
}
