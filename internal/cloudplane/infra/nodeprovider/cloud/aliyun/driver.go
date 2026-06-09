package aliyun

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"text/template"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ecs20140526 "github.com/alibabacloud-go/ecs-20140526/v7/client"
	tea "github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/credentials-go/credentials"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/nodeprovider"
	"mini-cloud/internal/cloudplane/infra/nodeprovider/cloud/utils"
)

// Name 定义当前 cloud-plane 模块复用的常量。
const Name = "aliyun"

var nodeNoProxy = []string{
	"127.0.0.1",
	"localhost",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.169.254",
	"100.100.100.200",
}

// RuntimeConfig 描述阿里云 node provider driver 使用的配置。
type RuntimeConfig struct {
	// CloudPlane 表示 provider 创建 node 所需的 cloud-plane 配置。
	CloudPlane cloudplaneconfig.Config
	// ProviderSpec 是解析后的云厂商 node 创建参数。
	ProviderSpec NodeSpec
}

// NodeSpec 描述阿里云 node 创建参数。
type NodeSpec struct {
	// ImageID 表示 image 的唯一标识。
	ImageID string `json:"imageId"`
	// KeyPairName 是创建 ECS 实例时绑定的 SSH key pair 名称。
	KeyPairName string `json:"keyPairName"`
	// VSwitchID 表示 VSwitch 的唯一标识。
	VSwitchID string `json:"vSwitchId"`
	// SecurityGroupID 表示 security group 的唯一标识。
	SecurityGroupID string `json:"securityGroupId"`
	// SystemDiskCategory 是云服务器系统盘类型。
	SystemDiskCategory string `json:"systemDiskCategory"`
	// SystemDiskSizeGiB 是系统盘容量，单位 GiB。
	SystemDiskSizeGiB int `json:"systemDiskSizeGiB"`
}

// providerDriver 是阿里云 node 生命周期驱动实现。
type providerDriver struct {
	// client 是阿里云 ECS OpenAPI SDK client。
	client *ecs20140526.Client
	// config 记录组件运行所需配置。
	config RuntimeConfig
}

// instanceTypeCapacity 描述阿里云实例规格容量。
type instanceTypeCapacity struct {
	// instanceType 是资源规格或类型。
	instanceType string
	// cpuMilli 记录 CPU 资源，单位为 millicore。
	cpuMilli int
	// memoryMi 记录内存资源，单位为 MiB。
	memoryMi int
}

// NewDriver 构造阿里云 node provider driver。
// 参数说明：cfg 提供创建阿里云 node 所需的 cloud-plane 配置。
func NewDriver(cfg cloudplaneconfig.Config) (nodeprovider.Driver, error) {
	// 先解析 providerSpec 和通用运行配置，确保 driver 持有的是已校验配置。
	typedConfig, err := ParseRuntimeConfig(cfg)
	if err != nil {
		return nil, err
	}
	// ECS client 绑定到配置中的 region。
	client, err := newECSClient(typedConfig.CloudPlane.Infrastructure.RegionID)
	if err != nil {
		return nil, err
	}
	// 返回的 driver 只保存 SDK client 和已解析配置。
	return &providerDriver{
		client: client,
		config: typedConfig,
	}, nil
}

// ParseRuntimeConfig 解析并校验当前 provider 的 node 创建配置。
// 参数说明：cfg 提供当前组件配置。
func ParseRuntimeConfig(cfg cloudplaneconfig.Config) (RuntimeConfig, error) {
	// 防止把非 aliyun 配置误交给 aliyun driver。
	if !strings.EqualFold(strings.TrimSpace(cfg.Infrastructure.Provider), Name) {
		return RuntimeConfig{}, fmt.Errorf("runtime config provider %q does not match %q", cfg.Infrastructure.Provider, Name)
	}
	// bootstrap token 会通过 user-data 注入，并在实例内渲染到 node-agent 配置文件；缺失时新节点无法注册。
	if strings.TrimSpace(cfg.NodeAgent.BootstrapToken) == "" {
		return RuntimeConfig{}, fmt.Errorf("nodeAgent.bootstrapToken is required for aliyun node provider driver")
	}
	if strings.TrimSpace(cfg.RuntimeProvisioning.InstanceType) == "" {
		return RuntimeConfig{}, fmt.Errorf("runtimeProvisioning.instanceType is required for aliyun node provider driver")
	}

	// providerSpec 使用 aliyun 专属结构解析，并拒绝未知字段。
	var spec NodeSpec
	if err := cfg.RuntimeProvisioning.ParseProviderSpec(&spec); err != nil {
		return RuntimeConfig{}, err
	}

	// 缺省系统盘类型使用 ESSD。
	if spec.SystemDiskCategory == "" {
		spec.SystemDiskCategory = "cloud_essd"
	}
	// 缺省系统盘大小 40GiB，避免过小系统盘导致 bootstrap 空间不足。
	if spec.SystemDiskSizeGiB == 0 {
		spec.SystemDiskSizeGiB = 40
	}
	// image、vSwitch 和 security group 是 RunInstances 的 provider 专属必要参数。
	if strings.TrimSpace(spec.ImageID) == "" {
		return RuntimeConfig{}, fmt.Errorf("runtimeProvisioning.providerSpec.imageId is required for aliyun")
	}
	if strings.TrimSpace(spec.VSwitchID) == "" {
		return RuntimeConfig{}, fmt.Errorf("runtimeProvisioning.providerSpec.vSwitchId is required for aliyun")
	}
	if strings.TrimSpace(spec.SecurityGroupID) == "" {
		return RuntimeConfig{}, fmt.Errorf("runtimeProvisioning.providerSpec.securityGroupId is required for aliyun")
	}
	// 系统盘大小必须为正数。
	if spec.SystemDiskSizeGiB <= 0 {
		return RuntimeConfig{}, fmt.Errorf("runtimeProvisioning.providerSpec.systemDiskSizeGiB must be greater than 0")
	}

	// 返回通用配置和 aliyun 专属 spec 的组合。
	return RuntimeConfig{
		CloudPlane:   cfg,
		ProviderSpec: spec,
	}, nil
}

// Create 在阿里云侧创建一台 node 云主机并返回实例身份。
// 参数说明：ctx 当前阿里云实现不依赖 context；request 只包含创建云主机所需的名称、幂等 token 和资源需求。
func (p *providerDriver) Create(_ context.Context, request nodeprovider.CreateRequest) (nodeprovider.CreateResult, error) {
	// 阿里云 node provider driver 只负责校验请求、生成 cloud-init user-data、调用 RunInstances。
	// 本地 intent 创建、状态回写和等待 node-agent ready 由上层 node controller 负责。
	// client token 用于支持同一请求重试幂等；ownership tags 由 driver 根据平台身份统一生成。
	if p == nil || p.client == nil {
		return nodeprovider.CreateResult{}, fmt.Errorf("aliyun node provider driver is not initialized")
	}
	instanceName := strings.TrimSpace(request.Name)
	if instanceName == "" {
		return nodeprovider.CreateResult{}, fmt.Errorf("node name is required")
	}
	clientToken := strings.TrimSpace(request.ClientToken)
	if clientToken == "" {
		return nodeprovider.CreateResult{}, fmt.Errorf("node clientToken is required")
	}
	// 请求资源必须为正数，后续还会和实例规格容量比较。
	if request.CPUMilli <= 0 {
		return nodeprovider.CreateResult{}, fmt.Errorf("node cpuMilli must be greater than 0")
	}
	if request.MemoryMi <= 0 {
		return nodeprovider.CreateResult{}, fmt.Errorf("node memoryMi must be greater than 0")
	}

	// 查询实例规格容量，确保所选 instance type 至少能容纳单个 run 请求。
	capacity, err := p.lookupInstanceTypeCapacity()
	if err != nil {
		return nodeprovider.CreateResult{}, err
	}
	// 单台 node 当前按单个 run 容量需求创建；规格不足时提前失败。
	if request.CPUMilli > capacity.cpuMilli || request.MemoryMi > capacity.memoryMi {
		return nodeprovider.CreateResult{}, fmt.Errorf(
			"runtime profile instance type %s only has %dm cpu / %dMi memory, which cannot satisfy request %dm / %dMi",
			capacity.instanceType,
			capacity.cpuMilli,
			capacity.memoryMi,
			request.CPUMilli,
			request.MemoryMi,
		)
	}

	// 使用上层已经生成的云实例名渲染 cloud-init user-data。
	userData, err := p.buildNodeUserData(instanceName, capacity)
	if err != nil {
		return nodeprovider.CreateResult{}, err
	}

	// 构造阿里云 RunInstances 请求；client token 用于支持 provider 幂等，tags 用于归属识别。
	runRequest := &ecs20140526.RunInstancesRequest{
		RegionId:                new(p.config.CloudPlane.Infrastructure.RegionID),
		InstanceChargeType:      new("PostPaid"),
		AutoPay:                 new(true),
		ImageId:                 new(p.config.ProviderSpec.ImageID),
		InstanceType:            new(p.config.CloudPlane.RuntimeProvisioning.InstanceType),
		Amount:                  new(int32(1)),
		ClientToken:             new(clientToken),
		InstanceName:            new(instanceName),
		Description:             new("mini-cloud node"),
		HostName:                new(instanceName),
		KeyPairName:             optionalPtr(p.config.ProviderSpec.KeyPairName),
		SecurityGroupId:         new(p.config.ProviderSpec.SecurityGroupID),
		VSwitchId:               new(p.config.ProviderSpec.VSwitchID),
		UserData:                new(userData),
		InternetMaxBandwidthOut: new(int32(0)),
		SystemDisk: &ecs20140526.RunInstancesRequestSystemDisk{
			Category: new(p.config.ProviderSpec.SystemDiskCategory),
			Size:     new(strconv.Itoa(p.config.ProviderSpec.SystemDiskSizeGiB)),
		},
		Tag: buildAliyunRunInstanceTags(utils.BuildOwnershipTags(p.config.CloudPlane.Plane.Name)),
	}
	// zone 是可选配置，未配置时由阿里云根据 vSwitch 等参数决定。
	if zoneID := strings.TrimSpace(p.config.CloudPlane.Infrastructure.ZoneID); zoneID != "" {
		runRequest.ZoneId = new(zoneID)
	}

	// 调用 ECS 创建一台实例。
	response, err := p.client.RunInstances(runRequest)
	if err != nil {
		return nodeprovider.CreateResult{}, fmt.Errorf("RunInstances failed: %s", formatAliyunSDKError(err))
	}
	// RunInstances 必须返回至少一个 instanceID。
	if response.Body == nil || response.Body.InstanceIdSets == nil || len(response.Body.InstanceIdSets.InstanceIdSet) == 0 || response.Body.InstanceIdSets.InstanceIdSet[0] == nil {
		return nodeprovider.CreateResult{}, fmt.Errorf("RunInstances returned no instance id")
	}

	// 取本次创建的第一台实例 ID；请求 Amount 固定为 1。
	instanceID := tea.StringValue(response.Body.InstanceIdSets.InstanceIdSet[0])
	// 返回云侧实例身份，上层负责写回本地 node 记录并等待 node-agent ready。
	return nodeprovider.CreateResult{
		InstanceID:   instanceID,
		InstanceName: instanceName,
		InstanceType: p.config.CloudPlane.RuntimeProvisioning.InstanceType,
	}, nil
}

// List 按 mini-cloud ownership 标签分页查询当前 region 下归属本平台的 ECS node。
// 参数说明：ctx 按接口签名保留，当前实现未传入阿里云 SDK 查询。
func (p *providerDriver) List(_ context.Context) ([]nodeprovider.Node, error) {
	// driver 或 SDK client 缺失时无法查询云资源。
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("aliyun node provider driver is not initialized")
	}

	// 只查询带 mini-cloud ownership 标签的平台 node。
	filters := buildAliyunDescribeInstanceTags(utils.BuildOwnershipTags(p.config.CloudPlane.Plane.Name))
	out := make([]nodeprovider.Node, 0)
	nextToken := ""

	// DescribeInstances 使用分页；nextToken 为空表示第一页或已经结束。
	for {
		req := &ecs20140526.DescribeInstancesRequest{
			RegionId:   new(p.config.CloudPlane.Infrastructure.RegionID),
			MaxResults: new(int32(100)),
			Tag:        filters,
		}
		if strings.TrimSpace(nextToken) != "" {
			req.NextToken = new(nextToken)
		}

		// 按 ownership 标签查询实例。
		resp, err := p.client.DescribeInstances(req)
		if err != nil {
			return nil, fmt.Errorf("DescribeInstances for mini-cloud nodes failed: %s", formatAliyunSDKError(err))
		}
		// body 或 instances 为空时视为没有更多结果。
		if resp.Body == nil || resp.Body.Instances == nil {
			break
		}

		// 将每个 ECS 实例转换为 nodeprovider 统一 node 视图。
		for _, item := range resp.Body.Instances.Instance {
			if item == nil {
				continue
			}
			// 保留云实例原始标签，供上层做 ownership 诊断或校验。
			tags := aliyunTagMap(item)
			out = append(out, nodeprovider.Node{
				InstanceID:   tea.StringValue(item.InstanceId),
				InstanceName: tea.StringValue(item.InstanceName),
				InstanceType: tea.StringValue(item.InstanceType),
				Tags:         tags,
			})
		}

		// 没有 nextToken 时分页结束。
		nextToken = tea.StringValue(resp.Body.NextToken)
		if nextToken == "" {
			break
		}
	}

	// 返回所有匹配 ownership 标签的实例视图。
	return out, nil
}

// Delete 在阿里云侧释放一台 node 云主机。
// 参数说明：ctx 用于在调用 SDK 前响应上层取消；request 只包含要删除的云实例 ID。
func (p *providerDriver) Delete(ctx context.Context, request nodeprovider.DeleteRequest) error {
	// driver 或 SDK client 缺失时不能执行云资源删除。
	if p == nil || p.client == nil {
		return fmt.Errorf("aliyun node provider driver is not initialized")
	}
	// Delete 只按云实例 ID 操作，不接收 service/run 等业务归属。
	instanceID := strings.TrimSpace(request.InstanceID)
	if instanceID == "" {
		return fmt.Errorf("node instanceID is required")
	}
	// 阿里云 SDK 方法本身没有 context 参数；调用前先检查上层是否已经取消。
	if err := ctx.Err(); err != nil {
		return err
	}

	// DeleteInstances 支持删除运行中的按量实例；node 已经由 cloud-plane drain，不再承载 active execution。
	req := &ecs20140526.DeleteInstancesRequest{
		RegionId:   new(p.config.CloudPlane.Infrastructure.RegionID),
		InstanceId: []*string{new(instanceID)},
		Force:      new(true),
		ForceStop:  new(false),
	}
	if _, err := p.client.DeleteInstances(req); err != nil {
		if isAliyunNodeNotFound(err) {
			// 云侧实例已经不存在时按幂等成功处理，让本地状态收敛为 deleted。
			return nil
		}
		return fmt.Errorf("DeleteInstances failed: %s", formatAliyunSDKError(err))
	}
	return nil
}

// lookupInstanceTypeCapacity 查询配置实例规格的 CPU 和内存容量。
func (p *providerDriver) lookupInstanceTypeCapacity() (instanceTypeCapacity, error) {
	// 查询配置中 instance type 的规格信息。
	response, err := p.client.DescribeInstanceTypes(&ecs20140526.DescribeInstanceTypesRequest{
		InstanceTypes: []*string{new(p.config.CloudPlane.RuntimeProvisioning.InstanceType)},
		MaxResults:    new(int64(1)),
	})
	if err != nil {
		return instanceTypeCapacity{}, fmt.Errorf("DescribeInstanceTypes failed: %s", formatAliyunSDKError(err))
	}
	// 响应必须包含目标 instance type。
	if response.Body == nil || response.Body.InstanceTypes == nil || len(response.Body.InstanceTypes.InstanceType) == 0 || response.Body.InstanceTypes.InstanceType[0] == nil {
		return instanceTypeCapacity{}, fmt.Errorf("DescribeInstanceTypes did not return runtime instance type %q", p.config.CloudPlane.RuntimeProvisioning.InstanceType)
	}

	// 阿里云返回 CPU core 和 GiB 内存，这里转换成 cloud-plane 使用的 millicore/MiB。
	item := response.Body.InstanceTypes.InstanceType[0]
	cpuMilli := int(tea.Int32Value(item.CpuCoreCount)) * 1000
	memoryMi := int(math.Round(float64(tea.Float32Value(item.MemorySize)) * 1024))
	// 防御 SDK 返回异常容量。
	if cpuMilli <= 0 || memoryMi <= 0 {
		return instanceTypeCapacity{}, fmt.Errorf("DescribeInstanceTypes returned invalid capacity for %q", p.config.CloudPlane.RuntimeProvisioning.InstanceType)
	}

	// 返回规范化后的容量。
	return instanceTypeCapacity{
		instanceType: tea.StringValue(item.InstanceTypeId),
		cpuMilli:     cpuMilli,
		memoryMi:     memoryMi,
	}, nil
}

//go:embed node_bootstrap.sh.tmpl
var nodeBootstrapTemplate string

// nodeBootstrapData 是渲染阿里云 node bootstrap 模板所需的数据。
type nodeBootstrapData struct {
	InstallRoot             string
	AgentBinaryURL          string
	DockerDaemonJSONBase64  string
	EgressProxyEnabledShell string
	EgressProxyEnabledYAML  string
	EgressProxyEndpoint     string
	NoProxyValue            string
	BootstrapToken          string
	BootstrapLog            string
	MetadataBase            string
	InstanceName            string
	ConnectEndpoint         string
	PlatformName            string
	Provider                string
	Region                  string
	InstanceType            string
	CPUMilli                int
	MemoryMi                int
	HeartbeatInterval       string
	WorkInterval            string
	HostPortMin             int
	HostPortMax             int
	NoProxyItems            []string
	WorkloadLogLokiURL      string
	WorkloadLogLokiTenantID string
	WorkloadOTLPEndpoint    string
	NodeAgentBinaryPath     string
	NodeAgentConfigPath     string
}

// buildNodeUserData 渲染阿里云 node 首次启动时执行的 user-data 脚本。
// 参数说明：instanceName 是云厂商实例名称；capacity 是要写入 node-agent 配置的节点容量。
func (p *providerDriver) buildNodeUserData(instanceName string, capacity instanceTypeCapacity) (string, error) {
	// Docker daemon 配置先序列化为 JSON，再以 base64 传给 shell，避免模板处理 JSON 引号和换行。
	dockerDaemonJSON, err := utils.BuildDockerDaemonJSON(p.config.CloudPlane.RuntimeProvisioning.RegistryMirrors)
	if err != nil {
		return "", err
	}

	// proxy 配置既要写入 shell 环境，也要写入 node-agent YAML；两处使用同一份输入。
	proxy := p.config.CloudPlane.RuntimeProvisioning
	egressProxyEnabled := strings.TrimSpace(proxy.EgressProxyEndpoint) != ""
	data := nodeBootstrapData{
		InstallRoot:             utils.ShellQuote("/opt/mini-cloud"),
		AgentBinaryURL:          utils.ShellQuote(p.config.CloudPlane.NodeAgent.BinaryURL),
		DockerDaemonJSONBase64:  utils.ShellQuote(base64.StdEncoding.EncodeToString([]byte(dockerDaemonJSON))),
		EgressProxyEnabledShell: utils.ShellQuote(strconv.FormatBool(egressProxyEnabled)),
		EgressProxyEnabledYAML:  strconv.FormatBool(egressProxyEnabled),
		EgressProxyEndpoint:     utils.ShellQuote(strings.TrimSpace(proxy.EgressProxyEndpoint)),
		NoProxyValue:            utils.ShellQuote(strings.Join(nodeNoProxy, ",")),
		BootstrapToken:          utils.ShellQuote(strings.TrimSpace(p.config.CloudPlane.NodeAgent.BootstrapToken)),
		BootstrapLog:            utils.ShellQuote("/var/log/mini-cloud-node-bootstrap.log"),
		MetadataBase:            utils.ShellQuote("http://100.100.100.200/latest"),
		InstanceName:            utils.ShellQuote(instanceName),
		ConnectEndpoint:         utils.ShellQuote(strings.TrimRight(p.config.CloudPlane.NodeAgent.ConnectEndpoint, "/")),
		PlatformName:            utils.ShellQuote(p.config.CloudPlane.Plane.Name),
		Provider:                utils.ShellQuote(p.config.CloudPlane.Infrastructure.Provider),
		Region:                  utils.ShellQuote(p.config.CloudPlane.Infrastructure.RegionID),
		InstanceType:            utils.ShellQuote(capacity.instanceType),
		CPUMilli:                capacity.cpuMilli,
		MemoryMi:                capacity.memoryMi,
		HeartbeatInterval:       utils.ShellQuote(strconv.Itoa(cloudplaneconfig.NodeAgentHeartbeatIntervalSeconds) + "s"),
		WorkInterval:            utils.ShellQuote(strconv.Itoa(cloudplaneconfig.NodeAgentWorkIntervalSeconds) + "s"),
		HostPortMin:             cloudplaneconfig.NodeAgentHostPortMin,
		HostPortMax:             cloudplaneconfig.NodeAgentHostPortMax,
		NoProxyItems:            utils.ShellQuoteItems(nodeNoProxy),
		WorkloadLogLokiURL:      utils.ShellQuote(strings.TrimSpace(p.config.CloudPlane.Observability.LokiURL)),
		WorkloadLogLokiTenantID: utils.ShellQuote(""),
		WorkloadOTLPEndpoint:    utils.ShellQuote(strings.TrimSpace(p.config.CloudPlane.Observability.OTLPEndpoint)),
		NodeAgentBinaryPath:     utils.ShellQuote("/opt/mini-cloud/bin/node-agent"),
		NodeAgentConfigPath:     utils.ShellQuote("/opt/mini-cloud/node-agent.yaml"),
	}

	// 模板文件是独立 shell 脚本，Go 只负责渲染变量，不再逐行拼接脚本。
	tmpl, err := template.New("aliyun-node-bootstrap").Option("missingkey=error").Parse(nodeBootstrapTemplate)
	if err != nil {
		return "", fmt.Errorf("parse aliyun node bootstrap template: %w", err)
	}
	var script bytes.Buffer
	if err := tmpl.Execute(&script, data); err != nil {
		return "", fmt.Errorf("render aliyun node bootstrap template: %w", err)
	}

	// 阿里云 user-data 接口接收 base64 编码后的脚本内容。
	return base64.StdEncoding.EncodeToString(script.Bytes()), nil
}

// newECSClient 使用默认凭据链和指定 region endpoint 构造 ECS SDK client。
// 参数说明：regionID 是云厂商地域标识。
func newECSClient(regionID string) (*ecs20140526.Client, error) {
	// 使用阿里云默认凭据链创建 credential。
	credential, err := credentials.NewCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("create aliyun credential: %w", err)
	}
	// ECS OpenAPI endpoint 根据 region 生成。
	client, err := ecs20140526.NewClient(&openapi.Config{
		Endpoint:   new(resolveECSEndpoint(regionID)),
		Credential: credential,
	})
	if err != nil {
		return nil, fmt.Errorf("create ecs client: %w", err)
	}
	// 返回已绑定 endpoint 和 credential 的 ECS client。
	return client, nil
}

// resolveECSEndpoint 根据 region 生成 ECS OpenAPI endpoint。
// 参数说明：regionID 是云厂商地域标识。
func resolveECSEndpoint(regionID string) string {
	return fmt.Sprintf("ecs.%s.aliyuncs.com", regionID)
}

// buildAliyunRunInstanceTags 将 nodeprovider ownership 标签转换为阿里云 RunInstances tag 结构。
// 参数说明：tags 是 nodeprovider 生成的标准 ownership 标签。
func buildAliyunRunInstanceTags(tags map[string]string) []*ecs20140526.RunInstancesRequestTag {
	result := make([]*ecs20140526.RunInstancesRequestTag, 0, len(tags))
	for key, value := range tags {
		// 阿里云 tag value 为空时跳过，避免写入无意义标签。
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		// 转换成 RunInstances 使用的 tag 结构。
		result = append(result, &ecs20140526.RunInstancesRequestTag{
			Key:   new(key),
			Value: new(value),
		})
	}
	// map 遍历顺序不保证稳定；标签语义不依赖顺序。
	return result
}

// buildAliyunDescribeInstanceTags 构造查询 ECS 实例时使用的 ownership 标签过滤条件。
// 参数说明：filters 是待转换为 DescribeInstances tag filter 的 key/value 条件。
func buildAliyunDescribeInstanceTags(filters map[string]string) []*ecs20140526.DescribeInstancesRequestTag {
	// 将通用 ownership filter 转成 DescribeInstances tag filter。
	result := make([]*ecs20140526.DescribeInstancesRequestTag, 0, len(filters))
	for key, value := range filters {
		// 空 value 不参与过滤。
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		// 使用 SDK setter 构造查询 tag。
		result = append(result, (&ecs20140526.DescribeInstancesRequestTag{}).
			SetKey(key).
			SetValue(value))
	}
	// map 遍历顺序不保证稳定；查询语义不依赖顺序。
	return result
}

// aliyunTagMap 将阿里云 tag 列表转换为 key/value map。
// 参数说明：instance 是 DescribeInstances 返回的 ECS 实例对象。
func aliyunTagMap(instance *ecs20140526.DescribeInstancesResponseBodyInstancesInstance) map[string]string {
	// nil instance 或无 tags 时返回空 map，方便调用方直接按 key 读取。
	out := make(map[string]string)
	if instance == nil || instance.Tags == nil {
		return out
	}
	// 遍历 SDK tag 列表，清理 key/value 外围空白。
	for _, tag := range instance.Tags.Tag {
		if tag == nil {
			continue
		}
		key := strings.TrimSpace(tea.StringValue(tag.TagKey))
		value := strings.TrimSpace(tea.StringValue(tag.TagValue))
		// 空 key 无法作为 ownership 字段，直接忽略。
		if key == "" {
			continue
		}
		out[key] = value
	}
	// 返回普通 map 供 nodeprovider 统一读取。
	return out
}

// formatAliyunSDKError 优先格式化阿里云 SDKError 的 code/message/data，非 SDKError 退回 err.Error。
// 参数说明：err 是需要转换或包装的错误。
func formatAliyunSDKError(err error) string {
	// nil 错误没有可格式化内容。
	if err == nil {
		return ""
	}
	// SDKError 中包含 code/message/data，优先使用这些结构化字段。
	if sdkErr, ok := errors.AsType[*tea.SDKError](err); ok {
		return fmt.Sprintf("sdk code=%s message=%s data=%v", tea.StringValue(sdkErr.Code), tea.StringValue(sdkErr.Message), sdkErr.Data)
	}
	// 非 SDKError 退回 err.Error。
	return err.Error()
}

// sdkErrorCode 从云厂商 SDK error 中提取错误码。
// 参数说明：err 是需要转换或包装的错误。
func sdkErrorCode(err error) string {
	if sdkErr, ok := errors.AsType[*tea.SDKError](err); !ok {
		return ""
	} else {
		return tea.StringValue(sdkErr.Code)
	}
}

// isAliyunNodeNotFound 判断阿里云返回是否表示实例不存在。
// 参数说明：err 是需要转换或包装的错误。
func isAliyunNodeNotFound(err error) bool {
	// 同时检查 SDK code 和 message，兼容不同 API 返回的 not found 表达。
	code := strings.ToLower(sdkErrorCode(err))
	message := strings.ToLower(err.Error())
	return strings.Contains(code, "notfound") ||
		strings.Contains(code, "invalidinstanceid.notfound") ||
		strings.Contains(message, "not found after write")
}

// optionalPtr 将非空字符串转换为 SDK 可选字符串指针。
// 参数说明：value 是待写入 SDK 请求的可选字符串。
func optionalPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
