all: build
.PHONY: all build container push clean buildx-setup multiplatform multiplatform-push

BUILD_IMAGE ?= colonyos/echoexecutor
PUSH_IMAGE ?= colonyos/echoexecutor:v1.0.8
PLATFORMS ?= linux/amd64,linux/arm64

VERSION := $(shell git rev-parse --short HEAD)
BUILDTIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

GOLDFLAGS += -X 'main.BuildVersion=$(VERSION)'
GOLDFLAGS += -X 'main.BuildTime=$(BUILDTIME)'

build:
	@mkdir -p ./bin
	@CGO_ENABLED=0 go build -ldflags="-s -w $(GOLDFLAGS)" -o ./bin/echo_executor ./cmd/main.go

container:
	docker build --build-arg VERSION=$(VERSION) --build-arg BUILDTIME=$(BUILDTIME) -t $(BUILD_IMAGE) .

push:
	docker tag $(BUILD_IMAGE) $(PUSH_IMAGE)
	docker push $(PUSH_IMAGE)
	docker tag $(BUILD_IMAGE) $(BUILD_IMAGE):latest
	docker push $(BUILD_IMAGE):latest

# Setup buildx for multiplatform builds
buildx-setup:
	@docker buildx create --name colonyos-builder --use 2>/dev/null || docker buildx use colonyos-builder
	@docker buildx inspect --bootstrap

# Build multiplatform images (local only, not pushed)
multiplatform: buildx-setup
	docker buildx build \
		--platform $(PLATFORMS) \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILDTIME=$(BUILDTIME) \
		-t $(BUILD_IMAGE) \
		.

# Build and push multiplatform images to registry
multiplatform-push: buildx-setup
	docker buildx build \
		--platform $(PLATFORMS) \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILDTIME=$(BUILDTIME) \
		-t $(PUSH_IMAGE) \
		-t $(BUILD_IMAGE):latest \
		--push \
		.

clean:
	rm -rf ./bin
