# Konfigurasi

Konfigurasi memakai **JSONC** (JSON dengan komentar) dan digabung berlapis,
dari yang terlemah ke terkuat:

1. **Bawaan** — nilai default aplikasi.
2. **Global** — `~/.config/jenderalcode/jenderal.jsonc` (Windows `%APPDATA%\jenderalcode`, macOS `~/Library/Application Support/jenderalcode`).
3. **Proyek** — `./jenderal.jsonc` (boleh di-commit untuk tim).
4. **Variabel lingkungan** — `JENDERAL_MODEL`, `JENDERAL_SMALL`, `JENDERAL_THEME`, `JENDERAL_LANGUAGE`, `JENDERAL_CURRENCY`.
5. **Flag CLI** — `--model`, `--mode`, dst.

String mendukung interpolasi `{env:NAMA_VAR}`, mis. `"api_key": "{env:ZAI_API_KEY}"`.
Contoh lengkap: [example.jenderal.jsonc](example.jenderal.jsonc).

## Kunci utama

| Kunci | Default | Keterangan |
| --- | --- | --- |
| `model` | — | Model percakapan, format `provider/model` |
| `small_model` | `model` | Judul sesi & ringkasan kompaksi (pilih yang murah) |
| `language` | `id` | `id` \| `en` |
| `theme` | `jenderal` | `jenderal` `dark` `light` `catppuccin` `tokyonight` |
| `currency` | `IDR` | Mata uang tampilan biaya |
| `exchange_rate` | `16500` | Kurs manual 1 USD → currency |
| `step_limit` | `50` | Batas langkah agent loop |
| `compact_percent` | `85` | Kompaksi otomatis (% context window) |
| `bash_timeout_sec` | `120` | Timeout default tool bash (maks 600) |
| `budget.per_session_usd` | `0` | Hentikan agen saat biaya sesi melampaui |
| `budget.per_day_usd` | `0` | (dicatat; peringatan) |
| `permission` | lihat bawah | Aturan izin per tool |
| `provider.<id>` | — | Override/ tambah provider |
| `mcp.<nama>` | — | Server MCP `local`/`remote` |

## Aturan izin

Nilai per tool berupa level (`"edit": "ask"`) atau pola per target
(`"bash": { "git status": "allow", "*": "ask" }`). Pola mendukung `*` dan `?`.
Urutan keputusan:

1. `--yolo` → semua diizinkan (hanya CI/sandbox).
2. Persetujuan "selalu untuk sesi ini" dari dialog.
3. **Akses file di luar direktori proyek selalu `ask`.**
4. Pola yang cocok (pola persis menang, lalu pola terpanjang).
5. Default per tool: read-only `allow`, mutasi `ask`.

## Provider

Provider dari katalog bawaan aktif otomatis saat kredensialnya ada (env
`OPENAI_API_KEY` dll., keychain, file kredensial, atau `provider.<id>.api_key`).
Provider baru yang OpenAI-compatible cukup ditambahkan:

```jsonc
"provider": {
  "penyedia-baru": {
    "type": "openai-compatible",
    "base_url": "https://api.penyedia-baru.com/v1",
    "api_key": "{env:BARU_API_KEY}"
  }
}
```

Adapter `anthropic` dipakai otomatis untuk provider Anthropic dan bisa
dipaksakan dengan `"type": "anthropic"`.

## File & folder

| Lokasi | Fungsi |
| --- | --- |
| `~/.config/jenderalcode/jenderal.jsonc` | Konfigurasi global |
| `./jenderal.jsonc` | Konfigurasi proyek |
| `JENDERAL.md` / `AGENTS.md` | Aturan proyek (dibaca agen tiap sesi) |
| `.jenderal/agents/*.md` | Custom agent |
| `.jenderal/commands/*.md` | Custom slash command |
| `.jenderalignore` | Larangan akses file tambahan |
| `~/.local/share/jenderalcode/jenderal.db` | Database sesi (SQLite WAL) |
| `~/.local/share/jenderalcode/snapshots/` | Snapshot file untuk undo/redo |
| `~/.cache/jenderalcode/models.json` | Katalog model hasil `--refresh` |

`JENDERAL_CONFIG_DIR`, `JENDERAL_DATA_DIR`, `JENDERAL_CACHE_DIR` mengganti lokasi
di atas — berguna untuk test dan sandbox.
