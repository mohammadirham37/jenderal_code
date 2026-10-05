package permission

import "testing"

func TestBashPatterns(t *testing.T) {
	r := New(map[string]any{
		"bash": map[string]any{
			"git status": "allow",
			"git *":      "allow",
			"rm *":       "deny",
			"*":          "ask",
		},
	}, "/proj", false)

	cases := []struct {
		cmd  string
		want Level
	}{
		{"git status", Allow},
		{"git status -sb", Allow}, // pola persis menang atas "git *"
		{"git push origin main", Allow},
		{"rm -rf /", Deny},
		{"ls -la", Ask},
		{"echo hello", Ask},
	}
	for _, c := range cases {
		got := r.Check("bash", c.cmd, Ask)
		if got.Level != c.want {
			t.Errorf("bash %q: ingin %s dapat %s", c.cmd, c.want, got.Level)
		}
	}
}

func TestSessionApproval(t *testing.T) {
	r := New(nil, "/proj", false)
	if d := r.Check("write", "/proj/a.txt", Ask); d.Level != Ask {
		t.Fatalf("default harus ask; dapat %s", d.Level)
	}
	r.ApproveSession("write", "/proj/a.txt")
	if d := r.Check("write", "/proj/a.txt", Ask); d.Level != Allow {
		t.Fatal("sesi harus diizinkan setelah ApproveSession")
	}
	// Target berbeda tetap ditanya.
	if d := r.Check("write", "/proj/b.txt", Ask); d.Level != Ask {
		t.Fatal("target lain tidak boleh ikut diizinkan")
	}
}

func TestYolo(t *testing.T) {
	r := New(map[string]any{"bash": "deny"}, "/proj", true)
	if d := r.Check("bash", "anything", Deny); d.Level != Allow {
		t.Fatal("yolo harus mengizinkan semua")
	}
	r.SetYolo(false)
	if d := r.Check("bash", "anything", Deny); d.Level != Deny {
		t.Fatal("setelah yolo off harus deny")
	}
}

func TestOutsideProjectAlwaysAsk(t *testing.T) {
	r := New(map[string]any{"write": "allow"}, "/proj", false)
	if d := r.Check("write", "/etc/passwd", Allow); d.Level != Ask {
		t.Fatalf("akses luar proyek harus ask; dapat %s", d.Level)
	}
	if d := r.Check("write", "/proj/main.go", Allow); d.Level != Allow {
		t.Fatal("akses dalam proyek harus tetap allow")
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"*", "hello", true},
		{"*", "hello world", true},
		{"git *", "git status", true},
		{"git *", "gitx status", false},
		{"rm -?", "rm -f", true},
		{"*.go", "main.go", true},
		{"*test*", "unit_test.go", true},
	}
	for _, c := range cases {
		if got := GlobMatch(c.pattern, c.s); got != c.want {
			t.Errorf("GlobMatch(%q, %q) = %v; ingin %v", c.pattern, c.s, got, c.want)
		}
	}
}
