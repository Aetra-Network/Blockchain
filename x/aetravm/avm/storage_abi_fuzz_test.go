package avm

import "testing"

func FuzzStorageABIImportAndExportNoPanic(f *testing.F) {
	params := DefaultStorageABIParams()
	state := AVMStorageState{
		Contracts: []ContractStorageExport{
			{
				Contract: testContractRaw,
				Entries: []AVMStorageEntry{
					{Key: []byte("counter"), Value: EncodeU64(7)},
				},
			},
		},
	}
	exported, err := ImportAVMStorageState(params, state)
	if err == nil {
		roundTrip, err := exported.ExportState()
		if err == nil {
			f.Add([]byte(roundTrip.Root))
		}
	}
	f.Add([]byte("bad"))
	f.Add([]byte{0x00, 0x01, 0x02})

	f.Fuzz(func(t *testing.T, bz []byte) {
		entries := []AVMStorageEntry{
			{Key: append([]byte(nil), bz...), Value: append([]byte(nil), bz...)},
		}
		state := AVMStorageState{
			Contracts: []ContractStorageExport{
				{
					Contract: testContractRaw,
					Entries:  entries,
				},
			},
		}
		abi, err := ImportAVMStorageState(DefaultStorageABIParams(), state)
		if err != nil {
			return
		}
		if _, err := abi.ExportState(); err != nil {
			t.Fatalf("export state failed: %v", err)
		}
		if _, err := abi.IterateStorage(testContractRaw, bz, 1); err != nil && len(bz) <= int(DefaultStorageABIParams().MaxKeyBytes) {
			t.Fatalf("iterate storage failed: %v", err)
		}
	})
}
