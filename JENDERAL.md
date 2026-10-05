# JENDERAL — Konteks Proyek

JenderalCode adalah agent coding yang dibangun dari PRD di file
`PRD JenderalCode.md` pada repo ini. Aplikasi ditulis Go murni (tanpa CGO).

## Perintah penting

- `go build ./...` — kompilasi semua paket
- `go test ./...` — jalankan unit test (core wajib lulus)
- `go vet ./...` — linter statis
- `go build -o jenderal ./cmd/jenderal` — build binary CLI/TUI
- `JENDERAL_MOCK=1 ./jenderal` — demo TUI tanpa API key

## Konvensi kode

- Bahasa Indonesia untuk komentar, pesan error, dan dokumentasi pengguna;
  identifier Go tetap bahasa Inggris.
- Satu package satu tanggung jawab; antarmuka (TUI/CLI/server) tidak pernah
  memanggil provider langsung — selalu lewat `internal/agent`.
- Event antarmuka dikirim lewat `internal/bus`; jangan mutasi state UI dari
  goroutine lain tanpa mengirim event.
- Setiap perilaku baru di agent loop wajib punya unit test di
  `internal/agent/agent_test.go` dengan provider mock.
- Harga/katalog model di `catalog/models.json` bersifat best-effort dan bisa
  diperbarui dengan `jenderal models --refresh`.

## Aturan tim

- Jangan menambah dependensi berat (Electron/Node, SDK CGO). Pure Go saja.
- API key tidak boleh ditulis ke log, sesi, atau ekspor.
- Server hanya boleh bind 127.0.0.1.
