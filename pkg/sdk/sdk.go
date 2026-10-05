// Package sdk adalah klien Go untuk server lokal JenderalCode
// (`jenderal serve`). Dipakai ekstensi editor, desktop, dan otomasi lain.
package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jenderalcode/jenderal/internal/bus"
	"github.com/jenderalcode/jenderal/internal/session"
)

// Client koneksi ke server JenderalCode.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New membuat klien ke server di baseURL (mis. http://127.0.0.1:4096)
// dengan token yang dicetak saat server start.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var rd *strings.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Jenderal-Token", c.token)
	return c.http.Do(req)
}

// CreateSession membuat sesi baru.
func (c *Client) CreateSession(ctx context.Context, projectPath, model string) (*session.Session, error) {
	resp, err := c.do(ctx, http.MethodPost, "/session", map[string]string{"project_path": projectPath, "model": model})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errFrom(resp)
	}
	var s session.Session
	return &s, json.NewDecoder(resp.Body).Decode(&s)
}

// ListSessions daftar sesi.
func (c *Client) ListSessions(ctx context.Context) ([]session.Session, error) {
	resp, err := c.do(ctx, http.MethodGet, "/session", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out []session.Session
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

// GetSession detail sesi + pesan.
func (c *Client) GetSession(ctx context.Context, id string) (*session.Session, []session.StoredMessage, error) {
	resp, err := c.do(ctx, http.MethodGet, "/session/"+id, nil)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Session  *session.Session        `json:"session"`
		Messages []session.StoredMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, nil, err
	}
	return out.Session, out.Messages, nil
}

// SendMessage mengirim pesan ke agen (asinkron di server).
func (c *Client) SendMessage(ctx context.Context, id, text, model string) error {
	resp, err := c.do(ctx, http.MethodPost, "/session/"+id+"/message", map[string]string{"text": text, "model": model})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errFrom(resp)
	}
	return nil
}

// AnswerPermission menjawab permintaan izin (allow/deny, always untuk sesi).
func (c *Client) AnswerPermission(ctx context.Context, sessionID, reqID string, allow, always bool) error {
	resp, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/session/%s/permission/%s", sessionID, reqID),
		map[string]any{"decision": map[bool]string{true: "allow", false: "deny"}[allow], "always": always})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// Abort menghentikan agen yang berjalan.
func (c *Client) Abort(ctx context.Context, id string) error {
	resp, err := c.do(ctx, http.MethodPost, "/session/"+id+"/abort", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// SetMode mengatur mode agen: build | plan | toggle.
func (c *Client) SetMode(ctx context.Context, id, mode string) error {
	resp, err := c.do(ctx, http.MethodPost, "/session/"+id+"/mode", map[string]string{"mode": mode})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Events berlangganan stream SSE event sesi; blokir sampai ctx selesai.
func (c *Client) Events(ctx context.Context, id string) (<-chan bus.Event, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/session/"+id+"/events", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Jenderal-Token", c.token)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := (&http.Client{Timeout: 0}).Do(req)
	if err != nil {
		return nil, err
	}
	out := make(chan bus.Event, 128)
	go func() {
		defer resp.Body.Close()
		defer close(out)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(line[5:])
			if payload == "" {
				continue
			}
			var ev bus.Event
			if err := json.Unmarshal([]byte(payload), &ev); err == nil {
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func errFrom(resp *http.Response) error {
	var e struct {
		Error string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&e)
	if e.Error == "" {
		return fmt.Errorf("server menjawab %d", resp.StatusCode)
	}
	return fmt.Errorf("%s", e.Error)
}
