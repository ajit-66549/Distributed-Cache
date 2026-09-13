package main

import (
	"fmt"

	"github.com/ajit-66549/distributed-cache/internal/config"
)

func main() {
	cfg := config.Load()
	fmt.Printf("Distributed cache server is starting...%s\n", cfg.Port)
}
