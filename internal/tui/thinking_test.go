package tui

import (
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_code/internal/bus"
)

func TestThinkingBlockLive(t *testing.T) {
	m := Model{th: LoadTheme("jenderal"), lang: dicts["id"]}

	// Dua delta reasoning → satu blok thinking streaming.
	m.handleEvent(bus.Event{Type: bus.EventReasoningDelta, Text: "menimbang "})
	m.handleEvent(bus.Event{Type: bus.EventReasoningDelta, Text: "opsi"})
	if len(m.blocks) != 1 || m.blocks[0].kind != "thinking" || !m.blocks[0].streaming {
		t.Fatalf("blok thinking tidak terbentuk: %+v", m.blocks)
	}
	if m.blocks[0].text != "menimbang opsi" {
		t.Errorf("teks reasoning = %q", m.blocks[0].text)
	}
	if !m.thinkOpen {
		t.Error("thinkOpen harus true")
	}
	if out := m.renderBlocks(); !strings.Contains(out, "berpikir") || !strings.Contains(out, "menimbang opsi") {
		t.Errorf("render tidak menampilkan isi thinking: %q", out)
	}

	// Delta jawaban pertama → thinking ditutup jadi ringkasan, jawaban mengalir.
	m.handleEvent(bus.Event{Type: bus.EventTextDelta, Text: "jawabannya"})
	if m.thinkOpen {
		t.Error("thinkOpen harus false setelah jawaban mulai")
	}
	if m.blocks[0].streaming || !strings.Contains(m.blocks[0].text, "berpikir selesai") {
		t.Errorf("blok thinking tidak diringkas: %+v", m.blocks[0])
	}
	if !strings.Contains(m.blocks[0].text, "2 kata") {
		t.Errorf("ringkasan harus memuat jumlah kata: %q", m.blocks[0].text)
	}
	if len(m.blocks) != 2 || m.blocks[1].kind != "assistant" {
		t.Errorf("blok jawaban tidak terbentuk: %+v", m.blocks)
	}

	// Burst reasoning kedua setelah jawaban → blok thinking baru.
	m.handleEvent(bus.Event{Type: bus.EventReasoningDelta, Text: "cek lagi"})
	if len(m.blocks) != 3 || m.blocks[2].kind != "thinking" || !m.blocks[2].streaming {
		t.Errorf("blok thinking kedua tidak terbentuk: %+v", m.blocks)
	}
}
