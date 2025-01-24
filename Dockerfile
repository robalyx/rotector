# ============================================
# Stage 1: Build stage
# ============================================
FROM golang:1.23.5-alpine AS builder

# Set working directory
WORKDIR /app

# Install build dependencies
RUN apk add --no-cache upx ca-certificates

# Copy go.mod and go.sum
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binaries with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o /app/bin/bot \
    ./cmd/bot

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o /app/bin/worker \
    ./cmd/worker

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o /app/bin/entrypoint \
    ./cmd/entrypoint

# Compress binaries after building
RUN upx --best --lzma /app/bin/*

# ============================================
# Stage 2: Final stage
# ============================================
FROM gcr.io/distroless/static-debian12:latest-amd64

# Set working directory
WORKDIR /app

# Copy CA certificates from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Set default environment variables
ENV RUN_TYPE=bot \
    WORKER_TYPE=ai \
    WORKERS_COUNT=1

# Copy binaries from builder
COPY --from=builder /app/bin/ /app/bin/

ENTRYPOINT ["/app/bin/entrypoint"]