package persistentdir

import (
	"errors"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"mini-cloud/internal/common/projectedfile"
)

const DefaultHostRoot = "/var/lib/mini-cloud/persistent-dirs"

var (
	ErrNameRequired       = errors.New("name is required")
	ErrInvalidName        = errors.New("name must use lowercase letters, digits, and hyphens")
	ErrMountPathRequired  = errors.New("mountPath is required")
	ErrMountPathAbsolute  = errors.New("mountPath must be an absolute container directory path")
	ErrMountPathInvalid   = errors.New("mountPath must not be /")
	ErrDuplicateName      = errors.New("persistent dir names must be unique")
	ErrDuplicateMountPath = errors.New("persistent dir mountPath values must be unique")
	ErrNestedMountPath    = errors.New("persistent dir mountPath values must not nest each other")
	ErrProjectedConflict  = errors.New("persistent dir mountPath values must not overlap projected file mountPath values")
	ErrSourcePathRequired = errors.New("sourcePath is required")
	ErrSourcePathAbsolute = errors.New("sourcePath must be an absolute host path")
	ErrServiceIDRequired  = errors.New("serviceID is required")
	persistentNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

type Spec struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
}

type Mount struct {
	Name       string `json:"name"`
	MountPath  string `json:"mountPath"`
	SourcePath string `json:"sourcePath"`
}

func (s Spec) Normalized() Spec {
	out := s
	out.Name = strings.TrimSpace(out.Name)
	out.MountPath = normalizeMountPath(out.MountPath)
	return out
}

func (s Spec) Validate() error {
	normalized := s.Normalized()
	if normalized.Name == "" {
		return ErrNameRequired
	}
	if !persistentNamePattern.MatchString(normalized.Name) {
		return ErrInvalidName
	}
	if normalized.MountPath == "" {
		return ErrMountPathRequired
	}
	if !strings.HasPrefix(normalized.MountPath, "/") {
		return ErrMountPathAbsolute
	}
	if normalized.MountPath == "/" {
		return ErrMountPathInvalid
	}
	return nil
}

func ValidateSpecs(items []Spec) error {
	if len(items) == 0 {
		return nil
	}
	seenNames := make(map[string]struct{}, len(items))
	seenMounts := make(map[string]struct{}, len(items))
	for _, item := range items {
		normalized := item.Normalized()
		if err := normalized.Validate(); err != nil {
			return err
		}
		if _, exists := seenNames[normalized.Name]; exists {
			return ErrDuplicateName
		}
		if _, exists := seenMounts[normalized.MountPath]; exists {
			return ErrDuplicateMountPath
		}
		seenNames[normalized.Name] = struct{}{}
		seenMounts[normalized.MountPath] = struct{}{}
	}
	return nil
}

func ValidateContainerInputs(dirs []Spec, files []projectedfile.Spec) error {
	if err := ValidateSpecs(dirs); err != nil {
		return err
	}
	normalizedDirs := CloneSpecs(dirs)
	for i := 0; i < len(normalizedDirs); i++ {
		for j := i + 1; j < len(normalizedDirs); j++ {
			if pathDescendsFrom(normalizedDirs[i].MountPath, normalizedDirs[j].MountPath) ||
				pathDescendsFrom(normalizedDirs[j].MountPath, normalizedDirs[i].MountPath) {
				return ErrNestedMountPath
			}
		}
	}
	for _, dir := range normalizedDirs {
		for _, file := range projectedfile.CloneSpecs(files) {
			if pathEqualsOrDescendsFrom(file.MountPath, dir.MountPath) ||
				pathEqualsOrDescendsFrom(dir.MountPath, file.MountPath) {
				return ErrProjectedConflict
			}
		}
	}
	return nil
}

func CloneSpecs(items []Spec) []Spec {
	if len(items) == 0 {
		return nil
	}
	out := make([]Spec, len(items))
	for i := range items {
		out[i] = items[i].Normalized()
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].MountPath < out[j].MountPath
	})
	return out
}

func (m Mount) Normalized() Mount {
	out := m
	out.Name = strings.TrimSpace(out.Name)
	out.MountPath = normalizeMountPath(out.MountPath)
	out.SourcePath = normalizeSourcePath(out.SourcePath)
	return out
}

func (m Mount) Validate() error {
	normalized := m.Normalized()
	if err := (Spec{Name: normalized.Name, MountPath: normalized.MountPath}).Validate(); err != nil {
		return err
	}
	if normalized.SourcePath == "" {
		return ErrSourcePathRequired
	}
	if !filepath.IsAbs(normalized.SourcePath) {
		return ErrSourcePathAbsolute
	}
	return nil
}

func CloneMounts(items []Mount) []Mount {
	if len(items) == 0 {
		return nil
	}
	out := make([]Mount, len(items))
	for i := range items {
		out[i] = items[i].Normalized()
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		if out[i].MountPath != out[j].MountPath {
			return out[i].MountPath < out[j].MountPath
		}
		return out[i].SourcePath < out[j].SourcePath
	})
	return out
}

func MaterializeMounts(root string, serviceID string, specs []Spec) ([]Mount, error) {
	if err := ValidateSpecs(specs); err != nil {
		return nil, err
	}
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return nil, ErrServiceIDRequired
	}
	root = strings.TrimSpace(root)
	if root == "" {
		root = DefaultHostRoot
	}
	out := make([]Mount, 0, len(specs))
	for _, item := range CloneSpecs(specs) {
		mount := Mount{
			Name:       item.Name,
			MountPath:  item.MountPath,
			SourcePath: filepath.Join(root, serviceID, item.Name),
		}
		if err := mount.Validate(); err != nil {
			return nil, err
		}
		out = append(out, mount)
	}
	return out, nil
}

func normalizeMountPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	cleaned := path.Clean(trimmed)
	if !strings.HasPrefix(cleaned, "/") {
		return trimmed
	}
	return cleaned
}

func normalizeSourcePath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}

func pathDescendsFrom(pathValue string, parent string) bool {
	if pathValue == parent {
		return false
	}
	return strings.HasPrefix(pathValue, parent+"/")
}

func pathEqualsOrDescendsFrom(pathValue string, parent string) bool {
	return pathValue == parent || pathDescendsFrom(pathValue, parent)
}
