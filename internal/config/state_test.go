package config

import (
	"path/filepath"
	"testing"
)

func TestStateRoundtrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JENDERAL_DATA_DIR", filepath.Join(dir, "data"))
	if err := EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if got := LastModel("fallback"); got != "fallback" {
		t.Errorf("state kosong: LastModel = %q", got)
	}
	SetState("last_model", "jenderalrouter/gpt-4o")
	if got := LastModel("fallback"); got != "jenderalrouter/gpt-4o" {
		t.Errorf("LastModel = %q, want jenderalrouter/gpt-4o", got)
	}
	// Nilai lain tidak hilang (merge).
	SetState("tema", "dark")
	if LoadState()["last_model"] != "jenderalrouter/gpt-4o" || LoadState()["tema"] != "dark" {
		t.Errorf("state tidak merge: %v", LoadState())
	}
}
