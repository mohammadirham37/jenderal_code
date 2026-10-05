// Package catalog menyimpan daftar provider dan model bawaan beserta
// harganya. Katalog di-embed ke binary sehingga bekerja offline, dan bisa
// diperbarui dengan `jenderalcode models --refresh` (tersimpan di cache OS).
package catalog

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

//go:embed models.json
var embedded embed.FS

// ModelInfo adalah metadata satu model AI.
type ModelInfo struct {
	ID                string  `json:"id"`
	ContextWindow     int     `json:"context_window"`
	MaxOutput         int     `json:"max_output"`
	SupportsTools     bool    `json:"supports_tools"`
	SupportsImages    bool    `json:"supports_images"`
	SupportsReasoning bool    `json:"supports_reasoning"`
	PriceInputPerM    float64 `json:"price_input_per_m"`  // USD per 1 juta token
	PriceOutputPerM   float64 `json:"price_output_per_m"` // USD per 1 juta token
	PriceCachePerM    float64 `json:"price_cache_per_m"`  // USD per 1 juta token cache
	Recommended       bool    `json:"recommended,omitempty"`
}

// ProviderDef adalah definisi satu provider di katalog.
type ProviderDef struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Adapter     string      `json:"adapter"` // openai | anthropic
	BaseURL     string      `json:"base_url"`
	EnvKey      string      `json:"env_key"`
	RequiresKey bool        `json:"requires_key"`
	Note        string      `json:"note,omitempty"`
	Models      []ModelInfo `json:"models"`
}

type catalogFile struct {
	Version   int           `json:"version"`
	Updated   string        `json:"updated"`
	Providers []ProviderDef `json:"providers"`
}

var (
	mu       sync.RWMutex
	loaded   *catalogFile
	cacheDir string
)

// SetCacheDir menentukan lokasi cache katalog hasil refresh.
// Harus dipanggil sebelum Load agar file cache terbaca.
func SetCacheDir(dir string) {
	mu.Lock()
	defer mu.Unlock()
	cacheDir = dir
	loaded = nil
}

// Load membaca katalog: file cache hasil refresh jika ada, kalau tidak
// katalog bawaan yang di-embed.
func Load() (*catalogFile, error) {
	mu.Lock()
	defer mu.Unlock()
	if loaded != nil {
		return loaded, nil
	}
	var data []byte
	if cacheDir != "" {
		if b, err := os.ReadFile(filepath.Join(cacheDir, "models.json")); err == nil && json.Valid(b) {
			data = b
		}
	}
	if data == nil {
		b, err := embedded.ReadFile("models.json")
		if err != nil {
			return nil, fmt.Errorf("catalog: katalog bawaan tidak terbaca: %w", err)
		}
		data = b
	}
	var c catalogFile
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("catalog: JSON tidak valid: %w", err)
	}
	loaded = &c
	return loaded, nil
}

// Providers mengembalikan semua provider terurut berdasarkan ID.
func Providers() ([]ProviderDef, error) {
	c, err := Load()
	if err != nil {
		return nil, err
	}
	out := append([]ProviderDef(nil), c.Providers...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Provider mencari satu provider berdasarkan ID.
func Provider(id string) (*ProviderDef, error) {
	c, err := Load()
	if err != nil {
		return nil, err
	}
	for i := range c.Providers {
		if c.Providers[i].ID == id {
			p := c.Providers[i]
			return &p, nil
		}
	}
	return nil, fmt.Errorf("provider %q tidak dikenal di katalog", id)
}

// Model mencari model "provider/model" atau "model" di dalam satu provider.
// Jika model tidak ada di katalog, dikembalikan metadata generik (0 harga)
// dengan flag found=false agar tetap bisa dipakai.
func Model(ref string) (ModelInfo, string, bool, error) {
	providerID, modelID := SplitModelRef(ref)
	if providerID == "" {
		c, err := Load()
		if err != nil {
			return ModelInfo{}, "", false, err
		}
		for _, p := range c.Providers {
			for _, m := range p.Models {
				if m.ID == modelID {
					return m, p.ID, true, nil
				}
			}
		}
		return ModelInfo{}, "", false, nil
	}
	p, err := Provider(providerID)
	if err != nil {
		return ModelInfo{}, "", false, err
	}
	for _, m := range p.Models {
		if m.ID == modelID {
			return m, providerID, true, nil
		}
	}
	return ModelInfo{}, providerID, false, nil
}

// SplitModelRef memecah referensi "provider/model" menjadi dua bagian.
// Tanpa "/" maka provider kosong dan seluruh string dianggap model.
func SplitModelRef(ref string) (providerID, modelID string) {
	for i := 0; i < len(ref); i++ {
		if ref[i] == '/' {
			return ref[:i], ref[i+1:]
		}
	}
	return "", ref
}

// JoinModelRef menggabungkan provider dan model menjadi referensi lengkap.
func JoinModelRef(providerID, modelID string) string {
	if providerID == "" {
		return modelID
	}
	return providerID + "/" + modelID
}

// SaveCache menyimpan katalog hasil refresh ke direktori cache.
func SaveCache(dir string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".models.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "models.json"))
}

// CacheAge mengembalikan umur file cache katalog (0 jika tidak ada).
func CacheAge(dir string) time.Duration {
	fi, err := os.Stat(filepath.Join(dir, "models.json"))
	if err != nil {
		return 0
	}
	return time.Since(fi.ModTime())
}
