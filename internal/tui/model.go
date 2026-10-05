package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"

	"github.com/jenderalcode/jenderal/catalog"
	"github.com/jenderalcode/jenderal/internal/agent"
	"github.com/jenderalcode/jenderal/internal/bus"
	"github.com/jenderalcode/jenderal/internal/config"
	"github.com/jenderalcode/jenderal/internal/provider"
	"github.com/jenderalcode/jenderal/internal/session"
)

// AppContext dependensi aplikasi yang dibagikan ke TUI.
type AppContext struct {
	Cfg   *config.Config
	Reg   *provider.Registry
	Store *session.Store
}

// chatBlock satu bagian tampilan chat.
type chatBlock struct {
	kind      string // user | assistant | tool | status | error
	text      string
	streaming bool
}

// viewState dialog aktif.
type viewState int

const (
	viewChat viewState = iota
	viewPerm
	viewModel
	viewSessions
	viewAgents
	viewPalette
	viewHelp
	viewCost
)

type modelItem struct {
	ref   string // provider/model
	label string
	info  catalog.ModelInfo
}

type paletteItem struct {
	cmd  string
	desc string
}

// Model utama Bubble Tea (implementasi tea.Model di update.go/view.go).
type Model struct {
	app  *AppContext
	ag   *agent.Agent
	th   Theme
	lang T

	width, height int

	blocks   []chatBlock
	viewport viewport.Model
	input    textarea.Model
	spinIdx  int

	view viewState

	streaming bool
	streamBuf strings.Builder

	permCh      chan agent.PermResponse
	pendingPerm *agent.PermRequest
	permIdx     int

	modelItems      []modelItem
	modelIdx        int
	sessions        []session.Session
	sessIdx         int
	agentsAll       []*agent.CustomAgent
	agentIdx        int
	palette         []paletteItem
	palIdx          int
	filteredPalette []paletteItem

	statusMsg string
	tokensIn  int64
	tokensOut int64
	costUSD   float64
	branch    string

	ctrlCAt time.Time
	program programIface
}

// NewModel membangun model TUI untuk satu sesi.
func NewModel(app *AppContext, ag *agent.Agent) Model {
	lang := dicts[app.Cfg.Language()]
	if lang == nil {
		lang = dicts["id"]
	}
	in := textarea.New()
	in.Placeholder = "Tanya apa saja… (Enter kirim · Ctrl+J baris baru)"
	in.Prompt = "› "
	in.CharLimit = 100000
	in.SetHeight(3)
	in.Focus()
	in.KeyMap.InsertNewline.SetKeys("ctrl+j")
	in.KeyMap.LineNext.SetKeys("down", "ctrl+n")
	in.KeyMap.LinePrevious.SetKeys("up", "ctrl+p")

	m := Model{
		app:      app,
		ag:       ag,
		th:       LoadTheme(app.Cfg.Theme()),
		lang:     lang,
		input:    in,
		viewport: viewport.New(80, 20),
		width:    80, // default; diperbarui oleh WindowSizeMsg
		height:   24,
	}
	m.layout()
	m.buildPalette()
	m.loadAgents()
	m.loadHistory()
	m.branch = gitBranch(ag.Sess.ProjectPath)
	return m
}

func gitBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (m *Model) buildPalette() {
	m.palette = []paletteItem{
		{"/model", "pilih model"},
		{"/agent", "pilih custom agent"},
		{"/new", "sesi baru"},
		{"/sessions", "daftar sesi"},
		{"/undo", "kembalikan perubahan terakhir"},
		{"/redo", "ulangi perubahan yang di-undo"},
		{"/compact", "ringkas konteks sekarang"},
		{"/cost", "lihat biaya sesi"},
		{"/init", "buat JENDERAL.md"},
		{"/share", "ekspor sesi ke Markdown"},
		{"/export", "ekspor sesi ke JSON"},
		{"/theme", "ganti tema"},
		{"/help", "bantuan"},
		{"/quit", "keluar"},
	}
	for _, cc := range agent.LoadCustomCommands(m.ag.Sess.ProjectPath) {
		m.palette = append(m.palette, paletteItem{"/" + cc.Name, cc.Description})
	}
}

func (m *Model) loadAgents() {
	m.agentsAll = m.ag.ListAgents()
}

// loadHistory memuat pesan sesi dari database ke blok chat.
func (m *Model) loadHistory() {
	msgs, err := m.ag.Store.ActiveMessages(m.ag.Sess.ID)
	if err != nil {
		return
	}
	m.blocks = nil
	for _, msg := range msgs {
		switch msg.Role {
		case "user":
			m.blocks = append(m.blocks, chatBlock{kind: "user", text: msg.Content})
		case "assistant":
			m.blocks = append(m.blocks, chatBlock{kind: "assistant", text: msg.Content})
		case "tool":
			m.blocks = append(m.blocks, chatBlock{kind: "tool", text: msg.ToolName + " → " + firstLine(msg.Content)})
		case "summary":
			m.blocks = append(m.blocks, chatBlock{kind: "status", text: "konteks diringkas otomatis"})
		}
	}
	u, _ := m.ag.Store.SessionUsage(m.ag.Sess.ID)
	m.tokensIn, m.tokensOut, m.costUSD = u.TokensIn, u.TokensOut, u.CostUSD
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// buildModelList menyusun daftar model dari semua provider siap pakai.
func (m *Model) buildModelList() {
	m.modelItems = nil
	for _, provID := range m.app.Reg.IDs() {
		p, err := m.app.Reg.Get(provID)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		models, err := p.Models(ctx)
		cancel()
		if err != nil {
			continue
		}
		for _, mi := range models {
			ref := catalog.JoinModelRef(provID, mi.ID)
			label := fmt.Sprintf("%-28s ctx %s · in $%.2f/M · out $%.2f/M %s",
				ref, humanTokens(mi.ContextWindow), mi.PriceInputPerM, mi.PriceOutputPerM,
				recMark(mi.Recommended))
			m.modelItems = append(m.modelItems, modelItem{ref: ref, label: label, info: mi})
		}
	}
	// Model aktif di depan.
	for i, it := range m.modelItems {
		if it.ref == m.ag.Model {
			if i > 0 {
				m.modelItems[0], m.modelItems[i] = m.modelItems[i], m.modelItems[0]
			}
			break
		}
	}
	m.modelIdx = 0
}

func humanTokens(n int) string {
	switch {
	case n >= 1000000:
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func recMark(rec bool) string {
	if rec {
		return "★"
	}
	return ""
}

// ---- Aksi slash command ----

// handleSlash mengeksekusi slash command; true bila tertangani.
func (m *Model) handleSlash(input string) bool {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return false
	}
	parts := strings.Fields(trimmed)
	name := strings.TrimPrefix(parts[0], "/")
	arg := ""
	if len(parts) > 1 {
		arg = strings.Join(parts[1:], " ")
	}
	switch name {
	case "help":
		m.view = viewHelp
	case "model":
		m.buildModelList()
		m.view = viewModel
	case "agent":
		m.loadAgents()
		m.agentIdx = 0
		m.view = viewAgents
	case "new":
		if sess, err := m.app.Store.CreateSession(m.ag.Sess.ProjectPath, "", m.app.Cfg.Model()); err == nil {
			if newAg, err := m.newAgentFor(sess); err == nil {
				m.ag = newAg
				m.blocks = nil
				m.tokensIn, m.tokensOut, m.costUSD = 0, 0, 0
				m.statusMsg = m.lang.get("new_session") + ": " + sess.ID
			}
		}
	case "sessions":
		if list, err := m.app.Store.ListSessions(m.ag.Sess.ProjectPath, "", 30); err == nil {
			m.sessions = list
			m.sessIdx = 0
			m.view = viewSessions
		}
	case "undo":
		m.doUndo()
	case "redo":
		m.doRedo()
	case "compact":
		ag := m.ag
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			if programRef == nil {
				return
			}
			if err := ag.Compact(ctx); err != nil {
				programRef.Send(eventMsg{bus.Event{Type: bus.EventStatus, SessionID: ag.Sess.ID, Text: "kompaksi: " + err.Error()}})
			} else {
				programRef.Send(eventMsg{bus.Event{Type: bus.EventStatus, SessionID: ag.Sess.ID, Text: "kompaksi selesai"}})
				programRef.Send(eventMsg{bus.Event{Type: bus.EventDone, SessionID: ag.Sess.ID}})
			}
		}()
	case "cost":
		m.view = viewCost
	case "init":
		content := agent.InitProject(m.ag.Sess.ProjectPath)
		if _, err := os.Stat(filepath.Join(m.ag.Sess.ProjectPath, "JENDERAL.md")); err == nil {
			m.statusMsg = m.lang.get("init_exists")
		} else if err := os.WriteFile(filepath.Join(m.ag.Sess.ProjectPath, "JENDERAL.md"), []byte(content), 0o644); err != nil {
			m.statusMsg = "gagal menulis JENDERAL.md: " + err.Error()
		} else {
			m.statusMsg = m.lang.get("init_done")
		}
	case "share":
		if p, err := m.exportMarkdown(); err == nil {
			m.statusMsg = m.lang.get("shared") + " " + p
		} else {
			m.statusMsg = err.Error()
		}
	case "export":
		if p, err := m.exportJSON(); err == nil {
			m.statusMsg = m.lang.get("shared") + " " + p
		} else {
			m.statusMsg = err.Error()
		}
	case "theme":
		if arg == "" {
			names := ThemeNames()
			cur := m.th.Name
			for i, n := range names {
				if n == cur {
					arg = names[(i+1)%len(names)]
					break
				}
			}
		}
		m.th = LoadTheme(arg)
		m.statusMsg = m.lang.get("theme_set") + " " + m.th.Name
	case "quit", "exit":
		os.Exit(0)
	default:
		// Custom command? ExpandCustomCommand ditangani di agent.Run; di sini
		// cukup diteruskan apa adanya bila ada di daftar.
		for _, pi := range m.palette {
			if pi.cmd == "/"+name {
				return false // biarkan dikirim ke agen (diekspansi di Run)
			}
		}
		m.statusMsg = "perintah tidak dikenal: /" + name + " (coba /help)"
	}
	return true
}

// newAgentFor membuat agen baru untuk sesi (pindah sesi/sesi baru).
func (m *Model) newAgentFor(sess *session.Session) (*agent.Agent, error) {
	b := bus.New()
	ag, err := agent.New(agent.Options{
		Config: m.app.Cfg, Registry: m.app.Reg, Store: m.app.Store, Session: sess, Bus: b,
	})
	if err != nil {
		return nil, err
	}
	ag.PermResolver = m.permResolver()
	return ag, nil
}

// doUndo membatalkan kelompok perubahan terakhir.
func (m *Model) doUndo() {
	rows, _, err := m.ag.Store.UndoGroup(m.ag.Sess.ID)
	if err != nil || rows == nil {
		m.statusMsg = m.lang.get("undo_empty")
		return
	}
	for _, r := range rows {
		if content, err := m.ag.Snap.Load(r.PrevSnap); err == nil {
			_ = os.WriteFile(r.Path, []byte(content), 0o644)
		}
	}
	m.loadHistory()
	m.refreshViewport()
	m.statusMsg = m.lang.get("undo_done")
}

// doRedo mengulang perubahan yang terakhir di-undo.
func (m *Model) doRedo() {
	rows, _, err := m.ag.Store.RedoGroup(m.ag.Sess.ID)
	if err != nil || rows == nil {
		m.statusMsg = m.lang.get("redo_empty")
		return
	}
	for _, r := range rows {
		if content, err := m.ag.Snap.Load(r.NewSnap); err == nil {
			_ = os.WriteFile(r.Path, []byte(content), 0o644)
		}
	}
	m.loadHistory()
	m.refreshViewport()
	m.statusMsg = m.lang.get("redo_done")
}

// exportMarkdown mengekspor sesi ke file Markdown di folder proyek.
func (m *Model) exportMarkdown() (string, error) {
	exp, err := m.app.Store.ExportSession(m.ag.Sess.ID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Sesi %s (%s)\n\n", exp.Session.Title, exp.Session.ID)
	for _, msg := range exp.Messages {
		switch msg.Role {
		case "user":
			b.WriteString("## Anda\n\n" + msg.Content + "\n\n")
		case "assistant":
			b.WriteString("## Asisten\n\n" + msg.Content + "\n\n")
		case "tool":
			b.WriteString("- tool `" + msg.ToolName + "`: " + firstLine(msg.Content) + "\n")
		}
	}
	p := filepath.Join(m.ag.Sess.ProjectPath, fmt.Sprintf("sesi-%s.md", m.ag.Sess.ID))
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// exportJSON mengekspor sesi ke file JSON.
func (m *Model) exportJSON() (string, error) {
	exp, err := m.app.Store.ExportSession(m.ag.Sess.ID)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(exp, "", "  ")
	if err != nil {
		return "", err
	}
	p := filepath.Join(m.ag.Sess.ProjectPath, fmt.Sprintf("sesi-%s.json", m.ag.Sess.ID))
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// refreshViewport menghitung ulang isi viewport.
func (m *Model) refreshViewport() {
	m.viewport.SetContent(m.renderBlocks())
	m.viewport.GotoBottom()
}
