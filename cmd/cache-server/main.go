package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ajit-66549/distributed-cache/internal/cache"
	"github.com/ajit-66549/distributed-cache/internal/config"
	"github.com/ajit-66549/distributed-cache/internal/server"
)

const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() (err error) {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	cacheStore, writer, err := openStore(cfg)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, writer.Close())
	}()
	handler := server.NewHandler(cacheStore)

	address := ":" + cfg.Port
	httpServer := server.NewHTTPServer(address, handler.Routes())

	shutdownSignal, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	workerContext, cancelWorkers := context.WithCancel(shutdownSignal)
	defer cancelWorkers()
	snapshotDone, err := cache.StartSnapshotScheduler(
		workerContext,
		cacheStore,
		cfg.SnapshotInterval,
	)
	if err != nil {
		return fmt.Errorf("start snapshot scheduler: %w", err)
	}
	defer func() {
		cancelWorkers()
		if snapshotDone != nil {
			err = errors.Join(err, <-snapshotDone)
		}
	}()
	// Force-close HTTP before waiting for snapshots on a shutdown failure.
	// The writer's earlier defer runs last, after snapshot work has finished.
	defer func() {
		err = errors.Join(err, httpServer.Close())
	}()
	log.Printf("periodic snapshots scheduled every %s", cfg.SnapshotInterval)

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("distributed cache server listening on %s", address)
		serverErrors <- httpServer.ListenAndServe()
	}()

	cache.StartExpirationCleanup(
		workerContext,
		cacheStore,
		time.Minute,
	)

	var shutdownErr error
	select {
	case <-shutdownSignal.Done():
		log.Println("shutdown signal received")
	case serveErr := <-serverErrors:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			shutdownErr = fmt.Errorf("server failed: %w", serveErr)
		}
	case snapshotErr := <-snapshotDone:
		snapshotDone = nil // The worker has already returned its completion result.
		if snapshotErr != nil {
			shutdownErr = fmt.Errorf("snapshot scheduler failed: %w", snapshotErr)
		}
	}
	cancelWorkers()

	shutdownContext, cancel := context.WithTimeout(
		context.Background(),
		shutdownTimeout,
	)
	defer cancel()

	if err := httpServer.Shutdown(shutdownContext); err != nil {
		return errors.Join(shutdownErr, fmt.Errorf("graceful shutdown failed: %w", err))
	}
	if snapshotDone != nil {
		shutdownErr = errors.Join(shutdownErr, <-snapshotDone)
		snapshotDone = nil
	}
	if shutdownErr != nil {
		return shutdownErr
	}

	log.Println("server stopped gracefully")
	return nil
}
