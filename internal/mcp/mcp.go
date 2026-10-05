// Package mcp mengimplementasikan client Model Context Protocol (MCP):
// server lokal via stdio (JSON-RPC newline-delimited) dan remote via HTTP.
// Tool dari server MCP didaftarkan ke tool registry agen dengan awalan
// mcp__<server>__<tool>.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Config konfigurasi satu server MCP dari jenderal.jsonc.
type Config struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"` // local | remote
	Command []string          `json:"command"`
	Env     map[string]string `json:"env"`
	URL     string            `json:"url"`
	Enabled *bool             `json:"enabled,omitempty"`
}

// EnabledOK default true bila tidak diset.
func (c Config) EnabledOK() bool {
	return c.Enabled == nil || *c.Enabled
}

// ToolDef deskripsi tool dari server MCP.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// Client koneksi ke satu server MCP.
type Client struct {
	name string

	// stdio
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	// remote http
	url string
	hc  *syncHTTPClient

	mu      sync.Mutex
	nextID  int
	pending map[int64]chan rpcResponse
	tools   []ToolDef
	closed  bool
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// StartLocal menjalankan server MCP lokal (stdio) dan melakukan handshake.
func StartLocal(ctx context.Context, name string, command []string, env map[string]string) (*Client, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("mcp %s: command kosong", name)
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard // hindari bocor ke UI; bisa diaktifkan via debug
	if env != nil {
		cmd.Env = append(cmd.Environ(), flattenEnv(env)...)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp %s: gagal menjalankan %v: %w", name, command, err)
	}
	c := &Client{
		name:    name,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReaderSize(stdout, 1<<20),
		pending: map[int64]chan rpcResponse{},
	}
	go c.readLoop()
	if err := c.initialize(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// ConnectRemote membuka koneksi ke server MCP remote via HTTP POST.
func ConnectRemote(ctx context.Context, name, url string) (*Client, error) {
	if url == "" {
		return nil, fmt.Errorf("mcp %s: url kosong", name)
	}
	c := &Client{name: name, url: url, pending: map[int64]chan rpcResponse{}}
	c.hc = newHTTPClient(url)
	if err := c.initialize(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func flattenEnv(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// initialize melakukan handshake MCP.
func (c *Client) initialize(ctx context.Context) error {
	params, _ := json.Marshal(map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "jenderalcode", "version": "1.0"},
	})
	if _, err := c.call(ctx, "initialize", params, 30*time.Second); err != nil {
		return fmt.Errorf("mcp %s: initialize: %w", c.name, err)
	}
	c.notify("notifications/initialized", nil)
	return nil
}

// ListTools mengambil daftar tool dari server.
func (c *Client) ListTools(ctx context.Context) ([]ToolDef, error) {
	res, err := c.call(ctx, "tools/list", json.RawMessage(`{}`), 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("mcp %s: tools/list: %w", c.name, err)
	}
	var out struct {
		Tools []ToolDef `json:"tools"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, fmt.Errorf("mcp %s: parse tools: %w", c.name, err)
	}
	c.mu.Lock()
	c.tools = out.Tools
	c.mu.Unlock()
	return out.Tools, nil
}

// CallTool memanggil tool di server MCP dan mengembalikan teks hasilnya.
func (c *Client) CallTool(ctx context.Context, toolName string, argsJSON string) (string, error) {
	arguments := json.RawMessage("{}")
	if argsJSON != "" {
		arguments = json.RawMessage(argsJSON)
	}
	params, _ := json.Marshal(map[string]any{"name": toolName, "arguments": arguments})
	res, err := c.call(ctx, "tools/call", params, 5*time.Minute)
	if err != nil {
		return "", err
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return string(res), nil
	}
	var b strings.Builder
	for _, c := range out.Content {
		b.WriteString(c.Text)
	}
	if out.IsError {
		return b.String(), fmt.Errorf("%s", b.String())
	}
	return b.String(), nil
}

// Tools mengembalikan daftar tool terakhir yang dikenal.
func (c *Client) Tools() []ToolDef {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tools
}

// Close menutup koneksi.
func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd != nil {
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		_ = c.cmd.Wait()
	}
}

// call mengirim request dan menunggu response. Remote memakai jalur
// request-response langsung; stdio memakai channel per-ID.
func (c *Client) call(ctx context.Context, method string, params json.RawMessage, timeout time.Duration) (json.RawMessage, error) {
	if c.hc != nil {
		return c.callRemote(ctx, method, params, timeout)
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("koneksi MCP sudah ditutup")
	}
	c.nextID++
	id := int64(c.nextID)
	ch := make(chan rpcResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	req := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	if err := c.send(ctx, data); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("koneksi MCP tertutup saat menunggu %s", method)
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("%s (kode %d)", resp.Error.Message, resp.Error.Code)
		}
		return resp.Result, nil
	case <-time.After(timeout):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("timeout menunggu respons %s", method)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// notify mengirim notifikasi tanpa menunggu balasan.
func (c *Client) notify(method string, params json.RawMessage) {
	n := rpcNotification{JSONRPC: "2.0", Method: method, Params: params}
	data, err := json.Marshal(n)
	if err != nil {
		return
	}
	_ = c.send(context.Background(), data)
}

// readLoop membaca baris JSON-RPC dari stdout server stdio.
func (c *Client) readLoop() {
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			c.Close()
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue // bukan response (mis. log)
		}
		if resp.ID == 0 {
			continue // notifikasi
		}
		c.mu.Lock()
		ch, ok := c.pending[int64(resp.ID)]
		delete(c.pending, int64(resp.ID))
		c.mu.Unlock()
		if ok {
			ch <- resp
		}
	}
}
