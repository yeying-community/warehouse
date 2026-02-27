#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROJECT_NAME="$(basename "${ROOT_DIR}")"
OUTPUT_DIR="${ROOT_DIR}/output"
ASSETS_DIR="${ROOT_DIR}/web/dist"
REQUESTED_TAG="${1:-}"
TAG_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+$'
MAIN_BRANCH="main"
ORIGINAL_REF="$(git -C "${ROOT_DIR}" symbolic-ref --quiet --short HEAD || git -C "${ROOT_DIR}" rev-parse --verify HEAD)"
SWITCHED_REF=0

cleanup() {
  if [[ "${SWITCHED_REF}" -eq 1 ]]; then
    echo "Restoring checkout: ${ORIGINAL_REF}"
    git -C "${ROOT_DIR}" checkout -q "${ORIGINAL_REF}"
  fi
}
trap cleanup EXIT

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "$1 is required" >&2
    exit 1
  fi
}

validate_tag() {
  local tag="$1"
  if [[ ! "${tag}" =~ ${TAG_PATTERN} ]]; then
    echo "Invalid tag format: ${tag}. Expected v<major>.<minor>.<patch>" >&2
    exit 1
  fi
}

ensure_clean_worktree_for_checkout() {
  local target="$1"
  local current
  current="$(git -C "${ROOT_DIR}" rev-parse --verify HEAD)"
  local target_commit
  target_commit="$(git -C "${ROOT_DIR}" rev-list -n 1 "${target}")"
  if [[ "${current}" == "${target_commit}" ]]; then
    return
  fi
  if ! git -C "${ROOT_DIR}" diff --quiet || ! git -C "${ROOT_DIR}" diff --cached --quiet; then
    echo "Working tree is not clean. Commit/stash changes before switching to ${target}." >&2
    exit 1
  fi
}

latest_semver_tag() {
  git -C "${ROOT_DIR}" tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -n1
}

next_patch_tag() {
  local latest="$1"
  if [[ -z "${latest}" ]]; then
    echo "v0.0.1"
    return
  fi
  local version="${latest#v}"
  local major minor patch
  IFS='.' read -r major minor patch <<<"${version}"
  echo "v${major}.${minor}.$((patch + 1))"
}

prepare_target_tag() {
  local tag

  git -C "${ROOT_DIR}" fetch --tags --quiet

  if [[ -n "${REQUESTED_TAG}" ]]; then
    validate_tag "${REQUESTED_TAG}"
    if ! git -C "${ROOT_DIR}" rev-parse -q --verify "refs/tags/${REQUESTED_TAG}" >/dev/null; then
      echo "Tag does not exist, skip packaging: ${REQUESTED_TAG}"
      exit 0
    fi
    echo "${REQUESTED_TAG}"
    return
  fi

  if ! git -C "${ROOT_DIR}" rev-parse -q --verify "refs/heads/${MAIN_BRANCH}" >/dev/null; then
    echo "Branch not found: ${MAIN_BRANCH}" >&2
    exit 1
  fi

  local latest_tag
  latest_tag="$(latest_semver_tag)"
  local main_hash
  main_hash="$(git -C "${ROOT_DIR}" rev-parse --verify "${MAIN_BRANCH}")"

  if [[ -n "${latest_tag}" ]]; then
    local latest_hash
    latest_hash="$(git -C "${ROOT_DIR}" rev-list -n 1 "${latest_tag}")"
    if [[ "${latest_hash}" == "${main_hash}" ]]; then
      echo "Latest tag ${latest_tag} already points to ${MAIN_BRANCH} HEAD, skip packaging."
      exit 0
    fi
  fi

  tag="$(next_patch_tag "${latest_tag}")"
  validate_tag "${tag}"
  echo "Creating tag ${tag} on ${MAIN_BRANCH}@${main_hash}"
  git -C "${ROOT_DIR}" tag "${tag}" "${main_hash}"
  echo "Pushing tag ${tag} to origin"
  git -C "${ROOT_DIR}" push origin "${tag}"
  echo "${tag}"
}

switch_to_tag() {
  local tag="$1"
  ensure_clean_worktree_for_checkout "${tag}"
  local current
  current="$(git -C "${ROOT_DIR}" rev-parse --verify HEAD)"
  local target
  target="$(git -C "${ROOT_DIR}" rev-list -n 1 "${tag}")"
  if [[ "${current}" != "${target}" ]]; then
    git -C "${ROOT_DIR}" checkout -q "${tag}"
    SWITCHED_REF=1
  fi
}

build_artifacts() {
  require_cmd npm
  echo "Building frontend assets..."
  if [[ ! -d "${ROOT_DIR}/web/node_modules" ]]; then
    (cd "${ROOT_DIR}/web" && npm install)
  fi
  (cd "${ROOT_DIR}/web" && npm run build)

  echo "Building backend binary..."
  (cd "${ROOT_DIR}" && make build)

  if [[ ! -x "${ROOT_DIR}/build/webdav" ]]; then
    echo "webdav binary not found: ${ROOT_DIR}/build/webdav" >&2
    exit 1
  fi
  if [[ ! -d "${ASSETS_DIR}" ]]; then
    echo "frontend assets not found: ${ASSETS_DIR}" >&2
    exit 1
  fi
  if [[ ! -f "${ROOT_DIR}/scripts/starter.sh" ]]; then
    echo "starter script not found: ${ROOT_DIR}/scripts/starter.sh" >&2
    exit 1
  fi
}

create_package() {
  local tag="$1"
  local git_hash
  git_hash="$(git -C "${ROOT_DIR}" rev-parse --short=7 HEAD)"
  local package_name="${PROJECT_NAME}-${tag}-${git_hash}"
  local staging_dir="${OUTPUT_DIR}/${package_name}"

  echo "Packaging ${package_name}.tar.gz"
  rm -rf "${staging_dir}"
  mkdir -p "${staging_dir}/bin" "${staging_dir}/scripts" "${staging_dir}/web"

  cp "${ROOT_DIR}/build/webdav" "${staging_dir}/bin/"
  cp "${ROOT_DIR}/config.yaml.template" "${staging_dir}/"
  cp "${ROOT_DIR}/scripts/starter.sh" "${staging_dir}/scripts/"
  if [[ -d "${ROOT_DIR}/resources" ]]; then
    cp -R "${ROOT_DIR}/resources" "${staging_dir}/"
  fi
  cp -R "${ASSETS_DIR}" "${staging_dir}/web/"

  mkdir -p "${OUTPUT_DIR}"
  tar -C "${OUTPUT_DIR}" -czf "${OUTPUT_DIR}/${package_name}.tar.gz" "${package_name}"
  rm -rf "${staging_dir}"
  echo "Package created: ${OUTPUT_DIR}/${package_name}.tar.gz"
}

main() {
  local tag
  tag="$(prepare_target_tag)"
  switch_to_tag "${tag}"
  build_artifacts
  create_package "${tag}"
}

main
