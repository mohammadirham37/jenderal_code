# PRD JenderalCode

Oct 5, 2026 · @Someone

## 1. Ringkasan Produk

JenderalCode adalah AI coding agent open-source yang ditulis dengan Go, berjalan sebagai CLI/TUI di terminal dan sebagai aplikasi desktop, serta bisa memakai provider AI mana pun (OpenAI, Z.ai, Kilo Code, Anthropic, Google, OpenRouter, Ollama, dan endpoint OpenAI-compatible lain).

Visi: "Satu komandan untuk semua model AI" — developer memimpin pasukan agen AI dari satu binary kecil, cepat, dan tanpa terikat vendor.

Nilai utama produk:

- **Ringan dan cepat**: satu binary statis tanpa runtime Node/Python, start di bawah 100 ms, memori idle di bawah 40 MB.
- **Bebas vendor**: ganti provider dan model kapan saja, bahkan di tengah sesi.
- **Satu inti, banyak tampilan**: CLI, TUI, desktop, dan mode server memakai engine agen yang sama.
- **Aman secara default**: setiap aksi berisiko (tulis file, jalankan perintah) butuh izin yang bisa diatur.
- **Terbuka dan bisa diperluas**: lisensi MIT, mendukung MCP (Model Context Protocol), custom agent, dan plugin.

## 2. Latar Belakang, Tujuan, dan Non-Tujuan

Masalahnya: banyak AI coding agent terikat satu vendor, berat (berbasis Electron/Node), atau mahal untuk developer di pasar dengan biaya sensitif seperti Indonesia. Developer ingin bebas memilih model murah (mis. GLM dari Z.ai) untuk tugas ringan dan model premium untuk tugas berat, tanpa ganti alat.

### Masalah yang diselesaikan

- Vendor lock-in: alat yang hanya bisa satu provider memaksa developer membayar harga provider itu.
- Konsumsi resource: aplikasi Electron memakan 300–800 MB RAM, berat di laptop spesifikasi menengah.
- Alur kerja terpecah: chat di browser, kode di editor, perintah di terminal.
- Biaya tidak terlihat: developer tidak tahu berapa token dan rupiah yang dihabiskan per sesi.

### Tujuan (v1.0)

1. Agen yang bisa membaca, menulis, mengedit kode, menjalankan perintah, dan mencari di codebase secara otonom dengan izin pengguna.
2. Minimal 8 provider didukung saat rilis, plus endpoint OpenAI-compatible kustom.
3. Satu binary per OS (Linux, macOS, Windows; amd64 dan arm64) untuk CLI/TUI, dan installer desktop.
4. Pelacakan token dan biaya per pesan, per sesi, dan per provider.
5. Sesi tersimpan lokal dan bisa dilanjutkan, dibagikan, atau di-undo.

### Non-tujuan (v1.0)

- Bukan IDE penuh: tidak ada editor kode bawaan selain diff viewer.
- Tidak melatih atau meng-host model sendiri.
- Tidak ada layanan cloud berbayar milik JenderalCode di v1.0; semua berjalan lokal.
- Tidak ada fitur kolaborasi real-time multi-user.

## 3. Target Pengguna dan Persona

Pengguna utama adalah developer individu yang nyaman di terminal; pengguna sekunder adalah tim kecil dan developer yang lebih suka GUI.

| Persona | Profil | Kebutuhan utama | Mode favorit |
| --- | --- | --- | --- |
| Raka, backend dev | 27 th, Go/Node, kerja di Linux via SSH | Cepat, ringan, jalan di server tanpa GUI | CLI/TUI |
| Sinta, freelancer | 24 th, web dev, laptop RAM 8 GB, budget ketat | Pakai model murah, pantau biaya dalam rupiah | Desktop |
| Bayu, tech lead | 33 th, memimpin tim 6 orang | Konfigurasi agen bersama, aturan proyek, audit aksi | CLI + server |
| Dimas, DevOps | 30 th, otomasi CI/CD | Mode non-interaktif, output JSON, skrip | CLI headless |
| Nadia, mahasiswa | 21 th, belajar pemrograman | Penjelasan kode, model gratis/lokal (Ollama) | Desktop |

### User story utama

- Sebagai developer, saya ingin mengetik `jenderal` di folder proyek dan langsung meminta agen memperbaiki bug, agar tidak perlu pindah ke browser.
- Sebagai pengguna hemat biaya, saya ingin memakai GLM untuk tugas ringan dan model premium untuk refactor besar, agar biaya tetap rendah.
- Sebagai tech lead, saya ingin menyimpan aturan proyek di file `JENDERAL.md`, agar agen mengikuti konvensi tim.
- Sebagai DevOps, saya ingin menjalankan `jenderal run "..." --json` di pipeline CI, agar review kode otomatis.
- Sebagai pengguna desktop, saya ingin melihat diff sebelum perubahan diterapkan, agar saya tetap memegang kendali.

## 4. Lingkup Produk

JenderalCode terdiri dari satu engine inti (`jenderal-core`) dan tiga antarmuka yang berbagi engine tersebut, sehingga fitur baru cukup dibuat sekali.

| Antarmuka | Deskripsi | Teknologi | Target rilis |
| --- | --- | --- | --- |
| CLI non-interaktif | `jenderal run "prompt"` untuk skrip dan CI, output teks atau JSON | Cobra | v0.1 |
| TUI interaktif | Chat penuh di terminal: daftar sesi, diff, dialog izin, pemilih model | Bubble Tea + Lip Gloss + Glamour | v0.2 |
| Server headless | `jenderal serve` membuka HTTP + SSE API lokal untuk klien lain (desktop, ekstensi editor) | net/http + chi | v0.3 |
| Desktop | Aplikasi GUI dengan panel chat, file tree, diff viewer, pengaturan provider | Wails v2 (Go backend + webview native) | v0.5 |

### Kenapa Wails untuk desktop

Wails memakai webview bawaan OS (WebView2 di Windows, WebKit di macOS/Linux), sehingga ukuran installer sekitar 10–15 MB dan RAM jauh di bawah Electron. Backend tetap Go, jadi desktop memanggil `jenderal-core` langsung tanpa proses terpisah. Alternatif yang dipertimbangkan: Fyne (UI kurang fleksibel untuk chat/markdown) dan Tauri (backend Rust, menambah bahasa kedua).

### Di dalam lingkup v1.0

- Agen otonom dengan tool bawaan (baca, tulis, edit, bash, grep, glob, web fetch, todo).
- Multi-provider dan pergantian model per sesi.
- Mode agen: **Build** (boleh mengubah file) dan **Plan** (hanya baca dan merencanakan).
- Sub-agen, MCP client, LSP untuk diagnostik.
- Sesi persisten, undo/redo, kompaksi konteks otomatis, share sesi sebagai file.

### Di luar lingkup v1.0

- Ekstensi VS Code/JetBrains resmi (direncanakan v1.x via server API).
- Marketplace plugin online.
- Sinkronisasi sesi ke cloud.

## 5. Kebutuhan Fungsional

Prioritas memakai skala P0 (wajib v1.0), P1 (sebaiknya v1.0), P2 (setelah v1.0).

### 5.1 Agent loop

| ID | Kebutuhan | Prioritas |
| --- | --- | --- |
| F-AG-01 | Loop agen: kirim prompt → terima stream → eksekusi tool call → kirim hasil → ulang hingga model selesai atau batas langkah tercapai (default 50) | P0 |
| F-AG-02 | Streaming token real-time ke semua antarmuka | P0 |
| F-AG-03 | Eksekusi tool paralel bila model meminta beberapa tool sekaligus dan tool bersifat read-only | P1 |
| F-AG-04 | Mode agen Build dan Plan, bisa ditukar dengan Tab | P0 |
| F-AG-05 | Custom agent dari file Markdown (`.jenderal/agents/*.md`) berisi system prompt, model, dan daftar tool yang diizinkan | P1 |
| F-AG-06 | Sub-agen: agen utama dapat mendelegasikan tugas ke agen lain dengan konteks terpisah | P1 |
| F-AG-07 | Interupsi dengan Esc: hentikan stream dan tool yang berjalan tanpa merusak sesi | P0 |
| F-AG-08 | Kompaksi konteks otomatis saat mendekati 85% batas context window model | P0 |

### 5.2 Tool bawaan

| Tool | Fungsi | Izin default |
| --- | --- | --- |
| `read` | Baca file dengan rentang baris | Izinkan |
| `write` | Buat atau timpa file | Tanya |
| `edit` | Ganti string unik dalam file (str\_replace) dan multi-edit | Tanya |
| `patch` | Terapkan unified diff | Tanya |
| `bash` | Jalankan perintah shell dengan timeout (default 2 menit) | Tanya |
| `glob` | Cari file berdasarkan pola | Izinkan |
| `grep` | Cari isi file (ripgrep jika tersedia, fallback Go) | Izinkan |
| `ls` | Daftar direktori, menghormati `.gitignore` | Izinkan |
| `webfetch` | Ambil halaman web sebagai Markdown | Tanya |
| `todo` | Kelola daftar tugas agen dalam sesi | Izinkan |
| `task` | Jalankan sub-agen | Izinkan |
| `lsp_diagnostics` | Ambil error/warning dari language server setelah edit | Izinkan |

### 5.3 Sistem izin

- Tiga level per tool atau per pola perintah: **izinkan**, **tanya**, **tolak**. Contoh: `bash: {"git status": allow, "rm *": deny, "*": ask}`.
- Dialog izin menampilkan diff atau perintah lengkap, dengan pilihan: sekali, selalu untuk sesi ini, tolak.
- Flag `--yolo` untuk menyetujui semua (dengan peringatan jelas), hanya untuk lingkungan sandbox/CI.
- Akses di luar direktori proyek selalu ditanya.

### 5.4 Sesi dan riwayat

- Sesi disimpan di SQLite lokal (`~/.local/share/jenderalcode/jenderal.db`).
- Daftar, cari, lanjutkan, ganti nama, hapus, dan fork sesi.
- Snapshot file sebelum setiap perubahan; perintah `/undo` dan `/redo` mengembalikan file dan pesan.
- Ekspor sesi ke Markdown atau JSON; impor dari JSON.
- Judul sesi dibuat otomatis oleh model kecil/murah.

### 5.5 Konteks proyek

- File aturan `JENDERAL.md` di root proyek dan `~/.config/jenderalcode/JENDERAL.md` global, dibaca otomatis; juga kompatibel membaca `AGENTS.md`.
- Perintah `/init` menganalisis repo dan membuat `JENDERAL.md` awal.
- Referensi file dengan `@path/ke/file` dan gambar dengan drag-drop atau tempel (untuk model multimodal).

### 5.6 Integrasi

- MCP client: server lokal (stdio) dan remote (HTTP/SSE), termasuk OAuth.
- LSP: auto-deteksi gopls, typescript-language-server, pyright, rust-analyzer, dll.
- Git: info branch dan status di status bar; opsi commit otomatis per langkah (P2).
- Custom command: file `.jenderal/commands/*.md` menjadi slash command.

### 5.7 Biaya dan penggunaan

- Hitung token input, output, dan cache per pesan; estimasi biaya dari tabel harga model.
- Tampilkan biaya dalam USD dan konversi ke mata uang pilihan (default IDR, kurs diatur manual atau diambil harian).
- Batas biaya per sesi atau per hari dengan peringatan dan penghentian otomatis (P1).

## 6. Dukungan Multi-Provider AI

Semua provider diakses lewat satu interface Go, dan sebagian besar cukup ditangani oleh dua adapter: OpenAI-compatible dan Anthropic Messages. Provider baru yang sudah OpenAI-compatible bisa ditambahkan hanya lewat konfigurasi, tanpa menulis kode.

### 6.1 Daftar provider target

| Provider | Adapter | Catatan | Prioritas |
| --- | --- | --- | --- |
| OpenAI | OpenAI (Chat Completions + Responses API) | Model GPT dan seri reasoning | P0 |
| Anthropic | Anthropic Messages | Prompt caching, extended thinking | P0 |
| Z.ai (GLM) | OpenAI-compatible; opsi endpoint Anthropic-compatible | Termasuk paket coding Z.ai; base URL perlu diverifikasi saat implementasi | P0 |
| Kilo Code (Kilo Gateway) | OpenAI-compatible | Akses banyak model via satu API key; detail endpoint perlu diverifikasi | P0 |
| OpenRouter | OpenAI-compatible | Ratusan model, harga diambil dari API mereka | P0 |
| Google Gemini | Gemini API (adapter khusus) | Context window besar, multimodal | P1 |
| DeepSeek, Moonshot (Kimi), Qwen, Groq, Mistral, xAI | OpenAI-compatible | Cukup lewat konfigurasi | P1 |
| Ollama, LM Studio, llama.cpp | OpenAI-compatible lokal | Gratis, offline, tanpa API key | P0 |
| Azure OpenAI, AWS Bedrock, Vertex AI | Adapter khusus | Kebutuhan enterprise | P2 |
| GitHub Copilot | OAuth device flow | Bergantung ketentuan layanan | P2 |

### 6.2 Interface provider (Go)

```go
type Provider interface {
    ID() string
    Models(ctx context.Context) ([]ModelInfo, error)
    Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
}

type ModelInfo struct {
    ID              string
    ContextWindow   int
    MaxOutput       int
    SupportsTools   bool
    SupportsImages  bool
    SupportsReasoning bool
    PriceInputPerM  float64 // USD per 1 juta token
    PriceOutputPerM float64
    PriceCachePerM  float64
}

type StreamEvent struct {
    Type     EventType // TextDelta, ReasoningDelta, ToolCall, Usage, Done, Error
    Text     string
    ToolCall *ToolCall
    Usage    *Usage
    Err      error
}
```

### 6.3 Kebutuhan provider

- Normalisasi format tool calling antar provider ke satu skema internal (JSON Schema).
- Fallback untuk model tanpa native tool calling: tool dijelaskan di prompt dan dipanggil via blok XML/JSON yang diparse.
- Retry dengan exponential backoff untuk error 429 dan 5xx (maks 5 kali), menghormati header `retry-after`.
- Katalog model dan harga: file bawaan yang diperbarui per rilis, plus `jenderal models --refresh` untuk mengambil versi terbaru.
- Login: `jenderal auth login <provider>` menyimpan API key di keychain OS (Keychain, Credential Manager, Secret Service); fallback file terenkripsi.
- Variabel lingkungan standar dibaca otomatis (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `ZAI_API_KEY`, dll.).
- Model terpisah untuk tugas kecil (judul sesi, ringkasan kompaksi) agar hemat biaya.
- Ganti model di tengah sesi dengan `/model` tanpa kehilangan riwayat.

## 7. Arsitektur Teknis

Semua logika ada di `jenderal-core`; CLI, TUI, desktop, dan server hanya lapisan tampilan yang berlangganan ke event bus core. Bahasa: Go 1.23+, tanpa CGO agar cross-compile ke semua OS cukup dengan satu perintah.

&#91;embedded content: arsitektur JenderalCode · 4 antarmuka, 1 core, 3 lapisan\]

Antarmuka mengirim perintah ke core; core memanggil provider, tool, dan integrasi, lalu menerbitkan event (TextDelta, ToolCall, PermissionRequest, Usage) yang dirender setiap antarmuka.

### 7.1 Prinsip desain

- **Event-driven**: core menerbitkan event lewat pub/sub internal (channel Go); antarmuka hanya subscriber.
- **Konkuren dengan aman**: setiap tool berjalan di goroutine dengan `context.Context` sehingga Esc membatalkan semuanya.
- **Pure Go**: SQLite via `modernc.org/sqlite`, tanpa CGO.
- **Antarmuka kecil**: `Provider`, `Tool`, `Store`, `Permission` adalah interface Go agar mudah di-mock dan diperluas.

### 7.2 Struktur repositori

```text
jenderalcode/
├── cmd/
│   ├── jenderal/            # entry CLI + TUI + serve
│   └── jenderal-desktop/    # entry Wails
├── internal/
│   ├── agent/       # loop, mode, sub-agen, kompaksi
│   ├── provider/    # interface + adapter openai, anthropic, gemini
│   ├── tool/        # read, write, edit, patch, bash, grep, ...
│   ├── permission/  # aturan allow/ask/deny
│   ├── session/     # SQLite, snapshot, undo/redo
│   ├── config/      # loader JSONC berlapis
│   ├── bus/         # event pub/sub
│   ├── mcp/         # MCP client
│   ├── lsp/         # LSP client
│   ├── server/      # HTTP + SSE API
│   └── tui/         # komponen Bubble Tea
├── desktop/frontend/  # UI web untuk Wails
├── pkg/sdk/           # SDK Go untuk klien server
├── catalog/models.json
└── docs/
```

### 7.3 Library utama

| Kebutuhan | Library | Alasan |
| --- | --- | --- |
| CLI | `spf13/cobra` | Standar de facto, autocomplete shell |
| TUI | `charmbracelet/bubbletea`, `bubbles`, `lipgloss`, `glamour` | Ekosistem TUI Go paling matang |
| Desktop | `wailsapp/wails` v2 | Webview native, backend Go langsung |
| Database | `modernc.org/sqlite` | SQLite pure Go, tanpa CGO |
| HTTP router | `go-chi/chi` | Ringan, kompatibel net/http |
| Provider SDK | `openai/openai-go`, `anthropics/anthropic-sdk-go` | SDK resmi; adapter lain via HTTP client sendiri |
| MCP | SDK Go resmi MCP atau `mark3labs/mcp-go` | Dukungan stdio dan HTTP |
| Syntax highlight | `alecthomas/chroma` | Ratusan bahasa |
| Diff | `sergi/go-diff` / `hexops/gotextdiff` | Unified diff dan patch |
| JSONC | `tailscale/hujson` | Parsing JSON dengan komentar |
| Keychain | `zalando/go-keyring` | Lintas OS |
| Logging | `log/slog` (stdlib) | Tanpa dependensi |
| Rilis | GoReleaser + GitHub Actions | Binary, Homebrew, Scoop, deb/rpm, AUR |

### 7.4 API server (ringkas)

| Endpoint | Fungsi |
| --- | --- |
| `POST /session` | Buat sesi |
| `GET /session` | Daftar sesi |
| `POST /session/{id}/message` | Kirim pesan ke agen |
| `GET /session/{id}/events` | Stream event (SSE) |
| `POST /session/{id}/permission/{req}` | Jawab permintaan izin |
| `POST /session/{id}/abort` | Hentikan agen |
| `GET /models` | Daftar model dan harga |

### 7.5 Distribusi

- Install: `curl -fsSL https://jenderalcode.dev/install | sh`, `brew install jenderalcode`, `scoop install jenderalcode`, `go install`, paket deb/rpm/AUR.
- Auto-update via `jenderal upgrade` dengan verifikasi checksum dan tanda tangan.

## 8. Kebutuhan Non-Fungsional

Target performa diukur di laptop referensi (4 core, RAM 8 GB, SSD) dan menjadi syarat lulus setiap rilis.

### 8.1 Performa

| Metrik | Target | Cara ukur |
| --- | --- | --- |
| Waktu start CLI/TUI | < 100 ms sampai prompt siap | `hyperfine` di CI |
| Waktu start desktop | < 1,5 detik sampai jendela interaktif | Benchmark manual per rilis |
| Memori idle TUI | < 40 MB RSS | Pengukuran otomatis di CI |
| Memori desktop | < 150 MB dengan 1 sesi aktif | Pengukuran manual |
| Ukuran binary CLI | < 30 MB (setelah strip) | Pemeriksaan artefak build |
| Ukuran installer desktop | < 20 MB | Pemeriksaan artefak build |
| Latensi render token | < 16 ms per frame (60 fps) | Profiling TUI |
| Grep di repo 100 ribu file | < 2 detik | Benchmark repo besar |

### 8.2 Keamanan

- API key tidak pernah ditulis ke log, sesi, atau file ekspor; disimpan di keychain OS.
- Server lokal hanya bind ke `127.0.0.1` dengan token acak per sesi; bind ke jaringan wajib flag eksplisit dan password.
- Sandbox opsional untuk `bash`: Landlock/bubblewrap di Linux, `sandbox-exec` di macOS, container Docker sebagai opsi lintas OS (P1).
- Proteksi path traversal dan symlink keluar proyek.
- Deteksi prompt injection dasar: konten dari web fetch dan file ditandai sebagai data tidak tepercaya di system prompt.
- Rilis ditandatangani (cosign + notarisasi macOS + signing Windows) dan disertai SBOM.

### 8.3 Privasi

- Tidak ada telemetri secara default; telemetri anonim hanya opt-in.
- Semua data sesi lokal; tidak ada server JenderalCode yang menerima kode pengguna.
- File `.jenderalignore` untuk mengecualikan file sensitif (mis. `.env`) dari akses agen; `.env*` dikecualikan secara default.

### 8.4 Keandalan dan kualitas

- Crash tidak boleh merusak database sesi (SQLite WAL mode, transaksi per pesan).
- Cakupan unit test minimal 70% untuk `core`, dengan mock provider untuk test agen yang deterministik.
- Evaluasi agen dengan benchmark internal (kumpulan tugas coding) per rilis untuk mendeteksi regresi.

### 8.5 Kompatibilitas

- OS: Linux (glibc dan musl), macOS 12+, Windows 10+; arsitektur amd64 dan arm64.
- Terminal: mendukung true color dan fallback 256 warna; berjalan di Windows Terminal, iTerm2, Alacritty, Kitty, WezTerm, tmux.
- Lokalisasi UI: Bahasa Indonesia dan Inggris sejak v1.0.

## 9. Pengalaman Pengguna (UX)

Perintah utama adalah `jenderal` (alias pendek `jc`); menjalankannya tanpa argumen langsung membuka TUI di direktori saat ini.

### 9.1 Perintah CLI

| Perintah | Fungsi |
| --- | --- |
| `jenderal` | Buka TUI interaktif di folder saat ini |
| `jenderal run "<prompt>"` | Jalankan satu tugas non-interaktif; `--json`, `--model`, `--agent`, `--yolo` |
| `jenderal serve [--port 4096]` | Jalankan server HTTP + SSE lokal |
| `jenderal desktop` | Buka aplikasi desktop di folder saat ini |
| `jenderal auth login\|logout\|list` | Kelola kredensial provider |
| `jenderal models [--refresh]` | Daftar model yang tersedia beserta harga |
| `jenderal sessions list\|export\|import\|rm` | Kelola sesi |
| `jenderal mcp add\|list\|rm` | Kelola server MCP |
| `jenderal init` | Buat `JENDERAL.md` dari analisis repo |
| `jenderal stats` | Ringkasan penggunaan token dan biaya |
| `jenderal upgrade` | Perbarui ke versi terbaru |

### 9.2 Slash command di TUI dan desktop

`/model`, `/agent`, `/new`, `/sessions`, `/undo`, `/redo`, `/compact`, `/cost`, `/init`, `/share`, `/export`, `/theme`, `/help`, plus custom command dari `.jenderal/commands/`.

### 9.3 Keybinding TUI

| Tombol | Aksi |
| --- | --- |
| Enter | Kirim pesan |
| Shift+Enter / Ctrl+J | Baris baru |
| Tab | Tukar mode Build ↔ Plan |
| Esc | Hentikan agen yang berjalan |
| Ctrl+K | Command palette |
| Ctrl+M | Pilih model |
| Ctrl+S | Daftar sesi |
| Ctrl+Z / Ctrl+Y | Undo / redo |
| Ctrl+E | Buka input di `$EDITOR` |
| Ctrl+C dua kali | Keluar |

Semua keybinding bisa diubah di konfigurasi, dengan preset Vim opsional.

### 9.4 Tata letak TUI

- Area chat utama dengan Markdown, syntax highlight, dan diff berwarna.
- Status bar bawah: model aktif, mode agen, token terpakai/batas konteks, biaya sesi (Rp), branch Git.
- Dialog modal untuk izin, pemilih model, dan sesi.
- Tema bawaan: `jenderal` (hijau army dan emas), `dark`, `light`, `catppuccin`, `tokyonight`; tema kustom via file JSON.

### 9.5 Layar desktop

1. **Onboarding**: pilih bahasa, tambah provider pertama (dengan tombol uji koneksi), pilih folder proyek.
2. **Workspace**: sidebar kiri (proyek dan sesi), panel tengah (chat), panel kanan (file tree, diff, todo agen).
3. **Diff viewer**: tampilan side-by-side, terima atau tolak per hunk.
4. **Pengaturan**: provider dan API key, model default, izin tool, MCP, tema, bahasa, batas biaya.
5. **Dashboard penggunaan**: grafik token dan biaya per hari, per provider, per proyek.

Desktop mendukung banyak jendela/proyek sekaligus, notifikasi OS saat agen selesai, dan tray icon.

## 10. Konfigurasi dan Format File

Konfigurasi memakai JSONC (JSON dengan komentar) dan digabung berlapis: bawaan → global (`~/.config/jenderalcode/jenderal.jsonc`) → proyek (`./jenderal.jsonc`) → variabel lingkungan → flag CLI. Skema JSON dipublikasikan agar editor memberi autocomplete.

```jsonc
{
  "$schema": "https://jenderalcode.dev/schema/config.json",
  "language": "id",
  "theme": "jenderal",
  "model": "zai/glm-4.6",          // model default: provider/model
  "small_model": "openai/gpt-4o-mini", // judul sesi & ringkasan
  "currency": "IDR",

  "provider": {
    "zai": {
      "api_key": "{env:ZAI_API_KEY}",
      "base_url": "<endpoint Z.ai, diverifikasi saat implementasi>"
    },
    "kilo": {
      "type": "openai-compatible",
      "api_key": "{env:KILO_API_KEY}",
      "base_url": "<endpoint Kilo Gateway>"
    },
    "lokal": {
      "type": "openai-compatible",
      "base_url": "http://localhost:11434/v1",
      "models": { "qwen2.5-coder:14b": { "context_window": 32768 } }
    }
  },

  "permission": {
    "edit": "ask",
    "bash": { "git status": "allow", "go test *": "allow", "rm -rf *": "deny", "*": "ask" },
    "webfetch": "ask"
  },

  "mcp": {
    "github": { "type": "remote", "url": "https://example.com/mcp" },
    "db": { "type": "local", "command": ["npx", "-y", "@example/mcp-postgres"] }
  },

  "budget": { "per_session_usd": 2.0, "per_day_usd": 10.0 },
  "keybinds": { "model_picker": "ctrl+m" }
}
```

### Lokasi file

| File/folder | Fungsi |
| --- | --- |
| `~/.config/jenderalcode/jenderal.jsonc` | Konfigurasi global |
| `./jenderal.jsonc` | Konfigurasi proyek (bisa di-commit) |
| `JENDERAL.md` / `AGENTS.md` | Aturan dan konteks proyek untuk agen |
| `.jenderal/agents/*.md` | Custom agent (frontmatter YAML + prompt) |
| `.jenderal/commands/*.md` | Custom slash command |
| `.jenderalignore` | File yang tidak boleh diakses agen |
| `~/.local/share/jenderalcode/jenderal.db` | Database sesi SQLite |
| `~/.local/share/jenderalcode/snapshots/` | Snapshot file untuk undo |
| `~/.cache/jenderalcode/models.json` | Katalog model dan harga |

Di Windows, lokasi mengikuti `%APPDATA%` dan `%LOCALAPPDATA%`; di macOS mengikuti `~/Library/Application Support`.

### Contoh custom agent

```markdown
---
name: reviewer
description: Review kode tanpa mengubah file
model: anthropic/claude-sonnet-4-5
tools: [read, grep, glob, lsp_diagnostics]
---
Kamu adalah senior reviewer. Fokus pada bug, keamanan, dan konvensi tim.
```

## 12. Metrik Keberhasilan, Risiko, dan Pertanyaan Terbuka

Keberhasilan v1.0 diukur dari adopsi, kualitas agen, dan efisiensi resource, dalam 6 bulan setelah rilis.

### 12.1 Metrik keberhasilan

| Metrik | Target 6 bulan pasca v1.0 |
| --- | --- |
| Bintang GitHub | 5.000 |
| Pengguna aktif mingguan (dari telemetri opt-in + unduhan) | 3.000 |
| Tingkat keberhasilan tugas di benchmark internal | ≥ 60% dengan model kelas menengah |
| Crash rate | < 0,5% sesi |
| Provider yang didukung | ≥ 15 |
| Kontributor eksternal | ≥ 30 |
| Rasio memori vs aplikasi Electron sejenis | ≤ 25% |

### 12.2 Risiko dan mitigasi

| Risiko | Dampak | Mitigasi |
| --- | --- | --- |
| Perbedaan format tool calling antar provider | Agen gagal di provider tertentu | Lapisan normalisasi + test kontrak per adapter dengan rekaman respons |
| Model murah lemah dalam tool use | Kualitas rendah, pengguna kecewa | Prompt khusus per keluarga model, fallback edit berbasis whole-file, label "direkomendasikan" di pemilih model |
| Agen menjalankan perintah merusak | Kehilangan data | Izin default "tanya", daftar deny bawaan, snapshot sebelum edit, sandbox opsional |
| Perubahan API/harga provider | Error atau biaya salah | Katalog model bisa di-refresh tanpa rilis baru |
| Webview Wails berbeda antar OS | Bug tampilan desktop | Test E2E di 3 OS di CI, komponen UI sederhana |
| Persaingan ketat (opencode, Claude Code, Aider, Cline, Kilo Code) | Sulit menarik pengguna | Fokus pada binary ringan, UI Bahasa Indonesia, biaya dalam rupiah, komunitas lokal |
| Ketentuan layanan provider (mis. login berbasis langganan) | Fitur harus dicabut | Utamakan API key resmi; integrasi OAuth hanya bila diizinkan |

### 12.3 Pertanyaan terbuka

- [ ] Lisensi final: MIT atau Apache-2.0 (Apache memberi perlindungan paten)?
- [ ] Frontend desktop memakai Svelte, React, atau vanilla + htmx?
- [ ] Apakah TUI dan desktop berbagi proses lewat server lokal, atau desktop menanam core langsung?
- [ ] Endpoint resmi Z.ai dan Kilo Gateway, serta apakah keduanya mendukung prompt caching, perlu diverifikasi.
- [ ] Apakah perlu mode "hemat" yang otomatis memilih model termurah per jenis tugas (router model)?
- [ ] Domain dan nama paket: `jenderalcode.dev`, `github.com/mohammadirham37/jenderal_code`?
- [ ] Model monetisasi jangka panjang (donasi, sponsor, versi tim berbayar)?
