package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"time"

	"nas-revival/backend/internal/accounts"
	"nas-revival/backend/internal/diskpower"
	"nas-revival/backend/internal/nfs"
	"nas-revival/backend/internal/shares"
	"nas-revival/backend/internal/smb"
)

const (
	MaxRequest   = 16 << 10
	readDeadline = 5 * time.Second
	opTimeout    = 60 * time.Second
	maxConns     = 8
)

// Dispatcher executes the fixed list of operations.
type Dispatcher struct {
	Net      *Manager
	Time     *TimeManager
	Smart    *SmartManager
	Power    *DiskPowerManager
	SysPower *SystemPowerManager
	// Config keeps the history of the managed files; nil disables it.
	Config *ConfigManager
	// Activity measures disk activity on demand; nil disables it.
	Activity *ActivityManager
	// Users manages NAS accounts and groups; nil disables it.
	Users *UserManager
	// Shares manages shared folders; nil disables it.
	Shares *ShareManager
	// Services lists and controls the allowlisted units; nil disables it.
	Services *ServiceManager
	// Smb manages the SMB service settings; nil disables it.
	Smb *SmbManager
	// Nfs manages the NFS exports; nil disables it.
	Nfs *NfsManager
}

type request struct {
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args"`
	// User is the web session user, recorded in the config history.
	User string `json:"user"`
}

type response struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  *Error `json:"error,omitempty"`
}

func decodeArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return invalid("", "Neplatné argumenty požadavku.")
	}
	return nil
}

// Handle runs one operation.
func (d *Dispatcher) Handle(ctx context.Context, req request) (any, error) {
	user := cleanUser(req.User)
	switch req.Op {
	case "network.get":
		return d.Net.Get(ctx), nil
	case "network.apply":
		cfg := NetConfig{UseDhcpDns: true}
		if err := decodeArgs(req.Args, &cfg); err != nil {
			return nil, err
		}
		d.Config.Prepare()
		return d.Net.Apply(ctx, cfg)
	case "network.confirm":
		st, err := d.Net.Confirm(ctx)
		if err == nil {
			d.Config.Changed(user, FileNetwork)
		}
		return st, err
	case "time.get":
		return d.Time.Get(ctx)
	case "time.apply":
		var a struct {
			NTPServers []string `json:"ntpServers"`
		}
		if err := decodeArgs(req.Args, &a); err != nil {
			return nil, err
		}
		d.Config.Prepare()
		st, err := d.Time.Apply(ctx, a.NTPServers)
		d.Config.Changed(user, FileTime)
		return st, err
	case "smart.test":
		if d.Smart == nil {
			break
		}
		var a struct {
			DiskID string `json:"diskId"`
			Type   string `json:"type"`
		}
		if err := decodeArgs(req.Args, &a); err != nil {
			return nil, err
		}
		return d.Smart.Test(ctx, a.DiskID, a.Type)
	case "disk.setPower":
		if d.Power == nil {
			break
		}
		var a struct {
			DiskID   string             `json:"diskId"`
			Settings diskpower.Settings `json:"settings"`
		}
		if err := decodeArgs(req.Args, &a); err != nil {
			return nil, err
		}
		d.Config.Prepare()
		r, err := d.Power.SetPower(ctx, a.DiskID, a.Settings)
		d.Config.Changed(user, FileDiskPower)
		return r, err
	case "disk.standby":
		if d.Power == nil {
			break
		}
		var a struct {
			DiskID string `json:"diskId"`
		}
		if err := decodeArgs(req.Args, &a); err != nil {
			return nil, err
		}
		return d.Power.Standby(ctx, a.DiskID)
	case "disk.activity":
		if d.Activity == nil {
			break
		}
		var a struct {
			Seconds int `json:"seconds"`
		}
		if err := decodeArgs(req.Args, &a); err != nil {
			return nil, err
		}
		return d.Activity.Run(ctx, a.Seconds)
	case "power.preflight":
		if d.SysPower == nil {
			break
		}
		return d.SysPower.Preflight(ctx), nil
	case "power.reboot", "power.shutdown":
		if d.SysPower == nil {
			break
		}
		var a struct {
			Force bool   `json:"force"`
			User  string `json:"user"`
		}
		if err := decodeArgs(req.Args, &a); err != nil {
			return nil, err
		}
		return d.SysPower.Do(ctx, req.Op[len("power."):], a.Force, a.User)
	case "config.list":
		if d.Config == nil {
			break
		}
		return d.Config.List()
	case "config.get", "config.rollback":
		if d.Config == nil {
			break
		}
		var a struct {
			ID       int  `json:"id"`
			Takeover bool `json:"takeover"`
		}
		if err := decodeArgs(req.Args, &a); err != nil {
			return nil, err
		}
		if a.ID < 1 {
			return nil, invalid("id", "Neplatné číslo verze.")
		}
		if req.Op == "config.get" {
			return d.Config.Get(a.ID)
		}
		return d.Config.Rollback(ctx, a.ID, a.Takeover, user)
	}
	if (strings.HasPrefix(req.Op, "user.") || strings.HasPrefix(req.Op, "group.")) && d.Users != nil {
		return d.handleUsers(ctx, req, user)
	}
	if strings.HasPrefix(req.Op, "share.") && d.Shares != nil {
		return d.handleShares(ctx, req, user)
	}
	if strings.HasPrefix(req.Op, "smb.") && d.Smb != nil {
		return d.handleSmb(ctx, req, user)
	}
	if strings.HasPrefix(req.Op, "nfs.") && d.Nfs != nil {
		return d.handleNfs(ctx, req, user)
	}
	if (strings.HasPrefix(req.Op, "service.") || req.Op == "journal.unit") && d.Services != nil {
		return d.handleServices(ctx, req, user)
	}
	return nil, invalid("", "Neznámá operace.")
}

// handleSmb runs the SMB service operations (SMB-01, SMB-02).
func (d *Dispatcher) handleSmb(ctx context.Context, req request, user string) (any, error) {
	var a struct {
		Settings *smb.Settings `json:"settings"`
	}
	if err := decodeArgs(req.Args, &a); err != nil {
		return nil, err
	}
	switch req.Op {
	case "smb.get":
		return d.Smb.Get(ctx)
	case "smb.status":
		return d.Smb.Status(ctx)
	case "smb.set":
		if a.Settings == nil {
			return nil, invalid("settings", "Chybí nastavení SMB.")
		}
		d.Config.Prepare()
		st, err := d.Smb.Set(ctx, *a.Settings, user)
		if err == nil {
			d.Config.Changed(user, FileSmb)
		}
		return st, err
	}
	return nil, invalid("", "Neznámá operace.")
}

// handleNfs runs the NFS export operations (NFS-01, BKP-12).
func (d *Dispatcher) handleNfs(ctx context.Context, req request, user string) (any, error) {
	var a struct {
		Settings *nfs.Settings `json:"settings"`
	}
	if err := decodeArgs(req.Args, &a); err != nil {
		return nil, err
	}
	switch req.Op {
	case "nfs.get":
		return d.Nfs.Get(ctx)
	case "nfs.status":
		return d.Nfs.Status(ctx)
	case "nfs.set":
		if a.Settings == nil {
			return nil, invalid("settings", "Chybí nastavení NFS.")
		}
		d.Config.Prepare()
		st, err := d.Nfs.Set(ctx, *a.Settings, user)
		if err == nil {
			d.Config.Changed(user, FileNfs)
		}
		return st, err
	}
	return nil, invalid("", "Neznámá operace.")
}

// handleServices runs the service operations (SVC-10..12).
func (d *Dispatcher) handleServices(ctx context.Context, req request, user string) (any, error) {
	var a struct {
		Name   string `json:"name"`
		Action string `json:"action"`
		Lines  int    `json:"lines"`
	}
	if err := decodeArgs(req.Args, &a); err != nil {
		return nil, err
	}
	switch req.Op {
	case "service.list":
		return d.Services.List(ctx)
	case "service.action":
		return d.Services.Action(ctx, a.Name, a.Action, user)
	case "journal.unit":
		return d.Services.Log(ctx, a.Name, a.Lines)
	}
	return nil, invalid("", "Neznámá operace.")
}

// handleShares runs the shared folder operations (SHARE-01..05).
func (d *Dispatcher) handleShares(ctx context.Context, req request, user string) (any, error) {
	var a struct {
		Share       *shares.Share `json:"share"`
		Name        string        `json:"name"`
		DeleteData  bool          `json:"deleteData"`
		ConfirmName string        `json:"confirmName"`
	}
	if err := decodeArgs(req.Args, &a); err != nil {
		return nil, err
	}
	m := d.Shares
	switch req.Op {
	case "share.list":
		return m.List(ctx)
	case "share.create", "share.update":
		if a.Share == nil {
			return nil, invalid("share", "Chybí sdílená složka.")
		}
		d.Config.Prepare()
		var l ShareList
		var err error
		if req.Op == "share.create" {
			l, err = m.Create(ctx, *a.Share, user)
		} else {
			l, err = m.Update(ctx, *a.Share, user)
		}
		if err == nil {
			d.Config.Changed(user, FileShares)
		}
		return l, err
	case "share.delete":
		d.Config.Prepare()
		r, err := m.Delete(ctx, ShareDelete{Name: a.Name, DeleteData: a.DeleteData, ConfirmName: a.ConfirmName}, user)
		if err == nil {
			d.Config.Changed(user, FileShares)
		}
		return r, err
	case "share.recycleCleanup":
		return m.RecycleCleanup(ctx, user)
	}
	return nil, invalid("", "Neznámá operace.")
}

// handleUsers runs the account operations (USR-01..03). Passwords arrive in
// the arguments and are never logged.
func (d *Dispatcher) handleUsers(ctx context.Context, req request, user string) (any, error) {
	var a struct {
		Name     string        `json:"name"`
		FullName string        `json:"fullName"`
		Password string        `json:"password"`
		Groups   []string      `json:"groups"`
		Members  []string      `json:"members"`
		Disabled bool          `json:"disabled"`
		Role     accounts.Role `json:"role"`
	}
	if err := decodeArgs(req.Args, &a); err != nil {
		return nil, err
	}
	u := d.Users
	switch req.Op {
	case "user.list":
		return u.List(ctx)
	case "user.create":
		return u.Create(ctx, UserCreate{Name: a.Name, FullName: a.FullName, Password: a.Password, Groups: a.Groups, Role: a.Role}, user)
	case "user.update":
		return u.Update(ctx, UserUpdate{Name: a.Name, FullName: a.FullName, Groups: a.Groups, Role: a.Role}, user)
	case "user.setPassword":
		return u.SetPassword(ctx, a.Name, a.Password, user)
	case "user.setDisabled":
		return u.SetDisabled(ctx, a.Name, a.Disabled, user)
	case "user.delete":
		return u.Delete(ctx, a.Name, user)
	case "group.create":
		return u.GroupCreate(ctx, a.Name, user)
	case "group.setMembers":
		return u.GroupSetMembers(ctx, a.Name, a.Members, user)
	case "group.delete":
		return u.GroupDelete(ctx, a.Name, user)
	}
	return nil, invalid("", "Neznámá operace.")
}

// Serve accepts connections on ln until ctx ends. Only peers whose uid equals
// allowedUID are served.
func Serve(ctx context.Context, ln net.Listener, allowedUID uint32, d *Dispatcher) error {
	go func() { <-ctx.Done(); ln.Close() }()
	sem := make(chan struct{}, maxConns)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case sem <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			defer conn.Close()
			serveConn(ctx, conn, allowedUID, d)
		}()
	}
}

func serveConn(ctx context.Context, conn net.Conn, allowedUID uint32, d *Dispatcher) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return
	}
	uid, err := peerUID(uc)
	if err != nil || uid != allowedUID {
		slog.Warn("helper: connection rejected", "uid", uid, "err", err)
		return
	}
	uc.SetDeadline(time.Now().Add(readDeadline))
	lr := &io.LimitedReader{R: uc, N: MaxRequest + 1}
	var req request
	if err := json.NewDecoder(lr).Decode(&req); err != nil {
		msg := "Neplatný požadavek."
		if lr.N <= 0 {
			msg = "Požadavek je příliš velký."
		}
		reply(uc, nil, invalid("", msg))
		return
	}
	if lr.N <= 0 {
		reply(uc, nil, invalid("", "Požadavek je příliš velký."))
		return
	}
	octx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	res, err := d.Handle(octx, req)
	reply(uc, res, err)
}

func reply(c net.Conn, res any, err error) {
	c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	var out response
	if err != nil {
		var he *Error
		if !errors.As(err, &he) {
			he = internalErr("Interní chyba.")
		}
		out.Error = he
	} else {
		out.OK, out.Result = true, res
	}
	json.NewEncoder(c).Encode(out)
}

// Listen creates the socket with directory 0750 and socket 0660, both owned by
// root and the group of groupName (gid -1 keeps the current group).
func Listen(path string, gid int) (net.Listener, error) {
	dir := dirOf(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o750); err != nil {
		return nil, err
	}
	if gid >= 0 {
		if err := os.Chown(dir, -1, gid); err != nil {
			return nil, err
		}
	}
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o660); err != nil {
		ln.Close()
		return nil, err
	}
	if gid >= 0 {
		if err := os.Chown(path, -1, gid); err != nil {
			ln.Close()
			return nil, err
		}
	}
	return ln, nil
}

func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			if i == 0 {
				return "/"
			}
			return p[:i]
		}
	}
	return "."
}

// LookupUser returns the uid and gid of a system user.
func LookupUser(name string) (uid uint32, gid int, err error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, 0, fmt.Errorf("user %q: %w", name, err)
	}
	id, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return 0, 0, err
	}
	g, err := strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, err
	}
	return uint32(id), g, nil
}
