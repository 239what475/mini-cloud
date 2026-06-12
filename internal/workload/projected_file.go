package workload

import (
	"errors"
	"path"
	"sort"
	"strings"
)

const (
	DefaultConfigFileMode uint32 = 0444
	DefaultSecretFileMode uint32 = 0400
)

var (
	ErrMountPathRequired  = errors.New("mountPath is required")
	ErrMountPathAbsolute  = errors.New("mountPath must be an absolute container file path")
	ErrMountPathInvalid   = errors.New("mountPath must target a file, not the container root")
	ErrDuplicateMountPath = errors.New("projected file mountPath values must be unique")
	ErrContentRequired    = errors.New("content is required")
	ErrFileModeInvalid    = errors.New("mode must be between 1 and 0777")
)

type ProjectedFile struct {
	MountPath string `json:"mountPath"`
	Content   string `json:"content"`
	Mode      uint32 `json:"mode"`
	Sensitive bool   `json:"sensitive"`
}

func (f ProjectedFile) Normalized() ProjectedFile {
	out := f
	out.MountPath = normalizeMountPath(out.MountPath)
	if out.Mode == 0 {
		if out.Sensitive {
			out.Mode = DefaultSecretFileMode
		} else {
			out.Mode = DefaultConfigFileMode
		}
	}
	return out
}

func (f ProjectedFile) Validate() error {
	normalized := f.Normalized()
	if normalized.MountPath == "" {
		return ErrMountPathRequired
	}
	if !strings.HasPrefix(normalized.MountPath, "/") {
		return ErrMountPathAbsolute
	}
	if normalized.MountPath == "/" {
		return ErrMountPathInvalid
	}
	if normalized.Content == "" {
		return ErrContentRequired
	}
	if normalized.Mode == 0 || normalized.Mode > 0777 {
		return ErrFileModeInvalid
	}
	return nil
}

func ValidateProjectedFiles(items []ProjectedFile) error {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		normalized := item.Normalized()
		if err := normalized.Validate(); err != nil {
			return err
		}
		if _, exists := seen[normalized.MountPath]; exists {
			return ErrDuplicateMountPath
		}
		seen[normalized.MountPath] = struct{}{}
	}
	return nil
}

func CloneProjectedFiles(items []ProjectedFile) []ProjectedFile {
	if len(items) == 0 {
		return nil
	}
	out := make([]ProjectedFile, len(items))
	for i := range items {
		out[i] = items[i].Normalized()
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].MountPath < out[j].MountPath
	})
	return out
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
