package tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderMarkdown adalah renderer Markdown ringan untuk TUI: heading,
// list, blok kode berbingkai, diff berwarna, bold/italic/inline-code.
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
			switch {
			case strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++"):
				body += lipgloss.NewStyle().Foreground(th.DiffAdd).Render(l) + "\n"
			case strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---"):
				body += lipgloss.NewStyle().Foreground(th.DiffDel).Render(l) + "\n"
			default:
				body += l + "\n"
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
func renderLine(ln string, th Theme, width int) []string {
	switch {
	case strings.HasPrefix(ln, "### "):
		return []string{headerStyle(th, 3).Render(inline(ln[4:], th)), ""}
	case strings.HasPrefix(ln, "## "):
		return []string{headerStyle(th, 2).Render(inline(ln[3:], th)), ""}
	case strings.HasPrefix(ln, "# "):
		return []string{headerStyle(th, 1).Render(inline(ln[2:], th)), ""}
	case strings.HasPrefix(ln, "- ") || strings.HasPrefix(ln, "* "):
		return []string{"  " + th.MutedStyle().Render("• ") + inline(ln[2:], th)}
	case len(ln) > 2 && ln[0] >= '0' && ln[0] <= '9' && (ln[1] == '.' || ln[1] == ')'):
		return []string{"  " + th.MutedStyle().Render(ln[:2]) + " " + inline(strings.TrimSpace(ln[2:]), th)}
	case strings.HasPrefix(ln, "> "):
		return []string{th.MutedStyle().Render("│ " + inline(ln[2:], th))}
	case strings.HasPrefix(ln, "+") && !strings.HasPrefix(ln, "+++"):
		return []string{lipgloss.NewStyle().Foreground(th.DiffAdd).Render(inline(ln, th))}
	case strings.HasPrefix(ln, "-") && !strings.HasPrefix(ln, "---"):
		return []string{lipgloss.NewStyle().Foreground(th.DiffDel).Render(inline(ln, th))}
	case strings.TrimSpace(ln) == "":
		return []string{""}
	default:
		return []string{inline(ln, th)}
	}
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
