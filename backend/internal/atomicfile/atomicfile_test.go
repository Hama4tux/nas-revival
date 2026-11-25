package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	for _, s := range []string{"one", "two"} {
		if err := Write(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(p)
		if string(b) != s {
			t.Fatalf("got %q", b)
		}
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	ents, _ := os.ReadDir(filepath.Dir(p))
	if len(ents) != 1 {
		t.Fatalf("temp files left: %v", ents)
	}
	if err := Write(filepath.Join(p, "x", "y"), nil, 0o600); err == nil {
		t.Fatal("expected error")
	}
}
