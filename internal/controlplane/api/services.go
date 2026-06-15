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

	"github.com/gin-gonic/gin"
)

type serviceSpec struct {
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

type serviceWorkloadSpec struct {
	InstanceClass string            `json:"instanceClass"`
	Exposure      string            `json:"exposure"`
	Image         string            `json:"image"`
	Command       []string          `json:"command"`
	Args          []string          `json:"args"`
	DefaultPort   int               `json:"defaultPort"`
	ReadinessPath string            `json:"readinessPath"`
	Env           map[string]string `json:"env"`
}

type serviceRunStatus struct {
	Phase   string `json:"phase"`
	Message string `json:"message,omitempty"`
}

type serviceFrontDoorStatus struct {
	CNAME        string                     `json:"cname,omitempty"`
	Verification *serviceFrontDoorDNSRecord `json:"verification,omitempty"`
}

type serviceFrontDoorDNSRecord struct {
	Host       string `json:"host"`
	RecordType string `json:"recordType"`
	Value      string `json:"value"`
}

type serviceStatus struct {
	Phase              string                 `json:"phase"`
	Message            string                 `json:"message,omitempty"`
	ObservedGeneration int64                  `json:"observedGeneration"`
	LastObservedAt     *time.Time             `json:"lastObservedAt,omitempty"`
	Run                serviceRunStatus       `json:"run"`
	FrontDoor          serviceFrontDoorStatus `json:"frontDoor"`
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

type serviceCreateRequest struct {
	Name        string       `json:"name"`
	DisplayName string       `json:"displayName"`
	Spec        *serviceSpec `json:"spec"`
}

type serviceUpdateRequest struct {
	DisplayName string               `json:"displayName"`
	Spec        *serviceWorkloadSpec `json:"spec"`
}

var errServiceSpecRequired = errors.New("spec is required")

type serviceHandler struct {
	logger   *slog.Logger
	services *coordination.ServiceOperations
}

func newServiceHandler(logger *slog.Logger, services *coordination.ServiceOperations) serviceHandler {
	return serviceHandler{
		logger:   logger,
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
		case coordination.IsServiceInputError(err):
			c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, coordination.ErrPlaneNotFound):
			c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			h.logger.Error("create service failed", "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}
	h.logger.Info("control service created", "service_id", service.Metadata.ID, "service_name", service.Metadata.Name, "plane_id", service.Spec.PlaneID)

	c.JSON(http.StatusCreated, buildServiceResource(service))
}

func (h serviceHandler) getService(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("serviceID"))
	if serviceID == "" {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "serviceID is required"})
		return
	}
	planeID := strings.TrimSpace(c.Query("planeID"))
	if planeID == "" {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}
	service, err := h.services.Get(c.Request.Context(), planeID, serviceID)
	if err != nil {
		switch {
		case errors.Is(err, coordination.ErrServiceNotFound):
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
	planeID := strings.TrimSpace(c.Query("planeID"))
	if planeID == "" {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}
	input, err := request.toUpdateInput(planeID, serviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	service, err := h.services.Update(c.Request.Context(), input)
	if err != nil {
		switch {
		case coordination.IsServiceInputError(err):
			c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, coordination.ErrServiceNotFound):
			c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, coordination.ErrPlaneNotFound):
			c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			h.logger.Error("update service failed", "service_id", serviceID, "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}
	h.logger.Info("control service updated", "service_id", service.Metadata.ID, "service_name", service.Metadata.Name, "plane_id", service.Spec.PlaneID)

	c.JSON(http.StatusOK, buildServiceResource(service))
}

func (h serviceHandler) deleteService(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("serviceID"))
	if serviceID == "" {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "serviceID is required"})
		return
	}

	planeID := strings.TrimSpace(c.Query("planeID"))
	if planeID == "" {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "planeID is required"})
		return
	}
	service, err := h.services.Delete(c.Request.Context(), coordination.DeleteServiceInput{PlaneID: planeID, ServiceID: serviceID})
	if err != nil {
		switch {
		case errors.Is(err, coordination.ErrServiceNotFound):
			c.JSON(http.StatusNotFound, map[string]any{"error": err.Error()})
		default:
			h.logger.Error("delete service failed", "service_id", serviceID, "error", err)
			c.JSON(http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		}
		return
	}
	h.logger.Info("control service deletion requested", "service_id", service.Metadata.ID, "service_name", service.Metadata.Name, "plane_id", service.Spec.PlaneID)
	c.JSON(http.StatusOK, buildServiceResource(service))
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
		Spec: serviceSpecFromModel(service.Spec),
		Status: serviceStatus{
			Phase:              service.Status.Observed.Phase,
			Message:            service.Status.Observed.Message,
			ObservedGeneration: service.Status.Observed.ObservedGeneration,
			LastObservedAt:     service.Status.Observed.LastObservedAt,
			Run:                buildServiceRun(service.Status.Run),
			FrontDoor:          buildServiceFrontDoor(service.Status.FrontDoor),
		},
	}
}

func buildServiceRun(input model.RunStatus) serviceRunStatus {
	return serviceRunStatus{
		Phase:   input.Phase,
		Message: input.Message,
	}
}

func buildServiceFrontDoor(input model.FrontDoorStatus) serviceFrontDoorStatus {
	out := serviceFrontDoorStatus{CNAME: input.CNAME}
	if input.Verification != nil {
		out.Verification = &serviceFrontDoorDNSRecord{
			Host:       input.Verification.Host,
			RecordType: input.Verification.RecordType,
			Value:      input.Verification.Value,
		}
	}
	return out
}

func serviceSpecFromModel(spec model.ServiceSpec) serviceSpec {
	out := serviceSpec{
		PlaneID:       spec.PlaneID,
		InstanceClass: spec.InstanceClass,
		Exposure:      spec.Exposure,
		Image:         spec.Image,
		Command:       slices.Clone(spec.Command),
		Args:          slices.Clone(spec.Args),
		DefaultPort:   spec.DefaultPort,
		ReadinessPath: spec.ReadinessPath,
		Env:           spec.Env,
	}
	if out.Command == nil {
		out.Command = []string{}
	}
	if out.Args == nil {
		out.Args = []string{}
	}
	if out.Env == nil {
		out.Env = map[string]string{}
	}
	return out
}

func (r serviceCreateRequest) toCreateInput() (coordination.CreateServiceInput, error) {
	if r.Spec == nil {
		return coordination.CreateServiceInput{}, errServiceSpecRequired
	}
	return coordination.CreateServiceInput{
		Name:        strings.TrimSpace(r.Name),
		DisplayName: strings.TrimSpace(r.DisplayName),
		Spec:        r.Spec.toServiceSpec(),
	}, nil
}

func (r serviceUpdateRequest) toUpdateInput(planeID string, serviceID string) (coordination.UpdateServiceInput, error) {
	if r.Spec == nil {
		return coordination.UpdateServiceInput{}, errServiceSpecRequired
	}
	return coordination.UpdateServiceInput{
		PlaneID:     strings.TrimSpace(planeID),
		ServiceID:   strings.TrimSpace(serviceID),
		DisplayName: strings.TrimSpace(r.DisplayName),
		Spec:        r.Spec.toWorkloadSpec(),
	}, nil
}

func (s serviceSpec) toServiceSpec() model.ServiceSpec {
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

func (s serviceWorkloadSpec) toWorkloadSpec() model.WorkloadSpec {
	return model.WorkloadSpec{
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
