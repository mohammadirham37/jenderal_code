package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/mohammadirham37/jenderal_code/catalog"
	"github.com/mohammadirham37/jenderal_code/internal/agent"
	"github.com/mohammadirham37/jenderal_code/internal/config"
	"github.com/mohammadirham37/jenderal_code/internal/provider"
	"github.com/mohammadirham37/jenderal_code/internal/session"
)

// cacheDirOf alias agar run.go tidak duplikat.
func cacheDirOf() string { return config.CacheDir() }

// ---- auth ----

func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Kelola kredensial provider (login/logout/list)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "login <provider>",
		Short: "Simpan API key provider di keychain OS (fallback file aman)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provID := args[0]
			def, err := catalog.Provider(provID)
			if err != nil {
				return err
			}
			fmt.Printf("API key untuk %s (%s): ", def.Name, def.ID)
			var key string
			if term.IsTerminal(int(os.Stdin.Fd())) {
				b, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Println()
				if err != nil {
					return err
				}
				key = strings.TrimSpace(string(b))
			} else {
				r := bufio.NewReader(os.Stdin)
				ln, _ := r.ReadString('\n')
				key = strings.TrimSpace(ln)
			}
			if key == "" {
				return fmt.Errorf("api key kosong")
			}
			if err := config.EnsureDirs(); err != nil {
				return err
			}
			store := provider.NewKeyStore(config.DataDir())
			if err := store.Set(provID, key); err != nil {
				return err
			}
			if _, ok := provider.ResolveKey(provID, def.EnvKey, "", store); ok {
				fmt.Printf("✔ API key %s tersimpan. Uji dengan: jenderal models\n", provID)
			}
			return nil
		},
	}, &cobra.Command{
		Use:   "logout <provider>",
		Short: "Hapus API key provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := provider.NewKeyStore(config.DataDir())
			if err := store.Delete(args[0]); err != nil {
				return err
			}
			fmt.Printf("✔ API key %s dihapus\n", args[0])
			return nil
		},
	}, &cobra.Command{
		Use:   "list",
		Short: "Tampilkan provider yang punya kredensial",
		RunE: func(cmd *cobra.Command, args []string) error {
			store := provider.NewKeyStore(config.DataDir())
			providers, _ := catalog.Providers()
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "PROVIDER\tSTATUS\tSUMBER")
			for _, p := range providers {
				if env := os.Getenv(p.EnvKey); env != "" {
					fmt.Fprintf(w, "%s\t✔ ada\tenv %s\n", p.ID, p.EnvKey)
					continue
				}
				if key, ok := store.Get(p.ID); ok && key != "" {
					src := "keychain"
					if _, err := os.Stat(filepath.Join(config.DataDir(), "credentials.json")); err == nil {
						// keyring bisa saja gagal; label generik
						src = "keychain/file"
					}
					fmt.Fprintf(w, "%s\t✔ ada\t%s\n", p.ID, src)
					continue
				}
				if p.RequiresKey {
					fmt.Fprintf(w, "%s\t✗ belum\t(jalankan: jenderal auth login %s)\n", p.ID, p.ID)
				} else {
					fmt.Fprintf(w, "%s\tlokal\t%s\n", p.ID, p.BaseURL)
				}
			}
			return w.Flush()
		},
	})
	return cmd
}

// ---- models ----

func modelsCmd() *cobra.Command {
	var refresh bool
	var prov string
	cmd := &cobra.Command{
		Use:   "models",
		Short: "Daftar model yang tersedia beserta harga",
		RunE: func(cmd *cobra.Command, args []string) error {
			if refresh {
				return refreshCatalog()
			}
			a, err := bootstrap(nil)
			if err != nil {
				return err
			}
			defer a.store.Close()
			ready := map[string]bool{}
			for _, id := range a.reg.IDs() {
				ready[id] = true
			}
			providers, _ := catalog.Providers()
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "PROVIDER\tMODEL\tCTX\tIN $/M\tOUT $/M\tSTATUS")
			for _, p := range providers {
				if prov != "" && p.ID != prov {
					continue
				}
				status := "belum ada key"
				if ready[p.ID] {
					status = "siap"
				} else if !p.RequiresKey {
					status = "lokal (cek server)"
				}
				for i, m := range p.Models {
					pname := p.ID
					if i > 0 {
						pname = ""
					}
					fmt.Fprintf(w, "%s\t%s%s\t%s\t%.2f\t%.2f\t%s\n",
						pname, m.ID, recStar(m.Recommended), humanTokensC(m.ContextWindow),
						m.PriceInputPerM, m.PriceOutputPerM, status)
				}
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "ambil daftar/harga model terbaru dari endpoint provider")
	cmd.Flags().StringVar(&prov, "provider", "", "batasi ke satu provider")
	return cmd
}

func recStar(rec bool) string {
	if rec {
		return " ★"
	}
	return ""
}

func humanTokensC(n int) string {
	switch {
	case n >= 1000000:
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return strconv.Itoa(n)
	}
}

// refreshCatalog menggabungkan daftar model dari endpoint provider siap pakai
// ke katalog dan menyimpannya di cache.
func refreshCatalog() error {
	a, err := bootstrap(nil)
	if err != nil {
		return err
	}
	defer a.store.Close()

	data, err := catalog_ProvidersJSON()
	if err != nil {
		return err
	}
	var cat struct {
		Version   int `json:"version"`
		Updated   string
		Providers []struct {
			ID     string `json:"id"`
			Models []struct {
				ID              string  `json:"id"`
				ContextWindow   int     `json:"context_window"`
				MaxOutput       int     `json:"max_output"`
				SupportsTools   bool    `json:"supports_tools"`
				PriceInputPerM  float64 `json:"price_input_per_m"`
				PriceOutputPerM float64 `json:"price_output_per_m"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &cat); err != nil {
		return err
	}
	changed := 0
	for i := range cat.Providers {
		p := &cat.Providers[i]
		pr, err := a.reg.Get(p.ID)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		live, err := pr.Models(ctx)
		cancel()
		if err != nil {
			continue
		}
		have := map[string]bool{}
		for _, m := range p.Models {
			have[m.ID] = true
		}
		for _, m := range live {
			if !have[m.ID] {
				p.Models = append(p.Models, struct {
					ID              string  `json:"id"`
					ContextWindow   int     `json:"context_window"`
					MaxOutput       int     `json:"max_output"`
					SupportsTools   bool    `json:"supports_tools"`
					PriceInputPerM  float64 `json:"price_input_per_m"`
					PriceOutputPerM float64 `json:"price_output_per_m"`
				}{m.ID, m.ContextWindow, m.MaxOutput, m.SupportsTools, m.PriceInputPerM, m.PriceOutputPerM})
				changed++
			}
		}
	}
	cat.Updated = time.Now().Format("2006-01-02")
	out, _ := json.MarshalIndent(cat, "", "  ")
	if err := catalog.SaveCache(config.CacheDir(), out); err != nil {
		return err
	}
	fmt.Printf("✔ katalog diperbarui (%d model baru) — tersimpan di %s\n", changed, filepath.Join(config.CacheDir(), "models.json"))
	return nil
}

func catalog_ProvidersJSON() ([]byte, error) {
	providers, err := catalog.Providers()
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"version": 1, "updated": time.Now().Format("2006-01-02"), "providers": providers})
}

// ---- sessions ----

func sessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Kelola sesi (list/export/import/rm/rename)",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list [kata-kunci]",
			Short: "Daftar sesi di proyek ini (--all untuk semua proyek)",
			RunE: func(cmd *cobra.Command, args []string) error {
				a, err := bootstrap(nil)
				if err != nil {
					return err
				}
				defer a.store.Close()
				all, _ := cmd.Flags().GetBool("all")
				q := ""
				if len(args) > 0 {
					q = strings.Join(args, " ")
				}
				projectPath := a.dir
				if all {
					projectPath = ""
				}
				list, err := a.store.ListSessions(projectPath, q, 50)
				if err != nil {
					return err
				}
				w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
				fmt.Fprintln(w, "ID\tJUDUL\tMODEL\tDIUBAH")
				for _, s := range list {
					title := s.Title
					if title == "" {
						title = "(tanpa judul)"
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.ID, title, s.Model, s.UpdatedAt.Format("2006-01-02 15:04"))
				}
				return w.Flush()
			},
		},
		&cobra.Command{
			Use:   "export <id>",
			Short: "Ekspor sesi ke JSON (--format md untuk Markdown)",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				a, err := bootstrap(nil)
				if err != nil {
					return err
				}
				defer a.store.Close()
				exp, err := a.store.ExportSession(args[0])
				if err != nil {
					return err
				}
				format, _ := cmd.Flags().GetString("format")
				outPath, _ := cmd.Flags().GetString("out")
				var data []byte
				if format == "md" {
					var b strings.Builder
					fmt.Fprintf(&b, "# Sesi %s (%s)\n\n", exp.Session.Title, exp.Session.ID)
					for _, m := range exp.Messages {
						switch m.Role {
						case "user":
							b.WriteString("## Anda\n\n" + m.Content + "\n\n")
						case "assistant":
							b.WriteString("## Asisten\n\n" + m.Content + "\n\n")
						case "tool":
							b.WriteString("- tool `" + m.ToolName + "`: " + firstLineOf(m.Content) + "\n")
						}
					}
					data = []byte(b.String())
				} else {
					data, err = json.MarshalIndent(exp, "", "  ")
					if err != nil {
						return err
					}
				}
				if outPath == "" {
					os.Stdout.Write(data)
					return nil
				}
				return os.WriteFile(outPath, data, 0o644)
			},
		},
		&cobra.Command{
			Use:   "import <file.json>",
			Short: "Impor sesi dari file JSON ekspor",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				a, err := bootstrap(nil)
				if err != nil {
					return err
				}
				defer a.store.Close()
				b, err := os.ReadFile(args[0])
				if err != nil {
					return err
				}
				var exp session.ExportFormat
				if err := json.Unmarshal(b, &exp); err != nil {
					return fmt.Errorf("file bukan ekspor sesi yang valid: %w", err)
				}
				id, err := a.store.ImportSession(&exp)
				if err != nil {
					return err
				}
				fmt.Printf("✔ sesi diimpor dengan ID %s\n", id)
				return nil
			},
		},
		&cobra.Command{
			Use:     "rm <id>",
			Aliases: []string{"delete"},
			Short:   "Hapus sesi",
			Args:    cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				a, err := bootstrap(nil)
				if err != nil {
					return err
				}
				defer a.store.Close()
				if err := a.store.DeleteSession(args[0]); err != nil {
					return err
				}
				fmt.Printf("✔ sesi %s dihapus\n", args[0])
				return nil
			},
		},
		&cobra.Command{
			Use:   "rename <id> <judul>",
			Short: "Ganti nama sesi",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				a, err := bootstrap(nil)
				if err != nil {
					return err
				}
				defer a.store.Close()
				if err := a.store.RenameSession(args[0], args[1]); err != nil {
					return err
				}
				fmt.Printf("✔ sesi %s dinamai %q\n", args[0], args[1])
				return nil
			},
		},
	)
	for _, c := range cmd.Commands() {
		switch c.Name() {
		case "list":
			c.Flags().Bool("all", false, "tampilkan sesi semua proyek")
		case "export":
			c.Flags().String("format", "json", "format ekspor: json | md")
			c.Flags().StringP("out", "o", "", "tulis ke file, bukan stdout")
		}
	}
	return cmd
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// ---- mcp ----

func mcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Kelola server MCP (add/list/rm)",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "add <nama> -- <command> [args...]",
			Short: "Tambah server MCP lokal ke konfigurasi proyek (--global untuk global)",
			Example: `jenderal mcp add db -- npx -y @example/mcp-postgres
jenderal mcp add github --global -- npx -y @modelcontextprotocol/server-github`,
			RunE: func(cmd *cobra.Command, args []string) error {
				global, _ := cmd.Flags().GetBool("global")
				remote, _ := cmd.Flags().GetString("url")
				if len(args) < 1 {
					return fmt.Errorf("sebutkan nama server; contoh: jenderal mcp add db -- npx -y @example/mcp-postgres")
				}
				name := args[0]
				entry := map[string]any{}
				if remote != "" {
					entry["type"] = "remote"
					entry["url"] = remote
				} else {
					rest := args[1:]
					if len(rest) == 0 {
						return fmt.Errorf("sebutkan command setelah --, atau gunakan --url untuk remote")
					}
					entry["type"] = "local"
					entry["command"] = rest
				}
				path := "jenderal.jsonc"
				if global {
					path = filepath.Join(config.ConfigDir(), "jenderal.jsonc")
				}
				return updateConfigFile(path, func(cfg map[string]any) {
					mcpMap, _ := cfg["mcp"].(map[string]any)
					if mcpMap == nil {
						mcpMap = map[string]any{}
					}
					mcpMap[name] = entry
					cfg["mcp"] = mcpMap
				}, fmt.Sprintf("server MCP %q ditambahkan ke %s", name, path))
			},
		},
		&cobra.Command{
			Use:   "list",
			Short: "Daftar server MCP terkonfigurasi",
			RunE: func(cmd *cobra.Command, args []string) error {
				a, err := bootstrap(nil)
				if err != nil {
					return err
				}
				defer a.store.Close()
				servers := a.cfg.MCPServers()
				if len(servers) == 0 {
					fmt.Println("belum ada server MCP; tambahkan dengan `jenderal mcp add <nama> -- <command>`")
					return nil
				}
				names := make([]string, 0, len(servers))
				for n := range servers {
					names = append(names, n)
				}
				sort.Strings(names)
				for _, n := range names {
					b, _ := json.Marshal(servers[n])
					fmt.Printf("%s  %s\n", n, string(b))
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "rm <nama>",
			Short: "Hapus server MCP dari konfigurasi proyek (--global untuk global)",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				global, _ := cmd.Flags().GetBool("global")
				path := "jenderal.jsonc"
				if global {
					path = filepath.Join(config.ConfigDir(), "jenderal.jsonc")
				}
				return updateConfigFile(path, func(cfg map[string]any) {
					if mcpMap, ok := cfg["mcp"].(map[string]any); ok {
						delete(mcpMap, args[0])
					}
				}, fmt.Sprintf("server MCP %q dihapus dari %s", args[0], path))
			},
		},
	)
	for _, c := range cmd.Commands() {
		c.Flags().Bool("global", false, "operasi pada konfigurasi global")
	}
	cmd.Commands()[0].Flags().String("url", "", "URL server MCP remote (bukan command lokal)")
	return cmd
}

// updateConfigFile membaca JSONC, menerapkan perubahan, menulis kembali.
// Komentar di file akan hilang saat ditulis ulang (catatan ke pengguna).
func updateConfigFile(path string, fn func(map[string]any), successMsg string) error {
	var cfgMap map[string]any = map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		m, err := config.Parse(b)
		if err != nil {
			return fmt.Errorf("%s tidak bisa diparsing: %w", path, err)
		}
		cfgMap = m
	}
	fn(cfgMap)
	out, err := json.MarshalIndent(cfgMap, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return err
	}
	fmt.Println("✔", successMsg)
	if len(cfgMap) > 0 {
		if _, err := os.Stat(path); err == nil {
			_ = os.Rename(path, path) // no-op
		}
	}
	return nil
}

// ---- init ----

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Analisis repo dan buat JENDERAL.md awal",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _ := os.Getwd()
			target := filepath.Join(dir, "JENDERAL.md")
			if _, err := os.Stat(target); err == nil {
				return fmt.Errorf("JENDERAL.md sudah ada; edit manual atau hapus dulu")
			}
			// pakai helper dari agent package lewat import tersembunyi
			content := agent.InitProject(dir)
			if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
				return err
			}
			fmt.Printf("✔ JENDERAL.md dibuat di %s\nBuka dan lengkapi bagian 'Aturan tim'.\n", target)
			return nil
		},
	}
}

// ---- stats ----

func statsCmd() *cobra.Command {
	var days int
	var all bool
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Ringkasan pemakaian token dan biaya",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := bootstrap(nil)
			if err != nil {
				return err
			}
			defer a.store.Close()
			projectPath := a.dir
			if all {
				projectPath = ""
			}
			since := time.Now().AddDate(0, 0, -days)
			rows, err := a.store.Usage(projectPath, since)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				fmt.Println("belum ada pemakaian tercatat.")
				return nil
			}
			cur := a.cfg.Currency()
			rate := a.cfg.ExchangeRate()
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "TANGGAL\tMODEL\tIN\tOUT\tCACHE\tBIAYA")
			var totIn, totOut, totCache int64
			var totCost float64
			for _, r := range rows {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Date, r.Model,
					humanTokensC(int(r.TokensIn)), humanTokensC(int(r.TokensOut)), humanTokensC(int(r.CacheRead)),
					formatCostC(r.CostUSD, cur, rate))
				totIn += r.TokensIn
				totOut += r.TokensOut
				totCache += r.CacheRead
				totCost += r.CostUSD
			}
			fmt.Fprintln(w, "\tTOTAL\t"+humanTokensC(int(totIn))+"\t"+humanTokensC(int(totOut))+"\t"+humanTokensC(int(totCache))+"\t"+formatCostC(totCost, cur, rate))
			return w.Flush()
		},
	}
	cmd.Flags().IntVar(&days, "days", 30, "rentang hari")
	cmd.Flags().BoolVar(&all, "all", false, "semua proyek")
	return cmd
}

func formatCostC(usd float64, currency string, rate float64) string {
	local := usd * rate
	if local >= 1000 {
		return fmt.Sprintf("%s %.0f", currency, local)
	}
	return fmt.Sprintf("%s %.2f", currency, local)
}

// ---- upgrade ----

func upgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Periksa dan unduh versi terbaru",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := &http.Client{Timeout: 20 * time.Second}
			req, _ := http.NewRequest("GET", "https://api.github.com/repos/mohammadirham37/jenderal_code/releases/latest", nil)
			req.Header.Set("Accept", "application/vnd.github+json")
			resp, err := client.Do(req)
			if err != nil || resp.StatusCode != 200 {
				fmt.Println("tidak bisa memeriksa pembaruan (repo rilis belum tersedia).")
				fmt.Println("Perbarui manual:")
				fmt.Println("  go install github.com/mohammadirham37/jenderal_code/cmd/jenderal@latest")
				if resp != nil {
					resp.Body.Close()
				}
				return nil
			}
			defer resp.Body.Close()
			var rel struct {
				TagName string `json:"tag_name"`
				HTMLURL string `json:"html_url"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
				return err
			}
			fmt.Printf("versi terpasang: %s\nversi terbaru : %s\n", Version, rel.TagName)
			if strings.TrimPrefix(rel.TagName, "v") == Version {
				fmt.Println("✔ sudah versi terbaru")
				return nil
			}
			fmt.Printf("unduh dari: %s\n", rel.HTMLURL)
			return nil
		},
	}
}
