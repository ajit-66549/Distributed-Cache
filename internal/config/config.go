package config

import (
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/ajit-66549/distributed-cache/internal/cache"
)

type Config struct {
	Port             string
	MaxBytes         int64
	DataDir          string
	SnapshotInterval time.Duration
}

func Load() Config {
	port := os.Getenv("CACHE_PORT")

	if port == "" {
		port = "8080"
	}
	dataDir := os.Getenv("CACHE_DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
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
	snapshotInterval := time.Minute
	if value := os.Getenv("CACHE_SNAPSHOT_INTERVAL"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			// Mark invalid input so Validate rejects it during startup.
			parsed = 0
		}
		snapshotInterval = parsed
	}

	return Config{
		Port:             port,
		MaxBytes:         maxBytes,
		DataDir:          dataDir,
		SnapshotInterval: snapshotInterval,
	}
}

func (c Config) Validate() error {
	if c.MaxBytes <= 0 {
		return errors.New("CACHE_MAX_BYTES must be a positive integer in bytes")
	}
	if c.DataDir == "" {
		return errors.New("CACHE_DATA_DIR must not be empty")
	}
	if c.SnapshotInterval <= 0 {
		return errors.New("CACHE_SNAPSHOT_INTERVAL must be a positive Go duration, such as 10s or 1m")
	}
	return nil
}
