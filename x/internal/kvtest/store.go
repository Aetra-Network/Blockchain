package kvtest

import (
	"context"
	"errors"
	"slices"
	"sort"

	corestore "cosmossdk.io/core/store"
	dbm "github.com/cosmos/cosmos-db"
)

type StoreService struct {
	store *Store
}

func NewStoreService() *StoreService {
	return &StoreService{store: &Store{values: map[string][]byte{}}}
}

func (s *StoreService) OpenKVStore(context.Context) corestore.KVStore {
	return s.store
}

func (s *StoreService) RawStore() *Store {
	return s.store
}

type Store struct {
	values		map[string][]byte
	setCounts	map[string]uint64
	delCounts	map[string]uint64
}

func (s *Store) Get(key []byte) ([]byte, error) {
	if key == nil {
		return nil, errors.New("nil key")
	}
	value, found := s.values[string(key)]
	if !found {
		return nil, nil
	}
	return append([]byte(nil), value...), nil
}

func (s *Store) Has(key []byte) (bool, error) {
	if key == nil {
		return false, errors.New("nil key")
	}
	_, found := s.values[string(key)]
	return found, nil
}

func (s *Store) Set(key, value []byte) error {
	if key == nil || value == nil {
		return errors.New("nil key or value")
	}
	s.values[string(key)] = append([]byte(nil), value...)
	if s.setCounts == nil {
		s.setCounts = make(map[string]uint64)
	}
	s.setCounts[string(key)]++
	return nil
}

func (s *Store) Delete(key []byte) error {
	if key == nil {
		return errors.New("nil key")
	}
	delete(s.values, string(key))
	if s.delCounts == nil {
		s.delCounts = make(map[string]uint64)
	}
	s.delCounts[string(key)]++
	return nil
}

// Snapshot copies every committed key/value. With Restore it models BaseApp
// discarding a transaction's KV cache branch, which reverts the WHOLE store --
// every key the tx touched, not just one. Tests that hand-roll a revert by
// rewriting a single key instead silently stop modeling anything real as soon
// as a keeper spreads its state over more than that one key.
func (s *Store) Snapshot() map[string][]byte {
	out := make(map[string][]byte, len(s.values))
	for key, value := range s.values {
		out[key] = append([]byte(nil), value...)
	}
	return out
}

// Restore replaces the store's contents with a Snapshot.
func (s *Store) Restore(snapshot map[string][]byte) {
	s.values = make(map[string][]byte, len(snapshot))
	for key, value := range snapshot {
		s.values[key] = append([]byte(nil), value...)
	}
}

func (s *Store) ResetWriteCounts() {
	s.setCounts = make(map[string]uint64)
	s.delCounts = make(map[string]uint64)
}

func (s *Store) SetCount(key []byte) uint64 {
	if s.setCounts == nil {
		return 0
	}
	return s.setCounts[string(key)]
}

func (s *Store) DeleteCount(key []byte) uint64 {
	if s.delCounts == nil {
		return 0
	}
	return s.delCounts[string(key)]
}

// Iterator returns an ascending iterator over [start, end). A nil start means
// "from the beginning" and a nil end means "to the very end", matching the
// corestore.KVStore contract. Keepers that store one KV record per entity read
// those records back by prefix scan, so this double has to iterate for real
// rather than reject the call.
func (s *Store) Iterator(start, end []byte) (dbm.Iterator, error) {
	return s.newIterator(start, end, true)
}

// ReverseIterator returns a descending iterator over [start, end).
func (s *Store) ReverseIterator(start, end []byte) (dbm.Iterator, error) {
	return s.newIterator(start, end, false)
}

func (s *Store) newIterator(start, end []byte, ascending bool) (dbm.Iterator, error) {
	if (start != nil && len(start) == 0) || (end != nil && len(end) == 0) {
		return nil, errors.New("empty start/end key")
	}
	keys := make([]string, 0, len(s.values))
	for key := range s.values {
		if start != nil && key < string(start) {
			continue
		}
		if end != nil && key >= string(end) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if !ascending {
		slices.Reverse(keys)
	}
	it := &iterator{store: s, keys: keys, start: start, end: end}
	return it, nil
}

type iterator struct {
	store *Store
	keys  []string
	pos   int
	start []byte
	end   []byte
}

func (i *iterator) Domain() (start, end []byte) { return i.start, i.end }
func (i *iterator) Valid() bool                 { return i.pos < len(i.keys) }
func (i *iterator) Next() {
	if i.Valid() {
		i.pos++
	}
}

func (i *iterator) Key() []byte {
	if !i.Valid() {
		panic("iterator is invalid")
	}
	return []byte(i.keys[i.pos])
}

func (i *iterator) Value() []byte {
	if !i.Valid() {
		panic("iterator is invalid")
	}
	return append([]byte(nil), i.store.values[i.keys[i.pos]]...)
}

func (i *iterator) Error() error { return nil }
func (i *iterator) Close() error { return nil }
