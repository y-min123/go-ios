#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${ROOT_DIR}/build"
SOURCE_IOS="${1:-}"
GO_BIN="${GO_BIN:-}"

ORIGINAL_INTEL="${BUILD_DIR}/ios-x86_64-macos12-original"
PRESERVED_ARM="${BUILD_DIR}/ios-arm64-macos11"
CATALINA_INTEL="${BUILD_DIR}/ios-x86_64-macos10.15"
UNIVERSAL_OUTPUT="${BUILD_DIR}/ios"

fail() {
  echo "error: $*" >&2
  exit 1
}

read_minos() {
  otool -l "$1" | awk '
    $1 == "cmd" && $2 == "LC_BUILD_VERSION" { in_build_version = 1; next }
    in_build_version && $1 == "minos" { print $2; exit }
  '
}

require_architecture() {
  local binary="$1"
  local architecture="$2"
  local architectures
  architectures="$(lipo -archs "${binary}")"

  [[ " ${architectures} " == *" ${architecture} "* ]] || \
    fail "${binary} does not contain ${architecture}; architectures: ${architectures}"
}

[[ -n "${SOURCE_IOS}" ]] || \
  fail "usage: GO_BIN=/path/to/go1.22.5/bin/go $0 /path/to/current/universal/ios"
[[ -f "${SOURCE_IOS}" ]] || fail "source ios binary not found: ${SOURCE_IOS}"
[[ -n "${GO_BIN}" ]] || fail "GO_BIN must point to the Go 1.22.5 executable"
[[ -x "${GO_BIN}" ]] || fail "GO_BIN is not executable: ${GO_BIN}"

GO_VERSION="$("${GO_BIN}" version)"
[[ "${GO_VERSION}" == go\ version\ go1.22.5\ * ]] || \
  fail "Go 1.22.5 is required; got: ${GO_VERSION}"

require_architecture "${SOURCE_IOS}" x86_64
require_architecture "${SOURCE_IOS}" arm64

mkdir -p "${BUILD_DIR}"
UNIVERSAL_TEMP="$(mktemp "${BUILD_DIR}/.ios-catalina-universal.XXXXXX")"
trap 'rm -f "${UNIVERSAL_TEMP}"' EXIT

echo "==> Splitting the current universal ios binary…"
lipo "${SOURCE_IOS}" -thin x86_64 -output "${ORIGINAL_INTEL}"
lipo "${SOURCE_IOS}" -thin arm64 -output "${PRESERVED_ARM}"

ARM_MINOS="$(read_minos "${PRESERVED_ARM}")"
[[ "${ARM_MINOS}" == "11.0" ]] || \
  fail "preserved arm64 slice must target macOS 11.0; got: ${ARM_MINOS:-unknown}"

echo "==> Building the Catalina-compatible x86_64 slice with ${GO_VERSION}…"
PUBLISH_SINGLE_ARCH_OUTPUT=0 GO_BIN="${GO_BIN}" "${ROOT_DIR}/scripts/build_intel.sh"
cp "${BUILD_DIR}/ios-x86_64" "${CATALINA_INTEL}"

INTEL_MINOS="$(read_minos "${CATALINA_INTEL}")"
[[ "${INTEL_MINOS}" == "10.15" ]] || \
  fail "new x86_64 slice must target macOS 10.15; got: ${INTEL_MINOS:-unknown}"

if nm -u "${CATALINA_INTEL}" | grep -q '_SecTrustCopyCertificateChain'; then
  fail "new x86_64 slice still references macOS 12-only SecTrustCopyCertificateChain"
fi

BUILT_GO_VERSION="$("${GO_BIN}" version -m "${CATALINA_INTEL}" | awk 'NR == 1 { print $2 }')"
[[ "${BUILT_GO_VERSION}" == "go1.22.5" ]] || \
  fail "new x86_64 slice was not built with Go 1.22.5; got: ${BUILT_GO_VERSION:-unknown}"

echo "==> Merging the Catalina x86_64 slice with the preserved arm64 slice…"
lipo -create "${CATALINA_INTEL}" "${PRESERVED_ARM}" -output "${UNIVERSAL_TEMP}"
require_architecture "${UNIVERSAL_TEMP}" x86_64
require_architecture "${UNIVERSAL_TEMP}" arm64
mv "${UNIVERSAL_TEMP}" "${UNIVERSAL_OUTPUT}"

echo "Universal ios binary written to ${UNIVERSAL_OUTPUT}"
echo "  original x86_64: ${ORIGINAL_INTEL}"
echo "  Catalina x86_64: ${CATALINA_INTEL} (macOS ${INTEL_MINOS}, ${BUILT_GO_VERSION})"
echo "  preserved arm64: ${PRESERVED_ARM} (macOS ${ARM_MINOS})"
lipo -info "${UNIVERSAL_OUTPUT}"
