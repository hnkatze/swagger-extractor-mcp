package config

import (
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
	expected := "json"
	if cfg.DefaultFormat != expected {
		t.Errorf("DefaultFormat = %q, want %q", cfg.DefaultFormat, expected)
	}
}
