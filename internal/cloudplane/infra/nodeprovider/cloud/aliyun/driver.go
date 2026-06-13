package aliyun

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ecs20140526 "github.com/alibabacloud-go/ecs-20140526/v7/client"
	dara "github.com/alibabacloud-go/tea-utils/v2/service"
	tea "github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/credentials-go/credentials"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/nodeprovider"
	"mini-cloud/internal/cloudplane/infra/nodeprovider/cloud/bootstrap"
)

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

type driverConfig struct {
	CloudPlane cloudplaneconfig.Config
	Provider   cloudplaneconfig.AliyunNodeConfig
}

type providerDriver struct {
	client *ecs20140526.Client
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
	client, err := newECSClient(typedConfig.CloudPlane.Infrastructure.RegionID)
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
		return driverConfig{}, fmt.Errorf("nodeAgent.token is required for aliyun node provider driver")
	}
	if strings.TrimSpace(cfg.NodeProvisioning.InstanceType) == "" {
		return driverConfig{}, fmt.Errorf("nodeProvisioning.instanceType is required for aliyun node provider driver")
	}

	spec := cfg.NodeProvisioning.Aliyun
	if spec.SystemDiskCategory == "" {
		spec.SystemDiskCategory = "cloud_essd"
	}
	if spec.SystemDiskSizeGiB == 0 {
		spec.SystemDiskSizeGiB = 40
	}
	if strings.TrimSpace(spec.ImageID) == "" {
		return driverConfig{}, fmt.Errorf("nodeProvisioning.aliyun.imageId is required")
	}
	if strings.TrimSpace(spec.VSwitchID) == "" {
		return driverConfig{}, fmt.Errorf("nodeProvisioning.aliyun.vSwitchId is required")
	}
	if strings.TrimSpace(spec.SecurityGroupID) == "" {
		return driverConfig{}, fmt.Errorf("nodeProvisioning.aliyun.securityGroupId is required")
	}
	if spec.SystemDiskSizeGiB <= 0 {
		return driverConfig{}, fmt.Errorf("nodeProvisioning.aliyun.systemDiskSizeGiB must be greater than 0")
	}
	return driverConfig{
		CloudPlane: cfg,
		Provider:   spec,
	}, nil
}

func (p *providerDriver) Create(ctx context.Context, request nodeprovider.CreateRequest) (nodeprovider.CreateResult, error) {
	if p == nil || p.client == nil {
		return nodeprovider.CreateResult{}, fmt.Errorf("aliyun node provider driver is not initialized")
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
	runRequest := &ecs20140526.RunInstancesRequest{
		RegionId:                new(p.config.CloudPlane.Infrastructure.RegionID),
		InstanceChargeType:      new("PostPaid"),
		AutoPay:                 new(true),
		ImageId:                 new(p.config.Provider.ImageID),
		InstanceType:            new(p.config.CloudPlane.NodeProvisioning.InstanceType),
		Amount:                  new(int32(1)),
		ClientToken:             new(clientToken),
		InstanceName:            new(instanceName),
		Description:             new("mini-cloud node"),
		HostName:                new(instanceName),
		SecurityGroupId:         new(p.config.Provider.SecurityGroupID),
		VSwitchId:               new(p.config.Provider.VSwitchID),
		UserData:                new(userData),
		InternetMaxBandwidthOut: new(int32(0)),
		SystemDisk: &ecs20140526.RunInstancesRequestSystemDisk{
			Category: new(p.config.Provider.SystemDiskCategory),
			Size:     new(strconv.Itoa(p.config.Provider.SystemDiskSizeGiB)),
		},
		Tag: buildAliyunRunInstanceTags(p.config.CloudPlane.Plane.Name),
	}
	if zoneID := strings.TrimSpace(p.config.CloudPlane.Infrastructure.ZoneID); zoneID != "" {
		runRequest.ZoneId = new(zoneID)
	}
	if keyPairName := strings.TrimSpace(p.config.Provider.KeyPairName); keyPairName != "" {
		runRequest.KeyPairName = new(keyPairName)
	}
	response, err := p.client.RunInstancesWithContext(ctx, runRequest, aliyunRuntimeOptions())
	if err != nil {
		return nodeprovider.CreateResult{}, fmt.Errorf("RunInstances failed: %s", formatAliyunSDKError(err))
	}
	if response.Body == nil || response.Body.InstanceIdSets == nil || len(response.Body.InstanceIdSets.InstanceIdSet) == 0 || response.Body.InstanceIdSets.InstanceIdSet[0] == nil {
		return nodeprovider.CreateResult{}, fmt.Errorf("RunInstances returned no instance id")
	}
	instanceID := tea.StringValue(response.Body.InstanceIdSets.InstanceIdSet[0])
	return nodeprovider.CreateResult{
		InstanceID:   instanceID,
		InstanceName: instanceName,
		InstanceType: p.config.CloudPlane.NodeProvisioning.InstanceType,
	}, nil
}

func (p *providerDriver) Delete(ctx context.Context, request nodeprovider.DeleteRequest) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("aliyun node provider driver is not initialized")
	}
	instanceID := strings.TrimSpace(request.InstanceID)
	if instanceID == "" {
		return fmt.Errorf("node instanceID is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	req := &ecs20140526.DeleteInstancesRequest{
		RegionId:   new(p.config.CloudPlane.Infrastructure.RegionID),
		InstanceId: []*string{new(instanceID)},
		Force:      new(true),
		ForceStop:  new(false),
	}
	if _, err := p.client.DeleteInstancesWithContext(ctx, req, aliyunRuntimeOptions()); err != nil {
		if isAliyunNodeNotFound(err) {
			return nil
		}
		return fmt.Errorf("DeleteInstances failed: %s", formatAliyunSDKError(err))
	}
	return nil
}

func (p *providerDriver) lookupInstanceTypeCapacity(ctx context.Context) (instanceTypeCapacity, error) {
	if err := ctx.Err(); err != nil {
		return instanceTypeCapacity{}, err
	}
	response, err := p.client.DescribeInstanceTypesWithContext(ctx, &ecs20140526.DescribeInstanceTypesRequest{
		InstanceTypes: []*string{new(p.config.CloudPlane.NodeProvisioning.InstanceType)},
		MaxResults:    new(int64(1)),
	}, aliyunRuntimeOptions())
	if err != nil {
		return instanceTypeCapacity{}, fmt.Errorf("DescribeInstanceTypes failed: %s", formatAliyunSDKError(err))
	}
	if response.Body == nil || response.Body.InstanceTypes == nil || len(response.Body.InstanceTypes.InstanceType) == 0 || response.Body.InstanceTypes.InstanceType[0] == nil {
		return instanceTypeCapacity{}, fmt.Errorf("DescribeInstanceTypes did not return configured instance type %q", p.config.CloudPlane.NodeProvisioning.InstanceType)
	}
	item := response.Body.InstanceTypes.InstanceType[0]
	cpuMilli := int(tea.Int32Value(item.CpuCoreCount)) * 1000
	memoryMi := int(math.Round(float64(tea.Float32Value(item.MemorySize)) * 1024))
	if cpuMilli <= 0 || memoryMi <= 0 {
		return instanceTypeCapacity{}, fmt.Errorf("DescribeInstanceTypes returned invalid capacity for %q", p.config.CloudPlane.NodeProvisioning.InstanceType)
	}
	return instanceTypeCapacity{
		instanceType: tea.StringValue(item.InstanceTypeId),
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
		MetadataBase:                "http://100.100.100.200/latest",
		MetadataGetFunction:         aliyunMetadataGetFunction,
		MetadataInit:                `METADATA_TOKEN="$(metadata_token || true)"`,
		MetadataInstanceIDPath:      "instance-id",
		MetadataPrivateIPPath:       "private-ipv4",
		NoProxyItems:                nodeNoProxy,
		PlatformName:                p.config.CloudPlane.Plane.Name,
		Provider:                    p.config.CloudPlane.Infrastructure.Provider,
		Region:                      p.config.CloudPlane.Infrastructure.RegionID,
		WorkloadEgressProxyEndpoint: p.config.CloudPlane.NodeProvisioning.WorkloadEgressProxyEndpoint,
		WorkloadOTLPEndpoint:        p.config.CloudPlane.Observability.OTLPEndpoint,
		CPUMilli:                    capacity.cpuMilli,
		MemoryMi:                    capacity.memoryMi,
	})
}

const aliyunMetadataGetFunction = `metadata_token() {
  curl -fsS -X PUT "$METADATA_BASE/api/token" -H 'X-aliyun-ecs-metadata-token-ttl-seconds: 21600'
}

metadata_get() {
  local path="$1"
  if [[ -n "${METADATA_TOKEN:-}" ]]; then
    curl -fsS -H "X-aliyun-ecs-metadata-token: $METADATA_TOKEN" "$METADATA_BASE/meta-data/$path"
    return
  fi
  curl -fsS "$METADATA_BASE/meta-data/$path"
}`

func newECSClient(regionID string) (*ecs20140526.Client, error) {
	credential, err := credentials.NewCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("create aliyun credential: %w", err)
	}
	client, err := ecs20140526.NewClient(&openapi.Config{
		Endpoint:   new(resolveECSEndpoint(regionID)),
		Credential: credential,
	})
	if err != nil {
		return nil, fmt.Errorf("create ecs client: %w", err)
	}
	return client, nil
}

func aliyunRuntimeOptions() *dara.RuntimeOptions {
	return &dara.RuntimeOptions{}
}

func resolveECSEndpoint(regionID string) string {
	return fmt.Sprintf("ecs.%s.aliyuncs.com", regionID)
}

func buildAliyunRunInstanceTags(platformName string) []*ecs20140526.RunInstancesRequestTag {
	result := []*ecs20140526.RunInstancesRequestTag{
		{
			Key:   new("managed-by"),
			Value: new("mini-cloud"),
		},
	}
	if platformName = strings.TrimSpace(platformName); platformName != "" {
		result = append(result, &ecs20140526.RunInstancesRequestTag{
			Key:   new("mini-cloud/platform"),
			Value: new(platformName),
		})
	}
	return result
}

func formatAliyunSDKError(err error) string {
	if err == nil {
		return ""
	}
	if sdkErr, ok := errors.AsType[*tea.SDKError](err); ok {
		return fmt.Sprintf("sdk code=%s message=%s data=%v", tea.StringValue(sdkErr.Code), tea.StringValue(sdkErr.Message), sdkErr.Data)
	}
	return err.Error()
}

func sdkErrorCode(err error) string {
	if sdkErr, ok := errors.AsType[*tea.SDKError](err); !ok {
		return ""
	} else {
		return tea.StringValue(sdkErr.Code)
	}
}

func isAliyunNodeNotFound(err error) bool {
	code := strings.ToLower(sdkErrorCode(err))
	message := strings.ToLower(err.Error())
	return strings.Contains(code, "notfound") ||
		strings.Contains(code, "invalidinstanceid.notfound") ||
		strings.Contains(message, "not found after write")
}
