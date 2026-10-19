// Package jsonrpc implements a JSON-RPC 2.0 endpoint that multiplexes
// all tool calls through a single POST /rpc route.
package jsonrpc

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lekeomotayo/radar/internal/apierr"
	"github.com/lekeomotayo/radar/internal/config"
	"github.com/lekeomotayo/radar/internal/tools"
)

// Request is a JSON-RPC 2.0 request envelope.
type Request struct {
	JSONRPC string      `json:"jsonrpc" binding:"required"`
	ID      interface{} `json:"id"`
	Method  string      `json:"method" binding:"required"`
	Params  interface{} `json:"params"`
}

// Response is a JSON-RPC 2.0 response envelope.
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError follows the JSON-RPC 2.0 error object specification.
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)

// methodMap maps JSON-RPC method names to radar tool codes.
var methodMap = map[string]string{
	"tools.ast_parse": "ast-parse",
	"tools.grep":      "grep",
	"tools.math_eval": "math-eval",
}

// Handler returns a Gin handler for the JSON-RPC 2.0 /rpc endpoint.
func Handler(cfg *config.Config) gin.HandlerFunc {
	astHandler := tools.HandleASTparse(cfg, nil)
	grepHandler := tools.HandleGrep(cfg, nil)
	mathHandler := tools.HandleMathEval(cfg, nil)

	dispatch := map[string]gin.HandlerFunc{
		"ast-parse": astHandler,
		"grep":      grepHandler,
		"math-eval": mathHandler,
	}

	return func(c *gin.Context) {
		var req Request
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusOK, errorResponse(nil, rpcParseError, "parse error: "+err.Error()))
			return
		}

		if req.JSONRPC != "2.0" {
			c.JSON(http.StatusOK, errorResponse(req.ID, rpcInvalidRequest, "jsonrpc must be \"2.0\""))
			return
		}

		tool, ok := methodMap[req.Method]
		if !ok {
			c.JSON(http.StatusOK, errorResponse(req.ID, rpcMethodNotFound,
				"method not found: "+req.Method))
			return
		}

		handler, ok := dispatch[tool]
		if !ok {
			c.JSON(http.StatusOK, errorResponse(req.ID, rpcInternalError, apierr.ErrInternalError.Message))
			return
		}

		// Swap the body so the underlying tool handler can bind it normally.
		// We re-encode only the params portion into the request body.
		c.Set("jsonrpc_id", req.ID)
		c.Set("jsonrpc_params", req.Params)

		handler(c)

		// Wrap tool handler's JSON response in RPC envelope
		if !c.IsAborted() {
			// The tool handler already wrote the response; nothing more to do.
		}
	}
}

func errorResponse(id interface{}, code int, msg string) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: msg},
	}
}
