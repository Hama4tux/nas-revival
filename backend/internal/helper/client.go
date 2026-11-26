package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"syscall"
	"time"

	"nas-revival/backend/internal/config"
	"nas-revival/backend/internal/diskactivity"
	"nas-revival/backend/internal/diskpower"
	"nas-revival/backend/internal/nfs"
	"nas-revival/backend/internal/shares"
	"nas-revival/backend/internal/smb"
	"nas-revival/backend/internal/sshd"
	"nas-revival/backend/internal/tasks"
)

type userKey struct{}

// WithUser attaches the web session user; the client sends it with every
// request so the helper can record who changed the configuration.
func WithUser(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, userKey{}, user)
}

// Client talks to the helper from the web process.
type Client struct {
	Socket  string
	Timeout time.Duration // default 10 s
}

type clientResponse struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
}

func unavailable() *Error {
	return &Error{Code: CodeHelperUnavailable, Message: "Služba pro změnu nastavení není dostupná."}
}

// Call performs one request. Transport problems become helper_unavailable.
func (c *Client) Call(ctx context.Context, op string, args, result any) error {
	to := c.Timeout
	if to == 0 {
		to = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	var dl net.Dialer
	conn, err := dl.DialContext(ctx, "unix", c.Socket)
	if err != nil {
		return unavailable()
	}
	defer conn.Close()
	if t, ok := ctx.Deadline(); ok {
		conn.SetDeadline(t)
	}
	req := map[string]any{"op": op, "args": args}
	if u, ok := ctx.Value(userKey{}).(string); ok {
		req["user"] = u
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if _, err := conn.Write(body); err != nil {
		return unavailable()
	}
	var resp clientResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return unavailable()
	}
	if !resp.OK {
		if resp.Error == nil {
			return unavailable()
		}
		return resp.Error
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(resp.Result, result)
}

func (c *Client) NetworkGet(ctx context.Context) (NetworkState, error) {
	var s NetworkState
	err := c.Call(ctx, "network.get", struct{}{}, &s)
	return s, err
}

func (c *Client) NetworkApply(ctx context.Context, cfg NetConfig) (NetworkState, error) {
	var s NetworkState
	err := c.Call(ctx, "network.apply", cfg, &s)
	return s, err
}

func (c *Client) NetworkConfirm(ctx context.Context) (NetworkState, error) {
	var s NetworkState
	err := c.Call(ctx, "network.confirm", struct{}{}, &s)
	return s, err
}

func (c *Client) TimeGet(ctx context.Context) (TimeState, error) {
	var s TimeState
	err := c.Call(ctx, "time.get", struct{}{}, &s)
	return s, err
}

func (c *Client) TimeApply(ctx context.Context, servers []string) (TimeState, error) {
	var s TimeState
	err := c.Call(ctx, "time.apply", map[string]any{"ntpServers": servers}, &s)
	return s, err
}

// SmartTest starts a self-test. Waking a sleeping disk takes several seconds,
// so the call gets more time than the settings operations.
func (c *Client) SmartTest(ctx context.Context, diskID, typ string) (SmartTestResult, error) {
	var r SmartTestResult
	long := *c
	long.Timeout = 45 * time.Second
	err := long.Call(ctx, "smart.test", map[string]string{"diskId": diskID, "type": typ}, &r)
	return r, err
}

// DiskSetPower stores and applies standby timeout and APM. A sleeping disk
// wakes up for hdparm, hence the longer timeout.
func (c *Client) DiskSetPower(ctx context.Context, diskID string, s diskpower.Settings) (DiskPowerResult, error) {
	var r DiskPowerResult
	long := *c
	long.Timeout = 45 * time.Second
	err := long.Call(ctx, "disk.setPower", map[string]any{"diskId": diskID, "settings": s}, &r)
	return r, err
}

// DiskStandby spins the disk down now.
func (c *Client) DiskStandby(ctx context.Context, diskID string) (DiskStandbyResult, error) {
	var r DiskStandbyResult
	long := *c
	long.Timeout = 45 * time.Second
	err := long.Call(ctx, "disk.standby", map[string]string{"diskId": diskID}, &r)
	return r, err
}

// DiskActivity measures disk activity for seconds; the call waits for it.
func (c *Client) DiskActivity(ctx context.Context, seconds int) (diskactivity.Result, error) {
	var r diskactivity.Result
	long := *c
	long.Timeout = time.Duration(seconds)*time.Second + 20*time.Second
	err := long.Call(ctx, "disk.activity", map[string]int{"seconds": seconds}, &r)
	return r, err
}

// PowerPreflight lists what a restart or shutdown would interrupt.
func (c *Client) PowerPreflight(ctx context.Context) (PowerPreflight, error) {
	var r PowerPreflight
	long := *c
	long.Timeout = 45 * time.Second
	err := long.Call(ctx, "power.preflight", struct{}{}, &r)
	return r, err
}

// PowerAction restarts ("reboot") or shuts down ("shutdown") the NAS.
func (c *Client) PowerAction(ctx context.Context, action string, force bool, user string) (PowerActionResult, error) {
	var r PowerActionResult
	long := *c
	long.Timeout = 45 * time.Second
	err := long.Call(ctx, "power."+action, map[string]any{"force": force, "user": user}, &r)
	return r, err
}

// ConfigHistory lists the versions of the managed files and their drift.
func (c *Client) ConfigHistory(ctx context.Context) (config.History, error) {
	var h config.History
	err := c.Call(ctx, "config.list", struct{}{}, &h)
	return h, err
}

// ConfigVersion returns one version with the differences to the current files.
func (c *Client) ConfigVersion(ctx context.Context, id int) (config.Detail, error) {
	var d config.Detail
	err := c.Call(ctx, "config.get", map[string]int{"id": id}, &d)
	return d, err
}

// ConfigRollback writes version id back. Disk power re-applies hdparm to
// every disk, hence the longer timeout.
func (c *Client) ConfigRollback(ctx context.Context, id int, takeover bool) (ConfigRollbackResult, error) {
	var r ConfigRollbackResult
	long := *c
	long.Timeout = 45 * time.Second
	err := long.Call(ctx, "config.rollback", map[string]any{"id": id, "takeover": takeover}, &r)
	return r, err
}

// Account operations (USR-01..03). Hashing argon2id and running the shadow
// tools takes a few seconds on the NAS, hence the longer timeout.
func (c *Client) users(ctx context.Context, op string, args any, result any) error {
	long := *c
	long.Timeout = 45 * time.Second
	return long.Call(ctx, op, args, result)
}

func (c *Client) UserList(ctx context.Context) (UserList, error) {
	var l UserList
	err := c.users(ctx, "user.list", struct{}{}, &l)
	return l, err
}

func (c *Client) UserCreate(ctx context.Context, u UserCreate) (UserList, error) {
	var l UserList
	err := c.users(ctx, "user.create", u, &l)
	return l, err
}

func (c *Client) UserUpdate(ctx context.Context, u UserUpdate) (UserList, error) {
	var l UserList
	err := c.users(ctx, "user.update", u, &l)
	return l, err
}

func (c *Client) UserSetPassword(ctx context.Context, name, password string) (PasswordResult, error) {
	var r PasswordResult
	err := c.users(ctx, "user.setPassword", map[string]string{"name": name, "password": password}, &r)
	return r, err
}

func (c *Client) UserSetDisabled(ctx context.Context, name string, disabled bool) (UserList, error) {
	var l UserList
	err := c.users(ctx, "user.setDisabled", map[string]any{"name": name, "disabled": disabled}, &l)
	return l, err
}

func (c *Client) UserDelete(ctx context.Context, name string) (UserList, error) {
	var l UserList
	err := c.users(ctx, "user.delete", map[string]string{"name": name}, &l)
	return l, err
}

func (c *Client) GroupCreate(ctx context.Context, name string) (UserList, error) {
	var l UserList
	err := c.users(ctx, "group.create", map[string]string{"name": name}, &l)
	return l, err
}

func (c *Client) GroupSetMembers(ctx context.Context, name string, members []string) (UserList, error) {
	var l UserList
	err := c.users(ctx, "group.setMembers", map[string]any{"name": name, "members": members}, &l)
	return l, err
}

func (c *Client) GroupDelete(ctx context.Context, name string) (UserList, error) {
	var l UserList
	err := c.users(ctx, "group.delete", map[string]string{"name": name}, &l)
	return l, err
}

// Shared folder operations (SHARE-01..05); applying ACLs to a big folder can
// take a while, hence the longer timeout.
func (c *Client) shares(ctx context.Context, op string, args any, result any) error {
	long := *c
	long.Timeout = 65 * time.Second
	return long.Call(ctx, op, args, result)
}

func (c *Client) ShareList(ctx context.Context) (ShareList, error) {
	var l ShareList
	err := c.shares(ctx, "share.list", struct{}{}, &l)
	return l, err
}

func (c *Client) ShareCreate(ctx context.Context, s shares.Share) (ShareList, error) {
	var l ShareList
	err := c.shares(ctx, "share.create", map[string]any{"share": s}, &l)
	return l, err
}

func (c *Client) ShareUpdate(ctx context.Context, s shares.Share) (ShareList, error) {
	var l ShareList
	err := c.shares(ctx, "share.update", map[string]any{"share": s}, &l)
	return l, err
}

func (c *Client) ShareDelete(ctx context.Context, d ShareDelete) (ShareDeleteResult, error) {
	var r ShareDeleteResult
	err := c.shares(ctx, "share.delete", d, &r)
	return r, err
}

func (c *Client) ShareRecycleCleanup(ctx context.Context) (shares.RecycleResult, error) {
	var r shares.RecycleResult
	long := *c
	long.Timeout = 11 * time.Minute
	err := long.Call(ctx, "share.recycleCleanup", struct{}{}, &r)
	return r, err
}

// AsError extracts a *Error from err.
func AsError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

func (c *Client) ServiceList(ctx context.Context) (ServiceList, error) {
	var r ServiceList
	err := c.Call(ctx, "service.list", nil, &r)
	return r, err
}

func (c *Client) ServiceAction(ctx context.Context, name, action string) (Service, error) {
	long := *c
	long.Timeout = 45 * time.Second
	var r Service
	err := long.Call(ctx, "service.action", map[string]any{"name": name, "action": action}, &r)
	return r, err
}

func (c *Client) ServiceLog(ctx context.Context, name string, lines int) (ServiceLog, error) {
	var r ServiceLog
	err := c.Call(ctx, "journal.unit", map[string]any{"name": name, "lines": lines}, &r)
	return r, err
}

func (c *Client) SmbGet(ctx context.Context) (SmbService, error) {
	var r SmbService
	err := c.Call(ctx, "smb.get", nil, &r)
	return r, err
}

// SmbSet can start or restart smbd, which takes a few seconds on the NAS.
func (c *Client) SmbSet(ctx context.Context, s smb.Settings) (SmbService, error) {
	long := *c
	long.Timeout = 45 * time.Second
	var r SmbService
	err := long.Call(ctx, "smb.set", map[string]any{"settings": s}, &r)
	return r, err
}

func (c *Client) SmbStatus(ctx context.Context) (smb.Status, error) {
	var r smb.Status
	err := c.Call(ctx, "smb.status", nil, &r)
	return r, err
}

func (c *Client) NfsGet(ctx context.Context) (NfsService, error) {
	var r NfsService
	err := c.Call(ctx, "nfs.get", nil, &r)
	return r, err
}

// NfsSet can start nfs-server, which takes a few seconds on the NAS.
func (c *Client) NfsSet(ctx context.Context, s nfs.Settings) (NfsService, error) {
	long := *c
	long.Timeout = 45 * time.Second
	var r NfsService
	err := long.Call(ctx, "nfs.set", map[string]any{"settings": s}, &r)
	return r, err
}

func (c *Client) NfsStatus(ctx context.Context) (NfsStatus, error) {
	var r NfsStatus
	err := c.Call(ctx, "nfs.status", nil, &r)
	return r, err
}

// HostnameSet changes the NAS name (hostnamectl over D-Bus).
func (c *Client) HostnameSet(ctx context.Context, name string) (HostnameState, error) {
	var r HostnameState
	err := c.Call(ctx, "net.hostname", map[string]string{"hostname": name}, &r)
	return r, err
}

// NetDiag runs one network test; a ping takes up to 10 s.
func (c *Client) NetDiag(ctx context.Context, test, target string) (DiagResult, error) {
	long := *c
	long.Timeout = 25 * time.Second
	var r DiagResult
	err := long.Call(ctx, "net.diag", map[string]string{"test": test, "target": target}, &r)
	return r, err
}

func (c *Client) SshGet(ctx context.Context) (SshService, error) {
	var r SshService
	err := c.Call(ctx, "ssh.get", nil, &r)
	return r, err
}

// SshSet runs sshd -t and reloads sshd, a few seconds on the NAS.
func (c *Client) SshSet(ctx context.Context, s sshd.Settings) (SshService, error) {
	long := *c
	long.Timeout = 45 * time.Second
	var r SshService
	err := long.Call(ctx, "ssh.set", map[string]any{"settings": s}, &r)
	return r, err
}

func (c *Client) SshKeys(ctx context.Context) (SshKeys, error) {
	var r SshKeys
	err := c.Call(ctx, "ssh.keys.list", nil, &r)
	return r, err
}

func (c *Client) SshKeyAdd(ctx context.Context, name, key string) (SshKeys, error) {
	var r SshKeys
	err := c.Call(ctx, "ssh.keys.add", map[string]string{"name": name, "key": key}, &r)
	return r, err
}

func (c *Client) SshKeyRemove(ctx context.Context, name, fingerprint string) (SshKeys, error) {
	var r SshKeys
	err := c.Call(ctx, "ssh.keys.remove", map[string]string{"name": name, "fingerprint": fingerprint}, &r)
	return r, err
}

func (c *Client) TaskGet(ctx context.Context) (TaskList, error) {
	var r TaskList
	err := c.Call(ctx, "task.get", nil, &r)
	return r, err
}

// TaskSet writes the timers; many systemctl calls on a slow CPU.
func (c *Client) TaskSet(ctx context.Context, l []tasks.Task) (TaskList, error) {
	long := *c
	long.Timeout = 60 * time.Second
	var r TaskList
	err := long.Call(ctx, "task.set", map[string]any{"tasks": l}, &r)
	return r, err
}

func (c *Client) TaskRun(ctx context.Context, id string) error {
	return c.Call(ctx, "task.run", map[string]string{"id": id}, nil)
}

func (c *Client) TaskStop(ctx context.Context, id string) error {
	return c.Call(ctx, "task.stop", map[string]string{"id": id}, nil)
}

// BackupKey returns the NAS public key (generated on first use).
func (c *Client) BackupKey(ctx context.Context) (BackupKey, error) {
	long := *c
	long.Timeout = 30 * time.Second
	var r BackupKey
	err := long.Call(ctx, "backup.key.get", nil, &r)
	return r, err
}

// BackupHostScan fetches the host keys of a server.
func (c *Client) BackupHostScan(ctx context.Context, host string, port int) (HostScan, error) {
	long := *c
	long.Timeout = 50 * time.Second
	var r HostScan
	err := long.Call(ctx, "backup.host.scan", map[string]any{"host": host, "port": port}, &r)
	return r, err
}

// BackupHostTrust stores a confirmed host key.
func (c *Client) BackupHostTrust(ctx context.Context, host string, port int, typ, key string) error {
	return c.Call(ctx, "backup.host.trust", map[string]any{"host": host, "port": port, "type": typ, "key": key}, nil)
}

// BackupTest tries the connection as the nas-backup account.
func (c *Client) BackupTest(ctx context.Context, host string, port int, user, path string) (BackupTestResult, error) {
	long := *c
	long.Timeout = 65 * time.Second
	var r BackupTestResult
	err := long.Call(ctx, "backup.test", map[string]any{"host": host, "port": port, "user": user, "path": path}, &r)
	return r, err
}

// BackupShareGet returns the state of the read-only versions share.
func (c *Client) BackupShareGet(ctx context.Context) (BackupShareState, error) {
	var r BackupShareState
	err := c.Call(ctx, "backup.share.get", nil, &r)
	return r, err
}

// BackupShareSet switches the versions share (re-renders the Samba config).
func (c *Client) BackupShareSet(ctx context.Context, enabled bool) (BackupShareState, error) {
	long := *c
	long.Timeout = 30 * time.Second
	var r BackupShareState
	err := long.Call(ctx, "backup.share.set", map[string]any{"enabled": enabled}, &r)
	return r, err
}

// callFile performs a request whose reply carries an open file descriptor
// (backup.open). The caller closes the file.
func (c *Client) callFile(ctx context.Context, op string, args, result any) (*os.File, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var dl net.Dialer
	conn, err := dl.DialContext(ctx, "unix", c.Socket)
	if err != nil {
		return nil, unavailable()
	}
	defer conn.Close()
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, unavailable()
	}
	if t, ok := ctx.Deadline(); ok {
		uc.SetDeadline(t)
	}
	req := map[string]any{"op": op, "args": args}
	if u, ok := ctx.Value(userKey{}).(string); ok {
		req["user"] = u
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := uc.Write(body); err != nil {
		return nil, unavailable()
	}
	// The descriptor arrives with the first bytes; read the rest until the
	// helper closes the connection.
	buf, oob := make([]byte, 16<<10), make([]byte, syscall.CmsgSpace(4*4))
	n, oobn, _, _, err := uc.ReadMsgUnix(buf, oob)
	if err != nil && n == 0 {
		return nil, unavailable()
	}
	var file *os.File
	if msgs, perr := syscall.ParseSocketControlMessage(oob[:oobn]); perr == nil {
		for _, m := range msgs {
			fds, ferr := syscall.ParseUnixRights(&m)
			if ferr != nil {
				continue
			}
			for _, fd := range fds {
				if file == nil {
					file = os.NewFile(uintptr(fd), "backup-file")
				} else {
					syscall.Close(fd)
				}
			}
		}
	}
	data := append([]byte{}, buf[:n]...)
	rest, err := io.ReadAll(io.LimitReader(uc, 64<<10))
	if err != nil && len(rest) == 0 && !errors.Is(err, io.EOF) {
		// A broken tail is fine as long as the JSON already arrived.
		rest = nil
	}
	data = append(data, rest...)
	var resp clientResponse
	if err := json.Unmarshal(bytes.TrimSpace(data), &resp); err != nil {
		closeFile(file)
		return nil, unavailable()
	}
	if !resp.OK {
		closeFile(file)
		if resp.Error == nil {
			return nil, unavailable()
		}
		return nil, resp.Error
	}
	if file == nil {
		return nil, unavailable()
	}
	if result != nil {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			file.Close()
			return nil, err
		}
	}
	return file, nil
}

func closeFile(f *os.File) {
	if f != nil {
		f.Close()
	}
}

// BackupVersions lists the versions of a backup job with their sizes.
func (c *Client) BackupVersions(ctx context.Context, id string) (BackupVersions, error) {
	long := *c
	long.Timeout = 65 * time.Second
	var r BackupVersions
	err := long.Call(ctx, "backup.versions", map[string]any{"id": id}, &r)
	return r, err
}

// BackupBrowse lists one directory of a version.
func (c *Client) BackupBrowse(ctx context.Context, id, date, path string, offset int) (BrowseResult, error) {
	long := *c
	long.Timeout = 30 * time.Second
	var r BrowseResult
	err := long.Call(ctx, "backup.browse", map[string]any{"id": id, "date": date, "path": path, "offset": offset}, &r)
	return r, err
}

// BackupOpen opens a file of a version; the caller closes it.
func (c *Client) BackupOpen(ctx context.Context, id, date, path string) (*os.File, BackupFileMeta, error) {
	var m BackupFileMeta
	f, err := c.callFile(ctx, "backup.open", map[string]any{"id": id, "date": date, "path": path}, &m)
	return f, m, err
}
