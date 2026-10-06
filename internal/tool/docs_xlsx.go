package tool

// write_xlsx: membuat file Excel (.xlsx) dari data sheets JSON dengan
// library excelize. Baris pertama tiap sheet di-bold sebagai header.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

type xlsxTool struct{ b *base }

func (t xlsxTool) Name() string { return "write_xlsx" }
func (t xlsxTool) Description() string {
	return "Buat file Excel (.xlsx). Parameter: path dan sheets: array JSON [{\"name\": \"Nama\", \"rows\": [[\"Kolom\", ...], [nilai, ...]]}]. Sel bisa string/angka/boolean. Baris pertama tiap sheet dibold sebagai header. Untuk .csv cukup tool write."
}
func (t xlsxTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Path tujuan, harus berekstensi .xlsx"},
			"sheets": map[string]any{
				"type":        "array",
				"description": "Daftar worksheet",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
						"rows": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "array"},
						},
					},
					"required": []string{"name", "rows"},
				},
			},
		},
		"required": []string{"path", "sheets"},
	}
}
func (t xlsxTool) DefaultPerm() perm { return permAsk }
func (t xlsxTool) ReadOnly() bool    { return false }

type xlsxSheet struct {
	Name string    `json:"name"`
	Rows [][]value `json:"rows"`
}

// value sel fleksibel: string, angka, atau boolean.
type value any

func (t xlsxTool) Exec(ctx context.Context, args map[string]any) (Result, error) {
	path, err := resolvePath(t.b.projDir, argString(args, "path"))
	if err != nil {
		return Result{}, err
	}
	if !strings.HasSuffix(strings.ToLower(path), ".xlsx") {
		return Result{Err: true, Content: "path harus berekstensi .xlsx"}, nil
	}
	if t.b.ignore.HardDenied(path) {
		return Result{Err: true, Content: errDenied(path).Error()}, nil
	}
	var sheets []xlsxSheet
	raw, _ := json.Marshal(args["sheets"])
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&sheets); err != nil {
		return Result{Err: true, Content: "sheets harus array [{name, rows}]"}, nil
	}
	if len(sheets) == 0 {
		return Result{Err: true, Content: "minimal satu sheet diperlukan"}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{Err: true, Content: err.Error()}, nil
	}

	f := excelize.NewFile()
	defer f.Close()
	for si, sh := range sheets {
		name := sh.Name
		if name == "" {
			name = fmt.Sprintf("Sheet%d", si+1)
		}
		if si == 0 {
			// Sheet bawaan dinamai sesuai sheet pertama.
			if err := f.SetSheetName("Sheet1", name); err != nil {
				return Result{Err: true, Content: err.Error()}, nil
			}
		} else {
			if _, err := f.NewSheet(name); err != nil {
				return Result{Err: true, Content: err.Error()}, nil
			}
		}
		for ri, row := range sh.Rows {
			for ci, cell := range row {
				cellRef, _ := excelize.CoordinatesToCellName(ci+1, ri+1)
				switch v := cell.(type) {
				case json.Number:
					if n, err := v.Float64(); err == nil {
						f.SetCellValue(name, cellRef, n)
					} else {
						f.SetCellValue(name, cellRef, v.String())
					}
				case nil:
					// kosongkan
				default:
					f.SetCellValue(name, cellRef, fmt.Sprintf("%v", v))
				}
			}
		}
		// Header baris pertama: bold + lebar kolom otomatis.
		if len(sh.Rows) > 0 {
			style, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
			if err == nil {
				last, _ := excelize.CoordinatesToCellName(max(len(sh.Rows[0]), 1), 1)
				f.SetCellStyle(name, "A1", last, style)
			}
			f.SetColWidth(name, "A", "Z", 18)
		}
	}
	if err := f.SaveAs(path); err != nil {
		return Result{Err: true, Content: err.Error()}, nil
	}
	return Result{
		Content: fmt.Sprintf("file Excel dibuat: %s (%d sheet)", displayPath(t.b.projDir, path), len(sheets)),
		Data:    map[string]any{"path": path},
	}, nil
}
