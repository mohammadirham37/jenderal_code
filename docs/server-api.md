# Server API & SDK

`jenderal serve` membuka API HTTP + SSE di `127.0.0.1` (bind lain ditolak).
Setiap start menghasilkan token acak yang harus dikirim pada setiap request
sebagai header `X-Jenderal-Token`, `Authorization: Bearer <token>`, atau query
`?token=`. Kecuali `/health`, semua endpoint menuntut token.

## Endpoint

| Method & path | Fungsi |
| --- | --- |
| `GET /health` | Cek server hidup (tanpa token) |
| `POST /session` | Buat sesi — body: `{"project_path", "model", "title"}` |
| `GET /session` | Daftar sesi — query: `q` (cari), maks 100 |
| `GET /session/{id}` | Detail sesi + semua pesan aktif |
| `DELETE /session/{id}` | Hapus sesi |
| `POST /session/{id}/message` | Kirim prompt — body: `{"text", "model?"}` (asinkron) |
| `GET /session/{id}/events` | Stream event SSE sesi |
| `POST /session/{id}/permission/{reqID}` | Jawab izin — body: `{"decision":"allow"\|"deny","always":bool}` |
| `POST /session/{id}/abort` | Hentikan agen (setara Esc) |
| `POST /session/{id}/model` | Ganti model — body: `{"model":"provider/model"}` |
| `POST /session/{id}/mode` | Ganti mode — body: `{"mode":"build"\|"plan"\|"toggle"}` |
| `GET /models` | Provider tersedia + katalog model & harga |

## Event SSE

Setiap event berformat `event: <type>` + `data: <json>`:

| Type | Isi |
| --- | --- |
| `text_delta` | `{"text": "potongan jawaban"}` |
| `reasoning_delta` | potongan reasoning model |
| `tool_call` | `{"tool_name","tool_call_id","args"}` |
| `tool_result` | `{"tool_name","result","is_err"}` |
| `usage` | `{"usage":{"input_tokens","output_tokens"},"detail":{"cost_usd"}}` |
| `permission_request` | `{"permission_id","tool_name","text","args"}` — jawab via endpoint permission |
| `status` | pesan status (kompaksi, MCP, dll.) |
| `error` | `{"text": "pesan error"}` |
| `done` | putaran agen selesai |

## Contoh dengan curl

```sh
TOKEN=...   # dicetak saat `jenderal serve` start
BASE=http://127.0.0.1:4096

SID=$(curl -s -X POST -H "X-Jenderal-Token: $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"project_path":"."}' $BASE/session | jq -r .id)

# dengarkan event di terminal lain:
curl -s -N -H "X-Jenderal-Token: $TOKEN" $BASE/session/$SID/events

curl -s -X POST -H "X-Jenderal-Token: $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"text":"jelaskan struktur repo ini"}' $BASE/session/$SID/message
```

## SDK Go (`pkg/sdk`)

```go
package main

import (
	"context"
	"fmt"

	"github.com/jenderalcode/jenderal/pkg/sdk"
)

func main() {
	c := sdk.New("http://127.0.0.1:4096", "<token>")
	ctx := context.Background()

	sess, _ := c.CreateSession(ctx, ".", "zai/glm-4.6")
	events, _ := c.Events(ctx, sess.ID)
	go func() {
		for ev := range events {
			if ev.Type == "text_delta" {
				fmt.Print(ev.Text)
			}
			if ev.Type == "permission_request" {
				// tampilkan dialog di aplikasimu, lalu jawab:
				_ = c.AnswerPermission(ctx, sess.ID, ev.PermissionID, true, false)
			}
		}
	}()

	_ = c.SendMessage(ctx, sess.ID, "jelaskan struktur repo", "")
	select {} // contoh: tunggu sampai selesai (lihat event "done")
}
```

## Catatan keamanan

- Server dirancang **lokal**: bind ke 127.0.0.1/::1/localhost saja.
- Token acak per start; tanpa token request ditolak 401.
- Klien GUI (desktop/ekstensi) memakai SDK ini — fondasi desktop v0.5.
