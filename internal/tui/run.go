package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mohammadirham37/jenderal_code/internal/agent"
	"github.com/mohammadirham37/jenderal_code/internal/bus"
	"github.com/mohammadirham37/jenderal_code/internal/mcp"
	"github.com/mohammadirham37/jenderal_code/internal/session"
)

// programRef jembatan global untuk event dari goroutine lain
// (satu TUI per proses).
var programRef programIface

// Run menjalankan TUI interaktif untuk satu sesi sampai pengguna keluar.
// programOpts opsional dipakai untuk pengujian (WithInput/WithOutput).
func Run(app *AppContext, sess *session.Session, mcpLog func(string), programOpts ...tea.ProgramOption) error {
	b := bus.New()
	ag, err := agent.New(agent.Options{
		Config: app.Cfg, Registry: app.Reg, Store: app.Store, Session: sess, Bus: b,
	})
	if err != nil {
		return err
	}

	// TUI memasang dialog izin via program.Send (diisi setelah program ada).
	m := NewModel(app, ag)

	// MCP: sambungkan server yang dikonfigurasi (best-effort, tanpa blokir).
	if len(app.Cfg.MCPServers()) > 0 {
		go func() {
			_ = mcp.AttachAll(context.Background(), app.Cfg.MCPServers(), ag.Tools, func(msg string) {
				b.Publish(bus.Event{Type: bus.EventStatus, Text: msg})
			})
		}()
	}

	p := tea.NewProgram(&m, append([]tea.ProgramOption{tea.WithAltScreen()}, programOpts...)...)
	programRef = p
	m.SetProgram(p)
	m.ag = ag
	// Agen awal juga butuh resolver dialog izin (agen hasil /new atau pindah
	// sesi memasangnya sendiri di newAgentFor); tanpa ini semua permintaan
	// izin auto-ditolak di mode BUILD.
	ag.PermResolver = m.permResolver()

	// Pompa event bus → program.
	subID, evCh := b.Subscribe()
	defer b.Unsubscribe(subID)
	go func() {
		for ev := range evCh {
			p.Send(eventMsg{ev})
		}
	}()

	if ag.Model == "" && len(app.Reg.IDs()) == 0 {
		// Tanpa provider: tampilkan panduan di status awal.
		m.statusMsg = m.lang.get("no_key")
	}

	_, err = p.Run()
	return err
}
