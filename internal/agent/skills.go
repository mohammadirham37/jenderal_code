package agent

// Integrasi skills dengan agen: tool use_skill (dipakai model sendiri),
// aktivasi manual (/skills di TUI), dan seksi system prompt.

import (
	"context"
	"fmt"
	"strings"

	"github.com/mohammadirham37/jenderal_code/internal/permission"
	"github.com/mohammadirham37/jenderal_code/internal/skills"
	"github.com/mohammadirham37/jenderal_code/internal/tool"
)

// ListSkills mengembalikan semua skill yang tersedia untuk proyek ini.
func (a *Agent) ListSkills() []skills.Skill {
	return skills.List(a.Sess.ProjectPath)
}

// ActivateSkill menandai skill aktif sehingga isinya ikut ke system prompt.
func (a *Agent) ActivateSkill(name string) error {
	sk, err := skills.Get(a.Sess.ProjectPath, name)
	if err != nil {
		return err
	}
	a.skillsMu.Lock()
	defer a.skillsMu.Unlock()
	for _, n := range a.activeSkills {
		if n == sk.Name {
			return nil
		}
	}
	a.activeSkills = append(a.activeSkills, sk.Name)
	return nil
}

// DeactivateSkill melepas skill yang aktif.
func (a *Agent) DeactivateSkill(name string) {
	a.skillsMu.Lock()
	defer a.skillsMu.Unlock()
	out := a.activeSkills[:0]
	for _, n := range a.activeSkills {
		if n != name {
			out = append(out, n)
		}
	}
	a.activeSkills = out
}

// ActiveSkills daftar nama skill aktif.
func (a *Agent) ActiveSkills() []string {
	a.skillsMu.Lock()
	defer a.skillsMu.Unlock()
	return append([]string(nil), a.activeSkills...)
}

// appendSkills melengkapi system prompt: daftar skill yang bisa dimuat
// model lewat tool use_skill, plus isi skill yang sudah diaktifkan.
func (a *Agent) appendSkills(b *strings.Builder) {
	sks := a.ListSkills()
	if len(sks) > 0 {
		b.WriteString("## Skills\n")
		b.WriteString("Skill adalah paket instruksi spesialis. Bila tugas pengguna cocok dengan salah satunya, muat dulu lewat tool use_skill sebelum mulai bekerja:\n")
		for _, sk := range sks {
			fmt.Fprintf(b, "- %s: %s\n", sk.Name, sk.Description)
		}
		b.WriteString("\n")
	}
	for _, name := range a.ActiveSkills() {
		if sk, err := skills.Get(a.Sess.ProjectPath, name); err == nil {
			fmt.Fprintf(b, "## Skill aktif: %s\n%s\n\n", sk.Name, sk.Body)
		}
	}
}

// skillTool implementasi tool use_skill: model memuat sendiri isi skill.
type skillTool struct{ ag *Agent }

func (t skillTool) Name() string { return "use_skill" }

func (t skillTool) Description() string {
	sks := t.ag.ListSkills()
	if len(sks) == 0 {
		return "Muat paket skill (belum ada skill terpasang)."
	}
	names := make([]string, 0, len(sks))
	for _, sk := range sks {
		names = append(names, sk.Name)
	}
	return "Muat paket skill/instruksi ke percakapan. Skill tersedia: " + strings.Join(names, ", ") + "."
}

func (t skillTool) Schema() map[string]any {
	sks := t.ag.ListSkills()
	names := make([]any, 0, len(sks))
	for _, sk := range sks {
		names = append(names, sk.Name)
	}
	nameProp := map[string]any{"type": "string", "description": "Nama skill"}
	if len(names) > 0 {
		nameProp["enum"] = names
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": nameProp,
		},
		"required": []string{"name"},
	}
}

func (t skillTool) DefaultPerm() permission.Level { return permission.Allow }
func (t skillTool) ReadOnly() bool                { return true }

func (t skillTool) Exec(ctx context.Context, args map[string]any) (tool.Result, error) {
	name := strings.TrimSpace(fmt.Sprintf("%v", args["name"]))
	sk, err := skills.Get(t.ag.Sess.ProjectPath, name)
	if err != nil {
		return tool.Result{Err: true, Content: err.Error()}, nil
	}
	return tool.Result{
		Content: fmt.Sprintf("Skill %s dimuat. Ikuti instruksinya:\n\n%s", sk.Name, sk.Body),
		Data:    map[string]any{"skill": sk.Name},
	}, nil
}
