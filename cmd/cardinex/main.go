package main

import (
	"log"
	"net/http"
	"os"

	"github.com/dippendicular/cardinex/internal/service"
)

func main() {
	addr := getenv("ADDR", ":8080")
	redisAddr := getenv("REDIS_ADDR", "localhost:6379")
	srv, err := service.New(redisAddr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("cardinex listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, srv.Routes()))
}

func getenv(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
