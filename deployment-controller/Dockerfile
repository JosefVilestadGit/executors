FROM colonyos/colonies:v1.9.0 AS base

FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy common package from parent context
COPY common/ /build/common/

# Copy deployment-controller source
COPY deployment-controller/ /build/deployment-controller/

WORKDIR /build/deployment-controller

# Build the binary
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/deployment-controller ./cmd/main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates wget
WORKDIR /
COPY --from=builder /bin/deployment-controller /bin/

CMD ["deployment-controller", "start", "-v"]
