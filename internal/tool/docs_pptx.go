package tool

// write_pptx: membuat file PowerPoint (.pptx) minimal 16:9 dari daftar slide.
// Semua teks membawa format eksplisit (posisi, ukuran, warna) agar tampilan
// tidak bergantung pada master/theme yang dibuat sesederhana mungkin.

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type pptxTool struct{ b *base }

func (t pptxTool) Name() string { return "write_pptx" }
func (t pptxTool) Description() string {
	return "Buat file PowerPoint (.pptx) 16:9. Parameter: path, title (opsional, slide sampul), subtitle (opsional), dan slides: array JSON [{\"title\", \"bullets\": [..]}]. Untuk .md/.csv cukup tool write."
}
func (t pptxTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":     map[string]any{"type": "string", "description": "Path tujuan, harus berekstensi .pptx"},
			"title":    map[string]any{"type": "string", "description": "Judul slide sampul (opsional)"},
			"subtitle": map[string]any{"type": "string", "description": "Subjudul slide sampul (opsional)"},
			"slides": map[string]any{
				"type":        "array",
				"description": "Daftar slide isi",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title":   map[string]any{"type": "string"},
						"bullets": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
					"required": []string{"title"},
				},
			},
		},
		"required": []string{"path", "slides"},
	}
}
func (t pptxTool) DefaultPerm() perm { return permAsk }
func (t pptxTool) ReadOnly() bool    { return false }

// pptxSlide satu slide isi.
type pptxSlide struct {
	Title   string   `json:"title"`
	Bullets []string `json:"bullets"`
}

func (t pptxTool) Exec(ctx context.Context, args map[string]any) (Result, error) {
	path, err := resolvePath(t.b.projDir, argString(args, "path"))
	if err != nil {
		return Result{}, err
	}
	if !strings.HasSuffix(strings.ToLower(path), ".pptx") {
		return Result{Err: true, Content: "path harus berekstensi .pptx"}, nil
	}
	if t.b.ignore.HardDenied(path) {
		return Result{Err: true, Content: errDenied(path).Error()}, nil
	}
	var slides []pptxSlide
	raw, _ := json.Marshal(args["slides"])
	if err := json.Unmarshal(raw, &slides); err != nil {
		return Result{Err: true, Content: "slides harus array [{title, bullets}]"}, nil
	}
	if len(slides) == 0 {
		return Result{Err: true, Content: "minimal satu slide diperlukan"}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{Err: true, Content: err.Error()}, nil
	}
	if err := writePptx(path, argString(args, "title"), argString(args, "subtitle"), slides); err != nil {
		return Result{Err: true, Content: err.Error()}, nil
	}
	return Result{
		Content: fmt.Sprintf("file PowerPoint dibuat: %s (%d slide)", displayPath(t.b.projDir, path), len(slides)),
		Data:    map[string]any{"path": path},
	}, nil
}

// writePptx menyusun paket .pptx.
func writePptx(path, coverTitle, coverSubtitle string, slides []pptxSlide) error {
	// Susun XML semua slide final: sampul (bila ada) menjadi slide1.
	var slideXMLs []string
	if strings.TrimSpace(coverTitle) != "" {
		slideXMLs = append(slideXMLs, pptxCoverSlide(coverTitle, coverSubtitle))
	}
	for _, s := range slides {
		slideXMLs = append(slideXMLs, pptxContentSlide(s))
	}
	total := len(slideXMLs)

	files := map[string]string{
		"[Content_Types].xml":                          pptxContentTypes(total),
		"_rels/.rels":                                  pptxRootRels,
		"ppt/presentation.xml":                         pptxPresentation(total),
		"ppt/_rels/presentation.xml.rels":              pptxPresentationRels(total),
		"ppt/slideMasters/slideMaster1.xml":            pptxMaster,
		"ppt/slideMasters/_rels/slideMaster1.xml.rels": pptxMasterRels,
		"ppt/slideLayouts/slideLayout1.xml":            pptxLayout,
		"ppt/slideLayouts/_rels/slideLayout1.xml.rels": pptxLayoutRels,
		"ppt/theme/theme1.xml":                         pptxTheme,
	}
	for i, xml := range slideXMLs {
		files[fmt.Sprintf("ppt/slides/slide%d.xml", i+1)] = xml
		files[fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", i+1)] = pptxSlideRels
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, body := range files {
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

func pptxContentTypes(total int) string {
	var ov strings.Builder
	for i := 1; i <= total; i++ {
		fmt.Fprintf(&ov, `<Override PartName="/ppt/slides/slide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`, i)
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/>` +
		`<Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/>` +
		`<Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/>` +
		`<Override PartName="/ppt/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/>` +
		ov.String() + `</Types>`
}

const pptxRootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/></Relationships>`

func pptxPresentation(nSlides int) string {
	var ids strings.Builder
	for i := 0; i < nSlides; i++ {
		fmt.Fprintf(&ids, `<p:sldId id="%d" r:id="rId%d"/>`, 256+i, i+2)
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:presentation xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst>` + ids.String() + `</p:sldIdLst><p:sldSz cx="12192000" cy="6858000"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>`
}

func pptxPresentationRels(nSlides int) string {
	var rels strings.Builder
	rels.WriteString(`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="slideMasters/slideMaster1.xml"/>`)
	for i := 0; i < nSlides; i++ {
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide%d.xml"/>`, i+2, i+1)
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels.String() + `</Relationships>`
}

const pptxMaster = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sldMaster xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/></p:spTree></p:cSld><p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/><p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst></p:sldMaster>`

const pptxMasterRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="../theme/theme1.xml"/></Relationships>`

const pptxLayout = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sldLayout xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" type="obj"><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/></p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`

const pptxLayoutRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/slideMaster1.xml"/></Relationships>`

const pptxSlideRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/></Relationships>`

const pptxTheme = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="Jenderal"><a:themeElements><a:clrScheme name="Jenderal"><a:dk1><a:srgbClr val="000000"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:dk2><a:srgbClr val="1F3864"/></a:dk2><a:lt2><a:srgbClr val="E7E6E6"/></a:lt2><a:accent1><a:srgbClr val="4472C4"/></a:accent1><a:accent2><a:srgbClr val="ED7D31"/></a:accent2><a:accent3><a:srgbClr val="A5A5A5"/></a:accent3><a:accent4><a:srgbClr val="FFC000"/></a:accent4><a:accent5><a:srgbClr val="5B9BD5"/></a:accent5><a:accent6><a:srgbClr val="70AD47"/></a:accent6><a:hlink><a:srgbClr val="0563C1"/></a:hlink><a:folHlink><a:srgbClr val="954F72"/></a:folHlink></a:clrScheme><a:fontScheme name="Jenderal"><a:majorFont><a:latin typeface="Calibri Light"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont><a:minorFont><a:latin typeface="Calibri"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont></a:fontScheme><a:fmtScheme name="Jenderal"><a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:fillStyleLst><a:lnStyleLst><a:ln><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln></a:lnStyleLst><a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst><a:bgFillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:bgFillStyleLst></a:fmtScheme></a:themeElements></a:theme>`

// pptxTitleShape kotak judul standar.
func pptxTitleShape(text string, id int) string {
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Judul"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="838200" y="365125"/><a:ext cx="10515600" cy="1325563"/></a:xfrm></p:spPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr lang="id-ID" sz="4000" b="1" dirty="0"><a:solidFill><a:srgbClr val="1F3864"/></a:solidFill></a:rPr><a:t>%s</a:t></a:r></a:p></p:txBody></p:sp>`, id, xmlEscape(text))
}

// pptxBulletPara satu paragraf butir; level>0 menjadi sub-butir.
func pptxBulletPara(text string, level int) string {
	const size = "2000"
	color := "333333"
	marL, indent, buch := 342900, -342900, "•"
	if level > 0 {
		marL, indent, buch, color = 685800, -342900, "–", "595959"
	}
	return fmt.Sprintf(`<a:p><a:pPr marL="%d" indent="%d"><a:buFont typeface="Arial" pitchFamily="34" charset="0"/><a:buChar char="%s"/></a:pPr><a:r><a:rPr lang="id-ID" sz="%s" dirty="0"><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:rPr><a:t>%s</a:t></a:r></a:p>`,
		marL, indent, buch, size, color, xmlEscape(text))
}

func pptxContentSlide(s pptxSlide) string {
	var body strings.Builder
	seen := map[string]bool{}
	for _, b := range s.Bullets {
		text := strings.TrimSpace(b)
		if text == "" {
			continue
		}
		level := 0
		for strings.HasPrefix(text, "-") || strings.HasPrefix(text, "*") {
			// butir bersarang ditulis dengan awalan "- " tambahan
			level++
			text = strings.TrimSpace(text[1:])
			if level > 2 {
				level = 2
				break
			}
		}
		if seen[text] {
			continue
		}
		seen[text] = true
		body.WriteString(pptxBulletPara(text, level))
	}
	out := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>` +
		pptxTitleShape(s.Title, 2)
	if body.Len() > 0 {
		out += fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="3" name="Isi"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="838200" y="1825625"/><a:ext cx="10515600" cy="4351338"/></a:xfrm></p:spPr><p:txBody><a:bodyPr/><a:lstStyle/>%s</p:txBody></p:sp>`, body.String())
	}
	out += `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`
	return out
}

func pptxCoverSlide(title, subtitle string) string {
	sub := ""
	if strings.TrimSpace(subtitle) != "" {
		sub = fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="4" name="Subjudul"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="838200" y="4229100"/><a:ext cx="10515600" cy="900000"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr anchor="t"/><a:lstStyle/><a:p><a:pPr algn="ctr"/><a:r><a:rPr lang="id-ID" sz="2000" dirty="0"><a:solidFill><a:srgbClr val="595959"/></a:solidFill></a:rPr><a:t>%s</a:t></a:r></a:p></p:txBody></p:sp>`, xmlEscape(subtitle))
	}
	out := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>` +
		fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Judul"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="838200" y="2743200"/><a:ext cx="10515600" cy="1500000"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr anchor="ctr"/><a:lstStyle/><a:p><a:pPr algn="ctr"/><a:r><a:rPr lang="id-ID" sz="4400" b="1" dirty="0"><a:solidFill><a:srgbClr val="1F3864"/></a:solidFill></a:rPr><a:t>%s</a:t></a:r></a:p></p:txBody></p:sp>`, xmlEscape(title)) +
		sub + `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`
	return out
}
