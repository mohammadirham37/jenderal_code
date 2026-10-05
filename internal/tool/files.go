package tool

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jenderalcode/jenderal/internal/permission"
)

type perm = permission.Level

const (
	permAllow perm = permission.Allow
	permAsk   perm = permission.Ask
	permDeny  perm = permission.Deny
)

// base adalah dependensi bersama semua tool file.
type base struct {
	projDir string
	ignore  *Ignore
}

// errDenied error baku untuk file yang dilarang.
func errDenied(path string) error {
	return fmt.Errorf("akses ditolak: %s dikecualikan dari agen (.jenderalignore atau larangan bawaan seperti .env)", path)
}

// ---- read ----

type readTool struct{ b *base }

func (t readTool) Name() string { return "read" }
func (t readTool) Description() string {
	return "Baca isi file teks dengan nomor baris. Gunakan offset/limit untuk file besar. Tidak bisa membaca file yang dikecualikan (.env, kredensial, .jenderalignore)."
}
func (t readTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":   map[string]any{"type": "string", "description": "Path file, relatif terhadap proyek atau absolut"},
			"offset": map[string]any{"type": "integer", "description": "Baris awal (1-based), opsional"},
			"limit":  map[string]any{"type": "integer", "description": "Jumlah baris maksimum, default 2000"},
		},
		"required": []string{"path"},
	}
}
func (t readTool) DefaultPerm() perm { return permAllow }
func (t readTool) ReadOnly() bool    { return true }
func (t readTool) Exec(ctx context.Context, args map[string]any) (Result, error) {
	path, err := resolvePath(t.b.projDir, argString(args, "path"))
	if err != nil {
		return Result{}, err
	}
	if t.b.ignore.HardDenied(path) {
		return Result{Err: true, Content: errDenied(path).Error()}, nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return Result{Err: true, Content: fmt.Sprintf("file tidak ditemukan: %s", path)}, nil
	}
	if fi.IsDir() {
		return Result{Err: true, Content: fmt.Sprintf("%s adalah direktori; gunakan tool ls", path)}, nil
	}
	if fi.Size() > 8<<20 {
		return Result{Err: true, Content: "file terlalu besar (>8 MB); baca dengan offset/limit"}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return Result{Err: true, Content: err.Error()}, nil
	}
	defer f.Close()

	offset := argInt(args, "offset")
	if offset < 1 {
		offset = 1
	}
	limit := argInt(args, "limit")
	if limit < 1 || limit > maxReadLines {
		limit = maxReadLines
	}

	var buf bytes.Buffer
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256*1024), 4<<20)
	line := 0
	total := 0
	first := true
	for sc.Scan() {
		line++
		if first {
			// Deteksi biner: NUL di awal file.
			if i := bytes.IndexByte(sc.Bytes(), 0); i >= 0 {
				return Result{Err: true, Content: "file biner; tidak bisa ditampilkan sebagai teks"}, nil
			}
			first = false
		}
		total++
		if line < offset {
			continue
		}
		if line >= offset+limit {
			break
		}
		txt := sc.Text()
		if len(txt) > 2000 {
			txt = txt[:2000] + "…"
		}
		fmt.Fprintf(&buf, "%6d\t%s\n", line, txt)
	}
	if err := sc.Err(); err != nil {
		return Result{Err: true, Content: err.Error()}, nil
	}
	if buf.Len() == 0 {
		return Result{Content: "(file kosong atau offset melewati akhir file)"}, nil
	}
	content := buf.String()
	if line >= offset+limit {
		content += fmt.Sprintf("\n(…file berlanjut; %d baris tersisa. Baca lagi dengan offset=%d)", max(0, total-(offset+limit-1)), offset+limit)
	}
	return Result{Content: content}, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---- write ----

type writeTool struct{ b *base }

func (t writeTool) Name() string { return "write" }
func (t writeTool) Description() string {
	return "Buat file baru atau timpa file yang ada sepenuhnya dengan konten yang diberikan. Direktori induk dibuat otomatis."
}
func (t writeTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "description": "Path file tujuan"},
			"content": map[string]any{"type": "string", "description": "Isi lengkap file"},
		},
		"required": []string{"path", "content"},
	}
}
func (t writeTool) DefaultPerm() perm { return permAsk }
func (t writeTool) ReadOnly() bool    { return false }
func (t writeTool) Exec(ctx context.Context, args map[string]any) (Result, error) {
	path, err := resolvePath(t.b.projDir, argString(args, "path"))
	if err != nil {
		return Result{}, err
	}
	if t.b.ignore.HardDenied(path) {
		return Result{Err: true, Content: errDenied(path).Error()}, nil
	}
	content := argString(args, "content")
	prev, existed := "", false
	if old, err := os.ReadFile(path); err == nil {
		prev, existed = string(old), true
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{Err: true, Content: err.Error()}, nil
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return Result{Err: true, Content: err.Error()}, nil
	}
	n := strings.Count(content, "\n") + 1
	return Result{
		Content: fmt.Sprintf("file ditulis: %s (%d baris)", displayPath(t.b.projDir, path), n),
		Data:    map[string]any{"path": path, "existed": existed, "prev_content": prev},
	}, nil
}

func displayPath(projDir, p string) string {
	if rel, err := filepath.Rel(projDir, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}

// ---- edit (str_replace + multi-edit) ----

type editTool struct{ b *base }

func (t editTool) Name() string { return "edit" }
func (t editTool) Description() string {
	return "Edit file dengan mengganti string. old_string harus unik dalam file (atau set replace_all=true). Untuk beberapa penggantian sekaligus, kirim array edits."
}
func (t editTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string", "description": "Path file"},
			"old_string":  map[string]any{"type": "string", "description": "Teks yang dicari (harus unik kecuali replace_all)"},
			"new_string":  map[string]any{"type": "string", "description": "Teks pengganti"},
			"replace_all": map[string]any{"type": "boolean", "description": "Ganti semua kemunculan (default false)"},
			"edits": map[string]any{
				"type":        "array",
				"description": "Multi-edit: daftar {old_string, new_string} diterapkan berurutan",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"old_string": map[string]any{"type": "string"},
						"new_string": map[string]any{"type": "string"},
					},
					"required": []string{"old_string", "new_string"},
				},
			},
		},
		"required": []string{"path"},
	}
}
func (t editTool) DefaultPerm() perm { return permAsk }
func (t editTool) ReadOnly() bool    { return false }

type editPair struct{ oldS, newS string }

func (t editTool) Exec(ctx context.Context, args map[string]any) (Result, error) {
	path, err := resolvePath(t.b.projDir, argString(args, "path"))
	if err != nil {
		return Result{}, err
	}
	if t.b.ignore.HardDenied(path) {
		return Result{Err: true, Content: errDenied(path).Error()}, nil
	}
	orig, err := os.ReadFile(path)
	if err != nil {
		return Result{Err: true, Content: "file tidak bisa dibaca: " + err.Error()}, nil
	}
	content := string(orig)

	var pairs []editPair
	if old := argString(args, "old_string"); old != "" {
		pairs = append(pairs, editPair{old, argString(args, "new_string")})
	}
	for _, e := range argSlice(args, "edits") {
		m, _ := e.(map[string]any)
		if m == nil {
			continue
		}
		o, _ := m["old_string"].(string)
		n, _ := m["new_string"].(string)
		if o != "" {
			pairs = append(pairs, editPair{o, n})
		}
	}
	if len(pairs) == 0 {
		return Result{Err: true, Content: "tidak ada old_string/edits yang diberikan"}, nil
	}
	replaceAll := argBool(args, "replace_all")

	for i, p := range pairs {
		count := strings.Count(content, p.oldS)
		if count == 0 {
			return Result{Err: true, Content: fmt.Sprintf("edit %d: old_string tidak ditemukan di %s. Baca file dulu untuk melihat isi persisnya.", i+1, displayPath(t.b.projDir, path))}, nil
		}
		if count > 1 && !replaceAll {
			return Result{Err: true, Content: fmt.Sprintf("edit %d: old_string muncul %d kali; perluas teksnya agar unik atau set replace_all=true", i+1, count)}, nil
		}
		if replaceAll {
			content = strings.ReplaceAll(content, p.oldS, p.newS)
		} else {
			content = strings.Replace(content, p.oldS, p.newS, 1)
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return Result{Err: true, Content: err.Error()}, nil
	}
	return Result{
		Content: fmt.Sprintf("file diedit: %s (%d penggantian)", displayPath(t.b.projDir, path), len(pairs)),
		Data:    map[string]any{"path": path, "existed": true, "prev_content": string(orig)},
	}, nil
}

// ---- patch (unified diff) ----

type patchTool struct{ b *base }

func (t patchTool) Name() string { return "patch" }
func (t patchTool) Description() string {
	return "Terapkan unified diff ke file. Diff boleh berisi beberapa file dengan header ---/+++."
}
func (t patchTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"diff": map[string]any{"type": "string", "description": "Unified diff lengkap"},
		},
		"required": []string{"diff"},
	}
}
func (t patchTool) DefaultPerm() perm { return permAsk }
func (t patchTool) ReadOnly() bool    { return false }
func (t patchTool) Exec(ctx context.Context, args map[string]any) (Result, error) {
	diff := argString(args, "diff")
	if strings.TrimSpace(diff) == "" {
		return Result{Err: true, Content: "diff kosong"}, nil
	}
	files, err := parseUnifiedDiff(diff)
	if err != nil {
		return Result{Err: true, Content: err.Error()}, nil
	}
	var applied []string
	var prevs = map[string]string{}
	for _, pf := range files {
		target := pf.path
		if !filepath.IsAbs(target) {
			target = filepath.Join(t.b.projDir, target)
		}
		target = filepath.Clean(target)
		if t.b.ignore.HardDenied(target) {
			return Result{Err: true, Content: errDenied(target).Error()}, nil
		}
		if pf.isNew {
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return Result{Err: true, Content: err.Error()}, nil
			}
			if err := os.WriteFile(target, []byte(pf.newContent()), 0o644); err != nil {
				return Result{Err: true, Content: err.Error()}, nil
			}
		} else {
			orig, err := os.ReadFile(target)
			if err != nil {
				return Result{Err: true, Content: fmt.Sprintf("%s: %v", target, err)}, nil
			}
			newContent, err := applyHunks(string(orig), pf.hunks)
			if err != nil {
				return Result{Err: true, Content: fmt.Sprintf("%s: %v", displayPath(t.b.projDir, target), err)}, nil
			}
			prevs[target] = string(orig)
			if err := os.WriteFile(target, []byte(newContent), 0o644); err != nil {
				return Result{Err: true, Content: err.Error()}, nil
			}
		}
		applied = append(applied, displayPath(t.b.projDir, target))
	}
	return Result{
		Content: "patch diterapkan ke: " + strings.Join(applied, ", "),
		Data:    map[string]any{"paths": applied},
	}, nil
}

type hunk struct {
	oldStart, oldCount, newStart, newCount int
	lines                                  []string // tanpa header @@; dimulai ' ', '-', '+'
}

type patchFile struct {
	path  string
	isNew bool
	hunks []hunk
}

// newContent menyusun isi file baru dari baris '+' semua hunk.
func (pf *patchFile) newContent() string {
	var lines []string
	for _, h := range pf.hunks {
		for _, l := range h.lines {
			if strings.HasPrefix(l, "+") {
				lines = append(lines, strings.TrimPrefix(l, "+"))
			}
		}
	}
	return strings.Join(lines, "\n")
}

// parseUnifiedDiff menguraikan teks unified diff menjadi per file.
func parseUnifiedDiff(diff string) ([]patchFile, error) {
	var files []patchFile
	var cur *patchFile
	var h *hunk
	lines := strings.Split(strings.ReplaceAll(diff, "\r\n", "\n"), "\n")
	for _, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "--- "):
			if cur != nil {
				files = append(files, *cur)
			}
			cur = &patchFile{}
			h = nil
		case strings.HasPrefix(ln, "+++ "):
			if cur == nil {
				continue
			}
			p := strings.TrimSpace(strings.TrimPrefix(ln, "+++ "))
			p = strings.TrimPrefix(p, "b/")
			if p == "/dev/null" {
				return nil, fmt.Errorf("header +++ /dev/null tidak didukung")
			}
			cur.path = p
		case strings.HasPrefix(ln, "@@"):
			if cur == nil {
				continue
			}
			hh, err := parseHunkHeader(ln)
			if err != nil {
				return nil, err
			}
			cur.hunks = append(cur.hunks, hh)
			h = &cur.hunks[len(cur.hunks)-1]
		case h != nil && (strings.HasPrefix(ln, " ") || strings.HasPrefix(ln, "+") || strings.HasPrefix(ln, "-") || ln == ""):
			if ln == "" {
				ln = " "
			}
			h.lines = append(h.lines, ln)
		case strings.HasPrefix(ln, "diff "):
			// abaikan baris meta
		default:
			// abaikan baris lain (index, mode, komentar)
		}
	}
	if cur != nil {
		files = append(files, *cur)
	}
	for i := range files {
		f := &files[i]
		if f.path == "" {
			return nil, fmt.Errorf("diff tidak punya header +++ untuk file")
		}
		f.path = strings.TrimPrefix(f.path, "b/")
		// File baru: hunk hanya menambah.
		isNew := true
		for _, hh := range f.hunks {
			for _, l := range hh.lines {
				if strings.HasPrefix(l, "-") {
					isNew = false
				}
			}
		}
		if strings.HasSuffix(f.path, "/dev/null") || len(f.hunks) > 0 && f.hunks[0].oldStart == 0 {
			f.isNew = true
		} else {
			f.isNew = isNew
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("tidak ada file dalam diff; gunakan format unified diff dengan header ---/+++ dan @@")
	}
	return files, nil
}

func parseHunkHeader(ln string) (hunk, error) {
	var h hunk
	var oldStart, oldCount, newStart, newCount int
	_, err := fmt.Sscanf(ln, "@@ -%d,%d +%d,%d @@", &oldStart, &oldCount, &newStart, &newCount)
	if err != nil {
		if _, err2 := fmt.Sscanf(ln, "@@ -%d +%d @@", &oldStart, &newStart); err2 != nil {
			return h, fmt.Errorf("header hunk tidak valid: %s", ln)
		}
		oldCount, newCount = 1, 1
	}
	return hunk{oldStart: oldStart, oldCount: oldCount, newStart: newStart, newCount: newCount}, nil
}

// applyHunks menerapkan hunk ke konten dengan pencocokan konteks toleran
// (offset dicari sampai ±100 baris). Hunk diproses dari bawah ke atas agar
// nomor baris hunk di atas tetap valid.
func applyHunks(content string, hunks []hunk) (string, error) {
	src := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	for i := len(hunks) - 1; i >= 0; i-- {
		h := hunks[i]
		// expected = baris yang harus cocok (konteks + penghapusan),
		// repl = hasil penggantinya (konteks + penambahan), urut asli.
		var expected, repl []string
		for _, l := range h.lines {
			switch {
			case strings.HasPrefix(l, "+"):
				repl = append(repl, strings.TrimPrefix(l, "+"))
			case strings.HasPrefix(l, "-"):
				expected = append(expected, strings.TrimPrefix(l, "-"))
			default:
				ctx := strings.TrimPrefix(l, " ")
				expected = append(expected, ctx)
				repl = append(repl, ctx)
			}
		}
		start := h.oldStart - 1
		if start < 0 {
			start = 0
		}
		if start > len(src) {
			start = len(src)
		}
		if !matchSeq(src, start, expected) {
			found := false
			for off := 1; off <= 100 && !found; off++ {
				for _, s := range []int{start - off, start + off} {
					if s >= 0 && matchSeq(src, s, expected) {
						start = s
						found = true
						break
					}
				}
			}
			if !found {
				return "", fmt.Errorf("konteks hunk @@ -%d tidak cocok dengan isi file", h.oldStart)
			}
		}
		out := []string{}
		out = append(out, src[:start]...)
		out = append(out, repl...)
		out = append(out, src[start+len(expected):]...)
		src = out
	}
	return strings.Join(src, "\n"), nil
}

// matchSeq memeriksa apakah seq cocok persis pada posisi start di src.
func matchSeq(src []string, start int, seq []string) bool {
	if start < 0 || start+len(seq) > len(src) {
		return len(seq) == 0 && start <= len(src)
	}
	for i, s := range seq {
		if src[start+i] != s {
			return false
		}
	}
	return true
}

// ---- glob ----

type globTool struct{ b *base }

func (t globTool) Name() string { return "glob" }
func (t globTool) Description() string {
	return "Cari file berdasarkan pola glob, mis. **/*.go atau src/**/*.ts. Menghormati .gitignore."
}
func (t globTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{"type": "string", "description": "Pola glob, mis. **/*.go"},
			"path":    map[string]any{"type": "string", "description": "Direktori awal (default root proyek)"},
		},
		"required": []string{"pattern"},
	}
}
func (t globTool) DefaultPerm() perm { return permAllow }
func (t globTool) ReadOnly() bool    { return true }
func (t globTool) Exec(ctx context.Context, args map[string]any) (Result, error) {
	pattern := argString(args, "pattern")
	if pattern == "" {
		return Result{Err: true, Content: "pattern wajib diisi"}, nil
	}
	root := t.b.projDir
	if p := argString(args, "path"); p != "" {
		if r, err := resolvePath(t.b.projDir, p); err == nil {
			root = r
		}
	}
	var matches []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(t.b.projDir, path)
		if d.IsDir() {
			if path != t.b.projDir && t.b.ignore.SoftSkipped(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if t.b.ignore.HardDenied(rel) {
			return nil
		}
		if matchGlobPattern(pattern, rel) || matchGlobPattern(pattern, path) {
			matches = append(matches, rel)
			if len(matches) >= maxListResults {
				return filepath.SkipAll
			}
		}
		return nil
	})
	sort.Strings(matches)
	if len(matches) == 0 {
		return Result{Content: "tidak ada file yang cocok dengan pola " + pattern}, nil
	}
	trunc := ""
	if len(matches) >= maxListResults {
		trunc = fmt.Sprintf("\n(…dibatasi %d hasil)", maxListResults)
	}
	return Result{Content: strings.Join(matches, "\n") + trunc}, nil
}

// matchGlobPattern mendukung ** melintasi direktori.
func matchGlobPattern(pattern, path string) bool {
	if !strings.Contains(pattern, "**") {
		ok, _ := filepath.Match(pattern, path)
		return ok
	}
	// Ubah ** menjadi segment matcher sederhana.
	parts := strings.Split(pattern, "**")
	if len(parts) != 2 {
		ok, _ := filepath.Match(pattern, path)
		return ok
	}
	prefix, suffix := parts[0], strings.TrimPrefix(parts[1], string(filepath.Separator))
	if !strings.HasPrefix(path, prefix) {
		// prefix bisa kosong
		if prefix != "" {
			return false
		}
	}
	rest := strings.TrimPrefix(path, prefix)
	segs := strings.Split(rest, string(filepath.Separator))
	for i := range segs {
		sub := strings.Join(segs[i:], string(filepath.Separator))
		if ok, _ := filepath.Match(suffix, sub); ok {
			return true
		}
	}
	return false
}

// ---- grep ----

type grepTool struct{ b *base }

func (t grepTool) Name() string { return "grep" }
func (t grepTool) Description() string {
	return "Cari teks/regex di dalam file. Mendukung filter glob include (mis. *.go). Keluaran format file:baris:teks."
}
func (t grepTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{"type": "string", "description": "Regex atau teks yang dicari"},
			"path":    map[string]any{"type": "string", "description": "File atau direktori awal (default root proyek)"},
			"include": map[string]any{"type": "string", "description": "Filter nama file glob, mis. *.go"},
		},
		"required": []string{"pattern"},
	}
}
func (t grepTool) DefaultPerm() perm { return permAllow }
func (t grepTool) ReadOnly() bool    { return true }
func (t grepTool) Exec(ctx context.Context, args map[string]any) (Result, error) {
	pattern := argString(args, "pattern")
	if pattern == "" {
		return Result{Err: true, Content: "pattern wajib diisi"}, nil
	}
	root := t.b.projDir
	if p := argString(args, "path"); p != "" {
		if r, err := resolvePath(t.b.projDir, p); err == nil {
			root = r
		}
	}
	include := argString(args, "include")

	// Coba ripgrep bila tersedia (jauh lebih cepat di repo besar).
	if rgPath, err := exec.LookPath("rg"); err == nil {
		cmdArgs := []string{"-n", "--no-heading", "-S", "--max-count", "20", "--color", "never"}
		if include != "" {
			cmdArgs = append(cmdArgs, "-g", include)
		}
		for _, d := range []string{".git", "node_modules", "vendor", "dist"} {
			cmdArgs = append(cmdArgs, "-g", "!"+d+"/**")
		}
		cmdArgs = append(cmdArgs, "-e", pattern, root)
		cmd := exec.CommandContext(ctx, rgPath, cmdArgs...)
		out, err := cmd.Output()
		if err == nil && len(bytes.TrimSpace(out)) > 0 {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) > maxGrepMatches {
				lines = lines[:maxGrepMatches]
				lines = append(lines, "(…dibatasi 300 hasil)")
			}
			return Result{Content: relativize(t.b.projDir, lines)}, nil
		}
		if err == nil {
			// ripgrep sukses tapi tidak ada hasil → lanjut fallback tidak perlu
			return Result{Content: "tidak ditemukan: " + pattern}, nil
		}
		// ripgrep error (mis. pattern tidak valid) → jatuh ke implementasi Go
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		// Coba sebagai teks literal.
		re, err = regexp.Compile(regexp.QuoteMeta(pattern))
		if err != nil {
			return Result{Err: true, Content: "pattern tidak valid: " + err.Error()}, nil
		}
	}
	var out []string
	count := 0
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || count >= maxGrepMatches || ctx.Err() != nil {
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(t.b.projDir, path)
		if d.IsDir() {
			if path != t.b.projDir && t.b.ignore.SoftSkipped(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if t.b.ignore.HardDenied(rel) {
			return nil
		}
		if include != "" {
			if ok, _ := filepath.Match(include, filepath.Base(path)); !ok {
				return nil
			}
		}
		fi, err := d.Info()
		if err != nil || fi.Size() > 2<<20 {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		ln := 0
		for sc.Scan() {
			ln++
			if re.Match(sc.Bytes()) {
				out = append(out, fmt.Sprintf("%s:%d:%s", rel, ln, truncateLine(sc.Text())))
				count++
				if count >= maxGrepMatches {
					break
				}
			}
		}
		return nil
	})
	if len(out) == 0 {
		return Result{Content: "tidak ditemukan: " + pattern}, nil
	}
	if count >= maxGrepMatches {
		out = append(out, "(…dibatasi 300 hasil)")
	}
	return Result{Content: strings.Join(out, "\n")}, nil
}

func relativize(projDir string, lines []string) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.ReplaceAll(l, projDir+string(filepath.Separator), "")
	}
	return strings.Join(out, "\n")
}

func truncateLine(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 240 {
		return s[:240] + "…"
	}
	return s
}

// ---- ls ----

type lsTool struct{ b *base }

func (t lsTool) Name() string { return "ls" }
func (t lsTool) Description() string {
	return "Daftar isi direktori, menghormati .gitignore. Direktori ditandai dengan /."
}
func (t lsTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Direktori (default root proyek)"},
		},
	}
}
func (t lsTool) DefaultPerm() perm { return permAllow }
func (t lsTool) ReadOnly() bool    { return true }
func (t lsTool) Exec(ctx context.Context, args map[string]any) (Result, error) {
	path := t.b.projDir
	if p := argString(args, "path"); p != "" {
		r, err := resolvePath(t.b.projDir, p)
		if err != nil {
			return Result{}, err
		}
		path = r
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return Result{Err: true, Content: err.Error()}, nil
	}
	var dirs, files []string
	skipped := 0
	for _, e := range entries {
		rel, _ := filepath.Rel(t.b.projDir, filepath.Join(path, e.Name()))
		if t.b.ignore.SoftSkipped(rel) || t.b.ignore.HardDenied(rel) {
			skipped++
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, e.Name()+"/")
		} else {
			files = append(files, e.Name())
		}
	}
	sort.Strings(dirs)
	sort.Strings(files)
	all := append(dirs, files...)
	if len(all) == 0 {
		return Result{Content: "(direktori kosong)"}, nil
	}
	note := ""
	if skipped > 0 {
		note = fmt.Sprintf("\n(%d entri dikecualikan oleh .gitignore/.jenderalignore)", skipped)
	}
	if len(all) > maxListResults {
		all = all[:maxListResults]
		note = "\n(…dibatasi 500 entri)" + note
	}
	return Result{Content: strings.Join(all, "\n") + note}, nil
}
