# Multi-stage build for shardforge-node and shardforge-proxy
# Stage 1: build both binaries
FROM golang:1.21-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /shardforge-node ./cmd/shardforge-node
RUN CGO_ENABLED=0 GOOS=linux go build -o /shardforge-proxy ./cmd/shardforge-proxy

# Stage 2: minimal runtime image containing both binaries
FROM alpine:3.19

# wget is used by Docker Compose healthchecks
RUN apk add --no-cache wget

COPY --from=builder /shardforge-node  /usr/local/bin/shardforge-node
COPY --from=builder /shardforge-proxy /usr/local/bin/shardforge-proxy

# Default entrypoint is the node binary; override per service in docker-compose.
ENTRYPOINT ["/usr/local/bin/shardforge-node"]
