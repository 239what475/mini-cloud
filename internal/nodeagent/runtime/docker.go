package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"

	"mini-cloud/internal/common/logctx"
	"mini-cloud/internal/common/projectedfile"
)

const (
	// dockerImagePullTimeout 是单次 Docker 拉取镜像操作的超时时间。
	dockerImagePullTimeout = 5 * time.Minute
	// dockerImagePullAttempts 是 Docker 拉取镜像的最大总尝试次数。
	dockerImagePullAttempts = 3
	// dockerImagePullRetryDelay 是两次 Docker 拉取镜像尝试之间的等待时间。
	dockerImagePullRetryDelay = 5 * time.Second
	// dockerCleanupTimeout 是失败回滚和清理 Docker 资源时使用的超时时间。
	dockerCleanupTimeout = 10 * time.Second
	// projectedFilesRootDir 是宿主机上存放投影文件的根目录。
	projectedFilesRootDir = "/var/lib/mini-cloud/node-agent/projected-files"
	// projectedExecutionsDir 是投影文件根目录下按执行分组的目录名。
	projectedExecutionsDir = "executions"
	// projectedFilesDir 是单个执行目录下真正发布给容器挂载的文件目录名。
	projectedFilesDir = "files"
	// projectedStagingPrefix 是投影文件暂存目录前缀。
	projectedStagingPrefix = ".staging-"
)

const (
	// dockerLabelManagedBy 标记容器由 mini-cloud node-agent 管理。
	dockerLabelManagedBy = "mini-cloud.managed-by"
	// dockerLabelNodeID 保存承载容器的节点 ID。
	dockerLabelNodeID = "mini-cloud.node-id"
	// dockerLabelExecutionID 保存容器所属执行 ID。
	dockerLabelExecutionID = "mini-cloud.execution-id"
	// dockerLabelPlanID 保存容器所属 execution plan ID。
	dockerLabelPlanID = "mini-cloud.plan-id"
	// dockerLabelServiceID 保存容器所属服务 ID。
	dockerLabelServiceID = "mini-cloud.service-id"
	// dockerLabelProjectionRef 保存容器投影文件引用。
	dockerLabelProjectionRef = "mini-cloud.projection-ref"
)

// Docker 基于 Docker Engine API 实现 Runtime。
type Docker struct {
	// logger 记录 Docker 运行时操作日志。
	logger *slog.Logger
	// client 是 Docker Engine API 客户端。
	client *client.Client

	// mu 保护 projectionDirs。
	mu sync.Mutex
	// projectionDirs 记录容器 ID 到投影文件目录的映射，用于停止和关闭时清理。
	projectionDirs map[string]trackedProjection
}

// trackedProjection 记录一个容器对应的投影文件目录。
type trackedProjection struct {
	// ExecutionID 是投影文件目录所属的执行 ID。
	ExecutionID string
	// Dir 是宿主机上的投影文件执行目录。
	Dir string
}

// NewDockerEngine 创建使用环境变量配置的 Docker Engine 客户端。
func NewDockerEngine(logger *slog.Logger) (*Docker, error) {
	dockerClient, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("create docker engine client: %w", err)
	}
	return &Docker{
		logger:         logger,
		client:         dockerClient,
		projectionDirs: make(map[string]trackedProjection),
	}, nil
}

// Run 确保镜像可用、创建投影文件挂载、启动 Docker 容器并返回宿主机端口。
func (d *Docker) Run(ctx context.Context, input RunInput) (RunResult, error) {
	registryAuth, err := buildRegistryAuth(input.ImageCredential)
	if err != nil {
		return RunResult{}, fmt.Errorf("build docker registry auth: %w", err)
	}
	if err := d.ensureImageAvailable(ctx, input.Image, registryAuth); err != nil {
		return RunResult{}, err
	}

	runInput := input
	if runInput.HostPort == 0 && (runInput.HostPortMin > 0 || runInput.HostPortMax > 0) {
		hostPort, err := selectAvailableHostPort(resolveHostBindIP(runInput.HostBindIP), runInput.HostPortMin, runInput.HostPortMax)
		if err != nil {
			return RunResult{}, err
		}
		runInput.HostPort = hostPort
	}

	config, hostConfig, portSpec, err := buildContainerCreateConfig(runInput)
	if err != nil {
		return RunResult{}, err
	}
	projectionDir, mounts, err := prepareProjectedMounts(runInput)
	if err != nil {
		return RunResult{}, err
	}
	if len(mounts) > 0 {
		hostConfig.Mounts = append(hostConfig.Mounts, mounts...)
	}
	runtimeMounts := append([]mount.Mount{}, mounts...)
	cleanupProjectionDir := true
	defer func() {
		if cleanupProjectionDir {
			d.cleanupProjectionDir("", projectionDir)
		}
	}()

	logger := logctx.Logger(ctx, d.logger)
	attemptedHostPorts := map[int]struct{}{}
	var created container.CreateResponse
	for {
		logger.Info("runtime docker api create container",
			"name", runInput.ContainerName,
			"image", runInput.Image,
			"container_port", runInput.ContainerPort,
			"host_port", runInput.HostPort,
		)
		created, err = d.client.ContainerCreate(ctx, config, hostConfig, nil, nil, runInput.ContainerName)
		if err == nil {
			break
		}
		if !shouldRetryHostPortCreate(err, runInput) {
			return RunResult{}, fmt.Errorf("docker container create failed: %w", err)
		}
		attemptedHostPorts[runInput.HostPort] = struct{}{}
		nextPort, selectErr := selectAvailableHostPortExcluding(resolveHostBindIP(runInput.HostBindIP), runInput.HostPortMin, runInput.HostPortMax, attemptedHostPorts)
		if selectErr != nil {
			return RunResult{}, errors.Join(
				fmt.Errorf("docker container create failed after host port retries: %w", err),
				selectErr,
			)
		}
		runInput.HostPort = nextPort
		config, hostConfig, portSpec, err = buildContainerCreateConfig(runInput)
		if err != nil {
			return RunResult{}, err
		}
		hostConfig.Mounts = append(hostConfig.Mounts, runtimeMounts...)
	}

	if err := d.client.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		d.removeCreatedContainer(context.WithoutCancel(ctx), created.ID)
		d.cleanupProjectionDir("", projectionDir)
		return RunResult{}, fmt.Errorf("docker container start failed: %w", err)
	}

	hostPort, err := d.detectHostPort(ctx, created.ID, portSpec)
	if err != nil {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), dockerCleanupTimeout)
		_ = d.client.ContainerStop(cleanupCtx, created.ID, container.StopOptions{})
		cancelCleanup()
		d.cleanupContainerProjectionDir(created.ID)
		return RunResult{}, err
	}
	d.trackProjectionDir(created.ID, projectionDir)
	cleanupProjectionDir = false

	return RunResult{
		ContainerID:   created.ID,
		ContainerName: runInput.ContainerName,
		HostPort:      hostPort,
	}, nil
}

// Stop 停止指定 Docker 容器，并清理该容器关联的投影文件目录。
func (d *Docker) Stop(ctx context.Context, containerID string) error {
	defer d.cleanupContainerProjectionDir(containerID)
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

// ResetNode 停止当前节点上由 mini-cloud 管理的旧工作负载容器。
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
		d.cleanupContainerProjectionDir(item.ID)
		d.logger.Info("stopped stale mini-cloud container during node runtime reset",
			"node_id", nodeID,
			"container_id", item.ID,
			"execution_id", item.Labels[dockerLabelExecutionID],
		)
	}
	return nil
}

// Logs 读取指定 Docker 容器的 stdout/stderr 尾部日志。
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

// StreamLogs 持续读取指定 Docker 容器日志流，并逐行发给 emit。
func (d *Docker) StreamLogs(ctx context.Context, containerID string, emit LogEmitter) error {
	reader, err := d.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: true,
	})
	if err != nil {
		return fmt.Errorf("docker follow logs failed: %w", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			logctx.Logger(ctx, d.logger).Warn("close docker follow logs stream failed", "container_id", containerID, "error", err)
		}
	}()

	stdoutWriter := newLogLineWriter("stdout", emit)
	stderrWriter := newLogLineWriter("stderr", emit)
	defer stdoutWriter.Flush()
	defer stderrWriter.Flush()

	if _, err := stdcopy.StdCopy(stdoutWriter, stderrWriter, reader); err != nil {
		return fmt.Errorf("decode docker follow logs stream: %w", err)
	}
	return nil
}

// CountRunning 返回 Docker 当前可见的运行中容器数量。
func (d *Docker) CountRunning(ctx context.Context) (int, error) {
	items, err := d.client.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("docker list containers failed: %w", err)
	}
	return len(items), nil
}

// Close 清理已跟踪的投影文件目录，并关闭 Docker 客户端。
func (d *Docker) Close() error {
	if d == nil {
		return nil
	}
	d.cleanupTrackedProjectionDirs()
	if d.client == nil {
		return nil
	}
	if err := d.client.Close(); err != nil {
		return fmt.Errorf("close docker engine client: %w", err)
	}
	return nil
}

// GarbageCollect 清理不再被当前跟踪状态或 Docker 容器标签引用的投影文件目录。
func (d *Docker) GarbageCollect(ctx context.Context) error {
	return d.CleanupOrphans(ctx)
}

// CleanupOrphans 删除没有被当前跟踪状态或 Docker 容器标签引用的投影文件目录。
func (d *Docker) CleanupOrphans(ctx context.Context) error {
	if d == nil {
		return nil
	}
	active := d.activeTrackedExecutions()
	if d.client != nil {
		filter := filters.NewArgs()
		filter.Add("label", dockerLabelManagedBy+"=node-agent")
		items, err := d.client.ContainerList(ctx, container.ListOptions{
			All:     true,
			Filters: filter,
		})
		if err != nil {
			return fmt.Errorf("docker list mini-cloud containers failed: %w", err)
		}
		for _, item := range items {
			executionID := strings.TrimSpace(item.Labels[dockerLabelExecutionID])
			if executionID != "" {
				active[executionID] = struct{}{}
			}
		}
	}

	executionsRoot := filepath.Join(projectedFilesRootDir, projectedExecutionsDir)
	entries, err := os.ReadDir(executionsRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read projected executions dir: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		executionID := entry.Name()
		if _, ok := active[executionID]; ok {
			continue
		}
		if err := os.RemoveAll(filepath.Join(executionsRoot, executionID)); err != nil {
			return fmt.Errorf("cleanup orphan projected execution %s: %w", executionID, err)
		}
	}
	return nil
}

// ensureImageAvailable 确保镜像本地可用；不存在时按重试策略拉取镜像。
func (d *Docker) ensureImageAvailable(ctx context.Context, imageRef string, registryAuth string) error {
	if _, err := d.client.ImageInspect(ctx, imageRef); err == nil {
		return nil
	} else if !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("docker inspect image failed: %w", err)
	}

	logger := logctx.Logger(ctx, d.logger)
	var lastErr error

	for attempt := 1; attempt <= dockerImagePullAttempts; attempt++ {
		logger.Info("runtime docker api pull image", "image", imageRef, "attempt", attempt, "max_attempts", dockerImagePullAttempts)

		pullCtx, cancel := context.WithTimeout(ctx, dockerImagePullTimeout)
		reader, err := d.client.ImagePull(pullCtx, imageRef, image.PullOptions{
			RegistryAuth: registryAuth,
		})
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

// detectHostPort 轮询 Docker inspect，读取容器端口映射到宿主机后的端口。
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

// buildContainerCreateConfig 根据 RunInput 构造 Docker 容器配置、宿主机配置和端口规格。
func buildContainerCreateConfig(input RunInput) (*container.Config, *container.HostConfig, nat.Port, error) {
	if input.ContainerPort <= 0 {
		return nil, nil, "", fmt.Errorf("container port must be greater than 0")
	}

	portSpec := nat.Port(fmt.Sprintf("%d/tcp", input.ContainerPort))
	hostBindIP := resolveHostBindIP(input.HostBindIP)
	if input.HostPort < 0 {
		return nil, nil, "", fmt.Errorf("host port must be greater than or equal to 0")
	}
	if (input.HostPortMin > 0 || input.HostPortMax > 0) && input.HostPort == 0 {
		return nil, nil, "", fmt.Errorf("host port must be selected when host port range is configured")
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
					HostIP: hostBindIP,
					HostPort: func() string {
						if input.HostPort <= 0 {
							return ""
						}
						return strconv.Itoa(input.HostPort)
					}(),
				},
			},
		},
	}
	return config, hostConfig, portSpec, nil
}

// resolveHostBindIP 返回 Docker 宿主机端口绑定地址；空值仅用于本地/测试路径，生产由 node-agent 传入私网 IP。
func resolveHostBindIP(value string) string {
	hostBindIP := strings.TrimSpace(value)
	if hostBindIP == "" {
		return "127.0.0.1"
	}
	return hostBindIP
}

// selectAvailableHostPort 在配置端口池中选择一个当前可监听的 hostPort。
func selectAvailableHostPort(hostBindIP string, minPort int, maxPort int) (int, error) {
	return selectAvailableHostPortExcluding(hostBindIP, minPort, maxPort, nil)
}

// selectAvailableHostPortExcluding 在配置端口池中选择一个当前可监听且不在排除集合内的 hostPort。
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

// shouldRetryHostPortCreate 判断 Docker 创建失败是否可能由 hostPort 竞争导致。
func shouldRetryHostPortCreate(err error, input RunInput) bool {
	if err == nil || input.HostPort <= 0 || input.HostPortMin <= 0 || input.HostPortMax <= 0 {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "port is already allocated") ||
		strings.Contains(message, "ports are not available") ||
		strings.Contains(message, "address already in use") ||
		strings.Contains(message, "bind: address already in use")
}

// isContainerAlreadyStopped 判断 Docker stop 对非运行容器返回的幂等错误。
func isContainerAlreadyStopped(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not modified") ||
		strings.Contains(message, "is not running") ||
		strings.Contains(message, "already stopped")
}

// prepareProjectedMounts 在默认投影根目录下准备 Docker bind mount。
func prepareProjectedMounts(input RunInput) (string, []mount.Mount, error) {
	return prepareProjectedMountsInRoot(projectedFilesRootDir, input)
}

// prepareProjectedMountsInRoot 通过暂存目录发布投影文件，并返回对应的 Docker bind mount。
func prepareProjectedMountsInRoot(rootDir string, input RunInput) (string, []mount.Mount, error) {
	projected := projectedfile.CloneFiles(input.ProjectedFiles)
	if len(projected) == 0 {
		return "", nil, nil
	}
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return "", nil, fmt.Errorf("projected files root dir is required")
	}
	executionID := strings.TrimSpace(input.ExecutionID)
	if executionID == "" {
		return "", nil, fmt.Errorf("executionID is required for projected files")
	}
	if !isSafePathSegment(executionID) {
		return "", nil, fmt.Errorf("executionID %q is not safe for projected files path", executionID)
	}

	executionDir := filepath.Join(rootDir, projectedExecutionsDir, executionID)
	finalDir := filepath.Join(executionDir, projectedFilesDir)
	if err := os.MkdirAll(executionDir, 0o700); err != nil {
		return "", nil, fmt.Errorf("create projected execution dir: %w", err)
	}
	stagingDir, err := os.MkdirTemp(executionDir, projectedStagingPrefix)
	if err != nil {
		return "", nil, fmt.Errorf("create projected files staging dir: %w", err)
	}
	mounts := make([]mount.Mount, 0, len(projected))
	for _, item := range projected {
		if err := item.Validate(); err != nil {
			_ = os.RemoveAll(stagingDir)
			return "", nil, err
		}
		relativePath := strings.TrimPrefix(item.MountPath, "/")
		sourcePath := filepath.Join(stagingDir, relativePath)
		if err := os.MkdirAll(filepath.Dir(sourcePath), 0o700); err != nil {
			_ = os.RemoveAll(stagingDir)
			return "", nil, fmt.Errorf("create projected file parent dir: %w", err)
		}
		if err := os.WriteFile(sourcePath, []byte(item.Content), os.FileMode(item.Mode)); err != nil {
			_ = os.RemoveAll(stagingDir)
			return "", nil, fmt.Errorf("write projected file %s: %w", item.MountPath, err)
		}
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   filepath.Join(finalDir, relativePath),
			Target:   item.MountPath,
			ReadOnly: true,
		})
	}
	if err := os.RemoveAll(finalDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return "", nil, fmt.Errorf("remove old projected files dir: %w", err)
	}
	if err := os.Rename(stagingDir, finalDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return "", nil, fmt.Errorf("publish projected files dir: %w", err)
	}
	return executionDir, mounts, nil
}

// buildContainerEnv 将环境变量 map 转换为 Docker 需要的 KEY=VALUE 列表。
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

// buildMiniCloudLabels 根据执行元数据构造 mini-cloud Docker 标签。
func buildMiniCloudLabels(input RunInput) map[string]string {
	labels := map[string]string{
		dockerLabelManagedBy: "node-agent",
	}
	addLabel(labels, dockerLabelNodeID, input.NodeID)
	addLabel(labels, dockerLabelExecutionID, input.ExecutionID)
	addLabel(labels, dockerLabelPlanID, input.PlanID)
	addLabel(labels, dockerLabelServiceID, input.ServiceID)
	projectionRef := strings.TrimSpace(input.ProjectionRef)
	if projectionRef == "" {
		projectionRef = strings.TrimSpace(input.ExecutionID)
	}
	addLabel(labels, dockerLabelProjectionRef, projectionRef)
	return labels
}

// addLabel 在 value 非空时向标签 map 写入键值。
func addLabel(labels map[string]string, key string, value string) {
	if value = strings.TrimSpace(value); value != "" {
		labels[key] = value
	}
}

// resolveContainerCommand 合并 Command 和 Args，得到传给 Docker 的容器命令。
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

// buildRegistryAuth 将镜像认证信息编码为 Docker RegistryAuth 字符串。
func buildRegistryAuth(credential *ImageCredential) (string, error) {
	if credential == nil {
		return "", nil
	}

	payload, err := json.Marshal(registry.AuthConfig{
		Username:      credential.Username,
		Password:      credential.Password,
		ServerAddress: credential.Server,
	})
	if err != nil {
		return "", fmt.Errorf("marshal registry auth config: %w", err)
	}
	return base64.URLEncoding.EncodeToString(payload), nil
}

// trackProjectionDir 记录容器和投影文件目录的关系，供后续清理使用。
func (d *Docker) trackProjectionDir(containerID string, dir string) {
	if strings.TrimSpace(containerID) == "" || strings.TrimSpace(dir) == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.projectionDirs[containerID] = trackedProjection{
		ExecutionID: filepath.Base(dir),
		Dir:         dir,
	}
}

// removeCreatedContainer 强制删除已创建但启动失败的 Docker 容器。
func (d *Docker) removeCreatedContainer(ctx context.Context, containerID string) {
	if strings.TrimSpace(containerID) == "" {
		return
	}
	cleanupCtx, cancelCleanup := context.WithTimeout(ctx, dockerCleanupTimeout)
	defer cancelCleanup()
	if err := d.client.ContainerRemove(cleanupCtx, containerID, container.RemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
		logctx.Logger(cleanupCtx, d.logger).Warn("remove created docker container failed",
			"container_id", containerID,
			"error", err,
		)
	}
}

// cleanupContainerProjectionDir 清理指定容器已跟踪的投影文件目录。
func (d *Docker) cleanupContainerProjectionDir(containerID string) {
	if strings.TrimSpace(containerID) == "" {
		return
	}
	d.mu.Lock()
	projection := d.projectionDirs[containerID]
	delete(d.projectionDirs, containerID)
	d.mu.Unlock()
	d.cleanupProjectionDir(containerID, projection.Dir)
}

// cleanupTrackedProjectionDirs 清理当前进程内仍被跟踪的全部投影文件目录。
func (d *Docker) cleanupTrackedProjectionDirs() {
	d.mu.Lock()
	items := make(map[string]trackedProjection, len(d.projectionDirs))
	for containerID, projection := range d.projectionDirs {
		items[containerID] = projection
		delete(d.projectionDirs, containerID)
	}
	d.mu.Unlock()
	for containerID, projection := range items {
		d.cleanupProjectionDir(containerID, projection.Dir)
	}
}

// activeTrackedExecutions 返回当前进程内仍被跟踪的执行 ID 集合。
func (d *Docker) activeTrackedExecutions() map[string]struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	active := make(map[string]struct{}, len(d.projectionDirs))
	for _, projection := range d.projectionDirs {
		if projection.ExecutionID != "" {
			active[projection.ExecutionID] = struct{}{}
		}
	}
	return active
}

// cleanupProjectionDir 删除指定投影文件目录并记录失败日志。
func (d *Docker) cleanupProjectionDir(containerID string, dir string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		d.logger.Warn("cleanup projected files dir failed",
			"container_id", containerID,
			"dir", dir,
			"error", err,
		)
	}
}

// isSafePathSegment 判断字符串是否可安全作为单级路径片段使用。
func isSafePathSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	return !strings.ContainsAny(value, `/\`)
}

// logLineWriter 将解复用后的单路日志字节流拆分为按行的 LogRecord。
type logLineWriter struct {
	// stream 是该 writer 对应的日志流名称。
	stream string
	// buffer 保存尚未遇到换行符的半行日志。
	buffer bytes.Buffer
	// emit 接收解析完成的日志行。
	emit LogEmitter
}

// newLogLineWriter 创建指定日志流的行解析 writer。
func newLogLineWriter(stream string, emit LogEmitter) *logLineWriter {
	return &logLineWriter{
		stream: stream,
		emit:   emit,
	}
}

// Write 累积字节并在遇到换行符时发出完整日志行。
func (w *logLineWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	_, _ = w.buffer.Write(p)
	for {
		data := w.buffer.Bytes()
		index := bytes.IndexByte(data, '\n')
		if index < 0 {
			break
		}
		line := string(data[:index])
		w.buffer.Next(index + 1)
		w.emitLine(line)
	}
	return len(p), nil
}

// Flush 发出缓冲区中最后一行未以换行符结尾的日志。
func (w *logLineWriter) Flush() {
	if w.buffer.Len() == 0 {
		return
	}
	line := w.buffer.String()
	w.buffer.Reset()
	w.emitLine(line)
}

// emitLine 解析 Docker 日志时间戳前缀并发出一条 LogRecord。
func (w *logLineWriter) emitLine(line string) {
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return
	}

	timestamp := time.Now().UTC()
	if space := strings.IndexByte(line, ' '); space > 0 {
		if parsed, err := time.Parse(time.RFC3339Nano, line[:space]); err == nil {
			timestamp = parsed.UTC()
			line = line[space+1:]
		}
	}
	if w.emit != nil {
		w.emit(LogRecord{
			Timestamp: timestamp,
			Stream:    w.stream,
			Line:      line,
		})
	}
}
