// Package config memuat konfigurasi JenderalCode dari file JSONC
// (JSON dengan komentar) yang digabung berlapis:
//
//	bawaan -> global (~/.config/jenderalcode/jenderal.jsonc)
//	       -> proyek (./jenderal.jsonc) -> variabel lingkungan -> flag CLI
//
// Nilai string mendukung interpolasi "{env:NAMA_VAR}".
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tailscale/hujson"
)

// Config adalah konfigurasi hasil gabungan seluruh lapisan.
// Akses nilai lewat method bertipe agar aman.
type Config struct {
	raw     map[string]any
	Sources []string // file yang berhasil dimuat, urut prioritas naik
}

// Parse mem-parsing satu konten JSONC menjadi map.
func Parse(data []byte) (map[string]any, error) {
	// hujson menoleransi komentar dan koma ekstra; standardize lalu
	// unmarshal sebagai JSON biasa.
	ast, err := hujson.Parse(data)
	if err != nil {
		return nil, err
	}
	ast.Standardize()
	var out map[string]any
	if err := json.Unmarshal([]byte(ast.String()), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Load memuat konfigurasi dari semua lapisan standar di dirKerja.
func Load(dirKerja string) (*Config, error) {
	c := &Config{raw: map[string]any{}}
	paths := []string{
		filepath.Join(ConfigDir(), "jenderal.jsonc"),
		filepath.Join(dirKerja, "jenderal.jsonc"),
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("config: %s: %w", p, err)
		}
		m, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("config: %s: %w", p, err)
		}
		mergeInto(c.raw, asMap(interpolate(m)))
		c.Sources = append(c.Sources, p)
	}
	// Lapisan variabel lingkungan: JENDERAL_MODEL, JENDERAL_THEME, dst.
	applyEnv(c.raw)
	return c, nil
}

// FromMap membuat Config langsung dari map (untuk test dan embed).
// Nilai "{env:VAR}" diinterpolasi seperti hasil Load.
func FromMap(m map[string]any) *Config {
	if m == nil {
		m = map[string]any{}
	}
	return &Config{raw: asMap(interpolate(cloneValue(m)))}
}

// Raw mengembalikan salinan map mentah (untuk serialisasi/debug).
func (c *Config) Raw() map[string]any {
	out, _ := cloneValue(c.raw).(map[string]any)
	return out
}

// Merge menerapkan override di atas konfigurasi saat ini (flag CLI).
func (c *Config) Merge(override map[string]any) {
	mergeInto(c.raw, asMap(interpolate(override)))
}

// Get mengambil nilai mentah dengan path "a.b.c".
func (c *Config) Get(path string) any {
	parts := strings.Split(path, ".")
	var cur any = c.raw
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return cur
}

// String mengambil nilai string dengan default.
func (c *Config) String(path, def string) string {
	if v, ok := c.Get(path).(string); ok && v != "" {
		return v
	}
	return def
}

// Int mengambil nilai angka dengan default (menerima float64 dari JSON).
func (c *Config) Int(path string, def int) int {
	switch v := c.Get(path).(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return def
}

// Float mengambil nilai pecahan dengan default.
func (c *Config) Float(path string, def float64) float64 {
	switch v := c.Get(path).(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return def
}

// Bool mengambil nilai boolean dengan default.
func (c *Config) Bool(path string, def bool) bool {
	if v, ok := c.Get(path).(bool); ok {
		return v
	}
	return def
}

// StringMap mengambil map[string]string dari sebuah path.
func (c *Config) StringMap(path string) map[string]string {
	out := map[string]string{}
	m, ok := c.Get(path).(map[string]any)
	if !ok {
		return out
	}
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// Map mengambil sub-map mentah dari sebuah path.
func (c *Config) Map(path string) map[string]any {
	m, _ := c.Get(path).(map[string]any)
	return m
}

// Aksesori bertipe untuk kunci standar.

func (c *Config) Model() string         { return c.String("model", "") }
func (c *Config) SmallModel() string    { return c.String("small_model", "") }
func (c *Config) Language() string      { return c.String("language", "id") }
func (c *Config) Theme() string         { return c.String("theme", "jenderal") }
func (c *Config) Currency() string      { return c.String("currency", "IDR") }
func (c *Config) ExchangeRate() float64 { return c.Float("exchange_rate", 16500) } // 1 USD -> currency
func (c *Config) StepLimit() int        { return c.Int("step_limit", 50) }
func (c *Config) CompactPercent() int   { return c.Int("compact_percent", 85) }
func (c *Config) BashTimeout() int      { return c.Int("bash_timeout_sec", 120) }

// Budget mengembalikan batas biaya dalam USD (0 = tanpa batas).
func (c *Config) BudgetPerSession() float64 { return c.Float("budget.per_session_usd", 0) }
func (c *Config) BudgetPerDay() float64     { return c.Float("budget.per_day_usd", 0) }

// Providers mengembalikan konfigurasi provider kustom/global.
func (c *Config) Providers() map[string]any { return c.Map("provider") }

// Permissions mengembalikan aturan izin mentah (tool -> level atau pola).
func (c *Config) Permissions() map[string]any { return c.Map("permission") }

// MCPServers mengembalikan konfigurasi server MCP.
func (c *Config) MCPServers() map[string]any { return c.Map("mcp") }

// Keybinds mengembalikan pemetaan aksi -> tombol.
func (c *Config) Keybinds() map[string]string { return c.StringMap("keybinds") }

// applyEnv membaca JENDERAL_* untuk beberapa kunci atas.
func applyEnv(raw map[string]any) {
	pairs := map[string]string{
		"JENDERAL_MODEL":    "model",
		"JENDERAL_SMALL":    "small_model",
		"JENDERAL_THEME":    "theme",
		"JENDERAL_LANGUAGE": "language",
		"JENDERAL_CURRENCY": "currency",
	}
	for env, key := range pairs {
		if v := os.Getenv(env); v != "" {
			raw[key] = v
		}
	}
}

// interpolate mengganti "{env:VAR}" dalam semua string di dalam struktur.
func interpolate(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = interpolate(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = interpolate(val)
		}
		return t
	case string:
		return expandEnv(t)
	}
	return v
}

// expandEnv mengganti seluruh kemunculan "{env:NAMA}" dengan nilai env.
// Variabel yang tidak ada diganti string kosong.
func expandEnv(s string) string {
	if !strings.Contains(s, "{env:") {
		return s
	}
	var b strings.Builder
	for {
		i := strings.Index(s, "{env:")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		j := strings.Index(s[i:], "}")
		if j < 0 {
			b.WriteString(s)
			return b.String()
		}
		name := s[i+5 : i+j]
		b.WriteString(s[:i])
		b.WriteString(os.Getenv(name))
		s = s[i+j+1:]
	}
}

// mergeInto menggabungkan src ke dst secara rekursif (map digabung,
// nilai lain menimpa). src menang atas dst.
func mergeInto(dst, src map[string]any) {
	for k, v := range src {
		if vm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				mergeInto(dm, vm)
				continue
			}
			cp, _ := cloneValue(vm).(map[string]any)
			dst[k] = cp
			continue
		}
		dst[k] = cloneValue(v)
	}
}

// cloneValue menyalin struktur map/slice agar lapisan tidak saling menimpa.
func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = cloneValue(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = cloneValue(val)
		}
		return out
	}
	return v
}

// mergeMaps diekspor untuk paket lain yang perlu menggabungkan konfigurasi.
func MergeMaps(dst, src map[string]any) { mergeInto(dst, src) }

// Dir untuk berbagai lokasi data aplikasi, mengikuti konvensi OS.
// Bisa dioverride dengan JENDERAL_CONFIG_DIR / _DATA_DIR / _CACHE_DIR.

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

func xdgOr(envVar, defRel string) string {
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	return filepath.Join(homeDir(), defRel)
}

func appDir(envVar, xdgVar, linuxDef, macDef, winDef string) string {
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv(winDef), "jenderalcode")
	case "darwin":
		return filepath.Join(homeDir(), "Library", "Application Support", macDef)
	default:
		return filepath.Join(xdgOr(xdgVar, linuxDef), "jenderalcode")
	}
}

// ConfigDir adalah lokasi konfigurasi global.
func ConfigDir() string {
	return appDir("JENDERAL_CONFIG_DIR", "XDG_CONFIG_HOME", ".config", "jenderalcode", "APPDATA")
}

// DataDir adalah lokasi data (database sesi, snapshot, kredensial).
func DataDir() string {
	return appDir("JENDERAL_DATA_DIR", "XDG_DATA_HOME", ".local/share", "jenderalcode", "LOCALAPPDATA")
}

// CacheDir adalah lokasi cache (katalog model hasil refresh).
func CacheDir() string {
	if v := os.Getenv("JENDERAL_CACHE_DIR"); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "jenderalcode", "cache")
	case "darwin":
		return filepath.Join(homeDir(), "Library", "Caches", "jenderalcode")
	default:
		return filepath.Join(xdgOr("XDG_CACHE_HOME", ".cache"), "jenderalcode")
	}
}

// DBPath adalah lokasi database SQLite sesi.
func DBPath() string { return filepath.Join(DataDir(), "jenderal.db") }

// SnapshotDir adalah lokasi snapshot file untuk undo.
func SnapshotDir() string { return filepath.Join(DataDir(), "snapshots") }

// EnsureDirs membuat semua direktori aplikasi.
func EnsureDirs() error {
	for _, d := range []string{ConfigDir(), DataDir(), CacheDir(), SnapshotDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// asMap melakukan type assertion any -> map[string]any dengan aman.
func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
