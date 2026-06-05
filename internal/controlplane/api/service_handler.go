package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"mini-cloud/internal/common/operationhistory"
	"mini-cloud/internal/common/projectedfile"
	controlservice "mini-cloud/internal/controlplane/service"
	servicecontroller "mini-cloud/internal/controlplane/servicecontroller"
	"mini-cloud/internal/controlplane/store"
)

type serviceSpec struct {
	Provider             string               `json:"provider"`
	Region               string               `json:"region"`
	PinnedPlaneID        string               `json:"pinnedPlaneID,omitempty"`
	InstanceClass        string               `json:"instanceClass"`
	Exposure             string               `json:"exposure"`
	Image                string               `json:"image"`
	Command              []string             `json:"command,omitempty"`
	Args                 []string             `json:"args,omitempty"`
	DefaultPort          int                  `json:"defaultPort"`
	ReadinessPath        string               `json:"readinessPath"`
	Env                  map[string]string    `json:"env,omitempty"`
	ConfigSetID          string               `json:"configSetID,omitempty"`
	SecretSetID          string               `json:"secretSetID,omitempty"`
	RegistryCredentialID string               `json:"registryCredentialID,omitempty"`
	ProjectedFiles       []projectedfile.Spec `json:"projectedFiles,omitempty"`
}

type serviceCondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	ObservedGeneration int64  `json:"observedGeneration"`
	LastTransitionAt   string `json:"lastTransitionAt"`
}

type serviceRunStatus struct {
	CurrentRunID   string `json:"currentRunID,omitempty"`
	LatestRunID    string `json:"latestRunID,omitempty"`
	Phase          string `json:"phase"`
	Message        string `json:"message,omitempty"`
	LastObservedAt string `json:"lastObservedAt,omitempty"`
}

type serviceStatus struct {
	ObservedGeneration int64              `json:"observedGeneration"`
	DesiredState       string             `json:"desiredState"`
	Phase              string             `json:"phase"`
	Healthy            bool               `json:"healthy"`
	Message            string             `json:"message,omitempty"`
	Conditions         []serviceCondition `json:"conditions,omitempty"`
	LastReconciledAt   *time.Time         `json:"lastReconciledAt,omitempty"`
	Run                serviceRunStatus   `json:"run"`
	AssignedPlaneID    string             `json:"assignedPlaneID,omitempty"`
	RemoteStatus       string             `json:"remoteStatus,omitempty"`
	RemoteMessage      string             `json:"remoteMessage,omitempty"`
}

type serviceMetadata struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Generation  int64  `json:"generation"`
}

type serviceResource struct {
	Metadata serviceMetadata `json:"metadata"`
	Spec     serviceSpec     `json:"spec"`
	Status   serviceStatus   `json:"status"`
}

type serviceEnvelope struct {
	Service serviceResource `json:"service"`
}

type serviceSpecInput struct {
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
	ProjectedFiles       []projectedfile.Spec `json:"projectedFiles"`
}

type serviceCreateRequest struct {
	Name        string            `json:"name"`
	DisplayName string            `json:"displayName"`
	Spec        *serviceSpecInput `json:"spec"`
}

type serviceUpdateRequest struct {
	DisplayName string            `json:"displayName"`
	Spec        *serviceSpecInput `json:"spec"`
}

var errServiceSpecRequired = errors.New("spec is required")

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

func (h serviceHandler) listServices(w http.ResponseWriter, r *http.Request) {
	items, err := h.services.List(r.Context())
	if err != nil {
		requestScopedLogger(r, h.logger).Error("list services failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}
	out := make([]serviceEnvelope, 0, len(items))
	for _, item := range items {
		out = append(out, serviceEnvelope{
			Service: buildServiceResource(item),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h serviceHandler) createService(w http.ResponseWriter, r *http.Request) {
	var request serviceCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	input, err := request.toCreateInput()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	logger := requestScopedLogger(r, h.logger)

	view, err := h.services.Create(r.Context(), input)
	if err != nil {
		switch {
		case isServiceInputError(err):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrConfigSetNotFound),
			errors.Is(err, store.ErrSecretSetNotFound),
			errors.Is(err, store.ErrRegistryCredentialNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrServiceNameAlreadyExists):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("create service failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}
	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.service.create",
		TargetType: "service",
		TargetID:   view.Service.Metadata.ID,
		TargetName: view.Service.Metadata.Name,
	})

	writeJSON(w, http.StatusCreated, serviceEnvelope{
		Service: buildServiceResource(view),
	})
}

func (h serviceHandler) getService(w http.ResponseWriter, r *http.Request) {
	serviceID := strings.TrimSpace(r.PathValue("serviceID"))
	if serviceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "serviceID is required"})
		return
	}
	view, err := h.services.Get(r.Context(), serviceID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrServiceNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		default:
			requestScopedLogger(r, h.logger).Error("get service failed", "service_id", serviceID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}
	writeJSON(w, http.StatusOK, serviceEnvelope{
		Service: buildServiceResource(view),
	})
}

func (h serviceHandler) updateService(w http.ResponseWriter, r *http.Request) {
	serviceID := strings.TrimSpace(r.PathValue("serviceID"))
	if serviceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "serviceID is required"})
		return
	}
	var request serviceUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	input, err := request.toUpdateInput()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	logger := requestScopedLogger(r, h.logger)

	view, err := h.services.Update(r.Context(), serviceID, input)
	if err != nil {
		switch {
		case isServiceInputError(err):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrServiceNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrConfigSetNotFound),
			errors.Is(err, store.ErrSecretSetNotFound),
			errors.Is(err, store.ErrRegistryCredentialNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("update service failed", "service_id", serviceID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}
	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.service.update",
		TargetType: "service",
		TargetID:   view.Service.Metadata.ID,
		TargetName: view.Service.Metadata.Name,
	})

	writeJSON(w, http.StatusOK, serviceEnvelope{
		Service: buildServiceResource(view),
	})
}

func (h serviceHandler) deleteService(w http.ResponseWriter, r *http.Request) {
	serviceID := strings.TrimSpace(r.PathValue("serviceID"))
	if serviceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "serviceID is required"})
		return
	}
	logger := requestScopedLogger(r, h.logger)

	view, err := h.services.Delete(r.Context(), serviceID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrServiceNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		default:
			logger.Error("delete service failed", "service_id", serviceID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}
	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
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

func buildServiceResource(view servicecontroller.View) serviceResource {
	status := buildServiceStatus(view)
	return serviceResource{
		Metadata: serviceMetadata{
			ID:          view.Service.Metadata.ID,
			Name:        view.Service.Metadata.Name,
			DisplayName: view.Service.Metadata.DisplayName,
			Generation:  view.Service.Metadata.Generation,
		},
		Spec: serviceSpec{
			Provider:             view.Service.Spec.Provider,
			Region:               view.Service.Spec.Region,
			PinnedPlaneID:        view.Service.Spec.PinnedPlaneID,
			InstanceClass:        view.Service.Spec.InstanceClass,
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
		},
		Status: status,
	}
}

func buildServiceStatus(view servicecontroller.View) serviceStatus {
	serviceItem := view.Service
	status := serviceStatus{
		ObservedGeneration: serviceItem.Status.Observed.ObservedGeneration,
		DesiredState:       string(serviceItem.Status.DesiredState),
		Phase:              serviceItem.Status.Observed.Phase,
		Healthy:            serviceItem.Status.Observed.Healthy,
		Message:            serviceItem.Status.Observed.Message,
		Conditions:         buildServiceConditions(serviceItem.Status.Observed.Conditions),
		LastReconciledAt:   serviceItem.Status.Observed.LastReconciledAt,
		Run:                buildServiceRun(serviceItem.Status.Run),
		AssignedPlaneID:    serviceItem.Status.Observed.AssignedPlaneID,
		RemoteStatus:       serviceItem.Status.Observed.RemoteStatus,
		RemoteMessage:      serviceItem.Status.Observed.RemoteMessage,
	}
	return status
}

func buildServiceRun(input controlservice.RunStatus) serviceRunStatus {
	out := serviceRunStatus{
		CurrentRunID: input.CurrentRunID,
		LatestRunID:  input.LatestRunID,
		Phase:        input.Phase,
		Message:      input.Message,
	}
	if input.LastObservedAt != nil {
		out.LastObservedAt = input.LastObservedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func buildServiceConditions(input []controlservice.Condition) []serviceCondition {
	if len(input) == 0 {
		return nil
	}
	out := make([]serviceCondition, 0, len(input))
	for _, item := range input {
		out = append(out, serviceCondition{
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

func (r serviceCreateRequest) toCreateInput() (controlservice.CreateInput, error) {
	if r.Spec == nil {
		return controlservice.CreateInput{}, errServiceSpecRequired
	}
	spec := *r.Spec
	return controlservice.CreateInput{
		Name:        strings.TrimSpace(r.Name),
		DisplayName: strings.TrimSpace(r.DisplayName),
		Spec: controlservice.Spec{
			Provider:             strings.TrimSpace(spec.Provider),
			Region:               strings.TrimSpace(spec.Region),
			PinnedPlaneID:        strings.TrimSpace(spec.PinnedPlaneID),
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
		},
	}, nil
}

func (r serviceUpdateRequest) toUpdateInput() (controlservice.UpdateInput, error) {
	if r.Spec == nil {
		return controlservice.UpdateInput{}, errServiceSpecRequired
	}
	spec := *r.Spec
	return controlservice.UpdateInput{
		DisplayName: strings.TrimSpace(r.DisplayName),
		Spec: controlservice.Spec{
			Provider:             strings.TrimSpace(spec.Provider),
			Region:               strings.TrimSpace(spec.Region),
			PinnedPlaneID:        strings.TrimSpace(spec.PinnedPlaneID),
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
		},
	}, nil
}

func isServiceInputError(err error) bool {
	return errors.Is(err, errServiceSpecRequired) ||
		errors.Is(err, controlservice.ErrServiceNameRequired) ||
		errors.Is(err, controlservice.ErrInvalidServiceName) ||
		errors.Is(err, controlservice.ErrDisplayNameRequired) ||
		errors.Is(err, controlservice.ErrProviderRequired) ||
		errors.Is(err, controlservice.ErrRegionRequired) ||
		errors.Is(err, controlservice.ErrPinnedPlaneIDInvalid) ||
		errors.Is(err, controlservice.ErrInvalidInstanceClass) ||
		errors.Is(err, controlservice.ErrInvalidExposure) ||
		errors.Is(err, controlservice.ErrImageRequired) ||
		errors.Is(err, controlservice.ErrInvalidDefaultPort) ||
		errors.Is(err, controlservice.ErrInvalidReadinessPath) ||
		errors.Is(err, controlservice.ErrInvalidEnvironmentKey) ||
		errors.Is(err, projectedfile.ErrMountPathRequired) ||
		errors.Is(err, projectedfile.ErrMountPathAbsolute) ||
		errors.Is(err, projectedfile.ErrMountPathInvalid) ||
		errors.Is(err, projectedfile.ErrSourceKindInvalid) ||
		errors.Is(err, projectedfile.ErrSourceIDRequired) ||
		errors.Is(err, projectedfile.ErrSourceKeyRequired) ||
		errors.Is(err, projectedfile.ErrDuplicateMountPath)
}
