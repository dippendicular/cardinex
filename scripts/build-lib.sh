#!/usr/bin/env bash
# Builds libcardinex for the host platform into $1 (default: dist/).
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
out="${1:-$root/dist}"
mkdir -p "$out"
case "$(uname -s)" in
  Darwin) name=libcardinex.dylib ;;
  MINGW*|MSYS*|CYGWIN*) name=cardinex.dll ;;
  *) name=libcardinex.so ;;
esac
cd "$root"
CGO_ENABLED=1 go build -trimpath -buildmode=c-shared -o "$out/$name" ./capi
# Ship the stable header, not the cgo-generated one.
cp capi/cardinex.h capi/cdnx_types.h "$out/"
echo "built $out/$name"
