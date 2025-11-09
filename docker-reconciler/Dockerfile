# Use Ubuntu-based image to support nvidia-smi
FROM ubuntu:22.04

# Install wget for healthchecks
# Note: nvidia-smi will be available if host has NVIDIA GPU and /proc/driver/nvidia is mounted
RUN apt-get update && \
    apt-get install -y wget ca-certificates && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /
COPY ./bin/docker-reconciler /bin
COPY --from=colonyos/colonies:latest /bin/colonies /bin/colonies
CMD ["docker-reconciler", "start", "-v"]
