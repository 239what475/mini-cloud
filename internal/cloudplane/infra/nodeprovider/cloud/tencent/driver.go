package tencent

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

	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	tcprofile "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	cvm "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm/v20170312"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/nodeprovider"
	"mini-cloud/internal/cloudplane/infra/nodeprovider/cloud/utils"
)

// Name 定义当前 cloud-plane 模块复用的常量。
const Name = "tencent"

var nodeNoProxy = []string{
	"127.0.0.1",
	"localhost",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.169.254",
	"metadata.tencentyun.com",
}

// RuntimeConfig 描述腾讯云 node provider driver 使用的配置。
type RuntimeConfig struct {
	// CloudPlane 表示 provider 创建 node 所需的 cloud-plane 配置。
	CloudPlane cloudplaneconfig.Config
	// ProviderSpec 是解析后的云厂商 node 创建参数。
	ProviderSpec NodeSpec
}

// NodeSpec 描述 tencent providerSpec 中的 CVM 创建参数。
type NodeSpec struct {
	// ImageID 表示 image 的唯一标识。
	ImageID string `json:"imageId"`
	// KeyIDs 是创建 CVM 实例时绑定的 SSH key ID 集合。
	KeyIDs []string `json:"keyIds"`
	// VPCID 表示 VPC 的唯一标识。
	VPCID string `json:"vpcId"`
	// SubnetID 表示 subnet 的唯一标识。
	SubnetID string `json:"subnetId"`
	// SecurityGroupIDs 表示 security group 的唯一标识集合。
	SecurityGroupIDs []string `json:"securityGroupIds"`
	// SystemDiskType 是云服务器系统盘类型。
	SystemDiskType string `json:"systemDiskType"`
	// SystemDiskSizeGiB 是系统盘容量，单位 GiB。
	SystemDiskSizeGiB int64 `json:"systemDiskSizeGiB"`
}

// providerDriver 是腾讯云 node 生命周期驱动实现。
type providerDriver struct {
	// client 是腾讯云 CVM OpenAPI SDK client。
	client *cvm.Client
	// config 记录组件运行所需配置。
	config RuntimeConfig
}

// instanceTypeCapacity 描述腾讯云实例规格容量。
type instanceTypeCapacity struct {
	// instanceType 是资源规格或类型。
	instanceType string
	// cpuMilli 记录 CPU 资源，单位为 millicore。
	cpuMilli int
	// memoryMi 记录内存资源，单位为 MiB。
	memoryMi int
}

// NewDriver 构造腾讯云 node provider driver。
// 参数说明：cfg 提供创建腾讯云 node 所需的 cloud-plane 配置。
func NewDriver(cfg cloudplaneconfig.Config) (nodeprovider.Driver, error) {
	// 先解析 providerSpec 和通用运行配置，确保 driver 持有的是已校验配置。
	typedConfig, err := ParseRuntimeConfig(cfg)
	if err != nil {
		return nil, err
	}
	// CVM client 绑定到配置中的 region。
	client, err := newCVMClient(typedConfig.CloudPlane.Infrastructure.RegionID)
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
	// 防止把非 tencent 配置误交给 tencent driver。
	if !strings.EqualFold(strings.TrimSpace(cfg.Infrastructure.Provider), Name) {
		return RuntimeConfig{}, fmt.Errorf("runtime config provider %q does not match %q", cfg.Infrastructure.Provider, Name)
	}
	// bootstrap token 会通过 user-data 注入，并在实例内渲染到 node-agent 配置文件；缺失时新节点无法注册。
	if strings.TrimSpace(cfg.NodeAgent.BootstrapToken) == "" {
		return RuntimeConfig{}, fmt.Errorf("nodeAgent.bootstrapToken is required for tencent node provider driver")
	}
	if strings.TrimSpace(cfg.RuntimeProvisioning.InstanceType) == "" {
		return RuntimeConfig{}, fmt.Errorf("runtimeProvisioning.instanceType is required for tencent node provider driver")
	}

	// providerSpec 使用 tencent 专属结构解析，并拒绝未知字段。
	var spec NodeSpec
	if err := cfg.RuntimeProvisioning.ParseProviderSpec(&spec); err != nil {
		return RuntimeConfig{}, err
	}

	// 缺省系统盘类型使用高性能云硬盘。
	if spec.SystemDiskType == "" {
		spec.SystemDiskType = "CLOUD_PREMIUM"
	}
	// 缺省系统盘大小 50GiB，避免过小系统盘导致 bootstrap 空间不足。
	if spec.SystemDiskSizeGiB == 0 {
		spec.SystemDiskSizeGiB = 50
	}
	// image、VPC、subnet 和 security group 是 RunInstances 的 provider 专属必要参数。
	if strings.TrimSpace(spec.ImageID) == "" {
		return RuntimeConfig{}, fmt.Errorf("runtimeProvisioning.providerSpec.imageId is required for tencent")
	}
	if strings.TrimSpace(spec.VPCID) == "" {
		return RuntimeConfig{}, fmt.Errorf("runtimeProvisioning.providerSpec.vpcId is required for tencent")
	}
	if strings.TrimSpace(spec.SubnetID) == "" {
		return RuntimeConfig{}, fmt.Errorf("runtimeProvisioning.providerSpec.subnetId is required for tencent")
	}
	if len(spec.SecurityGroupIDs) == 0 {
		return RuntimeConfig{}, fmt.Errorf("runtimeProvisioning.providerSpec.securityGroupIds is required for tencent")
	}
	// 系统盘大小必须为正数。
	if spec.SystemDiskSizeGiB <= 0 {
		return RuntimeConfig{}, fmt.Errorf("runtimeProvisioning.providerSpec.systemDiskSizeGiB must be greater than 0")
	}
	// 返回通用配置和 tencent 专属 spec 的组合。
	return RuntimeConfig{
		CloudPlane:   cfg,
		ProviderSpec: spec,
	}, nil
}

// Create 在腾讯云侧创建一台 node 云主机并返回实例身份。
// 参数说明：ctx 当前腾讯云实现不依赖 context；request 只包含创建云主机所需的名称、幂等 token 和资源需求。
func (p *providerDriver) Create(_ context.Context, request nodeprovider.CreateRequest) (nodeprovider.CreateResult, error) {
	// 腾讯云 node provider driver 只负责校验请求、生成 cloud-init user-data、调用 RunInstances。
	// 本地 intent 创建、状态回写和等待 node-agent ready 由上层 node controller 负责。
	// client token 用于支持同一请求重试幂等；ownership tags 由 driver 根据平台身份统一生成。
	if p == nil || p.client == nil {
		return nodeprovider.CreateResult{}, fmt.Errorf("tencent node provider driver is not initialized")
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

	// 构造腾讯云 RunInstances 请求；client token 用于支持 provider 幂等，tags 用于归属识别。
	runRequest := cvm.NewRunInstancesRequest()
	runRequest.InstanceChargeType = new("POSTPAID_BY_HOUR")
	runRequest.ImageId = new(p.config.ProviderSpec.ImageID)
	runRequest.InstanceType = new(p.config.CloudPlane.RuntimeProvisioning.InstanceType)
	runRequest.InstanceCount = new(int64(1))
	runRequest.InstanceName = new(instanceName)
	runRequest.HostName = new(buildNodeHostName(instanceName))
	runRequest.ClientToken = new(clientToken)
	runRequest.SecurityGroupIds = stringPtrs(p.config.ProviderSpec.SecurityGroupIDs)
	runRequest.UserData = new(userData)
	runRequest.SystemDisk = &cvm.SystemDisk{
		DiskType: new(p.config.ProviderSpec.SystemDiskType),
		DiskSize: new(p.config.ProviderSpec.SystemDiskSizeGiB),
	}
	runRequest.VirtualPrivateCloud = &cvm.VirtualPrivateCloud{
		VpcId:    new(p.config.ProviderSpec.VPCID),
		SubnetId: new(p.config.ProviderSpec.SubnetID),
	}
	runRequest.InternetAccessible = &cvm.InternetAccessible{
		PublicIpAssigned:        new(false),
		InternetMaxBandwidthOut: new(int64(0)),
	}
	runRequest.EnhancedService = &cvm.EnhancedService{
		SecurityService: &cvm.RunSecurityServiceEnabled{
			Enabled: new(true),
		},
		MonitorService: &cvm.RunMonitorServiceEnabled{
			Enabled: new(true),
		},
	}
	runRequest.TagSpecification = []*cvm.TagSpecification{
		{
			ResourceType: new("instance"),
			Tags:         buildTencentRunInstanceTags(utils.BuildOwnershipTags(p.config.CloudPlane.Plane.Name)),
		},
	}
	// zone 是可选配置，未配置时由腾讯云根据 subnet 等参数决定。
	if zoneID := strings.TrimSpace(p.config.CloudPlane.Infrastructure.ZoneID); zoneID != "" {
		runRequest.Placement = &cvm.Placement{
			Zone: new(zoneID),
		}
	}
	// SSH key 是可选配置，未配置时不写 LoginSettings。
	if len(p.config.ProviderSpec.KeyIDs) > 0 {
		runRequest.LoginSettings = &cvm.LoginSettings{
			KeyIds: stringPtrs(p.config.ProviderSpec.KeyIDs),
		}
	}

	// 调用 CVM 创建一台实例。
	response, err := p.client.RunInstances(runRequest)
	if err != nil {
		return nodeprovider.CreateResult{}, fmt.Errorf("RunInstances failed: %s", formatTencentSDKError(err))
	}
	// RunInstances 必须返回至少一个 instanceID。
	if response == nil || response.Response == nil || len(response.Response.InstanceIdSet) == 0 || response.Response.InstanceIdSet[0] == nil {
		return nodeprovider.CreateResult{}, fmt.Errorf("RunInstances returned no instance id")
	}

	// 取本次创建的第一台实例 ID；请求 InstanceCount 固定为 1。
	instanceID := valueString(response.Response.InstanceIdSet[0])
	// 返回云侧实例身份，上层负责写回本地 node 记录并等待 node-agent ready。
	return nodeprovider.CreateResult{
		InstanceID:   instanceID,
		InstanceName: instanceName,
		InstanceType: p.config.CloudPlane.RuntimeProvisioning.InstanceType,
	}, nil
}

// Delete 在腾讯云侧退还一台 node 云主机。
// 参数说明：ctx 用于在调用 SDK 前响应上层取消；request 只包含要删除的云实例 ID。
func (p *providerDriver) Delete(ctx context.Context, request nodeprovider.DeleteRequest) error {
	// driver 或 SDK client 缺失时不能执行云资源删除。
	if p == nil || p.client == nil {
		return fmt.Errorf("tencent node provider driver is not initialized")
	}
	// Delete 只按云实例 ID 操作，不接收 service/run 等业务归属。
	instanceID := strings.TrimSpace(request.InstanceID)
	if instanceID == "" {
		return fmt.Errorf("node instanceID is required")
	}
	// 腾讯云 SDK 方法本身没有 context 参数；调用前先检查上层是否已经取消。
	if err := ctx.Err(); err != nil {
		return err
	}

	// TerminateInstances 会退还按量 CVM；node 已经由 cloud-plane drain，不再承载 active execution。
	req := cvm.NewTerminateInstancesRequest()
	req.InstanceIds = []*string{new(instanceID)}
	response, err := p.client.TerminateInstances(req)
	if err != nil {
		if isTencentNodeNotFound(err) {
			// 云侧实例已经不存在时按幂等成功处理，让本地状态收敛为 deleted。
			return nil
		}
		return fmt.Errorf("TerminateInstances failed: %s", formatTencentSDKError(err))
	}
	if response == nil || response.Response == nil {
		return fmt.Errorf("TerminateInstances returned empty response")
	}
	return nil
}

// lookupInstanceTypeCapacity 查询配置实例规格的 CPU 和内存容量。
func (p *providerDriver) lookupInstanceTypeCapacity() (instanceTypeCapacity, error) {
	// 查询配置中 instance type 的规格信息。
	request := cvm.NewDescribeInstanceTypeConfigsRequest()
	request.Filters = []*cvm.Filter{
		{
			Name:   new("instance-type"),
			Values: []*string{new(p.config.CloudPlane.RuntimeProvisioning.InstanceType)},
		},
	}
	// zone 是可选过滤条件；配置后可避免拿到其他 zone 不支持的规格信息。
	if zoneID := strings.TrimSpace(p.config.CloudPlane.Infrastructure.ZoneID); zoneID != "" {
		request.Filters = append(request.Filters, &cvm.Filter{
			Name:   new("zone"),
			Values: []*string{new(zoneID)},
		})
	}

	// 调用 CVM 规格查询接口。
	response, err := p.client.DescribeInstanceTypeConfigs(request)
	if err != nil {
		return instanceTypeCapacity{}, fmt.Errorf("DescribeInstanceTypeConfigs failed: %s", formatTencentSDKError(err))
	}
	// 响应必须包含目标 instance type。
	if response == nil || response.Response == nil || len(response.Response.InstanceTypeConfigSet) == 0 || response.Response.InstanceTypeConfigSet[0] == nil {
		return instanceTypeCapacity{}, fmt.Errorf("DescribeInstanceTypeConfigs did not return runtime instance type %q", p.config.CloudPlane.RuntimeProvisioning.InstanceType)
	}

	// 腾讯云返回 CPU core 和 GiB 内存，这里转换成 cloud-plane 使用的 millicore/MiB。
	item := response.Response.InstanceTypeConfigSet[0]
	cpuMilli := int(valueInt64(item.CPU)) * 1000
	memoryMi := int(math.Round(float64(valueInt64(item.Memory)) * 1024))
	// 防御 SDK 返回异常容量。
	if cpuMilli <= 0 || memoryMi <= 0 {
		return instanceTypeCapacity{}, fmt.Errorf("DescribeInstanceTypeConfigs returned invalid capacity for %q", p.config.CloudPlane.RuntimeProvisioning.InstanceType)
	}

	// 返回规范化后的容量。
	return instanceTypeCapacity{
		instanceType: valueString(item.InstanceType),
		cpuMilli:     cpuMilli,
		memoryMi:     memoryMi,
	}, nil
}

//go:embed node_bootstrap.sh.tmpl
var nodeBootstrapTemplate string

// nodeBootstrapData 是渲染腾讯云 node bootstrap 模板所需的数据。
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

// buildNodeUserData 渲染腾讯云 node 首次启动时执行的 user-data 脚本。
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
		MetadataBase:            utils.ShellQuote("http://metadata.tencentyun.com/latest/meta-data"),
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
	tmpl, err := template.New("tencent-node-bootstrap").Option("missingkey=error").Parse(nodeBootstrapTemplate)
	if err != nil {
		return "", fmt.Errorf("parse tencent node bootstrap template: %w", err)
	}
	var script bytes.Buffer
	if err := tmpl.Execute(&script, data); err != nil {
		return "", fmt.Errorf("render tencent node bootstrap template: %w", err)
	}

	// 腾讯云 user-data 接口接收 base64 编码后的脚本内容。
	return base64.StdEncoding.EncodeToString(script.Bytes()), nil
}

// newCVMClient 构造绑定指定 region 的腾讯云 CVM client。
// 参数说明：regionID 是云厂商地域标识。
func newCVMClient(regionID string) (*cvm.Client, error) {
	// 使用项目内腾讯云凭据解析逻辑创建 credential。
	credential, err := ResolveCredential()
	if err != nil {
		return nil, fmt.Errorf("create tencent credential: %w", err)
	}

	// CVM endpoint 当前使用腾讯云公共 OpenAPI endpoint。
	clientProfile := tcprofile.NewClientProfile()
	clientProfile.HttpProfile.Endpoint = "cvm.tencentcloudapi.com"

	// SDK client 绑定 region、credential 和 profile。
	client, err := cvm.NewClient(credential, regionID, clientProfile)
	if err != nil {
		return nil, fmt.Errorf("create cvm client: %w", err)
	}
	// 返回可直接调用 CVM API 的 client。
	return client, nil
}

// buildNodeHostName 生成满足腾讯云主机名限制的 node hostname。
// 参数说明：instanceName 是云厂商实例名称。
func buildNodeHostName(instanceName string) string {
	// 腾讯云 hostname 不应以点或横线开头/结尾。
	hostName := strings.Trim(strings.TrimSpace(instanceName), ".-")
	// 腾讯云 Linux 实例 hostname 长度上限按 60 字符处理。
	if len(hostName) > 60 {
		hostName = strings.Trim(hostName[:60], ".-")
	}
	// 裁剪后过短时退回固定安全名称。
	if len(hostName) < 2 {
		return "mini-cloud-node"
	}
	// 返回可用于 RunInstances HostName 字段的值。
	return hostName
}

// buildTencentRunInstanceTags 将 nodeprovider ownership 标签转换为腾讯云 RunInstances tag 结构。
// 参数说明：tags 是 nodeprovider 生成的标准 ownership 标签。
func buildTencentRunInstanceTags(tags map[string]string) []*cvm.Tag {
	result := make([]*cvm.Tag, 0, len(tags))
	for key, value := range tags {
		// 腾讯云 tag value 为空时跳过，避免写入无意义标签。
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		// 转换成 RunInstances 使用的 tag 结构。
		result = append(result, &cvm.Tag{
			Key:   new(key),
			Value: new(value),
		})
	}
	// map 遍历顺序不保证稳定；标签语义不依赖顺序。
	return result
}

// formatTencentSDKError 提取腾讯云 SDK 错误码和 request id，生成可读错误。
// 参数说明：err 是需要转换或包装的错误。
func formatTencentSDKError(err error) string {
	// nil 错误没有可格式化内容。
	if err == nil {
		return ""
	}
	// TencentCloudSDKError 中包含 code/message/request id，优先使用这些结构化字段。
	if sdkErr, ok := errors.AsType[*sdkerrors.TencentCloudSDKError](err); ok {
		return fmt.Sprintf("sdk code=%s message=%s request_id=%s", sdkErr.GetCode(), sdkErr.GetMessage(), sdkErr.GetRequestId())
	}
	// 非 SDKError 退回 err.Error。
	return err.Error()
}

// sdkErrorCode 从云厂商 SDK error 中提取错误码。
// 参数说明：err 是需要转换或包装的错误。
func sdkErrorCode(err error) string {
	if sdkErr, ok := errors.AsType[*sdkerrors.TencentCloudSDKError](err); !ok {
		return ""
	} else {
		return sdkErr.GetCode()
	}
}

// isTencentNodeNotFound 判断腾讯云返回是否表示实例不存在。
// 参数说明：err 是需要转换或包装的错误。
func isTencentNodeNotFound(err error) bool {
	// 同时检查 SDK code 和 message，兼容不同 API 返回的 not found 表达。
	code := strings.ToLower(strings.TrimSpace(sdkErrorCode(err)))
	message := strings.ToLower(err.Error())
	return strings.Contains(code, "invalidinstanceid.notfound") ||
		strings.Contains(code, "resource.notfound") ||
		strings.Contains(message, "not found")
}

// valueString 安全读取 SDK 字符串指针。
// 参数说明：value 是 SDK 返回的字符串指针。
func valueString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// valueInt64 安全读取 SDK int64 指针。
// 参数说明：value 是 SDK 返回的 int64 指针。
func valueInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

// stringPtrs 将字符串切片转换为 SDK 字符串指针切片。
// 参数说明：values 表示值集合。
func stringPtrs(values []string) []*string {
	result := make([]*string, 0, len(values))
	for _, value := range values {
		// 空字符串不写入 SDK 请求，避免提交无效 ID。
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		// 每个有效字符串都转换为独立指针。
		result = append(result, new(value))
	}
	return result
}
