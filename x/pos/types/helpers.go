package types

import (
	"errors"
	"fmt"
	"sort"

	sdkmath "cosmossdk.io/math"
)

func validatorSetMap(fieldName string, validatorIDs []string) (map[string]struct{}, error) {
	seen := make(map[string]struct{}, len(validatorIDs))
	for _, validatorID := range validatorIDs {
		if err := validatePosToken(fieldName, validatorID); err != nil {
			return nil, err
		}
		if _, found := seen[validatorID]; found {
			return nil, fmt.Errorf("duplicate %s %q", fieldName, validatorID)
		}
		seen[validatorID] = struct{}{}
	}
	return seen, nil
}

func validateCanonicalValidatorIDs(fieldName string, validatorIDs []string) error {
	if len(validatorIDs) == 0 {
		return fmt.Errorf("%s ids are required", fieldName)
	}
	seen := make(map[string]struct{}, len(validatorIDs))
	var previous string
	for i, validatorID := range validatorIDs {
		if err := validatePosToken(fieldName+" id", validatorID); err != nil {
			return err
		}
		if _, found := seen[validatorID]; found {
			return fmt.Errorf("duplicate %s id %q", fieldName, validatorID)
		}
		seen[validatorID] = struct{}{}
		if i > 0 && previous >= validatorID {
			return fmt.Errorf("%s ids must be sorted canonically", fieldName)
		}
		previous = validatorID
	}
	return nil
}

func validateValidatorIDSet(fieldName string, values []string, expectedMembers []string) error {
	if len(values) != len(expectedMembers) {
		return fmt.Errorf("%s ids must include every task group member", fieldName)
	}
	if err := validateValidatorIDSubset(fieldName, values, expectedMembers); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, found := seen[value]; found {
			return fmt.Errorf("duplicate %s id %q", fieldName, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateValidatorIDSubset(fieldName string, values []string, members []string) error {
	memberSet := make(map[string]struct{}, len(members))
	for _, member := range members {
		memberSet[member] = struct{}{}
	}
	for _, value := range values {
		if _, found := memberSet[value]; !found {
			return fmt.Errorf("%s id %q is not a task group member", fieldName, value)
		}
	}
	return nil
}

func checkedAddUint64(left uint64, right uint64, message string) (uint64, error) {
	if ^uint64(0)-left < right {
		return 0, errors.New(message)
	}
	return left + right, nil
}

func mulUint64Overflow(left uint64, right uint64) (uint64, bool) {
	if left == 0 || right == 0 {
		return 0, false
	}
	if left > ^uint64(0)/right {
		return 0, true
	}
	return left * right, false
}

func ceilDivUint64(value uint64, divisor uint64) uint64 {
	if divisor == 0 {
		return 0
	}
	if value == 0 {
		return 0
	}
	return 1 + (value-1)/divisor
}

func validateSortedUniqueTokens(fieldName string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	var previous string
	for i, value := range values {
		if err := validatePosToken(fieldName, value); err != nil {
			return err
		}
		if _, found := seen[value]; found {
			return fmt.Errorf("duplicate %s %q", fieldName, value)
		}
		seen[value] = struct{}{}
		if i > 0 && previous >= value {
			return fmt.Errorf("%s values must be sorted canonically", fieldName)
		}
		previous = value
	}
	return nil
}

func cloneStringSlice(values []string) []string {
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func sortedStringKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func ratioBps(numerator uint64, denominator uint64) uint32 {
	if denominator == 0 {
		return BasisPoints
	}
	if numerator >= denominator {
		return BasisPoints
	}
	return uint32((uint64(BasisPoints) * numerator) / denominator)
}

func intRatioBps(numerator sdkmath.Int, denominator sdkmath.Int) uint32 {
	if denominator.IsNil() || !denominator.IsPositive() {
		return BasisPoints
	}
	if numerator.IsNil() || !numerator.IsPositive() {
		return 0
	}
	if numerator.GTE(denominator) {
		return BasisPoints
	}
	return uint32(numerator.MulRaw(int64(BasisPoints)).Quo(denominator).Uint64())
}

func boolAsUint64(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}
