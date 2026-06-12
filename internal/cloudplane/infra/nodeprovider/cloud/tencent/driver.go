package tencent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	tcprofile "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	cvm "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm/v20170312"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/nodeprovider"
	"mini-cloud/internal/cloudplane/infra/nodeprovider/cloud/bootstrap"
)

const Name = "tencent"

const tencentCVMRequestTimeout = 15 * time.Second

var nodeNoProxy = []string{
	"127.0.0.1",
	"localhost",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.169.254",
	"metadata.tencentyun.com",
}

type driverConfig struct {
	CloudPlane cloudplaneconfig.Config
	Provider   cloudplaneconfig.TencentNodeConfig
}

type providerDriver struct {
	client *cvm.Client
	config driverConfig
}

type instanceTypeCapacity struct {
	instanceType string
	cpuMilli     int
	memoryMi     int
}

func NewDriver(cfg cloudplaneconfig.Config) (nodeprovider.Driver, error) {
	typedConfig, err := newDriverConfig(cfg)
	if err != nil {
		return nil, err
	}
	client, err := newCVMClient(
		typedConfig.CloudPlane.Infrastructure.RegionID,
		typedConfig.CloudPlane.Infrastructure.TencentCredential,
	)
	if err != nil {
		return nil, err
	}
	return &providerDriver{
		client: client,
		config: typedConfig,
	}, nil
}

func newDriverConfig(cfg cloudplaneconfig.Config) (driverConfig, error) {
	if !strings.EqualFold(strings.TrimSpace(cfg.Infrastructure.Provider), Name) {
		return driverConfig{}, fmt.Errorf("cloud provider %q does not match %q", cfg.Infrastructure.Provider, Name)
	}
	if strings.TrimSpace(cfg.NodeAgent.Token) == "" {
		return driverConfig{}, fmt.Errorf("nodeAgent.token is required for tencent node provider driver")
	}
	if strings.TrimSpace(cfg.NodeProvisioning.InstanceType) == "" {
		return driverConfig{}, fmt.Errorf("nodeProvisioning.instanceType is required for tencent node provider driver")
	}

	spec := cfg.NodeProvisioning.Tencent
	if spec.SystemDiskType == "" {
		spec.SystemDiskType = "CLOUD_PREMIUM"
	}
	if spec.SystemDiskSizeGiB == 0 {
		spec.SystemDiskSizeGiB = 50
	}
	if strings.TrimSpace(spec.ImageID) == "" {
		return driverConfig{}, fmt.Errorf("nodeProvisioning.tencent.imageId is required")
	}
	if strings.TrimSpace(spec.VPCID) == "" {
		return driverConfig{}, fmt.Errorf("nodeProvisioning.tencent.vpcId is required")
	}
	if strings.TrimSpace(spec.SubnetID) == "" {
		return driverConfig{}, fmt.Errorf("nodeProvisioning.tencent.subnetId is required")
	}
	if len(spec.SecurityGroupIDs) == 0 {
		return driverConfig{}, fmt.Errorf("nodeProvisioning.tencent.securityGroupIds is required")
	}
	if spec.SystemDiskSizeGiB <= 0 {
		return driverConfig{}, fmt.Errorf("nodeProvisioning.tencent.systemDiskSizeGiB must be greater than 0")
	}
	return driverConfig{
		CloudPlane: cfg,
		Provider:   spec,
	}, nil
}

func (p *providerDriver) Create(ctx context.Context, request nodeprovider.CreateRequest) (nodeprovider.CreateResult, error) {
	if p == nil || p.client == nil {
		return nodeprovider.CreateResult{}, fmt.Errorf("tencent node provider driver is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return nodeprovider.CreateResult{}, err
	}
	instanceName := strings.TrimSpace(request.Name)
	if instanceName == "" {
		return nodeprovider.CreateResult{}, fmt.Errorf("node name is required")
	}
	clientToken := strings.TrimSpace(request.ClientToken)
	if clientToken == "" {
		return nodeprovider.CreateResult{}, fmt.Errorf("node clientToken is required")
	}
	if request.CPUMilli <= 0 {
		return nodeprovider.CreateResult{}, fmt.Errorf("node cpuMilli must be greater than 0")
	}
	if request.MemoryMi <= 0 {
		return nodeprovider.CreateResult{}, fmt.Errorf("node memoryMi must be greater than 0")
	}
	capacity, err := p.lookupInstanceTypeCapacity(ctx)
	if err != nil {
		return nodeprovider.CreateResult{}, err
	}
	if request.CPUMilli > capacity.cpuMilli || request.MemoryMi > capacity.memoryMi {
		return nodeprovider.CreateResult{}, fmt.Errorf(
			"configured instance type %s only has %dm cpu / %dMi memory, which cannot satisfy request %dm / %dMi",
			capacity.instanceType,
			capacity.cpuMilli,
			capacity.memoryMi,
			request.CPUMilli,
			request.MemoryMi,
		)
	}
	userData, err := p.buildNodeUserData(instanceName, capacity)
	if err != nil {
		return nodeprovider.CreateResult{}, err
	}
	runRequest := cvm.NewRunInstancesRequest()
	runRequest.InstanceChargeType = new("POSTPAID_BY_HOUR")
	runRequest.ImageId = new(p.config.Provider.ImageID)
	runRequest.InstanceType = new(p.config.CloudPlane.NodeProvisioning.InstanceType)
	runRequest.InstanceCount = new(int64(1))
	runRequest.InstanceName = new(instanceName)
	runRequest.HostName = new(buildNodeHostName(instanceName))
	runRequest.ClientToken = new(clientToken)
	runRequest.SecurityGroupIds = tencentStringPointers(p.config.Provider.SecurityGroupIDs)
	runRequest.UserData = new(userData)
	runRequest.SystemDisk = &cvm.SystemDisk{
		DiskType: new(p.config.Provider.SystemDiskType),
		DiskSize: new(p.config.Provider.SystemDiskSizeGiB),
	}
	runRequest.VirtualPrivateCloud = &cvm.VirtualPrivateCloud{
		VpcId:    new(p.config.Provider.VPCID),
		SubnetId: new(p.config.Provider.SubnetID),
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
			Tags:         buildTencentRunInstanceTags(p.config.CloudPlane.Plane.Name),
		},
	}
	if zoneID := strings.TrimSpace(p.config.CloudPlane.Infrastructure.ZoneID); zoneID != "" {
		runRequest.Placement = &cvm.Placement{
			Zone: new(zoneID),
		}
	}
	if len(p.config.Provider.KeyIDs) > 0 {
		runRequest.LoginSettings = &cvm.LoginSettings{
			KeyIds: tencentStringPointers(p.config.Provider.KeyIDs),
		}
	}
	response, err := p.client.RunInstancesWithContext(ctx, runRequest)
	if err != nil {
		return nodeprovider.CreateResult{}, fmt.Errorf("RunInstances failed: %s", formatTencentSDKError(err))
	}
	if response == nil || response.Response == nil || len(response.Response.InstanceIdSet) == 0 || response.Response.InstanceIdSet[0] == nil {
		return nodeprovider.CreateResult{}, fmt.Errorf("RunInstances returned no instance id")
	}
	instanceID := *response.Response.InstanceIdSet[0]
	return nodeprovider.CreateResult{
		InstanceID:   instanceID,
		InstanceName: instanceName,
		InstanceType: p.config.CloudPlane.NodeProvisioning.InstanceType,
	}, nil
}

func (p *providerDriver) Delete(ctx context.Context, request nodeprovider.DeleteRequest) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("tencent node provider driver is not initialized")
	}
	instanceID := strings.TrimSpace(request.InstanceID)
	if instanceID == "" {
		return fmt.Errorf("node instanceID is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	req := cvm.NewTerminateInstancesRequest()
	req.InstanceIds = []*string{new(instanceID)}
	response, err := p.client.TerminateInstancesWithContext(ctx, req)
	if err != nil {
		if isTencentNodeNotFound(err) {
			return nil
		}
		return fmt.Errorf("TerminateInstances failed: %s", formatTencentSDKError(err))
	}
	if response == nil || response.Response == nil {
		return fmt.Errorf("TerminateInstances returned empty response")
	}
	return nil
}

func (p *providerDriver) lookupInstanceTypeCapacity(ctx context.Context) (instanceTypeCapacity, error) {
	if err := ctx.Err(); err != nil {
		return instanceTypeCapacity{}, err
	}
	request := cvm.NewDescribeInstanceTypeConfigsRequest()
	request.Filters = []*cvm.Filter{
		{
			Name:   new("instance-type"),
			Values: []*string{new(p.config.CloudPlane.NodeProvisioning.InstanceType)},
		},
	}
	if zoneID := strings.TrimSpace(p.config.CloudPlane.Infrastructure.ZoneID); zoneID != "" {
		request.Filters = append(request.Filters, &cvm.Filter{
			Name:   new("zone"),
			Values: []*string{new(zoneID)},
		})
	}
	response, err := p.client.DescribeInstanceTypeConfigsWithContext(ctx, request)
	if err != nil {
		return instanceTypeCapacity{}, fmt.Errorf("DescribeInstanceTypeConfigs failed: %s", formatTencentSDKError(err))
	}
	if response == nil || response.Response == nil || len(response.Response.InstanceTypeConfigSet) == 0 || response.Response.InstanceTypeConfigSet[0] == nil || response.Response.InstanceTypeConfigSet[0].InstanceType == nil {
		return instanceTypeCapacity{}, fmt.Errorf("DescribeInstanceTypeConfigs did not return configured instance type %q", p.config.CloudPlane.NodeProvisioning.InstanceType)
	}
	item := response.Response.InstanceTypeConfigSet[0]
	var cpu int64
	if item.CPU != nil {
		cpu = *item.CPU
	}
	var memoryGiB int64
	if item.Memory != nil {
		memoryGiB = *item.Memory
	}
	cpuMilli := int(cpu) * 1000
	memoryMi := int(math.Round(float64(memoryGiB) * 1024))
	if cpuMilli <= 0 || memoryMi <= 0 {
		return instanceTypeCapacity{}, fmt.Errorf("DescribeInstanceTypeConfigs returned invalid capacity for %q", p.config.CloudPlane.NodeProvisioning.InstanceType)
	}
	return instanceTypeCapacity{
		instanceType: strings.TrimSpace(*item.InstanceType),
		cpuMilli:     cpuMilli,
		memoryMi:     memoryMi,
	}, nil
}

func (p *providerDriver) buildNodeUserData(instanceName string, capacity instanceTypeCapacity) (string, error) {
	return bootstrap.RenderBase64(bootstrap.Config{
		AgentBinaryURL:              p.config.CloudPlane.NodeAgent.BinaryURL,
		Token:                       p.config.CloudPlane.NodeAgent.Token,
		ConnectEndpoint:             p.config.CloudPlane.NodeAgent.ConnectEndpoint,
		DockerRegistryMirrors:       p.config.CloudPlane.NodeProvisioning.RegistryMirrors,
		InstanceName:                instanceName,
		InstanceType:                capacity.instanceType,
		MetadataBase:                "http://metadata.tencentyun.com/latest/meta-data",
		MetadataGetFunction:         tencentMetadataGetFunction,
		MetadataInstanceIDPath:      "instance-id",
		MetadataPrivateIPPath:       "local-ipv4",
		NoProxyItems:                nodeNoProxy,
		PlatformName:                p.config.CloudPlane.Plane.Name,
		Provider:                    p.config.CloudPlane.Infrastructure.Provider,
		Region:                      p.config.CloudPlane.Infrastructure.RegionID,
		WorkloadEgressProxyEndpoint: p.config.CloudPlane.NodeProvisioning.WorkloadEgressProxyEndpoint,
		WorkloadLogLokiURL:          p.config.CloudPlane.Observability.LokiURL,
		WorkloadLogLokiTenantID:     p.config.CloudPlane.Observability.LokiTenantID,
		WorkloadOTLPEndpoint:        p.config.CloudPlane.Observability.OTLPEndpoint,
		CPUMilli:                    capacity.cpuMilli,
		MemoryMi:                    capacity.memoryMi,
	})
}

const tencentMetadataGetFunction = `metadata_get() {
  local path="$1"
  curl -fsS "$METADATA_BASE/$path"
}`

func newCVMClient(regionID string, credentialConfig cloudplaneconfig.TencentCredentialConfig) (*cvm.Client, error) {
	secretID := strings.TrimSpace(credentialConfig.SecretID)
	secretKey := strings.TrimSpace(credentialConfig.SecretKey)
	if secretID == "" || secretKey == "" {
		return nil, fmt.Errorf("infrastructure.tencentCredential.secretId and secretKey are required")
	}
	credential := tccommon.NewTokenCredential(secretID, secretKey, strings.TrimSpace(credentialConfig.Token))
	clientProfile := tcprofile.NewClientProfile()
	clientProfile.HttpProfile.Endpoint = "cvm.tencentcloudapi.com"
	clientProfile.HttpProfile.ReqTimeout = int(tencentCVMRequestTimeout / time.Second)
	client, err := cvm.NewClient(credential, regionID, clientProfile)
	if err != nil {
		return nil, fmt.Errorf("create tencent cvm client: %w", err)
	}
	return client, nil
}

func buildNodeHostName(instanceName string) string {
	hostName := strings.Trim(strings.TrimSpace(instanceName), ".-")
	if len(hostName) > 60 {
		hostName = strings.Trim(hostName[:60], ".-")
	}
	if len(hostName) < 2 {
		return "mini-cloud-node"
	}
	return hostName
}

func buildTencentRunInstanceTags(platformName string) []*cvm.Tag {
	result := []*cvm.Tag{
		{
			Key:   new("managed-by"),
			Value: new("mini-cloud"),
		},
	}
	if platformName = strings.TrimSpace(platformName); platformName != "" {
		result = append(result, &cvm.Tag{
			Key:   new("mini-cloud/platform"),
			Value: new(platformName),
		})
	}
	return result
}

func formatTencentSDKError(err error) string {
	if err == nil {
		return ""
	}
	if sdkErr, ok := errors.AsType[*sdkerrors.TencentCloudSDKError](err); ok {
		return fmt.Sprintf("sdk code=%s message=%s request_id=%s", sdkErr.GetCode(), sdkErr.GetMessage(), sdkErr.GetRequestId())
	}
	return err.Error()
}

func sdkErrorCode(err error) string {
	if sdkErr, ok := errors.AsType[*sdkerrors.TencentCloudSDKError](err); !ok {
		return ""
	} else {
		return sdkErr.GetCode()
	}
}

func isTencentNodeNotFound(err error) bool {
	code := strings.ToLower(strings.TrimSpace(sdkErrorCode(err)))
	message := strings.ToLower(err.Error())
	return strings.Contains(code, "invalidinstanceid.notfound") ||
		strings.Contains(code, "resource.notfound") ||
		strings.Contains(message, "not found")
}

func tencentStringPointers(values []string) []*string {
	result := make([]*string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)

		if value == "" {
			continue
		}

		result = append(result, new(value))
	}
	return result
}
