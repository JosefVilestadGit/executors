# Multi-stage build for multiplatform support
FROM golang:1.24-alpine AS builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH

WORKDIR /build

# Copy entire executors directory (to get common/) and colonies directory
# Assumes build context is from repository root or parent of executors
COPY executors ./executors
COPY colonies ./colonies

# Navigate to docker executor directory
WORKDIR /build/executors/docker

# Download dependencies (replace directives will work now)
RUN go mod download

# Build binary for target platform
# Use shell parameter expansion for defaults (works in regular docker build)
ARG VERSION=dev
ARG BUILDTIME=unknown
RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-s -w -X 'main.BuildVersion=${VERSION}' -X 'main.BuildTime=${BUILDTIME}'" \
    -o docker_executor ./cmd/main.go

# Final stage - use colonies base image
FROM colonyos/colonies:v1.8.7

WORKDIR /
COPY --from=builder /build/executors/docker/docker_executor /bin/
RUN mkdir /cfs
CMD ["docker_executor", "start", "-v"]
