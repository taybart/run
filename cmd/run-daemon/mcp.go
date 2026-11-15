package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

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
		Description: "Restart a script registered with the run daemon. Waits ~1s and reports whether it's still running (or how it exited) along with its output since the restart, so a failed start (ex. a syntax error) shows up here.",
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
	{
		Name:        "get_logs",
		Description: "Get the tail of a script's output (stdout and stderr) registered with the run daemon, without restarting it.",
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

func mcpInitialize() map[string]any {
	return map[string]any{
		"protocolVersion": "2025-11-25",
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
	case "get_logs":
		return s.mcpGetLogs(p.Arguments)
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

	res := s.targets.list(func(dir string) bool { return dir == a.Dir })
	for i := range res {
		res[i].Dir = "" // (it's the one asked for)
	}

	text, err := json.Marshal(res)
	if err != nil {
		return nil, &rpcError{Code: -32603, Message: err.Error()}
	}
	return &mcpCallResult{Content: []mcpContent{{Type: "text", Text: string(text)}}}, nil
}

// describeRestart says how each instance's restart went, with its output (so
// the caller can see it came up, or why it didn't)
func describeRestart(script string, results []run.ResetResult) string {
	var b strings.Builder
	for i, r := range results {
		if i > 0 {
			b.WriteString("\n\n")
		}
		switch {
		case r.Running:
			fmt.Fprintf(&b, "restarted %s, it's running", script)
		case r.OK:
			fmt.Fprintf(&b, "restarted %s, it ran and exited with 0", script)
		default:
			fmt.Fprintf(&b, "restarting %s failed: %s", script, r.Error)
		}
		if r.Output != "" {
			cut := ""
			if r.Truncated {
				cut = " (the end of it)"
			}
			fmt.Fprintf(&b, "\noutput since the restart%s:\n%s", cut, r.Output)
		} else if r.Error == "" || r.ExitCode != 0 {
			b.WriteString("\n(no output since the restart)")
		}
	}
	return b.String()
}

func (s *Server) mcpRestartScript(args json.RawMessage) (*mcpCallResult, *rpcError) {
	var req run.Process
	if err := json.Unmarshal(args, &req); err != nil || req.Dir == "" || req.Script == "" {
		return nil, &rpcError{Code: -32602, Message: "dir and script are required"}
	}

	results, err := s.targets.restart(req)
	if err != nil {
		return &mcpCallResult{Content: []mcpContent{{Type: "text", Text: err.Error()}}, IsError: true}, nil
	}
	return &mcpCallResult{
		Content: []mcpContent{{Type: "text", Text: describeRestart(req.Script, results)}},
		IsError: !allOK(results),
	}, nil
}

// describeLogs says what each instance printed recently (so an agent can see
// it came up, or what it's been doing, without restarting it)
func describeLogs(script string, results []run.LogsResult) string {
	var b strings.Builder
	for i, r := range results {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if r.Error != "" {
			fmt.Fprintf(&b, "couldn't get %s's logs: %s", script, r.Error)
			continue
		}
		if r.Output == "" {
			fmt.Fprintf(&b, "%s hasn't printed anything", script)
			continue
		}
		cut := ""
		if r.Truncated {
			cut = " (the end of it)"
		}
		fmt.Fprintf(&b, "%s's recent output%s:\n%s", script, cut, r.Output)
	}
	return b.String()
}

func (s *Server) mcpGetLogs(args json.RawMessage) (*mcpCallResult, *rpcError) {
	var req run.Process
	if err := json.Unmarshal(args, &req); err != nil || req.Dir == "" || req.Script == "" {
		return nil, &rpcError{Code: -32602, Message: "dir and script are required"}
	}

	results, err := s.targets.logs(req)
	if err != nil {
		return &mcpCallResult{Content: []mcpContent{{Type: "text", Text: err.Error()}}, IsError: true}, nil
	}
	return &mcpCallResult{
		Content: []mcpContent{{Type: "text", Text: describeLogs(req.Script, results)}},
	}, nil
}
