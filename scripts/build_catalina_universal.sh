#!/usr/bin/env bash

# 从源码构建 x86_64 + arm64 两个分片并合并成 universal 二进制，
# 同时校验部署目标与符号，确保产物能在 macOS 10.15 起的系统上运行。
#
# 与 build_universal.sh 的区别：这个脚本会强制校验
#   - x86_64 分片 minos = 10.15，arm64 分片 minos = 11.0
#   - 两个分片都不引用 macOS 12 才有的符号
#   - 两个分片都由 Go 1.22.5 构建
# 校验不通过直接失败，避免产出一个在低版本系统上起不来的二进制。

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${ROOT_DIR}/build"
GO_BIN="${GO_BIN:-}"

REQUIRED_GO_VERSION="go1.22.5"
INTEL_MIN_MACOS="10.15"
ARM_MIN_MACOS="11.0"

CATALINA_INTEL="${BUILD_DIR}/ios-x86_64-macos10.15"
BIG_SUR_ARM="${BUILD_DIR}/ios-arm64-macos11"
UNIVERSAL_IOS="${BUILD_DIR}/ios"
UNIVERSAL_DYLIB="${BUILD_DIR}/libgoios.dylib"

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

require_minos() {
  local binary="$1"
  local expected="$2"
  local actual
  actual="$(read_minos "${binary}")"

  [[ "${actual}" == "${expected}" ]] || \
    fail "${binary} must target macOS ${expected}; got: ${actual:-unknown}"
}

# SecTrustCopyCertificateChain 是 macOS 12 才引入的，低版本系统上会因缺符号无法启动
require_no_macos12_symbols() {
  local binary="$1"

  if nm -u "${binary}" | grep -q '_SecTrustCopyCertificateChain'; then
    fail "${binary} still references macOS 12-only SecTrustCopyCertificateChain"
  fi
}

# go.work 里写了 toolchain go1.26.4，不关掉的话仓库内连 `go version` 都会静默切到新版本。
# GOTOOLCHAIN=local 保证执行的就是传入的那个 go 本身。
pinned_go() {
  GOWORK=off GOTOOLCHAIN=local "$@"
}

require_go_version() {
  local binary="$1"
  local built

  built="$(pinned_go "${GO_BIN}" version -m "${binary}" | awk 'NR == 1 { print $2 }')"
  [[ "${built}" == "${REQUIRED_GO_VERSION}" ]] || \
    fail "${binary} was not built with ${REQUIRED_GO_VERSION}; got: ${built:-unknown}"
}

require_architecture() {
  local binary="$1"
  local architecture="$2"
  local architectures
  architectures="$(lipo -archs "${binary}")"

  [[ " ${architectures} " == *" ${architecture} "* ]] || \
    fail "${binary} does not contain ${architecture}; architectures: ${architectures}"
}

# 未显式指定 GO_BIN 时，只在 golang.org/dl 的固定安装位置查找钉住的工具链。
# 绝不回落到 PATH 上的 go：新版本 Go 会让分片引用 macOS 12 才有的符号，
# 而 minos 和 lipo -info 都看不出这个问题。
if [[ -z "${GO_BIN}" ]]; then
  DL_WRAPPER="$(go env GOPATH 2>/dev/null || echo "${HOME}/go")/bin/${REQUIRED_GO_VERSION}"
  SDK_GO="${HOME}/sdk/${REQUIRED_GO_VERSION}/bin/go"

  if [[ -x "${DL_WRAPPER}" ]]; then
    GO_BIN="$(pinned_go "${DL_WRAPPER}" env GOROOT)/bin/go"
  elif [[ -x "${SDK_GO}" ]]; then
    GO_BIN="${SDK_GO}"
  fi
fi

[[ -n "${GO_BIN}" ]] || fail "$(cat <<EOF
${REQUIRED_GO_VERSION} not found. Install it once with:
  go install golang.org/dl/${REQUIRED_GO_VERSION}@latest
  ${REQUIRED_GO_VERSION} download
Then re-run $0, or point GO_BIN at the ${REQUIRED_GO_VERSION} executable.
EOF
)"
[[ -x "${GO_BIN}" ]] || fail "GO_BIN is not executable: ${GO_BIN}"

GO_VERSION="$(pinned_go "${GO_BIN}" version)"
[[ "${GO_VERSION}" == go\ version\ ${REQUIRED_GO_VERSION}\ * ]] || \
  fail "${REQUIRED_GO_VERSION} is required; got: ${GO_VERSION}"

mkdir -p "${BUILD_DIR}"
IOS_TEMP="$(mktemp "${BUILD_DIR}/.ios-universal.XXXXXX")"
DYLIB_TEMP="$(mktemp "${BUILD_DIR}/.libgoios-universal.XXXXXX")"
trap 'rm -f "${IOS_TEMP}" "${DYLIB_TEMP}"' EXIT

# 两个分片都用 PUBLISH_SINGLE_ARCH_OUTPUT=0 构建，
# 避免在合并成功之前就把单架构产物写进 build/ios 和 build/libgoios.dylib
echo "==> Building the x86_64 slice (min macOS ${INTEL_MIN_MACOS}) with ${GO_VERSION}…"
PUBLISH_SINGLE_ARCH_OUTPUT=0 GO_BIN="${GO_BIN}" "${ROOT_DIR}/scripts/build_intel.sh"
cp "${BUILD_DIR}/ios-x86_64" "${CATALINA_INTEL}"
require_minos "${CATALINA_INTEL}" "${INTEL_MIN_MACOS}"
require_no_macos12_symbols "${CATALINA_INTEL}"
require_go_version "${CATALINA_INTEL}"

echo "==> Building the arm64 slice (min macOS ${ARM_MIN_MACOS}) with ${GO_VERSION}…"
PUBLISH_SINGLE_ARCH_OUTPUT=0 GO_BIN="${GO_BIN}" "${ROOT_DIR}/scripts/build_arm.sh"
cp "${BUILD_DIR}/ios-arm64" "${BIG_SUR_ARM}"
require_minos "${BIG_SUR_ARM}" "${ARM_MIN_MACOS}"
require_no_macos12_symbols "${BIG_SUR_ARM}"
require_go_version "${BIG_SUR_ARM}"

echo "==> Merging ios CLI slices into a universal binary…"
lipo -create "${CATALINA_INTEL}" "${BIG_SUR_ARM}" -output "${IOS_TEMP}"
require_architecture "${IOS_TEMP}" x86_64
require_architecture "${IOS_TEMP}" arm64
mv "${IOS_TEMP}" "${UNIVERSAL_IOS}"

echo "==> Merging libgoios.dylib slices into a universal binary…"
lipo -create "${BUILD_DIR}/libgoios-x86_64.dylib" "${BUILD_DIR}/libgoios-arm64.dylib" -output "${DYLIB_TEMP}"
require_architecture "${DYLIB_TEMP}" x86_64
require_architecture "${DYLIB_TEMP}" arm64
mv "${DYLIB_TEMP}" "${UNIVERSAL_DYLIB}"
install_name_tool -id @rpath/libgoios.dylib "${UNIVERSAL_DYLIB}"

echo "Universal artifacts written to ${BUILD_DIR}"
echo "  ios:            ${UNIVERSAL_IOS}"
echo "    x86_64:       ${CATALINA_INTEL} (macOS $(read_minos "${CATALINA_INTEL}"), ${REQUIRED_GO_VERSION})"
echo "    arm64:        ${BIG_SUR_ARM} (macOS $(read_minos "${BIG_SUR_ARM}"), ${REQUIRED_GO_VERSION})"
echo "  libgoios.dylib: ${UNIVERSAL_DYLIB}"
lipo -info "${UNIVERSAL_IOS}"
lipo -info "${UNIVERSAL_DYLIB}"
