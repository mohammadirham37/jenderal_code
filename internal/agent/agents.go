package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mohammadirham37/jenderal_code/catalog"
	"github.com/mohammadirham37/jenderal_code/internal/bus"
	"github.com/mohammadirham37/jenderal_code/internal/permission"
	"github.com/mohammadirham37/jenderal_code/internal/provider"
	"github.com/mohammadirham37/jenderal_code/internal/tool"
)

// CustomAgent adalah agen khusus dari file .jenderal/agents/*.md dengan
// frontmatter YAML sederhana: name, description, model, tools.
type CustomAgent struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Model       string   `json:"model"`
	Tools       []string `json:"tools"`
	Prompt      string   `json:"prompt"`
}

// CustomCommand adalah slash command dari .jenderal/commands/*.md.
type CustomCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Template    string `json:"template"`
}

// LoadCustomAgents membaca semua custom agent di proyek.
func LoadCustomAgents(projDir string) []*CustomAgent {
	entries, err := os.ReadDir(filepath.Join(projDir, ".jenderal", "agents"))
	if err != nil {
		return nil
	}
	var out []*CustomAgent
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(projDir, ".jenderal", "agents", e.Name()))
		if err != nil {
			continue
		}
		fm, body := splitFrontmatter(string(b))
		ca := &CustomAgent{Prompt: body}
		ca.Name = fm["name"]
		if ca.Name == "" {
			ca.Name = strings.TrimSuffix(e.Name(), ".md")
		}
		ca.Description = fm["description"]
		ca.Model = fm["model"]
		if tools, ok := fm["tools"]; ok && tools != "" {
			for _, t := range strings.Split(strings.Trim(tools, "[]"), ",") {
				if t = strings.TrimSpace(t); t != "" {
					ca.Tools = append(ca.Tools, t)
				}
			}
		}
		out = append(out, ca)
	}
	return out
}

// LoadCustomCommands membaca semua custom slash command di proyek.
func LoadCustomCommands(projDir string) []CustomCommand {
	entries, err := os.ReadDir(filepath.Join(projDir, ".jenderal", "commands"))
	if err != nil {
		return nil
	}
	var out []CustomCommand
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(projDir, ".jenderal", "commands", e.Name()))
		if err != nil {
			continue
		}
		fm, body := splitFrontmatter(string(b))
		cc := CustomCommand{
			Name:        strings.TrimSuffix(e.Name(), ".md"),
			Description: fm["description"],
			Template:    body,
		}
		out = append(out, cc)
	}
	return out
}

// splitFrontmatter memisahkan frontmatter YAML minimalis (kunci: nilai)
// di antara garis "---" dari isi Markdown.
func splitFrontmatter(content string) (map[string]string, string) {
	fm := map[string]string{}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fm, content
	}
	i := 1
	for ; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			i++
			break
		}
		ln := lines[i]
		if idx := strings.Index(ln, ":"); idx > 0 {
			key := strings.TrimSpace(ln[:idx])
			val := strings.TrimSpace(ln[idx+1:])
			fm[key] = strings.Trim(val, `"'`)
		}
	}
	return fm, strings.Join(lines[i:], "\n")
}

// ExpandCustomCommand mengganti "/nama argumen" dengan template command
// kustom; $ARGUMENTS diganti argumennya. Mengembalikan input asli bila
// tidak cocok.
func ExpandCustomCommand(projDir, input string) string {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") || len(trimmed) < 2 {
		return input
	}
	name := trimmed[1:]
	args := ""
	if i := strings.IndexAny(name, " \t"); i >= 0 {
		args, name = strings.TrimSpace(name[i+1:]), name[:i]
	}
	for _, cc := range LoadCustomCommands(projDir) {
		if cc.Name == name {
			return strings.ReplaceAll(cc.Template, "$ARGUMENTS", args)
		}
	}
	return input
}

// ---- tool task: sub-agen ----

type taskTool struct{ ag *Agent }

func (t taskTool) Name() string { return "task" }
func (t taskTool) Description() string {
	return "Jalankan sub-agen dengan konteks terpisah untuk tugas mandiri (riset, review, analisis besar). Hasil akhir sub-agen dikembalikan ke Anda. Gunakan agent=<nama> untuk memakai custom agent .jenderal/agents/."
}
func (t taskTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{"type": "string", "description": "Tugas lengkap untuk sub-agen"},
			"agent":  map[string]any{"type": "string", "description": "Nama custom agent (opsional)"},
		},
		"required": []string{"prompt"},
	}
}
func (t taskTool) DefaultPerm() permission.Level { return permission.Allow }
func (t taskTool) ReadOnly() bool                { return true } // eksekusinya bisa membaca; izin tool anak tetap dicek
func (t taskTool) Exec(ctx context.Context, args map[string]any) (tool.Result, error) {
	if t.ag.depth > 0 {
		return tool.Result{Err: true, Content: "sub-agen tidak boleh menjalankan sub-agen lagi"}, nil
	}
	prompt := argStr(args, "prompt")
	if strings.TrimSpace(prompt) == "" {
		return tool.Result{Err: true, Content: "prompt wajib diisi"}, nil
	}
	agentName := argStr(args, "agent")

	// Cari custom agent bila diminta.
	var ca *CustomAgent
	for _, c := range LoadCustomAgents(t.ag.Sess.ProjectPath) {
		if c.Name == agentName {
			ca = c
			break
		}
	}
	if agentName != "" && ca == nil {
		return tool.Result{Err: true, Content: "custom agent tidak ditemukan: " + agentName}, nil
	}

	t.ag.publish(bus.Event{Type: bus.EventStatus, Text: "sub-agen bekerja: " + promptApproxAbs(prompt)})
	result, err := t.ag.runSubAgent(ctx, prompt, ca)
	if err != nil {
		return tool.Result{Err: true, Content: err.Error()}, nil
	}
	return tool.Result{Content: result, Data: map[string]any{"subagent": agentName}}, nil
}

func promptApproxAbs(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}

// runSubAgent menjalankan loop agen anak: konteks terpisah, tool terbatas,
// hasil persisten tidak masuk sesi induk.
func (a *Agent) runSubAgent(ctx context.Context, prompt string, ca *CustomAgent) (string, error) {
	if ca == nil {
		ca = &CustomAgent{
			Name:   "sub-agen",
			Prompt: "Anda sub-agen yang mengerjakan satu tugas mandiri dan melapor ringkas. Jangan mengubah file kecuali diminta.",
		}
	}
	provID, modelID := catalog.SplitModelRef(a.Model)
	if ca.Model != "" {
		provID, modelID = catalog.SplitModelRef(ca.Model)
	}
	prov, err := a.Reg.Get(provID)
	if err != nil {
		return "", err
	}

	// Tool terbatas sesuai daftar custom agent; task selalu dikeluarkan.
	allowed := map[string]bool{}
	for _, n := range ca.Tools {
		allowed[n] = true
	}
	var defs []provider.ToolDef
	for _, t := range a.Tools.List() {
		if t.Name() == "task" {
			continue
		}
		if len(allowed) > 0 && !allowed[t.Name()] {
			continue
		}
		if a.Mode == ModePlan && !t.ReadOnly() {
			continue
		}
		defs = append(defs, provider.ToolDef{Name: t.Name(), Description: t.Description(), Schema: t.Schema()})
	}

	system := "Anda sub-agen dari JenderalCode. Kerjakan tugas berikut secara mandiri dan laporkan hasilnya ringkas.\n\n" + ca.Prompt
	messages := []provider.Message{{Role: provider.RoleUser, Text: prompt}}
	var final string
	const maxSteps = 25
	for step := 0; step < maxSteps; step++ {
		if ctx.Err() != nil {
			return final, ctx.Err()
		}
		stream, err := prov.Stream(ctx, provider.ChatRequest{
			Model: modelID, System: system, Messages: messages, Tools: defs,
		})
		if err != nil {
			return final, err
		}
		var asst provider.Message
		for ev := range stream {
			switch ev.Type {
			case provider.StreamText:
				asst.Text += ev.Text
			case provider.StreamToolCall:
				asst.ToolCalls = append(asst.ToolCalls, *ev.ToolCall)
			case provider.StreamError:
				return final, ev.Err
			}
		}
		messages = append(messages, asst)
		if asst.Text != "" {
			final = asst.Text
		}
		if len(asst.ToolCalls) == 0 {
			break
		}
		offered := map[string]bool{}
		for _, d := range defs {
			offered[d.Name] = true
		}
		for _, call := range asst.ToolCalls {
			messages = append(messages, a.execSubTool(ctx, call, offered))
		}
	}
	if final == "" {
		final = "(sub-agen selesai tanpa laporan)"
	}
	return final, nil
}

// execSubTool mengeksekusi tool untuk sub-agen: izin tetap dicek dan hasil
// TIDAK disimpan ke sesi induk.
func (a *Agent) execSubTool(ctx context.Context, call provider.ToolCall, offered map[string]bool) provider.Message {
	t, ok := a.Tools.Get(call.Name)
	if !ok {
		return provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Text: "tool tidak dikenal: " + call.Name}
	}
	if offered != nil && !offered[call.Name] {
		return provider.Message{Role: provider.RoleTool, ToolCallID: call.ID,
			Text: "tool " + call.Name + " tidak tersedia untuk sub-agen ini."}
	}
	args := map[string]any{}
	if err := json.Unmarshal([]byte(call.Args), &args); err != nil {
		return provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, Text: "argumen JSON tidak valid: " + err.Error()}
	}
	target := targetOf(call.Name, args)
	dec := a.Perm.Check(call.Name, target, t.DefaultPerm())
	if dec.Level == permission.Ask {
		// Sub-agen meminjam resolver induk.
		dec = a.askUser(ctx, call.Name, target, t.Description(), args)
	}
	if dec.Level == permission.Deny {
		return provider.Message{Role: provider.RoleTool, ToolCallID: call.ID,
			Text: "izin ditolak untuk " + call.Name + " " + target}
	}
	res, err := t.Exec(ctx, args)
	if err != nil {
		res = tool.Result{Err: true, Content: err.Error()}
	}
	content := res.Content
	if res.Err {
		content = "ERROR: " + content
	}
	return provider.Message{Role: provider.RoleTool, ToolCallID: call.ID, ToolName: call.Name, Text: content}
}

// SwitchAgent mengganti agen aktif (custom agent) di tengah sesi.
func (a *Agent) SwitchAgent(name string) error {
	if name == "" || name == "jenderal" || name == "default" {
		a.custom = nil
		a.toolFilter = nil
		a.AgentName = ""
		return nil
	}
	for _, c := range LoadCustomAgents(a.Sess.ProjectPath) {
		if c.Name == name {
			a.custom = c
			a.AgentName = c.Name
			a.toolFilter = nil
			if len(c.Tools) > 0 {
				a.toolFilter = map[string]bool{}
				for _, n := range c.Tools {
					a.toolFilter[n] = true
				}
			}
			if c.Model != "" {
				if err := a.SetModel(c.Model); err != nil {
					// model kustom mungkin tak tersedia; tetap pakai model lama
					_ = err
				}
			}
			return nil
		}
	}
	return fmt.Errorf("custom agent %q tidak ditemukan (folder .jenderal/agents/)", name)
}

// ListAgents mengembalikan daftar custom agent proyek.
func (a *Agent) ListAgents() []*CustomAgent { return LoadCustomAgents(a.Sess.ProjectPath) }
