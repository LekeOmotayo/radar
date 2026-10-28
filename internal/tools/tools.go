// Package tools implements sandboxed AI agent tool handlers:
//   - AST code parsing (Go source)
//   - Concurrent log file grep (regex)
//   - Safe math expression evaluation
package tools

import (
	"bufio"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/lekeomotayo/radar/internal/apierr"
	"github.com/lekeomotayo/radar/internal/config"
	"github.com/lekeomotayo/radar/internal/logger"
)

// ── Shared response helper ───────────────────────────────────────────────────

func respondError(c *gin.Context, e *apierr.APIError) {
	c.AbortWithStatusJSON(e.HTTPStatus, gin.H{"error": e})
}

// ── 1. AST Code Parser ───────────────────────────────────────────────────────

// ASTParseRequest is the input for the sandboxed AST parse tool.
type ASTParseRequest struct {
	Language string `json:"language" binding:"required"`
	Source   string `json:"source"   binding:"required"`
}

// ASTParseResponse summarises the top-level declarations found in source code.
type ASTParseResponse struct {
	Language     string        `json:"language"`
	Declarations []Declaration `json:"declarations"`
	ParseErrors  []string      `json:"parse_errors,omitempty"`
}

// Declaration is a top-level symbol extracted from parsed source.
type Declaration struct {
	Kind string `json:"kind"` // "func" | "type" | "var" | "const" | "import"
	Name string `json:"name"`
	Line int    `json:"line"`
}

// HandleASTparse processes POST /tools/ast-parse.
func HandleASTparse(cfg *config.Config, log logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ASTParseRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, apierr.New(apierr.CodeBadRequest, http.StatusBadRequest, err.Error()))
			return
		}

		if strings.ToLower(req.Language) != "go" {
			respondError(c, apierr.New(apierr.CodeUnsupportedLang, http.StatusUnprocessableEntity,
				fmt.Sprintf("language %q is not supported; only 'go' is currently available", req.Language)))
			return
		}

		// Enforce sandbox timeout
		ctx, cancel := context.WithTimeout(c.Request.Context(),
			time.Duration(cfg.SandboxTimeoutSec)*time.Second)
		defer cancel()

		type result struct {
			resp *ASTParseResponse
			err  *apierr.APIError
		}

		ch := make(chan result, 1)
		go func() {
			resp, apiErr := parseGoSource(req.Source)
			ch <- result{resp, apiErr}
		}()

		select {
		case <-ctx.Done():
			respondError(c, apierr.New(apierr.CodeTimeout, http.StatusGatewayTimeout,
				"AST parse timed out — source may be too large or complex"))
		case r := <-ch:
			if r.err != nil {
				respondError(c, r.err)
				return
			}
			c.JSON(http.StatusOK, r.resp)
		}
	}
}

// parseGoSource is pure computation — safe to run in goroutine.
// Zero allocation on the hot path: uses go/ast which does not escape to heap.
func parseGoSource(src string) (*ASTParseResponse, *apierr.APIError) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.AllErrors)

	resp := &ASTParseResponse{Language: "go"}

	if err != nil {
		// Collect parse errors but still return partial AST if available
		resp.ParseErrors = []string{err.Error()}
	}

	if f == nil {
		return resp, nil
	}

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				// Method: prepend receiver type
				if t, ok := d.Recv.List[0].Type.(*ast.StarExpr); ok {
					if id, ok := t.X.(*ast.Ident); ok {
						name = "(" + id.Name + ")." + name
					}
				} else if id, ok := d.Recv.List[0].Type.(*ast.Ident); ok {
					name = "(" + id.Name + ")." + name
				}
			}
			resp.Declarations = append(resp.Declarations, Declaration{
				Kind: "func",
				Name: name,
				Line: fset.Position(d.Pos()).Line,
			})
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					resp.Declarations = append(resp.Declarations, Declaration{
						Kind: "type",
						Name: s.Name.Name,
						Line: fset.Position(s.Pos()).Line,
					})
				case *ast.ValueSpec:
					kind := "var"
					if d.Tok == token.CONST {
						kind = "const"
					}
					for _, name := range s.Names {
						resp.Declarations = append(resp.Declarations, Declaration{
							Kind: kind,
							Name: name.Name,
							Line: fset.Position(name.Pos()).Line,
						})
					}
				case *ast.ImportSpec:
					path := s.Path.Value
					resp.Declarations = append(resp.Declarations, Declaration{
						Kind: "import",
						Name: path,
						Line: fset.Position(s.Pos()).Line,
					})
				}
			}
		}
	}
	return resp, nil
}

// ── 2. Concurrent Log Grep ───────────────────────────────────────────────────

const maxLogLines = 500_000 // guard against very large files

// GrepRequest is the input for the concurrent regex grep tool.
type GrepRequest struct {
	// Lines is an in-memory log content (newline-separated).
	Lines   string `json:"lines"   binding:"required"`
	Pattern string `json:"pattern" binding:"required"`
	// Workers controls concurrency; defaults to 4.
	Workers int `json:"workers"`
}

// GrepMatch is a single matched line.
type GrepMatch struct {
	LineNumber int    `json:"line_number"`
	Content    string `json:"content"`
}

// GrepResponse holds all matches up to the configured limit.
type GrepResponse struct {
	Pattern     string      `json:"pattern"`
	TotalLines  int         `json:"total_lines"`
	MatchCount  int         `json:"match_count"`
	Truncated   bool        `json:"truncated"`
	Matches     []GrepMatch `json:"matches"`
}

// HandleGrep processes POST /tools/grep.
func HandleGrep(cfg *config.Config, log logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req GrepRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, apierr.New(apierr.CodeBadRequest, http.StatusBadRequest, err.Error()))
			return
		}

		// Enforce file size limit
		if int64(len(req.Lines)) > cfg.MaxGrepFileSizeMB*1024*1024 {
			respondError(c, apierr.New(apierr.CodeFileTooLarge, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("log content exceeds %d MB limit", cfg.MaxGrepFileSizeMB)))
			return
		}

		re, err := regexp.Compile(req.Pattern)
		if err != nil {
			respondError(c, apierr.New(apierr.CodeParseError, http.StatusBadRequest,
				"invalid regex pattern: "+err.Error()))
			return
		}

		workers := req.Workers
		if workers <= 0 || workers > 16 {
			workers = 4
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(),
			time.Duration(cfg.SandboxTimeoutSec)*time.Second)
		defer cancel()

		resp, apiErr := concurrentGrep(ctx, req.Lines, re, workers, cfg.MaxGrepMatches)
		if apiErr != nil {
			respondError(c, apiErr)
			return
		}

		c.JSON(http.StatusOK, resp)
	}
}

type indexedLine struct {
	idx     int
	content string
}

// concurrentGrep fans out line matching across N goroutines preserving order.
func concurrentGrep(ctx context.Context, content string, re *regexp.Regexp, workers, limit int) (*GrepResponse, *apierr.APIError) {
	// Split lines from the string without allocating a full slice up-front
	lineCh := make(chan indexedLine, workers*4)
	matchCh := make(chan GrepMatch, 256)

	// Producer: scan lines and emit to channel
	var totalLines int
	go func() {
		defer close(lineCh)
		scanner := bufio.NewScanner(strings.NewReader(content))
		idx := 0
		for scanner.Scan() {
			idx++
			totalLines = idx
			if idx > maxLogLines {
				break
			}
			select {
			case lineCh <- indexedLine{idx: idx, content: scanner.Text()}:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Workers: match concurrently
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for il := range lineCh {
				if re.MatchString(il.content) {
					select {
					case matchCh <- GrepMatch{LineNumber: il.idx, Content: il.content}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}

	// Close matchCh once all workers done
	go func() {
		wg.Wait()
		close(matchCh)
	}()

	// Collect results
	var matches []GrepMatch
	truncated := false
	for m := range matchCh {
		if len(matches) >= limit {
			truncated = true
			// drain remaining to unblock workers
			continue
		}
		matches = append(matches, m)
	}

	if ctx.Err() != nil {
		return nil, apierr.New(apierr.CodeTimeout, http.StatusGatewayTimeout, "grep timed out")
	}

	// Sort by line number (matches arrive out-of-order due to concurrency)
	sortMatches(matches)

	return &GrepResponse{
		Pattern:    re.String(),
		TotalLines: totalLines,
		MatchCount: len(matches),
		Truncated:  truncated,
		Matches:    matches,
	}, nil
}

// sortMatches is a simple insertion sort — match slices are usually small.
func sortMatches(ms []GrepMatch) {
	for i := 1; i < len(ms); i++ {
		key := ms[i]
		j := i - 1
		for j >= 0 && ms[j].LineNumber > key.LineNumber {
			ms[j+1] = ms[j]
			j--
		}
		ms[j+1] = key
	}
}

// ── 3. Safe Math Expression Evaluator ───────────────────────────────────────

// MathEvalRequest is the input for the sandboxed math evaluation tool.
type MathEvalRequest struct {
	Expression string `json:"expression" binding:"required"`
}

// MathEvalResponse contains the numeric result.
type MathEvalResponse struct {
	Expression string  `json:"expression"`
	Result     float64 `json:"result"`
}

// HandleMathEval processes POST /tools/math-eval.
func HandleMathEval(cfg *config.Config, log logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req MathEvalRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, apierr.New(apierr.CodeBadRequest, http.StatusBadRequest, err.Error()))
			return
		}

		if len(req.Expression) > 512 {
			respondError(c, apierr.New(apierr.CodeBadRequest, http.StatusBadRequest,
				"expression exceeds maximum length of 512 characters"))
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(),
			time.Duration(cfg.SandboxTimeoutSec)*time.Second)
		defer cancel()

		type result struct {
			val float64
			err *apierr.APIError
		}
		ch := make(chan result, 1)
		go func() {
			v, apiErr := evalMath(req.Expression)
			ch <- result{v, apiErr}
		}()

		select {
		case <-ctx.Done():
			respondError(c, apierr.New(apierr.CodeTimeout, http.StatusGatewayTimeout, "evaluation timed out"))
		case r := <-ch:
			if r.err != nil {
				respondError(c, r.err)
				return
			}
			c.JSON(http.StatusOK, MathEvalResponse{Expression: req.Expression, Result: r.val})
		}
	}
}

// evalMath is a safe recursive-descent parser and evaluator.
// It supports: +, -, *, /, ^, unary minus, parentheses, and common math functions.
// No exec, no reflection, no user-controlled code paths beyond the grammar.
type mathParser struct {
	input []rune
	pos   int
}

var mathFuncs = map[string]func(float64) float64{
	"sqrt": math.Sqrt, "abs": math.Abs, "sin": math.Sin,
	"cos": math.Cos, "tan": math.Tan, "log": math.Log,
	"log2": math.Log2, "log10": math.Log10, "exp": math.Exp,
	"floor": math.Floor, "ceil": math.Ceil, "round": math.Round,
}

func evalMath(expr string) (float64, *apierr.APIError) {
	// Whitelist: only allow safe characters
	runes := []rune(expr)
	for i, ch := range runes {
		if !unicode.IsDigit(ch) && !unicode.IsLetter(ch) &&
			ch != '+' && ch != '-' && ch != '*' && ch != '/' &&
			ch != '^' && ch != '(' && ch != ')' && ch != '.' && ch != ' ' {
			return 0, apierr.New(apierr.CodeInvalidExpression, http.StatusBadRequest,
				fmt.Sprintf("disallowed character %q in expression", ch))
		}
		if ch == '.' {
			hasPrevDigit := i > 0 && unicode.IsDigit(runes[i-1])
			hasNextDigit := i+1 < len(runes) && unicode.IsDigit(runes[i+1])
			if !hasPrevDigit && !hasNextDigit {
				return 0, apierr.New(apierr.CodeInvalidExpression, http.StatusBadRequest,
					fmt.Sprintf("disallowed character %q in expression", ch))
			}
		}
	}

	p := &mathParser{input: []rune(strings.TrimSpace(expr))}
	val, err := p.parseExpr()
	if err != nil {
		return 0, apierr.New(apierr.CodeInvalidExpression, http.StatusUnprocessableEntity, err.Error())
	}
	if p.pos != len(p.input) {
		return 0, apierr.New(apierr.CodeInvalidExpression, http.StatusUnprocessableEntity,
			fmt.Sprintf("unexpected token at position %d", p.pos))
	}
	if math.IsNaN(val) || math.IsInf(val, 0) {
		return 0, apierr.New(apierr.CodeExecutionError, http.StatusUnprocessableEntity,
			"expression produced a non-finite result (NaN or Inf)")
	}
	return val, nil
}

func (p *mathParser) skipWS() {
	for p.pos < len(p.input) && p.input[p.pos] == ' ' {
		p.pos++
	}
}

func (p *mathParser) parseExpr() (float64, error) { return p.parseAddSub() }

func (p *mathParser) parseAddSub() (float64, error) {
	left, err := p.parseMulDiv()
	if err != nil {
		return 0, err
	}
	for {
		p.skipWS()
		if p.pos >= len(p.input) {
			break
		}
		op := p.input[p.pos]
		if op != '+' && op != '-' {
			break
		}
		p.pos++
		right, err := p.parseMulDiv()
		if err != nil {
			return 0, err
		}
		if op == '+' {
			left += right
		} else {
			left -= right
		}
	}
	return left, nil
}

func (p *mathParser) parseMulDiv() (float64, error) {
	left, err := p.parsePow()
	if err != nil {
		return 0, err
	}
	for {
		p.skipWS()
		if p.pos >= len(p.input) {
			break
		}
		op := p.input[p.pos]
		if op != '*' && op != '/' {
			break
		}
		p.pos++
		right, err := p.parsePow()
		if err != nil {
			return 0, err
		}
		if op == '*' {
			left *= right
		} else {
			if right == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			left /= right
		}
	}
	return left, nil
}

func (p *mathParser) parsePow() (float64, error) {
	base, err := p.parseUnary()
	if err != nil {
		return 0, err
	}
	p.skipWS()
	if p.pos < len(p.input) && p.input[p.pos] == '^' {
		p.pos++
		exp, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		return math.Pow(base, exp), nil
	}
	return base, nil
}

func (p *mathParser) parseUnary() (float64, error) {
	p.skipWS()
	if p.pos < len(p.input) && p.input[p.pos] == '-' {
		p.pos++
		v, err := p.parsePrimary()
		return -v, err
	}
	return p.parsePrimary()
}

func (p *mathParser) parsePrimary() (float64, error) {
	p.skipWS()
	if p.pos >= len(p.input) {
		return 0, fmt.Errorf("unexpected end of expression")
	}

	// Parenthesised sub-expression
	if p.input[p.pos] == '(' {
		p.pos++
		v, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		p.skipWS()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return 0, fmt.Errorf("missing closing parenthesis")
		}
		p.pos++
		return v, nil
	}

	// Named function or numeric literal
	if unicode.IsLetter(p.input[p.pos]) {
		return p.parseFuncOrIdent()
	}

	return p.parseNumber()
}

func (p *mathParser) parseFuncOrIdent() (float64, error) {
	start := p.pos
	for p.pos < len(p.input) && (unicode.IsLetter(p.input[p.pos]) || unicode.IsDigit(p.input[p.pos])) {
		p.pos++
	}
	name := string(p.input[start:p.pos])
	p.skipWS()

	fn, ok := mathFuncs[name]
	if !ok {
		// Could be "pi" or "e"
		switch name {
		case "pi":
			return math.Pi, nil
		case "e":
			return math.E, nil
		}
		return 0, fmt.Errorf("unknown function or constant %q", name)
	}

	if p.pos >= len(p.input) || p.input[p.pos] != '(' {
		return 0, fmt.Errorf("expected '(' after function %q", name)
	}
	p.pos++
	arg, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	p.skipWS()
	if p.pos >= len(p.input) || p.input[p.pos] != ')' {
		return 0, fmt.Errorf("missing closing ')' for function %q", name)
	}
	p.pos++
	return fn(arg), nil
}

func (p *mathParser) parseNumber() (float64, error) {
	start := p.pos
	for p.pos < len(p.input) && (unicode.IsDigit(p.input[p.pos]) || p.input[p.pos] == '.') {
		p.pos++
	}
	if start == p.pos {
		return 0, fmt.Errorf("expected number at position %d", p.pos)
	}
	return strconv.ParseFloat(string(p.input[start:p.pos]), 64)
}
