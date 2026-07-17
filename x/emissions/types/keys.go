package types

import appparams "github.com/sovereign-l1/l1/app/params"

const (
	ModuleName	= "emissions"
	StoreKey	= ModuleName
	RouterKey	= ModuleName

	BaseDenom		= appparams.BaseDenom
	BasisPoints	uint32	= 10_000
)

var (
	ParamsKey			= []byte{0x01}
	EpochPrefix			= []byte{0x02}
	TotalMintedAccountingKey	= []byte{0x03}
	// LastEpochTimeKey stores the consensus block time of the last finalized
	// emission epoch, as a big-endian int64 of Unix seconds. It is what makes
	// the epoch fire on elapsed TIME rather than on a block count -- see
	// appparams.EmissionEpochDuration.
	LastEpochTimeKey		= []byte{0x04}
)
