package app

import (
	"fmt"
	"strconv"
	"strings"

	controlplanev1 "mini-cloud/internal/gen/proto/minicloud/controlplane/v1"
)

type serviceCreateForm struct {
	cursor int
	fields []serviceCreateField
}

type serviceCreateField struct {
	key         string
	label       string
	value       string
	required    bool
	description string
}

func newServiceCreateForm(planes []*controlplanev1.Plane) *serviceCreateForm {
	defaultProvider := "aliyun"
	defaultRegion := "cn-beijing"
	if selected := preferredPlane(planes); selected != nil {
		if strings.TrimSpace(selected.GetProvider()) != "" {
			defaultProvider = selected.GetProvider()
		}
		if strings.TrimSpace(selected.GetRegion()) != "" {
			defaultRegion = selected.GetRegion()
		}
	}
	return &serviceCreateForm{
		fields: []serviceCreateField{
			{key: "name", label: "name", required: true, description: "service id, lowercase and hyphen only"},
			{key: "display_name", label: "displayName", required: true, description: "operator-facing display name"},
			{key: "provider", label: "provider", value: defaultProvider, required: true, description: "aliyun or tencent"},
			{key: "region", label: "region", value: defaultRegion, required: true, description: "target region"},
			{key: "replicas", label: "replicas", value: "1", required: true, description: "desired replicas"},
			{key: "instance_class", label: "instanceClass", value: "small", required: true, description: "small, medium, or large"},
			{key: "exposure", label: "exposure", value: "public", required: true, description: "public or private"},
			{key: "image", label: "image", required: true, description: "container image"},
			{key: "default_port", label: "defaultPort", value: "8080", required: true, description: "container port"},
			{key: "readiness_path", label: "readinessPath", value: "/healthz", required: true, description: "HTTP readiness path"},
			{key: "projected_files", label: "projectedFiles", description: "optional: mountPath|sourceKind|sourceID|sourceKey;..."},
			{key: "persistent_dirs", label: "persistentDirs", description: "optional: name|mountPath;... (single replica, lowercase name, no overlap)"},
		},
	}
}

func (f *serviceCreateForm) moveUp() {
	if f == nil || f.cursor <= 0 {
		return
	}
	f.cursor--
}

func (f *serviceCreateForm) moveDown() {
	if f == nil || f.cursor >= len(f.fields)-1 {
		return
	}
	f.cursor++
}

func (f *serviceCreateForm) appendRunes(runes []rune) {
	if f == nil || f.cursor < 0 || f.cursor >= len(f.fields) {
		return
	}
	f.fields[f.cursor].value += string(runes)
}

func (f *serviceCreateForm) backspace() {
	if f == nil || f.cursor < 0 || f.cursor >= len(f.fields) {
		return
	}
	value := []rune(f.fields[f.cursor].value)
	if len(value) == 0 {
		return
	}
	f.fields[f.cursor].value = string(value[:len(value)-1])
}

func (f *serviceCreateForm) clear() {
	if f == nil || f.cursor < 0 || f.cursor >= len(f.fields) {
		return
	}
	f.fields[f.cursor].value = ""
}

func (f *serviceCreateForm) request(projectID string) (*controlplanev1.CreateServiceRequest, error) {
	values := make(map[string]string, len(f.fields))
	for _, field := range f.fields {
		value := strings.TrimSpace(field.value)
		if field.required && value == "" {
			return nil, fmt.Errorf("%s is required", field.label)
		}
		values[field.key] = value
	}

	replicas, err := strconv.Atoi(values["replicas"])
	if err != nil {
		return nil, fmt.Errorf("replicas must be an integer")
	}
	defaultPort, err := strconv.Atoi(values["default_port"])
	if err != nil {
		return nil, fmt.Errorf("defaultPort must be an integer")
	}
	projectedFiles, err := parseProjectedFiles(values["projected_files"])
	if err != nil {
		return nil, err
	}
	persistentDirs, err := parsePersistentDirs(values["persistent_dirs"])
	if err != nil {
		return nil, err
	}

	return &controlplanev1.CreateServiceRequest{
		ProjectId:   strings.TrimSpace(projectID),
		Name:        values["name"],
		DisplayName: values["display_name"],
		Spec: &controlplanev1.ServiceSpec{
			Provider:       values["provider"],
			Region:         values["region"],
			Replicas:       int32(replicas),
			InstanceClass:  values["instance_class"],
			Exposure:       values["exposure"],
			Image:          values["image"],
			DefaultPort:    int32(defaultPort),
			ReadinessPath:  values["readiness_path"],
			ProjectedFiles: projectedFiles,
			PersistentDirs: persistentDirs,
			RevisionPolicy: &controlplanev1.ServiceRevisionPolicy{
				Strategy: "candidate",
			},
		},
	}, nil
}

func parseProjectedFiles(raw string) ([]*controlplanev1.ProjectedFileSpec, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	entries := strings.Split(raw, ";")
	out := make([]*controlplanev1.ProjectedFileSpec, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.Split(entry, "|")
		if len(parts) != 4 {
			return nil, fmt.Errorf("projectedFiles entry %q must use mountPath|sourceKind|sourceID|sourceKey", entry)
		}
		item := &controlplanev1.ProjectedFileSpec{
			MountPath:  strings.TrimSpace(parts[0]),
			SourceKind: strings.TrimSpace(parts[1]),
			SourceId:   strings.TrimSpace(parts[2]),
			SourceKey:  strings.TrimSpace(parts[3]),
		}
		if item.GetMountPath() == "" || item.GetSourceKind() == "" || item.GetSourceId() == "" || item.GetSourceKey() == "" {
			return nil, fmt.Errorf("projectedFiles entry %q must not contain empty fields", entry)
		}
		out = append(out, item)
	}
	return out, nil
}

func parsePersistentDirs(raw string) ([]*controlplanev1.PersistentDirSpec, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	entries := strings.Split(raw, ";")
	out := make([]*controlplanev1.PersistentDirSpec, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.Split(entry, "|")
		if len(parts) != 2 {
			return nil, fmt.Errorf("persistentDirs entry %q must use name|mountPath", entry)
		}
		item := &controlplanev1.PersistentDirSpec{
			Name:      strings.TrimSpace(parts[0]),
			MountPath: strings.TrimSpace(parts[1]),
		}
		if item.GetName() == "" || item.GetMountPath() == "" {
			return nil, fmt.Errorf("persistentDirs entry %q must not contain empty fields", entry)
		}
		out = append(out, item)
	}
	return out, nil
}

func preferredPlane(planes []*controlplanev1.Plane) *controlplanev1.Plane {
	for _, item := range planes {
		if item.GetStatus().GetStatus() == "ready" {
			return item
		}
	}
	if len(planes) == 0 {
		return nil
	}
	return planes[0]
}
