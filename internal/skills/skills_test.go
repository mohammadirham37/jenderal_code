package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestListBuiltin memastikan skill bawaan tertanam dan terbaca.
func TestListBuiltin(t *testing.T) {
	list := List(t.TempDir())
	if len(list) == 0 {
		t.Fatal("tidak ada skill bawaan tertanam")
	}
	found := false
	for _, sk := range list {
		if sk.Name == "buat-dokumen" {
			found = true
			if !sk.Builtin {
				t.Error("buat-dokumen harusnya ditandai bawaan")
			}
			if sk.Description == "" || sk.Body == "" {
				t.Error("deskripsi/body buat-dokumen kosong")
			}
			if !strings.Contains(sk.Body, "write_docx") {
				t.Error("body buat-dokumen harus menyebut write_docx")
			}
		}
	}
	if !found {
		t.Error("skill bawaan buat-dokumen tidak ditemukan")
	}
}

// TestProjectOverride skill di proyek harus menimpa bawaan bernama sama
// dan Get harus menemukannya.
func TestProjectOverride(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, ".jenderal", "skills", "buat-dokumen")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: buat-dokumen\ndescription: versi proyek\n---\nIsi versi proyek."
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sk, err := Get(dir, "buat-dokumen")
	if err != nil {
		t.Fatal(err)
	}
	if sk.Builtin {
		t.Error("harus tergantikan versi proyek")
	}
	if sk.Description != "versi proyek" || !strings.Contains(sk.Body, "versi proyek") {
		t.Errorf("frontmatter/body proyek tidak terbaca: %+v", sk)
	}
}

// TestGetNotFound error jelas bila skill tidak ada.
func TestGetNotFound(t *testing.T) {
	if _, err := Get(t.TempDir(), "tidak-ada"); err == nil {
		t.Error("harusnya error")
	}
}

// TestParseTanpaFrontmatter file tanpa frontmatter tetap terbaca.
func TestParseTanpaFrontmatter(t *testing.T) {
	sk := parse("sederhana", "isi mentah")
	if sk.Name != "sederhana" || sk.Body != "isi mentah" {
		t.Errorf("parse salah: %+v", sk)
	}
}
