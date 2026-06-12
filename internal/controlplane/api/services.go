package api

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"mini-cloud/internal/controlplane/coordination"
	"mini-cloud/internal/controlplane/model"
	"mini-cloud/internal/controlplane/store"

	"github.com/gin-gonic/gin"
)

type serviceSpec struct {
	PlaneID       string            `json:"planeID"`
	InstanceClass string            `json:"instanceClass"`
	Exposure      string            `json:"exposure"`
	Image         string            `json:"image"`
	Command       []string          `json:"command,omitempty"`
	Args          []string          `json:"args,omitempty"`
	DefaultPort   int               `json:"defaultPort"`
	ReadinessPath string            `json:"readinessPath"`
	Env           map[string]string `json:"env,omitempty"`
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
	Message            string           `json:"message,omitempty"`
	LastObservedAt     *time.Time       `json:"lastObservedAt,omitempty"`
	Run                serviceRunStatus `json:"run"`
}

type serviceMetadata struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Host        string `json:"host"`
	Generation  int64  `json:"generation"`
}

type serviceResource struct {
	Metadata serviceMetadata `json:"metadata"`
	Spec     serviceSpec     `json:"spec"`
	Status   serviceStatus   `json:"status"`
}

type serviceSpecInput struct {
	PlaneID       string            `json:"planeID"`
	InstanceClass string            `json:"instanceClass"`
	Exposure      string            `json:"exposure"`
	Image         string            `json:"image"`
	Command       []string          `json:"command"`
	Args          []string          `json:"args"`
	DefaultPort   int               `json:"defaultPort"`
	ReadinessPath string            `json:"readinessPath"`
	Env           map[string]string `json:"env"`
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
		h.logger.Error("list services failed", "error", err)
		c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}
	out := make([]serviceResource, 0, len(items))
	for _, item := range items {
		out = append(out, buildServiceResource(item))
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
		case errors.Is(err, store.ErrServiceHostAlreadyExists):
			c.JSON(http.StatusConflict, map[string]any{"error": err.Error()})
			return
		default:
			h.logger.Error("create service failed", "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}
	recordControlEvent(h.logger, h.store, c.Request.Context(), store.CreateControlEventInput{
		Action:  "control.service.create",
		Message: "created service " + service.Metadata.Name,
	})

	c.JSON(http.StatusCreated, buildServiceResource(service))
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
			h.logger.Error("get service failed", "service_id", serviceID, "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}
	c.JSON(http.StatusOK, buildServiceResource(service))
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
			h.logger.Error("update service failed", "service_id", serviceID, "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}
	recordControlEvent(h.logger, h.store, c.Request.Context(), store.CreateControlEventInput{
		Action:  "control.service.update",
		Message: "updated service " + service.Metadata.Name,
	})

	c.JSON(http.StatusOK, buildServiceResource(service))
}

func (h serviceHandler) deleteService(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("serviceID"))
	if serviceID == "" {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "serviceID is required"})
		return
	}

	service, err := h.services.Delete(c.Request.Context(), serviceID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrServiceNotFound):
			c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
		default:
			h.logger.Error("delete service failed", "service_id", serviceID, "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}
	recordControlEvent(h.logger, h.store, c.Request.Context(), store.CreateControlEventInput{
		Action:  "control.service.delete",
		Message: "deleted service " + service.Metadata.Name,
	})
	c.JSON(http.StatusOK, map[string]any{
		"deleted":   true,
		"serviceID": serviceID,
	})
}

func buildServiceResource(service model.Service) serviceResource {
	return serviceResource{
		Metadata: serviceMetadata{
			ID:          service.Metadata.ID,
			Name:        service.Metadata.Name,
			DisplayName: service.Metadata.DisplayName,
			Host:        service.Metadata.Host,
			Generation:  service.Metadata.Generation,
		},
		Spec: serviceSpec{
			PlaneID:       service.Spec.PlaneID,
			InstanceClass: service.Spec.InstanceClass,
			Exposure:      service.Spec.Exposure,
			Image:         service.Spec.Image,
			Command:       slices.Clone(service.Spec.Command),
			Args:          slices.Clone(service.Spec.Args),
			DefaultPort:   service.Spec.DefaultPort,
			ReadinessPath: service.Spec.ReadinessPath,
			Env:           service.Spec.Env,
		},
		Status: serviceStatus{
			ObservedGeneration: service.Status.Observed.ObservedGeneration,
			DesiredState:       service.Status.DesiredState,
			Phase:              service.Status.Observed.Phase,
			Message:            service.Status.Observed.Message,
			LastObservedAt:     service.Status.Observed.LastObservedAt,
			Run:                buildServiceRun(service.Status.Run),
		},
	}
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
		PlaneID:       strings.TrimSpace(s.PlaneID),
		InstanceClass: strings.TrimSpace(s.InstanceClass),
		Exposure:      strings.TrimSpace(s.Exposure),
		Image:         strings.TrimSpace(s.Image),
		Command:       slices.Clone(s.Command),
		Args:          slices.Clone(s.Args),
		DefaultPort:   s.DefaultPort,
		ReadinessPath: strings.TrimSpace(s.ReadinessPath),
		Env:           s.Env,
	}
}
