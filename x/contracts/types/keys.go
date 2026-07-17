package types

const (
	ModuleName = "contracts"
	StoreKey   = ModuleName
)

// Per-record store key prefixes.
//
// The module's committed state used to live entirely under the keeper's
// genesisKey = {0x01} -- one JSON blob holding every code, contract and
// receipt. Gas is charged per byte written (WriteCostPerByte = 30, ten times
// the read cost), so every contract transaction re-wrote every other dapp's
// bytecode and paid for it: measured gas was exactly
// 3,033 + 3*blobBefore + 30*blobAfter, i.e. ~33 gas per byte of TOTAL module
// state, and a single contract crossed MaxTxGas = 1,000,000 on its 90th
// execution because every operation appended a receipt to that same blob.
//
// The three collections that grow with usage now get ONE KV RECORD EACH under
// the prefixes below, so a write touches only the records it actually changed.
//
// These prefixes must never collide with the keeper's genesisKey ({0x01}),
// which still holds the residual GenesisState (params, state root, and the
// small collections that do not grow with usage). They are distinct first
// bytes, so a prefix scan of any one of them can never observe another's
// records or the residual blob.
//
// Key layout:
//
//	{0x01}                  residual GenesisState (params + state root + cold collections)
//	{0x02} || code_id       one CodeRecord
//	{0x03} || address_user  one Contract
//	{0x04} || receipt_id    one ContractReceipt
//
// CodeID and ReceiptID are hex-encoded SHA-256 digests (fixed width) and
// AddressUser is a validated user-facing AE address, so within one namespace no
// key is a prefix of another and each record round-trips to exactly one entity.
var (
	CodeKeyPrefix     = []byte{0x02}
	ContractKeyPrefix = []byte{0x03}
	ReceiptKeyPrefix  = []byte{0x04}
)

// CodeKey returns the store key holding the CodeRecord for codeID.
func CodeKey(codeID string) []byte {
	return append(append([]byte(nil), CodeKeyPrefix...), codeID...)
}

// ContractKey returns the store key holding the Contract at addressUser.
func ContractKey(addressUser string) []byte {
	return append(append([]byte(nil), ContractKeyPrefix...), addressUser...)
}

// ReceiptKey returns the store key holding the ContractReceipt for receiptID.
func ReceiptKey(receiptID string) []byte {
	return append(append([]byte(nil), ReceiptKeyPrefix...), receiptID...)
}
