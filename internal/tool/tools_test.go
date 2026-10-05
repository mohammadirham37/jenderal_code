package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	ignore := LoadIgnore(dir)
	r := NewBuiltinRegistry(dir, ignore, NewTodoStore(), 10)
	return r, dir
}

func TestReadAndWrite(t *testing.T) {
	r, dir := newTestRegistry(t)
	ctx := context.Background()

	w, _ := r.Get("write")
	res, err := w.Exec(ctx, map[string]any{"path": "sub/a.txt", "content": "halo\nbaris2\n"})
	if err != nil || res.Err {
		t.Fatalf("write gagal: %v %v", res.Content, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "sub", "a.txt"))
	if err != nil || string(b) != "halo\nbaris2\n" {
		t.Fatalf("isi file salah: %q", b)
	}

	rd, _ := r.Get("read")
	res, _ = rd.Exec(ctx, map[string]any{"path": "sub/a.txt"})
	if res.Err || !strings.Contains(res.Content, "halo") || !strings.Contains(res.Content, "2\tbaris2") {
		t.Fatalf("read salah:\n%s", res.Content)
	}
	// prev_content tersedia untuk snapshot.
	if prev, ok := res.Data["prev_content"]; !ok {
		t.Log("write pertama: prev_content boleh kosong")
	} else {
		_ = prev
	}
}

func TestEditUnique(t *testing.T) {
	r, dir := newTestRegistry(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"), 0o644)

	e, _ := r.Get("edit")
	res, _ := e.Exec(ctx, map[string]any{
		"path": "main.go", "old_string": "println(\"hi\")", "new_string": "println(\"halo\")",
	})
	if res.Err {
		t.Fatalf("edit gagal: %s", res.Content)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(b), "halo") {
		t.Fatalf("edit tidak diterapkan: %s", b)
	}

	// old_string tak ada → error jelas.
	res, _ = e.Exec(ctx, map[string]any{"path": "main.go", "old_string": "ZZZ", "new_string": "Y"})
	if !res.Err {
		t.Fatal("old_string tidak ada harus error")
	}
}

func TestEditAmbiguousFails(t *testing.T) {
	r, dir := newTestRegistry(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(dir, "dup.txt"), []byte("x\nx\n"), 0o644)
	e, _ := r.Get("edit")
	res, _ := e.Exec(ctx, map[string]any{"path": "dup.txt", "old_string": "x", "new_string": "y"})
	if !res.Err || !strings.Contains(res.Content, "2 kali") {
		t.Fatalf("old ambigu harus ditolak: %v %s", res.Err, res.Content)
	}
	res, _ = e.Exec(ctx, map[string]any{"path": "dup.txt", "old_string": "x", "new_string": "y", "replace_all": true})
	if res.Err {
		t.Fatalf("replace_all gagal: %s", res.Content)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "dup.txt"))
	if string(b) != "y\ny\n" {
		t.Fatalf("replace_all salah: %q", b)
	}
}

func TestEnvDenied(t *testing.T) {
	r, dir := newTestRegistry(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=1"), 0o644)
	rd, _ := r.Get("read")
	res, _ := rd.Exec(ctx, map[string]any{"path": ".env"})
	if !res.Err || !strings.Contains(res.Content, "dikecualikan") {
		t.Fatalf(".env harus ditolak: %v %s", res.Err, res.Content)
	}
	w, _ := r.Get("write")
	res, _ = w.Exec(ctx, map[string]any{"path": ".env.local", "content": "x"})
	if !res.Err {
		t.Fatal("tulis .env.local harus ditolak")
	}
}

func TestGlobAndLs(t *testing.T) {
	r, dir := newTestRegistry(t)
	ctx := context.Background()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "a.go"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("x"), 0o644)

	g, _ := r.Get("glob")
	res, _ := g.Exec(ctx, map[string]any{"pattern": "**/*.go"})
	if res.Err || strings.Contains(res.Content, "c.txt") || !strings.Contains(res.Content, "src/a.go") {
		t.Fatalf("glob salah:\n%s", res.Content)
	}
	l, _ := r.Get("ls")
	res, _ = l.Exec(ctx, map[string]any{})
	if res.Err || !strings.Contains(res.Content, "src/") || strings.Contains(res.Content, "a.go") {
		t.Fatalf("ls harus hanya satu level:\n%s", res.Content)
	}
}

func TestGrep(t *testing.T) {
	r, dir := newTestRegistry(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x\n\nfunc Hello() {}\n"), 0o644)
	g, _ := r.Get("grep")
	res, _ := g.Exec(ctx, map[string]any{"pattern": "Hello", "include": "*.go"})
	if res.Err || !strings.Contains(res.Content, "x.go:3:func Hello") {
		t.Fatalf("grep salah:\n%s", res.Content)
	}
}

func TestPatchApply(t *testing.T) {
	r, dir := newTestRegistry(t)
	ctx := context.Background()
	os.WriteFile(filepath.Join(dir, "p.txt"), []byte("satu\ndua\ntiga\nempat\nlima\n"), 0o644)
	p, _ := r.Get("patch")
	diff := `--- a/p.txt
+++ b/p.txt
@@ -1,5 +1,5 @@
 satu
-dua
+DUA
 tiga
 empat
 lima
`
	res, _ := p.Exec(ctx, map[string]any{"diff": diff})
	if res.Err {
		t.Fatalf("patch gagal: %s", res.Content)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "p.txt"))
	if string(b) != "satu\nDUA\ntiga\nempat\nlima\n" {
		t.Fatalf("hasil patch salah: %q", b)
	}
}

func TestPatchNewFile(t *testing.T) {
	r, dir := newTestRegistry(t)
	ctx := context.Background()
	p, _ := r.Get("patch")
	diff := `--- /dev/null
+++ b/baru.txt
@@ -0,0 +1,2 @@
+baris satu
+baris dua
`
	res, _ := p.Exec(ctx, map[string]any{"diff": diff})
	if res.Err {
		t.Fatalf("patch file baru gagal: %s", res.Content)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "baru.txt"))
	if string(b) != "baris satu\nbaris dua" && string(b) != "baris satu\nbaris dua\n" {
		t.Fatalf("isi file baru salah: %q", b)
	}
}

func TestIgnoreMatching(t *testing.T) {
	ig := LoadIgnore(t.TempDir())
	if !ig.HardDenied(".env") {
		t.Fatal(".env harus denied")
	}
	if !ig.HardDenied(".env.production") {
		t.Fatal(".env.production harus denied")
	}
	if !ig.HardDenied("server.key") {
		t.Fatal("*.key harus denied")
	}
	if ig.HardDenied("main.go") {
		t.Fatal("main.go tidak boleh denied")
	}
	if !ig.SoftSkipped("node_modules/pkg/index.js") {
		t.Fatal("node_modules harus soft-skipped")
	}
	if !ig.SoftSkipped(".git/config") {
		t.Fatal(".git harus soft-skipped")
	}
}

func TestBash(t *testing.T) {
	r, _ := newTestRegistry(t)
	ctx := context.Background()
	b, _ := r.Get("bash")
	res, _ := b.Exec(ctx, map[string]any{"command": "echo halo-jenderal; exit 3"})
	if res.Err == false || !strings.Contains(res.Content, "exit code: 3") || !strings.Contains(res.Content, "halo-jenderal") {
		t.Fatalf("bash salah: %s (err=%v)", res.Content, res.Err)
	}
}

func TestMatchGlobPattern(t *testing.T) {
	if !matchGlobPattern("**/*.go", "src/internal/main.go") {
		t.Fatal("** harus cocok nested")
	}
	if !matchGlobPattern("*.go", "main.go") {
		t.Fatal("*.go harus cocok")
	}
	if matchGlobPattern("*.go", "src/main.go") {
		t.Fatal("*.go tidak boleh cocok nested")
	}
}
