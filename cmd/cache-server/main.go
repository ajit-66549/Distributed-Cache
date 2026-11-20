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
	"github.com/ajit-66549/distributed-cache/internal/metrics"
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

	measurements := metrics.New()
	cacheStore, writer, err := openStore(cfg, cache.WithMetrics(measurements))
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, writer.Close())
	}()
	shutdownSignal, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	readiness := server.NewReadiness(shutdownSignal, writer.Writable)
	defer readiness.BeginShutdown()
	handler := server.NewHandler(cacheStore, server.WithMonitoring(measurements.Gatherer(), readiness))

	address := ":" + cfg.Port
	routes := server.InstrumentHTTP(handler.Routes(), measurements)
	httpServer := server.NewHTTPServer(address, routes)

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

	cache.StartExpirationCleanup(
		workerContext,
		cacheStore,
		time.Minute,
	)
	readiness.MarkReady()

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("distributed cache server listening on %s", address)
		serverErrors <- httpServer.ListenAndServe()
	}()

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
	readiness.BeginShutdown()
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
