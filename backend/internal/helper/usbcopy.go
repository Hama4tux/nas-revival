package helper

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"nas-revival/backend/internal/atomicfile"
	"nas-revival/backend/internal/shares"
	"nas-revival/backend/internal/tasks"
)

// UsbCopyInfo is the result of usb.copy.get and usb.copy.set. Dest is the
// resolved folder the COPY button writes into ("" = not set).
type UsbCopyInfo struct {
	Share  string `json:"share"`
	Folder string `json:"folder"`
	Dest   string `json:"dest"`
}

// UsbCopyManager owns the destination of the COPY button (usbcopy.json and
// usbcopy.dest).
type UsbCopyManager struct {
	Path, DestPath, SharesPath string
	// Volume is where the data volume is mounted and the only volume a
	// destination share may live on (default tasks.BackupVolume).
	Volume string
	// Root is where Volume is mounted (empty = Volume itself; tests).
	Root string
	mu   sync.Mutex
}

// NewUsbCopyManager returns a manager with the production paths.
func NewUsbCopyManager() *UsbCopyManager {
	return &UsbCopyManager{Path: tasks.UsbCopyConfigPath, DestPath: tasks.UsbCopyDestPath,
		SharesPath: shares.DefaultConfigPath, Volume: tasks.BackupVolume, Root: tasks.BackupVolume}
}

func (m *UsbCopyManager) root() string {
	if m.Root != "" {
		return m.Root
	}
	return m.Volume
}

// destDir is the absolute destination folder of a share and subfolder.
func (m *UsbCopyManager) destDir(s shares.Share, folder string) string {
	p := filepath.Join(m.root(), s.Folder)
	if folder != "" {
		p = filepath.Join(p, folder)
	}
	return p
}

// Get returns the saved destination.
func (m *UsbCopyManager) Get(context.Context) (UsbCopyInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, err := tasks.LoadUsbCopy(m.Path)
	if err != nil {
		slog.Error("helper: read usbcopy.json", "err", err)
		return UsbCopyInfo{}, internalErr("Nastavení kopírování tlačítkem COPY nelze načíst.")
	}
	info := UsbCopyInfo{Share: c.Share, Folder: c.Folder}
	if c.Share != "" {
		if cfg, err := shares.Load(m.SharesPath); err == nil {
			if i := cfg.Find(c.Share); i >= 0 {
				info.Dest = m.destDir(cfg.Shares[i], c.Folder)
			}
		}
	}
	return info, nil
}

// Set validates the destination, creates the folder inside the share (it
// inherits the share's default ACL) and writes both files. An empty share
// clears the setting.
func (m *UsbCopyManager) Set(_ context.Context, share, folder, actor string) (UsbCopyInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if share == "" {
		if folder != "" {
			return UsbCopyInfo{}, invalid("share", "Vyberte sdílenou složku.")
		}
		for _, p := range []string{m.DestPath, m.Path} {
			if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
				slog.Error("helper: remove usb copy file", "err", err)
				return UsbCopyInfo{}, internalErr("Nastavení se nepodařilo uložit.")
			}
		}
		slog.Info("helper: usb.copy.set", "share", "", "user", actor)
		return UsbCopyInfo{}, nil
	}
	if folder != "" && !shares.ValidName(folder) {
		return UsbCopyInfo{}, invalid("folder", "Podsložka smí mít jen písmena, číslice, _ a - (nejvýše 32 znaků, jedna úroveň).")
	}
	cfg, err := shares.Load(m.SharesPath)
	if err != nil {
		slog.Error("helper: read shares.json", "err", err)
		return UsbCopyInfo{}, internalErr("Konfiguraci sdílených složek nelze načíst.")
	}
	i := cfg.Find(share)
	if i < 0 {
		return UsbCopyInfo{}, invalid("share", "Sdílená složka neexistuje.")
	}
	s := cfg.Shares[i]
	if !s.BackupTarget || s.Volume != m.Volume || !shares.ValidName(s.Folder) {
		return UsbCopyInfo{}, invalid("share", "Složka nemá volbu Cíl záloh (nebo není na datovém svazku).")
	}
	root := filepath.Join(m.root(), s.Folder)
	if st, err := os.Lstat(root); err != nil || !st.IsDir() {
		return UsbCopyInfo{}, invalid("share", "Složka sdílení na disku chybí.")
	}
	dest := m.destDir(s, folder)
	if dest != root {
		switch st, err := os.Lstat(dest); {
		case err == nil && !st.IsDir():
			return UsbCopyInfo{}, invalid("folder", "Podsložka už existuje a není složka.")
		case err != nil && !errors.Is(err, os.ErrNotExist):
			return UsbCopyInfo{}, internalErr("Podsložku nelze zkontrolovat.")
		case err != nil:
			// The new folder inherits the default ACL of the share.
			if err := os.Mkdir(dest, 0o770); err != nil {
				slog.Error("helper: mkdir usb copy dest", "err", err)
				return UsbCopyInfo{}, internalErr("Podsložku se nepodařilo vytvořit.")
			}
			os.Chmod(dest, 0o770)
		}
	}
	// Name the share as stored, so the in-use hook finds it.
	c := tasks.UsbCopy{Version: 1, Share: s.Name, Folder: folder}
	b, _ := json.MarshalIndent(c, "", "  ")
	if err := atomicfile.Write(m.DestPath, []byte(dest+"\n"), 0o644); err != nil {
		slog.Error("helper: write usbcopy.dest", "err", err)
		return UsbCopyInfo{}, internalErr("Nastavení se nepodařilo uložit.")
	}
	if err := atomicfile.Write(m.Path, append(b, '\n'), 0o644); err != nil {
		slog.Error("helper: write usbcopy.json", "err", err)
		return UsbCopyInfo{}, internalErr("Nastavení se nepodařilo uložit.")
	}
	slog.Info("helper: usb.copy.set", "share", s.Name, "folder", folder, "user", actor)
	return UsbCopyInfo{Share: s.Name, Folder: folder, Dest: dest}, nil
}
