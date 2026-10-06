package tool

// write_docx: mengubah Markdown (subset) menjadi file Word .docx (OOXML
// minimal, dibuat manual dengan archive/zip tanpa dependensi eksternal).
// Didukung: heading #/##/###, daftar - dan 1., **tebal**, *miring*, `kode`,
// blok kode ```, dan tabel | a | b | dengan baris pemisah |---|.

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type docxTool struct{ b *base }

func (t docxTool) Name() string { return "write_docx" }
func (t docxTool) Description() string {
	return "Buat file Word (.docx) dari Markdown. Mendukung heading #/##/###, daftar - dan 1., **tebal**, *miring*, `kode`, blok kode ```, dan tabel |a|b| dengan baris pemisah |---|. Untuk .md/.csv cukup tool write."
}
func (t docxTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "description": "Path tujuan, harus berekstensi .docx"},
			"content": map[string]any{"type": "string", "description": "Isi dokumen dalam format Markdown"},
		},
		"required": []string{"path", "content"},
	}
}
func (t docxTool) DefaultPerm() perm { return permAsk }
func (t docxTool) ReadOnly() bool    { return false }

func (t docxTool) Exec(ctx context.Context, args map[string]any) (Result, error) {
	path, err := resolvePath(t.b.projDir, argString(args, "path"))
	if err != nil {
		return Result{}, err
	}
	if !strings.HasSuffix(strings.ToLower(path), ".docx") {
		return Result{Err: true, Content: "path harus berekstensi .docx"}, nil
	}
	if t.b.ignore.HardDenied(path) {
		return Result{Err: true, Content: errDenied(path).Error()}, nil
	}
	content := argString(args, "content")
	if strings.TrimSpace(content) == "" {
		return Result{Err: true, Content: "content kosong"}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{Err: true, Content: err.Error()}, nil
	}
	if err := writeDocx(path, content); err != nil {
		return Result{Err: true, Content: err.Error()}, nil
	}
	return Result{
		Content: fmt.Sprintf("file Word dibuat: %s", displayPath(t.b.projDir, path)),
		Data:    map[string]any{"path": path},
	}, nil
}

// writeDocx menyusun paket .docx.
func writeDocx(path, markdown string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{
		"[Content_Types].xml": docxContentTypes,
		"_rels/.rels":         docxRels,
		"word/document.xml":   buildDocxDocument(markdown),
	} {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(body)); err != nil {
			return err
		}
	}
	return zw.Close()
}

const docxContentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`

const docxRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`

func buildDocxDocument(markdown string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	b.WriteString(docxBlocks(markdown))
	b.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720"/></w:sectPr>`)
	b.WriteString(`</w:body></w:document>`)
	return b.String()
}

// docxBlock satu elemen dokumen (paragraf, heading, tabel, ...).
type docxRun struct {
	text string
	bold bool
	ital bool
	code bool
}

func docxBlocks(md string) string {
	var out strings.Builder
	lines := strings.Split(md, "\n")
	i := 0
	for i < len(lines) {
		ln := strings.TrimRight(lines[i], "\r")
		trim := strings.TrimSpace(ln)
		switch {
		case trim == "":
			i++

		case strings.HasPrefix(trim, "```"):
			// Blok kode sampai fence penutup.
			i++
			var code []string
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
				code = append(code, lines[i])
				i++
			}
			i++ // lewati fence penutup
			for _, c := range code {
				out.WriteString(docxPara([]docxRun{{text: c, code: true}}, 0, "", docxCodePara))
			}
			out.WriteString(docxPara(nil, 0, "", ""))

		case isTableRow(trim):
			// Kumpulkan baris tabel berurutan.
			var rows [][]string
			for i < len(lines) {
				r := strings.TrimSpace(lines[i])
				if !isTableRow(r) {
					break
				}
				if !isTableSep(r) {
					rows = append(rows, parseTableRow(r))
				}
				i++
			}
			out.WriteString(docxTable(rows))
			out.WriteString(docxPara(nil, 0, "", "")) // pemisah wajib setelah tabel

		case strings.HasPrefix(trim, "#"):
			level := 0
			for level < len(trim) && trim[level] == '#' {
				level++
			}
			if level > 3 {
				level = 3
			}
			out.WriteString(docxPara(inlineRuns(strings.TrimSpace(trim[level:])), level, "", ""))
			i++

		case strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* "):
			for i < len(lines) {
				t := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(t, "- ") && !strings.HasPrefix(t, "* ") {
					break
				}
				out.WriteString(docxPara(inlineRuns(strings.TrimPrefix(t, t[:2])), 0, "•  ", docxBullet))
				i++
			}

		case isNumberedItem(trim):
			for i < len(lines) {
				t := strings.TrimSpace(lines[i])
				if !isNumberedItem(t) {
					break
				}
				num, rest, _ := strings.Cut(t, ". ")
				out.WriteString(docxPara(inlineRuns(rest), 0, num+". ", docxBullet))
				i++
			}

		default:
			out.WriteString(docxPara(inlineRuns(trim), 0, "", ""))
			i++
		}
	}
	return out.String()
}

func isTableRow(s string) bool {
	return strings.HasPrefix(s, "|") && strings.HasSuffix(s, "|") && strings.Count(s, "|") >= 2
}

func isTableSep(s string) bool {
	if !isTableRow(s) {
		return false
	}
	for _, cell := range strings.Split(strings.Trim(s, "|"), "|") {
		c := strings.TrimSpace(cell)
		if c != "" && strings.Trim(c, "-: ") != "" {
			return false
		}
	}
	return true
}

func parseTableRow(s string) []string {
	var cells []string
	for _, c := range strings.Split(strings.Trim(s, "|"), "|") {
		cells = append(cells, strings.TrimSpace(c))
	}
	return cells
}

func isNumberedItem(s string) bool {
	dot := strings.Index(s, ". ")
	if dot < 1 {
		return false
	}
	for _, ch := range s[:dot] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

const (
	docxCodePara  = `<w:pPr><w:shd w:val="clear" w:fill="F2F2F2"/><w:spacing w:after="0" w:line="240" w:lineRule="auto"/><w:ind w:left="240"/></w:pPr>`
	docxBullet    = `<w:pPr><w:ind w:left="432" w:hanging="288"/></w:pPr>`
	docxHeading1  = `<w:pPr><w:spacing w:before="360" w:after="160"/><w:outlineLvl w:val="0"/></w:pPr>`
	docxHeading2  = `<w:pPr><w:spacing w:before="280" w:after="120"/><w:outlineLvl w:val="1"/></w:pPr>`
	docxHeading3  = `<w:pPr><w:spacing w:before="240" w:after="100"/><w:outlineLvl w:val="2"/></w:pPr>`
	docxBodyPara  = `<w:pPr><w:spacing w:after="120"/></w:pPr>`
	docxSeparator = `<w:pPr><w:spacing w:after="60"/></w:pPr>`
)

// docxPara menyusun satu paragraf; pPr bawaan dipilih otomatis bila kosong.
func docxPara(runs []docxRun, heading int, prefix, pPr string) string {
	if pPr == "" {
		switch heading {
		case 1:
			pPr = docxHeading1
		case 2:
			pPr = docxHeading2
		case 3:
			pPr = docxHeading3
		default:
			pPr = docxBodyPara
		}
	}
	var b strings.Builder
	b.WriteString("<w:p>" + pPr)
	if prefix != "" {
		b.WriteString(`<w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">` + xmlEscape(prefix) + `</w:t></w:r>`)
	}
	for _, r := range runs {
		var pr strings.Builder
		pr.WriteString("<w:rPr>")
		if heading > 0 || r.bold {
			pr.WriteString("<w:b/>")
		}
		if r.ital {
			pr.WriteString("<w:i/>")
		}
		if r.code {
			pr.WriteString(`<w:rFonts w:ascii="Consolas" w:hAnsi="Consolas"/><w:shd w:val="clear" w:fill="F2F2F2"/>`)
		} else if heading > 0 {
			sz := map[int]string{1: "48", 2: "36", 3: "30"}[heading]
			pr.WriteString(`<w:color w:val="1F3864"/><w:sz w:val="` + sz + `"/>`)
		}
		if r.code {
			pr.WriteString(`<w:sz w:val="20"/>`)
		}
		pr.WriteString("</w:rPr>")
		if pr.Len() > len("<w:rPr></w:rPr>") {
			b.WriteString("<w:r>" + pr.String() + `<w:t xml:space="preserve">` + xmlEscape(r.text) + `</w:t></w:r>`)
			continue
		}
		b.WriteString(`<w:r><w:t xml:space="preserve">` + xmlEscape(r.text) + `</w:t></w:r>`)
	}
	b.WriteString("</w:p>")
	return b.String()
}

// docxTable menyusun tabel; baris pertama menjadi header (bold).
func docxTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	var b strings.Builder
	b.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/><w:tblBorders>` +
		`<w:top w:val="single" w:sz="4" w:color="BFBFBF"/><w:left w:val="single" w:sz="4" w:color="BFBFBF"/>` +
		`<w:bottom w:val="single" w:sz="4" w:color="BFBFBF"/><w:right w:val="single" w:sz="4" w:color="BFBFBF"/>` +
		`<w:insideH w:val="single" w:sz="4" w:color="BFBFBF"/><w:insideV w:val="single" w:sz="4" w:color="BFBFBF"/>` +
		`</w:tblBorders></w:tblPr><w:tblGrid>`)
	for i := 0; i < cols; i++ {
		b.WriteString(`<w:gridCol/>`)
	}
	b.WriteString(`</w:tblGrid>`)
	for ri, r := range rows {
		b.WriteString("<w:tr>")
		for ci := 0; ci < cols; ci++ {
			cell := ""
			if ci < len(r) {
				cell = r[ci]
			}
			b.WriteString(`<w:tc><w:tcPr><w:tcW w:w="0" w:type="auto"/></w:tcPr>`)
			runs := inlineRuns(cell)
			if ri == 0 {
				for i := range runs {
					runs[i].bold = true
				}
			}
			b.WriteString(docxPara(runs, 0, "", docxSeparator))
			b.WriteString(`</w:tc>`)
		}
		b.WriteString("</w:tr>")
	}
	b.WriteString(`</w:tbl>`)
	return b.String()
}

// inlineRuns mengurai **tebal**, *miring*, dan `kode` menjadi run.
func inlineRuns(s string) []docxRun {
	var runs []docxRun
	flush := func(text string, bold, ital, code bool) {
		if text != "" {
			runs = append(runs, docxRun{text: text, bold: bold, ital: ital, code: code})
		}
	}
	var buf strings.Builder
	bold, ital, code := false, false, false
	for i := 0; i < len(s); i++ {
		switch {
		case strings.HasPrefix(s[i:], "**"):
			flush(buf.String(), bold, ital, code)
			buf.Reset()
			bold = !bold
			i++
		case s[i] == '*' && !code:
			flush(buf.String(), bold, ital, code)
			buf.Reset()
			ital = !ital
		case s[i] == '`':
			flush(buf.String(), bold, ital, code)
			buf.Reset()
			code = !code
		default:
			buf.WriteByte(s[i])
		}
	}
	flush(buf.String(), bold, ital, code)
	return runs
}

// xmlEscape escape karakter khusus XML.
func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}
