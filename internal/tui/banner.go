package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// bannerLetters huruf gaya ANSI Shadow, enam baris per huruf. Panjang
// setiap baris huruf harus konsisten (dijaga oleh TestBannerWord).
var bannerLetters = map[string][]string{
	"J": {
		"     ██╗",
		"     ██║",
		"     ██║",
		"██   ██║",
		"╚█████╔╝",
		" ╚════╝ ",
	},
	"E": {
		"███████╗",
		"██╔════╝",
		"█████╗  ",
		"██╔══╝  ",
		"███████╗",
		"╚══════╝",
	},
	"N": {
		"███╗   ██╗",
		"████╗  ██║",
		"██╔██╗ ██║",
		"██║╚██╗██║",
		"██║ ╚████║",
		"╚═╝  ╚═══╝",
	},
	"D": {
		"██████╗ ",
		"██╔══██╗",
		"██║  ██║",
		"██║  ██║",
		"██████╔╝",
		"╚═════╝ ",
	},
	"R": {
		"██████╗ ",
		"██╔══██╗",
		"██████╔╝",
		"██╔══██╗",
		"██║  ██║",
		"╚═╝  ╚═╝",
	},
	"A": {
		" █████╗ ",
		"██╔══██╗",
		"███████║",
		"██╔══██║",
		"██║  ██║",
		"╚═╝  ╚═╝",
	},
	"L": {
		"██╗     ",
		"██║     ",
		"██║     ",
		"██║     ",
		"███████╗",
		"╚══════╝",
	},
	"C": {
		" ██████╗",
		"██╔════╝",
		"██║     ",
		"██║     ",
		"╚██████╗",
		" ╚═════╝",
	},
	"O": {
		" ██████╗ ",
		"██╔═══██╗",
		"██║   ██║",
		"██║   ██║",
		"╚██████╔╝",
		" ╚═════╝ ",
	},
}

// bannerWord menyusun kata menjadi enam baris huruf besar berjeda satu spasi.
// Semua baris persis sama lebar (spasi di ujung sengaja dipertahankan agar
// perataan kolom terjaga).
func bannerWord(word string) []string {
	rows := make([]string, 6)
	for i := range rows {
		parts := make([]string, 0, len(word))
		for _, ch := range word {
			parts = append(parts, bannerLetters[string(ch)][i])
		}
		rows[i] = strings.Join(parts, " ")
	}
	return rows
}

// bannerLebar lebar minimum layar untuk banner penuh; di bawah ini dipakai
// versi satu baris agar tidak terpotong.
const bannerLebar = 76

// renderBannerBlock menyusun blok banner tampilan awal: huruf besar dua
// warna, tagline + versi, tips tombol, dan garis pemisah.
func (m *Model) renderBannerBlock() string {
	prim := lipgloss.NewStyle().Foreground(m.th.Primary).Bold(true)
	acc := lipgloss.NewStyle().Foreground(m.th.Accent).Bold(true)

	if m.width < bannerLebar {
		title := prim.Render("⬢ JenderalCode")
		return title + m.th.MutedStyle().Render("  "+m.metaLine())
	}

	var art []string
	art = append(art, bannerWord("JENDERAL")...)
	art = append(art, bannerWord("CODE")...)
	for i := range art {
		style := prim
		if i >= 6 {
			style = acc
		}
		art[i] = style.Render(art[i])
	}

	ruleW := m.width - 4
	if ruleW > 74 {
		ruleW = 74
	}
	if ruleW < 10 {
		ruleW = 10
	}
	lines := append(art, "",
		m.th.MutedStyle().Render(m.metaLine()),
		m.th.MutedStyle().Render(m.lang.get("send_hint")),
		lipgloss.NewStyle().Foreground(m.th.Border).Render(strings.Repeat("─", ruleW)),
	)
	return strings.Join(lines, "\n")
}

// metaLine versi + tagline untuk bagian bawah banner.
func (m *Model) metaLine() string {
	meta := "jenderalcode"
	if v := strings.TrimSpace(m.app.Version); v != "" {
		meta += " v" + v
	}
	return meta + " — " + m.lang.get("banner_tagline")
}
