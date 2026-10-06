package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mohammadirham37/jenderal_code/internal/config"
)

const (
	// modulePath dipakai untuk mengenali root repo source dan ldflags.
	modulePath = "github.com/mohammadirham37/jenderal_code"
	// ghRepo repo GitHub untuk jalur pembaruan rilis.
	ghRepo = "mohammadirham37/jenderal_code"
)

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
		return fmt.Errorf("binary berjalan dari cache `go run`; bangun dulu dengan `go build -o jenderalcode ./cmd/jenderalcode`, lalu jalankan `jenderalcode update`")
	}

	src := source
	if src == "" {
		// 1) Deteksi dari lokasi binary (binary di dalam repo).
		src, err = detectSourceDir(exe)
		if err != nil {
			// 2) Lokasi yang diingat dari update sebelumnya.
			src = loadSourceHint()
		}
		if src == "" {
			// 3) Lokasi baku yang umum untuk clone repo.
			src = findKnownSourceDir()
		}
		if src == "" {
			// 4) Tanpa source repo (binary installer/go install):
			//    perbarui langsung dari GitHub Releases.
			return releaseUpdate(exe, checkOnly, force)
		}
	} else {
		if src, err = filepath.Abs(src); err != nil {
			return err
		}
		if !isJenderalSource(src) {
			return fmt.Errorf("%s bukan source repo %s (butuh .git dan go.mod yang cocok)", src, modulePath)
		}
	}
	// Ingat lokasi repo agar update berikutnya tidak butuh --source.
	saveSourceHint(src)

	// Commit binary saat ini: ldflags → build info (vcs.revision / versi
	// pseudo go install) → HEAD source.
	curCommit := Commit
	if curCommit == "" || curCommit == "dev" {
		curCommit = binaryCommit()
	}
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
		fmt.Println("pembaruan tersedia; jalankan `jenderalcode update` untuk memasang.")
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
		return fmt.Errorf("perintah `go` tidak ada di PATH; pasang Go atau build manual: go build -o %s ./cmd/jenderalcode", exe)
	}
	ld := fmt.Sprintf("-s -w -X '%s.Version=%s' -X '%s.Commit=%s' -X '%s.Date=%s'",
		modulePath+"/internal/cli", ver, modulePath+"/internal/cli", newCommit, modulePath+"/internal/cli", date)

	fmt.Printf("membangun binary baru (versi %s, commit %s) ...\n", ver, shortOf(newCommit))
	tmp := filepath.Join(filepath.Dir(exe), "."+filepath.Base(exe)+".new")
	build := exec.Command(goBin, "build", "-ldflags", ld, "-o", tmp, "./cmd/jenderalcode")
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
	fmt.Printf("  backup binary lama: %s\n  verifikasi dengan: jenderalcode version\n", bak)
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

// sourceHintPath file yang menyimpan lokasi repo dari update sebelumnya,
// sehingga `jenderalcode update` bekerja dari direktori mana pun.
func sourceHintPath() string {
	return filepath.Join(config.DataDir(), "source-path")
}

// knownSourceDirs lokasi baku tempat repo biasa di-clone.
func knownSourceDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	rel := []string{
		"jenderal_code",
		"Documents/jenderal_code",
		"Documents/go_project/jenderal_code",
		"Projects/jenderal_code",
		"projects/jenderal_code",
		"dev/jenderal_code",
		"code/jenderal_code",
		filepath.Join("go", "src", "github.com", "mohammadirham37", "jenderal_code"),
	}
	out := make([]string, 0, len(rel))
	for _, r := range rel {
		out = append(out, filepath.Join(home, r))
	}
	return out
}

// findKnownSourceDir mencari repo di lokasi baku; kosong bila tak ketemu.
func findKnownSourceDir() string {
	for _, dir := range knownSourceDirs() {
		if isJenderalSource(dir) {
			return dir
		}
	}
	return ""
}

// loadSourceHint membaca lokasi repo tersimpan; kosong bila tidak valid.
func loadSourceHint() string {
	b, err := os.ReadFile(sourceHintPath())
	if err != nil {
		return ""
	}
	dir := strings.TrimSpace(string(b))
	if !isJenderalSource(dir) {
		return ""
	}
	return dir
}

// saveSourceHint menyimpan lokasi repo (best-effort).
func saveSourceHint(dir string) {
	if !isJenderalSource(dir) {
		return
	}
	_ = config.EnsureDirs()
	_ = os.WriteFile(sourceHintPath(), []byte(dir), 0o644)
}

// binaryCommit membaca commit binary yang sedang berjalan dari build info:
// vcs.revision untuk build dari checkout git, atau hash di ujung
// pseudo-version untuk `go install ...@latest`.
func binaryCommit() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			return s.Value
		}
	}
	return commitFromVersion(bi.Main.Version)
}

// commitFromVersion mengekstrak commit hash (atau tag polos) dari nomor
// versi modul, mis. "v0.0.0-20261006120000-abc123def456" → "abc123def456".
func commitFromVersion(v string) string {
	if v == "" || v == "(devel)" {
		return ""
	}
	if i := strings.LastIndexByte(v, '-'); i >= 0 {
		h := v[i+1:]
		if isHexHash(h) {
			return h
		}
	}
	// Tag polos tanpa metadata (v0.1.0): dipakai sebagai identitas agar
	// perbandingan tetap jalan (tag ≠ HEAD → update tersedia).
	if strings.HasPrefix(v, "v") && !strings.Contains(v, "-") {
		return v
	}
	return ""
}

// isHexHash true bila s panjang 7–40 dan semuanya hex.
func isHexHash(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// isJenderalSource true jika dir adalah repo source jenderalcode (ada .git dan
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

// ---- jalur rilis GitHub (binary tanpa source repo) ----

// releaseUpdate memperbarui binary langsung dari GitHub Releases: unduh
// tarball sesuai OS/arsitektur, verifikasi SHA256, lalu mengganti binary
// (dan alias jc di folder yang sama) secara atomik.
func releaseUpdate(exe string, checkOnly, force bool) error {
	fmt.Println("source repo tidak ditemukan — memakai jalur rilis GitHub.")
	client := &http.Client{Timeout: 30 * time.Second}
	downloader := &http.Client{Timeout: 15 * time.Minute}

	tag, err := latestReleaseTag(client)
	if err != nil {
		return err
	}
	ver := strings.TrimPrefix(tag, "v")
	fmt.Printf("versi terpasang : %s\n", versionString())
	fmt.Printf("rilis terbaru   : %s\n", tag)

	if !force && ver == Version {
		fmt.Println("✔ binary sudah dari rilis terbaru")
		return nil
	}
	if checkOnly {
		fmt.Println("pembaruan tersedia; jalankan `jenderalcode update` untuk memasang.")
		return nil
	}

	name := fmt.Sprintf("jenderalcode_%s_%s_%s.tar.gz", ver, runtime.GOOS, runtime.GOARCH)
	base := fmt.Sprintf("https://github.com/%s/releases/download/%s", ghRepo, tag)
	tmp, err := os.MkdirTemp("", "jenderalcode-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	fmt.Printf("mengunduh %s ...\n", name)
	if err := downloadFile(downloader, base+"/"+name, filepath.Join(tmp, name)); err != nil {
		return fmt.Errorf("unduhan gagal: %w", err)
	}
	if err := downloadFile(client, base+"/SHA256SUMS", filepath.Join(tmp, "SHA256SUMS")); err != nil {
		return fmt.Errorf("checksum tidak bisa diunduh: %w", err)
	}
	if err := verifySHA256(filepath.Join(tmp, name), filepath.Join(tmp, "SHA256SUMS")); err != nil {
		return fmt.Errorf("checksum tidak cocok: %w", err)
	}

	// Ekstrak jenderalcode & jc, ganti binary atomik (rename dari file .new).
	f, err := os.Open(filepath.Join(tmp, name))
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	want := map[string]string{"jenderalcode": exe, "jc": filepath.Join(filepath.Dir(exe), "jc")}
	tr := tar.NewReader(gz)
	replaced := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		dest, ok := want[filepath.Base(hdr.Name)]
		if !ok || hdr.Typeflag != tar.TypeReg {
			continue
		}
		newPath := dest + ".new"
		out, err := os.OpenFile(newPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		out.Close()
		if err := os.Rename(newPath, dest); err != nil {
			return fmt.Errorf("gagal mengganti %s: %w", dest, err)
		}
		replaced++
	}
	if replaced == 0 {
		return fmt.Errorf("tarball tidak memuat binary yang diharapkan")
	}
	fmt.Printf("✔ binary diperbarui ke %s (%d file)\n  verifikasi dengan: jenderalcode version\n", tag, replaced)
	return nil
}

// latestReleaseTag mengambil tag rilis terbaru dari GitHub API.
func latestReleaseTag(client *http.Client) (string, error) {
	resp, err := client.Get("https://api.github.com/repos/" + ghRepo + "/releases/latest")
	if err != nil {
		return "", fmt.Errorf("tidak bisa menghubungi GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API status %d", resp.StatusCode)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	if rel.TagName == "" {
		return "", fmt.Errorf("belum ada rilis di GitHub")
	}
	return rel.TagName, nil
}

func downloadFile(client *http.Client, url, dest string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d untuk %s", resp.StatusCode, url)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

// verifySHA256 membandingkan sha256 file dengan entri di file SHA256SUMS.
func verifySHA256(path, sumsPath string) error {
	b, err := os.ReadFile(sumsPath)
	if err != nil {
		return err
	}
	want := ""
	name := filepath.Base(path)
	for _, ln := range strings.Split(string(b), "\n") {
		fields := strings.Fields(ln)
		if len(fields) == 2 && fields[1] == name {
			want = fields[0]
		}
	}
	if want == "" {
		return fmt.Errorf("entri %s tidak ada di SHA256SUMS", name)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("%s ≠ %s", got, want)
	}
	return nil
}
