// local-file-bridge: a tiny MCP server meant to run ON the phone (via a KernelSU
// module). It gives the phone assistant (Miclaw) the ability to move LOCAL files
// to/from URLs -- e.g. PUT a phone file to a sandbox upload_url, or save a
// download_url result onto the phone. Plain HTTP on 127.0.0.1 (localhost needs no
// TLS). Built natively for Android (GOOS=android, CGO via the NDK) so it uses the
// OS DNS resolver and CA store directly -- no bundled certs or custom DNS.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Built for Android (GOOS=android), so Go resolves DNS via the OS resolver and
// trusts the OS CA store natively -- no bundled certs or custom DNS needed.
var httpClient = &http.Client{Timeout: 10 * time.Minute}

func main() {
	addr := getenv("LOCAL_MCP_ADDR", "127.0.0.1:8765")

	s := server.NewMCPServer("local-file-bridge", "0.1.0",
		server.WithToolCapabilities(true),
		server.WithInstructions(
			"This server runs ON the user's phone and ONLY moves files between the phone "+
				"and a URL. It has NO shell and CANNOT run commands, process data, or use "+
				"Linux -- for any command / shell / sandbox / 沙箱 task use the separate Linux "+
				"sandbox MCP server, never this one. Use push_file(local_path, url) to upload a "+
				"phone file to a sandbox upload_url, and pull_file(url, local_path) to save a "+
				"sandbox download_url result onto the phone."),
	)

	s.AddTool(mcp.NewTool("push_file",
		mcp.WithDescription("Upload a LOCAL file from this device to a URL via HTTP PUT (e.g. to a sandbox upload_url)."),
		mcp.WithString("local_path", mcp.Required(), mcp.Description("Absolute path of the local file on this device.")),
		mcp.WithString("url", mcp.Required(), mcp.Description("Destination URL to PUT the bytes to.")),
	), handlePushFile)

	s.AddTool(mcp.NewTool("pull_file",
		mcp.WithDescription("Download a URL to a LOCAL file on this device via HTTP GET (e.g. save a sandbox download_url result)."),
		mcp.WithString("url", mcp.Required(), mcp.Description("Source URL to download.")),
		mcp.WithString("local_path", mcp.Required(), mcp.Description("Absolute local path to save the file to.")),
	), handlePullFile)

	s.AddTool(mcp.NewTool("list_files",
		mcp.WithDescription("List entries in a local directory on this device."),
		mcp.WithString("dir", mcp.Required(), mcp.Description("Absolute directory path.")),
	), handleListFiles)

	s.AddTool(mcp.NewTool("read_text",
		mcp.WithDescription("Read a small local text file (<=200KB) from this device."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Absolute file path.")),
	), handleReadText)

	token := os.Getenv("BRIDGE_TOKEN")
	var handler http.Handler = server.NewStreamableHTTPServer(s)
	if token != "" {
		handler = withAuth(handler, token)
	}
	fmt.Printf("local-file-bridge MCP listening on http://%s/mcp (auth=%t)\n", addr, token != "")
	if err := http.ListenAndServe(addr, handler); err != nil {
		fmt.Fprintln(os.Stderr, "server error:", err)
		os.Exit(1)
	}
}

// withAuth requires every request to carry "Authorization: Bearer <token>".
// Needed because on Android any local app can reach 127.0.0.1:8765, and this
// server (running as root) can read/write arbitrary files.
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

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func handlePushFile(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	localPath, _ := args["local_path"].(string)
	url, _ := args["url"].(string)
	if localPath == "" || url == "" {
		return mcp.NewToolResultError("local_path and url are required"), nil
	}
	f, err := os.Open(localPath)
	if err != nil {
		return mcp.NewToolResultError("open: " + err.Error()), nil
	}
	defer f.Close()
	st, _ := f.Stat()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, url, f)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if st != nil {
		httpReq.ContentLength = st.Size()
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return mcp.NewToolResultError("put: " + err.Error()), nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var size int64
	if st != nil {
		size = st.Size()
	}
	return mcp.NewToolResultText(fmt.Sprintf("uploaded %s (%d bytes) -> HTTP %d\n%s",
		localPath, size, resp.StatusCode, strings.TrimSpace(string(body)))), nil
}

func handlePullFile(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	url, _ := args["url"].(string)
	localPath, _ := args["local_path"].(string)
	if url == "" || localPath == "" {
		return mcp.NewToolResultError("url and local_path are required"), nil
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return mcp.NewToolResultError("get: " + err.Error()), nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return mcp.NewToolResultError(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))), nil
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out, err := os.Create(localPath)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer out.Close()
	n, err := io.Copy(out, resp.Body)
	if err != nil {
		return mcp.NewToolResultError("write: " + err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("downloaded %d bytes -> %s", n, localPath)), nil
}

func handleListFiles(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	dir, _ := args["dir"].(string)
	if dir == "" {
		return mcp.NewToolResultError("dir is required"), nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	var lines []string
	for _, e := range entries {
		kind := "file"
		var sz int64
		if e.IsDir() {
			kind = "dir"
		} else if info, err := e.Info(); err == nil {
			sz = info.Size()
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%d", kind, e.Name(), sz))
	}
	return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
}

func handleReadText(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	path, _ := args["path"].(string)
	if path == "" {
		return mcp.NewToolResultError("path is required"), nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if st.Size() > 200*1024 {
		return mcp.NewToolResultError(fmt.Sprintf("file too large (%d bytes); use pull/push instead", st.Size())), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
