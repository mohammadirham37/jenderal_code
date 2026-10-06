package tool

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docTestRegistry registry dengan base untuk uji tool dokumen.
func docTestRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	return NewBuiltinRegistry(dir, LoadIgnore(dir), NewTodoStore(), 30), dir
}

// assertXMLParts memastikan semua file .xml/.rels dalam zip bisa diparsing.
func assertXMLParts(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(f, st.Size())
	if err != nil {
		t.Fatalf("bukan zip OOXML valid: %v", err)
	}
	parts := map[string][]byte{}
	for _, zf := range zr.File {
		rc, err := zf.Open()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		rc.Close()
		parts[zf.Name] = buf.Bytes()
		if strings.HasSuffix(zf.Name, ".xml") || strings.HasSuffix(zf.Name, ".rels") {
			if err := xml.Unmarshal(buf.Bytes(), new(any)); err != nil {
				t.Errorf("%s bukan XML valid: %v", zf.Name, err)
			}
		}
	}
	return parts
}

func TestWriteDocx(t *testing.T) {
	r, _ := docTestRegistry(t)
	path := filepath.Join(t.TempDir(), "laporan.docx")
	res, ok := r.Get("write_docx")
	if !ok {
		t.Fatal("tool write_docx tidak terdaftar")
	}
	out, err := res.Exec(context.Background(), map[string]any{
		"path": path,
		"content": "# Laporan Q3\n\n## Ringkasan\n- Pendapatan **naik** 12%\n- Biaya `turun`\n\n1. Pertama\n2. Kedua\n\n```go\nfmt.Println(\"halo\")\n```\n\n| Bulan | Nilai |\n|---|---|\n| Juli | 100 |\n| Agustus | 200 |\n",
	})
	if err != nil || out.Err {
		t.Fatalf("exec gagal: %v / %s", err, out.Content)
	}
	parts := assertXMLParts(t, path)
	doc := string(parts["word/document.xml"])
	for _, want := range []string{"Laporan Q3", "naik", "turun", "Pertama", "fmt.Println", "Juli"} {
		if !strings.Contains(doc, want) {
			t.Errorf("document.xml tidak memuat %q", want)
		}
	}
	// Ekstensi salah harus ditolak.
	out2, _ := res.Exec(context.Background(), map[string]any{"path": "x.txt", "content": "hi"})
	if !out2.Err {
		t.Error("ekstensi non-.docx harusnya ditolak")
	}
}

func TestWritePptx(t *testing.T) {
	r, _ := docTestRegistry(t)
	path := filepath.Join(t.TempDir(), "deck.pptx")
	res, ok := r.Get("write_pptx")
	if !ok {
		t.Fatal("tool write_pptx tidak terdaftar")
	}
	out, err := res.Exec(context.Background(), map[string]any{
		"path":     path,
		"title":    "Rencana 2027",
		"subtitle": "Tim Jenderal",
		"slides": []any{
			map[string]any{"title": "Ringkasan", "bullets": []any{"Target naik", "- Detail target"}},
			map[string]any{"title": "Penutup", "bullets": []any{"Terima kasih"}},
		},
	})
	if err != nil || out.Err {
		t.Fatalf("exec gagal: %v / %s", err, out.Content)
	}
	parts := assertXMLParts(t, path)
	if _, ok := parts["ppt/slides/slide1.xml"]; !ok {
		t.Error("slide sampul tidak ada")
	}
	if _, ok := parts["ppt/slides/slide3.xml"]; !ok {
		t.Error("slide isi ke-2 tidak ada")
	}
	pres := string(parts["ppt/presentation.xml"])
	if strings.Count(pres, "<p:sldId ") != 3 {
		t.Errorf("presentation harus mendaftar 3 slide: %s", pres)
	}
	if !strings.Contains(string(parts["ppt/slides/slide2.xml"]), "Ringkasan") {
		t.Error("slide 2 harus berisi judul Ringkasan")
	}
	// Tanpa slides harus ditolak.
	out2, _ := res.Exec(context.Background(), map[string]any{"path": path, "slides": []any{}})
	if !out2.Err {
		t.Error("slides kosong harusnya ditolak")
	}
}

func TestWriteXlsx(t *testing.T) {
	r, _ := docTestRegistry(t)
	path := filepath.Join(t.TempDir(), "data.xlsx")
	res, ok := r.Get("write_xlsx")
	if !ok {
		t.Fatal("tool write_xlsx tidak terdaftar")
	}
	out, err := res.Exec(context.Background(), map[string]any{
		"path": path,
		"sheets": []any{
			map[string]any{"name": "Rekap", "rows": []any{
				[]any{"Bulan", "Nilai", "OK"},
				[]any{"Januari", 1500000, true},
				[]any{"Februari", 2500000, false},
			}},
			map[string]any{"name": "Detail", "rows": []any{[]any{"a", "b"}}},
		},
	})
	if err != nil || out.Err {
		t.Fatalf("exec gagal: %v / %s", err, out.Content)
	}
	parts := assertXMLParts(t, path)
	wb := string(parts["xl/workbook.xml"])
	if !strings.Contains(wb, "Rekap") || !strings.Contains(wb, "Detail") {
		t.Errorf("workbook tidak memuat kedua sheet: %s", wb)
	}
	if !strings.Contains(string(parts["xl/sharedStrings.xml"]), "Januari") {
		t.Error("data string tidak tersimpan")
	}
	// Ekstensi salah harus ditolak.
	out2, _ := res.Exec(context.Background(), map[string]any{"path": "x.csv", "sheets": []any{}})
	if !out2.Err {
		t.Error("ekstensi non-.xlsx harusnya ditolak")
	}
}
