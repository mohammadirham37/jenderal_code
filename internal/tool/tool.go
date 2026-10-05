// Package tool menyediakan tool bawaan agen: read, write, edit, patch,
// bash, glob, grep, ls, webfetch, todo, dan lsp_diagnostics. Tool task
// (sub-agen) dan tool MCP ditambahkan saat runtime lewat Registry.Add.
//
// Semua tool read-only bisa dieksekusi paralel; tool yang mengubah state
// dijalankan berurutan oleh loop agen.
package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jenderalcode/jenderal/internal/permission"
)

// Alias tipe izin diekspor agar adapter lain (MCP) bisa memakainya.
type (
	Level = permission.Level
)

const (
	LevelAllow = permission.Allow
	LevelAsk   = permission.Ask
	LevelDeny  = permission.Deny
)

// Result adalah hasil eksekusi tool.
type Result struct {
	Content string         // isi untuk model
	Err     bool           // true bila tool gagal (agar model bisa memperbaiki)
	Data    map[string]any // data tambahan untuk antarmuka (mis. path yang diubah)
}

// Batas ukuran eksekusi tool.
const (
	maxReadLines   = 2000
	maxListResults = 500
	maxGrepMatches = 300
)

// Tool adalah satu kemampuan yang bisa dipanggil model.
type Tool interface {
	Name() string
	Description() string
	Schema() map[string]any // JSON Schema parameter
	DefaultPerm() permission.Level
	ReadOnly() bool
	Exec(ctx context.Context, args map[string]any) (Result, error)
}

// Registry kumpulan tool yang tersedia.
type Registry struct {
	tools map[string]Tool
	order []string
}

// NewRegistry membuat registry kosong.
func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

// Add mendaftarkan tool (menimpa tool dengan nama sama, mis. MCP override).
func (r *Registry) Add(t Tool) {
	if _, ok := r.tools[t.Name()]; !ok {
		r.order = append(r.order, t.Name())
	}
	r.tools[t.Name()] = t
}

// Get mengambil tool berdasarkan nama.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Names mengembalikan nama semua tool terurut sesuai pendaftaran.
func (r *Registry) Names() []string {
	return append([]string(nil), r.order...)
}

// List mengembalikan semua tool.
func (r *Registry) List() []Tool {
	out := make([]Tool, 0, len(r.order))
	for _, n := range r.order {
		out = append(out, r.tools[n])
	}
	return out
}

// ---- Helper argumen dari model ----

func argString(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		switch t := v.(type) {
		case string:
			return t
		case float64:
			return strings.TrimSuffix(fmt.Sprintf("%v", t), ".0")
		default:
			return fmt.Sprintf("%v", t)
		}
	}
	return ""
}

func argInt(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case string:
		var n int
		fmt.Sscanf(v, "%d", &n)
		return n
	}
	return 0
}

func argBool(args map[string]any, key string) bool {
	v, _ := args[key].(bool)
	return v
}

func argSlice(args map[string]any, key string) []any {
	v, _ := args[key].([]any)
	return v
}

func argMap(args map[string]any, key string) map[string]any {
	v, _ := args[key].(map[string]any)
	return v
}

// resolvePath menyelesaikan path relatif terhadap direktori proyek,
// membersihkan ../, dan mengikuti symlink. Tidak membatasi di dalam
// proyek di sini (izin akses luar ditangani permission), tapi tetap
// menolak path kosong.
func resolvePath(projectDir, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path tidak boleh kosong")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(projectDir, path)
	}
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		// Pertahankan path asli untuk display; pakai resolved untuk cek.
		return resolved, nil
	}
	// File belum ada (untuk write): evaluasi direktori induknya.
	dir := filepath.Dir(path)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(resolved, filepath.Base(path)), nil
	}
	return path, nil
}

// ---- Ignore: .jenderalignore + default + .gitignore ----

// Ignore menyimpan pola file yang tidak boleh diakses agen.
type Ignore struct {
	projDir string
	hard    []string // dari .jenderalignore + default (dilarang keras)
	soft    []string // dari .gitignore (difilter pada ls/glob/grep)
}

// Default pola yang selalu dilarang (keamanan dan kebersihan).
var defaultHard = []string{
	".env", ".env.*", ".env.*/**",
	"*.pem", "*.key", "*.p12", "*.pfx",
	"id_rsa", "id_rsa.*", "id_ed25519", "id_ed25519.*", "*.ppk",
	"credentials.json", ".netrc", ".git-credentials",
	".DS_Store",
}

// DefaultSoft difilter dari daftar direktori.
var defaultSoft = []string{
	".git/**", "node_modules/**", "vendor/**", "dist/**", "build/**",
	".next/**", "__pycache__/**", "*.pyc", "target/**", "bin/**", "obj/**",
}

// LoadIgnore membaca .jenderalignore dan .gitignore di root proyek.
func LoadIgnore(projDir string) *Ignore {
	ig := &Ignore{projDir: projDir}
	ig.hard = append(ig.hard, defaultHard...)
	ig.soft = append(ig.soft, defaultSoft...)
	if lines := readLines(filepath.Join(projDir, ".jenderalignore")); lines != nil {
		ig.hard = append(ig.hard, lines...)
	}
	if lines := readLines(filepath.Join(projDir, ".gitignore")); lines != nil {
		ig.soft = append(ig.soft, lines...)
	}
	return ig
}

func readLines(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, ln := range strings.Split(string(b), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, "!") {
			continue
		}
		ln = strings.TrimSuffix(ln, "/")
		out = append(out, ln)
	}
	return out
}

// HardDenied true bila path (relatif atau absolut) cocok pola larangan
// keras — agen tidak boleh membaca atau menulis file itu sama sekali.
func (ig *Ignore) HardDenied(path string) bool {
	rel := ig.rel(path)
	for _, p := range ig.hard {
		if matchIgnore(p, rel) {
			return true
		}
	}
	return false
}

// SoftSkipped true bila path sebaiknya dilewati saat menelusuri direktori
// (aturan .gitignore dan cache build), tapi tidak dilarang keras.
func (ig *Ignore) SoftSkipped(path string) bool {
	rel := ig.rel(path)
	for _, p := range ig.soft {
		if matchIgnore(p, rel) {
			return true
		}
	}
	return false
}

func (ig *Ignore) rel(path string) string {
	if filepath.IsAbs(path) {
		if rel, err := filepath.Rel(ig.projDir, path); err == nil {
			return rel
		}
		return path
	}
	return path
}

// matchIgnore mencocokkan pola gitignore-style terhadap path relatif.
// Mendukung: nama, *.ext, dir/, dir/file, **/anak, awal/**, dan "dir/"
// yang berarti seluruh isi direktori.
func matchIgnore(pattern, rel string) bool {
	pattern = strings.TrimPrefix(pattern, "./")
	candidates := []string{rel}
	// "dir/" pada gitignore berarti semua isi dir; pola "dir" juga cocok.
	if matchOne(pattern, rel) {
		return true
	}
	// Cocokkan juga hanya nama berkas untuk pola tanpa '/'.
	if !strings.Contains(pattern, "/") {
		base := filepath.Base(rel)
		if ok, _ := filepath.Match(pattern, base); ok {
			return true
		}
	}
	// Pola dengan ** di awal: cocokkan bagian akhir.
	if strings.HasPrefix(pattern, "**/") {
		suffix := pattern[3:]
		parts := strings.Split(rel, string(filepath.Separator))
		for i := range parts {
			sub := strings.Join(parts[i:], string(filepath.Separator))
			candidates = append(candidates, sub)
			if matchOne(suffix, sub) {
				return true
			}
		}
	}
	for _, c := range candidates {
		if matchOne(pattern, c) {
			return true
		}
	}
	return false
}

// matchOne mencocokkan satu pola terhadap path, termasuk pola "dir/**".
func matchOne(pattern, path string) bool {
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return path == prefix || strings.HasPrefix(path, prefix+string(filepath.Separator))
	}
	ok, err := filepath.Match(pattern, path)
	if err != nil {
		return false
	}
	if ok {
		return true
	}
	// Pola "dir" cocok untuk path di dalam "dir/...".
	if !strings.ContainsAny(pattern, "*?") {
		return strings.HasPrefix(path, pattern+string(filepath.Separator))
	}
	return false
}
