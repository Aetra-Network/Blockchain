package types

// Size() implementations for the same closure of types handled in
// query_marshal.go / query_marshal_unmarshal.go. See query_marshal.go for
// the full rationale. Each Size() mirrors the exact accounting
// protoc-gen-gogo emits: a 1-byte tag for field numbers 1-15, a 2-byte tag
// for field numbers 16-31 (only Contract, fields 16-24, needs this here),
// plus the length-prefix varint (sovContractsQuery) for length-delimited
// fields, following proto3's zero-value field elision.

func (m *QueryParamsRequest) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	return n
}

func (m *QueryParamsResponse) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = m.Params.Size()
	n += 1 + l + sovContractsQuery(uint64(l))
	return n
}

func (m *Params) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.Authority)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	if m.Enabled {
		n += 2
	}
	if m.MaxCodeBytes != 0 {
		n += 1 + sovContractsQuery(uint64(m.MaxCodeBytes))
	}
	if m.MaxContractStorageBytes != 0 {
		n += 1 + sovContractsQuery(uint64(m.MaxContractStorageBytes))
	}
	if m.MaxGasPerExecution != 0 {
		n += 1 + sovContractsQuery(uint64(m.MaxGasPerExecution))
	}
	if m.StorageRentPerByteBlock != 0 {
		n += 1 + sovContractsQuery(uint64(m.StorageRentPerByteBlock))
	}
	if m.MaxInitDataBytes != 0 {
		n += 1 + sovContractsQuery(uint64(m.MaxInitDataBytes))
	}
	if m.MaxStateInitSaltBytes != 0 {
		n += 1 + sovContractsQuery(uint64(m.MaxStateInitSaltBytes))
	}
	if m.MaxStateInitDependencies != 0 {
		n += 1 + sovContractsQuery(uint64(m.MaxStateInitDependencies))
	}
	if m.MaxInternalMessageGasPerBlock != 0 {
		n += 1 + sovContractsQuery(uint64(m.MaxInternalMessageGasPerBlock))
	}
	if m.MinUpgradeDelay != 0 {
		n += 1 + sovContractsQuery(uint64(m.MinUpgradeDelay))
	}
	return n
}

func (m *QueryCodeRequest) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.CodeID)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	return n
}

func (m *QueryCodeResponse) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = m.Code.Size()
	n += 1 + l + sovContractsQuery(uint64(l))
	if m.Found {
		n += 2
	}
	return n
}

func (m *CodeRecord) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.CodeID)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.CodeHash)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	if m.CodeBytes != 0 {
		n += 1 + sovContractsQuery(uint64(m.CodeBytes))
	}
	l = len(m.Bytecode)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.Owner)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	return n
}

func (m *QueryCodesRequest) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = m.Pagination.Size()
	n += 1 + l + sovContractsQuery(uint64(l))
	return n
}

func (m *QueryCodesResponse) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	if len(m.Codes) > 0 {
		for _, e := range m.Codes {
			l = e.Size()
			n += 1 + l + sovContractsQuery(uint64(l))
		}
	}
	return n
}

func (m *PageRequest) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	if m.Limit != 0 {
		n += 1 + sovContractsQuery(uint64(m.Limit))
	}
	return n
}

func (m *QueryContractRequest) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.ContractAddress)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.ChainID)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.Namespace)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.Deployer)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	if m.StateInit != nil {
		l = m.StateInit.Size()
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	return n
}

func (m *CodeDependency) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.CodeID)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.CodeHash)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	return n
}

func (m *StateInit) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	if m.ABIVersion != 0 {
		n += 1 + sovContractsQuery(uint64(m.ABIVersion))
	}
	l = len(m.CodeID)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.CodeHash)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.InitData)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.Salt)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.SaltBytes)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.Owner)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	if len(m.Libraries) > 0 {
		for _, e := range m.Libraries {
			l = e.Size()
			n += 1 + l + sovContractsQuery(uint64(l))
		}
	}
	l = len(m.InitialStorageRoot)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	if m.InitialBalanceNAET != 0 {
		n += 1 + sovContractsQuery(uint64(m.InitialBalanceNAET))
	}
	if len(m.Capabilities) > 0 {
		for _, s := range m.Capabilities {
			l = len(s)
			n += 1 + l + sovContractsQuery(uint64(l))
		}
	}
	return n
}

func (m *Contract) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.AddressUser)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.AddressRaw)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.CodeID)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.CodeHash)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.StateInitHash)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = m.StateInit.Size()
	n += 1 + l + sovContractsQuery(uint64(l))
	l = len(m.Creator)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.Owner)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.Admin)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	if m.Upgradeable {
		n += 2
	}
	if m.UpgradesDisabled {
		n += 2
	}
	if m.SystemOwned {
		n += 2
	}
	if m.StorageSchemaVersion != 0 {
		n += 1 + sovContractsQuery(uint64(m.StorageSchemaVersion))
	}
	l = len(m.InitMsg)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.Data)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	if m.Balance != 0 {
		n += 2 + sovContractsQuery(uint64(m.Balance))
	}
	l = len(m.StateRoot)
	if l > 0 {
		n += 2 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.Status)
	if l > 0 {
		n += 2 + l + sovContractsQuery(uint64(l))
	}
	if m.StorageBytes != 0 {
		n += 2 + sovContractsQuery(uint64(m.StorageBytes))
	}
	if m.LastStorageChargeHeight != 0 {
		n += 2 + sovContractsQuery(uint64(m.LastStorageChargeHeight))
	}
	if m.StorageRentDebt != 0 {
		n += 2 + sovContractsQuery(uint64(m.StorageRentDebt))
	}
	if m.LogicalTime != 0 {
		n += 2 + sovContractsQuery(uint64(m.LogicalTime))
	}
	if m.CreatedHeight != 0 {
		n += 2 + sovContractsQuery(uint64(m.CreatedHeight))
	}
	if m.UpdatedHeight != 0 {
		n += 2 + sovContractsQuery(uint64(m.UpdatedHeight))
	}
	l = len(m.PendingUpgradeCodeID)
	if l > 0 {
		n += 2 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.PendingUpgradeMigrationHandler)
	if l > 0 {
		n += 2 + l + sovContractsQuery(uint64(l))
	}
	if m.PendingUpgradeScheduledHeight != 0 {
		n += 2 + sovContractsQuery(uint64(m.PendingUpgradeScheduledHeight))
	}
	if m.PendingUpgradeEarliestHeight != 0 {
		n += 2 + sovContractsQuery(uint64(m.PendingUpgradeEarliestHeight))
	}
	return n
}

func (m *QueryContractResponse) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.ContractAddress)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.StateRoot)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	if m.Found {
		n += 2
	}
	if m.Virtual {
		n += 2
	}
	l = m.Contract.Size()
	n += 1 + l + sovContractsQuery(uint64(l))
	l = len(m.Status)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	return n
}

func (m *QueryContractsRequest) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = m.Pagination.Size()
	n += 1 + l + sovContractsQuery(uint64(l))
	return n
}

func (m *QueryContractsResponse) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	if len(m.Contracts) > 0 {
		for _, e := range m.Contracts {
			l = e.Size()
			n += 1 + l + sovContractsQuery(uint64(l))
		}
	}
	return n
}

func (m *QueryContractStorageRequest) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.ContractAddress)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.KeyPrefix)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = m.Pagination.Size()
	n += 1 + l + sovContractsQuery(uint64(l))
	return n
}

func (m *ContractStorageEntry) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.ContractAddress)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.Key)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.Value)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	return n
}

func (m *QueryContractStorageResponse) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	if len(m.Entries) > 0 {
		for _, e := range m.Entries {
			l = e.Size()
			n += 1 + l + sovContractsQuery(uint64(l))
		}
	}
	return n
}

func (m *QueryContractReceiptsRequest) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.ContractAddress)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = m.Pagination.Size()
	n += 1 + l + sovContractsQuery(uint64(l))
	return n
}

func (m *ContractReceipt) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.ReceiptID)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.ContractAddress)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.Actor)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	l = len(m.Operation)
	if l > 0 {
		n += 1 + l + sovContractsQuery(uint64(l))
	}
	if m.ExitCode != 0 {
		n += 1 + sovContractsQuery(uint64(m.ExitCode))
	}
	if m.Amount != 0 {
		n += 1 + sovContractsQuery(uint64(m.Amount))
	}
	if m.GasUsed != 0 {
		n += 1 + sovContractsQuery(uint64(m.GasUsed))
	}
	if m.LogicalTime != 0 {
		n += 1 + sovContractsQuery(uint64(m.LogicalTime))
	}
	if m.Height != 0 {
		n += 1 + sovContractsQuery(uint64(m.Height))
	}
	return n
}

func (m *QueryContractReceiptsResponse) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	if len(m.Receipts) > 0 {
		for _, e := range m.Receipts {
			l = e.Size()
			n += 1 + l + sovContractsQuery(uint64(l))
		}
	}
	return n
}
