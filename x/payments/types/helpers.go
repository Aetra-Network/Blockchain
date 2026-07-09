package types

import (
	"errors"
	"fmt"
	"strings"

	sdkmath "cosmossdk.io/math"

	"github.com/sovereign-l1/l1/app/addressing"
)

func setFromStrings(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func parsePositiveInt(field, value string) (sdkmath.Int, error) {
	out, ok := sdkmath.NewIntFromString(strings.TrimSpace(value))
	if !ok || !out.IsPositive() {
		return sdkmath.Int{}, fmt.Errorf("%s must be a positive integer", field)
	}
	return out, nil
}

func parseNonNegativeInt(field, value string) (sdkmath.Int, error) {
	out, ok := sdkmath.NewIntFromString(strings.TrimSpace(value))
	if !ok || out.IsNegative() {
		return sdkmath.Int{}, fmt.Errorf("%s must be a non-negative integer", field)
	}
	return out, nil
}

func validatePositiveInt(field, value string) error {
	_, err := parsePositiveInt(field, value)
	return err
}

func validateNonNegativeInt(field, value string) error {
	_, err := parseNonNegativeInt(field, value)
	return err
}

func normalizeHashSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeHash(value)
		if normalized == "" {
			continue
		}
		out = append(out, normalized)
	}
	sortStrings(out)
	return out
}

func normalizeAddressSet(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		if _, found := seen[normalized]; found {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sortStrings(out)
	return out
}

func normalizeRequiredFields(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		if _, found := seen[normalized]; found {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sortStrings(out)
	return out
}

func validateRequiredFields(fields []string) error {
	fields = normalizeRequiredFields(fields)
	known := normalizeRequiredFields(CanonicalStateRequiredFields())
	knownSet := make(map[string]struct{}, len(known))
	for _, field := range known {
		knownSet[field] = struct{}{}
	}
	for _, field := range fields {
		if _, found := knownSet[field]; !found {
			return fmt.Errorf("payments channel state unknown required field %q", field)
		}
	}
	if len(fields) != len(known) {
		return errors.New("payments channel state required fields mismatch")
	}
	for i, field := range fields {
		if field != known[i] {
			return fmt.Errorf("payments channel state unknown required field %q", field)
		}
	}
	return nil
}

func normalizeAddress(value string) string {
	return strings.TrimSpace(value)
}

func validateAddressSet(field string, values []string, min, max int) error {
	if len(values) < min || len(values) > max {
		return fmt.Errorf("%s count must be between %d and %d", field, min, max)
	}
	seen := make(map[string]struct{}, len(values))
	var previous string
	for i, value := range values {
		if err := addressing.ValidateUserAddress(field, value); err != nil {
			return err
		}
		if _, found := seen[value]; found {
			return fmt.Errorf("duplicate %s", field)
		}
		seen[value] = struct{}{}
		if i > 0 && previous >= value {
			return fmt.Errorf("%s set must be sorted canonically", field)
		}
		previous = value
	}
	return nil
}

func normalizeHash(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeOptionalHash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return normalizeHash(value)
}

func normalizeAssetDenom(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return NativeDenom
	}
	return value
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
