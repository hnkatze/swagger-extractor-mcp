package config

import (
	"testing"
	"time"
)

func TestDefault_MaxSpecSize(t *testing.T) {
	cfg := Default()
	expected := int64(20 * 1024 * 1024)
	if cfg.MaxSpecSize != expected {
		t.Errorf("MaxSpecSize = %d, want %d", cfg.MaxSpecSize, expected)
	}
}

func TestDefault_FetchTimeout(t *testing.T) {
	cfg := Default()
	expected := 120 * time.Second
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

func TestLoad_Defaults(t *testing.T) {
	cfg := Load()

	if cfg.DefaultFormat != "toon" {
		t.Errorf("DefaultFormat = %q, want %q (default)", cfg.DefaultFormat, "toon")
	}
	if cfg.DefaultLimit != 50 {
		t.Errorf("DefaultLimit = %d, want 50 (default)", cfg.DefaultLimit)
	}
	if cfg.FetchTimeout != 120*time.Second {
		t.Errorf("FetchTimeout = %v, want %v (default)", cfg.FetchTimeout, 120*time.Second)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("SWAGGER_MCP_DEFAULT_FORMAT", "json")
	t.Setenv("SWAGGER_MCP_DEFAULT_LIMIT", "100")
	t.Setenv("SWAGGER_MCP_FETCH_TIMEOUT", "3m")

	cfg := Load()

	if cfg.DefaultFormat != "json" {
		t.Errorf("DefaultFormat = %q, want %q", cfg.DefaultFormat, "json")
	}
	if cfg.DefaultLimit != 100 {
		t.Errorf("DefaultLimit = %d, want 100", cfg.DefaultLimit)
	}
	if cfg.FetchTimeout != 3*time.Minute {
		t.Errorf("FetchTimeout = %v, want %v", cfg.FetchTimeout, 3*time.Minute)
	}
}

func TestLoad_InvalidEnvValues(t *testing.T) {
	t.Setenv("SWAGGER_MCP_DEFAULT_FORMAT", "xml")
	t.Setenv("SWAGGER_MCP_DEFAULT_LIMIT", "not-a-number")
	t.Setenv("SWAGGER_MCP_FETCH_TIMEOUT", "not-a-duration")

	cfg := Load()

	// Invalid values should fall back to defaults.
	if cfg.DefaultFormat != "toon" {
		t.Errorf("DefaultFormat = %q, want %q (default)", cfg.DefaultFormat, "toon")
	}
	if cfg.DefaultLimit != 50 {
		t.Errorf("DefaultLimit = %d, want 50 (default)", cfg.DefaultLimit)
	}
	if cfg.FetchTimeout != 120*time.Second {
		t.Errorf("FetchTimeout = %v, want %v (default)", cfg.FetchTimeout, 120*time.Second)
	}
}

// A zero or negative duration would make http.Client time out immediately,
// turning every index fetch into an instant failure, so it must fall back to
// the default.
func TestLoad_FetchTimeoutRejectsNonPositive(t *testing.T) {
	for _, value := range []string{"0s", "0", "-5s"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("SWAGGER_MCP_FETCH_TIMEOUT", value)

			cfg := Load()

			if cfg.FetchTimeout != 120*time.Second {
				t.Errorf("FetchTimeout = %v for %q, want %v (default)", cfg.FetchTimeout, value, 120*time.Second)
			}
		})
	}
}
