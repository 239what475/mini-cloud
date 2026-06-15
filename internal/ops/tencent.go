package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type lighthouseAttachmentResponse struct {
	CcnAttachedInstanceSet []struct {
		CcnID string `json:"CcnId"`
		State string `json:"State"`
	} `json:"CcnAttachedInstanceSet"`
}

type ccnAttachmentResponse struct {
	InstanceSet []struct {
		CcnID        string `json:"CcnId"`
		InstanceType string `json:"InstanceType"`
		InstanceID   string `json:"InstanceId"`
		State        string `json:"State"`
		Description  string `json:"Description"`
	} `json:"InstanceSet"`
}

type lighthouseFirewallResponse struct {
	FirewallRuleSet []firewallRule `json:"FirewallRuleSet"`
}

type firewallRule struct {
	Protocol                string `json:"Protocol"`
	Port                    string `json:"Port"`
	CidrBlock               string `json:"CidrBlock"`
	Action                  string `json:"Action"`
	FirewallRuleDescription string `json:"FirewallRuleDescription,omitempty"`
}

type tencentInstancesResponse struct {
	InstanceSet []struct {
		InstanceID string `json:"InstanceId"`
	} `json:"InstanceSet"`
}

func (r *Runner) attachLighthouseCCN(ctx context.Context, out TerraformOutput) error {
	regionID := out.RegionID()
	ccnID := ""
	if out.CCN.Value != nil {
		ccnID = strings.TrimSpace(out.CCN.Value.ID)
	}
	if ccnID == "" {
		return fmt.Errorf("ccn.id output is required for existing_lighthouse mode")
	}

	current, err := r.lighthouseAttachedCCN(ctx, regionID)
	if err != nil {
		return err
	}
	if current != "" && current != ccnID {
		return fmt.Errorf("lighthouse is already attached to %s; detach it before attaching %s", current, ccnID)
	}
	if current != ccnID {
		if _, err := runOutput(ctx, "tccli", "lighthouse", "AttachCcn", "--region", regionID, "--CcnId", ccnID); err != nil {
			return err
		}
	}

	var state string
	for i := 0; i < 12; i++ {
		state, err = r.lighthouseCCNState(ctx, regionID, ccnID)
		if err != nil {
			return err
		}
		if state == "ACTIVE" {
			return nil
		}
		if state == "PENDING" {
			pending, err := r.pendingLighthouseVPC(ctx, regionID, ccnID)
			if err != nil {
				return err
			}
			if pending != "" {
				_, err = runOutput(ctx, "tccli", "vpc", "AcceptAttachCcnInstances",
					"--region", regionID,
					"--cli-unfold-argument",
					"--CcnId", ccnID,
					"--Instances.0.InstanceType", "VPC",
					"--Instances.0.InstanceId", pending,
					"--Instances.0.InstanceRegion", regionID,
				)
				if err != nil {
					return err
				}
			}
		}
		time.Sleep(5 * time.Second)
	}
	if state == "PENDING" {
		return fmt.Errorf("lighthouse CCN attachment is still PENDING after automatic accept attempt")
	}
	return fmt.Errorf("lighthouse CCN attachment did not become ACTIVE; current state: %s", defaultString(state, "unknown"))
}

func (r *Runner) lighthouseAttachedCCN(ctx context.Context, regionID string) (string, error) {
	var response lighthouseAttachmentResponse
	if err := runJSON(ctx, &response, "tccli", "lighthouse", "DescribeCcnAttachedInstances", "--region", regionID); err != nil {
		return "", err
	}
	for _, item := range response.CcnAttachedInstanceSet {
		if item.State != "DELETED" {
			return item.CcnID, nil
		}
	}
	return "", nil
}

func (r *Runner) lighthouseCCNState(ctx context.Context, regionID string, ccnID string) (string, error) {
	var response lighthouseAttachmentResponse
	if err := runJSON(ctx, &response, "tccli", "lighthouse", "DescribeCcnAttachedInstances", "--region", regionID); err != nil {
		return "", err
	}
	for _, item := range response.CcnAttachedInstanceSet {
		if item.CcnID == ccnID {
			return item.State, nil
		}
	}
	return "", nil
}

func (r *Runner) pendingLighthouseVPC(ctx context.Context, regionID string, ccnID string) (string, error) {
	var response ccnAttachmentResponse
	if err := runJSON(ctx, &response, "tccli", "vpc", "DescribeCcnAttachedInstances", "--region", regionID, "--CcnId", ccnID); err != nil {
		return "", err
	}
	for _, item := range response.InstanceSet {
		if item.CcnID == ccnID && item.InstanceType == "VPC" && item.State == "PENDING" && item.Description == "Lighthouse VPC" {
			return item.InstanceID, nil
		}
	}
	return "", nil
}

func (r *Runner) ensureLighthouseFirewallRules(ctx context.Context, out TerraformOutput) error {
	regionID := out.RegionID()
	instanceID := strings.TrimSpace(out.Platform.Value.InstanceID)
	subnetCIDR := strings.TrimSpace(out.Network.Value.SubnetCIDRBlock)
	if regionID == "" || instanceID == "" || subnetCIDR == "" {
		return fmt.Errorf("terraform outputs are incomplete for Lighthouse firewall rules")
	}
	ports := []struct {
		port        int
		cidr        string
		description string
	}{
		{out.Network.Value.CloudPlaneGRPCPort, "0.0.0.0/0", "mini-cloud control-plane to cloud-plane"},
		{out.Network.Value.WorkloadProxyPort, subnetCIDR, "mini-cloud node to workload proxy"},
		{out.Network.Value.ArtifactHTTPPort, subnetCIDR, "mini-cloud node to node-agent artifact server"},
	}
	for _, wanted := range ports {
		if wanted.port == 0 {
			return fmt.Errorf("terraform network outputs are incomplete for Lighthouse firewall rules")
		}
		if wanted.cidr == "" {
			return fmt.Errorf("terraform network outputs are incomplete for Lighthouse firewall rules")
		}
	}

	var response lighthouseFirewallResponse
	if err := runJSON(ctx, &response, "tccli", "lighthouse", "DescribeFirewallRules", "--region", regionID, "--InstanceId", instanceID); err != nil {
		return err
	}
	exists := map[string]bool{}
	for _, rule := range response.FirewallRuleSet {
		if rule.Action == "ACCEPT" {
			exists[firewallKey(rule.Protocol, rule.Port, rule.CidrBlock)] = true
		}
	}
	for _, wanted := range ports {
		port := strconv.Itoa(wanted.port)
		if exists[firewallKey("TCP", port, wanted.cidr)] {
			continue
		}
		rule := firewallRule{
			Protocol:                "TCP",
			Port:                    port,
			CidrBlock:               wanted.cidr,
			Action:                  "ACCEPT",
			FirewallRuleDescription: wanted.description,
		}
		data, err := json.Marshal([]firewallRule{rule})
		if err != nil {
			return err
		}
		if _, err := runOutput(ctx, "tccli", "lighthouse", "CreateFirewallRules", "--region", regionID, "--InstanceId", instanceID, "--FirewallRules", string(data)); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) deleteLighthouseFirewallRules(ctx context.Context, out TerraformOutput) error {
	regionID := out.RegionID()
	instanceID := strings.TrimSpace(out.Platform.Value.InstanceID)
	subnetCIDR := strings.TrimSpace(out.Network.Value.SubnetCIDRBlock)
	if regionID == "" || instanceID == "" || subnetCIDR == "" {
		return nil
	}
	wanted := map[string]bool{
		firewallKey("TCP", strconv.Itoa(out.Network.Value.CloudPlaneGRPCPort), "0.0.0.0/0"): true,
		firewallKey("TCP", strconv.Itoa(out.Network.Value.WorkloadProxyPort), subnetCIDR):   true,
		firewallKey("TCP", strconv.Itoa(out.Network.Value.ArtifactHTTPPort), subnetCIDR):    true,
		firewallKey("TCP", "18080", "0.0.0.0/0"):                                            true,
	}
	var response lighthouseFirewallResponse
	if err := runJSON(ctx, &response, "tccli", "lighthouse", "DescribeFirewallRules", "--region", regionID, "--InstanceId", instanceID); err != nil {
		return err
	}
	var deleteRules []firewallRule
	for _, rule := range response.FirewallRuleSet {
		if rule.Action == "ACCEPT" && wanted[firewallKey(rule.Protocol, rule.Port, rule.CidrBlock)] {
			deleteRules = append(deleteRules, firewallRule{
				Protocol:                rule.Protocol,
				Port:                    rule.Port,
				CidrBlock:               rule.CidrBlock,
				Action:                  rule.Action,
				FirewallRuleDescription: rule.FirewallRuleDescription,
			})
		}
	}
	if len(deleteRules) == 0 {
		return nil
	}
	data, err := json.Marshal(deleteRules)
	if err != nil {
		return err
	}
	_, err = runOutput(ctx, "tccli", "lighthouse", "DeleteFirewallRules", "--region", regionID, "--InstanceId", instanceID, "--FirewallRules", string(data))
	return err
}

func (r *Runner) detachLighthouseCCN(ctx context.Context, out TerraformOutput) error {
	if out.CCN.Value == nil || strings.TrimSpace(out.CCN.Value.ID) == "" {
		return nil
	}
	regionID := out.RegionID()
	ccnID := strings.TrimSpace(out.CCN.Value.ID)
	state, err := r.lighthouseCCNState(ctx, regionID, ccnID)
	if err != nil {
		return err
	}
	if state != "" && state != "DELETED" {
		if _, err := runOutput(ctx, "tccli", "lighthouse", "DetachCcn", "--region", regionID, "--CcnId", ccnID); err != nil {
			return err
		}
	}
	for i := 0; i < 24; i++ {
		state, err = r.lighthouseCCNState(ctx, regionID, ccnID)
		if err != nil {
			return err
		}
		if state == "" || state == "DELETED" {
			return nil
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("timed out waiting for Lighthouse to detach from CCN %s", ccnID)
}

func (r *Runner) deleteTencentWorkerNodes(ctx context.Context, out TerraformOutput) error {
	ids, err := r.tencentWorkerNodeIDs(ctx, out)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	if _, err := runOutput(ctx, "tccli", "cvm", "TerminateInstances", "--region", out.RegionID(), "--InstanceIds", string(data)); err != nil {
		return err
	}
	for i := 0; i < 60; i++ {
		ids, err = r.tencentWorkerNodeIDs(ctx, out)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("timed out waiting for Tencent nodes to terminate: %v", ids)
}

func (r *Runner) tencentWorkerNodeIDs(ctx context.Context, out TerraformOutput) ([]string, error) {
	filters := fmt.Sprintf(`[{"Name":"tag:managed-by","Values":["mini-cloud"]},{"Name":"tag:mini-cloud/platform","Values":["%s"]}]`, out.Platform.Value.Name)
	var response tencentInstancesResponse
	if err := runJSON(ctx, &response, "tccli", "cvm", "DescribeInstances", "--region", out.RegionID(), "--Filters", filters); err != nil {
		return nil, err
	}
	platformID := strings.TrimSpace(out.Platform.Value.InstanceID)
	var ids []string
	for _, item := range response.InstanceSet {
		if item.InstanceID != "" && item.InstanceID != platformID {
			ids = append(ids, item.InstanceID)
		}
	}
	return ids, nil
}

func firewallKey(protocol string, port string, cidr string) string {
	return protocol + "|" + port + "|" + cidr
}
