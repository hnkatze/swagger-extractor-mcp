package config

import "time"

// Config holds the server configuration.
type Config struct {
	CacheTTL      time.Duration
	MaxCacheSize  int
	MaxSpecSize   int64
	FetchTimeout  time.Duration
	DefaultFormat string
}

// Default returns the default configuration.
func Default() Config {
	return Config{
		CacheTTL:      5 * time.Minute,
		MaxCacheSize:  20,
		MaxSpecSize:   20 * 1024 * 1024, // 20MB
		FetchTimeout:  30 * time.Second,
		DefaultFormat: "json",
	}
}
