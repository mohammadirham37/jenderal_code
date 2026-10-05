# JenderalCode

**Satu komandan untuk semua model AI.**

JenderalCode adalah AI coding agent open-source yang ditulis dengan Go — ringan,
cepat, dan bebas vendor. Jalankan di terminal (CLI/TUI), panggil dari skrip/CI,
atau pakai API lokalnya untuk klien GUI. Ganti provider kapan saja: OpenAI,
Anthropic, Z.ai (GLM), Kilo, OpenRouter, Gemini, DeepSeek, Ollama lokal, dan
endpoint OpenAI-compatible lain — semuanya lewat satu binary tanpa runtime Node/Python.

> Sesuai PRD v1.0. Implementasi ini mencakup **core engine + CLI + TUI + server API**
> (milestone v0.1–v0.3). Aplikasi desktop Wails menyusul di v0.5 — fondasinya
> (server API + SDK Go) sudah tersedia.

---

## Fitur utama

- **Agent otonom** — loop: prompt → stream → tool call → hasil → ulang, dengan
  batas langkah, interupsi `Esc`, dan kompaksi konteks otomatis saat mendekati
  85% context window.
- **Multi-provider** — 15 provider bawaan, dua adapter (OpenAI-compatible &
  Anthropic Messages), retry 429/5xx dengan backoff, katalog model + harga
  yang bisa di-refresh, biaya dalam USD & rupiah.
- **Mode Build / Plan** — `Tab` untuk tukar. Mode Plan hanya membaca dan
  merencanakan; tool mutator tidak ditawarkan ke model.
- **11 tool bawaan** — `read`, `write`, `edit`, `patch` (unified diff), `bash`
  (dengan timeout), `glob`, `grep` (ripgrep bila ada, fallback Go), `ls`,
  `webfetch`, `todo`, `lsp_diagnostics`, plus `task` (sub-agen).
- **Sistem izin 3 level** — `allow` / `ask` / `deny` per tool atau per pola
  perintah (`"rm *": "deny"`). Akses file di luar proyek selalu ditanya.
  Flag `--yolo` untuk CI/sandbox.
- **Sesi persisten** — SQLite (pure Go, WAL) di
  `~/.local/share/jenderalcode/jenderal.db`: lanjutkan, cari, ganti nama,
  hapus, fork via impor, ekspor Markdown/JSON, **undo/redo** file + pesan.
- **Konteks proyek** — `JENDERAL.md` (atau `AGENTS.md`) dibaca otomatis;
  `jenderalcode init` membuatnya dari analisis repo; referensi `@path/ke/file`.
- **Custom agent & command** — `.jenderal/agents/*.md` (system prompt + model +
  daftar tool) dan `.jenderal/commands/*.md` (slash command kustom).
- **MCP client** — server lokal (stdio) dan remote (HTTP); tool MCP otomatis
  jadi tool agen (`mcp__server__tool`).
- **Server API lokal** — `jenderalcode serve` membuka HTTP + SSE di `127.0.0.1`
  dengan token acak; SDK Go tersedia di `pkg/sdk`.

## Instalasi

```sh
# dari sumber (butuh Go 1.23+)
go install github.com/mohammadirham37/jenderal_code/cmd/jenderalcode@latest
go install github.com/mohammadirham37/jenderal_code/cmd/jc@latest          # alias pendek
```

Atau build manual:

```sh
git clone https://github.com/mohammadirham37/jenderal_code && cd jenderal_code
go build -o jenderalcode ./cmd/jenderalcode
```

## Mulai cepat

```sh
cd proyek-anda

jenderalcode                  # buka TUI interaktif
```

1. **Isi kredensial** (pilih salah satu):
   ```sh
   jenderalcode auth login zai     # atau openai, anthropic, openrouter, kilo, …
   export OPENAI_API_KEY=...   # variabel lingkungan juga dibaca
   ```
   Ollama/LM Studio lokal tidak butuh API key.
2. **Atur model default** di `jenderal.jsonc` (root proyek) atau global
   `~/.config/jenderalcode/jenderal.jsonc`:
   ```jsonc
   {
     "model": "zai/glm-4.6",          // provider/model
     "small_model": "openai/gpt-4o-mini", // judul sesi & ringkasan (hemat biaya)
     "currency": "IDR"
   }
   ```
   Bisa juga dipilih di dalam TUI dengan `Ctrl+M` atau `/model`.
3. **Ajukan pertanyaan** di TUI, atau non-interaktif:
   ```sh
   jenderalcode run "perbaiki test yang gagal di pkg/auth" --yolo   # di CI
   jenderalcode run "review diff terakhir" --mode plan              # hanya baca
   jenderalcode run "..." --json                                    # output JSONL
   ```

### Coba tanpa API key

```sh
JENDERAL_MOCK=1 jenderalcode          # TUI dengan provider mock offline
JENDERAL_MOCK=1 jenderalcodecode run "buatkan file demo" --yolo
```

## Perintah CLI

| Perintah | Fungsi |
| --- | --- |
| `jenderalcode` | TUI interaktif di folder saat ini (`--continue`, `--session`) |
| `jenderalcode run "<prompt>"` | Tugas non-interaktif; flag `--json --model --agent --yolo --mode plan` |
| `jenderalcode serve --port 4096` | API HTTP+SSE lokal (127.0.0.1, token acak) |
| `jenderalcode auth login\|logout\|list` | Kredensial provider (keychain OS, fallback file aman) |
| `jenderalcode models [--refresh]` | Daftar model + harga; `--refresh` ambil terbaru |
| `jenderalcode sessions list\|export\|import\|rm\|rename` | Kelola sesi |
| `jenderalcode mcp add\|list\|rm` | Kelola server MCP |
| `jenderalcode init` | Buat `JENDERAL.md` dari analisis repo |
| `jenderalcode stats` | Ringkasan token & biaya per hari/model |
| `jenderalcode upgrade` | Periksa versi terbaru |
| `jenderalcode update [--check] [--force]` | Update binary jika commit remote berbeda; tarik source lalu build ulang |
| `jenderalcode version` | Info versi |

## TUI

| Tombol | Aksi |
| --- | --- |
| `Enter` | Kirim pesan |
| `Ctrl+J` | Baris baru |
| `Tab` | Tukar mode Build ↔ Plan |
| `Esc` | Hentikan agen yang berjalan |
| `Ctrl+M` | Pilih model |
| `Ctrl+S` | Daftar sesi |
| `Ctrl+K` | Command palette |
| `Ctrl+Z` / `Ctrl+Y` | Undo / redo |
| `Ctrl+E` | Tulis input di `$EDITOR` |
| `Ctrl+C` 2x | Keluar |

Slash command: `/model` `/agent` `/new` `/sessions` `/undo` `/redo` `/compact`
`/cost` `/init` `/share` `/export` `/theme` `/help` — ditambah command kustom
dari `.jenderal/commands/`.

Tema bawaan: `jenderal` (hijau army & emas), `dark`, `light`, `catppuccin`,
`tokyonight`. Antarmuka tersedia dalam Bahasa Indonesia dan Inggris
(`"language": "id" | "en"`).

## Konfigurasi

Konfigurasi JSONC digabung berlapis: bawaan → global
(`~/.config/jenderalcode/jenderal.jsonc`) → proyek (`./jenderal.jsonc`) →
variabel lingkungan (`JENDERAL_MODEL`, dst.) → flag CLI. Nilai string mendukung
`{env:VAR}`. Contoh lengkap ada di [docs/example.jenderal.jsonc](docs/example.jenderal.jsonc);
rincian setiap kunci ada di [docs/configuration.md](docs/configuration.md).

```jsonc
{
  "model": "zai/glm-4.6",
  "currency": "IDR",
  "provider": {
    "zai": { "base_url": "https://api.z.ai/api/paas/v4", "api_key": "{env:ZAI_API_KEY}" }
  },
  "permission": {
    "bash": { "git status": "allow", "go test *": "allow", "rm -rf *": "deny", "*": "ask" }
  },
  "budget": { "per_session_usd": 2.0, "per_day_usd": 10.0 }
}
```

## Keamanan

- API key disimpan di keychain OS (Keychain / Credential Manager / Secret
  Service); fallback file `credentials.json` dengan izin `0600`.
- Server hanya bind `127.0.0.1` dan menuntut token acak per start.
- `.env*`, kredensial, dan file di `.jenderalignore` **tidak bisa** dibaca/ditulis agen.
- Izin default `ask` untuk semua aksi mutasi; snapshot sebelum setiap perubahan.
- Tanpa telemetri.

## Server API & SDK

```sh
jenderalcode serve --port 4096
# endpoint: POST /session · GET /session · POST /session/{id}/message
#           GET /session/{id}/events (SSE) · POST /session/{id}/permission/{id}
#           POST /session/{id}/abort · GET /models · GET /health
```

Klien Go tersedia di [`pkg/sdk`](pkg/sdk/sdk.go) — contoh pemakaian di
[docs/server-api.md](docs/server-api.md).

## Arsitektur

```
┌─────────┐  ┌─────────┐  ┌──────────────┐
│   TUI   │  │ CLI run │  │ klien server │──┐
└────┬────┘  └────┬────┘  └──────────────┘  │ HTTP+SSE
     │            │                  ┌──────┴──────┐
     ▼            ▼                  │   server    │
┌─────────────────────────────────────────────┐
│                jenderal-core                │
│  agent loop · permission · session (SQLite) │
│  provider adapters · tools · MCP · event bus│
└─────────────────────────────────────────────┘
```

Satu engine, semua antarmuka hanya pelanggan event bus. Rincian:
[docs/architecture.md](docs/architecture.md).

## Struktur repositori

```
├── cmd/jenderalcode/        # entry CLI + TUI + serve
├── cmd/jc/              # alias pendek
├── internal/
│   ├── agent/           # loop, mode, kompaksi, sub-agen, custom agent
│   ├── provider/        # interface + adapter openai & anthropic + mock
│   ├── tool/            # read/write/edit/patch/bash/grep/… + ignore
│   ├── permission/      # allow/ask/deny + pola
│   ├── session/         # SQLite, snapshot, undo/redo, ekspor
│   ├── config/          # JSONC berlapis
│   ├── bus/             # event pub/sub
│   ├── mcp/             # client MCP stdio + remote
│   ├── server/          # HTTP + SSE
│   └── tui/             # Bubble Tea
├── pkg/sdk/             # SDK Go untuk klien server
├── catalog/models.json  # katalog model & harga (di-embed)
└── docs/
```

## Pengembangan

```sh
go test ./...      # unit test core
go vet ./...
go build -o jenderalcode ./cmd/jenderalcode
```

## Roadmap

- **v0.4** — integrasi LSP penuh (`lsp_diagnostics` nyata), sandbox bash opsional.
- **v0.5** — aplikasi desktop Wails (webview native) di atas server API + SDK.
- **v1.x** — ekstensi editor resmi, marketplace plugin.

## Lisensi

MIT — lihat [LICENSE](LICENSE).
