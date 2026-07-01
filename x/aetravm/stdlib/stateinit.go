package stdlib

import (
	"errors"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	aetraaddress "github.com/sovereign-l1/l1/app/addressing"
	"github.com/sovereign-l1/l1/x/aetravm/avm"
	"github.com/sovereign-l1/l1/x/aetravm/chunk"
)

type StateInitBuilder struct {
	si      avm.StateInit
	root    *chunk.Chunk
	err     error
}

func NewStateInitBuilder() *StateInitBuilder {
	return &StateInitBuilder{
		si: avm.StateInit{ABIVersion: avm.StateInitABIVersion},
	}
}

func (b *StateInitBuilder) ABIVersion(version uint32) *StateInitBuilder {
	if b.err != nil {
		return b
	}
	b.si.ABIVersion = version
	return b
}

func (b *StateInitBuilder) CodeHash(hash [32]byte) *StateInitBuilder {
	if b.err != nil {
		return b
	}
	b.si.CodeHash = hash
	return b
}

func (b *StateInitBuilder) InitData(data []byte) *StateInitBuilder {
	if b.err != nil {
		return b
	}
	b.si.InitData = append([]byte(nil), data...)
	return b
}

func (b *StateInitBuilder) Salt(salt []byte) *StateInitBuilder {
	if b.err != nil {
		return b
	}
	b.si.Salt = append([]byte(nil), salt...)
	return b
}

func (b *StateInitBuilder) Deployer(addr Address) *StateInitBuilder {
	if b.err != nil {
		return b
	}
	b.si.DeployerAddress = aetraaddress.FormatAccAddress(sdk.AccAddress(addr.raw))
	return b
}

func (b *StateInitBuilder) ChainID(chainID string) *StateInitBuilder {
	if b.err != nil {
		return b
	}
	b.si.ChainID = strings.TrimSpace(chainID)
	return b
}

func (b *StateInitBuilder) Namespace(namespace string) *StateInitBuilder {
	if b.err != nil {
		return b
	}
	b.si.Namespace = strings.TrimSpace(namespace)
	return b
}

func (b *StateInitBuilder) Dependency(hash [32]byte) *StateInitBuilder {
	if b.err != nil {
		return b
	}
	b.si.DependencyHashes = append(b.si.DependencyHashes, hash)
	return b
}

func (b *StateInitBuilder) InitialStateRoot(root *Cell) *StateInitBuilder {
	if b.err != nil {
		return b
	}
	if root != nil {
		b.root = root.root
	}
	return b
}

func (b *StateInitBuilder) InitialBalance(balance uint64) *StateInitBuilder {
	if b.err != nil {
		return b
	}
	b.si.InitialBalance = balance
	return b
}

func (b *StateInitBuilder) Capabilities(flags uint64) *StateInitBuilder {
	if b.err != nil {
		return b
	}
	b.si.Capabilities = avm.DeployCapabilityMask{Flags: flags}
	return b
}

func (b *StateInitBuilder) Build() (*avm.StateInit, [32]byte, error) {
	if b.err != nil {
		return nil, [32]byte{}, b.err
	}
	if b.si.CodeHash == [32]byte{} {
		return nil, [32]byte{}, errors.New("state init code hash is required")
	}
	if strings.TrimSpace(b.si.DeployerAddress) == "" {
		return nil, [32]byte{}, errors.New("state init deployer is required")
	}
	if strings.TrimSpace(b.si.ChainID) == "" {
		return nil, [32]byte{}, errors.New("state init chain id is required")
	}
	b.si.InitialStateRoot = b.root
	hash, err := avm.HashStateInit(&b.si)
	if err != nil {
		return nil, [32]byte{}, err
	}
	si := b.si
	if len(si.DependencyHashes) > 0 {
		si.DependencyHashes = append([][32]byte(nil), si.DependencyHashes...)
	}
	if len(si.InitData) > 0 {
		si.InitData = append([]byte(nil), si.InitData...)
	}
	if len(si.Salt) > 0 {
		si.Salt = append([]byte(nil), si.Salt...)
	}
	return &si, hash, nil
}
