# Multiplatform Docker Image Builds

This document explains how to build and push multiplatform Docker images for ColonyOS executors.

## Supported Platforms

By default, images are built for:
- `linux/amd64` (x86_64)
- `linux/arm64` (ARM 64-bit, e.g., Apple Silicon, Raspberry Pi 4+)

You can customize this by setting the `PLATFORMS` variable.

## Prerequisites

1. **Docker Buildx** (included in Docker Desktop and recent Docker versions)
2. **QEMU** for cross-platform emulation (automatically installed by buildx)

Verify buildx is available:
```bash
docker buildx version
```

## Quick Start

### Build for Current Platform

Standard build (native platform only):

```bash
cd executors/docker
make container

# Or for docker-reconciler
cd executors/docker-reconciler
make container
```

### Build for Multiple Platforms

Build for all supported platforms:

```bash
cd executors/docker
make multiplatform

# Or for docker-reconciler
cd executors/docker-reconciler
make multiplatform
```

### Build and Push Multiplatform Images

Build and push to registry (requires push permissions):

```bash
cd executors/docker
make multiplatform-push PUSH_IMAGE=yourregistry/dockerexecutor:v1.0.0

# Or for docker-reconciler
cd executors/docker-reconciler
make multiplatform-push PUSH_IMAGE=yourregistry/docker-reconciler:latest
```

## Available Make Targets

### Common Targets

| Target | Description |
|--------|-------------|
| `build` | Build Go binary for current platform |
| `container` | Build Docker image for current platform |
| `push` | Tag and push current platform image |
| `clean` | Remove build artifacts |

### Multiplatform Targets

| Target | Description |
|--------|-------------|
| `buildx-setup` | Create and configure buildx builder |
| `multiplatform` | Build for multiple platforms (local) |
| `multiplatform-push` | Build and push multiplatform images |

## Customizing Platforms

Build for specific platforms:

```bash
make multiplatform PLATFORMS=linux/amd64,linux/arm64,linux/arm/v7
```

Common platform values:
- `linux/amd64` - Intel/AMD 64-bit
- `linux/arm64` - ARM 64-bit (Apple Silicon, modern ARM servers)
- `linux/arm/v7` - ARM 32-bit (older Raspberry Pi)
- `linux/386` - Intel/AMD 32-bit

## Build Arguments

Both Dockerfiles accept build arguments:

| Argument | Description | Default |
|----------|-------------|---------|
| `VERSION` | Build version (git commit hash) | Auto-detected |
| `BUILDTIME` | Build timestamp | Auto-detected |
| `TARGETOS` | Target OS (set by buildx) | `linux` |
| `TARGETARCH` | Target architecture (set by buildx) | `amd64` |

Example with custom version:

```bash
make multiplatform-push \
  VERSION=v1.2.3 \
  BUILDTIME=$(date -u '+%Y-%m-%dT%H:%M:%SZ') \
  PUSH_IMAGE=colonyos/dockerexecutor:v1.2.3
```

## How It Works

### Multi-Stage Builds

Both Dockerfiles use multi-stage builds:

**Stage 1 - Builder:**
- Uses `golang:1.24-alpine` base image
- Copies source code and dependencies
- Builds Go binary for target platform
- Uses `GOOS` and `GOARCH` for cross-compilation

**Stage 2 - Runtime:**
- Uses minimal runtime image (colonyos/colonies or ubuntu:22.04)
- Copies compiled binary from builder stage
- Much smaller final image size

### Cross-Compilation

Go supports cross-compilation natively:

```dockerfile
RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-s -w" -o binary ./cmd/main.go
```

- `CGO_ENABLED=0` - Disable C dependencies (fully static binary)
- `GOOS` and `GOARCH` - Set by buildx for each target platform
- Fallback values work with regular `docker build`

### Build Context

Both executors build from the repository root (not from their own directory) because they depend on:

**Docker Executor:**
- `executors/common/` - Shared executor utilities
- `colonies/` - Colonies client library

**Docker Reconciler:**
- `executors/common/` - Shared executor utilities
- `colonies/` - Colonies client library

The Makefiles automatically `cd ../..` to use the correct build context.

## Troubleshooting

### Builder Not Found

Error: `no builder "colonyos-builder" found`

Solution:
```bash
make buildx-setup
```

### Cannot Load Multiplatform Image

Error: `--load and --push are mutually exclusive`

Explanation: Docker can only load one platform at a time to local images.

Solutions:
- Use `multiplatform-push` to push directly to registry
- Or build for single platform: `make multiplatform PLATFORMS=linux/amd64`

### Cross-Platform Build Slow

First build is slow because:
1. Setting up QEMU emulation
2. Building for multiple architectures
3. Downloading base images for each platform

Subsequent builds use Docker layer caching and are faster.

### Build Fails with "go.mod requires go >= X.XX"

The Dockerfile uses `golang:1.24-alpine`. If you need a different version:

```dockerfile
FROM golang:1.25-alpine AS builder  # Update version
```

Or wait for Go to release that version.

## CI/CD Integration

### GitHub Actions

```yaml
name: Build Multiplatform Images

on:
  push:
    tags:
      - 'v*'

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v2

      - name: Login to Docker Hub
        uses: docker/login-action@v2
        with:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}

      - name: Build and push
        run: |
          cd executors/docker
          make multiplatform-push PUSH_IMAGE=colonyos/dockerexecutor:${{ github.ref_name }}
```

### GitLab CI

```yaml
build-multiplatform:
  image: docker:latest
  services:
    - docker:dind
  before_script:
    - docker buildx create --use
  script:
    - cd executors/docker
    - make multiplatform-push PUSH_IMAGE=$CI_REGISTRY_IMAGE:$CI_COMMIT_TAG
  only:
    - tags
```

## Examples

### Example 1: Build Both Executors

```bash
# Build docker executor
cd executors/docker
make multiplatform

# Build docker-reconciler
cd ../docker-reconciler
make multiplatform
```

### Example 2: Release to Docker Hub

```bash
# Tag and push docker executor
cd executors/docker
docker login
make multiplatform-push PUSH_IMAGE=colonyos/dockerexecutor:v1.0.5

# Tag and push docker-reconciler
cd ../docker-reconciler
make multiplatform-push PUSH_IMAGE=colonyos/docker-reconciler:v1.0.5
```

### Example 3: Test on Different Architectures

```bash
# Build for ARM64
make multiplatform PLATFORMS=linux/arm64

# Run on ARM64 machine
docker run colonyos/dockerexecutor:latest

# Or test locally with QEMU (slow)
docker run --platform linux/arm64 colonyos/dockerexecutor:latest
```

## Performance Tips

1. **Use BuildKit**: Enabled by default in recent Docker versions
   ```bash
   export DOCKER_BUILDKIT=1
   ```

2. **Parallel Builds**: Buildx builds all platforms in parallel automatically

3. **Layer Caching**: Keep frequently changing files (source code) at the end of Dockerfile

4. **Prune Builders**: Clean up old builders periodically
   ```bash
   docker buildx prune
   ```

5. **Use Build Cache**: Mount Go module cache for faster builds
   ```dockerfile
   RUN --mount=type=cache,target=/go/pkg/mod \
       go mod download
   ```

## Testing Multiplatform Images

Verify image supports multiple platforms:

```bash
docker buildx imagetools inspect colonyos/dockerexecutor:latest
```

Output shows supported platforms:
```
Name:      colonyos/dockerexecutor:latest
MediaType: application/vnd.docker.distribution.manifest.list.v2+json
Digest:    sha256:...

Manifests:
  Name:      colonyos/dockerexecutor:latest@sha256:...
  MediaType: application/vnd.docker.distribution.manifest.v2+json
  Platform:  linux/amd64

  Name:      colonyos/dockerexecutor:latest@sha256:...
  MediaType: application/vnd.docker.distribution.manifest.v2+json
  Platform:  linux/arm64
```

## Resources

- [Docker Buildx Documentation](https://docs.docker.com/buildx/working-with-buildx/)
- [Multi-platform Images](https://docs.docker.com/build/building/multi-platform/)
- [Go Cross-Compilation](https://go.dev/doc/install/source#environment)
- [Docker Manifest Lists](https://docs.docker.com/registry/spec/manifest-v2-2/#manifest-list)

## Support

For issues or questions:
- Check [ColonyOS GitHub Issues](https://github.com/colonyos/colonies/issues)
- Review Docker buildx logs: `docker buildx ls`
- Enable debug logging: `export BUILDX_DEBUG=1`
