// Command cardinex serves the HTTP API.
//
// Environment:
//
//	ADDR             listen address (default :8080)
//	REDIS_ADDR       Redis address; empty keeps state in memory only
//	FLUSH_INTERVAL   snapshot interval, the loss window without LOG_WRITES (default 5s)
//	LOG_WRITES       "1" appends every write to the Redis log before acknowledging (default 1)
//	CARDINEX_API_KEY optional bearer token
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dippendicular/cardinex/service"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	reg := service.NewRegistryFromEnv(log)
	loaded, corrupt, err := reg.Load(ctx)
	if err != nil {
		log.Error("load", "err", err)
		os.Exit(1)
	}
	log.Info("loaded sketches", "loaded", loaded, "corrupt", corrupt)

	interval := 5 * time.Second
	if v, err := time.ParseDuration(os.Getenv("FLUSH_INTERVAL")); err == nil && v > 0 {
		interval = v
	}
	done := make(chan struct{})
	go func() { reg.Run(ctx, interval); close(done) }()

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Info("cardinex listening", "addr", addr)
	srv := service.NewServer(reg, os.Getenv("CARDINEX_API_KEY"), log)
	if err := service.ListenAndServe(ctx, addr, srv.Routes()); err != nil {
		log.Error("serve", "err", err)
	}
	stop()
	<-done
}
