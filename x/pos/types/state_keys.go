package types

import (
	"errors"
	"fmt"
	"strings"
)

func EpochCurrentKey() string { return "epoch/current" }

func EpochRecordKey(epochID uint64) string {
	return stateKey("epoch", "records", uint64StateComponent(epochID))
}

func EpochPhaseKey(epochID uint64) string {
	return stateKey("epoch", "phase", uint64StateComponent(epochID))
}

func EpochSeedKey(epochID uint64) string {
	return stateKey("epoch", "seed", uint64StateComponent(epochID))
}

func ValidatorScoreKey(epochID uint64, validator string) (string, error) {
	return stateKeyChecked("valecon", "scores", uint64StateComponent(epochID), validator)
}

func ValidatorEffectiveStakeKey(epochID uint64, validator string) (string, error) {
	return stateKeyChecked("valecon", "effective_stake", uint64StateComponent(epochID), validator)
}

func ValidatorSaturationKey(epochID uint64, validator string) (string, error) {
	return stateKeyChecked("valecon", "saturation", uint64StateComponent(epochID), validator)
}

func ValidatorRoleKey(epochID uint64, validator string, role ValidatorRole) (string, error) {
	return stateKeyChecked("valecon", "roles", uint64StateComponent(epochID), validator, string(role))
}

func TaskGroupKey(epochID uint64, taskGroupID string) (string, error) {
	return stateKeyChecked("taskgroups", "groups", uint64StateComponent(epochID), taskGroupID)
}

func WorkloadKey(workloadID string) (string, error) {
	return stateKeyChecked("taskgroups", "workloads", workloadID)
}

func TaskAssignmentKey(epochID uint64, validator string, taskGroupID string) (string, error) {
	return stateKeyChecked("taskgroups", "assignments", uint64StateComponent(epochID), validator, taskGroupID)
}

func ProposerKey(epochID uint64, slot uint64, taskGroupID string) (string, error) {
	return stateKeyChecked("taskgroups", "proposer", uint64StateComponent(epochID), uint64StateComponent(slot), taskGroupID)
}

func EvidenceRecordKey(evidenceID string) (string, error) {
	return stateKeyChecked("evidence", "records", evidenceID)
}

func EvidenceByAccusedKey(validator string, evidenceID string) (string, error) {
	return stateKeyChecked("evidence", "by_accused", validator, evidenceID)
}

func EvidenceByReporterKey(reporter string, evidenceID string) (string, error) {
	return stateKeyChecked("evidence", "by_reporter", reporter, evidenceID)
}

func EvidenceVerificationGroupKey(evidenceID string) (string, error) {
	return stateKeyChecked("evidence", "verification_groups", evidenceID)
}

func EvidenceDepositKey(evidenceID string) (string, error) {
	return stateKeyChecked("evidence", "deposits", evidenceID)
}

func PerformanceRecordKey(epochID uint64, operator string, role ValidatorRole) (string, error) {
	return stateKeyChecked("performance", "records", uint64StateComponent(epochID), operator, string(role))
}

func PerformanceUptimeKey(epochID uint64, validator string) (string, error) {
	return stateKeyChecked("performance", "uptime", uint64StateComponent(epochID), validator)
}

func PerformanceCorrectnessKey(epochID uint64, validator string) (string, error) {
	return stateKeyChecked("performance", "correctness", uint64StateComponent(epochID), validator)
}

func PerformanceTasksKey(epochID uint64, validator string) (string, error) {
	return stateKeyChecked("performance", "tasks", uint64StateComponent(epochID), validator)
}

func RiskUnbondingKey(delegator string, validator string, creationHeight uint64) (string, error) {
	return stateKeyChecked("risk", "unbonding", delegator, validator, uint64StateComponent(creationHeight))
}

func RiskRedelegationKey(delegator string, sourceValidator string, destinationValidator string, epochID uint64) (string, error) {
	return stateKeyChecked("risk", "redelegation", delegator, sourceValidator, destinationValidator, uint64StateComponent(epochID))
}

func RiskExposureKey(epochID uint64, validator string, delegator string) (string, error) {
	return stateKeyChecked("risk", "exposure", uint64StateComponent(epochID), validator, delegator)
}

func stateKey(parts ...string) string {
	return strings.Join(parts, "/")
}

func stateKeyChecked(parts ...string) (string, error) {
	if len(parts) == 0 {
		return "", errors.New("state key parts are required")
	}
	for _, part := range parts {
		if err := validateStateKeyComponent("state key component", part); err != nil {
			return "", err
		}
	}
	return stateKey(parts...), nil
}

func uint64StateComponent(value uint64) string {
	return fmt.Sprintf("%d", value)
}

func validateStateKeyComponent(fieldName string, value string) error {
	if err := validatePosToken(fieldName, value); err != nil {
		return err
	}
	if strings.Contains(value, "/") {
		return fmt.Errorf("%s must not contain path separator", fieldName)
	}
	return nil
}
