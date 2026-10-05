package provider

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/mohammadirham37/jenderal_code/catalog"
	"github.com/mohammadirham37/jenderal_code/internal/config"
)

// Registry menyimpan semua provider yang siap dipakai (punya kredensial
// atau lokal). Provider tanpa kredensial dilewati, bukan error, agar
// pengguna bisa mengisi provider lain tanpa menghapus konfigurasi lama.
type Registry struct {
	providers map[string]Provider
	keys      *KeyStore
}

// NewRegistry membentuk registry dari katalog bawaan + konfigurasi pengguna.
func NewRegistry(cfg *config.Config, keys *KeyStore) (*Registry, error) {
	r := &Registry{providers: map[string]Provider{}, keys: keys}
	provCfg := cfg.Providers()

	for _, def := range mustProviders() {
		pcfg, _ := provCfg[def.ID].(map[string]any)

		adapter := def.Adapter
		baseURL := def.BaseURL
		configKey := ""
		if pcfg != nil {
			if t, ok := pcfg["type"].(string); ok && t != "" {
				adapter = normalizeAdapter(t)
			}
			if b, ok := pcfg["base_url"].(string); ok && b != "" {
				baseURL = b
			}
			if k, ok := pcfg["api_key"].(string); ok {
				configKey = k
			}
		}
		key, hasKey := ResolveKey(def.ID, def.EnvKey, configKey, keys)
		if def.RequiresKey && !hasKey {
			continue // lewati provider tanpa kredensial
		}

		models := def.Models
		if pcfg != nil {
			if mm, ok := pcfg["models"].(map[string]any); ok {
				models = applyModelOverrides(models, mm)
			}
		}
		r.register(adapter, def.ID, def.Name, baseURL, key, models)
	}

	// Provider kustom yang tidak ada di katalog.
	for id, v := range provCfg {
		if _, exists := r.providers[id]; exists {
			continue
		}
		pcfg, ok := v.(map[string]any)
		if !ok {
			continue
		}
		adapter := "openai"
		if t, ok := pcfg["type"].(string); ok && t != "" {
			adapter = normalizeAdapter(t)
		}
		baseURL, _ := pcfg["base_url"].(string)
		configKey, _ := pcfg["api_key"].(string)
		key, _ := ResolveKey(id, "", configKey, keys)
		var models []catalog.ModelInfo
		if mm, ok := pcfg["models"].(map[string]any); ok {
			models = applyModelOverrides(nil, mm)
		}
		if len(models) == 0 {
			models = []catalog.ModelInfo{{ID: "default", ContextWindow: 32768, MaxOutput: 8192, SupportsTools: true}}
		}
		r.register(adapter, id, id, baseURL, key, models)
	}
	return r, nil
}

func (r *Registry) register(adapter, id, name, baseURL, key string, models []catalog.ModelInfo) {
	switch adapter {
	case "anthropic":
		r.providers[id] = withRetry(NewAnthropic(AnthropicOptions{
			ID: id, Name: name, BaseURL: baseURL, APIKey: key, Models: models,
		}))
	default: // openai | openai-compatible
		r.providers[id] = withRetry(NewOpenAI(OpenAIOptions{
			ID: id, Name: name, BaseURL: baseURL, APIKey: key, Models: models,
		}))
	}
}

func mustProviders() []catalog.ProviderDef {
	ps, err := catalog.Providers()
	if err != nil {
		return nil
	}
	return ps
}

// normalizeAdapter memetakan nama tipe konfigurasi ke adapter internal.
func normalizeAdapter(t string) string {
	switch strings.ToLower(t) {
	case "anthropic", "anthropic-messages":
		return "anthropic"
	default:
		return "openai"
	}
}

// applyModelOverrides menggabungkan override model dari konfigurasi.
func applyModelOverrides(models []catalog.ModelInfo, overrides map[string]any) []catalog.ModelInfo {
	out := append([]catalog.ModelInfo(nil), models...)
	idx := map[string]int{}
	for i, m := range out {
		idx[m.ID] = i
	}
	for id, v := range overrides {
		mv, _ := v.(map[string]any)
		mi := catalog.ModelInfo{ID: id, ContextWindow: 32768, MaxOutput: 8192, SupportsTools: true}
		if i, ok := idx[id]; ok {
			mi = out[i]
		} else {
			out = append(out, mi)
			idx[id] = len(out) - 1
		}
		if mv != nil {
			if n, ok := mv["context_window"].(float64); ok && n > 0 {
				mi.ContextWindow = int(n)
			}
			if n, ok := mv["max_output"].(float64); ok && n > 0 {
				mi.MaxOutput = int(n)
			}
			if b, ok := mv["supports_tools"].(bool); ok {
				mi.SupportsTools = b
			}
			if b, ok := mv["supports_images"].(bool); ok {
				mi.SupportsImages = b
			}
			if b, ok := mv["supports_reasoning"].(bool); ok {
				mi.SupportsReasoning = b
			}
			if n, ok := mv["price_input_per_m"].(float64); ok {
				mi.PriceInputPerM = n
			}
			if n, ok := mv["price_output_per_m"].(float64); ok {
				mi.PriceOutputPerM = n
			}
			out[idx[id]] = mi
		}
	}
	return out
}

// Get mengembalikan provider berdasarkan ID.
func (r *Registry) Get(id string) (Provider, error) {
	if p, ok := r.providers[id]; ok {
		return p, nil
	}
	available := r.IDs()
	if len(available) == 0 {
		return nil, fmt.Errorf("provider %q tidak tersedia; tidak ada provider terkonfigurasi — jalankan `jenderal auth login <provider>` atau isi API key di jenderal.jsonc", id)
	}
	return nil, fmt.Errorf("provider %q tidak tersedia (kredensial belum diatur atau tidak dikenal); tersedia: %s", id, strings.Join(available, ", "))
}

// IDs mengembalikan ID provider yang siap dipakai, terurut.
func (r *Registry) IDs() []string {
	out := make([]string, 0, len(r.providers))
	for id := range r.providers {
		out = append(out, id)
	}
	// urutkan sederhana
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// All mengembalikan seluruh provider terdaftar.
func (r *Registry) All() map[string]Provider { return r.providers }

// ---- Pembungkus retry ----

// withRetry membungkus provider agar error 429/5xx/jaringan dicoba ulang
// dengan exponential backoff (maks 5 kali), menghormati Retry-After.
func withRetry(p Provider) Provider {
	return &retryProvider{inner: p, max: 5}
}

type retryProvider struct {
	inner Provider
	max   int
}

func (r *retryProvider) ID() string { return r.inner.ID() }
func (r *retryProvider) Models(ctx context.Context) ([]catalog.ModelInfo, error) {
	return r.inner.Models(ctx)
}

func (r *retryProvider) Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	for attempt := 0; ; attempt++ {
		ch, err := r.inner.Stream(ctx, req)
		if err == nil {
			return ch, nil
		}

		var re *RetryableError
		if attempt >= r.max-1 || ctx.Err() != nil || !asRetryable(err, &re) {
			return nil, err
		}
		delay := time.Duration(1<<uint(attempt)) * time.Second
		if re != nil && re.RetryAfter > delay {
			delay = re.RetryAfter
		}
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}

func asRetryable(err error, target **RetryableError) bool {
	for err != nil {
		if re, ok := err.(*RetryableError); ok {
			*target = re
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// ---- Mock provider (untuk test dan demo offline) ----

// MockTurn adalah satu jawaban terskrip dari MockProvider.
type MockTurn struct {
	Reasoning string
	Text      string
	ToolCalls []ToolCall
	Usage     Usage
	Err       error
}

// MockProvider memutar jawaban sesuai skrip; dipakai di unit test dan
// contoh `--provider mock` agar aplikasi bisa dicoba tanpa API key.
type MockProvider struct {
	mu       sync.Mutex
	id       string
	turns    []MockTurn
	i        int
	cycle    bool
	Requests []ChatRequest
}

// NewMock membuat provider mock dengan skrip jawaban.
func NewMock(id string, turns []MockTurn, cycle bool) *MockProvider {
	if len(turns) == 0 {
		turns = []MockTurn{{Text: "Halo! Saya JenderalCode (mock)."}}
	}
	return &MockProvider{id: id, turns: turns, cycle: cycle}
}

func (m *MockProvider) ID() string { return m.id }

func (m *MockProvider) Models(ctx context.Context) ([]catalog.ModelInfo, error) {
	return []catalog.ModelInfo{{
		ID: "mock-1", ContextWindow: 100000, MaxOutput: 8192,
		SupportsTools: true, Recommended: true,
	}}, nil
}

func (m *MockProvider) Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	m.mu.Lock()
	turn := m.turns[m.i]
	if m.i < len(m.turns)-1 {
		m.i++
	} else if m.cycle {
		m.i = 0
	}
	m.Requests = append(m.Requests, cloneRequest(req))
	m.mu.Unlock()

	ch := make(chan StreamEvent, 32)
	go func() {
		defer close(ch)
		if turn.Err != nil {
			ch <- StreamEvent{Type: StreamError, Err: turn.Err}
			return
		}
		if turn.Reasoning != "" {
			ch <- StreamEvent{Type: StreamReasoning, Text: turn.Reasoning}
		}
		// Kirim teks per kata agar terasa streaming.
		words := strings.Fields(turn.Text)
		for i, w := range words {
			if ctx.Err() != nil {
				ch <- StreamEvent{Type: StreamError, Err: ctx.Err()}
				return
			}
			sep := " "
			if i == len(words)-1 {
				sep = ""
			}
			ch <- StreamEvent{Type: StreamText, Text: w + sep}
			time.Sleep(time.Duration(rand.Intn(8)) * time.Millisecond)
		}
		for _, tc := range turn.ToolCalls {
			ch <- StreamEvent{Type: StreamToolCall, ToolCall: &tc}
		}
		u := turn.Usage
		ch <- StreamEvent{Type: StreamUsage, Usage: &u}
		ch <- StreamEvent{Type: StreamDone}
	}()
	return ch, nil
}

func cloneRequest(req ChatRequest) ChatRequest {
	c := req
	c.Messages = append([]Message(nil), req.Messages...)
	return c
}

// AddProvider mendaftarkan provider tambahan (mis. mock untuk demo offline).
func (r *Registry) AddProvider(p Provider) {
	r.providers[p.ID()] = p
}

// NewRegistryForTest membuat registry berisi satu provider (unit test).
func NewRegistryForTest(p Provider) *Registry {
	return &Registry{providers: map[string]Provider{p.ID(): p}, keys: nil}
}
