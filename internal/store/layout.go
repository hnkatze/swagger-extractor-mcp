// Package store owns the local index: a SQLite database inside a swa-mcp/
// directory that belongs to the consuming project (S2). The index command
// writes it explicitly (S3); every query reads it, never the network.
package store

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// DirName is the directory the indexer creates in the project root.
	DirName = "swa-mcp"
	// DBFileName is the SQLite file inside DirName.
	DBFileName = "swa-mcp.db"
	// EnvDir overrides store discovery with an explicit store directory.
	EnvDir = "SWA_MCP_DIR"
)

// FindRoot walks up from start looking for a directory that already contains a
// swa-mcp store directory and returns that project root. It reports false when
// no store exists above start, which lets the indexer fall back to the working
// directory instead of guessing a project root.
func FindRoot(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		info, statErr := os.Stat(filepath.Join(dir, DirName))
		if statErr == nil && info.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// DirFor returns the store directory to use when working in workDir:
// SWA_MCP_DIR wins, then the store directory of the nearest project root above
// workDir, otherwise workDir/swa-mcp (created by the caller on first write).
func DirFor(workDir string) (string, error) {
	if override := strings.TrimSpace(os.Getenv(EnvDir)); override != "" {
		return filepath.Abs(override)
	}
	if root, ok := FindRoot(workDir); ok {
		return filepath.Join(root, DirName), nil
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(abs, DirName), nil
}
