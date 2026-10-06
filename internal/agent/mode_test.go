package agent

import (
	"testing"

	"github.com/mohammadirham37/jenderal_code/internal/permission"
)

func TestModeSiklusDanFullAccess(t *testing.T) {
	ag := newSkillsTestAgent(t)
	if ag.Mode != ModeBuild {
		t.Fatalf("mode awal %q", ag.Mode)
	}
	ag.ToggleMode() // plan
	if ag.Mode != ModePlan {
		t.Errorf("siklus 1 = %q, want plan", ag.Mode)
	}
	ag.ToggleMode() // full
	if ag.Mode != ModeFull {
		t.Errorf("siklus 2 = %q, want full", ag.Mode)
	}
	dec := ag.Perm.Check("write", "index.html", permission.Ask)
	if !dec.Allowed() {
		t.Errorf("mode full harus auto-allow, dapat %v (%s)", dec.Level, dec.Reason)
	}
	ag.ToggleMode() // kembali build
	if ag.Mode != ModeBuild {
		t.Errorf("siklus 3 = %q, want build", ag.Mode)
	}
	if dec := ag.Perm.Check("write", "index.html", permission.Ask); dec.Allowed() {
		t.Error("mode build tidak boleh auto-allow")
	}
}
