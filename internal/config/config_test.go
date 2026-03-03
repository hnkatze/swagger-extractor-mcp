package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefault_CacheTTL(t *testing.T) {
	cfg := Default()
	expected := 5 * time.Minute
	if cfg.CacheTTL != expected {
		t.Errorf("CacheTTL = %v, want %v", cfg.CacheTTL, expected)
	}
}

func TestDefault_MaxCacheSize(t *testing.T) {
	cfg := Default()
	expected := 20
	if cfg.MaxCacheSize != expected {
		t.Errorf("MaxCacheSize = %d, want %d", cfg.MaxCacheSize, expected)
	}
}

func TestDefault_MaxSpecSize(t *testing.T) {
	cfg := Default()
	expected := int64(20 * 1024 * 1024)
	if cfg.MaxSpecSize != expected {
		t.Errorf("MaxSpecSize = %d, want %d", cfg.MaxSpecSize, expected)
	}
}

func TestDefault_FetchTimeout(t *testing.T) {
	cfg := Default()
	expected := 30 * time.Second
	if cfg.FetchTimeout != expected {
		t.Errorf("FetchTimeout = %v, want %v", cfg.FetchTimeout, expected)
	}
}

func TestDefault_DefaultFormat(t *testing.T) {
	cfg := Default()
	expected := "toon"
	if cfg.DefaultFormat != expected {
		t.Errorf("DefaultFormat = %q, want %q", cfg.DefaultFormat, expected)
	}
}

func TestDefault_DefaultLimit(t *testing.T) {
	cfg := Default()
	expected := 50
	if cfg.DefaultLimit != expected {
		t.Errorf("DefaultLimit = %d, want %d", cfg.DefaultLimit, expected)
	}
}

func TestDefault_NewFields(t *testing.T) {
	cfg := Default()

	if cfg.CacheDir != "" {
		t.Errorf("CacheDir = %q, want empty string", cfg.CacheDir)
	}
	if cfg.DiskCacheTTL != 24*time.Hour {
		t.Errorf("DiskCacheTTL = %v, want %v", cfg.DiskCacheTTL, 24*time.Hour)
	}
	if cfg.ConditionalFetch != true {
		t.Errorf("ConditionalFetch = %v, want true", cfg.ConditionalFetch)
	}
	if cfg.MaxDiskEntries != 50 {
		t.Errorf("MaxDiskEntries = %d, want 50", cfg.MaxDiskEntries)
	}
}

func TestLoad_Defaults(t *testing.T) {
	cfg := Load()

	// DiskCacheTTL should keep default when no env var is set.
	if cfg.DiskCacheTTL != 24*time.Hour {
		t.Errorf("DiskCacheTTL = %v, want %v", cfg.DiskCacheTTL, 24*time.Hour)
	}
	if cfg.ConditionalFetch != true {
		t.Errorf("ConditionalFetch = %v, want true", cfg.ConditionalFetch)
	}
	if cfg.MaxDiskEntries != 50 {
		t.Errorf("MaxDiskEntries = %d, want 50", cfg.MaxDiskEntries)
	}
	// Existing fields should also be present.
	if cfg.CacheTTL != 5*time.Minute {
		t.Errorf("CacheTTL = %v, want %v", cfg.CacheTTL, 5*time.Minute)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("SWAGGER_MCP_CACHE_DIR", "/tmp/my-cache")
	t.Setenv("SWAGGER_MCP_DISK_CACHE_TTL", "2h")
	t.Setenv("SWAGGER_MCP_CONDITIONAL_FETCH", "false")
	t.Setenv("SWAGGER_MCP_MAX_DISK_ENTRIES", "100")
	t.Setenv("SWAGGER_MCP_DEFAULT_FORMAT", "json")
	t.Setenv("SWAGGER_MCP_DEFAULT_LIMIT", "100")

	cfg := Load()

	if cfg.CacheDir != "/tmp/my-cache" {
		t.Errorf("CacheDir = %q, want %q", cfg.CacheDir, "/tmp/my-cache")
	}
	if cfg.DiskCacheTTL != 2*time.Hour {
		t.Errorf("DiskCacheTTL = %v, want %v", cfg.DiskCacheTTL, 2*time.Hour)
	}
	if cfg.ConditionalFetch != false {
		t.Errorf("ConditionalFetch = %v, want false", cfg.ConditionalFetch)
	}
	if cfg.MaxDiskEntries != 100 {
		t.Errorf("MaxDiskEntries = %d, want 100", cfg.MaxDiskEntries)
	}
	if cfg.DefaultFormat != "json" {
		t.Errorf("DefaultFormat = %q, want %q", cfg.DefaultFormat, "json")
	}
	if cfg.DefaultLimit != 100 {
		t.Errorf("DefaultLimit = %d, want 100", cfg.DefaultLimit)
	}
}

func TestLoad_InvalidEnvValues(t *testing.T) {
	t.Setenv("SWAGGER_MCP_DISK_CACHE_TTL", "not-a-duration")
	t.Setenv("SWAGGER_MCP_CONDITIONAL_FETCH", "maybe")
	t.Setenv("SWAGGER_MCP_MAX_DISK_ENTRIES", "abc")
	t.Setenv("SWAGGER_MCP_DEFAULT_FORMAT", "xml")
	t.Setenv("SWAGGER_MCP_DEFAULT_LIMIT", "not-a-number")

	cfg := Load()

	// Invalid values should fall back to defaults.
	if cfg.DiskCacheTTL != 24*time.Hour {
		t.Errorf("DiskCacheTTL = %v, want %v (default)", cfg.DiskCacheTTL, 24*time.Hour)
	}
	if cfg.ConditionalFetch != true {
		t.Errorf("ConditionalFetch = %v, want true (default)", cfg.ConditionalFetch)
	}
	if cfg.MaxDiskEntries != 50 {
		t.Errorf("MaxDiskEntries = %d, want 50 (default)", cfg.MaxDiskEntries)
	}
	if cfg.DefaultFormat != "toon" {
		t.Errorf("DefaultFormat = %q, want %q (default)", cfg.DefaultFormat, "toon")
	}
	if cfg.DefaultLimit != 50 {
		t.Errorf("DefaultLimit = %d, want 50 (default)", cfg.DefaultLimit)
	}
}

func TestLoad_CacheDirResolution(t *testing.T) {
	// Ensure SWAGGER_MCP_CACHE_DIR is not set so Load() resolves it.
	t.Setenv("SWAGGER_MCP_CACHE_DIR", "")

	cfg := Load()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir, skipping CacheDir resolution test")
	}

	expected := filepath.Join(home, ".swagger-mcp", "cache")
	if cfg.CacheDir != expected {
		t.Errorf("CacheDir = %q, want %q", cfg.CacheDir, expected)
	}
}
