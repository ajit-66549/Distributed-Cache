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
	defer func() {
		err = errors.Join(err, httpServer.Close())
	}()

	shutdownSignal, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("distributed cache server listening on %s", address)
		serverErrors <- httpServer.ListenAndServe()
	}()

	cache.StartExpirationCleanup(
		shutdownSignal,
		cacheStore,
		time.Minute,
	)

	select {
	case <-shutdownSignal.Done():
		log.Println("shutdown signal received")
	case serveErr := <-serverErrors:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			return fmt.Errorf("server failed: %w", serveErr)
		}
	}

	shutdownContext, cancel := context.WithTimeout(
		context.Background(),
		shutdownTimeout,
	)
	defer cancel()

	if err := httpServer.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	log.Println("server stopped gracefully")
	return nil
}
