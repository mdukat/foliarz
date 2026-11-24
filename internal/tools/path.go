package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolvePathInWorkDir resolves a tool path and rejects paths outside the
// working directory from ctx, including paths that escape through symlinks.
func resolvePathInWorkDir(ctx context.Context, path string) (string, error) {
	workDir := WorkDir(ctx)
	root, err := filepath.Abs(workDir)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	root = canonicalPath(root)

	resolved := resolvePath(ctx, path)
	absoluteTarget, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}
	target := canonicalPath(absoluteTarget)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q is outside the working directory %q", path, workDir)
	}
	return resolved, nil
}

// canonicalPath resolves symlinks in the existing part of path and keeps the
// missing suffix as-is, so new files can be checked before they are created.
func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	return filepath.Join(canonicalPath(parent), filepath.Base(path))
}

// resolvePath resolves a tool path against the working directory from ctx.
// It expands a leading "~" and strips a leading "@".
func resolvePath(ctx context.Context, path string) string {
	resolved := strings.TrimSpace(path)
	resolved = strings.TrimPrefix(resolved, "@")

	if resolved == "~" || strings.HasPrefix(resolved, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			resolved = filepath.Join(home, strings.TrimPrefix(resolved, "~"))
		}
	}
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(WorkDir(ctx), resolved)
	}
	return filepath.Clean(resolved)
}
