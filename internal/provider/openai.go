package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jenderalcode/jenderal/catalog"
)

// OpenAIOptions opsi adapter OpenAI-compatible.
type OpenAIOptions struct {
	ID          string
	Name        string
	BaseURL     string
	APIKey      string
	Models      []catalog.ModelInfo // fallback metadata
	ExtraHeader map[string]string   // mis. HTTP-Referer untuk OpenRouter
	HTTPClient  *http.Client
}

// openaiProvider berbicara dengan endpoint Chat Completions
// OpenAI-compatible (OpenAI, Z.ai, OpenRouter, Ollama, dll).
type openaiProvider struct {
	opts OpenAIOptions
	http *http.Client
}

// NewOpenAI membuat provider OpenAI-compatible.
func NewOpenAI(opts OpenAIOptions) Provider {
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Minute}
	}
	return &openaiProvider{opts: opts, http: hc}
}

func (p *openaiProvider) ID() string { return p.opts.ID }

// Models menggabungkan daftar model dari endpoint /models (jika tersedia)
// dengan metadata harga dari katalog.
func (p *openaiProvider) Models(ctx context.Context) ([]catalog.ModelInfo, error) {
	byID := map[string]catalog.ModelInfo{}
	for _, m := range p.opts.Models {
		byID[m.ID] = m
	}
	// Endpoint /models bersifat opsional (Ollama dan lainnya punya, beberapa tidak).
	out := []catalog.ModelInfo{}
	for _, m := range p.opts.Models {
		out = append(out, m)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(p.opts.BaseURL, "/")+"/models", nil)
	if err == nil {
		if p.opts.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+p.opts.APIKey)
		}
		if resp, err := p.http.Do(req); err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
			resp.Body.Close()
			if resp.StatusCode == 200 {
				var lr struct {
					Data []struct {
						ID string `json:"id"`
					} `json:"data"`
				}
				if json.Unmarshal(body, &lr) == nil {
					seen := map[string]bool{}
					for _, m := range out {
						seen[m.ID] = true
					}
					for _, d := range lr.Data {
						if d.ID == "" || seen[d.ID] {
							continue
						}
						seen[d.ID] = true
						mi := catalog.ModelInfo{ID: d.ID, ContextWindow: 32768, MaxOutput: 8192, SupportsTools: true}
						if meta, ok := byID[d.ID]; ok {
							mi = meta
						}
						out = append(out, mi)
					}
				}
			}
		}
	}
	return out, nil
}

// ---- Bentuk permintaan/respons Chat Completions ----

type oaMessage struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"` // string atau null
	ToolCalls  []oaToolCa `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type oaToolCa struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

type oaRequest struct {
	Model         string      `json:"model"`
	Messages      []oaMessage `json:"messages"`
	Stream        bool        `json:"stream"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
	Tools       []oaTool    `json:"tools,omitempty"`
	MaxTokens   int         `json:"max_tokens,omitempty"`
	Temperature *float64    `json:"temperature,omitempty"`
	Stop        interface{} `json:"stop,omitempty"`
}

type oaChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *oaUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

type oaUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	PromptTokensDet  *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// Stream menjalankan Chat Completions dengan stream SSE.
func (p *openaiProvider) Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	body := buildOpenAIRequest(req)
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(p.opts.BaseURL, "/")+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.opts.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.opts.APIKey)
	}
	for k, v := range p.opts.ExtraHeader {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.http.Do(httpReq)
	if err != nil {
		// Bungkus agar wrapper retry bisa membedakan error jaringan.
		return nil, &RetryableError{Err: fmt.Errorf("openai: %w", err)}
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		resp.Body.Close()
		return nil, apiErrorFromStatus(resp.StatusCode, resp.Header, b)
	}

	ch := make(chan StreamEvent, 64)
	go p.consume(ctx, resp, ch)
	return ch, nil
}

func (p *openaiProvider) consume(ctx context.Context, resp *http.Response, ch chan<- StreamEvent) {
	defer resp.Body.Close()
	defer close(ch)
	// tool_calls bisa datang bertahap per index; akumulasi sampai selesai.
	type acc struct {
		id, name, args string
	}
	accs := map[int]*acc{}
	flush := func() {
		for i := 0; i < len(accs); i++ {
			if a := accs[i]; a != nil {
				emitToolCall(ch, a.id, a.name, a.args)
			}
		}
		accs = map[int]*acc{}
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		if ctx.Err() != nil {
			ch <- StreamEvent{Type: StreamError, Err: ctx.Err()}
			return
		}
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[5:])
		if payload == "" || payload == "[DONE]" {
			if payload == "[DONE]" {
				break
			}
			continue
		}
		var ck oaChunk
		if err := json.Unmarshal([]byte(payload), &ck); err != nil {
			continue // baris non-JSON (beberapa server mengirim komentar)
		}
		if ck.Error != nil {
			ch <- StreamEvent{Type: StreamError, Err: fmt.Errorf("%s", ck.Error.Message)}
			return
		}
		for _, choice := range ck.Choices {
			d := choice.Delta
			if d.Content != "" {
				ch <- StreamEvent{Type: StreamText, Text: d.Content}
			}
			if d.ReasoningContent != "" {
				ch <- StreamEvent{Type: StreamReasoning, Text: d.ReasoningContent}
			}
			for _, tc := range d.ToolCalls {
				a := accs[tc.Index]
				if a == nil {
					a = &acc{}
					accs[tc.Index] = a
				}
				if tc.ID != "" {
					a.id = tc.ID
				}
				if tc.Function.Name != "" {
					a.name = tc.Function.Name
				}
				a.args += tc.Function.Arguments
			}
			if choice.FinishReason != nil && *choice.FinishReason == "tool_calls" {
				flush()
			}
		}
		if ck.Usage != nil {
			u := &Usage{
				InputTokens:  ck.Usage.PromptTokens,
				OutputTokens: ck.Usage.CompletionTokens,
			}
			if ck.Usage.PromptTokensDet != nil {
				u.CacheReadTokens = ck.Usage.PromptTokensDet.CachedTokens
			}
			ch <- StreamEvent{Type: StreamUsage, Usage: u}
		}
	}
	flush() // beberapa server tidak mengirim finish_reason tool_calls
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		ch <- StreamEvent{Type: StreamError, Err: fmt.Errorf("openai: stream: %w", err)}
		return
	}
	ch <- StreamEvent{Type: StreamDone}
}

// buildOpenAIRequest mengonversi ChatRequest internal ke bentuk API.
func buildOpenAIRequest(req ChatRequest) *oaRequest {
	msgs := []oaMessage{}
	if req.System != "" {
		msgs = append(msgs, oaMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			msgs = append(msgs, oaMessage{Role: "user", Content: m.Text})
		case RoleAssistant:
			am := oaMessage{Role: "assistant", Content: strOrNull(m.Text)}
			for _, tc := range m.ToolCalls {
				am.ToolCalls = append(am.ToolCalls, oaToolCa{
					ID:   tc.ID,
					Type: "function",
				})
				am.ToolCalls[len(am.ToolCalls)-1].Function.Name = tc.Name
				am.ToolCalls[len(am.ToolCalls)-1].Function.Arguments = tc.Args
			}
			msgs = append(msgs, am)
		case RoleTool:
			msgs = append(msgs, oaMessage{Role: "tool", Content: m.Text, ToolCallID: m.ToolCallID, Name: m.ToolName})
		case RoleSystem:
			msgs = append(msgs, oaMessage{Role: "system", Content: m.Text})
		}
	}
	r := &oaRequest{Model: req.Model, Messages: msgs, Stream: true}
	r.StreamOptions = &struct {
		IncludeUsage bool `json:"include_usage"`
	}{IncludeUsage: true}
	if len(req.Tools) > 0 {
		for _, t := range req.Tools {
			var ot oaTool
			ot.Type = "function"
			ot.Function.Name = t.Name
			ot.Function.Description = t.Description
			ot.Function.Parameters = t.Schema
			r.Tools = append(r.Tools, ot)
		}
	}
	if req.MaxTokens > 0 {
		r.MaxTokens = req.MaxTokens
	}
	r.Temperature = req.Temperature
	return r
}

func strOrNull(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// emitToolCall mengirim ToolCall lengkap; argumen dikosongkan menjadi "{}".
func emitToolCall(ch chan<- StreamEvent, id, name, args string) {
	if args == "" || args == "null" {
		args = "{}"
	}
	ch <- StreamEvent{Type: StreamToolCall, ToolCall: &ToolCall{ID: id, Name: name, Args: args}}
}

// apiErrorFromStatus membuat error dari respons non-200, menandai 429/5xx
// sebagai retryable dan menghormati header Retry-After.
func apiErrorFromStatus(status int, h http.Header, body []byte) error {
	msg := strings.TrimSpace(string(body))
	if len(msg) > 500 {
		msg = msg[:500]
	}
	// Coba ekstrak pesan error JSON.
	var je struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &je) == nil && je.Error.Message != "" {
		msg = je.Error.Message
	}
	e := fmt.Errorf("api %d: %s", status, msg)
	if status == 429 || status >= 500 {
		re := &RetryableError{Err: e}
		if ra := h.Get("Retry-After"); ra != "" {
			if d, err := time.ParseDuration(ra + "s"); err == nil {
				re.RetryAfter = d
			}
		}
		return re
	}
	return e
}

// RetryableError menandai error yang boleh dicoba ulang (429/5xx/jaringan).
type RetryableError struct {
	Err        error
	RetryAfter time.Duration
}

func (e *RetryableError) Error() string { return e.Err.Error() }
func (e *RetryableError) Unwrap() error { return e.Err }
