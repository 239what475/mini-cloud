package terraformlab

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CopyTree 把项目里的 deploy/terraform 复制到一个隔离临时目录。
// 真实云 E2E 需要同时保留多套 state，所以不能直接在仓库工作目录上跑 apply/destroy。
func CopyTree(projectDir string, destinationRoot string) (string, error) {
	sourceRoot := filepath.Join(projectDir, "deploy", "terraform")
	targetRoot := filepath.Join(destinationRoot, "terraform")

	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return "", fmt.Errorf("create terraform sandbox root %s: %w", targetRoot, err)
	}

	if err := filepath.WalkDir(sourceRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return fmt.Errorf("resolve relative path for %s: %w", path, err)
		}
		if relPath == "." {
			return nil
		}

		if shouldSkip(relPath, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		targetPath := filepath.Join(targetRoot, relPath)
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}

		if d.IsDir() {
			if err := os.MkdirAll(targetPath, info.Mode().Perm()); err != nil {
				return fmt.Errorf("create directory %s: %w", targetPath, err)
			}
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read file %s: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("create parent directory for %s: %w", targetPath, err)
		}
		if err := os.WriteFile(targetPath, data, info.Mode().Perm()); err != nil {
			return fmt.Errorf("write file %s: %w", targetPath, err)
		}
		return nil
	}); err != nil {
		return "", err
	}

	return targetRoot, nil
}

func shouldSkip(relPath string, d fs.DirEntry) bool {
	base := strings.TrimSpace(d.Name())
	if base == "" {
		return false
	}

	if d.IsDir() && base == ".terraform" {
		return true
	}
	if base == "tfplan" {
		return true
	}
	if strings.HasPrefix(base, "terraform.tfstate") {
		return true
	}
	return false
}
