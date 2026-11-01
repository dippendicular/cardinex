#!/usr/bin/env bash
# Cross-compiles libcardinex with zig cc into dist/<os>-<arch>[-musl]/.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
build() { # goos goarch zig-target file suffix
  mkdir -p "dist/$1-$2$5"
  CGO_ENABLED=1 GOOS=$1 GOARCH=$2 CC="zig cc -target $3" \
    go build -trimpath -buildmode=c-shared -o "dist/$1-$2$5/$4" ./capi
  cp capi/cardinex.h capi/cdnx_types.h "dist/$1-$2$5/"
}
build linux amd64 x86_64-linux-gnu libcardinex.so ""
build linux arm64 aarch64-linux-gnu libcardinex.so ""
build linux amd64 x86_64-linux-musl libcardinex.so "-musl"
build linux arm64 aarch64-linux-musl libcardinex.so "-musl"
build windows amd64 x86_64-windows-gnu cardinex.dll ""
# darwin needs the macOS SDK; it is built natively on a macOS runner (see CI).
