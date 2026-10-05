// Package provider mendefinisikan interface tunggal untuk semua penyedia
// AI dan menyediakan adapter OpenAI-compatible serta Anthropic Messages.
// Provider baru yang OpenAI-compatible cukup ditambahkan lewat konfigurasi.
package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/jenderalcode/jenderal/catalog"
)

// Role peran pesan dalam percakapan.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool" // hasil eksekusi tool
)

// ToolCall adalah permintaan pemanggilan tool dari model.
type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"` // JSON argumen
}

// ToolDef adalah definisi tool yang dikirim ke model (JSON Schema).
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"-"` // JSON Schema parameter
}

// Message adalah satu pesan dalam percakapan.
type Message struct {
	Role       Role       `json:"role"`
	Text       string     `json:"text,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // assistant: tool yang diminta
	ToolCallID string     `json:"tool_call_id,omitempty"` // tool: id panggilan yang dijawab
	ToolName   string     `json:"tool_name,omitempty"`
}

// Usage adalah pemakaian token satu permintaan.
type Usage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
}

// Total memambahkan Usage lain ke usage ini.
func (u *Usage) Add(o Usage) {
	u.InputTokens += o.InputTokens
	u.OutputTokens += o.OutputTokens
	u.CacheReadTokens += o.CacheReadTokens
	u.CacheWriteTokens += o.CacheWriteTokens
}

// StreamEventType jenis event stream dari provider.
type StreamEventType int

const (
	StreamText StreamEventType = iota
	StreamReasoning
	StreamToolCall // ToolCall lengkap (argumen sudah utuh)
	StreamUsage
	StreamError
	StreamDone
)

// StreamEvent adalah satu kejadian dalam stream jawaban model.
type StreamEvent struct {
	Type     StreamEventType
	Text     string
	ToolCall *ToolCall
	Usage    *Usage
	Err      error
}

// ChatRequest adalah permintaan chat lengkap ke provider.
type ChatRequest struct {
	Model       string    // ID model tanpa prefix provider
	System      string    // system prompt
	Messages    []Message // riwayat percakapan
	Tools       []ToolDef // tool yang boleh dipanggil
	MaxTokens   int       // 0 = default provider
	Temperature *float64  // nil = default provider
}

// Provider adalah interface tunggal semua penyedia AI.
type Provider interface {
	ID() string
	// Models daftar model yang tersedia beserta metadata dan harga.
	Models(ctx context.Context) ([]catalog.ModelInfo, error)
	// Stream mengirim permintaan dan mengembalikan channel event.
	// Channel ditutup setelah event Done atau Error.
	Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
}

// ErrNoKey dilempar saat provider butuh API key tetapi tidak ditemukan.
var ErrNoKey = errors.New("api key tidak ditemukan (atur lewat `jenderal auth login`, variabel lingkungan, atau jenderal.jsonc)")

// ModelRefError kesalahan referensi model "provider/model".
type ModelRefError struct{ Ref string }

func (e *ModelRefError) Error() string {
	return fmt.Sprintf("referensi model %q tidak valid (gunakan format provider/model, mis. zai/glm-4.6)", e.Ref)
}

// CostUSD menghitung biaya USD dari usage dan harga model.
func CostUSD(u Usage, m catalog.ModelInfo) float64 {
	cost := float64(u.InputTokens)/1e6*m.PriceInputPerM +
		float64(u.OutputTokens)/1e6*m.PriceOutputPerM +
		float64(u.CacheReadTokens)/1e6*m.PriceCachePerM
	return cost
}
