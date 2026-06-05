package service

import (
	"errors"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
)

const (
	InstanceClassSmall  = "small"
	InstanceClassMedium = "medium"
	InstanceClassLarge  = "large"
)

const (
	CellRolePrimary = "primary"
	CellRoleStandby = "standby"

	DesiredStateActive  = "active"
	DesiredStateDeleted = "deleted"

	PhasePending     = "pending"
	PhaseProgressing = "progressing"
	PhaseReady       = "ready"
	PhaseDegraded    = "degraded"
	PhaseDeleting    = "deleting"

	ConditionTrue  = "True"
	ConditionFalse = "False"
)

var (
	ErrServiceNameRequired                      = errors.New("name is required")
	ErrInvalidServiceName                       = errors.New("name must use lowercase letters, digits, and hyphens")
	ErrDisplayNameRequired                      = errors.New("displayName is required")
	ErrInvalidExposure                          = errors.New("exposure must be one of public, private")
	ErrImageRequired                            = errors.New("image is required")
	ErrInvalidDefaultPort                       = errors.New("defaultPort must be between 1 and 65535")
	ErrInvalidReadinessPath                     = errors.New("readinessPath must start with /")
	ErrInvalidEnvironmentKey                    = errors.New("env keys must not be empty")
	ErrPinnedPlaneIDInvalid                     = errors.New("pinnedPlaneID must not be blank when provided")
	ErrCellsRequired                            = errors.New("cells must contain at least one item")
	ErrCellKeyRequired                          = errors.New("cell key is required")
	ErrInvalidCellKey                           = errors.New("cell key must use lowercase letters, digits, and hyphens")
	ErrDuplicateCellKey                         = errors.New("cell keys must be unique within a service")
	ErrInvalidCellRole                          = errors.New("cell role must be one of primary, standby")
	ErrPrimaryCellRequired                      = errors.New("cells must contain exactly one primary cell")
	ErrMultiplePrimaryCells                     = errors.New("cells must contain exactly one primary cell")
	ErrProviderRequired                         = errors.New("provider is required")
	ErrRegionRequired                           = errors.New("region is required")
	ErrInvalidInstanceClass                     = errors.New("instanceClass must be one of small, medium, large")
	ErrPersistentDirsRunUpdateUnsupported       = errors.New("services with persistentDirs do not support run-changing updates once locked")
	ErrPersistentDirsPlacementChangeUnsupported = errors.New("services with persistentDirs do not support placement-changing updates once a run exists")
	serviceNamePattern                          = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

const (
	ConditionPlacementReady = "PlacementReady"
	ConditionApplied        = "Applied"
	ConditionReady          = "Ready"
)

const (
	ReasonPendingCreate          = "PendingCreate"
	ReasonSpecUpdated            = "SpecUpdated"
	ReasonDeletionRequested      = "DeletionRequested"
	ReasonNoPlacement            = "NoPlacement"
	ReasonNoEligiblePlacement    = "NoEligiblePlacement"
	ReasonApplyFailed            = "ApplyFailed"
	ReasonApplied                = "Applied"
	ReasonPlaneServiceNotHealthy = "PlaneServiceNotHealthy"
	ReasonPlaneServiceReady      = "PlaneServiceReady"
	ReasonPlaneServiceMissing    = "PlaneServiceMissing"
	ReasonObservationFailed      = "ObservationFailed"
	ReasonReconcileFailed        = "ReconcileFailed"
)

type Service struct {
	Metadata  Metadata      `json:"metadata"`
	Spec      Spec          `json:"spec"`
	Status    ServiceStatus `json:"status"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

type Metadata struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Generation  int64  `json:"generation"`
}

type Spec struct {
	Provider             string               `json:"provider"`
	Region               string               `json:"region"`
	PinnedPlaneID        string               `json:"pinnedPlaneID,omitempty"`
	InstanceClass        string               `json:"instanceClass"`
	Exposure             string               `json:"exposure"`
	Image                string               `json:"image"`
	Command              []string             `json:"command"`
	Args                 []string             `json:"args"`
	DefaultPort          int                  `json:"defaultPort"`
	ReadinessPath        string               `json:"readinessPath"`
	Env                  map[string]string    `json:"env"`
	ConfigSetID          string               `json:"configSetID"`
	SecretSetID          string               `json:"secretSetID"`
	RegistryCredentialID string               `json:"registryCredentialID"`
	ProjectedFiles       []projectedfile.Spec `json:"projectedFiles,omitempty"`
	PersistentDirs       []persistentdir.Spec `json:"persistentDirs,omitempty"`
	PersistentDirsLocked bool                 `json:"-"`
}

type ServiceStatus struct {
	DesiredState string    `json:"desiredState"`
	Observed     Status    `json:"observed"`
	Run          RunStatus `json:"run"`
}

type ServiceRun struct {
	ID         string     `json:"id"`
	ServiceID  string     `json:"serviceID"`
	Generation int64      `json:"generation"`
	PlanID     string     `json:"planID"`
	Spec       Spec       `json:"spec"`
	Status     string     `json:"status"`
	Message    string     `json:"message,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	ObservedAt *time.Time `json:"observedAt,omitempty"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

type ServicePlacement struct {
	ServiceID     string    `json:"serviceID"`
	PlaneID       string    `json:"planeID"`
	RemoteStatus  string    `json:"remoteStatus"`
	RemoteHealthy bool      `json:"remoteHealthy"`
	RemoteMessage string    `json:"remoteMessage"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type Cell struct {
	ServiceID     string    `json:"serviceID"`
	Key           string    `json:"key"`
	Role          string    `json:"role"`
	Provider      string    `json:"provider"`
	Region        string    `json:"region"`
	PinnedPlaneID string    `json:"pinnedPlaneID,omitempty"`
	InstanceClass string    `json:"instanceClass"`
	DesiredState  string    `json:"desiredState"`
	Status        Status    `json:"status"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type CellPlacement struct {
	ServiceID     string    `json:"serviceID"`
	CellKey       string    `json:"cellKey"`
	PlaneID       string    `json:"planeID"`
	RemoteStatus  string    `json:"remoteStatus"`
	RemoteHealthy bool      `json:"remoteHealthy"`
	RemoteMessage string    `json:"remoteMessage"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

const (
	CellAssignmentStatePlanned     = "planned"
	CellAssignmentStateDispatching = "dispatching"
	CellAssignmentStateApplied     = "applied"
	CellAssignmentStateReleasing   = "releasing"
	CellAssignmentStateFailed      = "failed"
)

type CellAssignment struct {
	ServiceID        string    `json:"serviceID"`
	CellKey          string    `json:"cellKey"`
	PlaneID          string    `json:"planeID"`
	TargetNodeID     string    `json:"targetNodeID"`
	TargetNodeEpoch  int64     `json:"targetNodeEpoch"`
	InventoryVersion int64     `json:"inventoryVersion"`
	CPUMilliReserved int       `json:"cpuMilliReserved"`
	MemoryMiReserved int       `json:"memoryMiReserved"`
	State            string    `json:"state"`
	LastError        string    `json:"lastError,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type Status struct {
	ObservedGeneration int64       `json:"observedGeneration"`
	Phase              string      `json:"phase"`
	Healthy            bool        `json:"healthy"`
	Message            string      `json:"message,omitempty"`
	Conditions         []Condition `json:"conditions,omitempty"`
	LastReconciledAt   *time.Time  `json:"lastReconciledAt,omitempty"`
}

type Condition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
	ObservedGeneration int64     `json:"observedGeneration"`
	LastTransitionAt   time.Time `json:"lastTransitionAt"`
}

type UpdateStatusInput struct {
	ObservedGeneration int64
	Phase              string
	Healthy            bool
	Message            string
	Conditions         []Condition
	LastReconciledAt   *time.Time
	Run                *RunStatus
}

type CreateRunInput struct {
	ID         string
	ServiceID  string
	Generation int64
	PlanID     string
	Spec       Spec
	Status     string
	Message    string
	ObservedAt *time.Time
}

type UpdateRunInput struct {
	Status     string
	Message    string
	ObservedAt *time.Time
}

type CellInput struct {
	Key           string `json:"key"`
	Role          string `json:"role"`
	Provider      string `json:"provider"`
	Region        string `json:"region"`
	PinnedPlaneID string `json:"pinnedPlaneID,omitempty"`
	InstanceClass string `json:"instanceClass"`
}

const (
	RunPhasePending     = "pending"
	RunPhaseDispatching = "dispatching"
	RunPhaseRunning     = "running"
	RunPhaseFailed      = "failed"
	RunPhaseSuperseded  = "superseded"
)

type RunStatus struct {
	CurrentRunID   string     `json:"currentRunID,omitempty"`
	LatestRunID    string     `json:"latestRunID,omitempty"`
	Phase          string     `json:"phase"`
	Message        string     `json:"message,omitempty"`
	LastObservedAt *time.Time `json:"lastObservedAt,omitempty"`
}

func CloneRunStatus(input RunStatus) RunStatus {
	out := input
	if input.LastObservedAt != nil {
		value := input.LastObservedAt.UTC()
		out.LastObservedAt = &value
	}
	return out
}

type CreateInput struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Spec        Spec   `json:"spec"`
}

type UpdateInput struct {
	DisplayName string `json:"displayName"`
	Spec        Spec   `json:"spec"`
}

func (in CreateInput) Validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return ErrServiceNameRequired
	}
	if !serviceNamePattern.MatchString(strings.TrimSpace(in.Name)) {
		return ErrInvalidServiceName
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return ErrDisplayNameRequired
	}
	return in.Spec.Validate()
}

func (in UpdateInput) Validate(serviceName string) error {
	if strings.TrimSpace(serviceName) == "" {
		return ErrServiceNameRequired
	}
	if !serviceNamePattern.MatchString(strings.TrimSpace(serviceName)) {
		return ErrInvalidServiceName
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		return ErrDisplayNameRequired
	}
	return in.Spec.Validate()
}

func (s Service) UpdateInput() UpdateInput {
	return UpdateInput{
		DisplayName: s.Metadata.DisplayName,
		Spec:        CloneSpec(s.Spec),
	}
}

func ValidatePersistentDirUpdate(current Service, input UpdateInput) error {
	if persistentDirPlacementChangeBlocked(current, input) {
		return ErrPersistentDirsPlacementChangeUnsupported
	}
	if persistentDirRunChangeBlocked(current, input) {
		return ErrPersistentDirsRunUpdateUnsupported
	}
	return nil
}

func PendingStatus(observedGeneration int64, now time.Time, reason string, message string) Status {
	return Status{
		ObservedGeneration: observedGeneration,
		Phase:              PhasePending,
		Healthy:            false,
		Message:            message,
		Conditions: []Condition{
			NewCondition(ConditionPlacementReady, ConditionFalse, reason, message, observedGeneration, now),
			NewCondition(ConditionApplied, ConditionFalse, reason, message, observedGeneration, now),
			NewCondition(ConditionReady, ConditionFalse, reason, message, observedGeneration, now),
		},
	}
}

func DeletingStatus(observedGeneration int64, now time.Time, message string) Status {
	return Status{
		ObservedGeneration: observedGeneration,
		Phase:              PhaseDeleting,
		Healthy:            false,
		Message:            message,
		Conditions: []Condition{
			NewCondition(ConditionPlacementReady, ConditionFalse, ReasonDeletionRequested, message, observedGeneration, now),
			NewCondition(ConditionApplied, ConditionFalse, ReasonDeletionRequested, message, observedGeneration, now),
			NewCondition(ConditionReady, ConditionFalse, ReasonDeletionRequested, message, observedGeneration, now),
		},
	}
}

func NewCondition(conditionType string, status string, reason string, message string, generation int64, observedAt time.Time) Condition {
	return Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
		LastTransitionAt:   observedAt,
	}
}

func CloneConditions(input []Condition) []Condition {
	if len(input) == 0 {
		return nil
	}
	out := make([]Condition, len(input))
	copy(out, input)
	return out
}

func CloneCellInputs(input []CellInput) []CellInput {
	if len(input) == 0 {
		return nil
	}
	out := make([]CellInput, len(input))
	copy(out, input)
	sort.Slice(out, func(i, j int) bool {
		left := cellSortRank(out[i].Role)
		right := cellSortRank(out[j].Role)
		if left != right {
			return left < right
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func SortCells(cells []Cell) {
	sort.Slice(cells, func(i, j int) bool {
		left := cellSortRank(cells[i].Role)
		right := cellSortRank(cells[j].Role)
		if left != right {
			return left < right
		}
		return cells[i].Key < cells[j].Key
	})
}

func PreferredCell(cells []Cell) *Cell {
	if len(cells) == 0 {
		return nil
	}
	copyCells := append([]Cell(nil), cells...)
	SortCells(copyCells)
	for _, cell := range copyCells {
		if cell.DesiredState == DesiredStateActive {
			copyCell := cell
			return &copyCell
		}
	}
	return nil
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func CloneSpec(input Spec) Spec {
	return Spec{
		Provider:             input.Provider,
		Region:               input.Region,
		PinnedPlaneID:        input.PinnedPlaneID,
		InstanceClass:        input.InstanceClass,
		Exposure:             input.Exposure,
		Image:                input.Image,
		Command:              append([]string(nil), input.Command...),
		Args:                 append([]string(nil), input.Args...),
		DefaultPort:          input.DefaultPort,
		ReadinessPath:        input.ReadinessPath,
		Env:                  copyStringMap(input.Env),
		ConfigSetID:          input.ConfigSetID,
		SecretSetID:          input.SecretSetID,
		RegistryCredentialID: input.RegistryCredentialID,
		ProjectedFiles:       projectedfile.CloneSpecs(input.ProjectedFiles),
		PersistentDirs:       persistentdir.CloneSpecs(input.PersistentDirs),
		PersistentDirsLocked: input.PersistentDirsLocked,
	}
}

func (spec Spec) Validate() error {
	provider, region, pinnedPlaneID, instanceClass, err := ResolveServicePlacementFields(spec.Provider, spec.Region, spec.PinnedPlaneID, spec.InstanceClass)
	if err != nil {
		return err
	}
	resolvedExposure := strings.ToLower(strings.TrimSpace(spec.Exposure))
	if resolvedExposure == "" {
		resolvedExposure = "public"
	}
	if resolvedExposure != "public" && resolvedExposure != "private" {
		return ErrInvalidExposure
	}
	if strings.TrimSpace(spec.Image) == "" {
		return ErrImageRequired
	}
	if spec.DefaultPort <= 0 || spec.DefaultPort > 65535 {
		return ErrInvalidDefaultPort
	}
	if !strings.HasPrefix(strings.TrimSpace(spec.ReadinessPath), "/") {
		return ErrInvalidReadinessPath
	}
	for key := range spec.Env {
		if strings.TrimSpace(key) == "" {
			return ErrInvalidEnvironmentKey
		}
	}
	if err := projectedfile.ValidateSpecs(spec.ProjectedFiles); err != nil {
		return err
	}
	if err := persistentdir.ValidateContainerInputs(spec.PersistentDirs, spec.ProjectedFiles); err != nil {
		return err
	}
	_ = provider
	_ = region
	_ = pinnedPlaneID
	_ = instanceClass
	return nil
}

func persistentDirRunChangeBlocked(current Service, input UpdateInput) bool {
	if len(current.Spec.PersistentDirs) == 0 && len(input.Spec.PersistentDirs) == 0 {
		return false
	}
	if !current.Spec.PersistentDirsLocked && current.Status.Run.CurrentRunID == "" && current.Status.Run.LatestRunID == "" {
		return false
	}
	return serviceNeedsNewRun(current.Spec, input.Spec)
}

func persistentDirPlacementChangeBlocked(current Service, input UpdateInput) bool {
	if len(current.Spec.PersistentDirs) == 0 && len(input.Spec.PersistentDirs) == 0 {
		return false
	}
	if !current.Spec.PersistentDirsLocked && current.Status.Run.CurrentRunID == "" && current.Status.Run.LatestRunID == "" {
		return false
	}

	currentProvider, currentRegion, currentPinnedPlaneID, _, err := ResolveServicePlacementFields(
		current.Spec.Provider,
		current.Spec.Region,
		current.Spec.PinnedPlaneID,
		current.Spec.InstanceClass,
	)
	if err != nil {
		return false
	}
	nextProvider, nextRegion, nextPinnedPlaneID, _, err := ResolveServicePlacementFields(
		input.Spec.Provider,
		input.Spec.Region,
		input.Spec.PinnedPlaneID,
		input.Spec.InstanceClass,
	)
	if err != nil {
		return false
	}
	return currentProvider != nextProvider ||
		currentRegion != nextRegion ||
		currentPinnedPlaneID != nextPinnedPlaneID
}

func serviceNeedsNewRun(before Spec, after Spec) bool {
	return !SpecRuntimeEqual(before, after)
}

func SpecRuntimeEqual(before Spec, after Spec) bool {
	return before.Region == after.Region &&
		before.InstanceClass == after.InstanceClass &&
		before.Image == after.Image &&
		reflect.DeepEqual(before.Command, after.Command) &&
		reflect.DeepEqual(before.Args, after.Args) &&
		reflect.DeepEqual(before.Env, after.Env) &&
		before.DefaultPort == after.DefaultPort &&
		before.ReadinessPath == after.ReadinessPath &&
		before.ConfigSetID == after.ConfigSetID &&
		before.SecretSetID == after.SecretSetID &&
		before.RegistryCredentialID == after.RegistryCredentialID &&
		reflect.DeepEqual(projectedfile.CloneSpecs(before.ProjectedFiles), projectedfile.CloneSpecs(after.ProjectedFiles)) &&
		reflect.DeepEqual(persistentdir.CloneSpecs(before.PersistentDirs), persistentdir.CloneSpecs(after.PersistentDirs))
}

func ResolveServicePlacementFields(provider string, region string, pinnedPlaneID string, instanceClass string) (string, string, string, string, error) {
	if strings.TrimSpace(provider) == "" {
		return "", "", "", "", ErrProviderRequired
	}
	if strings.TrimSpace(region) == "" {
		return "", "", "", "", ErrRegionRequired
	}
	if strings.TrimSpace(pinnedPlaneID) == "" {
		pinnedPlaneID = ""
	}
	if pinnedPlaneID != "" && strings.TrimSpace(pinnedPlaneID) == "" {
		return "", "", "", "", ErrPinnedPlaneIDInvalid
	}
	if !IsInstanceClass(instanceClass) {
		return "", "", "", "", ErrInvalidInstanceClass
	}
	return strings.TrimSpace(provider), strings.TrimSpace(region), strings.TrimSpace(pinnedPlaneID), instanceClass, nil
}

func NormalizeRunPhase(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "":
		return RunPhasePending
	default:
		return strings.ToLower(strings.TrimSpace(phase))
	}
}

func cellSortRank(role string) int {
	switch role {
	case CellRolePrimary:
		return 0
	case CellRoleStandby:
		return 1
	default:
		return 2
	}
}

func IsInstanceClass(class string) bool {
	switch class {
	case InstanceClassSmall, InstanceClassMedium, InstanceClassLarge:
		return true
	default:
		return false
	}
}
