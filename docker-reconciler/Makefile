all: build
.PHONY: all build container push clean buildx-setup multiplatform multiplatform-push

BUILD_IMAGE ?= colonyos/docker-reconciler
PUSH_IMAGE ?= colonyos/docker-reconciler:v1.0.5
PLATFORMS ?= linux/amd64,linux/arm64

VERSION := $(shell git rev-parse --short HEAD)
BUILDTIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

GOLDFLAGS += -X 'main.BuildVersion=$(VERSION)'
GOLDFLAGS += -X 'main.BuildTime=$(BUILDTIME)'

build:
	@mkdir -p ./bin
	@CGO_ENABLED=0 go build -ldflags="-s -w $(GOLDFLAGS)" -o ./bin/docker-reconciler ./cmd/main.go

# Build from repository root (two levels up) to include executors/ and colonies/
container:
	cd ../.. && docker build --no-cache -f executors/docker-reconciler/Dockerfile \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILDTIME=$(BUILDTIME) \
		-t $(BUILD_IMAGE) \
		.

push:
	docker tag $(BUILD_IMAGE) $(PUSH_IMAGE)
	docker push $(PUSH_IMAGE)

# Setup buildx for multiplatform builds
buildx-setup:
	@docker buildx create --name colonyos-builder --use 2>/dev/null || docker buildx use colonyos-builder
	@docker buildx inspect --bootstrap

# Build multiplatform images (local only, not pushed)
# Note: --load only works with single platform, so we build for current platform
multiplatform: buildx-setup
	cd ../.. && docker buildx build -f executors/docker-reconciler/Dockerfile \
		--platform $(PLATFORMS) \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILDTIME=$(BUILDTIME) \
		-t $(BUILD_IMAGE) \
		.

# Build and push multiplatform images to registry
multiplatform-push: buildx-setup
	cd ../.. && docker buildx build -f executors/docker-reconciler/Dockerfile \
		--platform $(PLATFORMS) \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILDTIME=$(BUILDTIME) \
		-t $(PUSH_IMAGE) \
		--push \
		.

clean:
	rm -rf ./bin
