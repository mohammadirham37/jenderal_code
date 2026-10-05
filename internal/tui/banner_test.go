package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestBannerWord memastikan semua baris tiap kata banner persis sama lebar,
// sehingga huruf tidak bergeser saat dirender.
func TestBannerWord(t *testing.T) {
	for _, word := range []string{"JENDERAL", "CODE"} {
		rows := bannerWord(word)
		if len(rows) != 6 {
			t.Fatalf("%s: jumlah baris %d, want 6", word, len(rows))
		}
		w := lipgloss.Width(rows[0])
		for i, r := range rows {
			if got := lipgloss.Width(r); got != w {
				t.Errorf("%s baris %d lebar %d, want %d (%q)", word, i, got, w, r)
			}
		}
		if w > bannerLebar-2 {
			t.Errorf("%s lebar %d melebihi ambang banner %d", word, w, bannerLebar-2)
		}
	}
}
