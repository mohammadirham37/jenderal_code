package cli

import (
	"os"
	"path/filepath"
	"testing"
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
	if err := os.MkdirAll(filepath.Join(repo, "cmd", "jenderal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module "+modulePath+"\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(repo, "cmd", "jenderal", "jenderal")
	got, err := detectSourceDir(exe)
	if err != nil {
		t.Fatalf("detectSourceDir: %v", err)
	}
	if got != repo {
		t.Errorf("detectSourceDir = %q, want %q", got, repo)
	}

	// Bukan repo jenderal → harus gagal.
	other := t.TempDir()
	if _, err := detectSourceDir(filepath.Join(other, "jenderal")); err == nil {
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
		t.Error("go.mod module berbeda harusnya tidak dikenali sebagai source jenderal")
	}
}
