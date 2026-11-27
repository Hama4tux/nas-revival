package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUsbCopyTargetUser(t *testing.T) {
	p := filepath.Join(t.TempDir(), "usbcopy.json")
	f := UsbCopyTargetUser(p)
	if _, ok := f("data"); ok {
		t.Fatal("missing file must not block")
	}
	os.WriteFile(p, []byte(`{"version":1,"share":"data","folder":"usb-backups"}`), 0o644)
	if u, ok := f("DATA"); !ok || u != "kopie tlačítkem COPY" {
		t.Fatalf("got %q %v", u, ok)
	}
	if _, ok := f("other"); ok {
		t.Fatal("other share must not block")
	}
	os.WriteFile(p, []byte(`{"version":1,"share":""}`), 0o644)
	if _, ok := f("data"); ok {
		t.Fatal("cleared setting must not block")
	}
	os.WriteFile(p, []byte(`garbage`), 0o644)
	if _, ok := f("data"); !ok {
		t.Fatal("unreadable setting must block")
	}
}
