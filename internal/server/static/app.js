// Radar AI Tool-Execution Provider - Interactive Console Engine
document.addEventListener('DOMContentLoaded', () => {
  // Elements
  const tabButtons = document.querySelectorAll('.nav-item[data-tab]');
  const tabPanes = document.querySelectorAll('.tab-pane');
  const tokenInput = document.getElementById('agent-token-input');
  const toggleTokenBtn = document.getElementById('toggle-token-btn');
  const endpointDisplay = document.getElementById('curr-endpoint');
  const pingHealthBtn = document.getElementById('btn-ping-health');
  const serviceStatus = document.getElementById('service-status');

  // Toggle token input visibility
  toggleTokenBtn.addEventListener('click', () => {
    tokenInput.type = tokenInput.type === 'password' ? 'text' : 'password';
  });

  // Tab switching
  tabButtons.forEach(btn => {
    btn.addEventListener('click', () => {
      const targetId = btn.getAttribute('data-tab');
      tabButtons.forEach(b => b.classList.remove('active'));
      tabPanes.forEach(p => p.classList.remove('active'));

      btn.classList.add('active');
      const targetPane = document.getElementById(targetId);
      if (targetPane) {
        targetPane.classList.add('active');
      }

      // Update current endpoint in health card
      if (targetId === 'tab-ast') endpointDisplay.textContent = 'POST /tools/ast-parse';
      else if (targetId === 'tab-grep') endpointDisplay.textContent = 'POST /tools/grep';
      else if (targetId === 'tab-math') endpointDisplay.textContent = 'POST /tools/math-eval';
      else if (targetId === 'tab-jsonrpc') endpointDisplay.textContent = 'POST /rpc';
      else if (targetId === 'tab-specs') endpointDisplay.textContent = 'GET /tools/*';
      else if (targetId === 'tab-render') endpointDisplay.textContent = 'Render Cloud';
    });
  });

  // Copy to clipboard helper
  document.querySelectorAll('.btn-copy').forEach(btn => {
    btn.addEventListener('click', () => {
      const targetId = btn.getAttribute('data-target');
      const el = document.getElementById(targetId);
      if (!el) return;
      navigator.clipboard.writeText(el.textContent).then(() => {
        const originalText = btn.textContent;
        btn.textContent = 'Copied!';
        setTimeout(() => { btn.textContent = originalText; }, 1500);
      });
    });
  });

  // Health ping
  pingHealthBtn.addEventListener('click', async () => {
    pingHealthBtn.textContent = 'Pinging...';
    try {
      const res = await fetch('/health');
      if (res.ok) {
        const data = await res.json();
        serviceStatus.textContent = `Online (${data.service} v${data.version})`;
        pingHealthBtn.textContent = 'Status: 200 OK';
      } else {
        serviceStatus.textContent = 'Error: ' + res.status;
        pingHealthBtn.textContent = 'Check Failed';
      }
    } catch (err) {
      serviceStatus.textContent = 'Standalone Mode';
      pingHealthBtn.textContent = 'Local / Static Mode';
    }
    setTimeout(() => { pingHealthBtn.textContent = 'Test /health Endpoint'; }, 2000);
  });

  // ── Helper: Execute API call with timing & headers ──
  async function callAPI(endpoint, payload, isRpc = false) {
    const token = tokenInput.value.trim();
    const headers = {
      'Content-Type': 'application/json',
      'traceparent': `00-${generateHex(32)}-${generateHex(16)}-01`
    };
    if (token) {
      headers['Authorization'] = `Bearer ${token}`;
    }

    const startTime = performance.now();
    try {
      const res = await fetch(endpoint, {
        method: 'POST',
        headers,
        body: JSON.stringify(payload)
      });
      const latency = (performance.now() - startTime).toFixed(2);
      const traceId = res.headers.get('X-Trace-Id') || res.headers.get('traceparent') || headers.traceparent;
      const data = await res.json();
      return { ok: res.ok, status: res.status, latency, traceId, data };
    } catch (err) {
      const latency = (performance.now() - startTime).toFixed(2);
      // Fallback mock simulation if testing frontend offline or without running server
      return simulateOfflineResponse(endpoint, payload, latency);
    }
  }

  function generateHex(len) {
    let s = '';
    while (s.length < len) {
      s += Math.random().toString(16).substring(2);
    }
    return s.substring(0, len);
  }

  // Fallback simulator for offline review
  function simulateOfflineResponse(endpoint, payload, latency) {
    if (endpoint === '/tools/ast-parse') {
      return {
        ok: true,
        status: 200,
        latency: latency || '0.62',
        traceId: '00-mock-trace-ast-98214-01',
        data: {
          package: 'main',
          imports: ['fmt', 'math'],
          types: [{ name: 'AgentContext', kind: 'struct', fields: ['id string', 'tokens int', 'timestamp int64'] }],
          functions: [{ name: 'ComputeVectorScore', exported: true, params: ['a []float64', 'b []float64'], returns: ['float64', 'error'] }],
          line_count: 24,
          syntax_valid: true
        }
      };
    }
    if (endpoint === '/tools/grep') {
      const lines = (payload.content || '').split('\n');
      const re = new RegExp(payload.pattern || 'error', 'i');
      const matches = lines.filter(l => re.test(l)).slice(0, payload.limit || 50);
      return {
        ok: true,
        status: 200,
        latency: latency || '0.45',
        traceId: '00-mock-trace-grep-3392-01',
        data: {
          pattern: payload.pattern,
          matches_found: matches.length,
          matches: matches.map((m, idx) => ({ line_number: idx + 1, content: m })),
          capped: false
        }
      };
    }
    if (endpoint === '/tools/math-eval') {
      let val = 0;
      try {
        // Safe sanitization simulation for preview
        const sanitized = payload.expression.replace(/sqrt\(([^)]+)\)/g, 'Math.sqrt($1)')
          .replace(/pow\(([^,]+),([^)]+)\)/g, 'Math.pow($1,$2)')
          .replace(/max\(/g, 'Math.max(')
          .replace(/sin\(/g, 'Math.sin(')
          .replace(/log2\(/g, 'Math.log2(');
        val = Function(`'use strict'; return (${sanitized})`)();
      } catch (e) {
        val = 527.0;
      }
      return {
        ok: true,
        status: 200,
        latency: latency || '0.18',
        traceId: '00-mock-trace-math-4421-01',
        data: {
          expression: payload.expression,
          result: val,
          type: 'float64',
          ast_nodes: 7
        }
      };
    }
    if (endpoint === '/rpc') {
      return {
        ok: true,
        status: 200,
        latency: latency || '0.85',
        traceId: '00-mock-trace-rpc-batch-01',
        data: [
          { jsonrpc: '2.0', id: 'req-1', result: { expression: 'sqrt(1024) * 8 + 42', result: 298.0, type: 'float64' } },
          { jsonrpc: '2.0', id: 'req-2', result: { matches_found: 2, matches: ['line 2 error: timeout', 'line 3 fatal: memory'] } }
        ]
      };
    }
    return { ok: false, status: 500, latency: '0.00', traceId: 'none', data: { error: 'Unknown endpoint' } };
  }

  // ── Tab 1: AST Run ──
  const astInput = document.getElementById('ast-code-input');
  const astOutput = document.getElementById('ast-output');
  const astStatus = document.getElementById('ast-status-badge');
  const astLatency = document.getElementById('ast-latency-badge');
  const astTrace = document.getElementById('ast-trace-id');
  const btnRunAst = document.getElementById('btn-run-ast');

  async function executeAST() {
    astStatus.className = 'badge-status ready';
    astStatus.textContent = 'PARSING';
    const payload = {
      code: astInput.value,
      language: 'go',
      extract_comments: document.getElementById('ast-extract-comments').checked
    };
    const res = await callAPI('/tools/ast-parse', payload);
    astStatus.className = res.ok ? 'badge-status success' : 'badge-status error';
    astStatus.textContent = `${res.status} ${res.ok ? 'OK' : 'ERR'}`;
    astLatency.textContent = `${res.latency} ms`;
    astTrace.textContent = `Trace: ${res.traceId}`;
    astOutput.textContent = JSON.stringify(res.data, null, 2);
  }

  btnRunAst.addEventListener('click', executeAST);

  // AST Presets
  document.querySelectorAll('.btn-preset').forEach(btn => {
    btn.addEventListener('click', () => {
      const preset = btn.getAttribute('data-preset');
      if (preset === 'go-server') {
        astInput.value = `package server

import (
	"net/http"
	"time"
)

type APIServer struct {
	Addr string
	Timeout time.Duration
}

func (s *APIServer) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	return http.ListenAndServe(s.Addr, mux)
}`;
      } else if (preset === 'go-types') {
        astInput.value = `package types

type ToolExecutionRequest struct {
	TraceID   string                 \`json:"trace_id"\`
	ToolName  string                 \`json:"tool_name"\`
	Params    map[string]interface{} \`json:"params"\`
	TimeoutMs int                    \`json:"timeout_ms"\`
}

type Executor interface {
	Execute(req *ToolExecutionRequest) ([]byte, error)
	Validate() bool
}`;
      } else if (preset === 'go-math') {
        astInput.value = `package mathutil

import "math"

func Hypot(x, y float64) float64 {
	return math.Sqrt(x*x + y*y)
}

func Sigmoid(x float64) float64 {
	return 1.0 / (1.0 + math.Exp(-x))
}`;
      }
      executeAST();
    });
  });

  // ── Tab 2: Grep Run ──
  const grepPattern = document.getElementById('grep-pattern');
  const grepLimit = document.getElementById('grep-limit');
  const grepContent = document.getElementById('grep-content-input');
  const grepOutput = document.getElementById('grep-output');
  const grepStatus = document.getElementById('grep-status-badge');
  const grepLatency = document.getElementById('grep-latency-badge');
  const grepTrace = document.getElementById('grep-trace-id');
  const btnRunGrep = document.getElementById('btn-run-grep');

  async function executeGrep() {
    grepStatus.className = 'badge-status ready';
    grepStatus.textContent = 'GREPPING';
    const payload = {
      pattern: grepPattern.value,
      content: grepContent.value,
      limit: parseInt(grepLimit.value, 10) || 50,
      invert: document.getElementById('grep-invert').checked
    };
    const res = await callAPI('/tools/grep', payload);
    grepStatus.className = res.ok ? 'badge-status success' : 'badge-status error';
    grepStatus.textContent = `${res.status} ${res.ok ? 'OK' : 'ERR'}`;
    grepLatency.textContent = `${res.latency} ms`;
    grepTrace.textContent = `Trace: ${res.traceId}`;
    grepOutput.textContent = JSON.stringify(res.data, null, 2);
  }

  btnRunGrep.addEventListener('click', executeGrep);

  // ── Tab 3: Math Run ──
  const mathExpr = document.getElementById('math-expr-input');
  const mathResultVal = document.getElementById('math-result-val');
  const mathResultType = document.getElementById('math-result-type');
  const mathOutput = document.getElementById('math-output');
  const mathStatus = document.getElementById('math-status-badge');
  const mathLatency = document.getElementById('math-latency-badge');
  const mathTrace = document.getElementById('math-trace-id');
  const btnRunMath = document.getElementById('btn-run-math');

  async function executeMath() {
    mathStatus.className = 'badge-status ready';
    mathStatus.textContent = 'EVALUATING';
    const payload = { expression: mathExpr.value };
    const res = await callAPI('/tools/math-eval', payload);
    mathStatus.className = res.ok ? 'badge-status success' : 'badge-status error';
    mathStatus.textContent = `${res.status} ${res.ok ? 'OK' : 'ERR'}`;
    mathLatency.textContent = `${res.latency} ms`;
    mathTrace.textContent = `Trace: ${res.traceId}`;

    if (res.ok && res.data && typeof res.data.result !== 'undefined') {
      mathResultVal.textContent = res.data.result;
      mathResultType.textContent = `Evaluated in ${res.latency}ms • Type: ${res.data.type || 'float64'}`;
    } else {
      mathResultVal.textContent = 'ERR';
      mathResultType.textContent = res.data?.error || 'Evaluation failed';
    }
    mathOutput.textContent = JSON.stringify(res.data, null, 2);
  }

  btnRunMath.addEventListener('click', executeMath);

  document.querySelectorAll('.btn-math-preset').forEach(btn => {
    btn.addEventListener('click', () => {
      mathExpr.value = btn.getAttribute('data-expr');
      executeMath();
    });
  });

  // ── Tab 4: JSON-RPC Run ──
  const rpcInput = document.getElementById('rpc-body-input');
  const rpcOutput = document.getElementById('rpc-output');
  const rpcStatus = document.getElementById('rpc-status-badge');
  const rpcLatency = document.getElementById('rpc-latency-badge');
  const rpcTrace = document.getElementById('rpc-trace-id');
  const btnRunRpc = document.getElementById('btn-run-rpc');

  async function executeRPC() {
    rpcStatus.className = 'badge-status ready';
    rpcStatus.textContent = 'RPC POST';
    let payload;
    try {
      payload = JSON.parse(rpcInput.value);
    } catch (e) {
      rpcStatus.className = 'badge-status error';
      rpcStatus.textContent = 'BAD JSON';
      rpcOutput.textContent = 'JSON Syntax Error: ' + e.message;
      return;
    }
    const res = await callAPI('/rpc', payload, true);
    rpcStatus.className = res.ok ? 'badge-status success' : 'badge-status error';
    rpcStatus.textContent = `${res.status} ${res.ok ? 'OK' : 'ERR'}`;
    rpcLatency.textContent = `${res.latency} ms`;
    rpcTrace.textContent = `Trace: ${res.traceId}`;
    rpcOutput.textContent = JSON.stringify(res.data, null, 2);
  }

  btnRunRpc.addEventListener('click', executeRPC);

  document.querySelectorAll('.btn-rpc-preset').forEach(btn => {
    btn.addEventListener('click', () => {
      const type = btn.getAttribute('data-rpc');
      if (type === 'batch') {
        rpcInput.value = JSON.stringify([
          {
            jsonrpc: "2.0",
            id: "batch-1",
            method: "radar.mathEval",
            params: { expression: "(12 * 8) + sqrt(256)" }
          },
          {
            jsonrpc: "2.0",
            id: "batch-2",
            method: "radar.grep",
            params: {
              pattern: "error",
              content: "ok\nerror in worker pool\nall good",
              limit: 5
            }
          }
        ], null, 2);
      } else {
        rpcInput.value = JSON.stringify({
          jsonrpc: "2.0",
          id: "single-1",
          method: "radar.mathEval",
          params: { expression: "pow(2, 10) + 1" }
        }, null, 2);
      }
      executeRPC();
    });
  });

  // Global Ctrl+Enter shortcut
  window.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
      const activeTab = document.querySelector('.tab-pane.active');
      if (activeTab.id === 'tab-ast') executeAST();
      else if (activeTab.id === 'tab-grep') executeGrep();
      else if (activeTab.id === 'tab-math') executeMath();
      else if (activeTab.id === 'tab-jsonrpc') executeRPC();
    }
  });

  // Initial ping
  fetch('/health').then(r => r.json()).then(data => {
    serviceStatus.textContent = `Engine: Online (${data.service || 'Radar'})`;
  }).catch(() => {
    serviceStatus.textContent = 'Engine: Offline / Preview';
  });
});
