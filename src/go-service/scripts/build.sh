#!/usr/bin/env bash
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
target_os="${1:-linux}"
target_arch="${2:-amd64}"
output="${3:-$root/bin/$target_os-$target_arch/comic-server}"
if [[ "$target_os" == windows && "$output" != *.exe ]]; then output="$output.exe"; fi
mkdir -p -- "$(dirname -- "$output")"
cd -- "$root"
CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -ldflags='-s -w' -o "$output" ./cmd/server
printf 'Built %s\n' "$output"
