// Package server menyediakan API HTTP + SSE lokal (bind 127.0.0.1) agar
// klien lain — desktop, ekstensi editor, SDK — bisa memakai engine agen.
// Akses tanpa token ditolak; token acak dicetak saat server start.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/mohammadirham37/jenderal_code/catalog"
	"github.com/mohammadirham37/jenderal_code/internal/agent"
	"github.com/mohammadirham37/jenderal_code/internal/bus"
	"github.com/mohammadirham37/jenderal_code/internal/config"
	"github.com/mohammadirham37/jenderal_code/internal/mcp"
	"github.com/mohammadirham37/jenderal_code/internal/permission"
	"github.com/mohammadirham37/jenderal_code/internal/provider"
	"github.com/mohammadirham37/jenderal_code/internal/session"
)

// Server HTTP+SSE.
type Server struct {
	cfg      *config.Config
	registry *provider.Registry
	store    *session.Store
	token    string

	mu       sync.Mutex
	agents   map[string]*agent.Agent
	agentBus map[string]*bus.Bus
	pending  map[string]chan agent.PermResponse // id permintaan izin
	subs     map[string]map[chan bus.Event]bool // sessionID -> pelanggan SSE
	mcps     []*mcp.Client
}

// Options opsi server.
type Options struct {
	Config   *config.Config
	Registry *provider.Registry
	Store    *session.Store
}

// New membuat server baru dengan token acak.
func New(o Options) *Server {
	return &Server{
		cfg:      o.Config,
		registry: o.Registry,
		store:    o.Store,
		token:    newToken(),
		agents:   map[string]*agent.Agent{},
		agentBus: map[string]*bus.Bus{},
		pending:  map[string]chan agent.PermResponse{},
		subs:     map[string]map[chan bus.Event]bool{},
	}
}

// Token mengembalikan token akses server ini.
func (s *Server) Token() string { return s.token }

func newToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Handler membangun router chi.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })

	// Semua endpoint lain butuh token.
	r.Group(func(r chi.Router) {
		r.Use(s.auth)
		r.Post("/session", s.createSession)
		r.Get("/session", s.listSessions)
		r.Get("/session/{id}", s.getSession)
		r.Delete("/session/{id}", s.deleteSession)
		r.Post("/session/{id}/message", s.postMessage)
		r.Get("/session/{id}/events", s.events)
		r.Post("/session/{id}/permission/{reqID}", s.answerPermission)
		r.Post("/session/{id}/abort", s.abort)
		r.Post("/session/{id}/model", s.setModel)
		r.Post("/session/{id}/mode", s.setMode)
		r.Get("/models", s.listModels)
	})
	return r
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get("X-Jenderal-Token")
		if tok == "" {
			auth := r.Header.Get("Authorization")
			tok = strings.TrimPrefix(auth, "Bearer ")
		}
		if tok == "" {
			tok = r.URL.Query().Get("token")
		}
		if tok != s.token {
			writeJSON(w, 401, map[string]string{"error": "token tidak valid"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ListenAndServe menjalankan server di addr (mis. 127.0.0.1:4096).
// Bind selain loopback ditolak demi keamanan (PRD 8.2).
func (s *Server) ListenAndServe(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err == nil && host != "" && host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return fmt.Errorf("server lokal hanya boleh bind 127.0.0.1 (diminta %q); bind ke jaringan tidak diizinkan", host)
	}
	srv := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}

// ---- Handler endpoint ----

type createSessionReq struct {
	ProjectPath string `json:"project_path"`
	Model       string `json:"model"`
	Title       string `json:"title"`
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && r.ContentLength > 0 {
		writeJSON(w, 400, map[string]string{"error": "JSON tidak valid"})
		return
	}
	if req.ProjectPath == "" {
		req.ProjectPath = "."
	}
	sess, err := s.store.CreateSession(req.ProjectPath, req.Title, req.Model)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if _, err := s.getOrCreateAgent(r.Context(), sess.ID); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, sess)
}

func (s *Server) getOrCreateAgent(ctx context.Context, id string) (*agent.Agent, error) {
	s.mu.Lock()
	if ag, ok := s.agents[id]; ok {
		s.mu.Unlock()
		return ag, nil
	}
	s.mu.Unlock()

	sess, err := s.store.GetSession(id)
	if err != nil {
		return nil, err
	}
	b := bus.New()
	ag, err := agent.New(agent.Options{
		Config: s.cfg, Registry: s.registry, Store: s.store, Session: sess, Bus: b,
	})
	if err != nil {
		return nil, err
	}
	ag.PermResolver = s.resolverFor(ag)
	// MCP: sambungkan sekali per agen.
	s.mcps = append(s.mcps, mcp.AttachAll(ctx, s.cfg.MCPServers(), ag.Tools, func(msg string) {
		b.Publish(bus.Event{Type: bus.EventStatus, SessionID: id, Text: msg})
	})...)

	s.mu.Lock()
	if existing, ok := s.agents[id]; ok {
		s.mu.Unlock()
		b.Close()
		return existing, nil
	}
	s.agents[id] = ag
	s.agentBus[id] = b
	s.mu.Unlock()
	return ag, nil
}

// resolverFor mengembalikan resolver izin yang menunggu jawaban lewat
// endpoint /permission/{id}; SSE memberi tahu klien.
func (s *Server) resolverFor(ag *agent.Agent) func(context.Context, agent.PermRequest) agent.PermResponse {
	return func(ctx context.Context, req agent.PermRequest) agent.PermResponse {
		ch := make(chan agent.PermResponse, 1)
		s.mu.Lock()
		s.pending[req.ID] = ch
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			delete(s.pending, req.ID)
			s.mu.Unlock()
		}()
		select {
		case resp := <-ch:
			return resp
		case <-ctx.Done():
			return agent.PermResponse{Decision: permission.Deny}
		case <-time.After(5 * time.Minute):
			return agent.PermResponse{Decision: permission.Deny}
		}
	}
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.ListSessions("", r.URL.Query().Get("q"), 100)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, sessions)
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, err := s.store.GetSession(id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	msgs, err := s.store.ActiveMessages(id)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"session": sess, "messages": msgs})
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.mu.Lock()
	if ag, ok := s.agents[id]; ok {
		ag.Abort()
		delete(s.agents, id)
	}
	s.mu.Unlock()
	if err := s.store.DeleteSession(id); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

type messageReq struct {
	Text  string `json:"text"`
	Model string `json:"model"`
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req messageReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Text) == "" {
		writeJSON(w, 400, map[string]string{"error": "field text wajib diisi"})
		return
	}
	ag, err := s.getOrCreateAgent(r.Context(), id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	if req.Model != "" && req.Model != ag.Model {
		if err := ag.SetModel(req.Model); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
	}
	if ag.Running() {
		writeJSON(w, 409, map[string]string{"error": "agen sedang berjalan; hentikan dulu"})
		return
	}
	go func() {
		ctx := context.Background()
		_, _ = ag.Run(ctx, req.Text)
	}()
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

// events mengalirkan event bus sesi sebagai SSE.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	fl, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "streaming tidak didukung"})
		return
	}
	ag, err := s.getOrCreateAgent(r.Context(), id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	ch := make(chan bus.Event, 256)
	s.mu.Lock()
	if s.subs[id] == nil {
		s.subs[id] = map[chan bus.Event]bool{}
	}
	s.subs[id][ch] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs[id], ch)
		s.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	fmt.Fprint(w, ": connected\n\n")
	fl.Flush()

	// Tiap pelanggan SSE berlangganan langsung ke bus agen.
	b := s.busOf(ag)
	if b == nil {
		writeJSON(w, 500, map[string]string{"error": "bus tidak tersedia"})
		return
	}
	subID, evCh := b.Subscribe()
	defer b.Unsubscribe(subID)

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case ev, ok := <-evCh:
			if !ok {
				return
			}
			data, _ := json.Marshal(ev)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data)
			fl.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// busOf mengambil bus milik agen sesi.
func (s *Server) busOf(ag *agent.Agent) *bus.Bus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentBus[ag.Sess.ID]
}

func (s *Server) answerPermission(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	reqID := chi.URLParam(r, "reqID")
	var body struct {
		Decision string `json:"decision"` // allow | deny
		Always   bool   `json:"always"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "JSON tidak valid"})
		return
	}
	s.mu.Lock()
	ch, ok := s.pending[reqID]
	s.mu.Unlock()
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "permintaan izin tidak ditemukan atau sudah dijawab"})
		return
	}
	dec := permission.Deny
	if body.Decision == "allow" {
		dec = permission.Allow
	}
	select {
	case ch <- agent.PermResponse{Decision: dec, Always: body.Always}:
	default:
	}
	writeJSON(w, 200, map[string]string{"ok": "1"})
	_ = id
}

func (s *Server) abort(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.mu.Lock()
	ag, ok := s.agents[id]
	s.mu.Unlock()
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "sesi tidak ditemukan"})
		return
	}
	ag.Abort()
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (s *Server) setModel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model == "" {
		writeJSON(w, 400, map[string]string{"error": "field model wajib"})
		return
	}
	ag, err := s.getOrCreateAgent(r.Context(), id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	if err := ag.SetModel(body.Model); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"ok": "1"})
}

func (s *Server) setMode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Mode string `json:"mode"` // build | plan | toggle
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "JSON tidak valid"})
		return
	}
	ag, err := s.getOrCreateAgent(r.Context(), id)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	switch body.Mode {
	case "build":
		ag.SetMode(agent.ModeBuild)
	case "plan":
		ag.SetMode(agent.ModePlan)
	default:
		ag.ToggleMode()
	}
	writeJSON(w, 200, map[string]string{"mode": string(ag.Mode)})
}

// listModels menampilkan provider tersedia + katalog model & harga.
func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	type provOut struct {
		ID     string              `json:"id"`
		Name   string              `json:"name"`
		Ready  bool                `json:"ready"`
		Models []catalog.ModelInfo `json:"models"`
	}
	ready := map[string]bool{}
	for _, id := range s.registry.IDs() {
		ready[id] = true
	}
	providers, _ := catalog.Providers()
	out := []provOut{}
	for _, p := range providers {
		out = append(out, provOut{ID: p.ID, Name: p.Name, Ready: ready[p.ID], Models: p.Models})
	}
	writeJSON(w, 200, map[string]any{"providers": out, "default_model": s.cfg.Model()})
}

// writeJSON helper respons JSON.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
