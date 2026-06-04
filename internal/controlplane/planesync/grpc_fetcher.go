package planesync

import (
	"context"
	"errors"
	"fmt"
	"strings"

	plane "mini-cloud/internal/controlplane/plane"
	planeclient "mini-cloud/internal/controlplane/planeclient"
)

type grpcPlaneSnapshotFetcher struct{}

func newGRPCPlaneSnapshotFetcher() planeSnapshotFetcher {
	return &grpcPlaneSnapshotFetcher{}
}

func (f *grpcPlaneSnapshotFetcher) Fetch(ctx context.Context, grpcEndpoint string, southboundToken string) (snapshot planeSnapshot, err error) {
	grpcEndpoint = strings.TrimRight(strings.TrimSpace(grpcEndpoint), "/")

	client, err := planeclient.New(grpcEndpoint, southboundToken)
	if err != nil {
		return planeSnapshot{}, &syncError{
			status:  plane.StatusOffline,
			message: fmt.Sprintf("initialize plane southbound client failed: %v", err),
		}
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			closeSyncErr := &syncError{
				status:  plane.StatusOffline,
				message: fmt.Sprintf("close plane southbound client failed: %v", closeErr),
			}
			if err != nil {
				err = errors.Join(err, closeSyncErr)
				return
			}
			err = closeSyncErr
		}
	}()

	snapshotResp, err := client.Snapshot(ctx)
	if err != nil {
		return planeSnapshot{}, &syncError{
			status:  plane.StatusOffline,
			message: fmt.Sprintf("load plane snapshot failed: %v", err),
		}
	}

	return planeSnapshot{
		Plane:         snapshotResp.Plane,
		Health:        snapshotResp.Health,
		Overview:      snapshotResp.Overview,
		Capacity:      snapshotResp.Capacity,
		Reliability:   snapshotResp.Reliability,
		Runtime:       snapshotResp.Runtime,
		RuntimeConfig: snapshotResp.RuntimeConfig,
		Executions:    snapshotResp.Executions,
	}, nil
}
