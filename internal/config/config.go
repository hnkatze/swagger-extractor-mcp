package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds the server configuration.
type Config struct {
	CacheTTL         time.Duration
	MaxCacheSize     int
	MaxSpecSize      int64
	FetchTimeout     time.Duration
	DefaultFormat    string
	DefaultLimit     int           // Max results for list/search (0 = unlimited)
	CacheDir         string        // Disk cache directory path
	DiskCacheTTL     time.Duration // TTL for disk cache entries
	ConditionalFetch bool          // Enable HTTP conditional requests (ETags)
	MaxDiskEntries   int           // Max number of cached specs on disk
}

// Default returns the default configuration.
func Default() Config {
	return Config{
		CacheTTL:         5 * time.Minute,
		MaxCacheSize:     20,
		MaxSpecSize:      20 * 1024 * 1024, // 20MB
		FetchTimeout:     30 * time.Second,
		DefaultFormat:    "toon",
		DefaultLimit:     50,
		CacheDir:         "",
		DiskCacheTTL:     24 * time.Hour,
		ConditionalFetch: true,
		MaxDiskEntries:   50,
	}
}

// Load returns a Config populated from defaults and environment variables.
// Environment variables:
//   - SWAGGER_MCP_CACHE_DIR         → CacheDir
//   - SWAGGER_MCP_DISK_CACHE_TTL    → DiskCacheTTL (time.ParseDuration format)
//   - SWAGGER_MCP_CONDITIONAL_FETCH → ConditionalFetch ("true"/"false"/"1"/"0")
//   - SWAGGER_MCP_MAX_DISK_ENTRIES  → MaxDiskEntries (integer)
//   - SWAGGER_MCP_DEFAULT_FORMAT    → DefaultFormat ("json" or "toon")
//   - SWAGGER_MCP_DEFAULT_LIMIT     → DefaultLimit (integer, 0 = unlimited)
func Load() Config {
	cfg := Default()

	if v := os.Getenv("SWAGGER_MCP_CACHE_DIR"); v != "" {
		cfg.CacheDir = v
	}

	if v := os.Getenv("SWAGGER_MCP_DEFAULT_FORMAT"); v != "" {
		switch strings.ToLower(v) {
		case "json", "toon":
			cfg.DefaultFormat = strings.ToLower(v)
		}
	}

	if v := os.Getenv("SWAGGER_MCP_DEFAULT_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.DefaultLimit = n
		}
	}

	if v := os.Getenv("SWAGGER_MCP_DISK_CACHE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.DiskCacheTTL = d
		}
	}

	if v := os.Getenv("SWAGGER_MCP_CONDITIONAL_FETCH"); v != "" {
		switch strings.ToLower(v) {
		case "true", "1":
			cfg.ConditionalFetch = true
		case "false", "0":
			cfg.ConditionalFetch = false
		}
	}

	if v := os.Getenv("SWAGGER_MCP_MAX_DISK_ENTRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxDiskEntries = n
		}
	}

	// Resolve CacheDir if still empty.
	if cfg.CacheDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cfg.CacheDir = filepath.Join(home, ".swagger-mcp", "cache")
		}
	}

	return cfg
}
