package tool

// NewBuiltinRegistry membuat registry berisi semua tool bawaan.
func NewBuiltinRegistry(projDir string, ignore *Ignore, todos *TodoStore, bashTimeout int) *Registry {
	b := &base{projDir: projDir, ignore: ignore}
	r := NewRegistry()
	r.Add(readTool{b})
	r.Add(writeTool{b})
	r.Add(editTool{b})
	r.Add(patchTool{b})
	r.Add(bashTool{b: b, defTimeo: bashTimeout})
	r.Add(globTool{b})
	r.Add(grepTool{b})
	r.Add(lsTool{b})
	r.Add(webfetchTool{})
	r.Add(todoTool{store: todos})
	r.Add(lspTool{})
	// Pembuat dokumen: .docx, .pptx, .xlsx (.md/.csv lewat tool write).
	r.Add(docxTool{b})
	r.Add(pptxTool{b})
	r.Add(xlsxTool{b})
	return r
}
