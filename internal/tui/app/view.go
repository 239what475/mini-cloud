package app

import (
	"fmt"
	"strings"
	"time"

	"mini-cloud/internal/common/util"
	controlplanev1 "mini-cloud/internal/gen/proto/minicloud/controlplane/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func renderTabs(active Tab) string {
	items := make([]string, 0, len(tabLabels))
	for idx, label := range tabLabels {
		if Tab(idx) == active {
			items = append(items, "["+label+"]")
			continue
		}
		items = append(items, " "+label+" ")
	}
	return "mini-cloud operator    " + strings.Join(items, " ")
}

func renderHelp(active Tab) string {
	base := "keys: q quit | tab switch | r refresh | 1-4 jump"
	switch active {
	case TabPlanes:
		return base + " | j/k move | s sync"
	case TabProjects:
		return base + " | j/k move | enter load services"
	case TabServices:
		return base + " | j/k move | n new-service"
	default:
		return base
	}
}

func renderOverview(item *controlplanev1.Overview) string {
	if item == nil {
		return "no overview data"
	}
	lines := []string{
		"Platform",
		fmt.Sprintf("  planes    total=%d ready=%d registering=%d degraded=%d offline=%d", item.GetPlanesTotal(), item.GetPlanesReady(), item.GetPlanesRegistering(), item.GetPlanesDegraded(), item.GetPlanesOffline()),
		fmt.Sprintf("  projects  total=%d", item.GetProjectsTotal()),
		fmt.Sprintf("  services  total=%d ready=%d progressing=%d pending=%d degraded=%d deleting=%d", item.GetServicesTotal(), item.GetServicesReady(), item.GetServicesProgressing(), item.GetServicesPending(), item.GetServicesDegraded(), item.GetServicesDeleting()),
	}
	return strings.Join(lines, "\n")
}

func renderPlanes(items []*controlplanev1.Plane, selected int) string {
	if len(items) == 0 {
		return "no planes"
	}

	var out strings.Builder
	out.WriteString("Planes\n")
	for idx, item := range items {
		out.WriteString(renderListLine(idx == selected, fmt.Sprintf("%s (%s/%s) status=%s op=%s",
			displayName(item.GetDisplayName(), item.GetName()),
			item.GetProvider(),
			item.GetRegion(),
			item.GetStatus().GetStatus(),
			item.GetOperation().GetState(),
		)))
		out.WriteString("\n")
	}

	selectedItem := items[clampIndex(selected, len(items))]
	out.WriteString("\nSelected\n")
	util.Fprintf(&out, "  id:                %s\n", selectedItem.GetId())
	util.Fprintf(&out, "  displayName:       %s\n", displayName(selectedItem.GetDisplayName(), selectedItem.GetName()))
	util.Fprintf(&out, "  grpcEndpoint:        %s\n", selectedItem.GetGrpcEndpoint())
	util.Fprintf(&out, "  lastHeartbeatAt:   %s\n", formatTimestamp(selectedItem.GetStatus().GetLastHeartbeatAt()))
	util.Fprintf(&out, "  lastSyncAt:        %s\n", formatTimestamp(selectedItem.GetStatus().GetLastSyncAt()))
	util.Fprintf(&out, "  latestInventory:   %d\n", selectedItem.GetStatus().GetLastInventoryVersion())
	if snapshot := selectedItem.GetLatestCapacitySnapshot(); snapshot != nil {
		util.Fprintf(&out, "  capacity:          nodes=%d/%d cpu=%dm/%dm mem=%dMi/%dMi\n",
			snapshot.GetNodesReady(),
			snapshot.GetNodesTotal(),
			snapshot.GetCpuMilliAllocated(),
			snapshot.GetCpuMilliCapacity(),
			snapshot.GetMemoryMiAllocated(),
			snapshot.GetMemoryMiCapacity(),
		)
	}
	if inventory := selectedItem.GetLatestRuntimeInventory(); inventory != nil {
		util.Fprintf(&out, "  runtimeInventory:  sync=%d nodes=%d/%d cpu=%dm/%dm mem=%dMi/%dMi\n",
			inventory.GetSyncVersion(),
			inventory.GetNodesReady(),
			inventory.GetNodesTotal(),
			inventory.GetCpuMilliAllocated(),
			inventory.GetCpuMilliCapacity(),
			inventory.GetMemoryMiAllocated(),
			inventory.GetMemoryMiCapacity(),
		)
	}
	if config := selectedItem.GetLatestRuntimeConfig(); config != nil {
		summary := "{}"
		if config.GetSummary() != nil {
			summary = config.GetSummary().String()
		}
		util.Fprintf(&out, "  runtimeConfig:     fingerprint=%s observedAt=%s\n", config.GetFingerprint(), formatTimestamp(config.GetObservedAt()))
		util.Fprintf(&out, "  runtimeSummary:    %s\n", summary)
	}
	return strings.TrimRight(out.String(), "\n")
}

func renderProjects(items []*controlplanev1.Project, selected int, activeProjectID string) string {
	if len(items) == 0 {
		return "no projects"
	}

	var out strings.Builder
	out.WriteString("Projects\n")
	for idx, item := range items {
		scope := ""
		if item.GetId() == strings.TrimSpace(activeProjectID) {
			scope = " *active"
		}
		out.WriteString(renderListLine(idx == selected, fmt.Sprintf("%s (%s)%s",
			displayName(item.GetDisplayName(), item.GetName()),
			item.GetId(),
			scope,
		)))
		out.WriteString("\n")
	}

	selectedItem := items[clampIndex(selected, len(items))]
	out.WriteString("\nSelected\n")
	util.Fprintf(&out, "  id:         %s\n", selectedItem.GetId())
	util.Fprintf(&out, "  name:       %s\n", selectedItem.GetName())
	util.Fprintf(&out, "  owner:      %s\n", selectedItem.GetOwnerUserId())
	util.Fprintf(&out, "  quota:      services=%d cpu=%dm mem=%dMi\n",
		selectedItem.GetQuota().GetMaxServices(),
		selectedItem.GetQuota().GetCpuMilli(),
		selectedItem.GetQuota().GetMemoryMi(),
	)
	util.Fprintf(&out, "  createdAt:  %s\n", formatTimestamp(selectedItem.GetCreatedAt()))
	return strings.TrimRight(out.String(), "\n")
}

func renderServices(activeProjectName string, activeProjectID string, items []*controlplanev1.Service, selected int, createForm *serviceCreateForm) string {
	projectLabel := firstNonEmpty(activeProjectName, activeProjectID, "(none)")
	var out strings.Builder
	util.Fprintf(&out, "Services for project: %s\n", projectLabel)
	if createForm != nil {
		out.WriteString("\nCreate Service\n")
		out.WriteString(renderServiceCreateForm(createForm))
		out.WriteString("\n")
	}
	if strings.TrimSpace(activeProjectID) == "" {
		out.WriteString("load a project first from the Projects tab")
		return out.String()
	}
	if len(items) == 0 {
		out.WriteString("no services")
		return out.String()
	}

	for idx, item := range items {
		status := item.GetStatus().GetPhase()
		rollout := item.GetStatus().GetRollout().GetPhase()
		out.WriteString(renderListLine(idx == selected, fmt.Sprintf("%s image=%s status=%s rollout=%s",
			displayName(item.GetMetadata().GetDisplayName(), item.GetMetadata().GetName()),
			item.GetSpec().GetImage(),
			status,
			rollout,
		)))
		out.WriteString("\n")
	}

	selectedItem := items[clampIndex(selected, len(items))]
	out.WriteString("\nSelected\n")
	util.Fprintf(&out, "  id:            %s\n", selectedItem.GetMetadata().GetId())
	util.Fprintf(&out, "  provider:      %s\n", selectedItem.GetSpec().GetProvider())
	util.Fprintf(&out, "  region:        %s\n", selectedItem.GetSpec().GetRegion())
	util.Fprintf(&out, "  image:         %s\n", selectedItem.GetSpec().GetImage())
	util.Fprintf(&out, "  replicas:      %d\n", selectedItem.GetSpec().GetReplicas())
	util.Fprintf(&out, "  instanceClass: %s\n", selectedItem.GetSpec().GetInstanceClass())
	util.Fprintf(&out, "  exposure:      %s\n", selectedItem.GetSpec().GetExposure())
	util.Fprintf(&out, "  desiredState:  %s\n", selectedItem.GetStatus().GetDesiredState())
	util.Fprintf(&out, "  status:        %s healthy=%t msg=%s\n",
		selectedItem.GetStatus().GetPhase(),
		selectedItem.GetStatus().GetHealthy(),
		selectedItem.GetStatus().GetMessage(),
	)
	if currentRevision := selectedItem.GetStatus().GetCurrentRevision(); currentRevision != nil {
		util.Fprintf(&out, "  currentRev:    %s\n", currentRevision.GetId())
	}
	if selection := selectedItem.GetStatus().GetPlacement(); selection != nil {
		util.Fprintf(&out, "  placement:     plane=%s status=%s healthy=%t\n",
			selection.GetPlaneId(),
			selection.GetRemoteStatus(),
			selection.GetRemoteHealthy(),
		)
	}
	if rollout := selectedItem.GetStatus().GetRollout(); rollout != nil {
		util.Fprintf(&out, "  rollout:       phase=%s stable=%s candidate=%s\n",
			rollout.GetPhase(),
			revisionID(rollout.GetStableRevision(), rollout.GetStableRevisionId()),
			revisionID(rollout.GetCandidateRevision(), rollout.GetCandidateRevisionId()),
		)
		util.Fprintf(&out, "  rolloutRep:    stable %d/%d/%d  candidate %d/%d/%d\n",
			rollout.GetStableAvailableReplicas(),
			rollout.GetStableReadyReplicas(),
			rollout.GetStableDesiredReplicas(),
			rollout.GetCandidateAvailableReplicas(),
			rollout.GetCandidateReadyReplicas(),
			rollout.GetCandidateDesiredReplicas(),
		)
	}
	return strings.TrimRight(out.String(), "\n")
}

func renderServiceCreateForm(form *serviceCreateForm) string {
	if form == nil {
		return ""
	}
	var out strings.Builder
	out.WriteString("  edit current field directly | tab/j/k move | backspace delete | ctrl+u clear | enter submit | esc cancel\n")
	for idx, field := range form.fields {
		prefix := "  "
		if idx == form.cursor {
			prefix = "> "
		}
		value := field.value
		if strings.TrimSpace(value) == "" {
			value = "<empty>"
		}
		util.Fprintf(&out, "%s%-16s %s\n", prefix, field.label+":", value)
		if strings.TrimSpace(field.description) != "" && idx == form.cursor {
			util.Fprintf(&out, "    %s\n", field.description)
		}
	}
	return strings.TrimRight(out.String(), "\n")
}

func renderListLine(selected bool, line string) string {
	prefix := "  "
	if selected {
		prefix = "> "
	}
	return prefix + line
}

func displayName(display string, fallback string) string {
	if trimmed := strings.TrimSpace(display); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(fallback)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func revisionID(revision *controlplanev1.ServiceCurrentRevision, fallback string) string {
	if revision != nil && strings.TrimSpace(revision.GetId()) != "" {
		return revision.GetId()
	}
	return strings.TrimSpace(fallback)
}

func formatTimestamp(value *timestamppb.Timestamp) string {
	if value == nil {
		return "-"
	}
	t := value.AsTime()
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}
