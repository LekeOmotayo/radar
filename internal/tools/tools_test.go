// Package tools_test provides concurrent race-safe unit tests for all tool handlers.
// Run with: go test -race ./internal/tools/...
package tools_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lekeomotayo/radar/internal/config"
	"github.com/lekeomotayo/radar/internal/tools"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func testCfg() *config.Config {
	return &config.Config{
		SandboxTimeoutSec: 5,
		MaxGrepFileSizeMB: 10,
		MaxGrepMatches:    500,
	}
}

func newRouter(cfg *config.Config) *gin.Engine {
	r := gin.New()
	r.POST("/tools/ast-parse", tools.HandleASTparse(cfg, nil))
	r.POST("/tools/grep", tools.HandleGrep(cfg, nil))
	r.POST("/tools/math-eval", tools.HandleMathEval(cfg, nil))
	return r
}

func postJSON(r *gin.Engine, path string, body interface{}) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ── AST Parse Tests ──────────────────────────────────────────────────────────

func TestASTparse_ValidGoSource(t *testing.T) {
	r := newRouter(testCfg())

	src := `package main
import "fmt"
func Hello(name string) string { return "Hello, " + name }
type Greeter struct{ Prefix string }
var DefaultPrefix = "Hello"
const MaxLen = 100
`
	w := postJSON(r, "/tools/ast-parse", map[string]string{
		"language": "go",
		"source":   src,
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp tools.ASTParseResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(resp.Declarations) == 0 {
		t.Error("expected non-empty declarations")
	}

	kinds := map[string]bool{}
	for _, d := range resp.Declarations {
		kinds[d.Kind] = true
	}
	for _, want := range []string{"func", "type", "var", "const", "import"} {
		if !kinds[want] {
			t.Errorf("expected declaration kind %q", want)
		}
	}
}

func TestASTparse_UnsupportedLanguage(t *testing.T) {
	r := newRouter(testCfg())
	w := postJSON(r, "/tools/ast-parse", map[string]string{
		"language": "python",
		"source":   "print('hello')",
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", w.Code)
	}
}

func TestASTparse_SyntaxError_PartialResult(t *testing.T) {
	r := newRouter(testCfg())
	w := postJSON(r, "/tools/ast-parse", map[string]string{
		"language": "go",
		"source":   `package main; func Broken( { }`, // syntax error
	})
	// Should still return 200 with parse_errors populated
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for partial AST, got %d", w.Code)
	}
	var resp tools.ASTParseResponse
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	if len(resp.ParseErrors) == 0 {
		t.Error("expected parse_errors for broken source")
	}
}

// Concurrent race test: 20 goroutines calling AST parse simultaneously.
func TestASTparse_Concurrent(t *testing.T) {
	r := newRouter(testCfg())
	src := `package main; func Add(a, b int) int { return a + b }`

	const goroutines = 20
	var wg sync.WaitGroup
	errors := make([]string, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			w := postJSON(r, "/tools/ast-parse", map[string]string{
				"language": "go",
				"source":   src,
			})
			if w.Code != http.StatusOK {
				errors[idx] = w.Body.String()
			}
		}(i)
	}

	wg.Wait()

	for i, e := range errors {
		if e != "" {
			t.Errorf("goroutine %d failed: %s", i, e)
		}
	}
}

// ── Grep Tests ───────────────────────────────────────────────────────────────

func buildLogLines(n int, matchEvery int) string {
	var sb strings.Builder
	for i := 1; i <= n; i++ {
		if i%matchEvery == 0 {
			sb.WriteString("ERROR: something went wrong on line ")
		} else {
			sb.WriteString("INFO: everything is fine on line ")
		}
		sb.WriteString(strings.Repeat("x", 20))
		sb.WriteByte('\n')
	}
	return sb.String()
}

func TestGrep_BasicMatch(t *testing.T) {
	r := newRouter(testCfg())
	lines := "INFO: server started\nERROR: connection refused\nINFO: retry attempt 1\n"
	w := postJSON(r, "/tools/grep", map[string]interface{}{
		"lines":   lines,
		"pattern": `ERROR`,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp tools.GrepResponse
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	if resp.MatchCount != 1 {
		t.Errorf("expected 1 match, got %d", resp.MatchCount)
	}
	if resp.Matches[0].LineNumber != 2 {
		t.Errorf("expected match on line 2, got %d", resp.Matches[0].LineNumber)
	}
}

func TestGrep_InvalidRegex(t *testing.T) {
	r := newRouter(testCfg())
	w := postJSON(r, "/tools/grep", map[string]interface{}{
		"lines":   "some log",
		"pattern": `[invalid`,
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid regex, got %d", w.Code)
	}
}

func TestGrep_NoMatches(t *testing.T) {
	r := newRouter(testCfg())
	w := postJSON(r, "/tools/grep", map[string]interface{}{
		"lines":   "INFO: all good\nINFO: still good\n",
		"pattern": `ERROR`,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp tools.GrepResponse
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	if resp.MatchCount != 0 {
		t.Errorf("expected 0 matches, got %d", resp.MatchCount)
	}
}

func TestGrep_LargeInputConcurrent(t *testing.T) {
	r := newRouter(testCfg())
	// Build 10k lines with every 10th being an error
	lines := buildLogLines(10_000, 10)

	const goroutines = 10
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := postJSON(r, "/tools/grep", map[string]interface{}{
				"lines":   lines,
				"pattern": `ERROR`,
				"workers": 4,
			})
			if w.Code != http.StatusOK {
				t.Errorf("concurrent grep failed: %s", w.Body.String())
			}
		}()
	}
	wg.Wait()
}

func TestGrep_OrderPreserved(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		sb.WriteString("MATCH line\n")
	}

	r := newRouter(testCfg())
	w := postJSON(r, "/tools/grep", map[string]interface{}{
		"lines":   sb.String(),
		"pattern": `MATCH`,
		"workers": 8,
	})

	var resp tools.GrepResponse
	json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
	for i := 1; i < len(resp.Matches); i++ {
		if resp.Matches[i].LineNumber < resp.Matches[i-1].LineNumber {
			t.Errorf("matches out of order at index %d: %d < %d",
				i, resp.Matches[i].LineNumber, resp.Matches[i-1].LineNumber)
		}
	}
}

// ── Math Eval Tests ──────────────────────────────────────────────────────────

type mathCase struct {
	expr   string
	result float64
	errMsg string
}

var mathCases = []mathCase{
	{expr: "2 + 3", result: 5},
	{expr: "10 - 4 * 2", result: 2},
	{expr: "(10 - 4) * 2", result: 12},
	{expr: "2 ^ 10", result: 1024},
	{expr: "sqrt(16)", result: 4},
	{expr: "log(1)", result: 0},
	{expr: "pi", result: 3.141592653589793},
	{expr: "-5 + 3", result: -2},
	{expr: "abs(-42)", result: 42},
	{expr: "floor(3.7)", result: 3},
	{expr: "ceil(3.2)", result: 4},
}

func TestMathEval_ValidExpressions(t *testing.T) {
	r := newRouter(testCfg())

	for _, tc := range mathCases {
		tc := tc
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()
			w := postJSON(r, "/tools/math-eval", map[string]string{"expression": tc.expr})
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			var resp tools.MathEvalResponse
			json.Unmarshal(w.Body.Bytes(), &resp) //nolint:errcheck
			const tolerance = 1e-9
			diff := resp.Result - tc.result
			if diff < -tolerance || diff > tolerance {
				t.Errorf("expr=%q: want %v, got %v", tc.expr, tc.result, resp.Result)
			}
		})
	}
}

func TestMathEval_DivisionByZero(t *testing.T) {
	r := newRouter(testCfg())
	w := postJSON(r, "/tools/math-eval", map[string]string{"expression": "1 / 0"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for division by zero, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMathEval_DisallowedCharacters(t *testing.T) {
	r := newRouter(testCfg())
	// Semicolons and backticks should be rejected at the whitelist gate
	for _, expr := range []string{"1; DROP TABLE", "os.Exit(0)", "`id`"} {
		w := postJSON(r, "/tools/math-eval", map[string]string{"expression": expr})
		if w.Code != http.StatusBadRequest {
			t.Errorf("expr=%q: expected 400, got %d", expr, w.Code)
		}
	}
}

func TestMathEval_Concurrent(t *testing.T) {
	r := newRouter(testCfg())
	const goroutines = 50
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := postJSON(r, "/tools/math-eval", map[string]string{
				"expression": "sqrt(2) ^ 2",
			})
			if w.Code != http.StatusOK {
				t.Errorf("concurrent math-eval failed: %s", w.Body.String())
			}
		}()
	}
	wg.Wait()
}

func TestMathEval_UnknownFunction(t *testing.T) {
	r := newRouter(testCfg())
	w := postJSON(r, "/tools/math-eval", map[string]string{"expression": "evil(42)"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for unknown function, got %d", w.Code)
	}
}
