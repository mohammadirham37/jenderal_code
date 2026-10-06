package catalog

import "testing"

// TestJenderalRouterBawaan memastikan provider jenderalrouter terpasang di
// katalog bawaan dengan adapter openai dan base URL panel yang benar.
func TestJenderalRouterBawaan(t *testing.T) {
	p, err := Provider("jenderalrouter")
	if err != nil {
		t.Fatalf("provider jenderalrouter tidak ada di katalog: %v", err)
	}
	if p.Adapter != "openai" {
		t.Errorf("adapter = %q, want openai", p.Adapter)
	}
	if p.BaseURL != "https://chat.jenderalpanel.com/v1" {
		t.Errorf("base_url = %q, want https://chat.jenderalpanel.com/v1", p.BaseURL)
	}
	if !p.RequiresKey {
		t.Error("jenderalrouter harus requires_key")
	}
	if p.EnvKey != "JENDERALROUTER_API_KEY" {
		t.Errorf("env_key = %q", p.EnvKey)
	}
	if len(p.Models) == 0 {
		t.Error("minimal satu model awal diperlukan")
	}
}
