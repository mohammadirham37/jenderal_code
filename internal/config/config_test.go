package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseJSONC(t *testing.T) {
	src := []byte(`{
		// komentar
		"model": "zai/glm-4.6", // komentar belakang
		"provider": {
			"lokal": {
				"type": "openai-compatible",
				"base_url": "http://localhost:11434/v1",
			},
		},
	}`)
	m, err := Parse(src)
	if err != nil {
		t.Fatalf("parse gagal: %v", err)
	}
	if m["model"] != "zai/glm-4.6" {
		t.Fatalf("model salah: %v", m["model"])
	}
	prov := m["provider"].(map[string]any)
	if prov["lokal"] == nil {
		t.Fatal("provider.lokal hilang")
	}
}

func TestLayeredLoad(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global")
	projDir := filepath.Join(dir, "proj")
	os.MkdirAll(globalDir, 0o755)
	os.MkdirAll(projDir, 0o755)

	os.WriteFile(filepath.Join(globalDir, "jenderal.jsonc"),
		[]byte(`{"theme": "dark", "currency": "USD", "step_limit": 30}`), 0o644)
	os.WriteFile(filepath.Join(projDir, "jenderal.jsonc"),
		[]byte(`{"theme": "catppuccin", "permission": {"edit": "ask"}}`), 0o644)

	t.Setenv("JENDERAL_CONFIG_DIR", globalDir)
	os.Unsetenv("JENDERAL_THEME")
	cfg, err := Load(projDir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme() != "catppuccin" {
		t.Fatalf("proyek harus menimpa global; dapat %s", cfg.Theme())
	}
	if cfg.Currency() != "USD" {
		t.Fatalf("currency global harus dipertahankan; dapat %s", cfg.Currency())
	}
	if cfg.StepLimit() != 30 {
		t.Fatalf("step_limit global; dapat %d", cfg.StepLimit())
	}
	if cfg.String("permission.edit", "") != "ask" {
		t.Fatal("permission.edit hilang")
	}
}

func TestEnvInterpolation(t *testing.T) {
	t.Setenv("SECRET_KEY_XYZ", "abc123")
	m, err := Parse([]byte(`{"provider": {"x": {"api_key": "{env:SECRET_KEY_XYZ}"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg := FromMap(m)
	if got := cfg.String("provider.x.api_key", ""); got != "abc123" {
		t.Fatalf("interpolasi gagal: %q", got)
	}
}

func TestEnvOverrideKeys(t *testing.T) {
	t.Setenv("JENDERAL_MODEL", "openai/gpt-4o")
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model() != "openai/gpt-4o" {
		t.Fatalf("env override model gagal: %q", cfg.Model())
	}
}

func TestBudgetAndPaths(t *testing.T) {
	cfg := FromMap(map[string]any{
		"budget":        map[string]any{"per_session_usd": 2.5},
		"exchange_rate": 16000.0,
	})
	if cfg.BudgetPerSession() != 2.5 {
		t.Fatal("budget salah")
	}
	if cfg.ExchangeRate() != 16000 {
		t.Fatal("exchange_rate salah")
	}
	if ConfigDir() == "" || DataDir() == "" || CacheDir() == "" {
		t.Fatal("path kosong")
	}
}
