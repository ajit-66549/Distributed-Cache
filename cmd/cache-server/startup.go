package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/ajit-66549/distributed-cache/internal/cache"
	"github.com/ajit-66549/distributed-cache/internal/config"
	"github.com/ajit-66549/distributed-cache/internal/persistence"
)

// openStore restores the cache before it can receive HTTP requests. The caller
// owns the returned writer and closes it after draining requests on shutdown.
func openStore(cfg config.Config) (*cache.Store, *persistence.AOF, error) {
	writer, err := persistence.OpenAOF(cfg.DataDir, cfg.MaxBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("open cache persistence: %w", err)
	}

	store, err := cache.NewStoreWithLog(cfg.MaxBytes, writer)
	if err != nil {
		return nil, nil, errors.Join(err, writer.Close())
	}

	if err := writer.Replay(store.ApplyRecoveredRecord); err != nil {
		return nil, nil, errors.Join(fmt.Errorf("recover cache: %w", err), writer.Close())
	}

	store.DeleteExpired()
	log.Printf("cache recovered from %s through sequence %d (%d live entries)", cfg.DataDir, writer.LastSequence(), store.Len())
	return store, writer, nil
}
