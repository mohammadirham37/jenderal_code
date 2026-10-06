---
name: buat-dokumen
description: Membuat file dokumen profesional — Word (.docx), PowerPoint (.pptx), Excel (.xlsx), CSV, dan Markdown — langsung dari chat.
---

# Membuat Dokumen dari Chat

Gunakan tool khusus berikut untuk membuat file dokumen. Semua path relatif
terhadap folder proyek. JANGAN mencoba menulis file .docx/.pptx/.xlsx dengan
tool `write` biasa — itu hanya untuk teks.

## Word (.docx) — tool `write_docx`

Parameter: `path` (harus berekstensi .docx) dan `content` berupa **Markdown**.
Elemen Markdown yang didukung:

- `# Judul`, `## Subjudul`, `### Sub-subjudul` — heading
- `- item` / `1. item` — daftar berbutir dan bernomor
- `**tebal**`, `*miring*`, `` `kode` `` — format inline
- ``` blok kode ``` — paragraf monospace dengan latar abu
- Tabel: baris `| A | B |` dengan baris pemisah `|---|---|` (baris pertama = header)

Contoh: `write_docx(path="laporan/laporan-januari.docx", content="# Laporan\n\n## Ringkasan\n- Pendapatan naik 12%\n")`

## PowerPoint (.pptx) — tool `write_pptx`

Parameter: `path` (.pptx), `title` (opsional, judul di slide sampul),
`subtitle` (opsional), dan `slides` berupa array JSON:
`[{"title": "Judul slide", "bullets": ["poin satu", "poin dua"]}]`.

Tips deck yang baik: 4–6 butir per slide, butir singkat (maks ±12 kata),
satu ide per slide, slide pembuka dan penutup.

## Excel (.xlsx) — tool `write_xlsx`

Parameter: `path` (.xlsx) dan `sheets` berupa array JSON:
`[{"name": "Rekap", "rows": [["Bulan", "Pendapatan"], ["Januari", 15000000]]}]`.
Sel bisa berupa string, angka, atau boolean. Baris pertama tiap sheet
sebaiknya header kolom (akan dibold otomatis). Buat beberapa sheet untuk
data yang terkait.

## CSV dan Markdown — tool `write`

- CSV: tulis langsung dengan tool `write`, pisahkan kolom dengan koma,
  apit nilai berisi koma dengan kutip ganda. Akhiri baris dengan `\n`.
- Markdown: tulis langsung dengan tool `write`.

## Aturan kualitas

1. Tentukan nama file yang deskriptif (mis. `laporan-penjualan-q3.docx`).
2. Untuk dokumen resmi, buka dengan struktur: judul → ringkasan → isi
   ber-heading → tabel data bila relevan → kesimpulan.
3. Setelah file dibuat, sampaikan lokasi filenya ke pengguna.
