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

// AnthropicOptions opsi adapter Anthropic Messages.
type AnthropicOptions struct {
	ID         string
	Name       string
	BaseURL    string // mis. https://api.anthropic.com
	APIKey     string
	Models     []catalog.ModelInfo
	HTTPClient *http.Client
}

type anthropicProvider struct {
	opts AnthropicOptions
	http *http.Client
}

// NewAnthropic membuat provider Anthropic Messages API.
func NewAnthropic(opts AnthropicOptions) Provider {
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Minute}
	}
	return &anthropicProvider{opts: opts, http: hc}
}

func (p *anthropicProvider) ID() string { return p.opts.ID }

func (p *anthropicProvider) Models(ctx context.Context) ([]catalog.ModelInfo, error) {
	return append([]catalog.ModelInfo(nil), p.opts.Models...), nil
}

// ---- Bentuk permintaan/respons Messages API ----

type anBlock struct {
	Type string `json:"type"` // text | tool_use | tool_result | thinking
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type anMessage struct {
	Role    string    `json:"role"` // user | assistant
	Content []anBlock `json:"content"`
}

type anTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type anRequest struct {
	Model     string      `json:"model"`
	MaxTokens int         `json:"max_tokens"`
	System    string      `json:"system,omitempty"`
	Messages  []anMessage `json:"messages"`
	Tools     []anTool    `json:"tools,omitempty"`
	Stream    bool        `json:"stream"`
	Metadata  *struct{}   `json:"metadata,omitempty"`
}

type anEvent struct {
	Type    string `json:"type"` // message_start | content_block_start | content_block_delta | content_block_stop | message_delta | message_stop | error
	Index   int    `json:"index"`
	Message *struct {
		Usage anUsage `json:"usage"`
	} `json:"message"`
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"` // text_delta | input_json_delta | thinking_delta
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *anUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type anUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

func (p *anthropicProvider) Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	body := buildAnthropicRequest(req, p.opts.Models)
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(p.opts.BaseURL, "/")+"/v1/messages", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("x-api-key", p.opts.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return nil, &RetryableError{Err: fmt.Errorf("anthropic: %w", err)}
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

func (p *anthropicProvider) consume(ctx context.Context, resp *http.Response, ch chan<- StreamEvent) {
	defer resp.Body.Close()
	defer close(ch)

	// Akumulasi tool_use per index blok.
	args := map[int]*strings.Builder{}
	toolMeta := map[int][2]string{} // index -> [id, name]

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	var eventType string
	for sc.Scan() {
		if ctx.Err() != nil {
			ch <- StreamEvent{Type: StreamError, Err: ctx.Err()}
			return
		}
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(line[6:])
		case strings.HasPrefix(line, "data:"):
			payload := strings.TrimSpace(line[5:])
			if payload == "" {
				continue
			}
			var ev anEvent
			if err := json.Unmarshal([]byte(payload), &ev); err != nil {
				continue
			}
			_ = eventType // data sudah memuat "type"
			switch ev.Type {
			case "message_start":
				if ev.Message != nil {
					u := &Usage{
						InputTokens:      ev.Message.Usage.InputTokens,
						CacheReadTokens:  ev.Message.Usage.CacheReadInputTokens,
						CacheWriteTokens: ev.Message.Usage.CacheCreationInputTokens,
					}
					ch <- StreamEvent{Type: StreamUsage, Usage: u}
				}
			case "content_block_start":
				if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
					toolMeta[ev.Index] = [2]string{ev.ContentBlock.ID, ev.ContentBlock.Name}
					args[ev.Index] = &strings.Builder{}
				}
			case "content_block_delta":
				if ev.Delta == nil {
					continue
				}
				switch ev.Delta.Type {
				case "text_delta":
					if ev.Delta.Text != "" {
						ch <- StreamEvent{Type: StreamText, Text: ev.Delta.Text}
					}
				case "thinking_delta":
					if ev.Delta.Thinking != "" {
						ch <- StreamEvent{Type: StreamReasoning, Text: ev.Delta.Thinking}
					}
				case "input_json_delta":
					if b, ok := args[ev.Index]; ok {
						b.WriteString(ev.Delta.PartialJSON)
					}
				}
			case "content_block_stop":
				if b, ok := args[ev.Index]; ok {
					meta := toolMeta[ev.Index]
					argStr := b.String()
					if argStr == "" {
						argStr = "{}"
					}
					emitToolCall(ch, meta[0], meta[1], argStr)
					delete(args, ev.Index)
				}
			case "message_delta":
				if ev.Usage != nil {
					u := &Usage{OutputTokens: ev.Usage.OutputTokens}
					ch <- StreamEvent{Type: StreamUsage, Usage: u}
				}
			case "message_stop":
				ch <- StreamEvent{Type: StreamDone}
				return
			case "error":
				if ev.Error != nil {
					ch <- StreamEvent{Type: StreamError, Err: fmt.Errorf("%s", ev.Error.Message)}
					return
				}
			}
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		ch <- StreamEvent{Type: StreamError, Err: fmt.Errorf("anthropic: stream: %w", err)}
		return
	}
	ch <- StreamEvent{Type: StreamDone}
}

// buildAnthropicRequest mengonversi ChatRequest ke bentuk Messages API.
// Pesan tool (hasil) berturut-turut digabung ke satu pesan user karena
// Anthropic menuntut tool_result berada di dalam pesan user.
func buildAnthropicRequest(req ChatRequest, models []catalog.ModelInfo) *anRequest {
	maxOut := 8192
	for _, m := range models {
		if m.ID == req.Model && m.MaxOutput > 0 {
			maxOut = m.MaxOutput
			break
		}
	}
	if req.MaxTokens > 0 && req.MaxTokens < maxOut {
		maxOut = req.MaxTokens
	}

	msgs := []anMessage{}
	appendBlock := func(role string, b anBlock) {
		// Anthropic: blok satu giliran berada dalam SATU pesan per role.
		// tool_result digabung ke user; blok assistant berturut-turut
		// (teks + tool_use) digabung ke pesan assistant terakhir.
		if len(msgs) > 0 && msgs[len(msgs)-1].Role == role {
			if role == "assistant" || (role == "user" && b.Type == "tool_result") {
				msgs[len(msgs)-1].Content = append(msgs[len(msgs)-1].Content, b)
				return
			}
		}
		msgs = append(msgs, anMessage{Role: role, Content: []anBlock{b}})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			if m.Text != "" {
				appendBlock("user", anBlock{Type: "text", Text: m.Text})
			}
		case RoleAssistant:
			if m.Text != "" {
				appendBlock("assistant", anBlock{Type: "text", Text: m.Text})
			}
			for _, tc := range m.ToolCalls {
				input := json.RawMessage(tc.Args)
				if !json.Valid(input) {
					input = json.RawMessage(`{}`)
				}
				appendBlock("assistant", anBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: input})
			}
		case RoleTool:
			appendBlock("user", anBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Text})
		case RoleSystem:
			// Sistem juga masuk ke system prompt utama.
		}
	}
	r := &anRequest{
		Model:     req.Model,
		MaxTokens: maxOut,
		System:    req.System,
		Messages:  msgs,
		Stream:    true,
	}
	for _, t := range req.Tools {
		schema := t.Schema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		r.Tools = append(r.Tools, anTool{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	return r
}
