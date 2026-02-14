# Multi-stage Dockerfile for Direttampd
# Stage 1: Build environment with newer glibc
FROM ubuntu:24.04 AS builder

# Install build dependencies including Go
RUN apt-get update && apt-get install -y \
    build-essential \
    g++ \
    make \
    wget \
    && rm -rf /var/lib/apt/lists/*

# Install Go 1.21 (architecture-aware)
ARG TARGETARCH
RUN if [ "$TARGETARCH" = "amd64" ]; then \
        wget -q https://go.dev/dl/go1.21.13.linux-amd64.tar.gz && \
        tar -C /usr/local -xzf go1.21.13.linux-amd64.tar.gz && \
        rm go1.21.13.linux-amd64.tar.gz; \
    else \
        wget -q https://go.dev/dl/go1.21.13.linux-arm64.tar.gz && \
        tar -C /usr/local -xzf go1.21.13.linux-arm64.tar.gz && \
        rm go1.21.13.linux-arm64.tar.gz; \
    fi

ENV PATH="/usr/local/go/bin:${PATH}"

# Set working directory
WORKDIR /build

# Copy source code
COPY . .

# Build C++ library first
# NOTE: MemoryPlayController directory must contain:
#   - ACQUA/ directory with library files
#   - Diretta/ directory with header files
#   - libACQUA_*.a and libDirettaHost_*.a static libraries
# Only build the library, skip test programs
RUN cd MemoryPlayController && \
    make -f Makefile.lib libmemoryplaycontroller.a && \
    cd ..

# Build Go application with CGO enabled
ENV CGO_ENABLED=1
RUN go build -o direttampd ./cmd/direttampd

# Stage 2: Runtime environment
FROM ubuntu:24.04

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    ffmpeg \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Create app directory and cache directory
RUN mkdir -p /app /var/cache/direttampd /etc/direttampd

# Copy binary from builder
COPY --from=builder /build/direttampd /app/direttampd

# Copy example config (can be overridden with volume mount)
COPY --from=builder /build/config.example.yaml /etc/direttampd/config.yaml

# Set working directory
WORKDIR /app

# Expose MPD port
EXPOSE 6600

# Default environment variables
ENV MPD_ADDR=0.0.0.0:6600 \
    CONFIG_PATH=/etc/direttampd/config.yaml

# Run as daemon by default
CMD ["/app/direttampd", "--daemon", "--mpd-addr", "${MPD_ADDR}", "--config", "${CONFIG_PATH}"]
