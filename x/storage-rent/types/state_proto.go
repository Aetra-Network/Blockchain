package types

import (
	gogoproto "github.com/cosmos/gogoproto/proto"
)

func (m *ContractRentRecord) Reset()          { *m = ContractRentRecord{} }
func (m *ContractRentRecord) String() string   { return gogoproto.CompactTextString(m) }
func (*ContractRentRecord) ProtoMessage()       {}

func (m *ContractRentRecord) XXX_Unmarshal(b []byte) error { return nil }
func (m *ContractRentRecord) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return gogoproto.Marshal(m)
	}
	return nil, nil
}

func (m *StorageRentParams) Reset()          { *m = StorageRentParams{} }
func (m *StorageRentParams) String() string   { return gogoproto.CompactTextString(m) }
func (*StorageRentParams) ProtoMessage()       {}

func (m *StorageRentParams) XXX_Unmarshal(b []byte) error { return nil }
func (m *StorageRentParams) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return gogoproto.Marshal(m)
	}
	return nil, nil
}

func (m *RentDistributionRecord) Reset()          { *m = RentDistributionRecord{} }
func (m *RentDistributionRecord) String() string   { return gogoproto.CompactTextString(m) }
func (*RentDistributionRecord) ProtoMessage()       {}

func (m *RentExemption) Reset()          { *m = RentExemption{} }
func (m *RentExemption) String() string   { return gogoproto.CompactTextString(m) }
func (*RentExemption) ProtoMessage()       {}

func (m *StorageRentState) Reset()          { *m = StorageRentState{} }
func (m *StorageRentState) String() string   { return gogoproto.CompactTextString(m) }
func (*StorageRentState) ProtoMessage()       {}

func (m *SystemRentReserve) Reset()          { *m = SystemRentReserve{} }
func (m *SystemRentReserve) String() string   { return gogoproto.CompactTextString(m) }
func (*SystemRentReserve) ProtoMessage()       {}

func init() {
	gogoproto.RegisterType((*ContractRentRecord)(nil), "l1.storagerent.v1.ContractRentRecord")
	gogoproto.RegisterType((*StorageRentParams)(nil), "l1.storagerent.v1.StorageRentParams")
	gogoproto.RegisterType((*RentDistributionRecord)(nil), "l1.storagerent.v1.RentDistributionRecord")
	gogoproto.RegisterType((*RentExemption)(nil), "l1.storagerent.v1.RentExemption")
	gogoproto.RegisterType((*StorageRentState)(nil), "l1.storagerent.v1.StorageRentState")
	gogoproto.RegisterType((*SystemRentReserve)(nil), "l1.storagerent.v1.SystemRentReserve")
}