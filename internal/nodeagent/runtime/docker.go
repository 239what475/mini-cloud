package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

const (
	dockerImagePullTimeout    = 5 * time.Minute
	dockerImagePullAttempts   = 3
	dockerImagePullRetryDelay = 5 * time.Second
	dockerCleanupTimeout      = 10 * time.Second
)

const (
	dockerLabelManagedBy   = "mini-cloud.managed-by"
	dockerLabelNodeID      = "mini-cloud.node-id"
	dockerLabelExecutionID = "mini-cloud.execution-id"
	dockerLabelPlanID      = "mini-cloud.plan-id"
	dockerLabelServiceID   = "mini-cloud.service-id"
)

type Docker struct {
	logger *slog.Logger
	client *client.Client
}

func NewDockerEngine(logger *slog.Logger) (*Docker, error) {
	dockerClient, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("create docker engine client: %w", err)
	}
	return &Docker{
		logger: logger,
		client: dockerClient,
	}, nil
}

func (d *Docker) Run(ctx context.Context, input RunInput) (RunResult, error) {
	if err := d.ensureImageAvailable(ctx, input.Image); err != nil {
		return RunResult{}, err
	}
	if err := d.stopServiceContainers(ctx, input.NodeID, input.ServiceID, input.ExecutionID); err != nil {
		return RunResult{}, err
	}

	hostBindIP := resolveHostBindIP(input.HostBindIP)
	hostPort, err := selectAvailableHostPort(hostBindIP, input.HostPortMin, input.HostPortMax)
	if err != nil {
		return RunResult{}, err
	}

	config, hostConfig, portSpec, err := buildContainerCreateConfig(input, hostPort)
	if err != nil {
		return RunResult{}, err
	}
	logger := d.logger
	attemptedHostPorts := map[int]struct{}{}
	var created container.CreateResponse
	for {
		logger.Info("runtime docker api create container",
			"name", input.ContainerName,
			"image", input.Image,
			"container_port", input.ContainerPort,
			"host_port", hostPort,
		)
		created, err = d.client.ContainerCreate(ctx, config, hostConfig, nil, nil, input.ContainerName)
		if err == nil {
			break
		}
		if !shouldRetryHostPortCreate(err) {
			return RunResult{}, fmt.Errorf("docker container create failed: %w", err)
		}
		attemptedHostPorts[hostPort] = struct{}{}
		nextPort, selectErr := selectAvailableHostPortExcluding(hostBindIP, input.HostPortMin, input.HostPortMax, attemptedHostPorts)
		if selectErr != nil {
			return RunResult{}, errors.Join(
				fmt.Errorf("docker container create failed after host port retries: %w", err),
				selectErr,
			)
		}
		hostPort = nextPort
		config, hostConfig, portSpec, err = buildContainerCreateConfig(input, hostPort)
		if err != nil {
			return RunResult{}, err
		}
	}

	if err := d.client.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		d.removeCreatedContainer(context.WithoutCancel(ctx), created.ID)
		return RunResult{}, fmt.Errorf("docker container start failed: %w", err)
	}

	detectedHostPort, err := d.detectHostPort(ctx, created.ID, portSpec)
	if err != nil {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), dockerCleanupTimeout)
		_ = d.client.ContainerStop(cleanupCtx, created.ID, container.StopOptions{})
		cancelCleanup()
		return RunResult{}, err
	}

	return RunResult{
		ContainerID:   created.ID,
		ContainerName: input.ContainerName,
		HostPort:      detectedHostPort,
	}, nil
}

func (d *Docker) Stop(ctx context.Context, containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return nil
	}
	if err := d.client.ContainerStop(ctx, containerID, container.StopOptions{}); err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("docker stop failed: %w", err)
	}
	return nil
}

func (d *Docker) ResetNode(ctx context.Context, nodeID string) error {
	if d == nil {
		return nil
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return fmt.Errorf("nodeID is required for runtime reset")
	}
	filter := filters.NewArgs()
	filter.Add("label", dockerLabelManagedBy+"=node-agent")
	filter.Add("label", dockerLabelNodeID+"="+nodeID)
	items, err := d.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filter,
	})
	if err != nil {
		return fmt.Errorf("docker list mini-cloud node containers failed: %w", err)
	}
	for _, item := range items {
		if err := d.client.ContainerStop(ctx, item.ID, container.StopOptions{}); err != nil {
			if cerrdefs.IsNotFound(err) || isContainerAlreadyStopped(err) {
				// Continue to remove below; a stopped-but-present container can still hold its name.
			} else {
				return fmt.Errorf("docker stop mini-cloud node container %s failed: %w", item.ID, err)
			}
		}
		if err := d.client.ContainerRemove(ctx, item.ID, container.RemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
			return fmt.Errorf("docker remove mini-cloud node container %s failed: %w", item.ID, err)
		}
		d.logger.Info("stopped stale mini-cloud container during node runtime reset",
			"node_id", nodeID,
			"container_id", item.ID,
			"execution_id", item.Labels[dockerLabelExecutionID],
		)
	}
	return nil
}

func (d *Docker) stopServiceContainers(ctx context.Context, nodeID string, serviceID string, currentExecutionID string) error {
	nodeID = strings.TrimSpace(nodeID)
	serviceID = strings.TrimSpace(serviceID)
	currentExecutionID = strings.TrimSpace(currentExecutionID)
	if nodeID == "" || serviceID == "" {
		return nil
	}
	filter := filters.NewArgs()
	filter.Add("label", dockerLabelManagedBy+"=node-agent")
	filter.Add("label", dockerLabelNodeID+"="+nodeID)
	filter.Add("label", dockerLabelServiceID+"="+serviceID)
	items, err := d.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filter,
	})
	if err != nil {
		return fmt.Errorf("docker list mini-cloud service containers failed: %w", err)
	}
	for _, item := range items {
		if item.Labels[dockerLabelExecutionID] == currentExecutionID {
			continue
		}
		if err := d.client.ContainerStop(ctx, item.ID, container.StopOptions{}); err != nil {
			if !cerrdefs.IsNotFound(err) && !isContainerAlreadyStopped(err) {
				return fmt.Errorf("docker stop old service container %s failed: %w", item.ID, err)
			}
		}
		if err := d.client.ContainerRemove(ctx, item.ID, container.RemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
			return fmt.Errorf("docker remove old service container %s failed: %w", item.ID, err)
		}
		d.logger.Info("stopped old mini-cloud service container before replacement",
			"node_id", nodeID,
			"service_id", serviceID,
			"container_id", item.ID,
			"execution_id", item.Labels[dockerLabelExecutionID],
		)
	}
	return nil
}

func (d *Docker) Logs(ctx context.Context, containerID string, tail int) (string, error) {
	if tail <= 0 {
		tail = 50
	}
	reader, err := d.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       strconv.Itoa(tail),
	})
	if err != nil {
		return "", fmt.Errorf("docker logs failed: %w", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, reader); err != nil {
		if closeErr := reader.Close(); closeErr != nil {
			return "", errors.Join(fmt.Errorf("decode docker logs stream: %w", err), fmt.Errorf("close docker logs stream: %w", closeErr))
		}
		return "", fmt.Errorf("decode docker logs stream: %w", err)
	}
	if err := reader.Close(); err != nil {
		return "", fmt.Errorf("close docker logs stream: %w", err)
	}

	parts := make([]string, 0, 2)
	if value := strings.TrimSpace(stdout.String()); value != "" {
		parts = append(parts, value)
	}
	if value := strings.TrimSpace(stderr.String()); value != "" {
		parts = append(parts, value)
	}
	return strings.Join(parts, "\n"), nil
}

func (d *Docker) Close() error {
	if d == nil {
		return nil
	}
	if d.client == nil {
		return nil
	}
	if err := d.client.Close(); err != nil {
		return fmt.Errorf("close docker engine client: %w", err)
	}
	return nil
}

func (d *Docker) ensureImageAvailable(ctx context.Context, imageRef string) error {
	if _, err := d.client.ImageInspect(ctx, imageRef); err == nil {
		return nil
	} else if !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("docker inspect image failed: %w", err)
	}

	logger := d.logger
	var lastErr error

	for attempt := 1; attempt <= dockerImagePullAttempts; attempt++ {
		logger.Info("runtime docker api pull image", "image", imageRef, "attempt", attempt, "max_attempts", dockerImagePullAttempts)

		pullCtx, cancel := context.WithTimeout(ctx, dockerImagePullTimeout)
		reader, err := d.client.ImagePull(pullCtx, imageRef, image.PullOptions{})
		if err == nil {
			if _, copyErr := io.Copy(io.Discard, reader); copyErr != nil {
				err = fmt.Errorf("drain docker image pull stream: %w", copyErr)
			}
			if closeErr := reader.Close(); closeErr != nil {
				closeErr = fmt.Errorf("close docker image pull stream: %w", closeErr)
				if err != nil {
					err = errors.Join(err, closeErr)
				} else {
					err = closeErr
				}
			}
		}
		cancel()

		if err == nil {
			if _, inspectErr := d.client.ImageInspect(ctx, imageRef); inspectErr == nil {
				return nil
			} else if !cerrdefs.IsNotFound(inspectErr) {
				err = fmt.Errorf("docker inspect pulled image failed: %w", inspectErr)
			} else {
				err = fmt.Errorf("docker image %s is still not present after pull completed", imageRef)
			}
		} else {
			err = fmt.Errorf("docker image pull failed: %w", err)
		}

		lastErr = err
		if attempt == dockerImagePullAttempts {
			break
		}

		logger.Warn("runtime docker image pull attempt failed, retrying",
			"image", imageRef,
			"attempt", attempt,
			"max_attempts", dockerImagePullAttempts,
			"error", err,
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(dockerImagePullRetryDelay):
		}
	}

	return fmt.Errorf("docker image pull failed after %d attempts: %w", dockerImagePullAttempts, lastErr)
}

func (d *Docker) detectHostPort(ctx context.Context, containerID string, portSpec nat.Port) (int, error) {
	var lastErr error
	for attempt := 1; attempt <= 10; attempt++ {
		inspected, err := d.client.ContainerInspect(ctx, containerID)
		if err == nil {
			bindings := inspected.NetworkSettings.Ports[portSpec]
			if len(bindings) > 0 {
				value := strings.TrimSpace(bindings[0].HostPort)
				if value != "" {
					hostPort, parseErr := strconv.Atoi(value)
					if parseErr == nil {
						return hostPort, nil
					}
					lastErr = fmt.Errorf("parse inspect host port %q: %w", value, parseErr)
				} else {
					lastErr = fmt.Errorf("docker inspect returned empty host port for %s", portSpec.Port())
				}
			} else {
				lastErr = fmt.Errorf("docker inspect ports missing %s", portSpec.Port())
			}
		} else {
			lastErr = fmt.Errorf("docker inspect failed: %w", err)
		}

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("host port not available yet")
	}
	return 0, fmt.Errorf("detect host port failed: %w", lastErr)
}

func buildContainerCreateConfig(input RunInput, hostPort int) (*container.Config, *container.HostConfig, nat.Port, error) {
	if input.ContainerPort <= 0 {
		return nil, nil, "", fmt.Errorf("container port must be greater than 0")
	}

	portSpec := nat.Port(fmt.Sprintf("%d/tcp", input.ContainerPort))
	hostBindIP := resolveHostBindIP(input.HostBindIP)
	if hostPort <= 0 {
		return nil, nil, "", fmt.Errorf("host port must be greater than 0")
	}
	config := &container.Config{
		Image:        input.Image,
		Env:          buildContainerEnv(input.Env),
		ExposedPorts: nat.PortSet{portSpec: struct{}{}},
		Labels:       buildMiniCloudLabels(input),
	}
	if cmd := resolveContainerCommand(input); len(cmd) > 0 {
		config.Cmd = cmd
	}

	hostConfig := &container.HostConfig{
		AutoRemove: true,
		ExtraHosts: []string{
			"host.docker.internal:host-gateway",
		},
		PortBindings: nat.PortMap{
			portSpec: []nat.PortBinding{
				{
					HostIP:   hostBindIP,
					HostPort: strconv.Itoa(hostPort),
				},
			},
		},
	}
	return config, hostConfig, portSpec, nil
}

func resolveHostBindIP(value string) string {
	hostBindIP := strings.TrimSpace(value)
	if hostBindIP == "" {
		return "127.0.0.1"
	}
	return hostBindIP
}

func selectAvailableHostPort(hostBindIP string, minPort int, maxPort int) (int, error) {
	return selectAvailableHostPortExcluding(hostBindIP, minPort, maxPort, nil)
}

func selectAvailableHostPortExcluding(hostBindIP string, minPort int, maxPort int, excluded map[int]struct{}) (int, error) {
	if minPort <= 0 || maxPort <= 0 {
		return 0, fmt.Errorf("host port range min and max must be greater than 0")
	}
	if minPort > maxPort {
		return 0, fmt.Errorf("host port range min must be less than or equal to max")
	}
	for port := minPort; port <= maxPort; port++ {
		if _, skip := excluded[port]; skip {
			continue
		}
		listener, err := net.Listen("tcp", net.JoinHostPort(hostBindIP, strconv.Itoa(port)))
		if err != nil {
			continue
		}
		if err := listener.Close(); err != nil {
			return 0, fmt.Errorf("close host port probe listener: %w", err)
		}
		return port, nil
	}
	return 0, fmt.Errorf("no available host port in range %d-%d for bind ip %s", minPort, maxPort, hostBindIP)
}

func shouldRetryHostPortCreate(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "port is already allocated") ||
		strings.Contains(message, "ports are not available") ||
		strings.Contains(message, "address already in use") ||
		strings.Contains(message, "bind: address already in use")
}

func isContainerAlreadyStopped(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not modified") ||
		strings.Contains(message, "is not running") ||
		strings.Contains(message, "already stopped")
}

func buildContainerEnv(env map[string]string) []string {
	values := make([]string, 0, len(env))

	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		values = append(values, fmt.Sprintf("%s=%s", key, env[key]))
	}
	return values
}

func buildMiniCloudLabels(input RunInput) map[string]string {
	labels := map[string]string{
		dockerLabelManagedBy: "node-agent",
	}
	addLabel(labels, dockerLabelNodeID, input.NodeID)
	addLabel(labels, dockerLabelExecutionID, input.ExecutionID)
	addLabel(labels, dockerLabelPlanID, input.PlanID)
	addLabel(labels, dockerLabelServiceID, input.ServiceID)
	return labels
}

func addLabel(labels map[string]string, key string, value string) {
	if value = strings.TrimSpace(value); value != "" {
		labels[key] = value
	}
}

func resolveContainerCommand(input RunInput) []string {
	if len(input.Command) > 0 {
		command := make([]string, 0, len(input.Command)+len(input.Args))
		command = append(command, input.Command...)
		command = append(command, input.Args...)
		return command
	}
	if len(input.Args) == 0 {
		return nil
	}
	command := make([]string, 0, len(input.Args))
	command = append(command, input.Args...)
	return command
}

func (d *Docker) removeCreatedContainer(ctx context.Context, containerID string) {
	if strings.TrimSpace(containerID) == "" {
		return
	}
	cleanupCtx, cancelCleanup := context.WithTimeout(ctx, dockerCleanupTimeout)
	defer cancelCleanup()
	if err := d.client.ContainerRemove(cleanupCtx, containerID, container.RemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
		d.logger.Warn("remove created docker container failed",
			"container_id", containerID,
			"error", err,
		)
	}
}

func isSafePathSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	return !strings.ContainsAny(value, `/\`)
}
