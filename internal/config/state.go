package config

// State kecil lintas sesi: pilihan terakhir pengguna (model aktif, dll.)
// disimpan di DataDir/state.json agar TUI membuka dengan pilihan yang sama.

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// StatePath lokasi file state.
func StatePath() string {
	return filepath.Join(DataDir(), "state.json")
}

// LoadState membaca seluruh state; map kosong bila belum ada/rusak.
func LoadState() map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(StatePath())
	if err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

// SetState menyimpan satu kunci state (best-effort, merge dengan yang lama).
func SetState(key, value string) {
	if key == "" {
		return
	}
	state := LoadState()
	state[key] = value
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	if err := EnsureDirs(); err != nil {
		return
	}
	_ = os.WriteFile(StatePath(), append(b, '\n'), 0o644)
}

// LastModel model terakhir yang dipilih pengguna, atau fallback.
func LastModel(fallback string) string {
	if v := LoadState()["last_model"]; v != "" {
		return v
	}
	return fallback
}
