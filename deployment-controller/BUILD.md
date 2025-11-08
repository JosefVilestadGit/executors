# Building the Deployment Controller

## Building the Binary

From the deployment-controller directory:

```bash
make build
```

This creates: `./bin/deployment-controller`

## Building the Container

The container build requires access to the shared `common` package, so it must be built from the parent (`executors`) directory.

### Using Make (Recommended)

From the deployment-controller directory:

```bash
make container
```

This automatically builds from the correct context.

### Manual Docker Build

From the **executors** directory (parent):

```bash
cd executors
docker build -f deployment-controller/Dockerfile -t colonyos/deployment-controller .
```

### Using docker-compose

From the deployment-controller directory:

```bash
docker-compose build
```

The `docker-compose.yml` is already configured with the correct build context.

## Build Context Explanation

The Dockerfile requires both:
- `common/` - Shared utilities used by multiple executors
- `deployment-controller/` - This executor's source code

The build context must include both directories, which is why we build from the parent `executors/` directory:

```
executors/
├── common/              # Shared dependency
└── deployment-controller/
    ├── Dockerfile       # References ../common
    ├── cmd/
    ├── pkg/
    └── ...
```

## Troubleshooting

### Error: "replacement directory ../common/ does not exist"

**Cause**: Building from the wrong directory

**Solution**: Use `make container` or build from the `executors/` directory:

```bash
cd executors
docker build -f deployment-controller/Dockerfile -t colonyos/deployment-controller .
```

### Error: "COPY failed: file not found"

**Cause**: Docker build context doesn't include required files

**Solution**: Ensure you're building from the `executors/` directory, not `executors/deployment-controller/`

## Version Information

Build version and time are injected during compilation:

```bash
# Local build with version info
make build

# Check version
./bin/deployment-controller version
```

The Makefile automatically captures:
- Git commit hash
- Build timestamp

## Cross-Compilation

To build for different platforms:

```bash
# Linux AMD64
GOOS=linux GOARCH=amd64 make build

# Linux ARM64
GOOS=linux GOARCH=arm64 make build

# macOS ARM64 (Apple Silicon)
GOOS=darwin GOARCH=arm64 make build
```

## Multi-Platform Container Images

Build for multiple architectures:

```bash
# Enable buildx
docker buildx create --use

# Build for multiple platforms
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -f deployment-controller/Dockerfile \
  -t colonyos/deployment-controller:latest \
  --push \
  .
```

Note: Run from the `executors/` directory.
