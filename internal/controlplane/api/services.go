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
	"mini-cloud/internal/controlplane/coordination"
	"mini-cloud/internal/controlplane/model"
	"mini-cloud/internal/controlplane/store"

	"github.com/gin-gonic/gin"
)

type serviceSpec struct {
	PlaneID            string                     `json:"planeID"`
	InstanceClass      string                     `json:"instanceClass"`
	Exposure           string                     `json:"exposure"`
	Image              string                     `json:"image"`
	Command            []string                   `json:"command,omitempty"`
	Args               []string                   `json:"args,omitempty"`
	DefaultPort        int                        `json:"defaultPort"`
	ReadinessPath      string                     `json:"readinessPath"`
	Env                map[string]string          `json:"env,omitempty"`
	SecretEnvKeys      []string                   `json:"secretEnvKeys,omitempty"`
	RegistryCredential *registryCredentialSummary `json:"registryCredential,omitempty"`
	Files              []projectedfile.File       `json:"files,omitempty"`
}

type registryCredentialSummary struct {
	Server             string `json:"server"`
	Username           string `json:"username"`
	PasswordConfigured bool   `json:"passwordConfigured"`
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
	PlaneID            string                     `json:"planeID"`
	InstanceClass      string                     `json:"instanceClass"`
	Exposure           string                     `json:"exposure"`
	Image              string                     `json:"image"`
	Command            []string                   `json:"command"`
	Args               []string                   `json:"args"`
	DefaultPort        int                        `json:"defaultPort"`
	ReadinessPath      string                     `json:"readinessPath"`
	Env                map[string]string          `json:"env"`
	SecretEnv          map[string]string          `json:"secretEnv"`
	RegistryCredential *registryCredentialRequest `json:"registryCredential"`
	Files              []projectedfile.File       `json:"files"`
}

type registryCredentialRequest struct {
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
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
	services *coordination.ServiceController
}

func newServiceHandler(logger *slog.Logger, stores *store.Store, services *coordination.ServiceController) serviceHandler {
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

	service, err := h.services.Create(c.Request.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrInvalidInput):
			c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
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
	recordControlEvent(logger, h.store, c.Request.Context(), store.CreateControlEventInput{
		Action:     "control.service.create",
		TargetType: "service",
		TargetID:   service.Metadata.ID,
		TargetName: service.Metadata.Name,
	})

	c.JSON(http.StatusCreated, serviceEnvelope{
		Service: buildServiceResource(service),
	})
}

func (h serviceHandler) getService(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("serviceID"))
	if serviceID == "" {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "serviceID is required"})
		return
	}
	service, err := h.services.Get(c.Request.Context(), serviceID)
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
		Service: buildServiceResource(service),
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

	service, err := h.services.Update(c.Request.Context(), serviceID, input)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrInvalidInput):
			c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrServiceNotFound):
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
	recordControlEvent(logger, h.store, c.Request.Context(), store.CreateControlEventInput{
		Action:     "control.service.update",
		TargetType: "service",
		TargetID:   service.Metadata.ID,
		TargetName: service.Metadata.Name,
	})

	c.JSON(http.StatusOK, serviceEnvelope{
		Service: buildServiceResource(service),
	})
}

func (h serviceHandler) deleteService(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("serviceID"))
	if serviceID == "" {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "serviceID is required"})
		return
	}
	logger := logctx.Logger(c.Request.Context(), h.logger)

	service, err := h.services.Delete(c.Request.Context(), serviceID)
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
	recordControlEvent(logger, h.store, c.Request.Context(), store.CreateControlEventInput{
		Action:     "control.service.delete",
		TargetType: "service",
		TargetID:   service.Metadata.ID,
		TargetName: service.Metadata.Name,
	})
	c.JSON(http.StatusOK, map[string]any{
		"deleted":   true,
		"serviceID": serviceID,
	})
}

func buildServiceResource(service model.Service) serviceResource {
	status := buildServiceStatus(service)
	return serviceResource{
		Metadata: serviceMetadata{
			ID:          service.Metadata.ID,
			Name:        service.Metadata.Name,
			DisplayName: service.Metadata.DisplayName,
			Generation:  service.Metadata.Generation,
		},
		Spec: serviceSpec{
			PlaneID:            service.Spec.PlaneID,
			InstanceClass:      service.Spec.InstanceClass,
			Exposure:           service.Spec.Exposure,
			Image:              service.Spec.Image,
			Command:            append([]string(nil), service.Spec.Command...),
			Args:               append([]string(nil), service.Spec.Args...),
			DefaultPort:        service.Spec.DefaultPort,
			ReadinessPath:      service.Spec.ReadinessPath,
			Env:                service.Spec.Env,
			SecretEnvKeys:      sortedKeys(service.Spec.SecretEnv),
			RegistryCredential: buildRegistryCredentialSummary(service.Spec.RegistryCredential),
			Files:              projectedfile.CloneFiles(service.Spec.Files),
		},
		Status: status,
	}
}

func buildServiceStatus(service model.Service) serviceStatus {
	serviceItem := service
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

func buildServiceRun(input model.RunStatus) serviceRunStatus {
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

func buildRegistryCredentialSummary(input *model.ServiceRegistryCredential) *registryCredentialSummary {
	if input == nil {
		return nil
	}
	return &registryCredentialSummary{
		Server:             input.Server,
		Username:           input.Username,
		PasswordConfigured: input.Password != "",
	}
}

func (r serviceCreateRequest) toCreateInput() (store.CreateServiceInput, error) {
	if r.Spec == nil {
		return store.CreateServiceInput{}, errServiceSpecRequired
	}
	return store.CreateServiceInput{
		Name:        strings.TrimSpace(r.Name),
		DisplayName: strings.TrimSpace(r.DisplayName),
		Spec:        r.Spec.toServiceSpec(),
	}, nil
}

func (r serviceUpdateRequest) toUpdateInput() (store.UpdateServiceInput, error) {
	if r.Spec == nil {
		return store.UpdateServiceInput{}, errServiceSpecRequired
	}
	return store.UpdateServiceInput{
		DisplayName: strings.TrimSpace(r.DisplayName),
		Spec:        r.Spec.toServiceSpec(),
	}, nil
}

func (s serviceSpecInput) toServiceSpec() model.ServiceSpec {
	return model.ServiceSpec{
		PlaneID:            strings.TrimSpace(s.PlaneID),
		InstanceClass:      strings.TrimSpace(s.InstanceClass),
		Exposure:           strings.TrimSpace(s.Exposure),
		Image:              strings.TrimSpace(s.Image),
		Command:            append([]string(nil), s.Command...),
		Args:               append([]string(nil), s.Args...),
		DefaultPort:        s.DefaultPort,
		ReadinessPath:      strings.TrimSpace(s.ReadinessPath),
		Env:                s.Env,
		SecretEnv:          s.SecretEnv,
		RegistryCredential: s.RegistryCredential.toRegistryCredential(),
		Files:              projectedfile.CloneFiles(s.Files),
	}
}

func (r *registryCredentialRequest) toRegistryCredential() *model.ServiceRegistryCredential {
	if r == nil {
		return nil
	}
	return &model.ServiceRegistryCredential{
		Server:   strings.TrimSpace(r.Server),
		Username: strings.TrimSpace(r.Username),
		Password: r.Password,
	}
}
