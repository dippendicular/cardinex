#!/usr/bin/env bash
# Installs Go inside cibuildwheel's manylinux/musllinux containers.
set -euo pipefail
v=1.24.4
arch=$(uname -m); [ "$arch" = x86_64 ] && arch=amd64; [ "$arch" = aarch64 ] && arch=arm64
curl -fsSL "https://go.dev/dl/go${v}.linux-${arch}.tar.gz" | tar -C /usr/local -xz
ln -sf /usr/local/go/bin/go /usr/local/bin/go
