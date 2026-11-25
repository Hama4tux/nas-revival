package helper

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"
)

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
	body, err := json.Marshal(map[string]any{"op": op, "args": args})
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

// AsError extracts a *Error from err.
func AsError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}
