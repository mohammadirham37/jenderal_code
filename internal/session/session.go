// Package session menyimpan sesi percakapan di SQLite lokal
// (~/.local/share/jenderalcode/jenderal.db) dengan mode WAL agar crash
// tidak merusak database. Setiap perubahan file dicatat sebagai snapshot
// sehingga /undo dan /redo bisa mengembalikan file dan pesan.
package session

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Session metadata satu sesi.
type Session struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	ProjectPath string    `json:"project_path"`
	Model       string    `json:"model"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// StoredMessage satu pesan tersimpan.
type StoredMessage struct {
	ID         int64              `json:"id"`
	SessionID  string             `json:"session_id"`
	Role       string             `json:"role"`
	Content    string             `json:"content"`
	ToolCalls  []ProviderToolCall `json:"tool_calls,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
	ToolName   string             `json:"tool_name,omitempty"`
	Model      string             `json:"model,omitempty"`
	TokensIn   int64              `json:"tokens_in,omitempty"`
	TokensOut  int64              `json:"tokens_out,omitempty"`
	CacheRead  int64              `json:"cache_read,omitempty"`
	CostUSD    float64            `json:"cost_usd,omitempty"`
	CreatedAt  time.Time          `json:"created_at"`
}

// ProviderToolCall bentuk tersimpan pemanggilan tool (sama dengan
// provider.ToolCall tapi bebas dependensi arah impor).
type ProviderToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

// ChangeRow satu perubahan file tercatat untuk undo/redo.
type ChangeRow struct {
	Path     string `json:"path"`
	PrevSnap string `json:"prev_snapshot"` // id snapshot isi sebelum perubahan
	NewSnap  string `json:"new_snapshot"`  // id snapshot isi sesudah perubahan
	MsgID    int64  `json:"msg_id"`
}

// Store database sesi.
type Store struct {
	db *sql.DB
}

// Open membuka (atau membuat) database di path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // hindari SQLITE_BUSY antar goroutine
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close menutup database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	schema := `
CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL DEFAULT '',
	project_path TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	role TEXT NOT NULL,
	content TEXT NOT NULL DEFAULT '',
	tool_calls TEXT,
	tool_call_id TEXT,
	tool_name TEXT,
	model TEXT,
	tokens_in INTEGER NOT NULL DEFAULT 0,
	tokens_out INTEGER NOT NULL DEFAULT 0,
	cache_read INTEGER NOT NULL DEFAULT 0,
	cost_usd REAL NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	active INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, id);
CREATE TABLE IF NOT EXISTS changes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	msg_id INTEGER NOT NULL,
	path TEXT NOT NULL,
	prev_snap TEXT NOT NULL,
	new_snap TEXT NOT NULL,
	undone INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_changes_session ON changes(session_id, id);
`
	_, err := s.db.Exec(schema)
	return err
}

// NewID membuat ID sesi pendek yang unik.
func NewID() string {
	b := make([]byte, 6)
	rand.Read(b)
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	out := make([]byte, len(b))
	for i, v := range b {
		out[i] = alphabet[int(v)%len(alphabet)]
	}
	return string(out)
}

// CreateSession membuat sesi baru.
func (s *Store) CreateSession(projectPath, title, model string) (*Session, error) {
	now := time.Now()
	sess := &Session{
		ID:          NewID(),
		Title:       title,
		ProjectPath: projectPath,
		Model:       model,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, title, project_path, model, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
		sess.ID, sess.Title, sess.ProjectPath, sess.Model, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// GetSession mengambil satu sesi; error jika tidak ada.
func (s *Store) GetSession(id string) (*Session, error) {
	row := s.db.QueryRow(`SELECT id, title, project_path, model, created_at, updated_at FROM sessions WHERE id=?`, id)
	return scanSession(row)
}

type rowScanner interface{ Scan(dest ...any) error }

func scanSession(row rowScanner) (*Session, error) {
	var sess Session
	var created, updated string
	if err := row.Scan(&sess.ID, &sess.Title, &sess.ProjectPath, &sess.Model, &created, &updated); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("sesi tidak ditemukan")
		}
		return nil, err
	}
	sess.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	sess.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &sess, nil
}

// ListSessions mengembalikan sesi terbaru; query memfilter judul/id.
// projectPath kosong berarti semua proyek.
func (s *Store) ListSessions(projectPath, query string, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 50
	}
	sqlStr := `SELECT id, title, project_path, model, created_at, updated_at FROM sessions`
	var args []any
	var conds []string
	if projectPath != "" {
		conds = append(conds, "project_path=?")
		args = append(args, projectPath)
	}
	if query != "" {
		conds = append(conds, "(title LIKE ? OR id LIKE ?)")
		args = append(args, "%"+query+"%", "%"+query+"%")
	}
	if len(conds) > 0 {
		sqlStr += " WHERE " + joinAnd(conds)
	}
	sqlStr += " ORDER BY updated_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.Query(sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sess)
	}
	return out, rows.Err()
}

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}

// RenameSession mengubah judul sesi.
func (s *Store) RenameSession(id, title string) error {
	_, err := s.db.Exec(`UPDATE sessions SET title=?, updated_at=? WHERE id=?`, title, time.Now().Format(time.RFC3339Nano), id)
	return err
}

// SetModel menyimpan model aktif sesi.
func (s *Store) SetModel(id, model string) error {
	_, err := s.db.Exec(`UPDATE sessions SET model=?, updated_at=? WHERE id=?`, model, time.Now().Format(time.RFC3339Nano), id)
	return err
}

// TouchSession memperbarui updated_at.
func (s *Store) TouchSession(id string) error {
	_, err := s.db.Exec(`UPDATE sessions SET updated_at=? WHERE id=?`, time.Now().Format(time.RFC3339Nano), id)
	return err
}

// DeleteSession menghapus sesi beserta pesan dan catatan perubahannya.
func (s *Store) DeleteSession(id string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id=?`, id)
	return err
}

// AppendMessage menyimpan pesan dan mengembalikan ID-nya.
func (s *Store) AppendMessage(sessionID string, m *StoredMessage) (int64, error) {
	m.SessionID = sessionID
	m.CreatedAt = time.Now()
	var toolCalls any
	if len(m.ToolCalls) > 0 {
		b, err := json.Marshal(m.ToolCalls)
		if err != nil {
			return 0, err
		}
		toolCalls = string(b)
	}
	res, err := s.db.Exec(
		`INSERT INTO messages (session_id, role, content, tool_calls, tool_call_id, tool_name, model, tokens_in, tokens_out, cache_read, cost_usd, created_at, active)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,1)`,
		sessionID, m.Role, m.Content, toolCalls, m.ToolCallID, m.ToolName, m.Model,
		m.TokensIn, m.TokensOut, m.CacheRead, m.CostUSD, m.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	m.ID = id
	return id, err
}

// ActiveMessages mengembalikan pesan aktif terurut (riwayat agen).
func (s *Store) ActiveMessages(sessionID string) ([]StoredMessage, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, role, content, tool_calls, tool_call_id, tool_name, model, tokens_in, tokens_out, cache_read, cost_usd, created_at
		 FROM messages WHERE session_id=? AND active=1 ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

// AllMessages mengembalikan semua pesan termasuk yang non-aktif (ekspor).
func (s *Store) AllMessages(sessionID string) ([]StoredMessage, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, role, content, tool_calls, tool_call_id, tool_name, model, tokens_in, tokens_out, cache_read, cost_usd, created_at
		 FROM messages WHERE session_id=? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

func scanMessages(rows *sql.Rows) ([]StoredMessage, error) {
	var out []StoredMessage
	for rows.Next() {
		var m StoredMessage
		var toolCalls sql.NullString
		var created string
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &toolCalls, &m.ToolCallID, &m.ToolName, &m.Model,
			&m.TokensIn, &m.TokensOut, &m.CacheRead, &m.CostUSD, &created); err != nil {
			return nil, err
		}
		if toolCalls.Valid && toolCalls.String != "" {
			json.Unmarshal([]byte(toolCalls.String), &m.ToolCalls)
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeactivateFrom menonaktifkan pesan dengan id >= from (hasil /undo).
// Mengembalikan jumlah pesan yang dinonaktifkan.
func (s *Store) DeactivateFrom(sessionID string, from int64) (int64, error) {
	res, err := s.db.Exec(`UPDATE messages SET active=0 WHERE session_id=? AND id>=? AND active=1`, sessionID, from)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ReactivateFrom mengaktifkan kembali pesan id >= from (hasil /redo).
func (s *Store) ReactivateFrom(sessionID string, from int64) (int64, error) {
	res, err := s.db.Exec(`UPDATE messages SET active=1 WHERE session_id=? AND id>=? AND active=0`, sessionID, from)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MaxMsgID mengembalikan id pesan terbesar (aktif) dalam sesi.
func (s *Store) MaxMsgID(sessionID string) (int64, error) {
	var id sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(id) FROM messages WHERE session_id=? AND active=1`, sessionID).Scan(&id)
	return id.Int64, err
}

// RecordChange mencatat satu perubahan file untuk undo/redo.
func (s *Store) RecordChange(sessionID string, msgID int64, path, prevSnap, newSnap string) error {
	_, err := s.db.Exec(
		`INSERT INTO changes (session_id, msg_id, path, prev_snap, new_snap, undone) VALUES (?,?,?,?,?,0)`,
		sessionID, msgID, path, prevSnap, newSnap)
	return err
}

// UndoGroup mengambil kelompok perubahan terakhir yang aktif (undone=0),
// menandainya undone, dan menonaktifkan pesan dari msg_id-nya.
// Mengembalikan nil bila tidak ada yang bisa di-undo.
func (s *Store) UndoGroup(sessionID string) ([]ChangeRow, int64, error) {
	var groupID int
	var msgID int64
	err := s.db.QueryRow(
		`SELECT id, msg_id FROM changes WHERE session_id=? AND undone=0 ORDER BY id DESC LIMIT 1`, sessionID).
		Scan(&groupID, &msgID)
	if err == sql.ErrNoRows {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT path, prev_snap, new_snap, msg_id FROM changes WHERE session_id=? AND undone=0 AND id>=?`, sessionID, groupID)
	if err != nil {
		return nil, 0, err
	}
	var out []ChangeRow
	for rows.Next() {
		var c ChangeRow
		if err := rows.Scan(&c.Path, &c.PrevSnap, &c.NewSnap, &c.MsgID); err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, c)
	}
	rows.Close()
	if _, err := s.db.Exec(`UPDATE changes SET undone=1 WHERE session_id=? AND undone=0 AND id>=?`, sessionID, groupID); err != nil {
		return nil, 0, err
	}
	if _, err := s.db.Exec(`UPDATE messages SET active=0 WHERE session_id=? AND id>=? AND active=1`, sessionID, msgID); err != nil {
		return nil, 0, err
	}
	return out, msgID, nil
}

// RedoGroup mengambil kelompok perubahan yang paling baru di-undo,
// menandainya aktif kembali, dan mengaktifkan pesannya.
// Mengembalikan nil bila tidak ada yang bisa di-redo.
func (s *Store) RedoGroup(sessionID string) ([]ChangeRow, int64, error) {
	var groupID int
	var msgID int64
	err := s.db.QueryRow(
		`SELECT id, msg_id FROM changes WHERE session_id=? AND undone=1 ORDER BY id DESC LIMIT 1`, sessionID).
		Scan(&groupID, &msgID)
	if err == sql.ErrNoRows {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT path, prev_snap, new_snap, msg_id FROM changes WHERE session_id=? AND undone=1 AND id>=?`, sessionID, groupID)
	if err != nil {
		return nil, 0, err
	}
	var out []ChangeRow
	for rows.Next() {
		var c ChangeRow
		if err := rows.Scan(&c.Path, &c.PrevSnap, &c.NewSnap, &c.MsgID); err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, c)
	}
	rows.Close()
	if _, err := s.db.Exec(`UPDATE changes SET undone=0 WHERE session_id=? AND undone=1 AND id>=?`, sessionID, groupID); err != nil {
		return nil, 0, err
	}
	if _, err := s.db.Exec(`UPDATE messages SET active=1 WHERE session_id=? AND id>=? AND active=0`, sessionID, msgID); err != nil {
		return nil, 0, err
	}
	return out, msgID, nil
}

// DiscardRedo membuang riwayat redo (dipanggil saat pengguna mengirim
// pesan baru setelah undo).
func (s *Store) DiscardRedo(sessionID string) error {
	_, err := s.db.Exec(`DELETE FROM changes WHERE session_id=? AND undone=1`, sessionID)
	return err
}

// ---- Agregat biaya/token ----

// UsageRow agregat pemakaian.
type UsageRow struct {
	Date      string  `json:"date"`
	Model     string  `json:"model"`
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
	CacheRead int64   `json:"cache_read"`
	CostUSD   float64 `json:"cost_usd"`
	Messages  int64   `json:"messages"`
}

// Usage mengagregasi token & biaya; since kosong berarti semua waktu.
// Hanya sesi pada projectPath yang dihitung (kosong = semua proyek).
func (s *Store) Usage(projectPath string, since time.Time) ([]UsageRow, error) {
	sqlStr := `SELECT substr(m.created_at,1,10) AS d, COALESCE(m.model,''),
		SUM(tokens_in), SUM(tokens_out), SUM(cache_read), SUM(cost_usd), COUNT(*)
		FROM messages m JOIN sessions s ON s.id = m.session_id
		WHERE m.active=1 AND m.model != ''`
	var args []any
	if projectPath != "" {
		sqlStr += " AND s.project_path=?"
		args = append(args, projectPath)
	}
	if !since.IsZero() {
		sqlStr += " AND m.created_at>=?"
		args = append(args, since.Format(time.RFC3339Nano))
	}
	sqlStr += " GROUP BY d, m.model ORDER BY d DESC, m.model"
	rows, err := s.db.Query(sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageRow
	for rows.Next() {
		var r UsageRow
		if err := rows.Scan(&r.Date, &r.Model, &r.TokensIn, &r.TokensOut, &r.CacheRead, &r.CostUSD, &r.Messages); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SessionUsage menghitung total token & biaya satu sesi.
func (s *Store) SessionUsage(sessionID string) (UsageRow, error) {
	var r UsageRow
	err := s.db.QueryRow(
		`SELECT COALESCE(SUM(tokens_in),0), COALESCE(SUM(tokens_out),0), COALESCE(SUM(cache_read),0), COALESCE(SUM(cost_usd),0)
		 FROM messages WHERE session_id=? AND active=1`, sessionID).
		Scan(&r.TokensIn, &r.TokensOut, &r.CacheRead, &r.CostUSD)
	return r, err
}

// ExportFormat bentuk JSON ekspor/impor sesi.
type ExportFormat struct {
	Session  Session         `json:"session"`
	Messages []StoredMessage `json:"messages"`
}

// ExportSession mengekspor sesi ke bentuk siap-JSON.
func (s *Store) ExportSession(id string) (*ExportFormat, error) {
	sess, err := s.GetSession(id)
	if err != nil {
		return nil, err
	}
	msgs, err := s.AllMessages(id)
	if err != nil {
		return nil, err
	}
	return &ExportFormat{Session: *sess, Messages: msgs}, nil
}

// ImportSession membuat sesi baru dari data ekspor dan mengembalikan ID.
func (s *Store) ImportSession(data *ExportFormat) (string, error) {
	if len(data.Messages) == 0 {
		return "", fmt.Errorf("data impor tidak berisi pesan")
	}
	title := data.Session.Title
	if title == "" {
		title = "impor " + time.Now().Format("2006-01-02 15:04")
	}
	sess, err := s.CreateSession(data.Session.ProjectPath, title+" (impor)", data.Session.Model)
	if err != nil {
		return "", err
	}
	for _, m := range data.Messages {
		_, err := s.AppendMessage(sess.ID, &StoredMessage{
			Role: m.Role, Content: m.Content, ToolCalls: m.ToolCalls,
			ToolCallID: m.ToolCallID, ToolName: m.ToolName, Model: m.Model,
			TokensIn: m.TokensIn, TokensOut: m.TokensOut, CacheRead: m.CacheRead, CostUSD: m.CostUSD,
		})
		if err != nil {
			return sess.ID, err
		}
	}
	return sess.ID, nil
}
