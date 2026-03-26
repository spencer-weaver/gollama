# ── builder ───────────────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Download deps first so this layer is cached unless go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o bin/gollama ./cmd/gollama

# ── runtime ───────────────────────────────────────────────────────────────────
FROM alpine:latest

WORKDIR /app

COPY --from=builder /build/bin/gollama bin/gollama

# config/ and models/ are expected to be volume-mounted at runtime.
# The binary falls back to DefaultConfig() if config/gollama.json is absent,
# and reads env vars (HA_TOKEN, HA_HOST, SEARXNG_URL) for sensitive values.

EXPOSE 8080

ENTRYPOINT ["/app/bin/gollama"]
