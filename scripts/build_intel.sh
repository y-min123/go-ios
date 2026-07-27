#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO_BIN="${GO_BIN:-$(command -v go || true)}"

if [[ -z "${GO_BIN}" ]]; then
  echo "go toolchain not found. Install Go 1.22+ or set GO_BIN." >&2
  exit 1
fi

BUILD_DIR="${ROOT_DIR}/build"
CACHE_DIR="${ROOT_DIR}/.cache"
GOMODCACHE="${CACHE_DIR}/gomod"
GOCACHE="${CACHE_DIR}/gocache"
IOS_OUTPUT="${BUILD_DIR}/ios-x86_64"
DYLIB_OUTPUT="${BUILD_DIR}/libgoios-x86_64.dylib"
PUBLISH_SINGLE_ARCH_OUTPUT="${PUBLISH_SINGLE_ARCH_OUTPUT:-1}"

mkdir -p "${BUILD_DIR}" "${GOMODCACHE}" "${GOCACHE}"

export MACOSX_DEPLOYMENT_TARGET=10.15
export CGO_ENABLED=1
export GOOS=darwin
export GOARCH=amd64
export GOMODCACHE
export GOCACHE

echo "==> Building libgoios.dylib (x86_64, min macOS 10.15)…"
"${GO_BIN}" build -buildmode=c-shared -o "${DYLIB_OUTPUT}" ./bridge
install_name_tool -id @rpath/libgoios.dylib "${DYLIB_OUTPUT}"
if [[ "${PUBLISH_SINGLE_ARCH_OUTPUT}" == "1" ]]; then
  cp "${DYLIB_OUTPUT}" "${BUILD_DIR}/libgoios.dylib"
fi

echo "==> Building ios CLI (x86_64)…"
# Force external linking so the Mach-O build version comes from the host linker,
# which honors MACOSX_DEPLOYMENT_TARGET=10.15 on darwin/amd64.
"${GO_BIN}" build -ldflags="-linkmode=external" -o "${IOS_OUTPUT}" ./main.go
if [[ "${PUBLISH_SINGLE_ARCH_OUTPUT}" == "1" ]]; then
  cp "${IOS_OUTPUT}" "${BUILD_DIR}/ios"
fi

echo "Artifacts written to ${BUILD_DIR}"
