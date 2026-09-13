# ============================================
# Stage 1: Build Go binaries without CGO
# ============================================
FROM golang:1.25-trixie AS builder

WORKDIR /app
ENV GOTOOLCHAIN=auto

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates tzdata libffi8 && rm -rf /var/lib/apt/lists/*

# Cache Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# OpenDAL uses purego; its service libraries are embedded in these binaries.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o /app/bot ./cmd/bot

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /app/gacha ./cmd/gacha

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /app/catalog ./cmd/catalog

RUN test -f config.json || cp config.example.json config.json

# ============================================
# Stage 2: Minimal runtime container
# ============================================
FROM debian:trixie-slim

# OpenDAL service v0.1.16 requires glibc >= 2.38; purego also needs libffi.
# Trixie supplies both, plus certificates, timezone data and FFmpeg.
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata ffmpeg libffi8 libgcc-s1 && rm -rf /var/lib/apt/lists/*

# Create non-root user and group for security
RUN groupadd --gid 10001 appgroup && \
    useradd --uid 10001 --gid appgroup --create-home --shell /usr/sbin/nologin appuser

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/bot /app/bot
COPY --from=builder /app/gacha /app/gacha
COPY --from=builder /app/catalog /app/catalog

# Copy configurations; builder supplies the example when config.json is absent.
COPY economy.json ./
COPY config.example.json ./
COPY --from=builder /app/config.json ./
COPY internal/stockmarket/companies.json ./internal/stockmarket/companies.json
COPY internal/stockmarket/companies.json ./companies.json

# Ensure correct file permissions
RUN mkdir -p /app/data/gacha /app/tmp && chmod 0700 /app/tmp && chown -R appuser:appgroup /app

# OpenDAL extracts embedded service libraries here; the directory must permit executable mappings.
ENV TMPDIR=/app/tmp

# Run as unprivileged user
USER appuser

# Expose API server port
EXPOSE 8080

ENTRYPOINT ["/app/bot"]
