package types

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	sdkmath "cosmossdk.io/math"
)

type ValidatorCapacity struct {
	MaxTaskGroups          uint32
	SupportedWorkloads     []WorkloadType
	ZoneSupport            []string
	HardwareClassOptional  string
	NetworkClassOptional   string
	AvailabilityCommitment uint32
}

type WorkloadTask struct {
	TaskID             string
	WorkloadID         string
	WorkloadType       WorkloadType
	ZoneID             string
	ShardID            string
	WorkloadClass      string
	RequiredValidators uint32
	Roles              []ValidatorRole
	ExcludedValidators []string
}

type WorkloadType string

type TaskAssignment struct {
	TaskID         string
	WorkloadID     string
	WorkloadType   WorkloadType
	ZoneID         string
	ShardID        string
	WorkloadClass  string
	Role           ValidatorRole
	Validators     []string
	AssignmentHash string
}

type TaskAssignmentSet struct {
	EpochID     uint64
	Seed        string
	Assignments []TaskAssignment
	Root        string
}

type TaskGroup struct {
	EpochID          uint64
	TaskGroupID      string
	WorkloadID       string
	WorkloadType     WorkloadType
	ValidatorMembers []string
	ProposerOrder    []string
	VerifierSet      []string
	MinimumGroupSize uint32
	StakeWeightRoot  string
	AssignmentSeed   string
	ActivationHeight uint64
	ExpiryHeight     uint64
}

type TaskGroupSet struct {
	EpochID uint64
	Seed    string
	Groups  []TaskGroup
	Root    string
}

func BuildTaskAssignments(params Params, epoch EpochRecord, validators []ScoredValidator, tasks []WorkloadTask) (TaskAssignmentSet, error) {
	if err := params.Validate(); err != nil {
		return TaskAssignmentSet{}, err
	}
	if err := epoch.Validate(); err != nil {
		return TaskAssignmentSet{}, err
	}
	if len(validators) == 0 {
		return TaskAssignmentSet{}, errors.New("task assignment requires active validators")
	}
	validatorSetHash, err := ComputeValidatorSetHash(validators)
	if err != nil {
		return TaskAssignmentSet{}, err
	}
	if validatorSetHash != epoch.ValidatorSetHash {
		return TaskAssignmentSet{}, errors.New("task assignments require committed validator set hash")
	}
	if len(tasks) == 0 {
		return TaskAssignmentSet{
			EpochID: epoch.EpochID,
			Seed:    epoch.Seed,
			Root:    PosEmptyRootHash,
		}, nil
	}

	orderedTasks := make([]WorkloadTask, len(tasks))
	for i, task := range tasks {
		normalized := normalizeWorkloadTask(params, task)
		if err := normalized.Validate(params); err != nil {
			return TaskAssignmentSet{}, err
		}
		orderedTasks[i] = normalized
	}
	sort.SliceStable(orderedTasks, func(i, j int) bool {
		return compareWorkloadTasks(orderedTasks[i], orderedTasks[j]) < 0
	})

	assignments := make([]TaskAssignment, 0)
	assignedTaskKeys := make(map[string]map[string]struct{})
	for _, task := range orderedTasks {
		key := taskKey(task)
		for _, role := range normalizedRoles(task.Roles, DefaultTaskRoles()) {
			eligible := validatorsForTaskRole(validators, task, role, assignedTaskKeys, key)
			if uint32(len(eligible)) < task.RequiredValidators {
				return TaskAssignmentSet{}, fmt.Errorf("insufficient validators for task %s role %s", task.TaskID, role)
			}
			selected := selectTaskValidatorIDs(epoch.Seed, task, role, eligible, task.RequiredValidators)
			markTaskGroupAssignments(assignedTaskKeys, key, selected)
			assignment := TaskAssignment{
				TaskID:        task.TaskID,
				WorkloadID:    task.WorkloadID,
				WorkloadType:  task.WorkloadType,
				ZoneID:        task.ZoneID,
				ShardID:       task.ShardID,
				WorkloadClass: task.WorkloadClass,
				Role:          role,
				Validators:    selected,
			}
			assignment.AssignmentHash = ComputeTaskAssignmentHash(epoch.EpochID, epoch.Seed, assignment)
			assignments = append(assignments, assignment)
		}
	}
	sort.SliceStable(assignments, func(i, j int) bool {
		return compareTaskAssignments(assignments[i], assignments[j]) < 0
	})
	root := ComputeTaskAssignmentRoot(epoch.EpochID, epoch.Seed, assignments)
	out := TaskAssignmentSet{EpochID: epoch.EpochID, Seed: epoch.Seed, Assignments: assignments, Root: root}
	return out, out.Validate()
}

func BuildTaskGroups(params Params, epoch EpochRecord, validators []ScoredValidator, tasks []WorkloadTask, activationHeight uint64, expiryHeight uint64) (TaskGroupSet, error) {
	if activationHeight == 0 {
		return TaskGroupSet{}, errors.New("task group activation height is required")
	}
	if expiryHeight <= activationHeight {
		return TaskGroupSet{}, errors.New("task group expiry height must be after activation height")
	}
	assignments, err := BuildTaskAssignments(params, epoch, validators, tasks)
	if err != nil {
		return TaskGroupSet{}, err
	}
	if len(tasks) == 0 {
		return TaskGroupSet{EpochID: epoch.EpochID, Seed: epoch.Seed, Root: PosEmptyRootHash}, nil
	}
	validatorByID := make(map[string]ScoredValidator, len(validators))
	for _, validator := range validators {
		validatorByID[validator.ValidatorID] = validator
	}
	taskByID := make(map[string]WorkloadTask, len(tasks))
	for _, task := range tasks {
		normalized := normalizeWorkloadTask(params, task)
		taskByID[taskKey(normalized)] = normalized
	}
	assignmentsByTask := make(map[string][]TaskAssignment)
	for _, assignment := range assignments.Assignments {
		key := taskKey(WorkloadTask{
			TaskID:        assignment.TaskID,
			WorkloadID:    assignment.WorkloadID,
			WorkloadType:  assignment.WorkloadType,
			ZoneID:        assignment.ZoneID,
			ShardID:       assignment.ShardID,
			WorkloadClass: assignment.WorkloadClass,
		})
		assignmentsByTask[key] = append(assignmentsByTask[key], assignment)
	}
	groups := make([]TaskGroup, 0, len(taskByID))
	taskKeys := sortedStringKeys(taskByID)
	for _, key := range taskKeys {
		task := taskByID[key]
		taskAssignments := assignmentsByTask[key]
		members := taskGroupMembers(taskAssignments)
		verifiers := taskGroupVerifiers(taskAssignments, members)
		group := TaskGroup{
			EpochID:          epoch.EpochID,
			WorkloadID:       task.WorkloadID,
			WorkloadType:     task.WorkloadType,
			ValidatorMembers: members,
			ProposerOrder:    taskGroupProposerOrder(epoch.Seed, task, members),
			VerifierSet:      verifiers,
			MinimumGroupSize: task.RequiredValidators,
			StakeWeightRoot:  ComputeTaskGroupStakeWeightRoot(epoch.EpochID, task, members, validatorByID),
			AssignmentSeed:   epoch.Seed,
			ActivationHeight: activationHeight,
			ExpiryHeight:     expiryHeight,
		}
		group.TaskGroupID = ComputeTaskGroupID(group)
		groups = append(groups, group)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return compareTaskGroups(groups[i], groups[j]) < 0
	})
	out := TaskGroupSet{
		EpochID: epoch.EpochID,
		Seed:    epoch.Seed,
		Groups:  groups,
		Root:    ComputeTaskGroupRoot(epoch.EpochID, epoch.Seed, groups),
	}
	return out, out.Validate()
}

func (t WorkloadTask) Validate(params Params) error {
	if err := validatePosToken("task id", t.TaskID); err != nil {
		return err
	}
	if err := validatePosToken("task workload id", t.WorkloadID); err != nil {
		return err
	}
	if err := validateWorkloadType(t.WorkloadType); err != nil {
		return err
	}
	if err := validatePosToken("task zone id", t.ZoneID); err != nil {
		return err
	}
	if err := validatePosToken("task shard id", t.ShardID); err != nil {
		return err
	}
	if err := validatePosToken("task workload class", t.WorkloadClass); err != nil {
		return err
	}
	if t.RequiredValidators < params.MinTaskGroupValidators {
		return fmt.Errorf("task validators must be at least %d", params.MinTaskGroupValidators)
	}
	if t.RequiredValidators > params.MaxTaskGroupValidators {
		return fmt.Errorf("task validators must be <= %d", params.MaxTaskGroupValidators)
	}
	if err := validateValidatorRoles(t.Roles); err != nil {
		return err
	}
	return validateExcludedValidators(t.ExcludedValidators)
}

func (c ValidatorCapacity) Validate() error {
	if c.MaxTaskGroups == 0 && len(c.SupportedWorkloads) == 0 && len(c.ZoneSupport) == 0 && c.HardwareClassOptional == "" && c.NetworkClassOptional == "" && c.AvailabilityCommitment == 0 {
		return nil
	}
	if c.MaxTaskGroups == 0 {
		return errors.New("validator capacity max task groups must be positive when capacity is declared")
	}
	if c.AvailabilityCommitment > BasisPoints {
		return fmt.Errorf("validator availability commitment must be <= %d bps", BasisPoints)
	}
	if err := validateWorkloadTypes(c.SupportedWorkloads); err != nil {
		return err
	}
	if err := validateZoneSupport(c.ZoneSupport); err != nil {
		return err
	}
	if c.HardwareClassOptional != "" {
		if err := validatePosToken("hardware class", c.HardwareClassOptional); err != nil {
			return err
		}
	}
	if c.NetworkClassOptional != "" {
		if err := validatePosToken("network class", c.NetworkClassOptional); err != nil {
			return err
		}
	}
	return nil
}

func (c ValidatorCapacity) SupportsAssignment(task WorkloadTask, assignedTaskKeys map[string]map[string]struct{}, validatorID string, taskKey string) bool {
	if !c.supportsWorkload(task.WorkloadType) || !c.supportsZone(task.ZoneID) {
		return false
	}
	if c.MaxTaskGroups == 0 {
		return true
	}
	current := assignedTaskKeys[validatorID]
	if _, alreadyAssignedToTask := current[taskKey]; alreadyAssignedToTask {
		return true
	}
	return uint32(len(current)) < c.MaxTaskGroups
}

func (c ValidatorCapacity) supportsWorkload(workloadType WorkloadType) bool {
	if len(c.SupportedWorkloads) == 0 {
		return true
	}
	for _, supported := range c.SupportedWorkloads {
		if supported == workloadType {
			return true
		}
	}
	return false
}

func (c ValidatorCapacity) supportsZone(zoneID string) bool {
	if len(c.ZoneSupport) == 0 {
		return true
	}
	for _, zone := range c.ZoneSupport {
		if zone == zoneID {
			return true
		}
	}
	return false
}

func (a TaskAssignment) Validate() error {
	if err := validatePosToken("assignment task id", a.TaskID); err != nil {
		return err
	}
	if err := validatePosToken("assignment workload id", a.WorkloadID); err != nil {
		return err
	}
	if err := validateWorkloadType(a.WorkloadType); err != nil {
		return err
	}
	if err := validatePosToken("assignment zone id", a.ZoneID); err != nil {
		return err
	}
	if err := validatePosToken("assignment shard id", a.ShardID); err != nil {
		return err
	}
	if err := validatePosToken("assignment workload class", a.WorkloadClass); err != nil {
		return err
	}
	if err := validateValidatorRole(a.Role); err != nil {
		return err
	}
	if len(a.Validators) == 0 {
		return errors.New("assignment validators are required")
	}
	seen := make(map[string]struct{}, len(a.Validators))
	var previous string
	for i, validatorID := range a.Validators {
		if err := validatePosToken("assignment validator id", validatorID); err != nil {
			return err
		}
		if _, found := seen[validatorID]; found {
			return fmt.Errorf("duplicate assignment validator %q", validatorID)
		}
		seen[validatorID] = struct{}{}
		if i > 0 && previous >= validatorID {
			return errors.New("assignment validators must be sorted canonically")
		}
		previous = validatorID
	}
	return validatePosHash("assignment hash", a.AssignmentHash)
}

func (s TaskAssignmentSet) Validate() error {
	if err := validatePosHash("assignment seed", s.Seed); err != nil {
		return err
	}
	if err := validatePosHash("assignment root", s.Root); err != nil {
		return err
	}
	for i, assignment := range s.Assignments {
		if err := assignment.Validate(); err != nil {
			return err
		}
		expectedHash := ComputeTaskAssignmentHash(s.EpochID, s.Seed, assignment)
		if assignment.AssignmentHash != expectedHash {
			return errors.New("assignment hash mismatch")
		}
		if i > 0 && compareTaskAssignments(s.Assignments[i-1], assignment) >= 0 {
			return errors.New("task assignments must be sorted canonically")
		}
	}
	expectedRoot := PosEmptyRootHash
	if len(s.Assignments) > 0 {
		expectedRoot = ComputeTaskAssignmentRoot(s.EpochID, s.Seed, s.Assignments)
	}
	if s.Root != expectedRoot {
		return errors.New("task assignment root mismatch")
	}
	return nil
}

func (g TaskGroup) Validate() error {
	if g.EpochID == 0 {
		return errors.New("task group epoch id is required")
	}
	if err := validatePosToken("task group id", g.TaskGroupID); err != nil {
		return err
	}
	if err := validatePosToken("task group workload id", g.WorkloadID); err != nil {
		return err
	}
	if err := validateWorkloadType(g.WorkloadType); err != nil {
		return err
	}
	if len(g.ValidatorMembers) < int(g.MinimumGroupSize) {
		return errors.New("task group members below minimum group size")
	}
	if err := validateCanonicalValidatorIDs("task group member", g.ValidatorMembers); err != nil {
		return err
	}
	if len(g.ProposerOrder) != len(g.ValidatorMembers) {
		return errors.New("task group proposer order must include every member")
	}
	if err := validateValidatorIDSet("task group proposer", g.ProposerOrder, g.ValidatorMembers); err != nil {
		return err
	}
	if len(g.VerifierSet) == 0 {
		return errors.New("task group verifier set is required")
	}
	if err := validateCanonicalValidatorIDs("task group verifier", g.VerifierSet); err != nil {
		return err
	}
	if err := validateValidatorIDSubset("task group verifier", g.VerifierSet, g.ValidatorMembers); err != nil {
		return err
	}
	if err := validatePosHash("task group stake weight root", g.StakeWeightRoot); err != nil {
		return err
	}
	if err := validatePosHash("task group assignment seed", g.AssignmentSeed); err != nil {
		return err
	}
	if g.ActivationHeight == 0 {
		return errors.New("task group activation height is required")
	}
	if g.ExpiryHeight <= g.ActivationHeight {
		return errors.New("task group expiry height must be after activation height")
	}
	if expected := ComputeTaskGroupID(g); g.TaskGroupID != expected {
		return errors.New("task group id mismatch")
	}
	return nil
}

func (s TaskGroupSet) Validate() error {
	if err := validatePosHash("task group set seed", s.Seed); err != nil {
		return err
	}
	if err := validatePosHash("task group set root", s.Root); err != nil {
		return err
	}
	for i, group := range s.Groups {
		if err := group.Validate(); err != nil {
			return err
		}
		if i > 0 && compareTaskGroups(s.Groups[i-1], group) >= 0 {
			return errors.New("task groups must be sorted canonically")
		}
	}
	expectedRoot := PosEmptyRootHash
	if len(s.Groups) > 0 {
		expectedRoot = ComputeTaskGroupRoot(s.EpochID, s.Seed, s.Groups)
	}
	if s.Root != expectedRoot {
		return errors.New("task group root mismatch")
	}
	return nil
}

func ComputeTaskAssignmentHash(epochID uint64, seed string, assignment TaskAssignment) string {
	return posHashRoot("aetheris-pos-task-assignment-v1", func(w posByteWriter) {
		posWriteUint64(w, epochID)
		posWritePart(w, seed)
		posWritePart(w, assignment.TaskID)
		posWritePart(w, assignment.WorkloadID)
		posWritePart(w, string(assignment.WorkloadType))
		posWritePart(w, assignment.ZoneID)
		posWritePart(w, assignment.ShardID)
		posWritePart(w, assignment.WorkloadClass)
		posWritePart(w, string(assignment.Role))
		posWriteUint64(w, uint64(len(assignment.Validators)))
		for _, validatorID := range assignment.Validators {
			posWritePart(w, validatorID)
		}
	})
}

func ComputeTaskAssignmentRoot(epochID uint64, seed string, assignments []TaskAssignment) string {
	return posHashRoot("aetheris-pos-task-assignment-root-v1", func(w posByteWriter) {
		posWriteUint64(w, epochID)
		posWritePart(w, seed)
		posWriteUint64(w, uint64(len(assignments)))
		for _, assignment := range assignments {
			posWritePart(w, assignment.AssignmentHash)
		}
	})
}

func ComputeTaskGroupID(group TaskGroup) string {
	return posHashRoot("aetheris-pos-task-group-id-v1", func(w posByteWriter) {
		posWriteUint64(w, group.EpochID)
		posWritePart(w, group.WorkloadID)
		posWritePart(w, string(group.WorkloadType))
		posWritePart(w, group.AssignmentSeed)
		posWriteUint64(w, group.ActivationHeight)
		posWriteUint64(w, group.ExpiryHeight)
	})
}

func ComputeTaskGroupStakeWeightRoot(epochID uint64, task WorkloadTask, members []string, validators map[string]ScoredValidator) string {
	return posHashRoot("aetheris-pos-task-group-stake-root-v1", func(w posByteWriter) {
		posWriteUint64(w, epochID)
		posWritePart(w, task.TaskID)
		posWritePart(w, task.WorkloadID)
		posWritePart(w, string(task.WorkloadType))
		posWriteUint64(w, uint64(len(members)))
		for _, validatorID := range members {
			validator := validators[validatorID]
			posWritePart(w, validatorID)
			posWritePart(w, validator.ScoreComponents.StakeWeightNaet.String())
			posWritePart(w, validator.VotingPowerNaet.String())
		}
	})
}

func ComputeTaskGroupRoot(epochID uint64, seed string, groups []TaskGroup) string {
	return posHashRoot("aetheris-pos-task-group-root-v1", func(w posByteWriter) {
		posWriteUint64(w, epochID)
		posWritePart(w, seed)
		posWriteUint64(w, uint64(len(groups)))
		for _, group := range groups {
			posWritePart(w, group.TaskGroupID)
			posWritePart(w, group.StakeWeightRoot)
		}
	})
}

func normalizedWorkloadTypes(workloadTypes []WorkloadType) []WorkloadType {
	out := make([]WorkloadType, len(workloadTypes))
	copy(out, workloadTypes)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i] < out[j]
	})
	return out
}

func normalizeWorkloadTask(params Params, task WorkloadTask) WorkloadTask {
	out := task
	if out.WorkloadID == "" {
		out.WorkloadID = out.TaskID
	}
	if out.WorkloadType == "" {
		out.WorkloadType = WorkloadTypeServiceValidation
	}
	if out.WorkloadClass == "" {
		out.WorkloadClass = DefaultWorkloadClass
	}
	if out.RequiredValidators == 0 {
		out.RequiredValidators = params.MinTaskGroupValidators
	}
	if len(out.Roles) == 0 {
		out.Roles = DefaultTaskRoles()
	}
	out.Roles = normalizedRoles(out.Roles, DefaultTaskRoles())
	return out
}

func cloneValidatorCapacity(capacity ValidatorCapacity) ValidatorCapacity {
	out := capacity
	out.SupportedWorkloads = make([]WorkloadType, len(capacity.SupportedWorkloads))
	copy(out.SupportedWorkloads, capacity.SupportedWorkloads)
	out.ZoneSupport = make([]string, len(capacity.ZoneSupport))
	copy(out.ZoneSupport, capacity.ZoneSupport)
	return out
}

func validateWorkloadType(workloadType WorkloadType) error {
	switch workloadType {
	case WorkloadTypeGlobalConsensus,
		WorkloadTypeZoneExecution,
		WorkloadTypeShardExecution,
		WorkloadTypeProofVerification,
		WorkloadTypeEvidenceVerification,
		WorkloadTypeDataAvailability,
		WorkloadTypeServiceValidation:
		return nil
	default:
		return fmt.Errorf("unsupported workload type %q", workloadType)
	}
}

func validateWorkloadTypes(workloadTypes []WorkloadType) error {
	seen := make(map[WorkloadType]struct{}, len(workloadTypes))
	for _, workloadType := range workloadTypes {
		if err := validateWorkloadType(workloadType); err != nil {
			return err
		}
		if _, found := seen[workloadType]; found {
			return fmt.Errorf("duplicate supported workload %q", workloadType)
		}
		seen[workloadType] = struct{}{}
	}
	return nil
}

func validateZoneSupport(zones []string) error {
	seen := make(map[string]struct{}, len(zones))
	for _, zone := range zones {
		if err := validatePosToken("zone support", zone); err != nil {
			return err
		}
		if _, found := seen[zone]; found {
			return fmt.Errorf("duplicate zone support %q", zone)
		}
		seen[zone] = struct{}{}
	}
	return nil
}

func validateExcludedValidators(validatorIDs []string) error {
	seen := make(map[string]struct{}, len(validatorIDs))
	for _, validatorID := range validatorIDs {
		if err := validatePosToken("excluded validator id", validatorID); err != nil {
			return err
		}
		if _, found := seen[validatorID]; found {
			return fmt.Errorf("duplicate excluded validator %q", validatorID)
		}
		seen[validatorID] = struct{}{}
	}
	return nil
}

func validatorsForTaskRole(validators []ScoredValidator, task WorkloadTask, role ValidatorRole, assignedTaskKeys map[string]map[string]struct{}, taskKey string) []ScoredValidator {
	out := make([]ScoredValidator, 0, len(validators))
	for _, validator := range validators {
		if ValidatorSupportsRole(validator.Candidate, role) &&
			!isExcludedValidator(validator.ValidatorID, task.ExcludedValidators) &&
			validator.Capacity.SupportsAssignment(task, assignedTaskKeys, validator.ValidatorID, taskKey) {
			out = append(out, validator)
		}
	}
	return out
}

func isExcludedValidator(validatorID string, excluded []string) bool {
	for _, excludedID := range excluded {
		if excludedID == validatorID {
			return true
		}
	}
	return false
}

func selectTaskValidatorIDs(seed string, task WorkloadTask, role ValidatorRole, validators []ScoredValidator, required uint32) []string {
	type rankedValidator struct {
		validatorID string
		rankHash    string
		score       sdkmath.Int
	}
	ranked := make([]rankedValidator, len(validators))
	for i, validator := range validators {
		ranked[i] = rankedValidator{
			validatorID: validator.ValidatorID,
			score:       validator.Score,
			rankHash: posHashRoot("aetheris-pos-task-rank-v1", func(w posByteWriter) {
				posWritePart(w, seed)
				posWritePart(w, task.TaskID)
				posWritePart(w, task.WorkloadID)
				posWritePart(w, string(task.WorkloadType))
				posWritePart(w, task.ZoneID)
				posWritePart(w, task.ShardID)
				posWritePart(w, string(role))
				posWritePart(w, validator.ValidatorID)
				posWritePart(w, validator.Score.String())
				posWritePart(w, validator.VotingPowerNaet.String())
			}),
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].rankHash != ranked[j].rankHash {
			return ranked[i].rankHash < ranked[j].rankHash
		}
		if !ranked[i].score.Equal(ranked[j].score) {
			return ranked[i].score.GT(ranked[j].score)
		}
		return ranked[i].validatorID < ranked[j].validatorID
	})
	selected := make([]string, 0, required)
	for i := uint32(0); i < required; i++ {
		selected = append(selected, ranked[i].validatorID)
	}
	sort.Strings(selected)
	return selected
}

func taskKey(task WorkloadTask) string {
	return strings.Join([]string{task.TaskID, task.WorkloadID, string(task.WorkloadType), task.ZoneID, task.ShardID, task.WorkloadClass}, "|")
}

func markTaskGroupAssignments(assignedTaskKeys map[string]map[string]struct{}, taskKey string, validatorIDs []string) {
	for _, validatorID := range validatorIDs {
		if _, found := assignedTaskKeys[validatorID]; !found {
			assignedTaskKeys[validatorID] = make(map[string]struct{})
		}
		assignedTaskKeys[validatorID][taskKey] = struct{}{}
	}
}

func taskGroupMembers(assignments []TaskAssignment) []string {
	seen := make(map[string]struct{})
	for _, assignment := range assignments {
		for _, validatorID := range assignment.Validators {
			seen[validatorID] = struct{}{}
		}
	}
	members := make([]string, 0, len(seen))
	for validatorID := range seen {
		members = append(members, validatorID)
	}
	sort.Strings(members)
	return members
}

func taskGroupVerifiers(assignments []TaskAssignment, members []string) []string {
	seen := make(map[string]struct{})
	for _, assignment := range assignments {
		if assignment.Role == ValidatorRoleVerifier || assignment.Role == ValidatorRoleEvidenceReviewer {
			for _, validatorID := range assignment.Validators {
				seen[validatorID] = struct{}{}
			}
		}
	}
	if len(seen) == 0 {
		for _, validatorID := range members {
			seen[validatorID] = struct{}{}
		}
	}
	verifiers := make([]string, 0, len(seen))
	for validatorID := range seen {
		verifiers = append(verifiers, validatorID)
	}
	sort.Strings(verifiers)
	return verifiers
}

func taskGroupProposerOrder(seed string, task WorkloadTask, members []string) []string {
	type proposerRank struct {
		validatorID string
		hash        string
	}
	ranks := make([]proposerRank, len(members))
	for i, validatorID := range members {
		ranks[i] = proposerRank{
			validatorID: validatorID,
			hash: posHashRoot("aetheris-pos-task-group-proposer-v1", func(w posByteWriter) {
				posWritePart(w, seed)
				posWritePart(w, task.TaskID)
				posWritePart(w, task.WorkloadID)
				posWritePart(w, string(task.WorkloadType))
				posWritePart(w, validatorID)
			}),
		}
	}
	sort.SliceStable(ranks, func(i, j int) bool {
		if ranks[i].hash != ranks[j].hash {
			return ranks[i].hash < ranks[j].hash
		}
		return ranks[i].validatorID < ranks[j].validatorID
	})
	out := make([]string, len(ranks))
	for i, rank := range ranks {
		out[i] = rank.validatorID
	}
	return out
}

func compareWorkloadTasks(left, right WorkloadTask) int {
	if left.TaskID < right.TaskID {
		return -1
	}
	if left.TaskID > right.TaskID {
		return 1
	}
	if left.WorkloadID < right.WorkloadID {
		return -1
	}
	if left.WorkloadID > right.WorkloadID {
		return 1
	}
	if left.WorkloadType < right.WorkloadType {
		return -1
	}
	if left.WorkloadType > right.WorkloadType {
		return 1
	}
	if left.ZoneID < right.ZoneID {
		return -1
	}
	if left.ZoneID > right.ZoneID {
		return 1
	}
	if left.ShardID < right.ShardID {
		return -1
	}
	if left.ShardID > right.ShardID {
		return 1
	}
	if left.WorkloadClass < right.WorkloadClass {
		return -1
	}
	if left.WorkloadClass > right.WorkloadClass {
		return 1
	}
	return 0
}

func compareTaskAssignments(left, right TaskAssignment) int {
	if cmp := compareWorkloadTasks(
		WorkloadTask{TaskID: left.TaskID, WorkloadID: left.WorkloadID, WorkloadType: left.WorkloadType, ZoneID: left.ZoneID, ShardID: left.ShardID, WorkloadClass: left.WorkloadClass},
		WorkloadTask{TaskID: right.TaskID, WorkloadID: right.WorkloadID, WorkloadType: right.WorkloadType, ZoneID: right.ZoneID, ShardID: right.ShardID, WorkloadClass: right.WorkloadClass},
	); cmp != 0 {
		return cmp
	}
	if left.Role < right.Role {
		return -1
	}
	if left.Role > right.Role {
		return 1
	}
	return 0
}

func compareTaskGroups(left, right TaskGroup) int {
	if left.EpochID < right.EpochID {
		return -1
	}
	if left.EpochID > right.EpochID {
		return 1
	}
	if left.WorkloadID < right.WorkloadID {
		return -1
	}
	if left.WorkloadID > right.WorkloadID {
		return 1
	}
	if left.WorkloadType < right.WorkloadType {
		return -1
	}
	if left.WorkloadType > right.WorkloadType {
		return 1
	}
	if left.TaskGroupID < right.TaskGroupID {
		return -1
	}
	if left.TaskGroupID > right.TaskGroupID {
		return 1
	}
	return 0
}
