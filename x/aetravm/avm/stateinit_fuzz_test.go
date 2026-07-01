package avm

import "testing"

func FuzzStateInitImportAndHashNoPanic(f *testing.F) {
	si := newStateInit()
	encoded, err := ExportStateInit(si)
	if err == nil {
		f.Add(encoded)
	}
	f.Add([]byte("bad"))
	f.Add([]byte{0x00, 0x01, 0x02, 0x03})

	f.Fuzz(func(t *testing.T, bz []byte) {
		imported, err := ImportStateInit(bz)
		if err != nil {
			return
		}
		if _, err := HashStateInit(imported); err != nil {
			t.Fatalf("hash state init failed: %v", err)
		}
		if _, err := ExportStateInit(imported); err != nil {
			t.Fatalf("export state init failed: %v", err)
		}
	})
}
