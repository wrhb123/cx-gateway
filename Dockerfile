# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
ARG VERSION=dev
ARG BUILD_TIME=unknown
ARG GIT_COMMIT=unknown

RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags "-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME} -X main.gitCommit=${GIT_COMMIT}" \
    -o /app/ai-proxy-gateway \
    ./cmd/server

# Runtime stage
FROM alpine:3.21

WORKDIR /app

# Install SQLite runtime dependency
RUN apk add --no-cache sqlite-libs ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /app/ai-proxy-gateway /app/ai-proxy-gateway

# Create data directory for SQLite
RUN mkdir -p /app/data

# Expose default ports
EXPOSE 8080 8081

# Environment variables
ENV SERVER_PORT=8080
ENV ADMIN_PORT=8081
ENV DATABASE_PATH=/app/data/proxy.db
ENV SINGLE_PORT=false

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the application
ENTRYPOINT ["/app/ai-proxy-gateway"]
