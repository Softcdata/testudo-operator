#!/bin/bash
set -e

# Default Configuration
VERSION="v1.0.0"
TARGET="all"

# Help function
usage() {
    echo "Usage: $0 [-v <version>] [-t <target>]"
    echo "Options:"
    echo "  -v <version>   Set version tag (default: v1.0.0)"
    echo "  -t <target>    Set push target: 'all', 'huawei', or 'private' (default: all)"
    echo "Example:"
    echo "  $0 -v v1.0.1 -t huawei"
    exit 1
}

# Parse arguments
while getopts "v:t:h" opt; do
  case $opt in
    v) VERSION="$OPTARG" ;;
    t) TARGET="$OPTARG" ;;
    h) usage ;;
    *) usage ;;
  esac
done

# Validate Target
if [[ "$TARGET" != "all" && "$TARGET" != "huawei" && "$TARGET" != "private" ]]; then
    echo "Error: Invalid target '$TARGET'. Must be 'all', 'huawei', or 'private'."
    usage
fi

# Registry Config
HUAWEI_REGISTRY="${HUAWEI_REGISTRY:-registry.example.com}"
HUAWEI_NAMESPACE="${HUAWEI_NAMESPACE:-disaster}"
PRIVATE_REGISTRY="${PRIVATE_REGISTRY:-registry.example.com}"
PRIVATE_NAMESPACE="${PRIVATE_NAMESPACE:-disaster}"

# Image Names
# Primary registry image.
HUAWEI_IMAGE="${HUAWEI_REGISTRY}/${HUAWEI_NAMESPACE}/disaster-operator:${VERSION}"

# Private Registry (Multi-arch single tag)
PRIVATE_IMAGE="${PRIVATE_REGISTRY}/${PRIVATE_NAMESPACE}/disaster-operator:${VERSION}"

# Credentials
HUAWEI_USER="${HUAWEI_USER:-}"
HUAWEI_PASS="${HUAWEI_PASS:-}"
PRIVATE_USER="${PRIVATE_USER:-}"
PRIVATE_PASS="${PRIVATE_PASS:-}"

echo "=========================================================="
echo "Project: Disaster Operator"
echo "Release Version: $VERSION"
echo "Target: $TARGET"
echo "Mode: Multi-Architecture (linux/amd64, linux/arm64)"
echo "=========================================================="

# Ensure buildx builder exists with config
if ! docker buildx inspect default_builder > /dev/null 2>&1; then
    echo "Creating new buildx builder with config..."
    docker buildx create --name default_builder --use --config ./buildkitd.toml
    docker buildx inspect --bootstrap
else
    # Check if the current builder is using the config, if not might need to recreate, 
    # but for simplicity let's just use it. If it fails, user might need to delete it.
    # To be safe, let's force recreate if we are in this script to ensure config is applied
    echo "Recreating buildx builder to ensure config is applied..."
    docker buildx rm default_builder || true
    docker buildx create --name default_builder --use --config ./buildkitd.toml
    docker buildx inspect --bootstrap
fi

# ----------------------------------------------------------
# Login Logic
# ----------------------------------------------------------
if [[ "$TARGET" == "all" || "$TARGET" == "huawei" ]]; then
    if [[ -n "$HUAWEI_USER" && -n "$HUAWEI_PASS" ]]; then
        echo "Logging into Huawei Cloud SWR..."
        echo "$HUAWEI_PASS" | docker login -u "$HUAWEI_USER" --password-stdin "$HUAWEI_REGISTRY"
    else
        echo "Skipping primary registry login; set HUAWEI_USER and HUAWEI_PASS or pre-login with docker."
    fi
fi

if [[ "$TARGET" == "all" || "$TARGET" == "private" ]]; then
    if [[ -n "$PRIVATE_USER" && -n "$PRIVATE_PASS" ]]; then
        echo "Logging into private registry..."
        echo "$PRIVATE_PASS" | docker login -u "$PRIVATE_USER" --password-stdin "$PRIVATE_REGISTRY"
    else
        echo "Skipping private registry login; set PRIVATE_USER and PRIVATE_PASS or pre-login with docker."
    fi
fi

# ----------------------------------------------------------
# Build and Push Logic
# ----------------------------------------------------------
echo "----------------------------------------------------------"
echo "Starting Build & Push..."

if [[ "$TARGET" == "all" ]]; then
    echo "Target 1: $HUAWEI_IMAGE"
    echo "Target 2: $PRIVATE_IMAGE"
    
    docker buildx build --platform linux/amd64,linux/arm64 \
        --no-cache --provenance=false \
        -t "$HUAWEI_IMAGE" \
        -t "$PRIVATE_IMAGE" \
        --push .

elif [[ "$TARGET" == "huawei" ]]; then
    echo "Target: $HUAWEI_IMAGE"
    
    docker buildx build --platform linux/amd64,linux/arm64 \
        --no-cache --provenance=false \
        -t "$HUAWEI_IMAGE" \
        --push .

elif [[ "$TARGET" == "private" ]]; then
    echo "Target: $PRIVATE_IMAGE"
    
    docker buildx build --platform linux/amd64,linux/arm64 \
        --no-cache --provenance=false \
        -t "$PRIVATE_IMAGE" \
        --push .
fi

echo "=========================================================="
echo "Completed!"
echo "Version: $VERSION"
echo "=========================================================="
