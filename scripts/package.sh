#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROJECT_NAME="$(basename "${ROOT_DIR}")"
TIMESTAMP="$(date '+%Y%m%d-%H%M%S')"
GIT_HASH="$(git -C "${ROOT_DIR}" rev-parse --short=7 HEAD 2>/dev/null || echo "unknown")"
OUTPUT_DIR="${ROOT_DIR}/output"
PACKAGE_NAME="${PROJECT_NAME}-${TIMESTAMP}-${GIT_HASH}"
STAGING_DIR="${OUTPUT_DIR}/${PACKAGE_NAME}"

ASSETS_DIR="${ROOT_DIR}/web/dist"

echo "Packaging ${PACKAGE_NAME}..."

if ! command -v npm >/dev/null 2>&1; then
  echo "npm is required to build frontend assets" >&2
  exit 1
fi

echo "Building frontend assets..."
if [[ ! -d "${ROOT_DIR}/web/node_modules" ]]; then
  (cd "${ROOT_DIR}/web" && npm install)
fi
(cd "${ROOT_DIR}/web" && npm run build)

if [[ ! -x "${ROOT_DIR}/build/webdav" ]]; then
  echo "Building backend binary..."
  (cd "${ROOT_DIR}" && make build)
fi

if [[ ! -x "${ROOT_DIR}/build/webdav" ]]; then
  echo "webdav binary not found: ${ROOT_DIR}/build/webdav" >&2
  exit 1
fi

if [[ ! -d "${ASSETS_DIR}" ]]; then
  echo "frontend assets not found: ${ASSETS_DIR}" >&2
  echo "run: cd web && npm install && npm run build" >&2
  exit 1
fi

rm -rf "${STAGING_DIR}"
mkdir -p "${STAGING_DIR}/bin" "${STAGING_DIR}/scripts" "${STAGING_DIR}/web"

cp "${ROOT_DIR}/build/webdav" "${STAGING_DIR}/bin/"
cp "${ROOT_DIR}/config.yaml.template" "${STAGING_DIR}/"
cp "${ROOT_DIR}/scripts/starter.sh" "${STAGING_DIR}/scripts/"
cp -R "${ASSETS_DIR}" "${STAGING_DIR}/web/"

mkdir -p "${OUTPUT_DIR}"
tar -C "${OUTPUT_DIR}" -czf "${OUTPUT_DIR}/${PACKAGE_NAME}.tar.gz" "${PACKAGE_NAME}"
rm -rf "${STAGING_DIR}"

echo "Package created: ${OUTPUT_DIR}/${PACKAGE_NAME}.tar.gz"
