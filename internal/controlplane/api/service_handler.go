package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/common/operationhistory"
	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
	controlservice "mini-cloud/internal/controlplane/service"
	servicecontroller "mini-cloud/internal/controlplane/servicecontroller"
	"mini-cloud/internal/controlplane/store"
)

type projectServiceCurrentRevision struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type projectServiceRevisionPolicy struct {
	Strategy string `json:"strategy"`
}

type projectServiceSpec struct {
	Provider             string                       `json:"provider"`
	Region               string                       `json:"region"`
	PinnedPlaneID        string                       `json:"pinnedPlaneID,omitempty"`
	Replicas             int                          `json:"replicas"`
	InstanceClass        string                       `json:"instanceClass"`
	RevisionPolicy       projectServiceRevisionPolicy `json:"revisionPolicy"`
	Exposure             string                       `json:"exposure"`
	Image                string                       `json:"image"`
	Command              []string                     `json:"command,omitempty"`
	Args                 []string                     `json:"args,omitempty"`
	DefaultPort          int                          `json:"defaultPort"`
	ReadinessPath        string                       `json:"readinessPath"`
	Env                  map[string]string            `json:"env,omitempty"`
	ConfigSetID          string                       `json:"configSetID,omitempty"`
	SecretSetID          string                       `json:"secretSetID,omitempty"`
	RegistryCredentialID string                       `json:"registryCredentialID,omitempty"`
	ProjectedFiles       []projectedfile.Spec         `json:"projectedFiles,omitempty"`
	PersistentDirs       []persistentdir.Spec         `json:"persistentDirs,omitempty"`
}

type projectServiceCondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	ObservedGeneration int64  `json:"observedGeneration"`
	LastTransitionAt   string `json:"lastTransitionAt"`
}

type projectServiceRolloutStatus struct {
	Phase                      string                         `json:"phase"`
	Message                    string                         `json:"message,omitempty"`
	StableRevisionID           string                         `json:"stableRevisionID,omitempty"`
	CandidateRevisionID        string                         `json:"candidateRevisionID,omitempty"`
	StableDesiredReplicas      int                            `json:"stableDesiredReplicas"`
	StableReadyReplicas        int                            `json:"stableReadyReplicas"`
	StableAvailableReplicas    int                            `json:"stableAvailableReplicas"`
	CandidateDesiredReplicas   int                            `json:"candidateDesiredReplicas"`
	CandidateReadyReplicas     int                            `json:"candidateReadyReplicas"`
	CandidateAvailableReplicas int                            `json:"candidateAvailableReplicas"`
	LastObservedAt             string                         `json:"lastObservedAt,omitempty"`
	StableRevision             *projectServiceCurrentRevision `json:"stableRevision,omitempty"`
	CandidateRevision          *projectServiceCurrentRevision `json:"candidateRevision,omitempty"`
}

type projectServiceStatus struct {
	ObservedGeneration int64                          `json:"observedGeneration"`
	DesiredState       string                         `json:"desiredState"`
	Phase              string                         `json:"phase"`
	Healthy            bool                           `json:"healthy"`
	Message            string                         `json:"message,omitempty"`
	Conditions         []projectServiceCondition      `json:"conditions,omitempty"`
	LastReconciledAt   *time.Time                     `json:"lastReconciledAt,omitempty"`
	CurrentRevision    *projectServiceCurrentRevision `json:"currentRevision,omitempty"`
	Rollout            projectServiceRolloutStatus    `json:"rollout"`
	Placement          *projectServicePlacement       `json:"placement,omitempty"`
}

type projectServicePlacement struct {
	PlaneID       string `json:"planeID"`
	RemoteStatus  string `json:"remoteStatus,omitempty"`
	RemoteHealthy bool   `json:"remoteHealthy"`
	RemoteMessage string `json:"remoteMessage,omitempty"`
}

type projectServiceMetadata struct {
	ID          string `json:"id"`
	ProjectID   string `json:"projectID"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Generation  int64  `json:"generation"`
}

type projectServiceResource struct {
	Metadata projectServiceMetadata `json:"metadata"`
	Spec     projectServiceSpec     `json:"spec"`
	Status   projectServiceStatus   `json:"status"`
}

type projectServiceEnvelope struct {
	Service projectServiceResource `json:"service"`
}

type projectServiceSpecInput struct {
	Provider             string                       `json:"provider"`
	Region               string                       `json:"region"`
	PinnedPlaneID        string                       `json:"pinnedPlaneID,omitempty"`
	Replicas             int                          `json:"replicas"`
	InstanceClass        string                       `json:"instanceClass"`
	RevisionPolicy       projectServiceRevisionPolicy `json:"revisionPolicy"`
	Exposure             string                       `json:"exposure"`
	Image                string                       `json:"image"`
	Command              []string                     `json:"command"`
	Args                 []string                     `json:"args"`
	DefaultPort          int                          `json:"defaultPort"`
	ReadinessPath        string                       `json:"readinessPath"`
	Env                  map[string]string            `json:"env"`
	ConfigSetID          string                       `json:"configSetID"`
	SecretSetID          string                       `json:"secretSetID"`
	RegistryCredentialID string                       `json:"registryCredentialID"`
	ProjectedFiles       []projectedfile.Spec         `json:"projectedFiles"`
	PersistentDirs       []persistentdir.Spec         `json:"persistentDirs"`
}

type projectServiceCreateRequest struct {
	Name        string                   `json:"name"`
	DisplayName string                   `json:"displayName"`
	Spec        *projectServiceSpecInput `json:"spec"`
}

type projectServiceUpdateRequest struct {
	DisplayName string                   `json:"displayName"`
	Spec        *projectServiceSpecInput `json:"spec"`
}

var errProjectServiceSpecRequired = errors.New("spec is required")

type serviceHandler struct {
	logger   *slog.Logger
	store    *store.Store
	services *servicecontroller.Controller
}

func newServiceHandler(logger *slog.Logger, stores *store.Store, services *servicecontroller.Controller) serviceHandler {
	return serviceHandler{
		logger:   logger,
		store:    stores,
		services: services,
	}
}

func (h serviceHandler) listProjectServices(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("projectID"))
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID is required"})
		return
	}
	items, err := h.services.ListByProject(r.Context(), projectID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrProjectNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		default:
			requestScopedLogger(r, h.logger).Error("list project services failed", "project_id", projectID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}
	out := make([]projectServiceEnvelope, 0, len(items))
	for _, item := range items {
		out = append(out, projectServiceEnvelope{
			Service: buildProjectServiceResource(item),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h serviceHandler) createProjectService(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("projectID"))
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID is required"})
		return
	}

	var request projectServiceCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	input, err := request.toCreateInput()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{ProjectID: projectID})
	logger := requestScopedLogger(r, h.logger)

	view, err := h.services.Create(r.Context(), projectID, input)
	if err != nil {
		switch {
		case isServiceInputError(err):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrProjectNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrProjectConfigSetNotFound),
			errors.Is(err, store.ErrProjectSecretSetNotFound),
			errors.Is(err, store.ErrProjectRegistryCredentialNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrServiceNameAlreadyExists):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("create project service failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}
	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		ProjectID:  projectID,
		Action:     "control.service.create",
		TargetType: "service",
		TargetID:   view.Service.Metadata.ID,
		TargetName: view.Service.Metadata.Name,
		Details:    buildServiceOperationDetails(view),
	})

	writeJSON(w, http.StatusCreated, projectServiceEnvelope{
		Service: buildProjectServiceResource(view),
	})
}

func (h serviceHandler) getProjectService(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("projectID"))
	serviceID := strings.TrimSpace(r.PathValue("serviceID"))
	if projectID == "" || serviceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID and serviceID are required"})
		return
	}
	view, err := h.services.Get(r.Context(), projectID, serviceID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrServiceNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		default:
			requestScopedLogger(r, h.logger).Error("get project service failed", "service_id", serviceID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}
	writeJSON(w, http.StatusOK, projectServiceEnvelope{
		Service: buildProjectServiceResource(view),
	})
}

func (h serviceHandler) updateProjectService(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("projectID"))
	serviceID := strings.TrimSpace(r.PathValue("serviceID"))
	if projectID == "" || serviceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID and serviceID are required"})
		return
	}
	var request projectServiceUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	input, err := request.toUpdateInput()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{ProjectID: projectID})
	logger := requestScopedLogger(r, h.logger)

	view, err := h.services.Update(r.Context(), projectID, serviceID, input)
	if err != nil {
		switch {
		case isServiceInputError(err):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrServiceNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrProjectConfigSetNotFound),
			errors.Is(err, store.ErrProjectSecretSetNotFound),
			errors.Is(err, store.ErrProjectRegistryCredentialNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("update project service failed", "service_id", serviceID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}
	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		ProjectID:  projectID,
		Action:     "control.service.update",
		TargetType: "service",
		TargetID:   view.Service.Metadata.ID,
		TargetName: view.Service.Metadata.Name,
		Details:    buildServiceOperationDetails(view),
	})

	writeJSON(w, http.StatusOK, projectServiceEnvelope{
		Service: buildProjectServiceResource(view),
	})
}

func (h serviceHandler) deleteProjectService(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("projectID"))
	serviceID := strings.TrimSpace(r.PathValue("serviceID"))
	if projectID == "" || serviceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "projectID and serviceID are required"})
		return
	}
	r = withRequestLogFields(r, logctx.Fields{ProjectID: projectID})
	logger := requestScopedLogger(r, h.logger)

	view, err := h.services.Delete(r.Context(), projectID, serviceID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrServiceNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		default:
			logger.Error("delete project service failed", "service_id", serviceID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}
	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		ProjectID:  projectID,
		Action:     "control.service.delete",
		TargetType: "service",
		TargetID:   view.Service.Metadata.ID,
		TargetName: view.Service.Metadata.Name,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"deleted":   true,
		"serviceID": serviceID,
	})
}

func buildProjectServiceResource(view servicecontroller.View) projectServiceResource {
	status := buildProjectServiceStatus(view)
	return projectServiceResource{
		Metadata: projectServiceMetadata{
			ID:          view.Service.Metadata.ID,
			ProjectID:   view.Service.Metadata.ProjectID,
			Name:        view.Service.Metadata.Name,
			DisplayName: view.Service.Metadata.DisplayName,
			Generation:  view.Service.Metadata.Generation,
		},
		Spec: projectServiceSpec{
			Provider:      view.Service.Spec.Provider,
			Region:        view.Service.Spec.Region,
			PinnedPlaneID: view.Service.Spec.PinnedPlaneID,
			Replicas:      view.Service.Spec.Replicas,
			InstanceClass: view.Service.Spec.InstanceClass,
			RevisionPolicy: projectServiceRevisionPolicy{
				Strategy: string(view.Service.Spec.RevisionPolicy.Strategy),
			},
			Exposure:             view.Service.Spec.Exposure,
			Image:                view.Service.Spec.Image,
			Command:              append([]string(nil), view.Service.Spec.Command...),
			Args:                 append([]string(nil), view.Service.Spec.Args...),
			DefaultPort:          view.Service.Spec.DefaultPort,
			ReadinessPath:        view.Service.Spec.ReadinessPath,
			Env:                  cloneStringMap(view.Service.Spec.Env),
			ConfigSetID:          view.Service.Spec.ConfigSetID,
			SecretSetID:          view.Service.Spec.SecretSetID,
			RegistryCredentialID: view.Service.Spec.RegistryCredentialID,
			ProjectedFiles:       projectedfile.CloneSpecs(view.Service.Spec.ProjectedFiles),
			PersistentDirs:       persistentdir.CloneSpecs(view.Service.Spec.PersistentDirs),
		},
		Status: status,
	}
}

func buildProjectServiceStatus(view servicecontroller.View) projectServiceStatus {
	serviceItem := view.Service
	status := projectServiceStatus{
		ObservedGeneration: serviceItem.Status.Observed.ObservedGeneration,
		DesiredState:       string(serviceItem.Status.DesiredState),
		Phase:              serviceItem.Status.Observed.Phase,
		Healthy:            serviceItem.Status.Observed.Healthy,
		Message:            serviceItem.Status.Observed.Message,
		Conditions:         buildProjectServiceConditions(serviceItem.Status.Observed.Conditions),
		LastReconciledAt:   serviceItem.Status.Observed.LastReconciledAt,
		Rollout:            buildProjectServiceRollout(serviceItem.Status.Rollout),
	}
	currentRevisionID := strings.TrimSpace(serviceItem.Status.Rollout.StableRevisionID)
	if currentRevisionID != "" {
		status.CurrentRevision = &projectServiceCurrentRevision{
			ID:    currentRevisionID,
			Label: currentRevisionID,
		}
	}
	if view.Placement != nil {
		status.Placement = &projectServicePlacement{
			PlaneID:       view.Placement.PlaneID,
			RemoteStatus:  view.Placement.RemoteStatus,
			RemoteHealthy: view.Placement.RemoteHealthy,
			RemoteMessage: view.Placement.RemoteMessage,
		}
	}
	return status
}

func buildProjectServiceRollout(input controlservice.RolloutStatus) projectServiceRolloutStatus {
	out := projectServiceRolloutStatus{
		Phase:                      input.Phase,
		Message:                    input.Message,
		StableRevisionID:           input.StableRevisionID,
		CandidateRevisionID:        input.CandidateRevisionID,
		StableDesiredReplicas:      input.StableDesiredReplicas,
		StableReadyReplicas:        input.StableReadyReplicas,
		StableAvailableReplicas:    input.StableAvailableReplicas,
		CandidateDesiredReplicas:   input.CandidateDesiredReplicas,
		CandidateReadyReplicas:     input.CandidateReadyReplicas,
		CandidateAvailableReplicas: input.CandidateAvailableReplicas,
	}
	if input.LastObservedAt != nil {
		out.LastObservedAt = input.LastObservedAt.UTC().Format(time.RFC3339)
	}
	if strings.TrimSpace(input.StableRevisionID) != "" {
		out.StableRevision = &projectServiceCurrentRevision{
			ID:    input.StableRevisionID,
			Label: input.StableRevisionID,
		}
	}
	if strings.TrimSpace(input.CandidateRevisionID) != "" {
		out.CandidateRevision = &projectServiceCurrentRevision{
			ID:    input.CandidateRevisionID,
			Label: input.CandidateRevisionID,
		}
	}
	return out
}

func buildProjectServiceConditions(input []controlservice.Condition) []projectServiceCondition {
	if len(input) == 0 {
		return nil
	}
	out := make([]projectServiceCondition, 0, len(input))
	for _, item := range input {
		out = append(out, projectServiceCondition{
			Type:               item.Type,
			Status:             string(item.Status),
			Reason:             item.Reason,
			Message:            item.Message,
			ObservedGeneration: item.ObservedGeneration,
			LastTransitionAt:   item.LastTransitionAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

func buildServiceOperationDetails(view servicecontroller.View) map[string]any {
	details := map[string]any{
		"provider":      view.Service.Spec.Provider,
		"region":        view.Service.Spec.Region,
		"replicas":      view.Service.Spec.Replicas,
		"instanceClass": string(view.Service.Spec.InstanceClass),
	}
	if view.Placement != nil {
		details["planeID"] = view.Placement.PlaneID
	}
	return details
}

func (r projectServiceCreateRequest) toCreateInput() (controlservice.CreateInput, error) {
	if r.Spec == nil {
		return controlservice.CreateInput{}, errProjectServiceSpecRequired
	}
	spec := *r.Spec
	return controlservice.CreateInput{
		Name:        strings.TrimSpace(r.Name),
		DisplayName: strings.TrimSpace(r.DisplayName),
		Spec: controlservice.Spec{
			Provider:             strings.TrimSpace(spec.Provider),
			Region:               strings.TrimSpace(spec.Region),
			PinnedPlaneID:        strings.TrimSpace(spec.PinnedPlaneID),
			Replicas:             spec.Replicas,
			InstanceClass:        strings.TrimSpace(spec.InstanceClass),
			Exposure:             strings.TrimSpace(spec.Exposure),
			Image:                strings.TrimSpace(spec.Image),
			Command:              append([]string(nil), spec.Command...),
			Args:                 append([]string(nil), spec.Args...),
			DefaultPort:          spec.DefaultPort,
			ReadinessPath:        strings.TrimSpace(spec.ReadinessPath),
			Env:                  cloneStringMap(spec.Env),
			ConfigSetID:          strings.TrimSpace(spec.ConfigSetID),
			SecretSetID:          strings.TrimSpace(spec.SecretSetID),
			RegistryCredentialID: strings.TrimSpace(spec.RegistryCredentialID),
			ProjectedFiles:       projectedfile.CloneSpecs(spec.ProjectedFiles),
			PersistentDirs:       persistentdir.CloneSpecs(spec.PersistentDirs),
			RevisionPolicy: controlservice.RevisionPolicy{
				Strategy: strings.TrimSpace(spec.RevisionPolicy.Strategy),
			},
		},
	}, nil
}

func (r projectServiceUpdateRequest) toUpdateInput() (controlservice.UpdateInput, error) {
	if r.Spec == nil {
		return controlservice.UpdateInput{}, errProjectServiceSpecRequired
	}
	spec := *r.Spec
	return controlservice.UpdateInput{
		DisplayName: strings.TrimSpace(r.DisplayName),
		Spec: controlservice.Spec{
			Provider:             strings.TrimSpace(spec.Provider),
			Region:               strings.TrimSpace(spec.Region),
			PinnedPlaneID:        strings.TrimSpace(spec.PinnedPlaneID),
			Replicas:             spec.Replicas,
			InstanceClass:        strings.TrimSpace(spec.InstanceClass),
			Exposure:             strings.TrimSpace(spec.Exposure),
			Image:                strings.TrimSpace(spec.Image),
			Command:              append([]string(nil), spec.Command...),
			Args:                 append([]string(nil), spec.Args...),
			DefaultPort:          spec.DefaultPort,
			ReadinessPath:        strings.TrimSpace(spec.ReadinessPath),
			Env:                  cloneStringMap(spec.Env),
			ConfigSetID:          strings.TrimSpace(spec.ConfigSetID),
			SecretSetID:          strings.TrimSpace(spec.SecretSetID),
			RegistryCredentialID: strings.TrimSpace(spec.RegistryCredentialID),
			ProjectedFiles:       projectedfile.CloneSpecs(spec.ProjectedFiles),
			PersistentDirs:       persistentdir.CloneSpecs(spec.PersistentDirs),
			RevisionPolicy: controlservice.RevisionPolicy{
				Strategy: strings.TrimSpace(spec.RevisionPolicy.Strategy),
			},
		},
	}, nil
}

func isServiceInputError(err error) bool {
	return errors.Is(err, errProjectServiceSpecRequired) ||
		errors.Is(err, controlservice.ErrProjectIDRequired) ||
		errors.Is(err, controlservice.ErrServiceNameRequired) ||
		errors.Is(err, controlservice.ErrInvalidServiceName) ||
		errors.Is(err, controlservice.ErrDisplayNameRequired) ||
		errors.Is(err, controlservice.ErrProviderRequired) ||
		errors.Is(err, controlservice.ErrRegionRequired) ||
		errors.Is(err, controlservice.ErrPinnedPlaneIDInvalid) ||
		errors.Is(err, controlservice.ErrInvalidReplicas) ||
		errors.Is(err, controlservice.ErrInvalidInstanceClass) ||
		errors.Is(err, controlservice.ErrInvalidInstanceClass) ||
		errors.Is(err, controlservice.ErrInvalidExposure) ||
		errors.Is(err, controlservice.ErrImageRequired) ||
		errors.Is(err, controlservice.ErrInvalidDefaultPort) ||
		errors.Is(err, controlservice.ErrInvalidReadinessPath) ||
		errors.Is(err, controlservice.ErrInvalidEnvironmentKey) ||
		errors.Is(err, controlservice.ErrPersistentDirsReplicaLimit) ||
		errors.Is(err, controlservice.ErrPersistentDirsRolloutUnsupported) ||
		errors.Is(err, controlservice.ErrPersistentDirsPlacementChangeUnsupported) ||
		errors.Is(err, projectedfile.ErrMountPathRequired) ||
		errors.Is(err, projectedfile.ErrMountPathAbsolute) ||
		errors.Is(err, projectedfile.ErrMountPathInvalid) ||
		errors.Is(err, projectedfile.ErrSourceKindInvalid) ||
		errors.Is(err, projectedfile.ErrSourceIDRequired) ||
		errors.Is(err, projectedfile.ErrSourceKeyRequired) ||
		errors.Is(err, projectedfile.ErrDuplicateMountPath) ||
		errors.Is(err, persistentdir.ErrNameRequired) ||
		errors.Is(err, persistentdir.ErrInvalidName) ||
		errors.Is(err, persistentdir.ErrMountPathRequired) ||
		errors.Is(err, persistentdir.ErrMountPathAbsolute) ||
		errors.Is(err, persistentdir.ErrMountPathInvalid) ||
		errors.Is(err, persistentdir.ErrDuplicateName) ||
		errors.Is(err, persistentdir.ErrDuplicateMountPath) ||
		errors.Is(err, persistentdir.ErrNestedMountPath) ||
		errors.Is(err, persistentdir.ErrProjectedConflict)
}
