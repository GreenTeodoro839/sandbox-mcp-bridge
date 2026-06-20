// sandbox-gateway: a tiny MCP server that runs ON the phone (via a KernelSU module)
// and is the SINGLE MCP endpoint Miclaw connects to. It unifies two things into one
// tool list so Miclaw's per-query tool routing can never split them apart:
//
//   - file transfer between the phone and the remote sandbox (push_file / pull_file),
//     handled locally here -- the bytes are read from the phone by path and streamed
//     to the sandbox server, never through the LLM; and
//   - every sandbox tool (exec, run_background, get_job, ...) which is PROXIED to the
//     remote sandbox-mcp server on the user's box.
//
// Connectivity gating: `initialize` performs a real initialize against the remote
// sandbox server. If the box is unreachable (or unconfigured), initialize returns a
// JSON-RPC error so Miclaw's connection FAILS and the half-broken MCP is never loaded
// into the model's context. That same call also fetches the box's instructions, which
// are surfaced (with the file-tool notes appended) to the model.
//
// Plain HTTP on 127.0.0.1 (localhost needs no TLS). Built natively for Android
// (GOOS=android, CGO via the NDK) so it uses the OS DNS resolver + CA store directly.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Long timeout for the data plane (large file transfers); short one for the control
// plane (initialize / tools/list / health), so a dead box fails fast at connect time.
var (
	dataClient = &http.Client{Timeout: 10 * time.Minute}
	ctrlClient = &http.Client{Timeout: 15 * time.Second}
)

var (
	piBase  string // remote sandbox-mcp base URL, e.g. https://sandbox.example.com:40443
	piToken string // bearer token for the remote /mcp and /files endpoints
)

const fileInstructions = "\n\n--- File transfer (phone <-> sandbox), handled by this gateway ---\n" +
	"- push_file(local_path, sandbox, remote_path): copy a file FROM the phone INTO the " +
	"sandbox workspace in ONE step. local_path is the absolute phone path (e.g. " +
	"/sdcard/Download/x.zip); sandbox is the target sandbox name; remote_path is the " +
	"destination in the workspace. It streams the bytes directly -- you do NOT need " +
	"upload_url/download_url, and the sandbox itself CANNOT read phone paths (never pass " +
	"/sdcard/... to exec or fetch_url).\n" +
	"  IMPORTANT: remote_path MUST be a RELATIVE name like \"input.zip\" or \"data/in.csv\", " +
	"NOT an absolute path like \"/tmp/input.zip\". The workspace is /workspace inside the " +
	"container; /tmp, /home, /root are SEPARATE places, so \"/tmp/input.zip\" would land at " +
	"/workspace/tmp/input.zip -- the wrong spot. Use just \"input.zip\" so exec can find it.\n" +
	"- pull_file(sandbox, remote_path, local_path): copy a file FROM the sandbox workspace " +
	"back ONTO the phone (remote_path follows the same relative-name rule; local_path is e.g. " +
	"/sdcard/Download/result.csv). To put a sandbox file onto phone storage use pull_file, " +
	"NOT download_url (download_url is only for giving the USER a browser link)."

// localTools are served by this gateway itself (not proxied). Their JSON schemas are
// returned in tools/list alongside the proxied sandbox tools.
func localTools() []json.RawMessage {
	defs := []string{
		`{"name":"push_file","description":"Copy a file FROM this phone INTO the sandbox workspace, in one step (no upload URL needed). Reads the local file and streams it to the sandbox.","inputSchema":{"type":"object","properties":{"local_path":{"type":"string","description":"Absolute path of the file ON THIS PHONE, e.g. /sdcard/Download/x.zip."},"sandbox":{"type":"string","description":"Name of the target sandbox (same name used with exec/run_background)."},"remote_path":{"type":"string","description":"Destination path in the sandbox workspace, e.g. \"input.zip\" or \"data/in.csv\"."}},"required":["local_path","sandbox","remote_path"]}}`,
		`{"name":"pull_file","description":"Copy a file FROM the sandbox workspace back ONTO this phone, in one step (no download URL needed).","inputSchema":{"type":"object","properties":{"sandbox":{"type":"string","description":"Name of the source sandbox."},"remote_path":{"type":"string","description":"Path of the file in the sandbox workspace, e.g. \"out/result.csv\"."},"local_path":{"type":"string","description":"Absolute path ON THIS PHONE to save the file to, e.g. /sdcard/Download/result.csv."}},"required":["sandbox","remote_path","local_path"]}}`,
	}
	out := make([]json.RawMessage, len(defs))
	for i, d := range defs {
		out[i] = json.RawMessage(d)
	}
	return out
}

func isLocalTool(name string) bool {
	switch name {
	case "push_file", "pull_file":
		return true
	}
	return false
}

// --- JSON-RPC envelope (stateless: single JSON body per request, mirroring the
// remote server's json_response mode, which Miclaw is known to accept) ---

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": rawID(id), "result": result})
}

func writeError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	writeJSON(w, map[string]any{
		"jsonrpc": "2.0",
		"id":      rawID(id),
		"error":   map[string]any{"code": code, "message": msg},
	})
}

// writeGateFailure fails the request with a non-2xx HTTP status. Miclaw's MCP transport
// treats a non-2xx status as a hard connection failure (observed: HTTP 401 -> the server
// is dropped/auto-disabled and never loaded), which is exactly what we want when the
// backend is unreachable or unconfigured -- a 200 + JSON-RPC error would not reliably
// stop the half-broken MCP from loading. 503 = backend down.
func writeGateFailure(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": nil,
		"error": map[string]any{"code": -32000, "message": msg},
	})
}

func rawID(id json.RawMessage) any {
	if len(id) == 0 {
		return nil
	}
	return id
}

func main() {
	addr := getenv("LOCAL_MCP_ADDR", "127.0.0.1:8765")
	piBase = strings.TrimRight(os.Getenv("SANDBOX_BASE_URL"), "/")
	piToken = os.Getenv("SANDBOX_TOKEN")
	inToken := os.Getenv("BRIDGE_TOKEN")

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", handleMCP)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]bool{"ok": true}) })

	var handler http.Handler = mux
	if inToken != "" {
		handler = withAuth(handler, inToken)
	}
	fmt.Printf("sandbox-gateway MCP on http://%s/mcp  (inbound auth=%t, backend=%q)\n",
		addr, inToken != "", piBase)
	if err := http.ListenAndServe(addr, handler); err != nil {
		fmt.Fprintln(os.Stderr, "server error:", err)
		os.Exit(1)
	}
}

// withAuth requires every request to carry "Authorization: Bearer <token>". On Android
// any local app can reach 127.0.0.1, and this server (root) can read/write any file and
// holds the backend token, so inbound auth is required.
func withAuth(next http.Handler, token string) http.Handler {
	expected := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != expected {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// Stateless JSON mode: no server-initiated SSE stream, so GET/DELETE are unused.
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		writeError(w, nil, -32700, "read error: "+err.Error())
		return
	}
	var req rpcReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, nil, -32700, "parse error: "+err.Error())
		return
	}

	// Notifications (no id, e.g. notifications/initialized) get an empty 202.
	if strings.HasPrefix(req.Method, "notifications/") || len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	ctx := r.Context()
	switch req.Method {
	case "initialize":
		handleInitialize(ctx, w, req)
	case "ping":
		writeResult(w, req.ID, map[string]any{})
	case "tools/list":
		handleToolsList(ctx, w, req)
	case "tools/call":
		handleToolsCall(ctx, w, req)
	default:
		// Anything else (resources/*, prompts/*, ...) is forwarded to the backend.
		res, rpcErr, err := piRPC(ctx, ctrlClient, req.Method, req.Params)
		if err != nil {
			writeError(w, req.ID, -32000, "backend error: "+err.Error())
			return
		}
		if rpcErr != nil {
			writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": rawID(req.ID), "error": rpcErr})
			return
		}
		writeResult(w, req.ID, res)
	}
}

func handleInitialize(ctx context.Context, w http.ResponseWriter, req rpcReq) {
	if piBase == "" || piToken == "" {
		writeGateFailure(w, "sandbox gateway not configured: set SANDBOX_BASE_URL and SANDBOX_TOKEN")
		return
	}
	// A real initialize against the backend doubles as the health gate AND fetches the
	// backend's instructions. Any failure -> non-2xx so Miclaw fails the connection and
	// never loads this MCP into the model's context (by design).
	res, rpcErr, err := piRPC(ctx, ctrlClient, "initialize", req.Params)
	if err != nil {
		writeGateFailure(w, "sandbox backend unreachable: "+err.Error())
		return
	}
	if rpcErr != nil {
		writeGateFailure(w, "sandbox backend rejected initialize: "+string(rpcErr))
		return
	}
	var pi struct {
		ProtocolVersion string          `json:"protocolVersion"`
		Capabilities    json.RawMessage `json:"capabilities"`
		Instructions    string          `json:"instructions"`
	}
	_ = json.Unmarshal(res, &pi)
	if pi.ProtocolVersion == "" {
		pi.ProtocolVersion = "2025-06-18"
	}
	writeResult(w, req.ID, map[string]any{
		"protocolVersion": pi.ProtocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": "sandbox-gateway", "version": "0.3.0"},
		"instructions":    pi.Instructions + fileInstructions,
	})
}

func handleToolsList(ctx context.Context, w http.ResponseWriter, req rpcReq) {
	res, rpcErr, err := piRPC(ctx, ctrlClient, "tools/list", req.Params)
	if err != nil {
		// tools/list is part of connection setup -> fail hard (non-2xx) like initialize.
		writeGateFailure(w, "sandbox backend unreachable: "+err.Error())
		return
	}
	if rpcErr != nil {
		writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": rawID(req.ID), "error": rpcErr})
		return
	}
	var piList struct {
		Tools []json.RawMessage `json:"tools"`
	}
	_ = json.Unmarshal(res, &piList)
	tools := append(piList.Tools, localTools()...) // sandbox tools first, then file tools
	writeResult(w, req.ID, map[string]any{"tools": tools})
}

func handleToolsCall(ctx context.Context, w http.ResponseWriter, req rpcReq) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(w, req.ID, -32602, "invalid params: "+err.Error())
		return
	}
	if isLocalTool(p.Name) {
		writeResult(w, req.ID, runLocalTool(ctx, p.Name, p.Arguments))
		return
	}
	// Proxy the tool call to the backend verbatim.
	res, rpcErr, err := piRPC(ctx, dataClient, "tools/call", req.Params)
	if err != nil {
		writeError(w, req.ID, -32000, "backend error: "+err.Error())
		return
	}
	if rpcErr != nil {
		writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": rawID(req.ID), "error": rpcErr})
		return
	}
	writeResult(w, req.ID, res)
}

// piRPC POSTs a single JSON-RPC request to the backend /mcp and returns its result (or
// the JSON-RPC error object). Tolerates an SSE-framed ("data: {...}") response body.
func piRPC(ctx context.Context, client *http.Client, method string, params json.RawMessage) (json.RawMessage, json.RawMessage, error) {
	reqBody := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if len(params) > 0 {
		reqBody["params"] = params
	}
	raw, _ := json.Marshal(reqBody)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, piBase+"/mcp", bytes.NewReader(raw))
	if err != nil {
		return nil, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+piToken)
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	payload := extractJSON(data)
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, nil, fmt.Errorf("bad backend response: %v", err)
	}
	if len(env.Error) > 0 {
		return nil, env.Error, nil
	}
	return env.Result, nil, nil
}

// extractJSON returns the JSON object from a body that is either a plain JSON-RPC
// response or an SSE stream whose "data:" line carries it.
func extractJSON(body []byte) []byte {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		return trimmed
	}
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte("data:")) {
			return bytes.TrimSpace(line[len("data:"):])
		}
	}
	return trimmed
}

// --- local tool execution ---

func runLocalTool(ctx context.Context, name string, args map[string]any) map[string]any {
	switch name {
	case "push_file":
		return pushFile(ctx, str(args, "local_path"), str(args, "sandbox"), str(args, "remote_path"))
	case "pull_file":
		return pullFile(ctx, str(args, "sandbox"), str(args, "remote_path"), str(args, "local_path"))
	}
	return toolErr("unknown tool: " + name)
}

func pushFile(ctx context.Context, localPath, sandbox, remotePath string) map[string]any {
	if localPath == "" || sandbox == "" || remotePath == "" {
		return toolErr("local_path, sandbox and remote_path are required")
	}
	f, err := os.Open(localPath)
	if err != nil {
		return toolErr("open: " + err.Error())
	}
	defer f.Close()
	st, _ := f.Stat()
	u := fmt.Sprintf("%s/files/push?sandbox=%s&path=%s", piBase,
		url.QueryEscape(sandbox), url.QueryEscape(remotePath))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u, f)
	if err != nil {
		return toolErr(err.Error())
	}
	httpReq.Header.Set("Authorization", "Bearer "+piToken)
	httpReq.Header.Set("Content-Type", "application/octet-stream")
	if st != nil {
		httpReq.ContentLength = st.Size()
	}
	resp, err := dataClient.Do(httpReq)
	if err != nil {
		return toolErr("push: " + err.Error())
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 400 {
		return toolErr(fmt.Sprintf("push HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(rb))))
	}
	var size int64
	if st != nil {
		size = st.Size()
	}
	return toolText(fmt.Sprintf("uploaded %s (%d bytes) -> sandbox %s:%s", localPath, size, sandbox, remotePath))
}

func pullFile(ctx context.Context, sandbox, remotePath, localPath string) map[string]any {
	if sandbox == "" || remotePath == "" || localPath == "" {
		return toolErr("sandbox, remote_path and local_path are required")
	}
	u := fmt.Sprintf("%s/files/pull?sandbox=%s&path=%s", piBase,
		url.QueryEscape(sandbox), url.QueryEscape(remotePath))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return toolErr(err.Error())
	}
	httpReq.Header.Set("Authorization", "Bearer "+piToken)
	resp, err := dataClient.Do(httpReq)
	if err != nil {
		return toolErr("pull: " + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return toolErr(fmt.Sprintf("pull HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b))))
	}
	out, err := os.Create(localPath)
	if err != nil {
		return toolErr(err.Error())
	}
	defer out.Close()
	n, err := io.Copy(out, resp.Body)
	if err != nil {
		return toolErr("write: " + err.Error())
	}
	return toolText(fmt.Sprintf("downloaded %d bytes -> %s", n, localPath))
}

// --- helpers ---

func toolText(text string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
}

func toolErr(text string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": true}
}

func str(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
