package main

import (
	"errors"
	"log"
	"net/http"


	"github.com/ajit-66549/distributed-cache/internal/config"
	"github.com/ajit-66549/distributed-cache/internal/cache"
	"github.com/ajit-66549/distributed-cache/internal/server"
)

func main() {
	cfg := config.Load()
	
	store := cache.NewStore()
	handler := server.NewHandler(store)

	address := ":" + cfg.Port

	httpServer := server.NewHTTPServer(address, handler.Routes())

	log.Printf("distributed cache server listening on %s", address)

	if err := httpServer.ListenAndServe(); err != nil &&
		!errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
	}
}
