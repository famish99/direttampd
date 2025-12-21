# Multi-stage Dockerfile for Direttampd
# Stage 1: Build environment
FROM golang:1.21-bookworm AS builder

# Install build dependencies
RUN apt-get update && apt-get install -y \
    build-essential \
    g++ \
    make \
    && rm -rf /var/lib/apt/lists/*

# Set working directory
WORKDIR /build

# Copy source code
COPY . .

# Build C++ library first
RUN cd MemoryPlayController && \
    make -f Makefile.lib && \
    cd ..

# Build Go application with CGO enabled
ENV CGO_ENABLED=1
RUN go build -o direttampd ./cmd/direttampd

# Stage 2: Runtime environment
FROM debian:bookworm-slim

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
