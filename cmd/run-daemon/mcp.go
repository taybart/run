package main

import (
	"encoding/json"
	"net/http"

	"github.com/taybart/run"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpCallResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

var mcpTools = []mcpTool{
	{
		Name:        "list_scripts",
		Description: "List scripts registered with the run daemon in a given directory",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"dir": map[string]any{"type": "string"},
			},
			"required": []string{"dir"},
		},
	},
	{
		Name:        "restart_script",
		Description: "Restart a script registered with the run daemon",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"dir":    map[string]any{"type": "string"},
				"script": map[string]any{"type": "string"},
				"args":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"required": []string{"dir", "script"},
		},
	},
}

func (s *Server) handleMCP() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json-rpc request", http.StatusBadRequest)
			return
		}

		var result any
		var rpcErr *rpcError

		switch req.Method {
		case "initialize":
			result = mcpInitialize()
		case "notifications/initialized":
			// client ack, nothing to do
		case "tools/list":
			result = map[string]any{"tools": mcpTools}
		case "tools/call":
			result, rpcErr = s.mcpToolsCall(req.Params)
		default:
			rpcErr = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
		}

		if req.ID == nil { // notification: no response body expected
			w.WriteHeader(http.StatusAccepted)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  result,
			Error:   rpcErr,
		})
	}
}

// mcpProtocolVersion is the only version this server actually implements:
// the classic initialize/initialized handshake ("Legacy" in newer tooling,
// as opposed to the handshake-less 2026-07-28 revision). The spec allows
// responding with a version the client didn't ask for; it's on the client
// to decide whether to proceed.
const mcpProtocolVersion = "2025-11-25"

func mcpInitialize() map[string]any {
	return map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "run-daemon",
			"version": "0.1.0",
		},
	}
}

func (s *Server) mcpToolsCall(params json.RawMessage) (*mcpCallResult, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid params"}
	}

	switch p.Name {
	case "list_scripts":
		return s.mcpListScripts(p.Arguments)
	case "restart_script":
		return s.mcpRestartScript(p.Arguments)
	default:
		return nil, &rpcError{Code: -32602, Message: "unknown tool: " + p.Name}
	}
}

func (s *Server) mcpListScripts(args json.RawMessage) (*mcpCallResult, *rpcError) {
	var a struct {
		Dir string `json:"dir"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Dir == "" {
		return nil, &rpcError{Code: -32602, Message: "dir is required"}
	}

	res := make([]run.Process, 0, len(s.targets[a.Dir]))
	for _, t := range s.targets[a.Dir] {
		res = append(res, run.Process{
			Script: t.script,
			Args:   t.args,
		})
	}

	text, err := json.Marshal(res)
	if err != nil {
		return nil, &rpcError{Code: -32603, Message: err.Error()}
	}
	return &mcpCallResult{Content: []mcpContent{{Type: "text", Text: string(text)}}}, nil
}

func (s *Server) mcpRestartScript(args json.RawMessage) (*mcpCallResult, *rpcError) {
	var req run.Process
	if err := json.Unmarshal(args, &req); err != nil || req.Dir == "" || req.Script == "" {
		return nil, &rpcError{Code: -32602, Message: "dir and script are required"}
	}

	reqKey := makeKey(req)
	for k, t := range s.targets[req.Dir] {
		if k == reqKey {
			if err := t.bus.Write(run.Message{Action: run.ActionReset}); err != nil {
				return &mcpCallResult{
					Content: []mcpContent{{Type: "text", Text: "failed to restart " + t.script + ": " + err.Error()}},
					IsError: true,
				}, nil
			}
			return &mcpCallResult{Content: []mcpContent{{Type: "text", Text: "restarted " + t.script}}}, nil
		}
	}
	return &mcpCallResult{
		Content: []mcpContent{{Type: "text", Text: "no matching script found"}},
		IsError: true,
	}, nil
}
