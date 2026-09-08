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
IOS_OUTPUT="${BUILD_DIR}/ios-arm64"
DYLIB_OUTPUT="${BUILD_DIR}/libgoios-arm64.dylib"
PUBLISH_SINGLE_ARCH_OUTPUT="${PUBLISH_SINGLE_ARCH_OUTPUT:-1}"

mkdir -p "${BUILD_DIR}" "${GOMODCACHE}" "${GOCACHE}"

export MACOSX_DEPLOYMENT_TARGET=11.0
export CGO_ENABLED=1
export GOOS=darwin
export GOARCH=arm64
export GOMODCACHE
export GOCACHE
# Build the root module standalone, ignoring the repo's go.work: its other
# members (ncm/, restapi/) require a newer Go than build_intel.sh's pinned
# Go 1.22.5 supports, and mixing workspace/non-workspace builds across
# build_universal.sh's two slices would be inconsistent.
export GOWORK=off
# go.mod 的 `toolchain` 只是下限，不加这行的话 GO_BIN 会被静默提升到更新的 Go，
# 而新版 Go 会让产物引用 macOS 12 才有的 SecTrustCopyCertificateChain
export GOTOOLCHAIN=local

echo "==> Building libgoios.dylib (arm64, min macOS 11)…"
"${GO_BIN}" build -buildmode=c-shared -o "${DYLIB_OUTPUT}" ./bridge
install_name_tool -id @rpath/libgoios.dylib "${DYLIB_OUTPUT}"
if [[ "${PUBLISH_SINGLE_ARCH_OUTPUT}" == "1" ]]; then
  cp "${DYLIB_OUTPUT}" "${BUILD_DIR}/libgoios.dylib"
fi

echo "==> Building ios CLI (arm64)…"
# Force external linking so the Mach-O build version comes from the host linker,
# which honors MACOSX_DEPLOYMENT_TARGET=11.0 on darwin/arm64.
"${GO_BIN}" build -ldflags="-linkmode=external" -o "${IOS_OUTPUT}" .
if [[ "${PUBLISH_SINGLE_ARCH_OUTPUT}" == "1" ]]; then
  cp "${IOS_OUTPUT}" "${BUILD_DIR}/ios"
fi

echo "Artifacts written to ${BUILD_DIR}"
