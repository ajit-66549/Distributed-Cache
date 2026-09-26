package main

import (
	"context"
	"errors"
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
	cfg := config.Load()

	cacheStore := cache.NewStore()
	handler := server.NewHandler(cacheStore)

	address := ":" + cfg.Port
	httpServer := server.NewHTTPServer(address, handler.Routes())

	go func() {
		log.Printf("distributed cache server listening on %s", address)

		if err := httpServer.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	}()

	shutdownSignal, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	cache.StartExpirationCleanup(
		shutdownSignal,
		cacheStore,
		time.Minute,
	)

	<-shutdownSignal.Done()

	log.Println("shutdown signal received")

	shutdownContext, cancel := context.WithTimeout(
		context.Background(),
		shutdownTimeout,
	)
	defer cancel()

	if err := httpServer.Shutdown(shutdownContext); err != nil {
		log.Fatalf("graceful shutdown failed: %v", err)
	}

	log.Println("server stopped gracefully")
}
