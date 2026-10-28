# Radar — Controlled AI Agent Tool Execution Sandbox Demo Script

## 1. The 30-Second Elevator Pitch
> *"Giving autonomous AI agents direct bash or shell execution is an enterprise security nightmare — agents can loop infinitely, exfiltrate secrets, or execute destructive commands. **Radar** is a high-performance Go microservice (built on Gin and JSON-RPC 2.0) that exposes a strictly constrained, sandboxed tool provider to AI agents. It features an in-memory Go AST source parser, a bounded concurrent log-grep engine with worker pools, and an allowlisted mathematical evaluator — all secured with per-IP token-bucket rate limiting, context timeouts, and W3C trace propagation."*

---

## 2. Senior Architecture Deep Dive

```
AI Agent / LLM -> [REST or JSON-RPC 2.0 (/rpc)]
                         |
                         v
              [Gin Middleware Chain]
    • Bearer Token Authentication (Per-Agent)
    • Token Bucket Rate Limiter (20 req/s, 40 burst)
    • W3C Distributed Trace Context
    • Zap Structured Logger & Recovery
                         |
                         v
              [Tool Sandboxing Runtime]
    • Context Deadline Enforcement (5s timeout)
    • Strict Output Buffer Limits (Max 500 matches)
           /             |             \
          v              v              v
   [Go AST Parser] [Concurrent Grep] [AST Math Evaluator]
   (go/parser)     (Worker Pool)    (No shell / eval)
```

---

## 3. How to Run & Demo

```powershell
Set-Location "C:\Users\HP\Documents\Exquisapps\ID's\lekeOmotayo\radar"
.\radar-server.exe
# Or run with source:
go run ./cmd/radar
```
Open **`http://localhost:8080`** in your browser.

### Demo Flow:
1. **Health & Liveness Check**: Call `GET /health`. Show JSON status, version, and server uptime.
2. **AST Parser Tool**: Paste a Go struct snippet in the web console. Show how Radar parses AST nodes and returns function signatures and fields without executing any code.
3. **Concurrent Grep Tool**: Search a log buffer with regex. Explain how worker pool concurrency streams lines with bounded memory.
4. **Sandboxed Math Evaluator**: Evaluate `(45 * 12) / sqrt(144)`. Then attempt `os.system('whoami')` to prove that malicious shell injections are rejected by the AST tokenizer.
5. **JSON-RPC 2.0 Flexibility**: Demonstrate the same tool call over `POST /rpc` with `{"jsonrpc": "2.0", "method": "tools.math", "params": {...}}`.

---

## 4. Senior Defense Questions & Answers

### Q: "How does Radar prevent denial-of-service from large log files?"
> **Answer:** *"The grep tool never loads the entire file into memory. It streams lines via `bufio.Scanner` across a fixed worker pool, enforces a file size cap (`RADAR_MAX_GREP_FILE_MB`), and terminates immediately once `RADAR_MAX_GREP_MATCHES` (default 500) is reached or the context timeout expires."*

### Q: "Why support both REST and JSON-RPC 2.0?"
> **Answer:** *"Modern agent orchestration frameworks (like LangChain, AutoGen, and semantic kernel agents) communicate via JSON-RPC for batching and typed RPC contracts, while web frontends and microservices prefer REST. Radar supports both on the same router."*
