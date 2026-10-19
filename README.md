# Radar — AI Tool-Execution Provider

> A high-performance, concurrent Go microservice that acts as a **secure tool-execution provider for AI agents**, exposing sandboxed capabilities over REST and JSON-RPC 2.0.

---

## System Design

```
┌──────────────────────────────────────────────────────────────────────┐
│                        AI Agent (Client)                             │
│               REST POST /tools/*  │  JSON-RPC POST /rpc             │
└────────────────────────┬─────────────────────────────────────────────┘
                         │ HTTPS + Bearer Token
                         ▼
┌──────────────────────────────────────────────────────────────────────┐
│                         Gin HTTP Server                              │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                    Middleware Stack                          │   │
│  │  SecurityHeaders → TraceContext → RequestLogger →           │   │
│  │  RateLimiter (per-IP token bucket) → BearerAuth             │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │                     Tool Handlers                           │    │
│  │                                                             │    │
│  │  POST /tools/ast-parse  ──►  Sandboxed Go AST Parser       │    │
│  │  POST /tools/grep       ──►  Concurrent Regex Log Grep     │    │
│  │  POST /tools/math-eval  ──►  Safe Math Expression Eval     │    │
│  │                                                             │    │
│  │  POST /rpc              ──►  JSON-RPC 2.0 Multiplexer      │    │
│  │    tools.ast_parse, tools.grep, tools.math_eval            │    │
│  └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  GET /health  GET /ready  (unauthenticated)                         │
└──────────────────────────────────────────────────────────────────────┘
```

### Key Design Decisions

| Concern | Approach |
|---|---|
| **Concurrency** | Each tool executes in an isolated goroutine with a `context.WithTimeout`; grep fans out to a configurable worker pool |
| **Security** | Per-IP token-bucket rate limiter; bearer token allowlist; character whitelist on math expressions; no exec/shell calls |
| **Tracing** | W3C Trace Context (`traceparent` / `tracestate`) propagated inbound and re-emitted on every response |
| **Error handling** | Typed `APIError` with machine-readable `code` field and HTTP status; JSON-RPC errors follow spec codes |
| **Zero-allocation hot paths** | `go/parser` + `bufio.Scanner` reuse; pre-allocated response singletons for common errors |
| **Graceful shutdown** | `os.Signal` + `http.Server.Shutdown` with 15 s drain window |
| **Deployment** | Multi-stage Docker build → distroless nonroot runtime (~8 MB image) |

---

## Project Structure

```
radar/
├── cmd/
│   └── radar/
│       └── main.go            # Entry point, graceful shutdown
├── internal/
│   ├── apierr/
│   │   └── errors.go          # Typed error codes + pre-built singletons
│   ├── config/
│   │   └── config.go          # Env-based configuration loader
│   ├── jsonrpc/
│   │   └── handler.go         # JSON-RPC 2.0 multiplexer
│   ├── logger/
│   │   └── logger.go          # Zap structured logger wrapper
│   ├── middleware/
│   │   ├── middleware.go      # TraceContext, RateLimiter, BearerAuth, SecurityHeaders
│   │   └── middleware_test.go
│   ├── server/
│   │   └── server.go          # Gin router wiring
│   └── tools/
│       ├── tools.go           # AST parse, grep, math-eval implementations
│       └── tools_test.go      # Concurrent race-safe tests
├── Dockerfile                 # Multi-stage: deps → builder → distroless
├── docker-compose.yml
├── .env.example
├── go.mod
└── README.md
```

---

## Tools API

### 1. `POST /tools/ast-parse` — Sandboxed Go AST Parser

Parses Go source code and returns all top-level declarations without executing any code.

**Request**
```json
{
  "language": "go",
  "source": "package main\nimport \"fmt\"\nfunc Hello() { fmt.Println(\"hi\") }"
}
```

**Response**
```json
{
  "language": "go",
  "declarations": [
    { "kind": "import", "name": "\"fmt\"", "line": 2 },
    { "kind": "func",   "name": "Hello",  "line": 3 }
  ],
  "parse_errors": []
}
```

**Error codes:** `UNSUPPORTED_LANGUAGE`, `TOOL_TIMEOUT`, `BAD_REQUEST`

---

### 2. `POST /tools/grep` — Concurrent Log Grep

Regex-matches newline-separated log content using a concurrent worker pool. Results are returned in line-number order.

**Request**
```json
{
  "lines": "INFO: start\nERROR: disk full\nWARN: retry\n",
  "pattern": "ERROR|WARN",
  "workers": 4
}
```

**Response**
```json
{
  "pattern": "ERROR|WARN",
  "total_lines": 3,
  "match_count": 2,
  "truncated": false,
  "matches": [
    { "line_number": 2, "content": "ERROR: disk full" },
    { "line_number": 3, "content": "WARN: retry" }
  ]
}
```

**Error codes:** `PARSE_ERROR` (bad regex), `FILE_TOO_LARGE`, `TOOL_TIMEOUT`

---

### 3. `POST /tools/math-eval` — Safe Math Expression Evaluator

Evaluates arithmetic expressions using a hand-written recursive-descent parser. No `eval`, no `exec`, no reflection.

**Supported operators:** `+`, `-`, `*`, `/`, `^` (power), unary `-`  
**Supported functions:** `sqrt`, `abs`, `sin`, `cos`, `tan`, `log`, `log2`, `log10`, `exp`, `floor`, `ceil`, `round`  
**Supported constants:** `pi`, `e`

**Request**
```json
{ "expression": "sqrt(2) ^ 2 + floor(pi)" }
```

**Response**
```json
{
  "expression": "sqrt(2) ^ 2 + floor(pi)",
  "result": 5.000000000000001
}
```

**Error codes:** `INVALID_EXPRESSION`, `EXECUTION_ERROR` (NaN/Inf), `TOOL_TIMEOUT`

---

### 4. `POST /rpc` — JSON-RPC 2.0 Multiplexer

All tools are also accessible via a single JSON-RPC 2.0 endpoint.

**Methods:** `tools.ast_parse`, `tools.grep`, `tools.math_eval`

**Request**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools.math_eval",
  "params": { "expression": "2 ^ 8" }
}
```

---

## Security Model

| Layer | Mechanism |
|---|---|
| **Authentication** | Bearer token checked against `RADAR_AGENT_TOKENS` allowlist |
| **Rate limiting** | Per-IP token bucket: `RADAR_RATE_LIMIT` rps, `RADAR_RATE_BURST` burst |
| **Input validation** | Strict binding + character whitelisting on math expressions |
| **Sandboxing** | Each tool runs in a goroutine with `context.WithTimeout`; no shell, no exec |
| **Least privilege** | Distroless nonroot container (UID 65532); no writable filesystem |
| **Headers** | `X-Content-Type-Options`, `X-Frame-Options`, `Cache-Control: no-store` |

---

## Getting Started

### Run locally

```bash
# Copy env template
cp .env.example .env

# Download dependencies
go mod download

# Run
go run ./cmd/radar
```

### Run tests (with race detector)

```bash
go test -race -v ./...
```

### Docker

```bash
# Build & run via Compose
docker compose up --build

# Or build manually
docker build -t radar:latest .
docker run -p 8080:8080 --env-file .env radar:latest
```

### Health check

```bash
curl http://localhost:8080/health
# {"status":"ok","version":"1.0.0","service":"radar"}
```

---

## Configuration Reference

| Variable | Default | Description |
|---|---|---|
| `RADAR_ENV` | `development` | `development` or `production` (enables Gin release mode) |
| `RADAR_ADDR` | `:8080` | Listen address |
| `RADAR_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `RADAR_RATE_LIMIT` | `20` | Requests per second per IP |
| `RADAR_RATE_BURST` | `40` | Burst allowance |
| `RADAR_AGENT_TOKENS` | _(empty)_ | Comma-separated bearer tokens; empty = dev mode (no auth) |
| `RADAR_MAX_GREP_FILE_MB` | `10` | Maximum log content size for grep (MB) |
| `RADAR_MAX_GREP_MATCHES` | `500` | Maximum matches returned per grep request |
| `RADAR_SANDBOX_TIMEOUT_SEC` | `5` | Per-tool execution timeout (seconds) |
| `RADAR_SERVICE_NAME` | `radar` | Service name embedded in trace headers |

---

## Distributed Tracing

Every request carries W3C Trace Context headers:

- **Inbound:** If `traceparent` is present, the existing trace ID is preserved and a new child span ID is generated.
- **Outbound:** A `traceparent` header is always written on every response.
Format: `traceparent: 00-{traceId}-{spanId}-01`

---

## 💻 Interactive Developer Console & UI Playground

Radar includes a high-performance, dark glassmorphism developer console and tool sandbox embedded directly into the Go binary (`embed.FS`). 

When Radar is running, navigate to `http://localhost:8080/` in any browser to access:

- **AST Code Parser Sandbox**: Paste Go/JS/Python snippets, inspect AST nodes, parse structs, interfaces, and function signatures.
- **Concurrent Grep Simulator**: Test multi-line log streams against regular expressions with real-time match counts and latency stats.
- **Safe Math Evaluator**: Interactive formula playground testing safe math AST tokens without `eval()` or syscall execution.
- **JSON-RPC 2.0 Multiplexer**: Test atomic or batched tool executions against `/rpc` with live request/response JSON inspection.
- **Bearer Token Auth Toggle**: Configure your `RADAR_AGENT_TOKENS` directly in the navbar.
- **Live Latency & W3C Trace IDs**: Real-time measurement of execution latency and trace headers.

---

## 🚀 How to Deploy on Render

Radar is packaged as a minimal, statically-linked binary in a `distroless/static-debian12:nonroot` container. You can deploy it to [Render](https://render.com) as a Docker Web Service in under 2 minutes.

### Option 1: 1-Click Render Blueprint (`render.yaml`)

Radar includes a root `render.yaml` blueprint:

1. Push your repository to GitHub or GitLab.
2. Go to the [Render Dashboard](https://dashboard.render.com/) and navigate to **Blueprints > New Blueprint Instance**.
3. Select the `radar` repository.
4. Render will parse `render.yaml` and configure:
   - Environment: `Docker` (using the multi-stage `Dockerfile`)
   - Region: `Oregon`
   - Port: `8080`
   - Health Check Path: `/health`
   - Auto-generated `RADAR_AGENT_TOKENS`
5. Click **Apply**. Once built, the web console and endpoints will be live at `https://radar-agent-service.onrender.com/`.

### Option 2: Manual Web Service Deployment via Render Dashboard

1. In Render Dashboard, click **New + > Web Service**.
2. Select your Git repository.
3. Choose **Docker** as the runtime environment (Render automatically builds the root `Dockerfile`).
4. Set the **Instance Type** (e.g. Free or Starter).
5. Open **Advanced Settings**:
   - **Health Check Path**: `/health`
6. Add the following **Environment Variables**:
   | Variable | Value | Description |
   |---|---|---|
   | `RADAR_ENV` | `production` | Enables Gin release mode |
   | `RADAR_PORT` | `8080` | Port listened by Radar HTTP server |
   | `RADAR_ADDR` | `0.0.0.0:8080` | Bind address |
   | `RADAR_AGENT_TOKENS` | `your-secret-agent-token` | Comma-separated allowlist of Bearer tokens |
   | `RADAR_RATE_LIMIT` | `100` | Requests per second limit per IP |
   | `RADAR_RATE_BURST` | `200` | Token bucket burst allowance |
   | `RADAR_LOG_LEVEL` | `info` | Structured log level |
7. Click **Create Web Service**. Render builds the multi-stage image, runs health checks against `/health`, and serves the live service with automatic TLS.

