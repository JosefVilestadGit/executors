# Multi-stage build for multiplatform support
FROM golang:1.24-alpine AS builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH

WORKDIR /build

# Copy source code
COPY . .

# Download dependencies
RUN go mod download

# Build binary for target platform
ARG VERSION=dev
ARG BUILDTIME=unknown
RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-s -w -X 'main.BuildVersion=${VERSION}' -X 'main.BuildTime=${BUILDTIME}'" \
    -o echo_executor ./cmd/main.go

# Final stage
FROM alpine:3.19

RUN apk add --no-cache ca-certificates

WORKDIR /
COPY --from=builder /build/echo_executor /bin/echo_executor

CMD ["echo_executor", "start"]
