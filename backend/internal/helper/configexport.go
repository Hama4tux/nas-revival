package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"regexp"
	"time"
	"unicode/utf8"

	"nas-revival/backend/internal/config"
	"nas-revival/backend/internal/diskpower"
	"nas-revival/backend/internal/firewall"
	"nas-revival/backend/internal/nfs"
	"nas-revival/backend/internal/shares"
	"nas-revival/backend/internal/smb"
	"nas-revival/backend/internal/sshd"
)

// Config export file (CFG-05, ADR 0011): the managed files of the config
// history as JSON. It holds no secrets: passwords, keys, the TLS certificate
// and the backup credentials are not managed files.
const (
	ArchiveFormat = "nas-revival-config"
	// ArchiveSchema is the newest schema this build writes and reads; older
	// schemas are migrated in migrateArchive.
	ArchiveSchema = 1
	// MaxArchiveFile caps one file in an import; the managed files are a few KiB.
	MaxArchiveFile = 32 << 10
	// MaxArchive caps the whole export file (API body and helper request).
	MaxArchive = 64 << 10
)

// ConfigArchive is the export file.
type ConfigArchive struct {
	Format   string        `json:"format"`
	Schema   int           `json:"schema"`
	Created  time.Time     `json:"created"`
	Hostname string        `json:"hostname"`
	Files    []ArchiveFile `json:"files"`
}

// ArchiveFile is one managed file; Present=false means the file does not
// exist (built-in defaults).
type ArchiveFile struct {
	ID      string `json:"id"`
	Present bool   `json:"present"`
	Content string `json:"content,omitempty"`
}

// ConfigImportPreview is the result of config.importPreview.
type ConfigImportPreview struct {
	Created  time.Time `json:"created"`
	Hostname string    `json:"hostname"`
	// OtherHost: the file comes from a NAS with another name.
	OtherHost bool `json:"otherHost"`
	config.ImportDetail
}

// ConfigImportResult is the result of config.import.
type ConfigImportResult struct {
	Restored        []string         `json:"restored"`
	NetworkPending  *Pending         `json:"networkPending"`
	FirewallPending *FirewallPending `json:"firewallPending"`
}

func (c *ConfigManager) hostname() string {
	if c.Hostname != nil {
		return c.Hostname()
	}
	h, _ := os.Hostname()
	return h
}

func (c *ConfigManager) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

// Export returns the export file of the current configuration.
func (c *ConfigManager) Export(user string) (ConfigArchive, error) {
	files, err := c.Store.Export()
	if err != nil {
		return ConfigArchive{}, configErr(err)
	}
	a := ConfigArchive{Format: ArchiveFormat, Schema: ArchiveSchema, Created: c.now(), Hostname: c.hostname(), Files: []ArchiveFile{}}
	for _, f := range files {
		a.Files = append(a.Files, ArchiveFile{ID: f.ID, Present: f.Present, Content: string(f.Data)})
	}
	slog.Info("helper: config.export", "user", user)
	return a, nil
}

var archiveHostRe = regexp.MustCompile(`^[A-Za-z0-9.-]{0,253}$`)

func badArchive(msg string) error { return invalid("archive", msg) }

// migrateArchive turns an older schema into ArchiveSchema. Schema 1 is the
// first one; a future schema adds its step here.
func migrateArchive(a *ConfigArchive) error {
	switch {
	case a.Schema > ArchiveSchema:
		return badArchive("Soubor vytvořila novější verze NAS Revival; nejdřív aktualizujte tento NAS.")
	case a.Schema < 1:
		return badArchive("Soubor nemá platnou verzi formátu.")
	}
	return nil
}

// parseArchive decodes and checks an export file and returns its files in the
// canonical form the web UI itself writes.
func parseArchive(raw []byte) (ConfigArchive, []config.Incoming, error) {
	var a ConfigArchive
	if len(raw) > MaxArchive {
		return a, nil, badArchive("Soubor je příliš velký.")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil || a.Format != ArchiveFormat {
		return a, nil, badArchive("Soubor není export konfigurace NAS Revival.")
	}
	if err := migrateArchive(&a); err != nil {
		return a, nil, err
	}
	if !archiveHostRe.MatchString(a.Hostname) {
		a.Hostname = ""
	}
	seen := map[string]bool{}
	in := []config.Incoming{}
	for _, f := range a.Files {
		label, known := archiveLabels[f.ID]
		if !known {
			return a, nil, badArchive("Soubor obsahuje neznámou část konfigurace.")
		}
		if seen[f.ID] {
			return a, nil, badArchive("Část konfigurace " + label + " je v souboru dvakrát.")
		}
		seen[f.ID] = true
		if len(f.Content) > MaxArchiveFile || !utf8.ValidString(f.Content) || (!f.Present && f.Content != "") {
			return a, nil, badArchive("Část konfigurace " + label + " není platná.")
		}
		inc := config.Incoming{ID: f.ID, Present: f.Present}
		if f.Present {
			data, present, err := canonical(f.ID, []byte(f.Content))
			if err != nil {
				return a, nil, badArchive("Část konfigurace " + label + " není platná.")
			}
			inc.Present, inc.Data = present, data
		}
		in = append(in, inc)
	}
	if len(in) == 0 {
		return a, nil, badArchive("Soubor neobsahuje žádnou konfiguraci.")
	}
	return a, in, nil
}

// archiveLabels names the managed files in import errors.
var archiveLabels = map[string]string{
	FileNetwork: "Síť", FileTime: "Čas (NTP)", FileDiskPower: "Napájení disků", FileShares: "Sdílené složky",
	FileSmb: "SMB", FileNfs: "NFS", FileSsh: "SSH", FileFirewall: "Firewall",
}

// canonical validates one incoming file and renders it again, so an import
// writes only what the settings pages could write (no foreign directives in
// the network or NTP file, no unknown JSON fields). present=false: the file
// stands for the built-in defaults.
func canonical(id string, b []byte) ([]byte, bool, error) {
	switch id {
	case FileNetwork:
		c, err := ValidateNetConfig(ParseNetwork(string(b)))
		if err != nil {
			return nil, false, err
		}
		return []byte(RenderNetwork(c)), true, nil
	case FileTime:
		servers, err := ValidateNTP(ParseTimesyncd(string(b)))
		if err != nil || len(servers) == 0 {
			return nil, false, err
		}
		return []byte(RenderTimesyncd(servers)), true, nil
	case FileDiskPower:
		var c diskpower.Config
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil || c.Version != 1 {
			return nil, false, badArchive("")
		}
		for id, s := range c.Disks {
			if !diskIDRe.MatchString(id) || diskpower.Validate(s) != nil {
				return nil, false, badArchive("")
			}
		}
		out, err := diskpower.Encode(c)
		return out, true, err
	case FileShares:
		c, err := shares.Parse(b)
		if err != nil {
			return nil, false, err
		}
		out, err := shares.Encode(c)
		return out, true, err
	case FileSmb:
		s, err := smb.Parse(b)
		if err != nil {
			return nil, false, err
		}
		out, err := smb.Encode(s)
		return out, true, err
	case FileNfs:
		s, err := nfs.Parse(b)
		if err != nil {
			return nil, false, err
		}
		out, err := nfs.Encode(s)
		return out, true, err
	case FileSsh:
		s, err := sshd.Parse(b)
		if err != nil {
			return nil, false, err
		}
		out, err := sshd.Encode(s)
		return out, true, err
	case FileFirewall:
		s, err := firewall.Parse(b)
		if err != nil {
			return nil, false, badArchive("")
		}
		out, err := firewall.Encode(s)
		return out, true, err
	}
	return nil, false, badArchive("")
}

// ImportPreview shows what an import would change; nothing is written.
func (c *ConfigManager) ImportPreview(raw []byte) (ConfigImportPreview, error) {
	a, in, err := parseArchive(raw)
	if err != nil {
		return ConfigImportPreview{}, err
	}
	d, err := c.Store.Compare(in)
	if err != nil {
		return ConfigImportPreview{}, configErr(err)
	}
	return ConfigImportPreview{Created: a.Created, Hostname: a.Hostname,
		OtherHost: a.Hostname != "" && a.Hostname != c.hostname(), ImportDetail: d}, nil
}

// Import writes the files of an export like a rollback: drift needs takeover,
// the network file goes last through the confirm-or-rollback window, and the
// result is a new version of kind "import" (the version before it is the undo).
func (c *ConfigManager) Import(ctx context.Context, raw []byte, takeover bool, user, clientAddr string) (ConfigImportResult, error) {
	_, in, err := parseArchive(raw)
	if err != nil {
		return ConfigImportResult{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.pendingErr(); err != nil {
		return ConfigImportResult{}, err
	}
	plan, err := c.Store.PlanImport(in, user, takeover)
	if err != nil {
		return ConfigImportResult{}, configErr(err)
	}
	res, err := c.apply(ctx, plan, user, clientAddr, config.KindImport, 0)
	slog.Info("helper: config.import", "restored", res.Restored, "user", user, "ok", err == nil)
	return ConfigImportResult{Restored: res.Restored, NetworkPending: res.NetworkPending, FirewallPending: res.FirewallPending}, err
}
