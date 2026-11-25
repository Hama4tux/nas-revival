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
	"sync"
	"time"
)

const (
	MaxRequest   = 16 << 10
	readDeadline = 5 * time.Second
	opTimeout    = 60 * time.Second
	maxConns     = 8
)

// Dispatcher executes the fixed list of operations.
type Dispatcher struct {
	Net   *Manager
	Time  *TimeManager
	Smart *SmartManager
}

type request struct {
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args"`
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
	switch req.Op {
	case "network.get":
		return d.Net.Get(ctx), nil
	case "network.apply":
		cfg := NetConfig{UseDhcpDns: true}
		if err := decodeArgs(req.Args, &cfg); err != nil {
			return nil, err
		}
		return d.Net.Apply(ctx, cfg)
	case "network.confirm":
		return d.Net.Confirm(ctx)
	case "time.get":
		return d.Time.Get(ctx)
	case "time.apply":
		var a struct {
			NTPServers []string `json:"ntpServers"`
		}
		if err := decodeArgs(req.Args, &a); err != nil {
			return nil, err
		}
		return d.Time.Apply(ctx, a.NTPServers)
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
