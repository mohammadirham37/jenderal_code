package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// modulePath dipakai untuk mengenali root repo source dan ldflags.
const modulePath = "github.com/mohammadirham37/jenderal_code"

// ---- update ----

func updateCmd() *cobra.Command {
	var source string
	var checkOnly, force bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Perbarui binary jika commit terbaru di remote berbeda",
		Long: `Membandingkan commit binary yang sedang berjalan dengan commit terbaru di remote Git.

Jika berbeda, source ditarik (fast-forward) lalu binary dibangun ulang
dan menggantikan binary lama; backup disimpan sebagai <binary>.bak.

Binary yang dibangun tanpa ldflags (commit "dev") dibandingkan dengan
HEAD source lokal; gunakan --force untuk memaksa build ulang.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(source, checkOnly, force)
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "direktori source repo (default: deteksi dari lokasi binary)")
	cmd.Flags().BoolVar(&checkOnly, "check", false, "hanya cek ketersediaan pembaruan, tanpa mengubah apa pun")
	cmd.Flags().BoolVar(&force, "force", false, "bangun ulang binary meski commit sudah sama")
	return cmd
}

func runUpdate(source string, checkOnly, force bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	if strings.Contains(exe, "go-build") {
		return fmt.Errorf("binary berjalan dari cache `go run`; bangun dulu dengan `go build -o jenderal ./cmd/jenderal`, lalu jalankan `jenderal update`")
	}

	src := source
	if src == "" {
		if src, err = detectSourceDir(exe); err != nil {
			return err
		}
	} else {
		if src, err = filepath.Abs(src); err != nil {
			return err
		}
		if !isJenderalSource(src) {
			return fmt.Errorf("%s bukan source repo %s (butuh .git dan go.mod yang cocok)", src, modulePath)
		}
	}

	// Commit binary saat ini; kalau binary dibangun tanpa ldflags, pakai HEAD source.
	curCommit := Commit
	if curCommit == "" || curCommit == "dev" {
		if h, err := gitOut(src, "rev-parse", "HEAD"); err == nil {
			curCommit = h
		}
	}

	branch, err := gitOut(src, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Errorf("baca branch source %s: %w", src, err)
	}
	if branch == "" || branch == "HEAD" {
		return fmt.Errorf("source di %s berada di detached HEAD; pindah ke branch biasa dulu", src)
	}
	remoteOut, err := gitOut(src, "ls-remote", "origin", "refs/heads/"+branch)
	if err != nil {
		return fmt.Errorf("tidak bisa menghubungi origin: %w", err)
	}
	fields := strings.Fields(remoteOut)
	if len(fields) == 0 {
		return fmt.Errorf("branch %s tidak ditemukan di remote origin", branch)
	}
	latest := fields[0]

	fmt.Printf("versi terpasang : %s\n", versionString())
	fmt.Printf("commit binary   : %s\n", shortOf(curCommit))
	fmt.Printf("commit remote   : %s (origin/%s)\n", shortOf(latest), branch)

	if !force && sameCommit(curCommit, latest) {
		fmt.Println("✔ binary sudah dari commit terbaru")
		return nil
	}
	if checkOnly {
		fmt.Println("pembaruan tersedia; jalankan `jenderal update` untuk memasang.")
		return nil
	}

	fmt.Printf("menarik perubahan dari origin/%s ...\n", branch)
	if _, err := gitOut(src, "pull", "--ff-only", "origin", branch); err != nil {
		return fmt.Errorf("git pull gagal (mungkin ada perubahan lokal); tarik manual lalu ulangi: %w", err)
	}
	newCommit, err := gitOut(src, "rev-parse", "HEAD")
	if err != nil {
		return err
	}

	ver := Version
	if t, err := gitOut(src, "describe", "--tags", "--abbrev=0"); err == nil {
		if v := strings.TrimPrefix(strings.TrimSpace(t), "v"); v != "" {
			ver = v
		}
	}
	date := time.Now().Format("2006-01-02")

	goBin, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("perintah `go` tidak ada di PATH; pasang Go atau build manual: go build -o %s ./cmd/jenderal", exe)
	}
	ld := fmt.Sprintf("-s -w -X '%s.Version=%s' -X '%s.Commit=%s' -X '%s.Date=%s'",
		modulePath+"/internal/cli", ver, modulePath+"/internal/cli", newCommit, modulePath+"/internal/cli", date)

	fmt.Printf("membangun binary baru (versi %s, commit %s) ...\n", ver, shortOf(newCommit))
	tmp := filepath.Join(filepath.Dir(exe), "."+filepath.Base(exe)+".new")
	build := exec.Command(goBin, "build", "-ldflags", ld, "-o", tmp, "./cmd/jenderal")
	build.Dir = src
	var buildOut bytes.Buffer
	build.Stdout, build.Stderr = &buildOut, &buildOut
	if err := build.Run(); err != nil {
		os.Remove(tmp)
		msg := strings.TrimSpace(buildOut.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("go build gagal: %s", msg)
	}

	bak := exe + ".bak"
	if err := copyFile(exe, bak); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("backup binary lama gagal: %w", err)
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("gagal mengganti %s: %w", exe, err)
	}
	fmt.Printf("✔ binary diperbarui: %s → %s\n", shortOf(curCommit), shortOf(newCommit))
	fmt.Printf("  backup binary lama: %s\n  verifikasi dengan: jenderal version\n", bak)
	return nil
}

// detectSourceDir mencari root repo source dari lokasi binary ke atas.
func detectSourceDir(exe string) (string, error) {
	dir := filepath.Dir(exe)
	for {
		if isJenderalSource(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("source repo tidak ditemukan dari lokasi binary %s; bangun binary dari repo, atau tentukan lewat --source", exe)
		}
		dir = parent
	}
}

// isJenderalSource true jika dir adalah repo source jenderal (ada .git dan
// go.mod dengan module yang cocok).
func isJenderalSource(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return false
	}
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	return strings.HasPrefix(string(b), "module "+modulePath)
}

// gitOut menjalankan git di dir dan mengembalikan stdout yang sudah di-trim.
func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return strings.TrimSpace(outBuf.String()), nil
}

// sameCommit membandingkan dua hash commit; hash pendek boleh dicocokkan
// dengan yang panjang lewat prefix.
func sameCommit(a, b string) bool {
	a, b = strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// shortOf memotong hash commit untuk ditampilkan.
func shortOf(c string) string {
	c = strings.TrimSpace(c)
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
