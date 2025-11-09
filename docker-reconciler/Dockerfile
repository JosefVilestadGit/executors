# Multi-stage build for multiplatform support
FROM golang:1.24-alpine AS builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH

WORKDIR /build

# Copy entire executors directory (we need common/) and colonies directory
# Assumes build context is from repository root
COPY executors ./executors
COPY colonies ./colonies

# Navigate to docker-reconciler and build
WORKDIR /build/executors/docker-reconciler

# Download dependencies
RUN go mod download

# Build binary for target platform
# Use runtime.GOARCH and runtime.GOOS as fallback for regular docker build
ARG VERSION=dev
ARG BUILDTIME=unknown
RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-s -w -X 'main.BuildVersion=${VERSION}' -X 'main.BuildTime=${BUILDTIME}'" \
    -o docker-reconciler ./cmd/main.go

# Final stage - use Ubuntu for nvidia-smi support
FROM ubuntu:22.04

# Install wget for healthchecks and ca-certificates
# Note: nvidia-smi will be available if host has NVIDIA GPU and /proc/driver/nvidia is mounted
RUN apt-get update && \
    apt-get install -y wget ca-certificates && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /
COPY --from=builder /build/executors/docker-reconciler/docker-reconciler /bin/
COPY --from=colonyos/colonies:latest /bin/colonies /bin/colonies
CMD ["docker-reconciler", "start", "-v"]
