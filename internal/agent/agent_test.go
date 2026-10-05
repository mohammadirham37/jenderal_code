package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mohammadirham37/jenderal_code/internal/config"
	"github.com/mohammadirham37/jenderal_code/internal/permission"
	"github.com/mohammadirham37/jenderal_code/internal/provider"
	"github.com/mohammadirham37/jenderal_code/internal/session"
)

// setup membangun lingkungan agent test lengkap di direktori sementara.
func setup(t *testing.T, turns []provider.MockTurn) (*Agent, *session.Store, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("JENDERAL_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("JENDERAL_CONFIG_DIR", filepath.Join(dir, "cfg"))
	t.Setenv("JENDERAL_CACHE_DIR", filepath.Join(dir, "cache"))
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	// Pastikan snapshot masuk direktori test.
	t.Setenv("JENDERAL_DATA_DIR", filepath.Join(dir, "data"))
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	cfg := config.FromMap(map[string]any{
		"model": "mock/mock-1", "step_limit": 5.0,
	})
	mock := provider.NewMock("mock", turns, false)
	regMock := provider.NewRegistryForTest(mock)

	store, err := session.Open(filepath.Join(dir, "data", "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	sess, err := store.CreateSession(dir, "uji", "mock/mock-1")
	if err != nil {
		t.Fatal(err)
	}
	ag, err := New(Options{Config: cfg, Registry: regMock, Store: store, Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	ag.Perm.SetYolo(true) // test: izinkan semua
	return ag, store, dir
}

func TestAgentTextOnly(t *testing.T) {
	ag, _, _ := setup(t, []provider.MockTurn{
		{Text: "jawaban akhir dari mock", Usage: provider.Usage{InputTokens: 10, OutputTokens: 5}},
	})
	out, err := ag.Run(context.Background(), "halo")
	if err != nil {
		t.Fatal(err)
	}
	if out != "jawaban akhir dari mock" {
		t.Fatalf("output salah: %q", out)
	}
	msgs, _ := ag.Store.ActiveMessages(ag.Sess.ID)
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Fatalf("pesan tersimpan salah: %+v", msgs)
	}
	if msgs[1].TokensOut != 5 {
		t.Fatalf("usage tidak tersimpan: %+v", msgs[1])
	}
	if ag.CostUSD() != 0 { // harga mock 0
		t.Fatal("biaya mock harus 0")
	}
}

func TestAgentToolLoopWritesFile(t *testing.T) {
	ag, _, dir := setup(t, []provider.MockTurn{
		{ToolCalls: []provider.ToolCall{{
			ID: "call-1", Name: "write",
			Args: `{"path": "hasil.txt", "content": "isi dari agen"}`,
		}}},
		{Text: "file sudah dibuat"},
	})
	out, err := ag.Run(context.Background(), "buat file hasil.txt")
	if err != nil {
		t.Fatal(err)
	}
	if out != "file sudah dibuat" {
		t.Fatalf("output salah: %q", out)
	}
	b, err := os.ReadFile(filepath.Join(dir, "hasil.txt"))
	if err != nil || string(b) != "isi dari agen" {
		t.Fatalf("file tidak ditulis: %v %q", err, b)
	}
	msgs, _ := ag.Store.ActiveMessages(ag.Sess.ID)
	roles := []string{}
	for _, m := range msgs {
		roles = append(roles, m.Role)
	}
	want := []string{"user", "assistant", "tool", "assistant"}
	if strings.Join(roles, ",") != strings.Join(want, ",") {
		t.Fatalf("urutan role salah: %v", roles)
	}
	// Snapshot tercatat untuk undo.
	rows, _, err := ag.Store.UndoGroup(ag.Sess.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("perubahan tidak tercatat: %v %v", rows, err)
	}
	_ = rows
}

func TestAgentPlanModeDeniesWrite(t *testing.T) {
	ag, _, dir := setup(t, []provider.MockTurn{
		{ToolCalls: []provider.ToolCall{{
			ID: "call-1", Name: "write",
			Args: `{"path": "tidak-boleh.txt", "content": "x"}`,
		}}},
		{Text: "selesai"},
	})
	ag.SetMode(ModePlan)
	_, err := ag.Run(context.Background(), "coba tulis")
	if err != nil {
		t.Fatal(err)
	}
	if _, serr := os.Stat(filepath.Join(dir, "tidak-boleh.txt")); serr == nil {
		t.Fatal("mode plan tidak boleh menulis file")
	}
	// Dalam mode plan, tool mutator tidak ditawarkan.
	for _, d := range ag.activeTools() {
		if d.Name == "write" || d.Name == "bash" {
			t.Fatalf("tool mutator %s tidak boleh ada di mode plan", d.Name)
		}
	}
}

func TestAgentPermissionDeny(t *testing.T) {
	ag, _, dir := setup(t, []provider.MockTurn{
		{ToolCalls: []provider.ToolCall{{
			ID: "call-1", Name: "write",
			Args: `{"path": "izin.txt", "content": "x"}`,
		}}},
		{Text: "oke"},
	})
	ag.Perm.SetYolo(false)
	// Resolver pengguna menolak semua.
	ag.PermResolver = func(ctx context.Context, req PermRequest) PermResponse {
		return PermResponse{Decision: permission.Deny}
	}
	_, err := ag.Run(context.Background(), "tulis file")
	if err != nil {
		t.Fatal(err)
	}
	if _, serr := os.Stat(filepath.Join(dir, "izin.txt")); serr == nil {
		t.Fatal("file tidak boleh ditulis saat izin ditolak")
	}
	// Model menerima pesan ERROR agar bisa memperbaiki diri.
	msgs, _ := ag.Store.ActiveMessages(ag.Sess.ID)
	found := false
	for _, m := range msgs {
		if m.Role == "tool" && strings.Contains(m.Content, "izin ditolak") {
			found = true
		}
	}
	if !found {
		t.Fatal("pesan penolakan izin tidak sampai ke model")
	}
}

func TestAgentDeniesEnvRead(t *testing.T) {
	ag, _, dir := setup(t, []provider.MockTurn{
		{ToolCalls: []provider.ToolCall{{
			ID: "call-1", Name: "read", Args: `{"path": ".env"}`,
		}}},
		{Text: "sudah dibaca"},
	})
	os.WriteFile(filepath.Join(dir, ".env"), []byte("RAHASIA=1"), 0o644)
	_, err := ag.Run(context.Background(), "baca .env")
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := ag.Store.ActiveMessages(ag.Sess.ID)
	for _, m := range msgs {
		if m.Role == "tool" && strings.Contains(m.Content, "RAHASIA") {
			t.Fatal("isi .env tidak boleh terbaca")
		}
	}
}

func TestAgentStepLimit(t *testing.T) {
	// Mock terus memanggil tool read tanpa akhir → kena batas langkah.
	loop := provider.NewMock("mock", []provider.MockTurn{
		{ToolCalls: []provider.ToolCall{{ID: "x", Name: "read", Args: `{"path": "a.txt"}`}}},
	}, true)
	dir := t.TempDir()
	t.Setenv("JENDERAL_DATA_DIR", filepath.Join(dir, "data"))
	config.EnsureDirs()
	cfg := config.FromMap(map[string]any{"model": "mock/mock-1", "step_limit": 3.0})
	regMock := provider.NewRegistryForTest(loop)
	store, _ := session.Open(filepath.Join(dir, "data", "x.db"))
	defer store.Close()
	sess, _ := store.CreateSession(dir, "", "mock/mock-1")
	ag, err := New(Options{Config: cfg, Registry: regMock, Store: store, Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	ag.Perm.SetYolo(true)
	_, err = ag.Run(context.Background(), "loop")
	if err == nil || !strings.Contains(err.Error(), "batas langkah") {
		t.Fatalf("harus kena batas langkah: %v", err)
	}
}

func TestSubAgentTask(t *testing.T) {
	// Mock utama memanggil tool task; mock sub tak terpisah dari turn
	// yang sama karena runSubAgent memakai provider yang sama dengan skrip.
	turns := []provider.MockTurn{
		{ToolCalls: []provider.ToolCall{{
			ID: "call-1", Name: "task",
			Args: `{"prompt": "ringkas proyek ini"}`,
		}}},
		{Text: "laporan dari sub-agen: selesai"},
	}
	// Untuk task, sub-agent stream memakai provider yang sama; turn ke-2
	// ("laporan...") dipakai sub-agen, lalu agen utama membaca hasilnya
	// dan turn berikutnya dipakai agen utama (cycle=false → kosong).
	// Beri skrip cukup: tambahkan turn terakhir untuk agen utama.
	mock := provider.NewMock("mock", append(turns, provider.MockTurn{Text: "final utama"}), false)

	dir := t.TempDir()
	t.Setenv("JENDERAL_DATA_DIR", filepath.Join(dir, "data"))
	config.EnsureDirs()
	cfg := config.FromMap(map[string]any{"model": "mock/mock-1", "step_limit": 5.0})
	regMock := provider.NewRegistryForTest(mock)
	store, _ := session.Open(filepath.Join(dir, "data", "t.db"))
	defer store.Close()
	sess, _ := store.CreateSession(dir, "", "mock/mock-1")
	ag, err := New(Options{Config: cfg, Registry: regMock, Store: store, Session: sess})
	if err != nil {
		t.Fatal(err)
	}
	ag.Perm.SetYolo(true)
	out, err := ag.Run(context.Background(), "delegasikan riset")
	if err != nil {
		t.Fatal(err)
	}
	if out != "final utama" {
		t.Fatalf("output utama salah: %q", out)
	}
}
