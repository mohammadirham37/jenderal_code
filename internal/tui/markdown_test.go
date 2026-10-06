package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestWrapANSIPlain(t *testing.T) {
	text := strings.Repeat("kata ", 40)
	for _, w := range wrapANSI(text, 20) {
		if got := lipgloss.Width(w); got > 20 {
			t.Errorf("baris melebihi lebar: %d (%q)", got, w)
		}
	}
}

func TestWrapANSIBoldSpan(t *testing.T) {
	// Span bold berisi spasi: saat terpotong, baris lanjutan harus tetap bold.
	text := "\x1b[1m" + strings.Repeat("teks tebal ", 10) + "\x1b[0m"
	lines := wrapANSI(text, 30)
	if len(lines) < 2 {
		t.Fatalf("harus terpecah beberapa baris, dapat %d", len(lines))
	}
	for i, ln := range lines {
		if !strings.Contains(ln, "\x1b[1m") {
			t.Errorf("baris %d kehilangan style bold: %q", i, ln)
		}
		if strings.Count(ln, "\x1b[0m") != 1 {
			t.Errorf("baris %d reset tidak seimbang: %q", i, ln)
		}
	}
}

func TestWrapANSITokenPanjang(t *testing.T) {
	url := "https://contoh.com/" + strings.Repeat("x", 100)
	lines := wrapANSI("lihat "+url+" sekarang", 40)
	for i, ln := range lines {
		if got := lipgloss.Width(ln); got > 40 {
			t.Errorf("baris %d lebar %d melebihi 40", i, got)
		}
	}
	if !strings.Contains(strings.ReplaceAll(strings.Join(lines, ""), "\x1b[0m", ""), strings.Repeat("x", 100)) {
		t.Error("URL harus lengkap (terpotong hanya per baris)")
	}
}

func TestRenderMarkdownLebar(t *testing.T) {
	th := Theme{Primary: "#fff", Accent: "#fff", Muted: "#fff"}
	md := "## Ringkasan\n\n" + strings.Repeat("kalimat cukup panjang untuk meluber di layar sempit. ", 8) +
		"\n- butir satu dengan kalimat yang juga sengaja dibuat panjang agar wrap\n" +
		"```go\nfmt.Println(\"baris kode yang panjang sekali sehingga harus terpotong di batas lebar\")\n```\n"
	out := renderMarkdown(md, th, 60)
	for i, ln := range strings.Split(out, "\n") {
		// Kotak kode (border + padding) boleh selebar penuh; teks biasa ≤ inner.
		if got := lipgloss.Width(ln); got > 60 {
			t.Errorf("baris %d lebar %d melebihi 60: %q", i, got, ln)
		}
	}
}
