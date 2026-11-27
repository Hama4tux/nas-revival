package helper

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"nas-revival/backend/internal/config"
)

// Managed file IDs (the registry of config history, CFG-01..04).
const (
	FileNetwork   = "network"
	FileTime      = "time"
	FileDiskPower = "disk-power"
	FileShares    = "shares"
	FileSmb       = "smb"
	FileNfs       = "nfs"
	FileSsh       = "ssh"
)

// ConfigManager serves config.list, config.get and config.rollback and
// records a version after every successful settings change.
type ConfigManager struct {
	Store *config.Store
	Net   *Manager
	Time  *TimeManager
	Power *DiskPowerManager
	// Shares restores shares.json; nil when shares are disabled.
	Shares *ShareManager
	// Smb restores smb.json; nil when SMB is disabled.
	Smb *SmbManager
	// Nfs restores nfs.json; nil when NFS is disabled.
	Nfs *NfsManager
	// Ssh restores ssh.json; nil when SSH settings are disabled.
	Ssh *SshManager
	// Hostname and Now stamp the export file; nil uses the system.
	Hostname func() string
	Now      func() time.Time
	mu       sync.Mutex // one rollback or import at a time
}

// ConfigRollbackResult is the result of config.rollback.
type ConfigRollbackResult struct {
	RollbackOf int      `json:"rollbackOf"`
	Restored   []string `json:"restored"`
	// NetworkPending: the network file was restored and waits for
	// confirmation from the new address like any network change.
	NetworkPending *Pending `json:"networkPending"`
}

func configErr(err error) error {
	switch {
	case errors.Is(err, config.ErrNotFound):
		return &Error{Code: CodeNotFound, Message: "Tato verze konfigurace v historii není."}
	case errors.Is(err, config.ErrDrift):
		return &Error{Code: CodeConfigDrift, Message: "Soubory byly mezitím změněny mimo webové rozhraní. Potvrďte, že je chcete přepsat."}
	case errors.Is(err, config.ErrTooLarge):
		return internalErr("Spravovaný soubor je nečekaně velký.")
	}
	var he *Error
	if errors.As(err, &he) {
		return he
	}
	slog.Error("helper: config", "err", err)
	return internalErr("Historii konfigurace nelze načíst.")
}

// Prepare records changes made outside the web UI before a settings write.
// A failing history must not block the change itself; it is logged.
func (c *ConfigManager) Prepare() {
	if c == nil {
		return
	}
	if err := c.Store.Prepare(); err != nil {
		slog.Error("helper: config history before change failed", "err", err)
	}
}

// Changed records the new version after a successful settings write.
func (c *ConfigManager) Changed(user, module string) {
	if c == nil {
		return
	}
	if _, _, err := c.Store.Record(user, config.KindChange, module, 0); err != nil {
		slog.Error("helper: config history after change failed", "module", module, "err", err)
	}
}

func (c *ConfigManager) List() (config.History, error) {
	h, err := c.Store.List()
	if err != nil {
		return h, configErr(err)
	}
	return h, nil
}

func (c *ConfigManager) Get(id int) (config.Detail, error) {
	d, err := c.Store.Get(id)
	if err != nil {
		return d, configErr(err)
	}
	return d, nil
}

var errNetPending = &Error{Code: CodeChangePending, Message: "Změna sítě čeká na potvrzení; nejdřív ji potvrďte nebo nechte vrátit."}

// Rollback writes version id back. Time and disk power are applied first; the
// network file goes last and through the confirm-or-rollback window, so a
// version with an unreachable address reverts by itself.
func (c *ConfigManager) Rollback(ctx context.Context, id int, takeover bool, user string) (ConfigRollbackResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Net.HasPending() {
		return ConfigRollbackResult{}, errNetPending
	}
	plan, err := c.Store.Plan(id, user, takeover)
	if err != nil {
		return ConfigRollbackResult{}, configErr(err)
	}
	restored, pending, err := c.apply(ctx, plan, user, config.KindRollback, id)
	if err == nil {
		slog.Info("helper: config.rollback", "version", id, "restored", restored, "user", user)
	}
	return ConfigRollbackResult{RollbackOf: id, Restored: restored, NetworkPending: pending}, err
}

// apply writes the planned files through their owners, the network file
// last. Whatever was written is recorded as one version of kind, also when a
// later file fails.
func (c *ConfigManager) apply(ctx context.Context, plan []config.Restore, user, kind string, rollbackOf int) (restored []string, pending *Pending, err error) {
	restored = []string{}
	defer func() {
		if len(restored) == 0 {
			return
		}
		if _, _, rerr := c.Store.Record(user, kind, "", rollbackOf); rerr != nil {
			slog.Error("helper: config history after "+kind+" failed", "err", rerr)
		}
	}()
	var network *config.Restore
	for i := range plan {
		r := plan[i]
		switch r.ID {
		case FileNetwork:
			network = &plan[i]
			continue
		case FileTime:
			_, err = c.Time.Restore(ctx, r.Data, r.Present)
		case FileDiskPower:
			err = c.Power.Restore(ctx, r.Data, r.Present)
		case FileShares:
			if c.Shares == nil {
				err = internalErr("Sdílené složky nejsou dostupné.")
			} else {
				err = c.Shares.Restore(ctx, r.Data, r.Present)
			}
		case FileSmb:
			if c.Smb == nil {
				err = internalErr("Nastavení SMB není dostupné.")
			} else {
				err = c.Smb.Restore(ctx, r.Data, r.Present)
			}
		case FileNfs:
			if c.Nfs == nil {
				err = internalErr("Nastavení NFS není dostupné.")
			} else {
				err = c.Nfs.Restore(ctx, r.Data, r.Present)
			}
		case FileSsh:
			if c.Ssh == nil {
				err = internalErr("Nastavení SSH není dostupné.")
			} else {
				err = c.Ssh.Restore(ctx, r.Data, r.Present)
			}
		default:
			err = internalErr("Neznámý spravovaný soubor.")
		}
		if err != nil {
			return restored, nil, err
		}
		restored = append(restored, r.ID)
	}
	if network != nil {
		st, err := c.Net.Restore(ctx, network.Data, network.Present)
		if err != nil {
			return restored, nil, err
		}
		restored = append(restored, FileNetwork)
		pending = st.Pending
	}
	return restored, pending, nil
}

var userRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// cleanUser keeps only well-formed user names for the history.
func cleanUser(u string) string {
	if userRe.MatchString(u) {
		return u
	}
	return "unknown"
}
