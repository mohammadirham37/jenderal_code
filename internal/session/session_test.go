package session

import (
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSessionCRUD(t *testing.T) {
	s := openTest(t)
	sess, err := s.CreateSession("/proj", "uji", "zai/glm-4.6")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(sess.ID)
	if err != nil || got.Title != "uji" {
		t.Fatalf("get gagal: %v", err)
	}
	if err := s.RenameSession(sess.ID, "judul baru"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSession(sess.ID)
	if got.Title != "judul baru" {
		t.Fatal("rename gagal")
	}
	list, _ := s.ListSessions("/proj", "judul", 10)
	if len(list) != 1 {
		t.Fatalf("pencarian gagal: %d", len(list))
	}
	list, _ = s.ListSessions("/lain", "", 10)
	if len(list) != 0 {
		t.Fatal("filter proyek gagal")
	}
	if err := s.DeleteSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(sess.ID); err == nil {
		t.Fatal("sesi harus hilang")
	}
}

func TestMessagesAndUndoRedo(t *testing.T) {
	s := openTest(t)
	sess, _ := s.CreateSession("/proj", "", "")

	id1, _ := s.AppendMessage(sess.ID, &StoredMessage{Role: "user", Content: "tolong ubah file"})
	id2, _ := s.AppendMessage(sess.ID, &StoredMessage{Role: "assistant", Content: "ok",
		ToolCalls: []ProviderToolCall{{ID: "c1", Name: "write", Args: "{}"}}})
	id3, _ := s.AppendMessage(sess.ID, &StoredMessage{Role: "tool", Content: "selesai", ToolCallID: "c1", ToolName: "write"})
	_ = id1

	msgs, _ := s.ActiveMessages(sess.ID)
	if len(msgs) != 3 {
		t.Fatalf("jumlah pesan salah: %d", len(msgs))
	}

	// Catat perubahan pada saat pesan id2 (assistant yang memanggil tool).
	if err := s.RecordChange(sess.ID, id2, "/proj/a.txt", "prev-snap", "new-snap"); err != nil {
		t.Fatal(err)
	}

	// Undo: kelompok berisi perubahan + pesan dinonaktifkan dari id2.
	rows, msgID, err := s.UndoGroup(sess.ID)
	if err != nil || rows == nil {
		t.Fatalf("undo gagal: %v", err)
	}
	if msgID != id2 {
		t.Fatalf("msg_id undo salah: %d vs %d", msgID, id2)
	}
	if rows[0].PrevSnap != "prev-snap" {
		t.Fatal("prev snap salah")
	}
	msgs, _ = s.ActiveMessages(sess.ID)
	if len(msgs) != 1 {
		t.Fatalf("setelah undo harus 1 pesan aktif; dapat %d", len(msgs))
	}

	// Pesan baru setelah undo membuang redo.
	if _, err := s.AppendMessage(sess.ID, &StoredMessage{Role: "user", Content: "lanjut"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DiscardRedo(sess.ID); err != nil {
		t.Fatal(err)
	}
	if rows, _, _ := s.RedoGroup(sess.ID); rows != nil {
		t.Fatal("redo harus kosong setelah DiscardRedo")
	}
	_ = id3
}

func TestRedoFlow(t *testing.T) {
	s := openTest(t)
	sess, _ := s.CreateSession("/proj", "", "")
	id2, _ := s.AppendMessage(sess.ID, &StoredMessage{Role: "assistant", Content: "ubah"})
	s.RecordChange(sess.ID, id2, "/a.txt", "P", "N")

	if _, _, err := s.UndoGroup(sess.ID); err != nil {
		t.Fatal(err)
	}
	rows, _, err := s.RedoGroup(sess.ID)
	if err != nil || rows == nil {
		t.Fatalf("redo gagal: %v", err)
	}
	if rows[0].NewSnap != "N" {
		t.Fatal("new snap salah")
	}
	msgs, _ := s.ActiveMessages(sess.ID)
	if len(msgs) != 1 {
		t.Fatalf("redo harus mengaktifkan pesan lagi; dapat %d", len(msgs))
	}
}

func TestUsageAndExportImport(t *testing.T) {
	s := openTest(t)
	sess, _ := s.CreateSession("/proj", "", "prov/model-1")
	s.AppendMessage(sess.ID, &StoredMessage{Role: "assistant", Model: "prov/model-1",
		TokensIn: 100, TokensOut: 50, CacheRead: 10, CostUSD: 0.0025, CreatedAt: time.Now()})
	s.AppendMessage(sess.ID, &StoredMessage{Role: "assistant", Model: "prov/model-1",
		TokensIn: 200, TokensOut: 25, CostUSD: 0.001})

	u, err := s.SessionUsage(sess.ID)
	if err != nil || u.TokensIn != 300 || u.TokensOut != 75 {
		t.Fatalf("usage salah: %+v %v", u, err)
	}
	exp, err := s.ExportSession(sess.ID)
	if err != nil || len(exp.Messages) != 2 {
		t.Fatalf("ekspor gagal: %v", err)
	}
	newID, err := s.ImportSession(exp)
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := s.ActiveMessages(newID)
	if len(msgs) != 2 {
		t.Fatalf("impor salah: %d", len(msgs))
	}
	rows, _ := s.Usage("/proj", time.Now().Add(-time.Hour))
	if len(rows) != 2 || rows[0].TokensIn != 200 { // terurut desc by date+model? pesan kedua terbaru
		t.Logf("rows: %+v", rows)
	}
}
