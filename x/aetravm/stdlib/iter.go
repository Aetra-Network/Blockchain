package stdlib

import (
	"fmt"
	"sort"
)

func IterateBounded[T any](items []T, limit int, fn func(index int, item T) error) error {
	if limit < 0 {
		return fmt.Errorf("iteration limit must be non-negative")
	}
	if len(items) > limit {
		return fmt.Errorf("iteration exceeds limit %d", limit)
	}
	for i, item := range items {
		if err := fn(i, item); err != nil {
			return err
		}
	}
	return nil
}

func IterateMapStrings[V any](items map[string]V, limit int, fn func(key string, value V) error) error {
	if limit < 0 {
		return fmt.Errorf("iteration limit must be non-negative")
	}
	if len(items) > limit {
		return fmt.Errorf("iteration exceeds limit %d", limit)
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := fn(key, items[key]); err != nil {
			return err
		}
	}
	return nil
}

