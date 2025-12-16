# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /build

# Install git for version detection and ca-certificates for HTTPS
RUN apk add --no-cache git ca-certificates tzdata

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build arguments for version injection
ARG VERSION=dev

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o twitch-miner-go \
    ./cmd/miner

# Final stage - scratch image for minimal size
FROM scratch

# Copy CA certificates for HTTPS requests
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy timezone data
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy binary
COPY --from=builder /build/twitch-miner-go /twitch-miner-go

# Create data directories (will be mounted as volumes)
VOLUME ["/config", "/cookies", "/logs", "/analytics"]

# Working directory
WORKDIR /

# Default config path
ENV CONFIG_PATH=/config/config.json

# Run the binary
ENTRYPOINT ["/twitch-miner-go"]
CMD ["-config", "/config/config.json"]
