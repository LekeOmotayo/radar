# ==============================================================================
# Radar — AI Tool-Execution Provider
# Multi-stage Dockerfile: builder → distroless production image
# ==============================================================================

# ── Stage 1: Module cache (maximizes layer caching) ──────────────────────────
FROM golang:1.22-alpine AS deps

WORKDIR /radar

# Copy only dependency manifests first — cache bust only on dep changes
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# ── Stage 2: Build ────────────────────────────────────────────────────────────
FROM deps AS builder

WORKDIR /radar

# Copy full source
COPY . .

# Build statically linked binary with security hardening flags
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
      -trimpath \
      -ldflags="-s -w -extldflags '-static' -X main.version=$(cat go.mod | grep ^module | awk '{print $2}')" \
      -o /out/radar \
      ./cmd/radar

# Run tests with race detector during CI build (remove for pure prod builds)
RUN go test -race -timeout 60s ./... 2>&1 | tee /out/test-results.txt

# ── Stage 3: Minimal production runtime ──────────────────────────────────────
# Using distroless/static — no shell, no package manager, minimal attack surface
FROM gcr.io/distroless/static-debian12:nonroot AS runner

WORKDIR /app

# Copy only the compiled binary from the builder
COPY --from=builder /out/radar /app/radar

# Distroless nonroot already runs as UID 65532 — no USER directive needed

# Expose service port
EXPOSE 8080

# Liveness probe friendly: binary exits cleanly on signal
ENTRYPOINT ["/app/radar"]
