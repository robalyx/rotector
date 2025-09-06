FROM --platform=$BUILDPLATFORM golang:1.25.1-alpine3.21 AS builder

ARG ENABLE_UPX=true
ARG TARGETOS
ARG TARGETARCH

RUN apk add --no-cache ca-certificates git
RUN if [ "$ENABLE_UPX" = "true" ]; then apk add --no-cache upx; fi

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH

RUN go build -ldflags="-s -w" -o bin/bot ./cmd/bot && \
    go build -ldflags="-s -w" -o bin/worker ./cmd/worker && \
    go build -ldflags="-s -w" -o bin/entrypoint ./cmd/entrypoint && \
    go build -ldflags="-s -w" -o bin/export ./cmd/export && \
    go build -ldflags="-s -w" -o bin/db ./cmd/db

RUN if [ "$ENABLE_UPX" = "true" ]; then \
        upx --best --lzma bin/bot bin/worker bin/entrypoint bin/export bin/db; \
    fi

RUN mkdir -p logs

FROM gcr.io/distroless/static-debian12:latest

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /src/bin /app/bin
COPY --from=builder /src/logs /app/logs

WORKDIR /app

ENV RUN_TYPE=bot WORKER_TYPE=friend WORKERS_COUNT=1

ENTRYPOINT ["/app/bin/entrypoint"]