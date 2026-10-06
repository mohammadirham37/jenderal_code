package tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// renderMarkdown adalah renderer Markdown ringan untuk TUI: heading,
// list, blok kode berbingkai, diff berwarna, bold/italic/inline-code.
// Semua baris dibungkus ke lebar layar (wrapANSI).
// Cukup untuk kebutuhan chat tanpa dependensi berat.
func renderMarkdown(src string, th Theme, width int) string {
	if width < 20 {
		width = 20
	}
	inner := width - 4
	var out []string
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	inCode := false
	var codeBuf []string
	codeLang := ""

	flushCode := func() {
		if len(codeBuf) == 0 {
			out = append(out, codeStyle(th).Width(inner).Render(""))
			return
		}
		body := ""
		for _, l := range codeBuf {
			styled := l
			switch {
			case strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++"):
				styled = lipgloss.NewStyle().Foreground(th.DiffAdd).Render(l)
			case strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---"):
				styled = lipgloss.NewStyle().Foreground(th.DiffDel).Render(l)
			}
			for _, w := range wrapANSI(styled, inner-2) {
				body += w + "\n"
			}
		}
		title := codeLang
		if title == "" {
			title = "kode"
		}
		box := codeStyle(th).Width(inner).Render(strings.TrimSuffix(body, "\n"))
		out = append(out, box, "")
	}

	for _, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "```"):
			if inCode {
				flushCode()
				codeBuf = nil
				codeLang = ""
				inCode = false
			} else {
				inCode = true
				codeLang = strings.TrimSpace(strings.TrimPrefix(ln, "```"))
			}
		case inCode:
			codeBuf = append(codeBuf, ln)
		default:
			out = append(out, renderLine(ln, th, width)...)
		}
	}
	if inCode {
		flushCode()
	}
	return strings.Join(out, "\n")
}

func codeStyle(th Theme) lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(th.Border).
		Padding(0, 1)
}

// renderLine merender satu baris Markdown ke slice baris keluaran.
// Semua hasil dibungkus wrapANSI agar tidak melewati lebar layar.
func renderLine(ln string, th Theme, width int) []string {
	inner := width - 4
	wrap := func(s string) []string { return wrapANSI(s, inner) }
	switch {
	case strings.HasPrefix(ln, "### "):
		return append(wrap(headerStyle(th, 3).Render(inline(ln[4:], th))), "")
	case strings.HasPrefix(ln, "## "):
		return append(wrap(headerStyle(th, 2).Render(inline(ln[3:], th))), "")
	case strings.HasPrefix(ln, "# "):
		return append(wrap(headerStyle(th, 1).Render(inline(ln[2:], th))), "")
	case strings.HasPrefix(ln, "- ") || strings.HasPrefix(ln, "* "):
		return wrap("  " + th.MutedStyle().Render("• ") + inline(ln[2:], th))
	case len(ln) > 2 && ln[0] >= '0' && ln[0] <= '9' && (ln[1] == '.' || ln[1] == ')'):
		return wrap("  " + th.MutedStyle().Render(ln[:2]) + " " + inline(strings.TrimSpace(ln[2:]), th))
	case strings.HasPrefix(ln, "> "):
		return wrap(th.MutedStyle().Render("│ " + inline(ln[2:], th)))
	case strings.HasPrefix(ln, "+") && !strings.HasPrefix(ln, "+++"):
		return wrap(lipgloss.NewStyle().Foreground(th.DiffAdd).Render(inline(ln, th)))
	case strings.HasPrefix(ln, "-") && !strings.HasPrefix(ln, "---"):
		return wrap(lipgloss.NewStyle().Foreground(th.DiffDel).Render(inline(ln, th)))
	case strings.TrimSpace(ln) == "":
		return []string{""}
	default:
		return wrap(inline(ln, th))
	}
}

var reSGR = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// wrapANSI membungkus string (yang mungkin mengandung kode ANSI) ke lebar
// tertentu, memotong di batas kata. Status warna yang sedang terbuka
// diteruskan ke baris berikutnya, sehingga teks bold berlanjut benar.
func wrapANSI(s string, width int) []string {
	if width < 8 {
		width = 8
	}
	type atom struct {
		r   rune
		sgr string // status warna aktif saat rune ini
		w   int
	}
	var atoms []atom
	sgr := ""
	applySGR := func(seq string) {
		if !strings.HasSuffix(seq, "m") {
			return // bukan SGR (mis. erase) — abaikan untuk pewarnaan
		}
		params := seq[2 : len(seq)-1]
		if params == "0" || params == "" {
			sgr = ""
			return
		}
		if !strings.Contains(sgr, seq) {
			sgr += seq
		}
	}
	last := 0
	for _, m := range reSGR.FindAllStringIndex(s, -1) {
		for _, r := range s[last:m[0]] {
			atoms = append(atoms, atom{r: r, sgr: sgr, w: runewidth.RuneWidth(r)})
		}
		applySGR(s[m[0]:m[1]])
		last = m[1]
	}
	for _, r := range s[last:] {
		atoms = append(atoms, atom{r: r, sgr: sgr, w: runewidth.RuneWidth(r)})
	}

	var out []string
	var cur strings.Builder
	curW, emitted := 0, ""
	closeLine := func() {
		if emitted != "" {
			cur.WriteString("\x1b[0m")
		}
		out = append(out, cur.String())
		cur.Reset()
		curW, emitted = 0, ""
	}
	writeAtom := func(a atom) {
		if a.sgr != emitted {
			cur.WriteString(a.sgr)
			emitted = a.sgr
		}
		cur.WriteRune(a.r)
		curW += a.w
	}

	i := 0
	for i < len(atoms) {
		// Spasi: lewati di awal baris, pemisah kata.
		if atoms[i].r == ' ' {
			if curW > 0 && curW+1 <= width {
				writeAtom(atoms[i])
			}
			i++
			continue
		}
		// Kumpulkan satu kata.
		j := i
		wordW := 0
		for j < len(atoms) && atoms[j].r != ' ' {
			wordW += atoms[j].w
			j++
		}
		if curW > 0 && curW+wordW > width {
			closeLine()
		}
		// Kata lebih panjang dari lebar: potong paksa per karakter.
		for k := i; k < j; k++ {
			if curW+atoms[k].w > width && curW > 0 {
				closeLine()
			}
			writeAtom(atoms[k])
		}
		i = j
	}
	if curW > 0 || len(out) == 0 {
		closeLine()
	}
	return out
}

func headerStyle(th Theme, level int) lipgloss.Style {
	s := lipgloss.NewStyle().Foreground(th.Primary).Bold(true)
	switch level {
	case 1:
		return s.Underline(true)
	case 2:
		return s
	default:
		return lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	}
}

var (
	reBold   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reItalic = regexp.MustCompile(`\*([^*]+)\*`)
	reCode   = regexp.MustCompile("`([^`]+)`")
	reLink   = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
)

// inline merender elemen Markdown sebaris.
func inline(s string, th Theme) string {
	s = reCode.ReplaceAllStringFunc(s, func(m string) string {
		inner := strings.Trim(m, "`")
		return lipgloss.NewStyle().Foreground(th.Accent).Render(inner)
	})
	s = reBold.ReplaceAllString(s, "\x1b[1m$1\x1b[0m")
	s = reItalic.ReplaceAllString(s, "\x1b[3m$1\x1b[0m")
	s = reLink.ReplaceAllString(s, "$1 ($2)")
	return s
}

// MutedStyle helper.
func (th Theme) MutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(th.Muted)
}

// formatCost menampilkan biaya dalam mata uang target.
func formatCost(usd float64, currency string, rate float64) string {
	local := usd * rate
	if local >= 1000 {
		return fmt.Sprintf("%s %.0f", currency, local)
	}
	return fmt.Sprintf("%s %.2f", currency, local)
}
