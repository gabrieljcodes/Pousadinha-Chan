# ============================================
# Stage 1: Build static binary
# ============================================
FROM golang:1.24-alpine AS builder

WORKDIR /app
ENV GOTOOLCHAIN=auto

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build static binary with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o /app/bot ./cmd/bot

# ============================================
# Stage 2: Minimal runtime container
# ============================================
FROM alpine:3.21

# Install CA certificates for HTTPS/WSS (Discord & External APIs) and timezone data
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user and group for security
RUN addgroup -g 10001 -S appgroup && \
    adduser -u 10001 -S appuser -G appgroup

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/bot /app/bot

# Copy default configurations (wildcards ensure build succeeds whether config.json is present or not)
COPY economy.json ./
COPY config.example.json ./
COPY config.json* ./
COPY internal/stockmarket/companies.json ./internal/stockmarket/companies.json
COPY internal/stockmarket/companies.json ./companies.json

# Ensure correct file permissions
RUN chown -R appuser:appgroup /app

# Run as unprivileged user
USER appuser

# Expose API server port
EXPOSE 8080

ENTRYPOINT ["/app/bot"]
