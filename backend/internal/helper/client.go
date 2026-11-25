package helper

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"

	"nas-revival/backend/internal/config"
	"nas-revival/backend/internal/diskpower"
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

// AsError extracts a *Error from err.
func AsError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}
