#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${ROOT_DIR}/build"
INTEL_IOS="${BUILD_DIR}/ios-x86_64"
ARM_IOS="${BUILD_DIR}/ios-arm64"
INTEL_DYLIB="${BUILD_DIR}/libgoios-x86_64.dylib"
ARM_DYLIB="${BUILD_DIR}/libgoios-arm64.dylib"

"${ROOT_DIR}/scripts/build_intel.sh"
"${ROOT_DIR}/scripts/build_arm.sh"

echo "==> Merging ios CLI slices into a universal binary…"
lipo -create "${INTEL_IOS}" "${ARM_IOS}" -output "${BUILD_DIR}/ios"

echo "==> Merging libgoios.dylib slices into a universal binary…"
lipo -create "${INTEL_DYLIB}" "${ARM_DYLIB}" -output "${BUILD_DIR}/libgoios.dylib"
install_name_tool -id @rpath/libgoios.dylib "${BUILD_DIR}/libgoios.dylib"

echo "Universal artifacts written to ${BUILD_DIR}"
