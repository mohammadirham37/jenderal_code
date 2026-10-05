// Package bus menyediakan pub/sub event internal yang menjadi tulang
// punggung komunikasi antara engine agen dan antarmuka (CLI, TUI, server).
// Core menerbitkan event; antarmuka hanya berlangganan.
package bus

import (
	"sync"
)

// EventType mengidentifikasi jenis event.
type EventType string

const (
	EventTextDelta         EventType = "text_delta"         // potongan teks jawaban model
	EventReasoningDelta    EventType = "reasoning_delta"    // potongan reasoning model
	EventToolCall          EventType = "tool_call"          // model meminta eksekusi tool
	EventToolResult        EventType = "tool_result"        // hasil eksekusi tool
	EventUsage             EventType = "usage"              // pemakaian token & biaya
	EventStatus            EventType = "status"             // status agen (mis. "merangkum konteks")
	EventPermissionRequest EventType = "permission_request" // butuh keputusan pengguna
	EventPermissionDone    EventType = "permission_done"    // keputusan izin diproses
	EventMessageStored     EventType = "message_stored"     // pesan tersimpan ke sesi
	EventTitle             EventType = "title"              // judul sesi dibuat
	EventModeChanged       EventType = "mode_changed"
	EventModelChanged      EventType = "model_changed"
	EventError             EventType = "error"
	EventDone              EventType = "done" // putaran agen selesai
)

// Usage adalah pemakaian token satu permintaan model.
type Usage struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`
}

// Event adalah satu kejadian yang disiarkan ke pelanggan.
type Event struct {
	Type         EventType      `json:"type"`
	SessionID    string         `json:"session_id,omitempty"`
	Text         string         `json:"text,omitempty"`
	ToolName     string         `json:"tool_name,omitempty"`
	ToolCallID   string         `json:"tool_call_id,omitempty"`
	Args         string         `json:"args,omitempty"`
	Result       string         `json:"result,omitempty"`
	IsErr        bool           `json:"is_err,omitempty"`
	Usage        *Usage         `json:"usage,omitempty"`
	PermissionID string         `json:"permission_id,omitempty"`
	Detail       map[string]any `json:"detail,omitempty"`
	Err          string         `json:"err,omitempty"`
}

// Bus adalah pub/sub event sederhana berbasis channel.
// Publish bersifat non-blocking per pelanggan dengan buffer besar;
// jika buffer pelanggan penuh, kirim memblokir sampai terbaca agar
// tidak ada delta teks yang hilang.
type Bus struct {
	mu     sync.Mutex
	nextID int
	subs   map[int]chan Event
	closed bool
}

// New membuat bus kosong.
func New() *Bus {
	return &Bus{subs: map[int]chan Event{}}
}

// Subscribe mendaftarkan pelanggan baru dan mengembalikan id serta channel.
func (b *Bus) Subscribe() (int, <-chan Event) {
	ch := make(chan Event, 512)
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextID
	b.nextID++
	b.subs[id] = ch
	return id, ch
}

// Unsubscribe melepas pelanggan dan menutup channel-nya.
func (b *Bus) Unsubscribe(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, ok := b.subs[id]; ok {
		delete(b.subs, id)
		close(ch)
	}
}

// Publish menyiarkan event ke semua pelanggan. Aman dipanggil konkuren.
// Setelah Close, Publish tidak melakukan apa-apa.
func (b *Bus) Publish(e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	for _, ch := range b.subs {
		ch <- e
	}
}

// Close menutup bus dan semua channel pelanggan.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, ch := range b.subs {
		close(ch)
		delete(b.subs, id)
	}
}
