package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the server and indexer configuration.
//
// It contains only settings something still reads: the indexer's fetch timeout
// and spec size limit, and the defaults the tools use for formatting and paging.
// The old fetch-time cache settings are gone with the fetch path (S1).
type Config struct {
	MaxSpecSize   int64         // Largest body the indexer will read
	FetchTimeout  time.Duration // Timeout for the indexer's HTTP read
	DefaultFormat string        // Default output format ("json" or "toon")
	DefaultLimit  int           // Max results for list/search (0 = unlimited)
}

// Default returns the default configuration.
func Default() Config {
	return Config{
		MaxSpecSize:   20 * 1024 * 1024, // 20MB
		FetchTimeout:  120 * time.Second,
		DefaultFormat: "toon",
		DefaultLimit:  50,
	}
}

// Load returns a Config populated from defaults and environment variables.
// Environment variables:
//   - SWAGGER_MCP_DEFAULT_FORMAT → DefaultFormat ("json" or "toon")
//   - SWAGGER_MCP_DEFAULT_LIMIT  → DefaultLimit (integer, 0 = unlimited)
//   - SWAGGER_MCP_FETCH_TIMEOUT  → FetchTimeout (time.ParseDuration format, must be > 0)
func Load() Config {
	cfg := Default()

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

	// A zero or negative duration would make every index fetch fail instantly.
	if v := os.Getenv("SWAGGER_MCP_FETCH_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.FetchTimeout = d
		}
	}

	return cfg
}
