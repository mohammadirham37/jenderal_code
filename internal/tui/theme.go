// Package tui mengimplementasikan antarmuka terminal interaktif (Bubble
// Tea): area chat dengan Markdown, dialog izin, pemilih model/sesi/agent,
// status bar, tema, dan dua bahasa (id/en).
package tui

import "github.com/charmbracelet/lipgloss"

// Theme palet warna satu tema.
type Theme struct {
	Name      string
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Accent    lipgloss.Color
	Text      lipgloss.Color
	Muted     lipgloss.Color
	Border    lipgloss.Color
	Error     lipgloss.Color
	Success   lipgloss.Color
	DiffAdd   lipgloss.Color
	DiffDel   lipgloss.Color
	UserBg    lipgloss.Color
	UserFg    lipgloss.Color
}

// themes semua tema bawaan (PRD 9.4).
var themes = map[string]Theme{
	"jenderal": {
		Name: "jenderal", Primary: lipgloss.Color("#5B8C4A"), Secondary: lipgloss.Color("#D4A017"),
		Accent: lipgloss.Color("#8FBC6F"), Text: lipgloss.Color("#E8E6E3"), Muted: lipgloss.Color("#8A9A7B"),
		Border: lipgloss.Color("#3E5A32"), Error: lipgloss.Color("#E06C75"), Success: lipgloss.Color("#98C379"),
		DiffAdd: lipgloss.Color("#98C379"), DiffDel: lipgloss.Color("#E06C75"),
		UserBg: lipgloss.Color("#2F4A26"), UserFg: lipgloss.Color("#F5F1E8"),
	},
	"dark": {
		Name: "dark", Primary: lipgloss.Color("#61AFEF"), Secondary: lipgloss.Color("#C678DD"),
		Accent: lipgloss.Color("#56B6C2"), Text: lipgloss.Color("#DCDFE4"), Muted: lipgloss.Color("#5C6370"),
		Border: lipgloss.Color("#3E4451"), Error: lipgloss.Color("#E06C75"), Success: lipgloss.Color("#98C379"),
		DiffAdd: lipgloss.Color("#98C379"), DiffDel: lipgloss.Color("#E06C75"),
		UserBg: lipgloss.Color("#282C34"), UserFg: lipgloss.Color("#FFFFFF"),
	},
	"light": {
		Name: "light", Primary: lipgloss.Color("#0B6E99"), Secondary: lipgloss.Color("#8250DF"),
		Accent: lipgloss.Color("#0969DA"), Text: lipgloss.Color("#1F2328"), Muted: lipgloss.Color("#6E7781"),
		Border: lipgloss.Color("#D0D7DE"), Error: lipgloss.Color("#CF222E"), Success: lipgloss.Color("#1A7F37"),
		DiffAdd: lipgloss.Color("#1A7F37"), DiffDel: lipgloss.Color("#CF222E"),
		UserBg: lipgloss.Color("#DDF4E4"), UserFg: lipgloss.Color("#1F2328"),
	},
	"catppuccin": {
		Name: "catppuccin", Primary: lipgloss.Color("#89B4FA"), Secondary: lipgloss.Color("#CBA6F7"),
		Accent: lipgloss.Color("#94E2D5"), Text: lipgloss.Color("#CDD6F4"), Muted: lipgloss.Color("#6C7086"),
		Border: lipgloss.Color("#45475A"), Error: lipgloss.Color("#F38BA8"), Success: lipgloss.Color("#A6E3A1"),
		DiffAdd: lipgloss.Color("#A6E3A1"), DiffDel: lipgloss.Color("#F38BA8"),
		UserBg: lipgloss.Color("#313244"), UserFg: lipgloss.Color("#CDD6F4"),
	},
	"tokyonight": {
		Name: "tokyonight", Primary: lipgloss.Color("#7AA2F7"), Secondary: lipgloss.Color("#BB9AF7"),
		Accent: lipgloss.Color("#7DCFFF"), Text: lipgloss.Color("#C0CAF5"), Muted: lipgloss.Color("#565F89"),
		Border: lipgloss.Color("#292E42"), Error: lipgloss.Color("#F7768E"), Success: lipgloss.Color("#9ECE6A"),
		DiffAdd: lipgloss.Color("#9ECE6A"), DiffDel: lipgloss.Color("#F7768E"),
		UserBg: lipgloss.Color("#24283B"), UserFg: lipgloss.Color("#C0CAF5"),
	},
}

// LoadTheme mengambil tema berdasarkan nama; tak dikenal → jenderal.
func LoadTheme(name string) Theme {
	if t, ok := themes[name]; ok {
		return t
	}
	return themes["jenderal"]
}

// ThemeNames daftar nama tema terurut.
func ThemeNames() []string {
	out := make([]string, 0, len(themes))
	for n := range themes {
		out = append(out, n)
	}
	// urut manual agar tema utama di depan
	order := []string{"jenderal", "dark", "light", "catppuccin", "tokyonight"}
	var res []string
	seen := map[string]bool{}
	for _, o := range order {
		res = append(res, o)
		seen[o] = true
	}
	for _, n := range out {
		if !seen[n] {
			res = append(res, n)
		}
	}
	return res
}
