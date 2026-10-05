package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mohammadirham37/jenderal_code/internal/bus"
	"github.com/mohammadirham37/jenderal_code/internal/config"
	"github.com/mohammadirham37/jenderal_code/internal/provider"
	"github.com/mohammadirham37/jenderal_code/internal/session"
)

// TestTUICheckout menguji TUI secara headless: render awal, kirim pesan,
// jawaban agen tampil, lalu keluar dengan Ctrl+C dua kali.
func TestTUICheckout(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JENDERAL_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("JENDERAL_CONFIG_DIR", filepath.Join(dir, "cfg"))
	t.Setenv("JENDERAL_CACHE_DIR", filepath.Join(dir, "cache"))
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	cfg := config.FromMap(map[string]any{
		"model": "mock/mock-1", "language": "id", "theme": "jenderal",
	})
	mock := provider.NewMock("mock", []provider.MockTurn{
		{Text: "jawaban dari mock untuk TUI"},
	}, false)
	reg := provider.NewRegistryForTest(mock)
	store, err := session.Open(filepath.Join(dir, "data", "tui.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	sess, err := store.CreateSession(dir, "uji-tui", "mock/mock-1")
	if err != nil {
		t.Fatal(err)
	}
	app := &AppContext{Cfg: cfg, Reg: reg, Store: store}

	// Program dengan input/output pipa (headless).
	inputR, inputW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inputW.Close()
	out := &bytes.Buffer{}

	// Jalankan Run di goroutine dengan opsi program khusus.
	done := make(chan error, 1)
	go func() {
		done <- Run(app, sess, nil,
			tea.WithInput(inputR),
			tea.WithOutput(out),
			tea.WithoutSignalHandler(),
		)
	}()

	// Tunggu program siap.
	time.Sleep(500 * time.Millisecond)

	// Kirim pesan + Enter.
	inputW.Write([]byte("halo dari test"))
	time.Sleep(200 * time.Millisecond)
	inputW.Write([]byte("\r"))

	// Tunggu agen selesai merespons.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), "jawaban dari mock untuk TUI") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "jawaban dari mock untuk TUI") {
		t.Fatalf("jawaban mock tidak muncul di TUI.\n--- output:\n%s", truncateStr(outStr, 2000))
	}
	if !strings.Contains(outStr, "mock/mock-1") {
		t.Errorf("model tidak tampil di status bar")
	}
	if !strings.Contains(outStr, "BUILD") {
		t.Errorf("mode tidak tampil di status bar")
	}

	// Keluar: Ctrl+C dua kali cepat.
	inputW.Write([]byte("\x03"))
	time.Sleep(150 * time.Millisecond)
	inputW.Write([]byte("\x03"))

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program keluar dengan error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("program tidak keluar setelah Ctrl+C 2x")
	}

	// Pesan tersimpan di sesi.
	msgs, _ := store.ActiveMessages(sess.ID)
	if len(msgs) != 2 || msgs[0].Role != "user" || !strings.Contains(msgs[1].Content, "jawaban dari mock") {
		t.Fatalf("pesan sesi salah: %+v", msgs)
	}
}

// TestModelPicker membuka picker model dengan Ctrl+M dan menampilkan model.
func TestModelPicker(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JENDERAL_DATA_DIR", filepath.Join(dir, "data"))
	config.EnsureDirs()
	cfg := config.FromMap(map[string]any{"model": "mock/mock-1"})
	mock := provider.NewMock("mock", nil, false)
	reg := provider.NewRegistryForTest(mock)
	store, _ := session.Open(filepath.Join(dir, "data", "mp.db"))
	defer store.Close()
	sess, _ := store.CreateSession(dir, "", "mock/mock-1")
	app := &AppContext{Cfg: cfg, Reg: reg, Store: store}

	inputR, inputW, _ := os.Pipe()
	defer inputW.Close()
	out := &bytes.Buffer{}
	done := make(chan error, 1)
	go func() {
		done <- Run(app, sess, nil, tea.WithInput(inputR), tea.WithOutput(out), tea.WithoutSignalHandler())
	}()
	time.Sleep(500 * time.Millisecond)
	inputW.Write([]byte("\x0d")) // Ctrl+M
	time.Sleep(300 * time.Millisecond)
	inputW.Write([]byte("\x03\x03"))

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("tidak keluar")
	}
	if !strings.Contains(out.String(), "mock-1") {
		t.Fatalf("picker model tidak menampilkan mock-1:\n%s", truncateStr(out.String(), 1500))
	}
}

var _ = bus.Event{}
