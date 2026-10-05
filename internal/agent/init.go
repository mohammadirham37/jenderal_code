package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// InitProject menganalisis repo dan menghasilkan isi JENDERAL.md awal
// (dipakai `jenderalcode init` dan slash command /init).
func InitProject(projDir string) string {
	var b strings.Builder
	name := filepath.Base(projDir)
	b.WriteString("# JENDERAL — Konteks Proyek\n\n")
	b.WriteString("File ini dibaca otomatis oleh JenderalCode setiap sesi. Tulis aturan tim, konvensi kode, dan perintah penting di sini.\n\n")

	fmt.Fprintf(&b, "## Ringkasan proyek\n\n- Nama: %s\n", name)
	if desc := detectDescription(projDir); desc != "" {
		fmt.Fprintf(&b, "- Deskripsi: %s\n", desc)
	}
	langs := detectLanguages(projDir)
	if len(langs) > 0 {
		fmt.Fprintf(&b, "- Bahasa utama: %s\n", strings.Join(langs, ", "))
	}
	if br := gitBranchQuiet(projDir); br != "" {
		fmt.Fprintf(&b, "- Branch default saat analisis: %s\n", br)
	}
	b.WriteString("\n")

	if cmds := detectBuildCommands(projDir); len(cmds) > 0 {
		b.WriteString("## Perintah penting\n\n")
		for _, c := range cmds {
			fmt.Fprintf(&b, "- `%s`\n", c)
		}
		b.WriteString("\n")
	}

	if len(langs) > 0 {
		b.WriteString("## Konvensi kode\n\n")
		b.WriteString("- Ikuti gaya kode yang sudah ada di file sekitar.\n")
		b.WriteString("- Jalankan test sebelum menyatakan selesai.\n\n")
	}

	b.WriteString("## Aturan tim\n\n")
	b.WriteString("- (tambahkan aturan tim Anda di sini)\n")
	return b.String()
}

// detectLanguages menghitung ekstensi file untuk menebak bahasa.
func detectLanguages(dir string) []string {
	count := map[string]int{}
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "dist", "build", "__pycache__", "target":
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.TrimPrefix(filepath.Ext(d.Name()), ".")
		if ext == "" {
			return nil
		}
		count[ext]++
		return nil
	})
	langByExt := map[string]string{
		"go": "Go", "rs": "Rust", "py": "Python", "ts": "TypeScript", "tsx": "TypeScript",
		"js": "JavaScript", "jsx": "JavaScript", "java": "Java", "kt": "Kotlin",
		"rb": "Ruby", "php": "PHP", "c": "C", "h": "C", "cpp": "C++", "cs": "C#",
		"swift": "Swift", "dart": "Dart", "sh": "Shell", "lua": "Lua",
	}
	seen := map[string]bool{}
	type kv struct {
		ext   string
		count int
	}
	var list []kv
	for e, c := range count {
		if l, ok := langByExt[e]; ok && c >= 3 {
			list = append(list, kv{e, c})
			_ = l
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].count > list[j].count })
	var out []string
	for _, kv := range list {
		l := langByExt[kv.ext]
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func detectDescription(dir string) string {
	for _, f := range []string{"README.md", "readme.md", "README", "go.mod", "package.json"} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			continue
		}
		for _, ln := range strings.Split(string(b), "\n") {
			ln = strings.TrimSpace(ln)
			if ln == "" || strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, "//") || strings.HasPrefix(ln, "{") {
				if s := strings.TrimPrefix(ln, "# "); s != ln && len(s) > 8 {
					return strings.Trim(s, "# ")
				}
				continue
			}
			if len(ln) > 8 {
				return ln
			}
		}
	}
	return ""
}

func detectBuildCommands(dir string) []string {
	var cmds []string
	if fileExists(dir, "Makefile") {
		cmds = append(cmds, "make", "make test")
	}
	if fileExists(dir, "go.mod") {
		cmds = append(cmds, "go build ./...", "go test ./...")
	}
	if fileExists(dir, "package.json") {
		cmds = append(cmds, "npm install", "npm test")
	}
	if fileExists(dir, "Cargo.toml") {
		cmds = append(cmds, "cargo build", "cargo test")
	}
	if fileExists(dir, "pyproject.toml") || fileExists(dir, "requirements.txt") {
		cmds = append(cmds, "pytest")
	}
	return cmds
}

func fileExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func gitBranchQuiet(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
