package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/mohammadirham37/jenderal_code/catalog"
	"github.com/mohammadirham37/jenderal_code/internal/config"
)

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }

// layout menyesuaikan ukuran komponen dengan jendela.
func (m *Model) layout() {
	statusH := 1
	inputH := 3
	m.viewport.Width = m.width
	m.viewport.Height = m.height - statusH - inputH - 2
	if m.viewport.Height < 3 {
		m.viewport.Height = 3
	}
	m.input.SetWidth(m.width - 4)
}

// View merender seluruh tampilan.
func (m *Model) View() string {
	if m.width == 0 {
		return "memuat…"
	}
	switch m.view {
	case viewPerm:
		return m.viewPermDialog()
	case viewProvider:
		return m.viewProviderDialog()
	case viewProviderKey:
		return m.viewProviderKeyDialog()
	case viewSkills:
		lines := make([]string, len(m.skillList))
		active := map[string]bool{}
		for _, n := range m.ag.ActiveSkills() {
			active[n] = true
		}
		for i, sk := range m.skillList {
			mark := "  "
			if active[sk.Name] {
				mark = "★ "
			}
			lines[i] = mark + sk.Name + " — " + sk.Description
		}
		return m.viewList(m.lang.get("skills_title"), lines, m.skillIdx)
	case viewModel:
		return m.viewList(m.lang.get("model_title"), m.modelLines(), m.modelIdx)
	case viewSessions:
		lines := []string{"[+] " + m.lang.get("new_session")}
		for _, s := range m.sessions {
			title := s.Title
			if title == "" {
				title = "(tanpa judul)"
			}
			lines = append(lines, fmt.Sprintf("[%s] %s · %s", s.ID, title, s.UpdatedAt.Format("02/01 15:04")))
		}
		return m.viewList(m.lang.get("session_title"), lines, m.sessIdx)
	case viewAgents:
		lines := []string{"[default] jenderal (agen utama)"}
		for _, a := range m.agentsAll {
			lines = append(lines, fmt.Sprintf("[%s] %s — %s", a.Name, a.Name, a.Description))
		}
		return m.viewList(m.lang.get("agent_title"), lines, m.agentIdx)
	case viewPalette:
		return m.viewList(m.lang.get("palette_title"), m.paletteLines(), m.palIdx)
	case viewHelp:
		return m.viewHelp()
	case viewCost:
		return m.viewCost()
	}
	return m.viewChat()
}

// viewChat tampilan utama: chat + input + status bar.
func (m *Model) viewChat() string {
	border := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(m.th.Border).Padding(0, 1)
	inputBox := border.Render(m.input.View())
	return lipgloss.JoinVertical(lipgloss.Left,
		m.viewport.View(),
		inputBox,
		m.statusBar(),
	)
}

// statusBar baris status bawah (PRD 9.4).
func (m *Model) statusBar() string {
	st := lipgloss.NewStyle().Background(m.th.Border).Foreground(m.th.Text).Width(m.width)
	model := m.ag.Model
	if model == "" {
		model = m.lang.get("no_key")
	}
	mode := m.lang.get("mode_build")
	if m.ag.Mode == "plan" {
		mode = lipgloss.NewStyle().Background(m.th.Secondary).Foreground(lipgloss.Color("#000000")).
			Render(m.lang.get("mode_plan"))
	}
	left := fmt.Sprintf(" %s │ %s │ ", model, mode)
	ctx := m.contextInfo()
	cost := formatCost(m.costUSD, m.app.Cfg.Currency(), m.app.Cfg.ExchangeRate())
	right := fmt.Sprintf("%s │ %s ", ctx, cost)
	if m.branch != "" {
		right += fmt.Sprintf("│ %s ", m.branch)
	}
	if m.streaming {
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		right = fmt.Sprintf("%s %s │ %s", frames[m.spinIdx], m.statusOrThinking(), right)
	} else if m.statusMsg != "" {
		right = m.statusMsg + " │ " + right
	}
	line := left + right
	if w := lipgloss.Width(line); w < m.width {
		line += strings.Repeat(" ", m.width-w)
	}
	return st.Render(line)
}

func (m *Model) statusOrThinking() string {
	if m.statusMsg != "" {
		return m.statusMsg
	}
	return m.lang.get("thinking")
}

// contextInfo info token vs context window model aktif.
func (m *Model) contextInfo() string {
	meta, _, found, _ := catalog.Model(m.ag.Model)
	if !found || meta.ContextWindow == 0 {
		meta = catalog.ModelInfo{ContextWindow: 128000}
	}
	return fmt.Sprintf("%s/%s %s", humanTokens(int(m.tokensIn)), humanTokens(meta.ContextWindow), m.lang.get("tokens"))
}

// renderBlocks merender seluruh blok chat.
func (m *Model) renderBlocks() string {
	var out []string
	w := m.width - 4
	if w < 20 {
		w = 20
	}
	for _, b := range m.blocks {
		switch b.kind {
		case "banner":
			out = append(out, m.renderBannerBlock(), "")
		case "user":
			bubble := lipgloss.NewStyle().
				Background(m.th.UserBg).Foreground(m.th.UserFg).
				Padding(0, 1)
			out = append(out, m.th.MutedStyle().Render(m.lang.get("you")), bubble.Render(wrapText(b.text, w-4)), "")
		case "assistant":
			out = append(out, m.th.MutedStyle().Render(m.lang.get("assistant")),
				renderMarkdown(b.text, m.th, w), "")
		case "tool":
			out = append(out, lipgloss.NewStyle().Foreground(m.th.Accent).Render(b.text))
		case "status":
			out = append(out, m.th.MutedStyle().Render("· "+b.text))
		case "error":
			out = append(out, lipgloss.NewStyle().Foreground(m.th.Error).Render("! "+b.text), "")
		}
	}
	return strings.Join(out, "\n")
}

func wrapText(s string, width int) string {
	if width < 10 {
		return s
	}
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if len(ln) <= width {
			out = append(out, ln)
			continue
		}
		// wrap kata sederhana
		for len(ln) > width {
			cut := strings.LastIndex(ln[:width], " ")
			if cut < width/2 {
				cut = width
			}
			out = append(out, ln[:cut])
			ln = strings.TrimPrefix(ln[cut:], " ")
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// viewList template dialog daftar generik.
func (m *Model) viewList(title string, lines []string, idx int) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(m.th.Primary).Bold(true).Render(title) + "\n\n")
	start := 0
	if len(lines) > 15 {
		start = max(0, idx-7)
	}
	end := min(len(lines), start+15)
	for i := start; i < end; i++ {
		cursor := "  "
		style := lipgloss.NewStyle()
		if i == idx {
			cursor = "▸ "
			style = lipgloss.NewStyle().Foreground(m.th.Primary).Bold(true)
		}
		b.WriteString(style.Render(cursor+lines[i]) + "\n")
	}
	if len(lines) == 0 {
		b.WriteString(m.th.MutedStyle().Render("(kosong)") + "\n")
	}
	b.WriteString("\n" + m.th.MutedStyle().Render("↑↓ pilih · Enter OK · Esc batal"))
	return boxDialog(b.String(), m, m.width-8)
}

func (m *Model) modelLines() []string {
	lines := make([]string, len(m.modelItems))
	for i, it := range m.modelItems {
		lines[i] = it.label
	}
	return lines
}

func (m *Model) paletteLines() []string {
	filter := strings.TrimPrefix(strings.TrimSpace(m.input.Value()), "/")
	var lines []string
	m.filteredPalette = m.palette[:0:0]
	for _, p := range m.palette {
		if filter != "" && !strings.Contains(p.cmd, "/"+filter) {
			continue
		}
		m.filteredPalette = append(m.filteredPalette, p)
		lines = append(lines, fmt.Sprintf("%-12s %s", p.cmd, m.th.MutedStyle().Render(p.desc)))
	}
	if len(m.filteredPalette) == 0 {
		m.filteredPalette = m.palette
		return m.paletteLinesPlain()
	}
	return lines
}

// paletteLinesPlain tanpa filter.
func (m *Model) paletteLinesPlain() []string {
	lines := make([]string, len(m.palette))
	for i, p := range m.palette {
		lines[i] = fmt.Sprintf("%-12s %s", p.cmd, m.th.MutedStyle().Render(p.desc))
	}
	return lines
}

// viewPermDialog dialog izin modal.
func (m *Model) viewPermDialog() string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(m.th.Secondary).Bold(true).
		Render("⚠ "+m.lang.get("perm_title")) + "\n\n")
	fmt.Fprintf(&b, "tool: %s\n", m.pendingPerm.Tool)
	if m.pendingPerm.Target != "" {
		fmt.Fprintf(&b, "target: %s\n", m.pendingPerm.Target)
	}
	if m.pendingPerm.Preview != "" {
		b.WriteString("\n" + renderMarkdown("```\n"+truncateStr(m.pendingPerm.Preview, 900)+"\n```", m.th, m.width-10) + "\n")
	}
	opts := []string{m.lang.get("perm_once"), m.lang.get("perm_always"), m.lang.get("perm_deny")}
	b.WriteString("\n")
	for i, o := range opts {
		if i == m.permIdx {
			b.WriteString(lipgloss.NewStyle().Foreground(m.th.Primary).Bold(true).
				Render(fmt.Sprintf("▸ [%d] %s", i+1, o)) + "\n")
		} else {
			b.WriteString(fmt.Sprintf("  [%d] %s\n", i+1, o))
		}
	}
	b.WriteString("\n" + m.th.MutedStyle().Render("↑↓ / 1-3 · Enter OK · Esc tolak"))
	return boxDialog(b.String(), m, m.width-8)
}

// provBadge badge status koneksi satu provider.
func (m *Model) provBadge(it provItem) string {
	switch it.status {
	case "ready":
		label := m.lang.get("provider_ready")
		if it.viaEnv {
			label = m.lang.get("provider_env")
		}
		return lipgloss.NewStyle().Foreground(m.th.Success).Render("✔ " + label)
	case "lokal":
		return m.th.MutedStyle().Render("◦ " + m.lang.get("provider_lokal"))
	default:
		return lipgloss.NewStyle().Foreground(m.th.Error).Render("✗ " + m.lang.get("provider_need_key"))
	}
}

// truncRune memotong string aman-rune dengan elipsis.
func truncRune(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// viewProviderDialog daftar semua provider di katalog dengan badge status.
func (m *Model) viewProviderDialog() string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(m.th.Primary).Bold(true).
		Render("⬢ "+m.lang.get("provider_title")) + "\n\n")
	start, end := 0, len(m.provItems)
	if len(m.provItems) > 12 {
		start = max(0, m.provIdx-6)
		end = min(len(m.provItems), start+12)
	}
	for i := start; i < end; i++ {
		it := m.provItems[i]
		cursor, dotStyle := "  ", m.th.MutedStyle()
		if i == m.provIdx {
			cursor = "▸ "
		}
		if it.active {
			dotStyle = lipgloss.NewStyle().Foreground(m.th.Primary).Bold(true)
		}
		name := lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%-16s", truncRune(it.name, 16)))
		id := m.th.MutedStyle().Render(fmt.Sprintf("%-10s", truncRune(it.id, 10)))
		cnt := m.th.MutedStyle().Render(fmt.Sprintf("%-8s", fmt.Sprintf("%d model", it.modelCount)))
		line := cursor + dotStyle.Render("●") + " " + name + " " + id + " " + cnt
		// Badge mulai di kolom tetap agar sejajar di semua baris.
		badge := m.provBadge(it)
		if pad := 41 - lipgloss.Width(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		line += badge
		if it.active {
			line += "  " + lipgloss.NewStyle().Foreground(m.th.Primary).Render(m.lang.get("provider_active"))
		}
		b.WriteString(line + "\n")
	}
	if len(m.provItems) == 0 {
		b.WriteString(m.th.MutedStyle().Render("(kosong)") + "\n")
	}
	b.WriteString("\n" + m.th.MutedStyle().Render(m.lang.get("provider_footer")))
	return boxDialog(b.String(), m, m.width-8)
}

// viewProviderKeyDialog input API key untuk provider yang belum konek.
func (m *Model) viewProviderKeyDialog() string {
	name, id := m.provKeyFor, m.provKeyFor
	for _, it := range m.provItems {
		if it.id == m.provKeyFor {
			name = it.name
			break
		}
	}
	var b strings.Builder
	title := lipgloss.NewStyle().Foreground(m.th.Primary).Bold(true).
		Render("⬢ " + m.lang.get("provider_key_title") + " " + name)
	b.WriteString(title + m.th.MutedStyle().Render("  ·  "+id) + "\n\n")
	b.WriteString(m.th.MutedStyle().Render(m.lang.get("provider_key_hint")) + "\n\n")
	b.WriteString(m.provKeyInput.View() + "\n")
	b.WriteString("\n" + m.th.MutedStyle().Render("Enter "+m.lang.get("provider_key_footer")))
	return boxDialog(b.String(), m, m.width-8)
}

// viewHelp layar bantuan.
func (m *Model) viewHelp() string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(m.th.Primary).Bold(true).Render(m.lang.get("help_title")) + "\n\n")
	rows := [][2]string{
		{"Enter", "kirim pesan"},
		{"Ctrl+J", "baris baru"},
		{"Tab", "tukar mode Build ↔ Plan"},
		{"Esc", "hentikan agen yang berjalan"},
		{"Ctrl+K", "command palette"},
		{"Ctrl+M", "pilih model"},
		{"Ctrl+S", "daftar sesi"},
		{"Ctrl+Z / Ctrl+Y", "undo / redo"},
		{"Ctrl+E", "tulis di $EDITOR"},
		{"Ctrl+C 2x", "keluar"},
	}
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("  %-16s %s\n", lipgloss.NewStyle().Foreground(m.th.Accent).Render(r[0]), r[1]))
	}
	b.WriteString("\nSlash command:\n")
	for _, p := range m.palette {
		b.WriteString(fmt.Sprintf("  %-12s %s\n", p.cmd, p.desc))
	}
	return boxDialog(b.String(), m, m.width-8)
}

// viewCost layar biaya sesi.
func (m *Model) viewCost() string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(m.th.Primary).Bold(true).Render(m.lang.get("cost_title")) + "\n\n")
	fmt.Fprintf(&b, "  token input : %s\n", humanTokens(int(m.tokensIn)))
	fmt.Fprintf(&b, "  token output: %s\n", humanTokens(int(m.tokensOut)))
	fmt.Fprintf(&b, "  biaya       : %s\n", formatCost(m.costUSD, m.app.Cfg.Currency(), m.app.Cfg.ExchangeRate()))
	if u := m.ag.CostUSD(); u > 0 {
		fmt.Fprintf(&b, "  (hitungan agen: %s)\n", formatCost(u, m.app.Cfg.Currency(), m.app.Cfg.ExchangeRate()))
	}
	if bps := m.app.Cfg.BudgetPerSession(); bps > 0 {
		fmt.Fprintf(&b, "  batas sesi  : %s\n", formatCost(bps, "USD", 1))
	}
	b.WriteString("\n" + m.th.MutedStyle().Render("Esc kembali"))
	return boxDialog(b.String(), m, m.width-8)
}

// boxDialog membungkus dialog di tengah layar.
func boxDialog(content string, m *Model, width int) string {
	if width < 30 {
		width = 30
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.th.Border).
		Padding(1, 2).
		Width(width).
		Render(content)
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center, box)
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var _ = config.ConfigDir
var _ = time.Second
