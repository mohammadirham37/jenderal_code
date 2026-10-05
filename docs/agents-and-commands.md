# Custom Agent & Custom Command

## Custom agent (`.jenderal/agents/*.md`)

Custom agent adalah agen spesialis dengan konteks terpisah. Setiap file
Markdown berisi frontmatter YAML sederhana + system prompt.

`.jenderal/agents/reviewer.md`:

```markdown
---
name: reviewer
description: Review kode tanpa mengubah file
model: anthropic/claude-sonnet-4-5
tools: [read, grep, glob, lsp_diagnostics]
---
Kamu senior reviewer. Fokus pada bug, keamanan, dan konvensi tim.
Laporkan temuan diurutkan dari yang paling berisiko.
```

- `name` — dipakai di `/agent`, `--agent`, dan tool `task` (default: nama file).
- `model` — opsional; kalau kosong memakai model sesi.
- `tools` — opsional; daftar tool yang boleh dipakai (kosong = semua).
- Isi file menjadi system prompt tambahan.

Pemakaian:

- TUI: `/agent` → pilih dari daftar.
- CLI: `jenderalcode run "review modul auth" --agent reviewer`.
- Sub-agen: agen utama memanggil tool `task` dengan `"agent": "reviewer"`.
  Sub-agen punya konteks terpisah, hasil akhirnya dikembalikan ke agen utama,
  dan sub-agen tidak boleh memanggil `task` lagi (tidak bersarang).

## Custom command (`.jenderal/commands/*.md`)

Nama file menjadi slash command. `$ARGUMENTS` diganti argumen setelah nama.

`.jenderal/commands/refactor.md`:

```markdown
---
description: Refactor file dengan rencana dulu
---
Refactor file berikut dengan langkah kecil dan aman: $ARGUMENTS.
Mulai dari membaca file, buat rencana di todo, lalu eksekusi per langkah.
Jalankan test setelah setiap perubahan.
```

Di TUI ketik `/refactor internal/auth/handler.go` — template diperluas menjadi
prompt lengkap sebelum dikirim ke agen. Custom command juga muncul di command
palette (`Ctrl+K`).

## `.jenderalignore`

Satu pola per baris (gaya gitignore) untuk memperluas larangan akses agen.
Larangan bawaan yang tidak bisa dilepas: `.env*`, `*.pem`, `*.key`, `id_rsa*`,
kredensial, dan lainnya. `.gitignore` difilter dari hasil `ls`/`glob`/`grep`.
