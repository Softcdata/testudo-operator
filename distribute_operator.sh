#!/usr/bin/env bash
set -euo pipefail

VERSION="${VERSION:-v1.0.0}"
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"
BUILDER="${BUILDER:-default_builder}"
NO_CACHE=1

DOCKERHUB_REPO="${DOCKERHUB_REPO:-docker.io/softcdata/testudo-operator}"
ALIYUN_REPO="${ALIYUN_REPO:-crpi-ftcmc8yukyvoj8qy.cn-hangzhou.personal.cr.aliyuncs.com/softcdata/testudo-operator}"

usage() {
  cat <<'USAGE'
Usage:
  ./distribute_operator.sh [options]

Options:
  -v, --version <tag>     Image version tag. Default: v1.0.0
      --platforms <list>  Buildx platforms. Default: linux/amd64,linux/arm64
      --builder <name>    Buildx builder name. Default: default_builder
      --cache             Allow Docker build cache. Default: no-cache
  -h, --help              Show this help.

Environment overrides:
  VERSION, PLATFORMS, BUILDER, DOCKERHUB_REPO, ALIYUN_REPO, GO_IMAGE, RUNTIME_IMAGE

Notes:
  This script does not store registry credentials. Run docker login for DockerHub
  and Aliyun ACR before executing it if the local Docker client is not logged in.
USAGE
}

log() {
  printf '\n[%s] %s\n' "$(date '+%H:%M:%S')" "$*"
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Error: required command not found: $1" >&2
    exit 1
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -v|--version)
      VERSION="${2:?missing version}"
      shift 2
      ;;
    --platforms)
      PLATFORMS="${2:?missing platforms}"
      shift 2
      ;;
    --builder)
      BUILDER="${2:?missing builder name}"
      shift 2
      ;;
    --cache)
      NO_CACHE=0
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Error: unknown option: $1" >&2
      usage
      exit 1
      ;;
  esac
done

DOCKERHUB_IMAGE="${DOCKERHUB_REPO}:${VERSION}"
ALIYUN_IMAGE="${ALIYUN_REPO}:${VERSION}"

require_command docker

log "Release version: ${VERSION}"
echo "DockerHub image: ${DOCKERHUB_IMAGE}"
echo "Aliyun image:    ${ALIYUN_IMAGE}"
echo "Platforms:       ${PLATFORMS}"
echo "Buildx builder:  ${BUILDER}"

if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  log "Git state"
  git status --short --branch
fi

log "Preparing buildx builder"
if docker buildx inspect "${BUILDER}" >/dev/null 2>&1; then
  docker buildx use "${BUILDER}"
else
  if [[ -f buildkitd.toml ]]; then
    docker buildx create --name "${BUILDER}" --use --config buildkitd.toml
  else
    docker buildx create --name "${BUILDER}" --use
  fi
fi
docker buildx inspect --bootstrap

BUILD_ARGS=()
if [[ -n "${GO_IMAGE:-}" ]]; then
  BUILD_ARGS+=(--build-arg "GO_IMAGE=${GO_IMAGE}")
fi
if [[ -n "${RUNTIME_IMAGE:-}" ]]; then
  BUILD_ARGS+=(--build-arg "RUNTIME_IMAGE=${RUNTIME_IMAGE}")
fi
if [[ "${NO_CACHE}" -eq 1 ]]; then
  BUILD_ARGS+=(--no-cache)
fi

log "Building and pushing multi-platform image"
docker buildx build \
  --builder "${BUILDER}" \
  --platform "${PLATFORMS}" \
  --provenance=false \
  "${BUILD_ARGS[@]}" \
  -t "${DOCKERHUB_IMAGE}" \
  -t "${ALIYUN_IMAGE}" \
  --push \
  .

log "Completed"
echo "DockerHub: ${DOCKERHUB_IMAGE}"
echo "Aliyun:    ${ALIYUN_IMAGE}"
echo
echo "Inspect with:"
echo "  docker buildx imagetools inspect ${DOCKERHUB_IMAGE}"
echo "  docker buildx imagetools inspect ${ALIYUN_IMAGE}"
