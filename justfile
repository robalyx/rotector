set windows-shell := ["C:/Program Files/Git/bin/bash.exe", "-c"]

# Default recipe to display help information
default:
    @just --list

# Build all binaries
build:
    go build -o bin/bot ./cmd/bot
    go build -o bin/worker ./cmd/worker
    go build -o bin/export ./cmd/export
    go build -o bin/db ./cmd/db

# Run tests with coverage
test:
    go test -v -race -cover ./...

# Run linter
lint:
    golangci-lint run --fix --timeout 300s

# Run the bot service
run-bot:
    go run ./cmd/bot

# Run the worker service with specified type and count
run-worker type="friend" count="1":
    go run ./cmd/worker {{type}} --workers {{count}}

# Run database migrations
run-db *args:
    go run ./cmd/db {{args}}

# Run data export with standardized settings
run-export description="Export" version="1.0.1":
    # Create exports directory if it doesn't exist
    mkdir -p exports
    # Run export command with standardized settings
    go run ./cmd/export \
        -o exports \
        --salt "r0t3ct0r_$(date +%Y%m%d)_$RANDOM" \
        --export-version {{version}} \
        --description "{{description}}" \
        --hash-type argon2id \
        --c 10 \
        --i 16 \
        -m 32

# Run queue command
run-queue:
    go run ./cmd/queue

# Clean build artifacts
clean:
    rm -rf bin/
    go clean -cache -testcache

# Download dependencies
deps:
    go mod download
    go mod tidy

# Generate mocks and other generated code
generate:
    go generate ./...

# Build container image
build-container *args:
    dagger call build {{args}}

# Publish container image
# Usage: just publish-container [image-name] [platform] [upx]
publish-container image-name platform="linux/amd64" upx="true":
    dagger call publish \
        --src . \
        --image-name "{{image-name}}" \
        --platforms "{{platform}}" \
        --enable-upx "{{upx}}"

# Build container image without UPX compression
build-container-no-upx *args:
    dagger call build --enable-upx "false" {{args}}
