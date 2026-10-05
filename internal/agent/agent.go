// Package agent berisi loop agen: kirim prompt → terima stream → eksekusi
// tool call → kirim hasil → ulang hingga model selesai atau batas langkah
// tercapai. Mendukung mode Build/Plan, kompaksi konteks otomatis, sub-agen,
// custom agent Markdown, interupsi Esc, dan pelacakan biaya.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mohammadirham37/jenderal_code/catalog"
	"github.com/mohammadirham37/jenderal_code/internal/bus"
	"github.com/mohammadirham37/jenderal_code/internal/config"
	"github.com/mohammadirham37/jenderal_code/internal/permission"
	"github.com/mohammadirham37/jenderal_code/internal/provider"
	"github.com/mohammadirham37/jenderal_code/internal/session"
	"github.com/mohammadirham37/jenderal_code/internal/tool"
)

// Mode mode agen: build (boleh ubah file) atau plan (hanya baca).
type Mode string

const (
	ModeBuild Mode = "build"
	ModePlan  Mode = "plan"
)

// PermRequest permintaan izin yang diteruskan ke antarmuka pengguna.
type PermRequest struct {
	ID          string `json:"id"`
	Tool        string `json:"tool"`
	Target      string `json:"target"`
	Description string `json:"description"`
	Preview     string `json:"preview"` // diff/perintah lengkap
}

// PermResponse jawaban pengguna atas permintaan izin.
type PermResponse struct {
	Decision permission.Level `json:"decision"` // allow | deny
	Always   bool             `json:"always"`   // selalu untuk sesi ini
}

// Agent adalah satu agen yang berjalan pada satu sesi.
type Agent struct {
	Cfg    *config.Config
	Reg    *provider.Registry
	Tools  *tool.Registry
	Perm   *permission.Rules
	Store  *session.Store
	Sess   *session.Session
	Todos  *tool.TodoStore
	Ignore *tool.Ignore
	Snap   *tool.SnapshotStore
	Bus    *bus.Bus

	Model      string // "provider/model"
	SmallModel string
	Mode       Mode
	AgentName  string

	// PermResolver dipasang antarmuka (TUI/server/CLI) untuk menampilkan
	// dialog izin. Bila nil, permintaan izin otomatis ditolak.
	PermResolver func(context.Context, PermRequest) PermResponse

	// OnCost dipanggil setiap ada usage baru (untuk dashboard).
	OnCost func(session.UsageRow)

	ctx       context.Context
	cancel    context.CancelFunc
	running   atomic.Bool
	mu        sync.Mutex
	costUSD   float64
	usageTot  provider.Usage
	lastInTok int64
	depth     int // kedalaman sub-agen

	custom     *CustomAgent    // custom agent aktif (nil = default)
	toolFilter map[string]bool // batasi tool (custom agent); nil = semua
}

// Options opsi pembuatan Agent.
type Options struct {
	Config   *config.Config
	Registry *provider.Registry
	Store    *session.Store
	Session  *session.Session
	Bus      *bus.Bus
	Depth    int // 0 = agen utama
}

// New membangun Agent lengkap: tool bawaan, izin, ignore, todo.
func New(o Options) (*Agent, error) {
	cfg := o.Config
	projDir := o.Session.ProjectPath
	ignore := tool.LoadIgnore(projDir)
	todos := tool.NewTodoStore()
	snap := tool.NewSnapshotStore(config.SnapshotDir())

	permRules := permission.New(cfg.Permissions(), projDir, false)

	ag := &Agent{
		Cfg:    cfg,
		Reg:    o.Registry,
		Perm:   permRules,
		Store:  o.Store,
		Sess:   o.Session,
		Todos:  todos,
		Ignore: ignore,
		Snap:   snap,
		Bus:    o.Bus,
		Model:  o.Session.Model,
		Mode:   ModeBuild,
		depth:  o.Depth,
	}
	ag.SmallModel = cfg.SmallModel()
	ag.Tools = tool.NewBuiltinRegistry(projDir, ignore, todos, cfg.BashTimeout())
	// Tool task (sub-agen) hanya untuk agen utama — tidak bersarang.
	if o.Depth == 0 {
		ag.Tools.Add(taskTool{ag: ag})
	}
	if ag.Model == "" {
		ag.Model = cfg.Model()
	}
	return ag, nil
}

// SetModel mengganti model di tengah sesi tanpa kehilangan riwayat.
func (a *Agent) SetModel(ref string) error {
	if ref == "" {
		return fmt.Errorf("referensi model kosong")
	}
	provID, _ := catalog.SplitModelRef(ref)
	if provID == "" {
		// cari provider yang punya model ini
		for _, id := range a.Reg.IDs() {
			p, err := a.Reg.Get(id)
			if err != nil {
				continue
			}
			ms, err := p.Models(context.Background())
			if err != nil {
				continue
			}
			for _, m := range ms {
				if m.ID == ref {
					ref = catalog.JoinModelRef(id, ref)
					provID = id
					break
				}
			}
			if provID != "" {
				break
			}
		}
	}
	if _, err := a.Reg.Get(provID); err != nil {
		return err
	}
	a.Model = ref
	_ = a.Store.SetModel(a.Sess.ID, ref)
	a.publish(bus.Event{Type: bus.EventModelChanged, Text: ref})
	return nil
}

// ToggleMode menukar Build ↔ Plan dan menyiarkan event.
func (a *Agent) ToggleMode() Mode {
	if a.Mode == ModeBuild {
		a.Mode = ModePlan
	} else {
		a.Mode = ModeBuild
	}
	a.publish(bus.Event{Type: bus.EventModeChanged, Text: string(a.Mode)})
	return a.Mode
}

// SetMode memaksa mode tertentu.
func (a *Agent) SetMode(m Mode) {
	a.Mode = m
	a.publish(bus.Event{Type: bus.EventModeChanged, Text: string(m)})
}

// CostUSD total biaya sesi berjalan (USD).
func (a *Agent) CostUSD() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.costUSD
}

// UsageTotal total token sesi berjalan.
func (a *Agent) UsageTotal() provider.Usage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.usageTot
}

// Running true bila agen sedang memproses.
func (a *Agent) Running() bool { return a.running.Load() }

// Abort menghentikan agen yang berjalan (Esc) tanpa merusak sesi.
func (a *Agent) Abort() {
	if a.cancel != nil {
		a.cancel()
	}
}

// Run menjalankan satu giliran agen untuk input pengguna.
// Mengembalikan teks jawaban akhir (bisa kosong bila diinterupsi).
func (a *Agent) Run(ctx context.Context, input string) (string, error) {
	if a.running.Load() {
		return "", fmt.Errorf("agen sedang berjalan; tekan Esc untuk menghentikan")
	}
	if a.Model == "" {
		return "", fmt.Errorf("model belum diatur; jalankan `jenderal auth login <provider>` lalu set model di jenderal.jsonc atau /model")
	}
	runCtx, cancel := context.WithCancel(ctx)
	a.ctx = runCtx
	a.cancel = cancel
	defer cancel()
	a.running.Store(true)
	defer a.running.Store(false)

	// Redo tidak valid setelah percakapan berlanjut.
	_ = a.Store.DiscardRedo(a.Sess.ID)

	// Ekspansi custom command (.jenderal/commands/*.md) hanya untuk input
	// yang bukan slash command UI (UI menangani slash sebelum memanggil Run).
	input = ExpandCustomCommand(a.Sess.ProjectPath, input)

	// Pesan referensi @path → sisipkan isi file.
	input = a.expandAtRefs(input)

	if _, err := a.Store.AppendMessage(a.Sess.ID, &session.StoredMessage{Role: "user", Content: input}); err != nil {
		return "", err
	}
	a.publish(bus.Event{Type: bus.EventMessageStored, Text: "user"})

	final, err := a.loop(runCtx)
	defer a.publish(bus.Event{Type: bus.EventDone})
	if err != nil && runCtx.Err() != nil {
		// Interupsi pengguna: bukan error sesi.
		a.publish(bus.Event{Type: bus.EventStatus, Text: "dihentikan oleh pengguna"})
		return finalText(final), nil
	}
	// Judul otomatis dari model kecil.
	a.maybeTitle()
	return finalText(final), err
}

func finalText(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleAssistant && msgs[i].Text != "" {
			return msgs[i].Text
		}
	}
	return ""
}

// loop adalah inti agent loop (F-AG-01).
func (a *Agent) loop(ctx context.Context) ([]provider.Message, error) {
	provID, modelID := catalog.SplitModelRef(a.Model)
	prov, err := a.Reg.Get(provID)
	if err != nil {
		return nil, err
	}
	meta, _, found, _ := catalog.Model(a.Model)
	if !found {
		meta = catalog.ModelInfo{ID: modelID, ContextWindow: 128000, MaxOutput: 16384, SupportsTools: true}
	}

	system := a.buildSystemPrompt()
	tools := a.activeTools()

	var out []provider.Message
	for step := 1; step <= a.Cfg.StepLimit(); step++ {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		// Kompaksi otomatis bila mendekati batas konteks (F-AG-08).
		if err := a.maybeCompact(ctx, provID, meta); err != nil {
			a.publish(bus.Event{Type: bus.EventStatus, Text: "kompaksi gagal: " + err.Error()})
		}

		// Batas biaya per sesi (P1).
		if budget := a.Cfg.BudgetPerSession(); budget > 0 && a.CostUSD() >= budget {
			a.publish(bus.Event{Type: bus.EventError, Text: fmt.Sprintf("batas biaya sesi USD %.2f tercapai; agen dihentikan", budget)})
			return out, fmt.Errorf("batas biaya sesi tercapai")
		}

		hist, err := a.providerMessages()
		if err != nil {
			return out, err
		}
		req := provider.ChatRequest{
			Model:    modelID,
			System:   system,
			Messages: hist,
			Tools:    tools,
		}
		a.publish(bus.Event{Type: bus.EventStatus, Text: "berpikir…"})

		stream, err := prov.Stream(ctx, req)
		if err != nil {
			return out, err
		}
		asst := provider.Message{Role: provider.RoleAssistant}
		var usage provider.Usage
		var streamErr error
		for ev := range stream {
			switch ev.Type {
			case provider.StreamText:
				asst.Text += ev.Text
				a.publish(bus.Event{Type: bus.EventTextDelta, Text: ev.Text})
			case provider.StreamReasoning:
				a.publish(bus.Event{Type: bus.EventReasoningDelta, Text: ev.Text})
			case provider.StreamToolCall:
				asst.ToolCalls = append(asst.ToolCalls, *ev.ToolCall)
				a.publish(bus.Event{Type: bus.EventToolCall, ToolName: ev.ToolCall.Name, ToolCallID: ev.ToolCall.ID, Args: ev.ToolCall.Args})
			case provider.StreamUsage:
				if ev.Usage != nil {
					usage.Add(*ev.Usage)
				}
			case provider.StreamError:
				streamErr = ev.Err
			}
		}
		if streamErr != nil && asst.Text == "" && len(asst.ToolCalls) == 0 {
			return out, streamErr
		}

		cost := provider.CostUSD(usage, meta)
		a.mu.Lock()
		a.costUSD += cost
		a.usageTot.Add(usage)
		a.lastInTok = usage.InputTokens
		a.mu.Unlock()
		a.publish(bus.Event{Type: bus.EventUsage, Usage: &bus.Usage{
			InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
			CacheReadTokens: usage.CacheReadTokens,
		}, Detail: map[string]any{"cost_usd": cost, "model": a.Model}})

		if asst.Text != "" || len(asst.ToolCalls) > 0 {
			stored := &session.StoredMessage{
				Role: string(provider.RoleAssistant), Content: asst.Text,
				Model: a.Model, TokensIn: usage.InputTokens, TokensOut: usage.OutputTokens,
				CacheRead: usage.CacheReadTokens, CostUSD: cost,
			}
			for _, tc := range asst.ToolCalls {
				stored.ToolCalls = append(stored.ToolCalls, session.ProviderToolCall{ID: tc.ID, Name: tc.Name, Args: tc.Args})
			}
			msgID, err := a.Store.AppendMessage(a.Sess.ID, stored)
			if err != nil {
				return out, err
			}
			_ = msgID
			a.publish(bus.Event{Type: bus.EventMessageStored, Text: "assistant"})
		}
		out = append(out, asst)

		if len(asst.ToolCalls) == 0 {
			return out, nil // model selesai
		}

		// Eksekusi tool calls (F-AG-03: paralel bila semua read-only).
		// Model hanya boleh memanggil tool yang ditawarkan pada permintaan ini.
		offered := map[string]bool{}
		for _, d := range tools {
			offered[d.Name] = true
		}
		results := a.execToolCalls(ctx, asst.ToolCalls, offered)
		out = append(out, results...)

		// Kompaksi juga dicek setelah hasil tool masuk (konteks naik).
		_ = step
	}
	return out, fmt.Errorf("batas langkah agen (%d) tercapai; naikkan step_limit di konfigurasi bila perlu", a.Cfg.StepLimit())
}

// activeTools mengembalikan daftar tool sesuai mode dan filter custom agent.
// Mode Plan hanya membaca: tool mutator tidak ditawarkan ke model.
func (a *Agent) activeTools() []provider.ToolDef {
	var defs []provider.ToolDef
	for _, t := range a.Tools.List() {
		if a.toolFilter != nil && !a.toolFilter[t.Name()] {
			continue
		}
		if a.Mode == ModePlan && !t.ReadOnly() {
			continue
		}
		defs = append(defs, provider.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Schema:      t.Schema(),
		})
	}
	return defs
}

// execToolCalls mengeksekusi daftar tool call dan menghasilkan pesan hasil.
// Read-only dijalankan paralel; mutator berurutan demi keamanan snapshot.
func (a *Agent) execToolCalls(ctx context.Context, calls []provider.ToolCall, offered map[string]bool) []provider.Message {
	// Pisah: run berurutan, tapi kelompok read-only berurutan-berurutan
	// boleh paralel di dalam grupnya.
	type job struct {
		call     provider.ToolCall
		readOnly bool
	}
	jobs := make([]job, len(calls))
	allRO := true
	for i, c := range calls {
		t, ok := a.Tools.Get(c.Name)
		jobs[i] = job{call: c, readOnly: ok && t.ReadOnly()}
		if !jobs[i].readOnly {
			allRO = false
		}
	}

	results := make([]provider.Message, len(calls))
	var wg sync.WaitGroup
	if allRO && len(calls) > 1 {
		for i, j := range jobs {
			wg.Add(1)
			go func(i int, j job) {
				defer wg.Done()
				results[i] = a.execOne(ctx, j.call, offered)
			}(i, j)
		}
		wg.Wait()
	} else {
		for i, j := range jobs {
			results[i] = a.execOne(ctx, j.call, offered)
		}
	}
	return results
}

// execOne mengeksekusi satu tool call: izin → snapshot → eksekusi → simpan hasil.
func (a *Agent) execOne(ctx context.Context, call provider.ToolCall, offered map[string]bool) provider.Message {
	return a.execOnePersist(ctx, call, true, offered)
}

// execOnePersist versi dengan kontrol persistensi (sub-agen tidak persist).
func (a *Agent) execOnePersist(ctx context.Context, call provider.ToolCall, persist bool, offered map[string]bool) provider.Message {
	t, ok := a.Tools.Get(call.Name)
	if !ok {
		return a.toolResultMsg(call, "tool tidak dikenal: "+call.Name, true, persist)
	}
	if offered != nil && !offered[call.Name] {
		return a.toolResultMsg(call, "tool "+call.Name+" tidak tersedia dalam konfigurasi/mode agen ini. Gunakan tool yang ditawarkan saja.", true, persist)
	}
	args := map[string]any{}
	if err := json.Unmarshal([]byte(call.Args), &args); err != nil {
		return a.toolResultMsg(call, "argumen JSON tidak valid: "+err.Error(), true, persist)
	}
	target := targetOf(call.Name, args)

	// Sistem izin (bagian 5.3 PRD).
	dec := a.Perm.Check(call.Name, target, t.DefaultPerm())
	if dec.Level == permission.Ask {
		dec = a.askUser(ctx, call.Name, target, t.Description(), args)
	}
	if dec.Level == permission.Deny {
		a.publish(bus.Event{Type: bus.EventPermissionDone, ToolName: call.Name, Result: "ditolak"})
		return a.toolResultMsg(call, "izin ditolak oleh pengguna untuk: "+call.Name+" "+target+
			". Jangan ulangi aksi ini; tanyakan alternatif kepada pengguna bila perlu.", true, persist)
	}

	// Interupsi sebelum eksekusi.
	if ctx.Err() != nil {
		return a.toolResultMsg(call, "dibatalkan oleh pengguna", true, persist)
	}

	res, err := t.Exec(ctx, args)
	if err != nil {
		res = tool.Result{Err: true, Content: err.Error()}
	}

	// Snapshot file untuk undo/redo (hanya agen utama yang persist).
	if persist {
		if paths := changedPaths(res); len(paths) > 0 {
			msgID, _ := a.Store.MaxMsgID(a.Sess.ID)
			for _, p := range paths {
				if prev, ok := res.Data["prev_content"].(string); ok {
					prevID, err := a.Snap.Save(p, prev)
					if err == nil {
						newContent, _ := readFileForSnap(p)
						newID, _ := a.Snap.Save("new:"+p, newContent)
						_ = a.Store.RecordChange(a.Sess.ID, msgID, p, prevID, newID)
					}
				}
			}
			a.publish(bus.Event{Type: bus.EventStatus, Text: "file berubah: " + strings.Join(paths, ", ")})
		}
	}

	a.publish(bus.Event{
		Type: bus.EventToolResult, ToolName: call.Name, ToolCallID: call.ID,
		Result: truncate(res.Content, 2000), IsErr: res.Err,
	})
	content := res.Content
	if res.Err {
		content = "ERROR: " + content
	}
	return a.toolResultMsg(call, content, false, persist)
}

// askUser memanggil resolver antarmuka; tanpa resolver dianggap tolak.
func (a *Agent) askUser(ctx context.Context, name, target, desc string, args map[string]any) permission.Decision {
	req := PermRequest{
		ID:          fmt.Sprintf("%d", time.Now().UnixNano()),
		Tool:        name,
		Target:      target,
		Description: desc,
		Preview:     previewOf(name, args),
	}
	a.publish(bus.Event{Type: bus.EventPermissionRequest, PermissionID: req.ID,
		ToolName: name, Text: target, Args: req.Preview})
	var resp PermResponse
	if a.PermResolver != nil {
		resp = a.PermResolver(ctx, req)
	}
	if resp.Decision == permission.Allow {
		if resp.Always {
			a.Perm.ApproveSession(name, target)
		}
		return permission.Decision{Level: permission.Allow, Reason: "disetujui pengguna"}
	}
	return permission.Decision{Level: permission.Deny, Reason: "ditolak pengguna"}
}

func (a *Agent) toolResultMsg(call provider.ToolCall, content string, isErr, persist bool) provider.Message {
	m := provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, ToolName: call.Name, Text: content}
	if persist {
		stored := &session.StoredMessage{Role: string(provider.RoleTool), Content: content, ToolCallID: call.ID, ToolName: call.Name}
		if _, err := a.Store.AppendMessage(a.Sess.ID, stored); err == nil {
			a.publish(bus.Event{Type: bus.EventMessageStored, Text: "tool"})
		}
	}
	_ = isErr
	return m
}

func (a *Agent) publish(e bus.Event) {
	if a.Bus != nil {
		e.SessionID = a.Sess.ID
		a.Bus.Publish(e)
	}
}

// targetOf menentukan "target" izin: perintah untuk bash, path untuk file.
func targetOf(name string, args map[string]any) string {
	switch name {
	case "bash":
		return argStr(args, "command")
	case "webfetch":
		return argStr(args, "url")
	default:
		if p := argStr(args, "path"); p != "" {
			return p
		}
		return ""
	}
}

func argStr(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

// previewOf membuat pratinjau untuk dialog izin.
func previewOf(name string, args map[string]any) string {
	switch name {
	case "bash":
		return argStr(args, "command")
	case "write":
		return fmt.Sprintf("tulis file %s (%d byte)", argStr(args, "path"), len(argStr(args, "content")))
	case "edit":
		if _, ok := args["edits"]; ok {
			return fmt.Sprintf("multi-edit %s", argStr(args, "path"))
		}
		return fmt.Sprintf("edit %s:\n- %s\n+ %s", argStr(args, "path"),
			truncate(argStr(args, "old_string"), 300), truncate(argStr(args, "new_string"), 300))
	case "patch":
		return truncate(argStr(args, "diff"), 1500)
	case "webfetch":
		return argStr(args, "url")
	}
	b, _ := json.MarshalIndent(args, "", "  ")
	return truncate(string(b), 800)
}

// changedPaths mengekstrak path file yang berubah dari hasil tool.
func changedPaths(res tool.Result) []string {
	var out []string
	switch v := res.Data["path"].(type) {
	case string:
		if v != "" {
			out = append(out, v)
		}
	}
	if ps, ok := res.Data["paths"].([]string); ok {
		out = append(out, ps...)
	}
	return out
}

func readFileForSnap(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// providerMessages mengonversi pesan tersimpan aktif menjadi bentuk
// provider, melewati pesan ringkasan sebagai konteks user.
func (a *Agent) providerMessages() ([]provider.Message, error) {
	stored, err := a.Store.ActiveMessages(a.Sess.ID)
	if err != nil {
		return nil, err
	}
	out := make([]provider.Message, 0, len(stored))
	for _, m := range stored {
		switch m.Role {
		case "user":
			out = append(out, provider.Message{Role: provider.RoleUser, Text: m.Content})
		case "assistant":
			pm := provider.Message{Role: provider.RoleAssistant, Text: m.Content}
			for _, tc := range m.ToolCalls {
				pm.ToolCalls = append(pm.ToolCalls, provider.ToolCall{ID: tc.ID, Name: tc.Name, Args: tc.Args})
			}
			out = append(out, pm)
		case "tool":
			out = append(out, provider.Message{Role: provider.RoleTool, Text: m.Content,
				ToolCallID: m.ToolCallID, ToolName: m.ToolName})
		case "summary":
			out = append(out, provider.Message{Role: provider.RoleUser, Text: m.Content})
		}
	}
	return out, nil
}
