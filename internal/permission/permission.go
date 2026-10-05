// Package permission menangani sistem izin tiga level (allow/ask/deny)
// per tool dan per pola perintah, mis.:
//
//	"bash": { "git status": "allow", "rm *": "deny", "*": "ask" }
//
// Aturan membaca konfigurasi `permission` dari jenderal.jsonc; keputusan
// "selalu untuk sesi ini" disimpan di memori.
package permission

import (
	"path/filepath"

	"sort"
	"strings"
)

// Level hasil keputusan izin.
type Level string

const (
	Allow Level = "allow"
	Ask   Level = "ask"
	Deny  Level = "deny"
)

// Decision hasil pemeriksaan izin.
type Decision struct {
	Level  Level  `json:"level"`
	Reason string `json:"reason,omitempty"`
}

// Allowed true bila keputusan = allow.
func (d Decision) Allowed() bool { return d.Level == Allow }

// Rules kumpulan aturan izin satu sesi.
type Rules struct {
	raw        map[string]any  // tool -> Level(string) | map[pattern]Level
	approved   map[string]bool // kunci "tool\x00target" disetujui utk sesi ini
	projectDir string          // dipakai untuk deteksi akses luar proyek
	yolo       bool
}

// New membuat Rules dari konfigurasi mentah.
func New(raw map[string]any, projectDir string, yolo bool) *Rules {
	if raw == nil {
		raw = map[string]any{}
	}
	return &Rules{
		raw:        raw,
		approved:   map[string]bool{},
		projectDir: projectDir,
		yolo:       yolo,
	}
}

// SetYolo mengubah mode yolo saat runtime.
func (r *Rules) SetYolo(v bool) { r.yolo = v }

// ApproveSession menyetujui pasangan tool+target untuk sisa sesi.
func (r *Rules) ApproveSession(tool, target string) {
	r.approved[tool+"\x00"+target] = true
}

// Approved true bila sudah pernah disetujui untuk sesi ini.
func (r *Rules) Approved(tool, target string) bool {
	return r.approved[tool+"\x00"+target]
}

// Check menentukan level izin untuk tool dengan target (perintah bash atau
// path file). defaultLevel dipakai bila tidak ada aturan yang cocok.
func (r *Rules) Check(tool, target string, def Level) Decision {
	if r.yolo {
		return Decision{Allow, "--yolo: semua izin disetujui"}
	}
	if r.Approved(tool, target) {
		return Decision{Allow, "disetujui untuk sesi ini"}
	}
	// Akses file di luar direktori proyek selalu ditanya (sebelum aturan).
	if isPathTool(tool) && target != "" && r.projectDir != "" && outsideDir(r.projectDir, target) {
		return Decision{Ask, "di luar direktori proyek"}
	}
	rule := r.raw[tool]
	switch rv := rule.(type) {
	case string:
		return Decision{parseLevel(rv, def), "aturan " + tool}
	case map[string]any:
		if d, ok := matchPatterns(rv, target); ok {
			if d == Ask {
				return Decision{Ask, "cocok pola izin"}
			}
			return Decision{d, "cocok pola izin"}
		}
	}
	return Decision{def, "default tool " + tool}
}

// matchPatterns mencocokkan target dengan pola pada aturan per-pola.
// Cocok persis menang; jika tidak ada, pola glob terpanjang yang cocok.
func matchPatterns(rules map[string]any, target string) (Level, bool) {
	// Cocok persis lebih dulu.
	if lv, ok := rules[target]; ok {
		return parseLevelAny(lv, Ask), true
	}
	// Kumpulkan pola glob yang cocok, ambil yang paling spesifik (terpanjang).
	best := ""
	var bestLv Level
	found := false
	pats := make([]string, 0, len(rules))
	for p := range rules {
		pats = append(pats, p)
	}
	sort.Slice(pats, func(i, j int) bool { return len(pats[i]) > len(pats[j]) })
	for _, p := range pats {
		if GlobMatch(p, target) {
			best, bestLv, found = p, parseLevelAny(rules[p], Ask), true
			break
		}
	}
	return bestLv, found && best != ""
}

// parseLevel mengubah string level; nilai tak dikenal menjadi def.
func parseLevel(s string, def Level) Level {
	switch Level(strings.ToLower(s)) {
	case Allow:
		return Allow
	case Ask:
		return Ask
	case Deny:
		return Deny
	}
	return def
}

func parseLevelAny(v any, def Level) Level {
	s, _ := v.(string)
	return parseLevel(s, def)
}

// isPathTool melaporkan apakah target tool adalah path file.
func isPathTool(tool string) bool {
	switch tool {
	case "read", "write", "edit", "patch":
		return true
	}
	return false
}

// GlobMatch mencocokkan pola dengan wildcard '*' (banyak karakter) dan
// '?' (satu karakter), tanpa dukungan path separator khusus.
func GlobMatch(pattern, s string) bool {
	// Implementasi DP sederhana untuk * dan ?.
	px, sx := 0, 0
	starPx, starSx := -1, 0
	for sx < len(s) {
		switch {
		case px < len(pattern) && (pattern[px] == '?' || pattern[px] == s[sx]):
			px++
			sx++
		case px < len(pattern) && pattern[px] == '*':
			starPx = px
			starSx = sx
			px++
		case starPx >= 0:
			px = starPx + 1
			starSx++
			sx = starSx
		default:
			return false
		}
	}
	for px < len(pattern) && pattern[px] == '*' {
		px++
	}
	return px == len(pattern)
}

// outsideDir memeriksa apakah path berada di luar dir.
func outsideDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
