package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mohammadirham37/jenderal_code/internal/agent"
	"github.com/mohammadirham37/jenderal_code/internal/bus"
	"github.com/mohammadirham37/jenderal_code/internal/config"
	"github.com/mohammadirham37/jenderal_code/internal/provider"
	"github.com/mohammadirham37/jenderal_code/internal/session"
)

// TestTUIFullAccessWriteMurni membuktikan Tab×2 → FULL ACCESS membuat tool
// write dieksekusi tanpa dialog izin pada agen awal TUI.
func TestTUIFullAccessWriteMurni(t *testing.T) {
	dir := t.TempDir()
	cfg := config.FromMap(map[string]any{"model": "mock/mock-1"})
	mock := provider.NewMock("mock", nil, false)
	reg := provider.NewRegistryForTest(mock)
	store, err := session.Open(filepath.Join(dir, "data", "fa.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	sess, err := store.CreateSession(dir, "", "mock/mock-1")
	if err != nil {
		t.Fatal(err)
	}
	ag, err := agent.New(agent.Options{
		Config: cfg, Registry: reg, Store: store, Session: sess, Bus: bus.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(&AppContext{Cfg: cfg, Reg: reg, Store: store, Keys: nil}, ag)

	// Sebelum: mode BUILD → write harus Ask (tidak auto-allow).
	if dec := ag.Perm.Check("write", "x.txt", ag.Tools.List()[0].DefaultPerm()); dec.Allowed() {
		t.Fatal("mode BUILD tidak boleh auto-allow")
	}

	// Tab dua kali: BUILD → PLAN → FULL ACCESS.
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if ag.Mode != agent.ModePlan {
		t.Fatalf("setelah Tab 1 mode = %q, want plan", ag.Mode)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if ag.Mode != agent.ModeFull {
		t.Fatalf("setelah Tab 2 mode = %q, want full", ag.Mode)
	}

	// Full access: write langsung dijalankan tanpa resolver.
	w, _ := ag.Tools.Get("write")
	res, err := w.Exec(nil, map[string]any{"path": "langsung.txt", "content": "isi"})
	if err != nil || res.Err {
		t.Fatalf("write di full access gagal: %v / %s", err, res.Content)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "langsung.txt")); string(b) != "isi" {
		t.Errorf("file tidak tertulis: %q", string(b))
	}
}

// TestTUIDialogIzinMuncul end-to-end headless: agen awal TUI yang memanggil
// write harus menampilkan dialog izin (bug lama: auto-tolak tanpa dialog),
// lalu file tertulis setelah pengguna menyetujui.
func TestTUIDialogIzinMuncul(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JENDERAL_DATA_DIR", filepath.Join(dir, "data"))
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := config.FromMap(map[string]any{"model": "mock/mock-1"})
	mock := provider.NewMock("mock", []provider.MockTurn{
		{
			ToolCalls: []provider.ToolCall{{
				Name: "write",
				Args: `{"path": "izin-uji.txt", "content": "disetujui"}`,
			}},
			Text: "berhasil menulis file izin-uji",
		},
	}, false)
	reg := provider.NewRegistryForTest(mock)
	store, err := session.Open(filepath.Join(dir, "data", "uji.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	sess, err := store.CreateSession(dir, "", "mock/mock-1")
	if err != nil {
		t.Fatal(err)
	}

	app := &AppContext{Cfg: cfg, Reg: reg, Store: store}
	inputR, inputW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inputW.Close()
	out := &strings.Builder{}
	done := make(chan error, 1)
	go func() {
		done <- Run(app, sess, nil,
			tea.WithInput(inputR), tea.WithOutput(out), tea.WithoutSignalHandler())
	}()
	time.Sleep(500 * time.Millisecond)

	// Kirim pesan → mock memanggil write → dialog izin harus tampil.
	inputW.Write([]byte("tolong simpan file"))
	time.Sleep(200 * time.Millisecond)
	inputW.Write([]byte("\r"))

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), "Izin diperlukan") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "Izin diperlukan") {
		t.Fatalf("dialog izin tidak muncul di agen awal TUI.\n--- output:\n%s", out.String())
	}

	// Setujui "selalu untuk sesi ini" (pilihan 2).
	time.Sleep(200 * time.Millisecond)
	inputW.Write([]byte("2"))

	deadline = time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(filepath.Join(dir, "izin-uji.txt")); err == nil && string(b) == "disetujui" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	b, err := os.ReadFile(filepath.Join(dir, "izin-uji.txt"))
	if err != nil || string(b) != "disetujui" {
		t.Fatalf("file tidak tertulis setelah izin disetujui: %v %q\n--- output:\n%s", err, string(b), out.String())
	}

	// Tunggu agen selesai merespons sebelum keluar.
	deadline = time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), "berhasil menulis file izin-uji") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	inputW.Write([]byte("\x03"))
	time.Sleep(300 * time.Millisecond)
	inputW.Write([]byte("\x03"))
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("program tidak keluar.\n--- output:\n%s", out.String())
	}
}
