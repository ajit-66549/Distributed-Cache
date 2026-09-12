package config

import "os"

type Config struct {
	Port string
}

func Load() Config {
	port := os.Getenv("CACHE_PORT")

	if port == "" {
		port = "8080"
	}

	return Config{
		Port: port,
	}
}