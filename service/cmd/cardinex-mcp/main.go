// Command cardinex-mcp serves the Cardinex MCP tools over stdio.
// It uses the same environment as cardinex (REDIS_ADDR, FLUSH_INTERVAL,
// LOG_WRITES), so an agent and the HTTP API can share sketches through Redis.
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

const version = "0.1.0"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil)) // stdout carries the protocol
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	reg := service.NewRegistryFromEnv(log)
	if _, _, err := reg.Load(ctx); err != nil {
		log.Error("load", "err", err)
		os.Exit(1)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { reg.Run(runCtx, 5*time.Second); close(done) }()

	if err := service.NewMCP(reg, version).Serve(ctx, os.Stdin, os.Stdout); err != nil {
		log.Error("mcp", "err", err)
	}
	cancel()
	<-done
}
