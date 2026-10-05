package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jenderalcode/jenderal/catalog"
)

// serverOpenAI tiruan endpoint chat/completions SSE.
func serverOpenAI(t *testing.T, authOK *bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authOK != nil {
			tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if tok != "key-123" {
				w.WriteHeader(401)
				w.Write([]byte(`{"error":{"message":"bad key"}}`))
				return
			}
		}
		if r.URL.Path == "/models" {
			w.Write([]byte(`{"data":[{"id":"model-x"},{"id":"model-y"}]}`))
			return
		}
		if r.URL.Path != "/chat/completions" {
			w.WriteHeader(404)
			return
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if req["stream"] != true {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		write := func(s string) { w.Write([]byte(s)); fl.Flush() }
		write("data: {\"choices\":[{\"delta\":{\"content\":\"halo \"}}]}\n\n")
		write("data: {\"choices\":[{\"delta\":{\"content\":\"dunia\"}}]}\n\n")
		write("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"write\",\"arguments\":\"{\\\"path\\\":\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n")
		write("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"a.txt\\\"}\"}}]}}]}\n\n")
		write("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
		write("data: [DONE]\n\n")
	}))
}

func TestOpenAIStream(t *testing.T) {
	ok := true
	srv := serverOpenAI(t, &ok)
	defer srv.Close()

	p := NewOpenAI(OpenAIOptions{ID: "fake", BaseURL: srv.URL, APIKey: "key-123"})
	ch, err := p.Stream(context.Background(), ChatRequest{Model: "model-x", Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	var calls []ToolCall
	var usage *Usage
	for ev := range ch {
		switch ev.Type {
		case StreamText:
			text.WriteString(ev.Text)
		case StreamToolCall:
			calls = append(calls, *ev.ToolCall)
		case StreamUsage:
			usage = ev.Usage
		case StreamError:
			t.Fatalf("stream error: %v", ev.Err)
		}
	}
	if text.String() != "halo dunia" {
		t.Fatalf("teks salah: %q", text.String())
	}
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Name != "write" {
		t.Fatalf("tool call salah: %+v", calls)
	}
	// Argumen gabungan dua delta harus JSON valid.
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Args), &args); err != nil {
		t.Fatalf("argumen tidak valid: %q %v", calls[0].Args, err)
	}
	if args["path"] != "a.txt" {
		t.Fatalf("args.path salah: %v", args["path"])
	}
	if usage == nil || usage.InputTokens != 10 || usage.OutputTokens != 5 {
		t.Fatalf("usage salah: %+v", usage)
	}
}

func TestOpenAIModelsMerge(t *testing.T) {
	srv := serverOpenAI(t, nil)
	defer srv.Close()
	p := NewOpenAI(OpenAIOptions{
		ID: "fake", BaseURL: srv.URL,
		Models: []catalog.ModelInfo{{ID: "model-x", ContextWindow: 128000, SupportsTools: true}},
	})
	ms, err := p.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, m := range ms {
		ids[m.ID] = true
	}
	if !ids["model-x"] || !ids["model-y"] {
		t.Fatalf("gabungan model salah: %v", ids)
	}
}

// serverAnthropic tiruan endpoint /v1/messages SSE.
func serverAnthropic(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-1" {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path != "/v1/messages" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		write := func(s string) { w.Write([]byte(s)); fl.Flush() }
		write("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":20,\"cache_read_input_tokens\":5}}}\n\n")
		write("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n")
		write("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"jawaban\"}}\n\n")
		write("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		write("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"tu_1\",\"name\":\"read\"}}\n\n")
		write("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\": \\\"main.go\\\"\"}}\n\n")
		write("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"}\"}}\n\n")
		write("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n")
		write("event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":15}}\n\n")
		write("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
}

func TestAnthropicStream(t *testing.T) {
	srv := serverAnthropic(t)
	defer srv.Close()
	p := NewAnthropic(AnthropicOptions{ID: "fake", BaseURL: srv.URL, APIKey: "sk-1"})
	ch, err := p.Stream(context.Background(), ChatRequest{Model: "claude-x", Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	var calls []ToolCall
	var in, out, cache int64
	for ev := range ch {
		switch ev.Type {
		case StreamText:
			text.WriteString(ev.Text)
		case StreamToolCall:
			calls = append(calls, *ev.ToolCall)
		case StreamUsage:
			if ev.Usage.InputTokens > 0 {
				in = ev.Usage.InputTokens
				cache = ev.Usage.CacheReadTokens
			}
			if ev.Usage.OutputTokens > 0 {
				out = ev.Usage.OutputTokens
			}
		case StreamError:
			t.Fatalf("stream error: %v", ev.Err)
		}
	}
	if text.String() != "jawaban" {
		t.Fatalf("teks salah: %q", text.String())
	}
	if len(calls) != 1 || calls[0].Name != "read" {
		t.Fatalf("tool call salah: %+v", calls)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Args), &args); err != nil || args["path"] != "main.go" {
		t.Fatalf("argumen salah: %q", calls[0].Args)
	}
	if in != 20 || out != 15 || cache != 5 {
		t.Fatalf("usage salah: in=%d out=%d cache=%d", in, out, cache)
	}
}

func TestAnthropicToolResultMerging(t *testing.T) {
	// Dua hasil tool berturut-turut harus digabung ke SATU pesan user.
	req := ChatRequest{
		Model:  "m",
		System: "s",
		Messages: []Message{
			{Role: RoleUser, Text: "q"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "a", Name: "read", Args: "{}"}, {ID: "b", Name: "grep", Args: "{}"}}},
			{Role: RoleTool, ToolCallID: "a", Text: "hasil-a"},
			{Role: RoleTool, ToolCallID: "b", Text: "hasil-b"},
		},
	}
	r := buildAnthropicRequest(req, nil)
	if len(r.Messages) != 3 {
		t.Fatalf("pesan user harus digabung; dapat %d pesan", len(r.Messages))
	}
	last := r.Messages[len(r.Messages)-1]
	if last.Role != "user" || len(last.Content) != 2 ||
		last.Content[0].Type != "tool_result" || last.Content[1].ToolUseID != "b" {
		t.Fatalf("tool_result salah: %+v", last)
	}
	if r.System != "s" || r.MaxTokens <= 0 {
		t.Fatal("system/max_tokens hilang")
	}
}

func TestMockProvider(t *testing.T) {
	m := NewMock("mock", []MockTurn{
		{Text: "pertama"},
		{Text: "kedua", ToolCalls: []ToolCall{{ID: "t1", Name: "read", Args: `{}`}}, Usage: Usage{InputTokens: 3, OutputTokens: 4}},
	}, false)
	ch, _ := m.Stream(context.Background(), ChatRequest{Model: "mock-1"})
	var got []StreamEvent
	for ev := range ch {
		got = append(got, ev)
	}
	if len(got) == 0 {
		t.Fatal("tidak ada event")
	}
	m2 := NewMock("mock", []MockTurn{
		{ToolCalls: []ToolCall{{ID: "t1", Name: "read", Args: `{}`}}},
		{Text: "selesai"},
	}, false)
	ch2, _ := m2.Stream(context.Background(), ChatRequest{})
	for range ch2 {
	}
	if len(m2.Requests) != 1 {
		t.Fatalf("requests tercatat salah: %d", len(m2.Requests))
	}
}
