all: build
.PHONY: all build container push clean

BUILD_IMAGE ?= colonyos/docker-reconciler
PUSH_IMAGE ?= colonyos/docker-reconciler:latest

VERSION := $(shell git rev-parse --short HEAD)
BUILDTIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

GOLDFLAGS += -X 'main.BuildVersion=$(VERSION)'
GOLDFLAGS += -X 'main.BuildTime=$(BUILDTIME)'

build:
	@mkdir -p ./bin
	@CGO_ENABLED=0 go build -ldflags="-s -w $(GOLDFLAGS)" -o ./bin/docker-reconciler ./cmd/main.go

container: build
	docker build -t $(BUILD_IMAGE) .

push:
	docker tag $(BUILD_IMAGE) $(PUSH_IMAGE)
	docker push $(PUSH_IMAGE)

clean:
	rm -rf ./bin
