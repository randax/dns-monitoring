# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev libpcap-dev

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build arguments for version information
ARG VERSION=dev
ARG BUILD_TIME
ARG GIT_COMMIT

# Build the binary with version information
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w \
    -X 'main.Version=${VERSION}' \
    -X 'main.BuildTime=${BUILD_TIME}' \
    -X 'main.GitCommit=${GIT_COMMIT}'" \
    -o dns-monitor \
    ./cmd/dns-monitoring

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    libpcap \
    tzdata \
    && rm -rf /var/cache/apk/*

# Create non-root user
RUN addgroup -g 1000 -S dnsmonitor && \
    adduser -u 1000 -S dnsmonitor -G dnsmonitor

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/dns-monitor /app/dns-monitor

# Copy configuration files
COPY --from=builder /app/config.yaml /app/config.yaml
COPY --from=builder /app/config-minimal.yaml /app/config-minimal.yaml

# Copy examples and docs if they exist
COPY --from=builder /app/examples /app/examples
COPY --from=builder /app/docs /app/docs

# Create necessary directories
RUN mkdir -p /app/logs /app/data && \
    chown -R dnsmonitor:dnsmonitor /app

# Switch to non-root user (Note: passive monitoring will require root)
USER dnsmonitor

# Expose Prometheus metrics port
EXPOSE 9090

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD ["/app/dns-monitor", "version"]

# Default command
ENTRYPOINT ["/app/dns-monitor"]
CMD ["monitor", "-c", "/app/config.yaml"]