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
    go build -o bin/rest ./cmd/rest
    go build -o bin/rpc ./cmd/rpc

# Run tests with coverage
test:
    go test -v ./...

# Run linter
lint:
    golangci-lint run --fix --timeout 120s

# Run the bot service
run-bot:
    go run ./cmd/bot

# Run the worker service with specified type and count
run-worker type="friend" count="1":
    go run ./cmd/worker {{type}} --workers {{count}}

# Run the REST API service
run-rest:
    go run ./cmd/rest

# Run the RPC service
run-rpc:
    go run ./cmd/rpc

# Run database migrations
run-db *args:
    go run ./cmd/db {{args}}

# Run data export
run-export *args:
    go run ./cmd/export {{args}}

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

# Build container image using Dagger
build-container *args:
    dagger call build {{args}}

# Publish container image using Dagger
publish-container name platform="linux/amd64":
    dagger call publish --src . --image-name {{name}} --platforms {{platform}}
