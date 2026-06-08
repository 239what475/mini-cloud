package api

import (
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/common/projectedfile"
	domain "mini-cloud/internal/controlplane/domain"
	"mini-cloud/internal/controlplane/eventlog"
	"mini-cloud/internal/controlplane/serviceops"
	"mini-cloud/internal/controlplane/store"

	"github.com/gin-gonic/gin"
)

type serviceSpec struct {
	PlaneID              string               `json:"planeID"`
	InstanceClass        string               `json:"instanceClass"`
	Exposure             string               `json:"exposure"`
	Image                string               `json:"image"`
	Command              []string             `json:"command,omitempty"`
	Args                 []string             `json:"args,omitempty"`
	DefaultPort          int                  `json:"defaultPort"`
	ReadinessPath        string               `json:"readinessPath"`
	Env                  map[string]string    `json:"env,omitempty"`
	SecretEnvKeys        []string             `json:"secretEnvKeys,omitempty"`
	RegistryCredentialID string               `json:"registryCredentialID,omitempty"`
	Files                []projectedfile.File `json:"files,omitempty"`
}

type serviceRunStatus struct {
	CurrentRunID   string `json:"currentRunID,omitempty"`
	LatestRunID    string `json:"latestRunID,omitempty"`
	Phase          string `json:"phase"`
	Message        string `json:"message,omitempty"`
	LastObservedAt string `json:"lastObservedAt,omitempty"`
}

type serviceStatus struct {
	ObservedGeneration int64            `json:"observedGeneration"`
	DesiredState       string           `json:"desiredState"`
	Phase              string           `json:"phase"`
	Healthy            bool             `json:"healthy"`
	Message            string           `json:"message,omitempty"`
	LastReconciledAt   *time.Time       `json:"lastReconciledAt,omitempty"`
	Run                serviceRunStatus `json:"run"`
	AssignedPlaneID    string           `json:"assignedPlaneID,omitempty"`
	RemoteStatus       string           `json:"remoteStatus,omitempty"`
	RemoteMessage      string           `json:"remoteMessage,omitempty"`
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
	PlaneID              string               `json:"planeID"`
	InstanceClass        string               `json:"instanceClass"`
	Exposure             string               `json:"exposure"`
	Image                string               `json:"image"`
	Command              []string             `json:"command"`
	Args                 []string             `json:"args"`
	DefaultPort          int                  `json:"defaultPort"`
	ReadinessPath        string               `json:"readinessPath"`
	Env                  map[string]string    `json:"env"`
	SecretEnv            map[string]string    `json:"secretEnv"`
	RegistryCredentialID string               `json:"registryCredentialID"`
	Files                []projectedfile.File `json:"files"`
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
	services *serviceops.Controller
}

func newServiceHandler(logger *slog.Logger, stores *store.Store, services *serviceops.Controller) serviceHandler {
	return serviceHandler{
		logger:   logger,
		store:    stores,
		services: services,
	}
}

func (h serviceHandler) listServices(c *gin.Context) {
	items, err := h.services.List(c.Request.Context())
	if err != nil {
		logctx.Logger(c.Request.Context(), h.logger).Error("list services failed", "error", err)
		c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}
	out := make([]serviceEnvelope, 0, len(items))
	for _, item := range items {
		out = append(out, serviceEnvelope{
			Service: buildServiceResource(item),
		})
	}
	c.JSON(http.StatusOK, map[string]any{"items": out})
}

func (h serviceHandler) createService(c *gin.Context) {
	var request serviceCreateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	input, err := request.toCreateInput()
	if err != nil {
		c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	logger := logctx.Logger(c.Request.Context(), h.logger)

	view, err := h.services.Create(c.Request.Context(), input)
	if err != nil {
		switch {
		case domain.IsInvalidInput(err):
			c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrRegistryCredentialNotFound):
			c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrPlaneNotFound):
			c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrServiceNameAlreadyExists):
			c.JSON(http.StatusConflict, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("create service failed", "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}
	recordControlEvent(logger, h.store, c.Request.Context(), eventlog.CreateInput{
		Action:     "control.service.create",
		TargetType: "service",
		TargetID:   view.Service.Metadata.ID,
		TargetName: view.Service.Metadata.Name,
	})

	c.JSON(http.StatusCreated, serviceEnvelope{
		Service: buildServiceResource(view),
	})
}

func (h serviceHandler) getService(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("serviceID"))
	if serviceID == "" {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "serviceID is required"})
		return
	}
	view, err := h.services.Get(c.Request.Context(), serviceID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrServiceNotFound):
			c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
		default:
			logctx.Logger(c.Request.Context(), h.logger).Error("get service failed", "service_id", serviceID, "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}
	c.JSON(http.StatusOK, serviceEnvelope{
		Service: buildServiceResource(view),
	})
}

func (h serviceHandler) updateService(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("serviceID"))
	if serviceID == "" {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "serviceID is required"})
		return
	}
	var request serviceUpdateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	input, err := request.toUpdateInput()
	if err != nil {
		c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	logger := logctx.Logger(c.Request.Context(), h.logger)

	view, err := h.services.Update(c.Request.Context(), serviceID, input)
	if err != nil {
		switch {
		case domain.IsInvalidInput(err):
			c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrServiceNotFound):
			c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrRegistryCredentialNotFound):
			c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrPlaneNotFound):
			c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("update service failed", "service_id", serviceID, "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}
	recordControlEvent(logger, h.store, c.Request.Context(), eventlog.CreateInput{
		Action:     "control.service.update",
		TargetType: "service",
		TargetID:   view.Service.Metadata.ID,
		TargetName: view.Service.Metadata.Name,
	})

	c.JSON(http.StatusOK, serviceEnvelope{
		Service: buildServiceResource(view),
	})
}

func (h serviceHandler) deleteService(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("serviceID"))
	if serviceID == "" {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "serviceID is required"})
		return
	}
	logger := logctx.Logger(c.Request.Context(), h.logger)

	view, err := h.services.Delete(c.Request.Context(), serviceID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrServiceNotFound):
			c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
		default:
			logger.Error("delete service failed", "service_id", serviceID, "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}
	recordControlEvent(logger, h.store, c.Request.Context(), eventlog.CreateInput{
		Action:     "control.service.delete",
		TargetType: "service",
		TargetID:   view.Service.Metadata.ID,
		TargetName: view.Service.Metadata.Name,
	})
	c.JSON(http.StatusOK, map[string]any{
		"deleted":   true,
		"serviceID": serviceID,
	})
}

func buildServiceResource(view serviceops.View) serviceResource {
	status := buildServiceStatus(view)
	return serviceResource{
		Metadata: serviceMetadata{
			ID:          view.Service.Metadata.ID,
			Name:        view.Service.Metadata.Name,
			DisplayName: view.Service.Metadata.DisplayName,
			Generation:  view.Service.Metadata.Generation,
		},
		Spec: serviceSpec{
			PlaneID:              view.Service.Spec.PlaneID,
			InstanceClass:        view.Service.Spec.InstanceClass,
			Exposure:             view.Service.Spec.Exposure,
			Image:                view.Service.Spec.Image,
			Command:              append([]string(nil), view.Service.Spec.Command...),
			Args:                 append([]string(nil), view.Service.Spec.Args...),
			DefaultPort:          view.Service.Spec.DefaultPort,
			ReadinessPath:        view.Service.Spec.ReadinessPath,
			Env:                  view.Service.Spec.Env,
			SecretEnvKeys:        sortedKeys(view.Service.Spec.SecretEnv),
			RegistryCredentialID: view.Service.Spec.RegistryCredentialID,
			Files:                projectedfile.CloneFiles(view.Service.Spec.Files),
		},
		Status: status,
	}
}

func buildServiceStatus(view serviceops.View) serviceStatus {
	serviceItem := view.Service
	status := serviceStatus{
		ObservedGeneration: serviceItem.Status.Observed.ObservedGeneration,
		DesiredState:       string(serviceItem.Status.DesiredState),
		Phase:              serviceItem.Status.Observed.Phase,
		Healthy:            serviceItem.Status.Observed.Healthy,
		Message:            serviceItem.Status.Observed.Message,
		LastReconciledAt:   serviceItem.Status.Observed.LastReconciledAt,
		Run:                buildServiceRun(serviceItem.Status.Run),
		AssignedPlaneID:    serviceItem.Status.Observed.AssignedPlaneID,
		RemoteStatus:       serviceItem.Status.Observed.RemoteStatus,
		RemoteMessage:      serviceItem.Status.Observed.RemoteMessage,
	}
	return status
}

func buildServiceRun(input domain.RunStatus) serviceRunStatus {
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

func sortedKeys(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (r serviceCreateRequest) toCreateInput() (domain.ServiceCreateInput, error) {
	if r.Spec == nil {
		return domain.ServiceCreateInput{}, domain.InvalidInput(errServiceSpecRequired)
	}
	return domain.ServiceCreateInput{
		Name:        strings.TrimSpace(r.Name),
		DisplayName: strings.TrimSpace(r.DisplayName),
		Spec:        r.Spec.toDomainSpec(),
	}, nil
}

func (r serviceUpdateRequest) toUpdateInput() (domain.ServiceUpdateInput, error) {
	if r.Spec == nil {
		return domain.ServiceUpdateInput{}, domain.InvalidInput(errServiceSpecRequired)
	}
	return domain.ServiceUpdateInput{
		DisplayName: strings.TrimSpace(r.DisplayName),
		Spec:        r.Spec.toDomainSpec(),
	}, nil
}

func (s serviceSpecInput) toDomainSpec() domain.Spec {
	return domain.Spec{
		PlaneID:              strings.TrimSpace(s.PlaneID),
		InstanceClass:        strings.TrimSpace(s.InstanceClass),
		Exposure:             strings.TrimSpace(s.Exposure),
		Image:                strings.TrimSpace(s.Image),
		Command:              append([]string(nil), s.Command...),
		Args:                 append([]string(nil), s.Args...),
		DefaultPort:          s.DefaultPort,
		ReadinessPath:        strings.TrimSpace(s.ReadinessPath),
		Env:                  s.Env,
		SecretEnv:            s.SecretEnv,
		RegistryCredentialID: strings.TrimSpace(s.RegistryCredentialID),
		Files:                projectedfile.CloneFiles(s.Files),
	}
}
