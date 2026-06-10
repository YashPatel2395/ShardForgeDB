# Multi-stage build for shardforge-node
# Stage 1: build the binary
FROM golang:1.21-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /shardforge-node ./cmd/shardforge-node

# Stage 2: minimal runtime image
FROM alpine:3.19

# wget is used by Docker Compose healthcheck
RUN apk add --no-cache wget

COPY --from=builder /shardforge-node /usr/local/bin/shardforge-node

ENTRYPOINT ["/usr/local/bin/shardforge-node"]
