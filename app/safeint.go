package app

import (
	"fmt"
	"math"
)

func safeUint64FromInt64(v int64, field string) (uint64, error) {
	if v < 0 {
		return 0, fmt.Errorf("%s cannot be negative: %d", field, v)
	}
	return uint64(v), nil
}

func safeUint32FromInt(v int, field string) (uint32, error) {
	if v < 0 || uint64(v) > math.MaxUint32 {
		return 0, fmt.Errorf("%s is out of range for uint32: %d", field, v)
	}
	return uint32(v), nil
}
