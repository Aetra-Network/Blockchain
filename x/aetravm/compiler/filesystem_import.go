package compiler

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NewFilesystemImportResolver resolves project-local .atlx imports from a root directory.
// Package-style imports continue to fall back to deterministic lock-only entries.
func NewFilesystemImportResolver(root string) DependencyResolver {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = filepath.Clean(root)
	}
	return &filesystemImportResolver{root: absRoot}
}

type filesystemImportResolver struct {
	root string
}

func (r *filesystemImportResolver) ResolveImport(imp ImportDecl) (ResolvedDependency, *SourceFile, error) {
	if r == nil {
		return packageImportDependency(imp), nil, nil
	}

	if isProjectLocalImport(imp.Path) {
		path, err := r.resolveLocalPath(imp.Path)
		if err != nil {
			return ResolvedDependency{}, nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return ResolvedDependency{}, nil, err
		}
		parsed, err := ParseSourceNamed(path, string(data))
		if err != nil {
			return ResolvedDependency{}, nil, err
		}
		hash := sha256.Sum256(data)
		dep := dependencyFromParts(filepath.ToSlash(r.relativePath(path)), imp.Version, imp.Alias, hash, hash)
		return dep, parsed, nil
	}

	return packageImportDependency(imp), nil, nil
}

func (r *filesystemImportResolver) resolveLocalPath(importPath string) (string, error) {
	if r.root == "" {
		return "", fmt.Errorf("filesystem import resolver has empty root")
	}

	normalized := filepath.Clean(filepath.FromSlash(importPath))
	candidate := normalized
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(r.root, candidate)
	}
	candidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(r.root, candidate)
	if err != nil {
		return "", err
	}
	relSlash := filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(relSlash, "../") {
		return "", fmt.Errorf("import %q escapes project root %q", importPath, r.root)
	}

	info, err := os.Stat(candidate)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("import %q resolved to directory %q, expected file", importPath, candidate)
	}
	return candidate, nil
}

func (r *filesystemImportResolver) relativePath(path string) string {
	if r == nil || r.root == "" {
		return filepath.Clean(path)
	}
	rel, err := filepath.Rel(r.root, path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(rel)
}

func packageImportDependency(imp ImportDecl) ResolvedDependency {
	sum := sha256.Sum256([]byte(imp.Path + "@" + imp.Version))
	return dependencyFromParts(imp.Path, imp.Version, imp.Alias, sum, sum)
}

func isProjectLocalImport(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if strings.HasSuffix(lower, ".atlx") {
		return true
	}
	return strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") || strings.HasPrefix(trimmed, ".\\") || strings.HasPrefix(trimmed, "..\\")
}

// CompileFile compiles a single file and resolves local project imports
// relative to that file's directory when no custom resolver is provided.
func (c *Compiler) CompileFile(path string) (*Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	clone := *c
	if clone.opts.Resolver == nil {
		clone.opts.Resolver = NewFilesystemImportResolver(filepath.Dir(path))
	}
	return clone.CompileFiles([]NamedSource{{Name: path, Data: data}})
}
