# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Install git for version detection and ca-certificates for HTTPS
RUN apk add --no-cache git ca-certificates tzdata curl xz

# Download Tailwind CLI
RUN curl -sLo /usr/local/bin/tailwindcss https://github.com/tailwindlabs/tailwindcss/releases/download/v3.4.17/tailwindcss-linux-x64 \
    && chmod +x /usr/local/bin/tailwindcss

# Download and install UPX
RUN curl -sLo /tmp/upx.tar.xz https://github.com/upx/upx/releases/download/v5.0.2/upx-5.0.2-amd64_linux.tar.xz \
    && cd /tmp && tar -xf upx.tar.xz \
    && mv upx-5.0.2-amd64_linux/upx /usr/local/bin/ \
    && rm -rf /tmp/upx*

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build Tailwind CSS
RUN tailwindcss -c tailwind.config.js \
    -i internal/web/static/css/input.css \
    -o internal/web/static/css/app.css \
    --minify

# Build arguments for version injection
ARG VERSION=dev

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X github.com/PatrickWalther/twitch-miner-go/internal/version.Version=${VERSION}" \
    -o twitch-miner-go \
    ./cmd/miner

# Compress binary with UPX
RUN upx --best --lzma twitch-miner-go

# Final stage - scratch image for minimal size
FROM scratch

# Copy CA certificates for HTTPS requests
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy timezone data
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy binary
COPY --from=builder /build/twitch-miner-go /twitch-miner-go

# Create data directories (will be mounted as volumes)
VOLUME ["/config", "/cookies", "/logs", "/database"]

# Working directory
WORKDIR /

# Default config path
ENV CONFIG_PATH=/config/config.json

# Run the binary
ENTRYPOINT ["/twitch-miner-go"]
CMD ["-config", "/config/config.json"]
