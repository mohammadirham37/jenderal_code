package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mohammadirham37/jenderal_code/internal/config"
)

func TestSameCommit(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"abc123", "abc123", true},
		{"ABC123", "abc123", true},
		{"abc123def456", "abc123", true}, // full hash vs short
		{"abc123", "abc123def456", true},
		{"", "abc123", false},
		{"abc123", "abd123", false},
	}
	for _, c := range cases {
		if got := sameCommit(c.a, c.b); got != c.want {
			t.Errorf("sameCommit(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestShortOf(t *testing.T) {
	full := "0123456789abcdef0123456789abcdef01234567"
	if got := shortOf(full); got != "0123456789ab" {
		t.Errorf("shortOf(full) = %q", got)
	}
	if got := shortOf("abc"); got != "abc" {
		t.Errorf("shortOf(abc) = %q", got)
	}
}

func TestDetectSourceDir(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "cmd", "jenderalcode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module "+modulePath+"\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(repo, "cmd", "jenderalcode", "jenderal")
	got, err := detectSourceDir(exe)
	if err != nil {
		t.Fatalf("detectSourceDir: %v", err)
	}
	if got != repo {
		t.Errorf("detectSourceDir = %q, want %q", got, repo)
	}

	// Bukan repo jenderalcode → harus gagal.
	other := t.TempDir()
	if _, err := detectSourceDir(filepath.Join(other, "jenderalcode")); err == nil {
		t.Error("detectSourceDir berhasil di luar repo; harusnya error")
	}
}

func TestIsJenderalSourceModuleMismatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module lain/ajaib\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isJenderalSource(dir) {
		t.Error("go.mod module berbeda harusnya tidak dikenali sebagai source jenderalcode")
	}
}

// TestSourceHint menyimpan lalu membaca ulang lokasi repo; path tidak valid
// harus diabaikan.
func TestSourceHint(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JENDERAL_DATA_DIR", filepath.Join(dir, "data"))
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if got := loadSourceHint(); got != "" {
		t.Errorf("hint awal harus kosong, dapat %q", got)
	}
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module "+modulePath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	saveSourceHint(repo)
	if got := loadSourceHint(); got != repo {
		t.Errorf("hint = %q, want %q", got, repo)
	}
	// Repo sudah tidak valid → hint diabaikan.
	os.RemoveAll(filepath.Join(repo, ".git"))
	if got := loadSourceHint(); got != "" {
		t.Errorf("hint harus dibuang bila repo tidak valid, dapat %q", got)
	}
}

// TestCommitFromVersion membedah pseudo-version go install dan tag polos.
func TestCommitFromVersion(t *testing.T) {
	cases := map[string]string{
		"v0.0.0-20261006120000-abc123def456":   "abc123def456",
		"v1.2.3-0.20261006120000-0123456789ab": "0123456789ab",
		"(devel)":                              "",
		"":                                     "",
		"v0.1.0":                               "v0.1.0",
		"v0.0.0-20261006120000-abc123def456-extra": "",
	}
	for v, want := range cases {
		if got := commitFromVersion(v); got != want {
			t.Errorf("commitFromVersion(%q) = %q, want %q", v, got, want)
		}
	}
}
