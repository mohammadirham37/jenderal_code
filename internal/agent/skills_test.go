package agent

import (
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_code/internal/bus"
	"github.com/mohammadirham37/jenderal_code/internal/config"
	"github.com/mohammadirham37/jenderal_code/internal/provider"
	"github.com/mohammadirham37/jenderal_code/internal/session"
)

// newSkillsTestAgent agen dengan mock provider untuk uji skills.
func newSkillsTestAgent(t *testing.T) *Agent {
	t.Helper()
	dir := t.TempDir()
	cfg := config.FromMap(map[string]any{"model": "mock/mock-1"})
	mock := provider.NewMock("mock", nil, false)
	reg := provider.NewRegistryForTest(mock)
	store, err := session.Open(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	sess, err := store.CreateSession(dir, "uji-skill", "mock/mock-1")
	if err != nil {
		t.Fatal(err)
	}
	ag, err := New(Options{Config: cfg, Registry: reg, Store: store, Session: sess, Bus: bus.New()})
	if err != nil {
		t.Fatal(err)
	}
	return ag
}

func TestSkillBuiltinTerdaftar(t *testing.T) {
	ag := newSkillsTestAgent(t)
	sks := ag.ListSkills()
	found := false
	for _, sk := range sks {
		if sk.Name == "buat-dokumen" {
			found = true
		}
	}
	if !found {
		t.Fatal("skill buat-dokumen tidak tersedia untuk agen")
	}
	if _, ok := ag.Tools.Get("use_skill"); !ok {
		t.Error("tool use_skill tidak terdaftar")
	}
	if _, ok := ag.Tools.Get("write_docx"); !ok {
		t.Error("tool write_docx tidak terdaftar")
	}
}

func TestActivateSkillKeSystemPrompt(t *testing.T) {
	ag := newSkillsTestAgent(t)
	prompt := ag.buildSystemPrompt()
	if strings.Contains(prompt, "Skill aktif") {
		t.Error("belum aktif tapi sudah muncul di prompt")
	}
	if err := ag.ActivateSkill("buat-dokumen"); err != nil {
		t.Fatal(err)
	}
	prompt = ag.buildSystemPrompt()
	if !strings.Contains(prompt, "## Skill aktif: buat-dokumen") {
		t.Error("skill aktif tidak masuk system prompt")
	}
	if !strings.Contains(prompt, "write_pptx") {
		t.Error("isi skill tidak masuk system prompt")
	}
	// Aktifkan dua kali tidak menduplikasi.
	_ = ag.ActivateSkill("buat-dokumen")
	if got := len(ag.ActiveSkills()); got != 1 {
		t.Errorf("activeSkills = %d, want 1", got)
	}
	ag.DeactivateSkill("buat-dokumen")
	if got := len(ag.ActiveSkills()); got != 0 {
		t.Errorf("setelah deaktivasi = %d, want 0", got)
	}
}

func TestSkillToolExec(t *testing.T) {
	ag := newSkillsTestAgent(t)
	st, _ := ag.Tools.Get("use_skill")
	res, err := st.Exec(nil, map[string]any{"name": "buat-dokumen"})
	if err != nil || res.Err {
		t.Fatalf("exec gagal: %v / %s", err, res.Content)
	}
	if !strings.Contains(res.Content, "write_docx") {
		t.Error("isi skill tidak dikembalikan")
	}
	res2, _ := st.Exec(nil, map[string]any{"name": "tidak-ada"})
	if !res2.Err {
		t.Error("skill tidak dikenal harusnya error")
	}
	// Skema memuat enum nama skill.
	if _, ok := st.Schema()["properties"].(map[string]any)["name"].(map[string]any)["enum"]; !ok {
		t.Error("schema name harus punya enum nama skill")
	}
}
