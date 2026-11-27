package tasks

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// Files of the COPY button destination (ADR 0010). usbcopy.json is the
// setting; usbcopy.dest is the resolved absolute folder as one line, which the
// shell script nsa310-copy reads (it cannot parse JSON or shares.json).
const (
	UsbCopyConfigPath = "/etc/nas-revival/usbcopy.json"
	UsbCopyDestPath   = "/etc/nas-revival/usbcopy.dest"
)

// UsbCopy is usbcopy.json. An empty Share means no destination is set.
type UsbCopy struct {
	Version int    `json:"version"`
	Share   string `json:"share"`
	Folder  string `json:"folder,omitempty"`
}

// LoadUsbCopy reads usbcopy.json; a missing file means "not set".
func LoadUsbCopy(path string) (UsbCopy, error) {
	c := UsbCopy{Version: 1}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil || c.Version != 1 {
		return UsbCopy{Version: 1}, errors.New("invalid usbcopy.json")
	}
	return c, nil
}

// UsbCopyTargetUser is the share manager hook (ShareManager.
// InUseAsBackupTarget) for the COPY button: it names the button when the
// share is its destination. An unreadable file counts as "in use".
func UsbCopyTargetUser(path string) func(share string) (string, bool) {
	return func(share string) (string, bool) {
		c, err := LoadUsbCopy(path)
		if err != nil {
			return "kopie tlačítkem COPY (nastavení nelze přečíst)", true
		}
		if c.Share != "" && strings.EqualFold(c.Share, share) {
			return "kopie tlačítkem COPY", true
		}
		return "", false
	}
}
