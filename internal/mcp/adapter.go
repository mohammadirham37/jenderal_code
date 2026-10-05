package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jenderalcode/jenderal/internal/tool"
)

// send mengirim data ke server: stdin (stdio) atau POST (remote).
func (c *Client) send(ctx context.Context, data []byte) error {
	if c.stdin != nil {
		data = append(data, '\n')
		_, err := c.stdin.Write(data)
		return err
	}
	if c.hc != nil {
		// Remote: POST dan abaikan respons (notifikasi).
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		_, err := c.hc.exchange(cctx, data)
		return err
	}
	return fmt.Errorf("tidak ada transport")
}

// syncHTTPClient klien HTTP untuk remote MCP (JSON POST sederhana).
type syncHTTPClient struct {
	url  string
	mu   sync.Mutex
	hc   *http.Client
	sess string // session id dari header bila server memberi
}

func newHTTPClient(url string) *syncHTTPClient {
	return &syncHTTPClient{url: url, hc: &http.Client{Timeout: 5 * time.Minute}}
}

// exchange mengirim satu request JSON dan mengembalikan respons.
func (h *syncHTTPClient) exchange(ctx context.Context, data []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	h.mu.Lock()
	if h.sess != "" {
		req.Header.Set("Mcp-Session-Id", h.sess)
	}
	h.mu.Unlock()
	resp, err := h.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		h.mu.Lock()
		h.sess = sid
		h.mu.Unlock()
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("mcp remote %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	ct := resp.Header.Get("Content-Type")
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if strings.Contains(ct, "text/event-stream") {
		// Ambil data pertama yang berisi JSON-RPC response.
		for _, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data:") {
				payload := strings.TrimSpace(line[5:])
				if payload != "" && payload != "[DONE]" {
					return []byte(payload), nil
				}
			}
		}
		return nil, fmt.Errorf("mcp remote: tidak ada data dalam event stream")
	}
	return body, nil
}

// sendRemote override untuk remote: langsung exchange dan teruskan ke readLoop.
// Karena remote bersifat request-response, call() untuk remote menggunakan
// jalur berbeda di bawah.

// callRemote adalah call() khusus remote transport.
func (c *Client) callRemote(ctx context.Context, method string, params json.RawMessage, timeout time.Duration) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := int64(c.nextID)
	c.mu.Unlock()
	req := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body, err := c.hc.exchange(cctx, data)
	if err != nil {
		return nil, err
	}
	var resp rpcResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("mcp remote: respons tidak valid: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("%s (kode %d)", resp.Error.Message, resp.Error.Code)
	}
	return resp.Result, nil
}

// ---- Adapter tool.Tool untuk MCP ----

// ToolAdapter membungkus tool server MCP sebagai tool internal.
type ToolAdapter struct {
	Client *Client
	Server string
	Def    ToolDef
}

func (t ToolAdapter) Name() string { return "mcp__" + sanitize(t.Server) + "__" + sanitize(t.Def.Name) }
func (t ToolAdapter) Description() string {
	if t.Def.Description == "" {
		return "Tool dari server MCP " + t.Server
	}
	return t.Def.Description + " (MCP: " + t.Server + ")"
}
func (t ToolAdapter) ReadOnly() bool          { return false } // aman: selalu lewat izin
func (t ToolAdapter) DefaultPerm() tool.Level { return tool.LevelAsk }

func (t ToolAdapter) Schema() map[string]any {
	var schema map[string]any
	if err := json.Unmarshal(t.Def.InputSchema, &schema); err != nil || schema == nil {
		schema = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return schema
}

func (t ToolAdapter) Exec(ctx context.Context, args map[string]any) (tool.Result, error) {
	b, err := json.Marshal(args)
	if err != nil {
		return tool.Result{}, err
	}
	out, err := t.Client.CallTool(ctx, t.Def.Name, string(b))
	if err != nil {
		return tool.Result{Err: true, Content: err.Error()}, nil
	}
	return tool.Result{Content: out}, nil
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, s)
}

// AttachAll membuka semua server MCP yang dikonfigurasi dan mendaftarkan
// tool-nya ke registry. Server yang gagal dilaporkan lewat onLog, bukan
// error fatal.
func AttachAll(ctx context.Context, configs map[string]any, reg *tool.Registry, onLog func(string)) []*Client {
	var clients []*Client
	for name, v := range configs {
		var cfg Config
		b, err := json.Marshal(v)
		if err != nil {
			continue
		}
		if err := json.Unmarshal(b, &cfg); err != nil {
			continue
		}
		cfg.Name = name
		if !cfg.EnabledOK() {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		var cl *Client
		switch cfg.Type {
		case "remote", "http", "sse":
			cl, err = ConnectRemote(cctx, name, cfg.URL)
		default: // local
			cl, err = StartLocal(cctx, name, cfg.Command, cfg.Env)
		}
		if err != nil {
			cancel()
			if onLog != nil {
				onLog("MCP " + name + ": " + err.Error())
			}
			continue
		}
		tools, err := cl.ListTools(cctx)
		cancel()
		if err != nil {
			cl.Close()
			if onLog != nil {
				onLog("MCP " + name + ": " + err.Error())
			}
			continue
		}
		for _, td := range tools {
			reg.Add(ToolAdapter{Client: cl, Server: name, Def: td})
		}
		clients = append(clients, cl)
		if onLog != nil {
			onLog(fmt.Sprintf("MCP %s terhubung (%d tool)", name, len(tools)))
		}
	}
	return clients
}
