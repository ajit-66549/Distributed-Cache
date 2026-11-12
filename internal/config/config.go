package config

import (
	"errors"
	"os"
	"strconv"

	"github.com/ajit-66549/distributed-cache/internal/cache"
)

type Config struct {
	Port     string
	MaxBytes int64
}

func Load() Config {
	port := os.Getenv("CACHE_PORT")

	if port == "" {
		port = "8080"
	}

	maxBytes := cache.DefaultMaxBytes
	if value := os.Getenv("CACHE_MAX_BYTES"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			// Mark invalid input so Validate rejects it during startup.
			parsed = 0
		}
		maxBytes = parsed
	}

	return Config{
		Port:     port,
		MaxBytes: maxBytes,
	}
}

func (c Config) Validate() error {
	if c.MaxBytes <= 0 {
		return errors.New("CACHE_MAX_BYTES must be a positive integer in bytes")
	}
	return nil
}
