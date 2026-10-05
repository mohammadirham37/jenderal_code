# Arsitektur

## Prinsip desain (dari PRD)

1. **Event-driven** — semua logika ada di core; antarmuka (CLI, TUI, server)
   hanya pelanggan event bus (`internal/bus`). Event: `text_delta`,
   `tool_call`, `permission_request`, `usage`, `done`, dll.
2. **Konkuren aman** — setiap tool berjalan dengan `context.Context`; `Esc`
   membatalkan stream dan tool sekaligus. Tool read-only boleh paralel.
3. **Pure Go** — SQLite via `modernc.org/sqlite` (tanpa CGO), sehingga
   cross-compile Linux/macOS/Windows amd64+arm64 cukup satu perintah.
4. **Interface kecil** — `Provider`, `Tool`, `Store`, izin (`permission.Rules`)
   adalah interface/struct kecil yang mudah di-mock (lihat provider mock).

## Lapisan

```
antarmuka (tui, cli, server)          <- hanya render + input
    │  perintah (Run, SetModel, Abort)
    ▼
internal/agent                        <- orkestrasi
    │  agent loop: prompt → stream → tool → hasil → ulang
    │  mode Build/Plan · kompaksi otomatis · sub-agen (task)
    │  custom agent (.jenderal/agents) · title otomatis · budget
    ├── internal/provider             <- abstraksi AI
    │     Provider interface
    │     ├─ adapter OpenAI-compatible (OpenAI, Z.ai, Kilo, OpenRouter,
    │     │   Gemini, DeepSeek, Groq, Ollama, …)
    │     ├─ adapter Anthropic Messages (prompt caching, thinking)
    │     └─ retry 429/5xx (backoff, retry-after) + mock
    ├── internal/tool                 <- kemampuan agen
    │     read write edit patch bash glob grep ls webfetch todo
    │     lsp_diagnostics (+ task dari agent, + MCP dari internal/mcp)
    │     .jenderalignore / .gitignore / larangan .env bawaan
    ├── internal/permission           <- allow/ask/deny + pola perintah
    ├── internal/session              <- SQLite (WAL): pesan, usage,
    │     snapshot file, undo/redo, ekspor/impor
    ├── internal/mcp                  <- client MCP stdio + remote HTTP
    ├── internal/config               <- JSONC berlapis + {env:VAR}
    ├── catalog                       <- model & harga (di-embed + cache refresh)
    └── internal/bus                  <- pub/sub event
```

## Agent loop

```
Run(input)
 ├─ simpan pesan user (DB)
 ├─ ekspansi custom command & @referensi file
 └─ loop (maks step_limit):
      ├─ kompaksi bila token ≥ compact_percent × context window
      ├─ cek budget sesi
      ├─ provider.Stream(system, tools, riwayat) → emit text/reasoning delta
      ├─ simpan pesan assistant + usage + biaya
      ├─ tanpa tool call? → selesai (publish done, buat judul)
      └─ eksekusi tool calls:
           ├─ paralel bila semua read-only
           ├─ per call: permission.Check → ask? → resolver UI
           ├─ snapshot isi lama → Exec → catat perubahan (undo/redo)
           └─ simpan hasil tool ke DB → ulangi loop
```

## Undo / redo

Setiap mutasi file dicatat di tabel `changes` berisi path, snapshot **isi
sebelum** dan **isi sesudah** (file blob di `snapshots/`), serta id pesan
pemicu. `/undo` memulihkan isi sebelum + menonaktifkan pesan sejak pemicu;
`/redo` memulihkan isi sesudah + mengaktifkan kembali pesan. Mengirim pesan
baru setelah undo membuang riwayat redo. Isi pesan tidak pernah dihapus
(non-aktif saja) sehingga ekspor tetap lengkap.

## Keputusan penyederhanaan (v0.1)

- **Tool call streaming**: argumen diakumulasi per adapter dan diterbitkan
  sebagai satu event `ToolCall` lengkap — cukup untuk semua UI saat ini.
- **Plan mode**: tool mutator tidak ditawarkan ke model (bukan sekadar
  ditolak), memanggil tetap dijawab pesan error agar model bisa menyesuaikan.
- **MCP remote**: JSON POST request-response (subset streamable HTTP); server
  remote yang wajib SSE penuh belum didukung.
- **`lsp_diagnostics`**: tool terdaftar tetapi mengembalikan keterangan
  "belum aktif" — integrasi LSP penuh dijadwalkan v0.4.
- **Desktop**: tidak disertakan; antarmuka GUI memakai `internal/server` +
  `pkg/sdk` (Wails menyusul).
