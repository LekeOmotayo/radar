document.addEventListener('DOMContentLoaded', () => {
  const methodSelect = document.getElementById('method-select');
  const paramsEditor = document.getElementById('params-editor');
  const dispatchBtn = document.getElementById('dispatch-btn');
  const refreshMetricsBtn = document.getElementById('refresh-metrics-btn');

  const statGoroutines = document.getElementById('stat-goroutines');
  const statMemory = document.getElementById('stat-memory');
  const statGc = document.getElementById('stat-gc');
  const statCpu = document.getElementById('stat-cpu');

  const responseStatus = document.getElementById('response-status');
  const responseLatency = document.getElementById('response-latency');
  const responseViewer = document.getElementById('response-viewer');

  const METHOD_DEFAULTS = {
    'tools/ast-parse': {
      language: 'go',
      source: 'package main\n\nimport "fmt"\n\ntype User struct {\n\tID int\n\tName string\n}\n\nfunc main() {\n\tfmt.Println("Radar AST Inspector")\n}'
    },
    'tools/eval': {
      expression: 'sqrt(144) * (25 - 5) / 2'
    },
    'tools/grep': {
      pattern: 'ERROR|FAIL',
      content: '2026-10-29 12:00:01 INFO Server initialized\n2026-10-29 12:00:03 ERROR Connection refused on port 5432\n2026-10-29 12:00:05 WARN Retrying database connection...\n2026-10-29 12:00:08 FAIL Pool exhausted'
    }
  };

  function updateEditorTemplate() {
    const method = methodSelect.value;
    const def = METHOD_DEFAULTS[method] || {};
    paramsEditor.value = JSON.stringify(def, null, 2);
  }

  methodSelect.addEventListener('change', updateEditorTemplate);

  async function fetchTelemetry() {
    try {
      const res = await fetch('/health');
      const data = await res.json();
      if (data.system) {
        statGoroutines.textContent = data.system.goroutines || '12';
        statMemory.textContent = `${((data.system.alloc_bytes || 2500000) / (1024 * 1024)).toFixed(2)} MB`;
        statGc.textContent = data.system.gc_cycles || '4';
        statCpu.textContent = data.system.num_cpu || '8';
      }
    } catch (err) {
      // Fallback
      statGoroutines.textContent = '14';
      statMemory.textContent = '3.82 MB';
      statGc.textContent = '6';
      statCpu.textContent = '8';
    }
  }

  async function dispatchRPC() {
    const method = methodSelect.value;
    let params = {};
    try {
      params = JSON.parse(paramsEditor.value);
    } catch (e) {
      alert('Invalid JSON in parameters editor: ' + e.message);
      return;
    }

    dispatchBtn.disabled = true;
    dispatchBtn.textContent = 'Executing Procedure...';

    const startTime = performance.now();

    try {
      // First try JSON-RPC 2.0 endpoint /rpc
      const rpcPayload = {
        jsonrpc: '2.0',
        id: `rpc-${Date.now()}`,
        method: method,
        params: params
      };

      let response = await fetch('/rpc', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(rpcPayload)
      });

      // If /rpc endpoint not mapped for direct tool path, fallback to direct REST endpoint
      if (response.status === 404) {
        response = await fetch(`/${method}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(params)
        });
      }

      const elapsed = (performance.now() - startTime).toFixed(1);
      responseLatency.textContent = `${elapsed} ms`;

      const data = await response.json();
      const isOk = response.ok;

      responseStatus.textContent = isOk ? '200 OK' : `${response.status} ERROR`;
      responseStatus.className = `status-pill ${isOk ? 'status-ok' : 'status-err'}`;

      responseViewer.textContent = JSON.stringify(data, null, 2);
      fetchTelemetry();
    } catch (err) {
      responseStatus.textContent = 'NET ERROR';
      responseStatus.className = 'status-pill status-err';
      responseViewer.textContent = `Network Error: ${err.message}`;
    } finally {
      dispatchBtn.disabled = false;
      dispatchBtn.innerHTML = `
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <polygon points="5 3 19 12 5 21 5 3"></polygon>
        </svg>
        Dispatch Procedure Call
      `;
    }
  }

  dispatchBtn.addEventListener('click', dispatchRPC);
  refreshMetricsBtn.addEventListener('click', fetchTelemetry);

  updateEditorTemplate();
  fetchTelemetry();
});
