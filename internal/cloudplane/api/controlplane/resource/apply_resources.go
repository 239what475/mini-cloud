package resource

import (
	"context"
	"strings"

	controlplane "mini-cloud/internal/cloudplane/api/controlplane"
	"mini-cloud/internal/contract/cloudplaneapi"
	cloudplanev1 "mini-cloud/internal/gen/proto/minicloud/cloudplane/v1"
)

func (s *Server) ApplyResources(ctx context.Context, req *cloudplanev1.ApplyResourcesRequest) (*cloudplanev1.ApplyResourcesResponse, error) {
	if err := s.auth.Authorize(ctx); err != nil {
		return nil, err
	}

	resources := cloudplaneapi.ResourceBundle{
		ConfigSets:          configSetsFromProto(req.GetConfigSets()),
		SecretSets:          secretSetsFromProto(req.GetSecretSets()),
		RegistryCredentials: registryCredentialsFromProto(req.GetRegistryCredentials()),
	}
	changed, err := s.store.ApplyResources(ctx, resources)
	if err != nil {
		return nil, s.applyResourcesStatusError("apply resources", err)
	}
	action := cloudplaneapi.ApplyActionNoop
	if changed {
		action = cloudplaneapi.ApplyActionUpdated
	}
	return &cloudplanev1.ApplyResourcesResponse{Action: action}, nil
}

func configSetsFromProto(items []*cloudplanev1.ResourceConfigSet) []cloudplaneapi.ConfigSet {
	if len(items) == 0 {
		return nil
	}
	out := make([]cloudplaneapi.ConfigSet, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, cloudplaneapi.ConfigSet{
			ID:     strings.TrimSpace(item.GetId()),
			Name:   strings.TrimSpace(item.GetName()),
			Values: controlplane.CopyStringMap(item.GetValues()),
		})
	}
	return out
}

func secretSetsFromProto(items []*cloudplanev1.ResourceSecretSet) []cloudplaneapi.SecretSet {
	if len(items) == 0 {
		return nil
	}
	out := make([]cloudplaneapi.SecretSet, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, cloudplaneapi.SecretSet{
			ID:     strings.TrimSpace(item.GetId()),
			Name:   strings.TrimSpace(item.GetName()),
			Values: controlplane.CopyStringMap(item.GetValues()),
		})
	}
	return out
}

func registryCredentialsFromProto(items []*cloudplanev1.ResourceRegistryCredential) []cloudplaneapi.RegistryCredential {
	if len(items) == 0 {
		return nil
	}
	out := make([]cloudplaneapi.RegistryCredential, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, cloudplaneapi.RegistryCredential{
			ID:       strings.TrimSpace(item.GetId()),
			Name:     strings.TrimSpace(item.GetName()),
			Server:   strings.TrimSpace(item.GetServer()),
			Username: strings.TrimSpace(item.GetUsername()),
			Password: item.GetPassword(),
		})
	}
	return out
}
