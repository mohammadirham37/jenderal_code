package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/mohammadirham37/jenderal_code/catalog"
	"github.com/mohammadirham37/jenderal_code/internal/agent"
	"github.com/mohammadirham37/jenderal_code/internal/bus"
	"github.com/mohammadirham37/jenderal_code/internal/mcp"
	"github.com/mohammadirham37/jenderal_code/internal/permission"
	"github.com/mohammadirham37/jenderal_code/internal/provider"
	"github.com/mohammadirham37/jenderal_code/internal/server"
	"github.com/mohammadirham37/jenderal_code/internal/session"
	"github.com/mohammadirham37/jenderal_code/internal/tui"
)

// catalog_SetCacheDir mengaktifkan cache katalog hasil refresh.
func catalog_SetCacheDir() { catalog.SetCacheDir(cacheDirOf()) }

// mockScript percakapan demo provider mock (JENDERAL_MOCK=1):
// giliran pertama membuat satu file, giliran kedua menutup tugas.
func mockScript() []provider.MockTurn {
	demoFile := "package main\n\nfunc main() {\n\tprintln(\"halo dari JenderalCode\")\n}\n"
	return []provider.MockTurn{
		{
			Text: "Baik, saya buatkan berkas demo.\n\n```go\n" + demoFile + "```",
			ToolCalls: []provider.ToolCall{{
				ID:   "demo-1",
				Name: "write",
				Args: `{"path": "demo-jenderal.go", "content": "package main\n\nfunc main() {\n\tprintln(\"halo dari JenderalCode\")\n}\n"}`,
			}},
			Usage: provider.Usage{InputTokens: 120, OutputTokens: 80},
		},
		{Text: "✔ Selesai: file `demo-jenderal.go` dibuat. Jalankan `go run demo-jenderal.go` untuk mencoba.", Usage: provider.Usage{InputTokens: 210, OutputTokens: 40}},
	}
}

// attachMock mendaftarkan provider mock bila JENDERAL_MOCK=1 sehingga
// aplikasi bisa dicoba tanpa API key sama sekali.
func attachMock(a *app) {
	if os.Getenv("JENDERAL_MOCK") != "1" {
		return
	}
	a.reg.AddProvider(provider.NewMock("mock", mockScript(), false))
	if a.cfg.Model() == "" {
		a.cfg.Merge(map[string]any{"model": "mock/mock-1"})
	}
}

// ---- run ----

func runCmd() *cobra.Command {
	var (
		jsonOut bool
		model   string
		agentN  string
		yolo    bool
		sessID  string
		cont    bool
		mode    string
	)
	cmd := &cobra.Command{
		Use:   `run "<prompt>"`,
		Short: "Jalankan satu tugas non-interaktif (untuk skrip dan CI)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			overrides := map[string]any{}
			if model != "" {
				overrides["model"] = model
			}
			a, err := bootstrap(overrides)
			if err != nil {
				return err
			}
			defer a.store.Close()
			catalog_SetCacheDir()

			// Sesi: lanjutkan atau buat baru.
			var sess *session.Session
			if cont {
				if list, err := a.store.ListSessions(a.dir, "", 1); err == nil && len(list) > 0 {
					sess = &list[0]
				}
			} else if sessID != "" {
				sess, err = a.store.GetSession(sessID)
				if err != nil {
					return err
				}
			}
			if sess == nil {
				sess, err = a.store.CreateSession(a.dir, "", a.cfg.Model())
				if err != nil {
					return err
				}
			}

			ag, err := agent.New(agent.Options{Config: a.cfg, Registry: a.reg, Store: a.store, Session: sess, Bus: bus.New()})
			if err != nil {
				return err
			}
			if yolo {
				ag.Perm.SetYolo(true)
				fmt.Fprintln(os.Stderr, "⚠ mode --yolo: SEMUA aksi disetujui otomatis. Gunakan hanya di lingkungan sandbox/CI.")
			} else if term.IsTerminal(int(os.Stdin.Fd())) {
				ag.PermResolver = terminalPermResolver
			} else {
				// Non-interaktif tanpa --yolo: permintaan izin otomatis ditolak.
				ag.PermResolver = func(ctx context.Context, req agent.PermRequest) agent.PermResponse {
					fmt.Fprintf(os.Stderr, "izin ditolak otomatis (non-interaktif): %s %s — gunakan --yolo atau atur permission di jenderal.jsonc\n",
						req.Tool, req.Target)
					return agent.PermResponse{Decision: permission.Deny}
				}
			}
			switch strings.ToLower(mode) {
			case "plan":
				ag.SetMode(agent.ModePlan)
			}
			if agentN != "" {
				if err := ag.SwitchAgent(agentN); err != nil {
					return err
				}
			}
			// MCP best-effort.
			mcp.AttachAll(context.Background(), a.cfg.MCPServers(), ag.Tools, func(msg string) {
				fmt.Fprintln(os.Stderr, "·", msg)
			})

			subID, evCh := ag.Bus.Subscribe()
			defer ag.Bus.Unsubscribe(subID)
			events := make(chan bus.Event, 256)
			go func() {
				for ev := range evCh {
					events <- ev
				}
				close(events)
			}()

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			resultCh := make(chan struct {
				text string
				err  error
			}, 1)
			go func() {
				text, err := ag.Run(ctx, args[0])
				resultCh <- struct {
					text string
					err  error
				}{text, err}
			}()

			tok := struct {
				in, out int64
				cost    float64
			}{}
			var result struct {
				text string
				err  error
			}
			got := false
		pump:
			for {
				select {
				case ev, ok := <-events:
					if !ok {
						break pump
					}
					switch ev.Type {
					case bus.EventTextDelta:
						if jsonOut {
							printJSONL(map[string]any{"type": "text_delta", "text": ev.Text})
						} else {
							fmt.Print(ev.Text)
						}
					case bus.EventToolCall:
						if jsonOut {
							printJSONL(map[string]any{"type": "tool_call", "name": ev.ToolName, "args": ev.Args})
						} else {
							fmt.Fprintf(os.Stderr, "· %s %s\n", ev.ToolName, summarizeToolArgs(ev.Args))
						}
					case bus.EventToolResult:
						if jsonOut {
							printJSONL(map[string]any{"type": "tool_result", "name": ev.ToolName, "result": ev.Result, "is_err": ev.IsErr})
						}
					case bus.EventUsage:
						if ev.Usage != nil {
							tok.in += ev.Usage.InputTokens
							tok.out += ev.Usage.OutputTokens
						}
						if c, ok := ev.Detail["cost_usd"].(float64); ok {
							tok.cost += c
						}
					case bus.EventError:
						if jsonOut {
							printJSONL(map[string]any{"type": "error", "error": ev.Text})
						} else {
							fmt.Fprintln(os.Stderr, "! "+ev.Text)
						}
					}
				case res := <-resultCh:
					result = res
					got = true
					// Drain sisa event singkat setelah agen selesai.
					deadline := time.After(2 * time.Second)
					for {
						select {
						case ev, ok := <-events:
							if !ok {
								break pump
							}
							if ev.Type == bus.EventTextDelta && !jsonOut {
								fmt.Print(ev.Text)
							}
						case <-deadline:
							break pump
						}
					}
				}
			}
			if !got {
				r := <-resultCh
				result = r
			}

			if jsonOut {
				printJSONL(map[string]any{
					"type": "result", "session_id": sess.ID, "text": result.text,
					"tokens_in": tok.in, "tokens_out": tok.out, "cost_usd": tok.cost,
					"error": errStr(result.err),
				})
			} else {
				if result.text != "" && !strings.HasSuffix(result.text, "\n") {
					fmt.Println()
				}
				fmt.Fprintf(os.Stderr, "\n[sesi %s · %d→%d token · $%.4f]\n", sess.ID, tok.in, tok.out, tok.cost)
			}
			return result.err
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output JSONL per event")
	cmd.Flags().StringVar(&model, "model", "", "model (provider/model)")
	cmd.Flags().StringVar(&agentN, "agent", "", "nama custom agent (.jenderal/agents/)")
	cmd.Flags().BoolVar(&yolo, "yolo", false, "setujui semua izin otomatis (hanya sandbox/CI)")
	cmd.Flags().StringVar(&sessID, "session", "", "lanjutkan sesi tertentu")
	cmd.Flags().BoolVar(&cont, "continue", false, "lanjutkan sesi terakhir")
	cmd.Flags().StringVar(&mode, "mode", "build", "mode agen: build | plan")
	return cmd
}

func summarizeToolArgs(args string) string {
	var m map[string]any
	if json.Unmarshal([]byte(args), &m) != nil {
		return ""
	}
	for _, k := range []string{"command", "path", "url", "pattern"} {
		if s, ok := m[k].(string); ok && s != "" {
			s = strings.ReplaceAll(s, "\n", " ")
			if len(s) > 80 {
				s = s[:80] + "…"
			}
			return "→ " + s
		}
	}
	return ""
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func printJSONL(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

// terminalPermResolver meminta keputusan izin di terminal.
func terminalPermResolver(ctx context.Context, req agent.PermRequest) agent.PermResponse {
	fmt.Fprintf(os.Stderr, "\n⚠ izin %s diperlukan:\n", req.Tool)
	if req.Preview != "" {
		for _, ln := range strings.Split(req.Preview, "\n") {
			fmt.Fprintf(os.Stderr, "  │ %s\n", ln)
		}
	} else if req.Target != "" {
		fmt.Fprintf(os.Stderr, "  │ %s\n", req.Target)
	}
	fmt.Fprintln(os.Stderr, "  [1] sekali  [2] selalu untuk sesi ini  [3] tolak")
	for {
		fmt.Fprint(os.Stderr, "pilihan: ")
		var answer string
		fmt.Scanln(&answer)
		switch strings.TrimSpace(answer) {
		case "1", "y", "ya", "yes":
			return agent.PermResponse{Decision: permission.Allow}
		case "2", "a":
			return agent.PermResponse{Decision: permission.Allow, Always: true}
		case "3", "n", "tidak", "no":
			return agent.PermResponse{Decision: permission.Deny}
		}
	}
}

// ---- serve ----

func serveCmd() *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Jalankan API HTTP+SSE lokal untuk klien lain (desktop, editor)",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := bootstrap(nil)
			if err != nil {
				return err
			}
			defer a.store.Close()
			srv := server.New(server.Options{Config: a.cfg, Registry: a.reg, Store: a.store})
			addr := fmt.Sprintf("127.0.0.1:%d", port)
			fmt.Printf("jenderal server berjalan di http://%s\n", addr)
			fmt.Printf("token akses: %s\n", srv.Token())
			fmt.Println("endpoint: POST /session · GET /session · POST /session/{id}/message · GET /session/{id}/events (SSE) · POST /session/{id}/abort · GET /models")
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			go func() {
				<-ctx.Done()
				os.Exit(0)
			}()
			return srv.ListenAndServe(addr)
		},
	}
	cmd.Flags().IntVar(&port, "port", 4096, "port HTTP lokal")
	return cmd
}

// ---- desktop (placeholder jujur) ----

func desktopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "desktop",
		Short: "Buka aplikasi desktop (Wails) — belum tersedia di rilis ini",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("aplikasi desktop (Wails) direncanakan untuk v0.5; sementara gunakan:\n" +
				"  jenderal            (TUI)\n" +
				"  jenderal serve      (API untuk klien GUI)")
		},
	}
}

// ---- TUI ----

// launchTUI membuka TUI dengan sesi baru atau sesi lanjutan.
func launchTUI(a *app, continueLast bool, sessID string) error {
	var sess *session.Session
	var err error
	if sessID != "" {
		sess, err = a.store.GetSession(sessID)
		if err != nil {
			return err
		}
	} else if continueLast {
		if list, err2 := a.store.ListSessions(a.dir, "", 1); err2 == nil && len(list) > 0 {
			sess = &list[0]
		}
	}
	if sess == nil {
		sess, err = a.store.CreateSession(a.dir, "", a.cfg.Model())
		if err != nil {
			return err
		}
	}
	if a.cfg.Model() == "" {
		ready := a.reg.IDs()
		sort.Strings(ready)
		fmt.Fprintln(os.Stderr, "Catatan: belum ada model default. Atur \"model\" di jenderal.jsonc atau jalankan `jenderal auth login <provider>` lalu pilih model dengan /model di dalam TUI.")
		if len(ready) == 0 {
			fmt.Fprintln(os.Stderr, "Tidak ada provider dengan kredensial. Contoh cepat:")
			fmt.Fprintln(os.Stderr, "  export ZAI_API_KEY=...        # atau OPENAI_API_KEY, ANTHROPIC_API_KEY, dll.")
			fmt.Fprintln(os.Stderr, "  jenderal auth login zai       # simpan permanen")
			fmt.Fprintln(os.Stderr, "  Ollama lokal tidak butuh API key: pastikan `ollama serve` berjalan.")
		}
	}
	return tui.Run(&tui.AppContext{Cfg: a.cfg, Reg: a.reg, Store: a.store}, sess, nil)
}
