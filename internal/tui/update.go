package tui

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mohammadirham37/jenderal_code/internal/agent"
	"github.com/mohammadirham37/jenderal_code/internal/bus"
	"github.com/mohammadirham37/jenderal_code/internal/permission"
)

// programIface bagian tea.Program yang dipakai Model (menghindari impor siklus).
type programIface interface {
	Send(msg tea.Msg)
}

// eventMsg event bus dari agen.
type eventMsg struct{ ev bus.Event }

// tickMsg animasi spinner.
type tickMsg struct{}

// permRequestMsg permintaan izin dari resolver.
type permRequestMsg struct {
	req agent.PermRequest
	ch  chan agent.PermResponse
}

// editorDoneMsg hasil kembali dari $EDITOR.
type editorDoneMsg struct{ content string }

// SetProgram dipanggil sebelum program berjalan.
func (m *Model) SetProgram(p programIface) { m.program = p }

// permResolver menjembatani dialog izin TUI ke agen.
func (m *Model) permResolver() func(context.Context, agent.PermRequest) agent.PermResponse {
	return func(ctx context.Context, req agent.PermRequest) agent.PermResponse {
		ch := make(chan agent.PermResponse, 1)
		if m.program == nil {
			return agent.PermResponse{Decision: permission.Deny}
		}
		m.program.Send(permRequestMsg{req: req, ch: ch})
		select {
		case r := <-ch:
			return r
		case <-ctx.Done():
			return agent.PermResponse{Decision: permission.Deny}
		case <-time.After(15 * time.Minute):
			return agent.PermResponse{Decision: permission.Deny}
		}
	}
}

// tick spinner tiap 120ms.
func tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} })
}

// Init memulai loop tick.
func (m *Model) Init() tea.Cmd { return tick() }

// Update menangani seluruh pesan.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width > 10 && msg.Height > 5 { // abaikan ukuran nol dari pty kosong
			m.width, m.height = msg.Width, msg.Height
			m.layout()
			m.refreshViewport()
		}
		return m, nil

	case tickMsg:
		if m.streaming {
			m.spinIdx = (m.spinIdx + 1) % len([]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"})
			return m, tick()
		}
		return m, nil

	case permRequestMsg:
		m.pendingPerm = &msg.req
		m.permCh = msg.ch
		m.permIdx = 0
		m.view = viewPerm
		return m, nil

	case editorDoneMsg:
		if msg.content != "" {
			m.input.SetValue(msg.content)
		}
		return m, nil

	case eventMsg:
		return m.handleEvent(msg.ev)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	// Teruskan ke textarea bila di chat.
	if m.view == viewChat {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// handleEvent memproses event bus menjadi perubahan tampilan.
func (m *Model) handleEvent(ev bus.Event) (tea.Model, tea.Cmd) {
	switch ev.Type {
	case bus.EventTextDelta:
		if !m.streaming {
			m.streaming = true
		}
		if m.streamBuf.Len() == 0 {
			// Blok asisten baru: bersihkan status sementara.
			m.statusMsg = ""
		}
		m.streamBuf.WriteString(ev.Text)
		m.upsertAssistantBlock()
		m.refreshViewport()
	case bus.EventReasoningDelta:
		// ditampilkan sebagai status ringan
		m.statusMsg = "…berpikir"
	case bus.EventToolCall:
		m.flushStream()
		m.blocks = append(m.blocks, chatBlock{kind: "tool", text: "⚙ " + ev.ToolName + " " + summarizeArgs(ev.Args)})
		m.refreshViewport()
	case bus.EventToolResult:
		// Tambahkan cuplikan hasil ke blok tool terakhir yang cocok.
		for i := len(m.blocks) - 1; i >= 0; i-- {
			if m.blocks[i].kind == "tool" && strings.HasPrefix(m.blocks[i].text, "⚙ "+ev.ToolName) {
				res := firstLine(ev.Result)
				if len(res) > 120 {
					res = res[:120] + "…"
				}
				m.blocks[i].text += "\n  " + res
				break
			}
		}
		m.refreshViewport()
	case bus.EventUsage:
		if ev.Usage != nil {
			m.tokensIn += ev.Usage.InputTokens
			m.tokensOut += ev.Usage.OutputTokens
		}
		if c, ok := ev.Detail["cost_usd"].(float64); ok {
			m.costUSD += c
		}
	case bus.EventStatus:
		m.statusMsg = ev.Text
	case bus.EventTitle:
		// judul sesi diperbarui; tidak ada tampilan khusus
	case bus.EventModeChanged:
		m.statusMsg = "mode: " + ev.Text
	case bus.EventModelChanged:
		m.statusMsg = "model: " + ev.Text
	case bus.EventError:
		m.flushStream()
		m.blocks = append(m.blocks, chatBlock{kind: "error", text: ev.Text})
		m.streaming = false
		m.refreshViewport()
	case bus.EventDone:
		m.flushStream()
		m.streaming = false
		m.statusMsg = ""
		m.branch = gitBranch(m.ag.Sess.ProjectPath)
		m.refreshViewport()
		return m, tick()
	}
	return m, nil
}

// upsertAssistantBlock memperbarui blok asisten yang sedang mengalir.
func (m *Model) upsertAssistantBlock() {
	if len(m.blocks) > 0 && m.blocks[len(m.blocks)-1].kind == "assistant" && m.blocks[len(m.blocks)-1].streaming {
		m.blocks[len(m.blocks)-1].text = m.streamBuf.String()
		return
	}
	m.blocks = append(m.blocks, chatBlock{kind: "assistant", text: m.streamBuf.String(), streaming: true})
}

// flushStream menutup blok streaming aktif.
func (m *Model) flushStream() {
	if m.streamBuf.Len() > 0 && len(m.blocks) > 0 {
		last := &m.blocks[len(m.blocks)-1]
		if last.kind == "assistant" && last.streaming {
			last.text = m.streamBuf.String()
			last.streaming = false
		}
	}
	m.streamBuf.Reset()
}

func summarizeArgs(args string) string {
	var m map[string]any
	if err := jsonUnmarshal(args, &m); err != nil {
		return ""
	}
	if c, ok := m["command"].(string); ok && c != "" {
		return "→ " + firstLine(c)
	}
	if p, ok := m["path"].(string); ok && p != "" {
		return "→ " + p
	}
	if u, ok := m["url"].(string); ok && u != "" {
		return "→ " + u
	}
	return ""
}

// handleKey memproses tombol per tampilan.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch m.view {
	case viewPerm:
		switch key {
		case "up", "k":
			if m.permIdx > 0 {
				m.permIdx--
			}
		case "down", "j":
			if m.permIdx < 2 {
				m.permIdx++
			}
		case "1", "2", "3":
			m.permIdx = int(key[1] - '1')
			return m, m.answerPerm()
		case "enter":
			return m, m.answerPerm()
		case "esc", "q":
			m.permIdx = 2
			return m, m.answerPerm()
		}
		return m, nil

	case viewModel:
		switch key {
		case "up", "k":
			if m.modelIdx > 0 {
				m.modelIdx--
			}
		case "down", "j":
			if m.modelIdx < len(m.modelItems)-1 {
				m.modelIdx++
			}
		case "enter":
			if m.modelIdx < len(m.modelItems) {
				ref := m.modelItems[m.modelIdx].ref
				if err := m.ag.SetModel(ref); err == nil {
					m.statusMsg = "model: " + ref
				} else {
					m.statusMsg = err.Error()
				}
			}
			m.view = viewChat
		case "esc", "q":
			m.view = viewChat
		}
		return m, nil

	case viewSessions:
		switch key {
		case "up", "k":
			if m.sessIdx > 0 {
				m.sessIdx--
			}
		case "down", "j":
			if m.sessIdx < len(m.sessions) {
				m.sessIdx++
			}
		case "enter":
			return m, m.switchSession()
		case "esc", "q":
			m.view = viewChat
		}
		return m, nil

	case viewAgents:
		switch key {
		case "up", "k":
			if m.agentIdx > 0 {
				m.agentIdx--
			}
		case "down", "j":
			if m.agentIdx < len(m.agentsAll) {
				m.agentIdx++
			}
		case "enter":
			if m.agentIdx == 0 {
				_ = m.ag.SwitchAgent("")
				m.statusMsg = "agent: default"
			} else if m.agentIdx-1 < len(m.agentsAll) {
				ca := m.agentsAll[m.agentIdx-1]
				if err := m.ag.SwitchAgent(ca.Name); err == nil {
					m.statusMsg = "agent: " + ca.Name
				} else {
					m.statusMsg = err.Error()
				}
			}
			m.view = viewChat
		case "esc", "q":
			m.view = viewChat
		}
		return m, nil

	case viewPalette:
		switch key {
		case "up", "k", "ctrl+p":
			if m.palIdx > 0 {
				m.palIdx--
			}
		case "down", "j", "ctrl+n":
			if m.palIdx < len(m.filteredPalette)-1 {
				m.palIdx++
			}
		case "enter":
			if m.palIdx < len(m.filteredPalette) {
				cmd := m.filteredPalette[m.palIdx].cmd
				m.view = viewChat
				m.input.SetValue(cmd)
				return m, m.submitInput()
			}
		case "esc", "q":
			m.view = viewChat
			m.input.Reset()
		case "backspace":
			v := m.input.Value()
			if len(v) > 0 {
				m.input.SetValue(v[:len(v)-1])
			}
			m.palIdx = 0
		default:
			// ketik untuk filter
			if len(key) == 1 {
				m.input.SetValue(m.input.Value() + key)
				m.palIdx = 0
			}
		}
		return m, nil

	case viewHelp, viewCost:
		if key == "esc" || key == "q" || key == "enter" {
			m.view = viewChat
		}
		return m, nil
	}

	// ---- view chat ----
	switch key {
	case "tab":
		m.ag.ToggleMode()
		return m, nil
	case "esc":
		if m.streaming {
			m.ag.Abort()
			m.statusMsg = m.lang.get("aborting")
		} else {
			m.statusMsg = ""
		}
		return m, nil
	case "ctrl+c":
		if time.Since(m.ctrlCAt) < 2*time.Second {
			return m, tea.Quit
		}
		m.ctrlCAt = time.Now()
		m.statusMsg = m.lang.get("quit")
		return m, nil
	case "ctrl+m":
		m.buildModelList()
		m.view = viewModel
		return m, nil
	case "ctrl+s":
		if list, err := m.app.Store.ListSessions(m.ag.Sess.ProjectPath, "", 30); err == nil {
			m.sessions = list
			m.sessIdx = 0
			m.view = viewSessions
		}
		return m, nil
	case "ctrl+k":
		m.palIdx = 0
		m.input.SetValue("")
		m.view = viewPalette
		return m, nil
	case "ctrl+z":
		m.doUndo()
		return m, nil
	case "ctrl+y":
		m.doRedo()
		return m, nil
	case "ctrl+e":
		return m, openEditor()
	case "enter":
		return m, m.submitInput()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// submitInput mengirim isi textarea.
func (m *Model) submitInput() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" || m.streaming {
		return nil
	}
	m.input.Reset()

	// Slash command UI ditangani lokal.
	if strings.HasPrefix(text, "/") && m.handleSlash(text) {
		m.refreshViewport()
		return nil
	}

	m.blocks = append(m.blocks, chatBlock{kind: "user", text: text})
	m.refreshViewport()
	m.streaming = true

	ag := m.ag
	go func() {
		ctx := context.Background()
		_, _ = ag.Run(ctx, text)
	}()
	return tick()
}

// answerPerm mengirim keputusan izin ke resolver.
func (m *Model) answerPerm() tea.Cmd {
	if m.permCh == nil {
		m.view = viewChat
		return nil
	}
	resp := agent.PermResponse{Decision: permission.Deny}
	switch m.permIdx {
	case 0:
		resp = agent.PermResponse{Decision: permission.Allow}
	case 1:
		resp = agent.PermResponse{Decision: permission.Allow, Always: true}
	}
	ch := m.permCh
	m.permCh = nil
	m.pendingPerm = nil
	m.view = viewChat
	return func() tea.Msg {
		ch <- resp
		return nil
	}
}

// switchSession berpindah ke sesi terpilih.
func (m *Model) switchSession() tea.Cmd {
	idx := m.sessIdx
	// index 0 = sesi baru
	if idx == 0 {
		if sess, err := m.app.Store.CreateSession(m.ag.Sess.ProjectPath, "", m.app.Cfg.Model()); err == nil {
			if newAg, err := m.newAgentFor(sess); err == nil {
				m.ag = newAg
				m.blocks = nil
				m.tokensIn, m.tokensOut, m.costUSD = 0, 0, 0
				m.statusMsg = m.lang.get("new_session") + ": " + sess.ID
			}
		}
		m.view = viewChat
		m.refreshViewport()
		return nil
	}
	sess := m.sessions[idx-1]
	if sess.ID == m.ag.Sess.ID {
		m.view = viewChat
		return nil
	}
	if newAg, err := m.newAgentFor(&sess); err == nil {
		m.ag = newAg
		m.loadHistory()
		m.statusMsg = m.lang.get("session_switched") + " " + sess.ID
	}
	m.view = viewChat
	m.refreshViewport()
	return nil
}

// openEditor membuka isi textarea di $EDITOR (Ctrl+E).
func openEditor() tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	f, err := os.CreateTemp("", "jenderal-input-*.md")
	if err != nil {
		return nil
	}
	_ = f.Close()
	cmd := exec.Command(editor, f.Name())
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(f.Name())
		if err != nil {
			return editorDoneMsg{content: ""}
		}
		b, err := os.ReadFile(f.Name())
		if err != nil {
			return editorDoneMsg{content: ""}
		}
		return editorDoneMsg{content: strings.TrimRight(string(b), "\n")}
	})
}
