#!/usr/bin/env bash
set -euo pipefail

IMAGE_REPO="${IMAGE_REPO:-disaster-operator}"
TAG="${TAG:-$(date +%Y%m%d-%H%M%S)}"
PLATFORM="${PLATFORM:-linux/amd64}"
OUT_DIR="${OUT_DIR:-dist}"
CONTAINER_TOOL="${CONTAINER_TOOL:-docker}"
GO_IMAGE="${GO_IMAGE:-docker.io/library/golang:1.24}"
RUNTIME_IMAGE="${RUNTIME_IMAGE:-docker.io/library/alpine:3.20}"
NO_CACHE="${NO_CACHE:-0}"

usage() {
  cat <<'USAGE'
Usage:
  ./build_export_operator.sh [options]

Options:
  -t, --tag <tag>           Image tag. Default: current timestamp.
  -i, --image <repo>        Image repository/name. Default: disaster-operator.
  -p, --platform <platform> Docker platform. Default: linux/amd64.
  -o, --out-dir <dir>       Output directory. Default: dist.
      --no-cache            Build without Docker cache.
  -h, --help                Show this help.

Environment overrides:
  IMAGE_REPO, TAG, PLATFORM, OUT_DIR, CONTAINER_TOOL, GO_IMAGE, RUNTIME_IMAGE, NO_CACHE

Examples:
  ./build_export_operator.sh
  ./build_export_operator.sh --tag fix-init-image-20260708
  PLATFORM=linux/arm64 ./build_export_operator.sh --tag arm64-test
USAGE
}

log() {
  printf '[%s] %s\n' "$(date '+%H:%M:%S')" "$*"
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Error: required command not found: $1" >&2
    exit 1
  fi
}

platform_suffix() {
  printf '%s' "$1" | tr '/:' '--'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -t|--tag)
      TAG="${2:?missing tag}"
      shift 2
      ;;
    -i|--image)
      IMAGE_REPO="${2:?missing image repo/name}"
      shift 2
      ;;
    -p|--platform)
      PLATFORM="${2:?missing platform}"
      shift 2
      ;;
    -o|--out-dir)
      OUT_DIR="${2:?missing output dir}"
      shift 2
      ;;
    --no-cache)
      NO_CACHE=1
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

require_command "${CONTAINER_TOOL}"

if [[ ! -f Dockerfile ]]; then
  echo "Error: Dockerfile not found. Run this script from the disaster-operator repo root." >&2
  exit 1
fi

if [[ ! -f dist/velero-crds.yaml ]]; then
  echo "Error: dist/velero-crds.yaml not found. Run 'make build-crd' first." >&2
  exit 1
fi

mkdir -p "${OUT_DIR}"

IMAGE="${IMAGE_REPO}:${TAG}"
SUFFIX="$(platform_suffix "${PLATFORM}")"
ARTIFACT="${OUT_DIR}/disaster-operator-${TAG}-${SUFFIX}.tar"
LOG_FILE="${OUT_DIR}/disaster-operator-${TAG}-${SUFFIX}-build.log"
META_FILE="${OUT_DIR}/disaster-operator-${TAG}-${SUFFIX}.meta"
SHA_FILE="${ARTIFACT}.sha256"

BUILD_ARGS=(
  build
  --build-arg "GO_IMAGE=${GO_IMAGE}"
  --build-arg "RUNTIME_IMAGE=${RUNTIME_IMAGE}"
  --platform "${PLATFORM}"
  --provenance=false
  -t "${IMAGE}"
)
if [[ "${NO_CACHE}" == "1" ]]; then
  BUILD_ARGS+=(--no-cache)
fi
BUILD_ARGS+=(.)

{
  log "Image: ${IMAGE}"
  log "Platform: ${PLATFORM}"
  log "Artifact: ${ARTIFACT}"
  log "Build log: ${LOG_FILE}"

  log "Building image"
  "${CONTAINER_TOOL}" "${BUILD_ARGS[@]}"

  log "Exporting image tar"
  "${CONTAINER_TOOL}" save -o "${ARTIFACT}" "${IMAGE}"

  log "Writing checksum"
  sha256sum "${ARTIFACT}" | tee "${SHA_FILE}"

  log "Inspecting image"
  "${CONTAINER_TOOL}" image inspect "${IMAGE}" --format 'imageID={{.Id}} size={{.Size}} created={{.Created}}'

  log "Done"
} 2>&1 | tee "${LOG_FILE}"

{
  printf 'image=%s\n' "${IMAGE}"
  printf 'tag=%s\n' "${TAG}"
  printf 'platform=%s\n' "${PLATFORM}"
  printf 'artifact=%s\n' "${ARTIFACT}"
  printf 'sha256_file=%s\n' "${SHA_FILE}"
  printf 'log=%s\n' "${LOG_FILE}"
  printf 'go_image=%s\n' "${GO_IMAGE}"
  printf 'runtime_image=%s\n' "${RUNTIME_IMAGE}"
  printf 'no_cache=%s\n' "${NO_CACHE}"
} > "${META_FILE}"

printf '\nOutput:\n'
printf '  image:    %s\n' "${IMAGE}"
printf '  tar:      %s\n' "${ARTIFACT}"
printf '  sha256:   %s\n' "${SHA_FILE}"
printf '  log:      %s\n' "${LOG_FILE}"
printf '  metadata: %s\n' "${META_FILE}"
