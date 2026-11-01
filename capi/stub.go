//go:build !cgo

// Without cgo there is no C ABI; this stub keeps `go build ./...` working.
package main

func main() {}
