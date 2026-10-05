package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mohammadirham37/jenderal_code/catalog"
	"github.com/mohammadirham37/jenderal_code/internal/bus"
	"github.com/mohammadirham37/jenderal_code/internal/provider"
	"github.com/mohammadirham37/jenderal_code/internal/session"
)

// maybeCompact menjalankan kompaksi otomatis bila token input terakhir
// mendekati compact_percent (default 85%) dari context window model (F-AG-08).
func (a *Agent) maybeCompact(ctx context.Context, provID string, meta catalog.ModelInfo) error {
	if meta.ContextWindow <= 0 || a.lastInTok == 0 {
		return nil
	}
	limit := int64(meta.ContextWindow) * int64(a.Cfg.CompactPercent()) / 100
	if a.lastInTok < limit {
		return nil
	}
	msgs, err := a.Store.ActiveMessages(a.Sess.ID)
	if err != nil {
		return err
	}
	const keep = 8 // simpan beberapa pesan terakhir apa adanya
	if len(msgs) <= keep+2 {
		return nil
	}
	a.publish(bus.Event{Type: bus.EventStatus, Text: "konteks mendekati batas; mengompaksi…"})
	if err := a.Compact(ctx); err != nil {
		return err
	}
	a.lastInTok = 0 // reset pemicu
	return nil
}

// Compact merangkum percakapan lama dengan model kecil dan menonaktifkan
// pesan aslinya. Pesan tetap tersimpan di database (ikut terekspor).
func (a *Agent) Compact(ctx context.Context) error {
	msgs, err := a.Store.ActiveMessages(a.Sess.ID)
	if err != nil {
		return err
	}
	const keep = 8
	if len(msgs) <= keep+2 {
		return fmt.Errorf("percakapan masih pendek; tidak perlu kompaksi")
	}
	old := msgs[:len(msgs)-keep]
	boundary := old[len(old)-1].ID

	summary, err := a.summarize(ctx, old)
	if err != nil {
		return err
	}
	// Nonaktifkan pesan lama dulu, baru simpan ringkasan agar id ringkasan
	// tidak ikut dinonaktifkan oleh DeactivateFrom.
	if _, err := a.Store.DeactivateFrom(a.Sess.ID, boundary+1); err != nil {
		return err
	}
	if _, err := a.Store.AppendMessage(a.Sess.ID, &session.StoredMessage{
		Role:    "summary",
		Content: "[Ringkasan konteks sebelumnya — dibuat otomatis oleh kompaksi]\n\n" + summary,
		Model:   a.Model,
	}); err != nil {
		return err
	}
	a.publish(bus.Event{Type: bus.EventStatus,
		Text: fmt.Sprintf("kompaksi selesai: %d pesan diringkas", len(old))})
	return nil
}

// summarize memanggil model kecil untuk meringkas transkrip.
func (a *Agent) summarize(ctx context.Context, msgs []session.StoredMessage) (string, error) {
	ref := a.SmallModel
	if ref == "" {
		ref = a.Model
	}
	provID, modelID := catalog.SplitModelRef(ref)
	prov, err := a.Reg.Get(provID)
	if err != nil {
		// Fallback ke model utama bila small model tidak tersedia.
		provID, modelID = catalog.SplitModelRef(a.Model)
		prov, err = a.Reg.Get(provID)
		if err != nil {
			return "", err
		}
	}
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case "user":
			fmt.Fprintf(&b, "PENGGUNA: %s\n", truncate(m.Content, 2000))
		case "assistant":
			fmt.Fprintf(&b, "ASISTEN: %s\n", truncate(m.Content, 2000))
		case "tool":
			fmt.Fprintf(&b, "TOOL(%s): %s\n", m.ToolName, truncate(m.Content, 400))
		}
	}
	prompt := "Ringkas percakapan coding berikut untuk konteks agen: keputusan penting, file yang disentuh, perubahan yang sudah dilakukan, dan tugas yang tersisa. Maksimal 300 kata.\n\n" + b.String()

	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	stream, err := prov.Stream(cctx, provider.ChatRequest{
		Model:    modelID,
		System:   "Kamu merangkum percakapan coding secara padat dan akurat.",
		Messages: []provider.Message{{Role: provider.RoleUser, Text: prompt}},
	})
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for ev := range stream {
		switch ev.Type {
		case provider.StreamText:
			out.WriteString(ev.Text)
		case provider.StreamError:
			return "", ev.Err
		}
	}
	return out.String(), nil
}

// maybeTitle membuat judul sesi otomatis dengan model kecil (sekali per sesi).
func (a *Agent) maybeTitle() {
	if a.Sess.Title != "" {
		return
	}
	msgs, err := a.Store.ActiveMessages(a.Sess.ID)
	if err != nil || len(msgs) < 2 {
		return
	}
	var firstUser, firstAsst string
	for _, m := range msgs {
		if m.Role == "user" && firstUser == "" {
			firstUser = truncate(m.Content, 800)
		}
		if m.Role == "assistant" && firstAsst == "" {
			firstAsst = truncate(m.Content, 800)
		}
		if firstUser != "" && firstAsst != "" {
			break
		}
	}
	if firstUser == "" {
		return
	}
	ref := a.SmallModel
	if ref == "" {
		ref = a.Model
	}
	provID, modelID := catalog.SplitModelRef(ref)
	prov, err := a.Reg.Get(provID)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream, err := prov.Stream(ctx, provider.ChatRequest{
		Model:  modelID,
		System: "Beri judul sangat singkat (3-5 kata, tanpa tanda kutip) untuk percakapan ini. Jawab hanya judulnya.",
		Messages: []provider.Message{{Role: provider.RoleUser,
			Text: "Permintaan pengguna: " + firstUser + "\nAwal jawaban: " + firstAsst}},
	})
	if err != nil {
		return
	}
	var title strings.Builder
	for ev := range stream {
		if ev.Type == provider.StreamText {
			title.WriteString(ev.Text)
		}
	}
	t := strings.TrimSpace(strings.Trim(title.String(), "\"'` \n."))
	if t == "" || len(t) > 80 {
		return
	}
	if err := a.Store.RenameSession(a.Sess.ID, t); err == nil {
		a.Sess.Title = t
		a.publish(bus.Event{Type: bus.EventTitle, Text: t})
	}
}
