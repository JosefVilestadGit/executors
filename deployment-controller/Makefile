all: build
.PHONY: all build

BUILD_IMAGE ?= colonyos/deployment-controller
PUSH_IMAGE ?= colonyos/deployment-controller:v1.0.0

VERSION := $(shell git rev-parse --short HEAD)
BUILDTIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

GOLDFLAGS += -X 'main.BuildVersion=$(VERSION)'
GOLDFLAGS += -X 'main.BuildTime=$(BUILDTIME)'

build:
	@CGO_ENABLED=0 go build -ldflags="-s -w $(GOLDFLAGS)" -o ./bin/deployment-controller ./cmd/main.go

container:
	cd .. && docker build -f deployment-controller/Dockerfile -t $(BUILD_IMAGE) .

push:
	docker tag $(BUILD_IMAGE) $(PUSH_IMAGE)
	docker push $(PUSH_IMAGE)

clean:
	rm -rf ./bin
