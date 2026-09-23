# Build the manager binary
# Use --platform=${BUILDPLATFORM} to run build stage on host architecture (faster cross-compilation)
ARG GO_IMAGE=docker.io/library/golang:1.24
ARG RUNTIME_IMAGE=docker.io/library/alpine:3.20

FROM --platform=${BUILDPLATFORM} ${GO_IMAGE} AS builder
ARG TARGETOS=linux
ARG TARGETARCH
ARG BUILDARCH

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# Cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN GOPROXY=https://goproxy.cn,direct go mod download

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY pkg/ pkg/
COPY internal/ internal/

# Copy the tracked Velero chart source; package it in the builder stage.
COPY velero-11.1.1/ velero-11.1.1/

# Use a build-platform Helm binary while packaging the chart. The builder runs
# on BUILDPLATFORM, so executing TARGETARCH's binary would fail during
# cross-platform builds.
# Helm version should match what's currently used in the project
ARG HELM_VERSION=v3.14.0
RUN curl -fsSL https://get.helm.sh/helm-${HELM_VERSION}-linux-${BUILDARCH}.tar.gz | tar xz && \
    mv linux-${BUILDARCH}/helm /usr/local/bin/helm && \
    rm -rf linux-${BUILDARCH} && \
    chmod +x /usr/local/bin/helm && \
    helm package velero-11.1.1/velero --destination /workspace

# The runtime image must contain a Helm binary for its target architecture.
RUN curl -fsSL https://get.helm.sh/helm-${HELM_VERSION}-linux-${TARGETARCH}.tar.gz | tar xz && \
    mv linux-${TARGETARCH}/helm /workspace/helm && \
    rm -rf linux-${TARGETARCH} && \
    chmod +x /workspace/helm

# Build the Go binary for target platform
# CGO_ENABLED=0 ensures static linking for cross-compilation
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -a -o manager cmd/main.go

# Use alpine as minimal base image to package the manager binary
# Alpine official image supports multi-arch (amd64, arm64, etc.)
FROM ${RUNTIME_IMAGE}

USER 1000:1000
WORKDIR /app

# Copy built artifacts from builder stage
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/velero-11.1.1.tgz .
COPY ./velero.values.yaml .
COPY ./velero-crds.yaml .
COPY --from=builder /workspace/helm /usr/local/bin/helm

ENTRYPOINT ["/app/manager"]
