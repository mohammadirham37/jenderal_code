// Package cli mengimplementasikan semua perintah `jenderal` (PRD 9.1).
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jenderalcode/jenderal/internal/config"
	"github.com/jenderalcode/jenderal/internal/provider"
	"github.com/jenderalcode/jenderal/internal/session"
)

// Versi diisi saat build (-ldflags).
var (
	Version = "0.1.0"
	Commit  = "dev"
	Date    = "unknown"
)

// app kumpulan objek siap pakai untuk semua perintah.
type app struct {
	cfg   *config.Config
	reg   *provider.Registry
	store *session.Store
	keys  *provider.KeyStore
	dir   string // direktori proyek
}

// bootstrap memuat konfigurasi, registry provider, dan database sesi.
func bootstrap(overrides map[string]any) (*app, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if err := config.EnsureDirs(); err != nil {
		return nil, err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, err
	}
	if overrides != nil {
		cfg.Merge(overrides)
	}
	catalog_SetCacheDir()
	keys := provider.NewKeyStore(config.DataDir())
	reg, err := provider.NewRegistry(cfg, keys)
	if err != nil {
		return nil, err
	}
	store, err := session.Open(config.DBPath())
	if err != nil {
		return nil, fmt.Errorf("gagal membuka database sesi: %w", err)
	}
	a := &app{cfg: cfg, reg: reg, store: store, keys: keys, dir: dir}
	attachMock(a)
	return a, nil
}

// NewRootCommand menyusun pohon perintah.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "jenderal",
		Short: "JenderalCode — satu komandan untuk semua model AI",
		Long: `JenderalCode adalah AI coding agent open-source yang ringan dan bebas vendor.

Jalankan tanpa argumen untuk membuka TUI interaktif di folder saat ini.
Contoh:
  jenderal                     # TUI interaktif
  jenderal run "perbaiki bug"  # satu tugas non-interaktif
  jenderal serve --port 4096   # API HTTP+SSE lokal`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			model, _ := cmd.Flags().GetString("model")
			continueLast, _ := cmd.Flags().GetBool("continue")
			sessID, _ := cmd.Flags().GetString("session")
			overrides := map[string]any{}
			if model != "" {
				overrides["model"] = model
			}
			a, err := bootstrap(overrides)
			if err != nil {
				return err
			}
			defer a.store.Close()
			return launchTUI(a, continueLast, sessID)
		},
	}
	root.Flags().StringP("model", "m", "", "model default (provider/model)")
	root.Flags().BoolP("continue", "c", false, "lanjutkan sesi terakhir di proyek ini")
	root.Flags().StringP("session", "s", "", "lanjutkan sesi dengan ID tertentu")

	root.AddCommand(runCmd(), serveCmd(), authCmd(), modelsCmd(),
		sessionsCmd(), mcpCmd(), initCmd(), statsCmd(), upgradeCmd(), desktopCmd())

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Tampilkan versi jenderal",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(versionString())
		},
	})
	root.CompletionOptions.HiddenDefaultCmd = true
	return root
}

// Execute menjalankan CLI; mengembalikan exit code.
func Execute() int {
	root := NewRootCommand()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func versionString() string {
	return fmt.Sprintf("jenderal %s (commit %s, dibangun %s)", Version, Commit, Date)
}

var _ = filepath.Join
