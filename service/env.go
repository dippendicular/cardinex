package service

import (
	"log/slog"
	"os"
)

// NewRegistryFromEnv builds a registry from REDIS_ADDR and LOG_WRITES.
func NewRegistryFromEnv(log *slog.Logger) *Registry {
	var store Store = NewMemoryStore()
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		store = NewRedisStore(addr)
	}
	return NewRegistry(store, os.Getenv("LOG_WRITES") != "0", log)
}
