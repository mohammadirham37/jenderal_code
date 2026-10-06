// Package skills menyediakan sistem "skill": paket instruksi Markdown
// (SKILL.md) yang bisa dimuat agen maupun pengguna untuk kemampuan khusus,
// mis. membuat dokumen Word/PowerPoint/Excel.
//
// Sumber skill berlapis (nama sama: proyek menang):
//   - bawaan tertanam di binary (embedded)
//   - global  ~/.config/jenderalcode/skills/<nama>/SKILL.md
//   - proyek  <proyek>/.jenderal/skills/<nama>/SKILL.md
package skills

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed builtin/*/*.md
var builtinFS embed.FS

// Skill satu paket instruksi.
type Skill struct {
	Name        string // nama unik (nama folder)
	Description string // satu baris untuk daftar pilihan
	Body        string // isi instruksi (Markdown) setelah frontmatter
	Path        string // sumber file (kosong untuk bawaan)
	Builtin     bool   // true bila tertanam di binary
}

// globalDir lokasi skill global pengguna.
func globalDir() string {
	if v := os.Getenv("JENDERAL_CONFIG_DIR"); v != "" {
		return filepath.Join(v, "skills")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "jenderalcode", "skills")
}

// List mengumpulkan semua skill yang tersedia untuk proyek.
// Hasil diurutkan: bawaan dulu, lalu abjad.
func List(projectDir string) []Skill {
	seen := map[string]bool{}
	var out []Skill

	// Bawaan (embedded).
	if entries, err := fs.ReadDir(builtinFS, "builtin"); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			b, err := builtinFS.ReadFile("builtin/" + e.Name() + "/SKILL.md")
			if err != nil {
				continue
			}
			sk := parse(e.Name(), string(b))
			sk.Builtin = true
			seen[sk.Name] = true
			out = append(out, sk)
		}
	}

	// Global lalu proyek (proyek menimpa).
	for _, dir := range []string{globalDir(), filepath.Join(projectDir, ".jenderal", "skills")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name(), "SKILL.md"))
			if err != nil {
				continue
			}
			sk := parse(e.Name(), string(b))
			sk.Path = filepath.Join(dir, e.Name(), "SKILL.md")
			if seen[sk.Name] {
				for i := range out {
					if out[i].Name == sk.Name {
						out[i] = sk // proyek/global menimpa bawaan
					}
				}
				continue
			}
			seen[sk.Name] = true
			out = append(out, sk)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Builtin != out[j].Builtin {
			return out[i].Builtin
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Get mengambil satu skill berdasarkan nama; fallback ke body mentah bila
// frontmatter tidak lengkap.
func Get(projectDir, name string) (Skill, error) {
	for _, sk := range List(projectDir) {
		if sk.Name == name {
			return sk, nil
		}
	}
	return Skill{}, fmt.Errorf("skill %q tidak ditemukan; lihat daftar dengan /skills", name)
}

// parse memisahkan frontmatter "key: value" dari body.
func parse(dirName, raw string) Skill {
	sk := Skill{Name: dirName, Body: strings.TrimSpace(raw)}
	body, ok := strings.CutPrefix(raw, "---\n")
	if !ok {
		return sk
	}
	head, rest, ok := strings.Cut(body, "\n---")
	if !ok {
		return sk
	}
	for _, ln := range strings.Split(head, "\n") {
		k, v, ok := strings.Cut(ln, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "name":
			if v != "" {
				sk.Name = v
			}
		case "description":
			sk.Description = v
		}
	}
	sk.Body = strings.TrimSpace(strings.TrimPrefix(rest, "\n"))
	return sk
}
