package helper

import (
	"context"
	"os"
	"time"
)

// File Station (W4.1) and USB storage (W4.2) client calls. The session user travels in ctx.

func (c *Client) withTimeout(d time.Duration) *Client {
	long := *c
	long.Timeout = d
	return &long
}

// FileShares lists the shares the session user may open.
func (c *Client) FileShares(ctx context.Context) (FileShareList, error) {
	var r FileShareList
	err := c.Call(ctx, "fs.shares", struct{}{}, &r)
	return r, err
}

// FileList lists one directory; a cold directory of thousands of entries
// takes a while on the NSA310.
func (c *Client) FileList(ctx context.Context, share, path string, offset int) (FileList, error) {
	var r FileList
	err := c.withTimeout(30*time.Second).Call(ctx, "fs.list", map[string]any{"share": share, "path": path, "offset": offset}, &r)
	return r, err
}

// FileOpen opens a file for download; the caller closes it.
func (c *Client) FileOpen(ctx context.Context, share, path string) (*os.File, BackupFileMeta, error) {
	var m BackupFileMeta
	f, err := c.callFile(ctx, "fs.open", map[string]any{"share": share, "path": path}, &m)
	return f, m, err
}

func (c *Client) FileMkdir(ctx context.Context, share, dir, name string) error {
	return c.Call(ctx, "fs.mkdir", map[string]any{"share": share, "path": dir, "name": name}, nil)
}

func (c *Client) FileRename(ctx context.Context, share, path, name string) error {
	return c.Call(ctx, "fs.rename", map[string]any{"share": share, "path": path, "name": name}, nil)
}

func (c *Client) FileMove(ctx context.Context, share string, paths []string, dest string) (FileBatch, error) {
	var r FileBatch
	err := c.withTimeout(60*time.Second).Call(ctx, "fs.move", map[string]any{"share": share, "paths": paths, "dest": dest}, &r)
	return r, err
}

// FileDelete may walk big trees (permanent delete, touching binned files).
func (c *Client) FileDelete(ctx context.Context, share string, paths []string) (FileBatch, error) {
	var r FileBatch
	err := c.withTimeout(recycleTimeout).Call(ctx, "fs.delete", map[string]any{"share": share, "paths": paths}, &r)
	return r, err
}

// FileUploadOpen creates the temporary file of an upload; the caller writes
// into the returned descriptor and then commits or aborts with the token.
func (c *Client) FileUploadOpen(ctx context.Context, share, dir, name string, size int64, overwrite bool) (*os.File, string, error) {
	var t UploadTicket
	f, err := c.callFile(ctx, "fs.uploadOpen", map[string]any{"share": share, "path": dir, "name": name, "size": size, "overwrite": overwrite}, &t)
	return f, t.Token, err
}

func (c *Client) FileUploadCommit(ctx context.Context, share, dir, token, name string, overwrite bool) error {
	return c.Call(ctx, "fs.uploadCommit", map[string]any{"share": share, "path": dir, "token": token, "name": name, "overwrite": overwrite}, nil)
}

func (c *Client) FileUploadAbort(ctx context.Context, share, dir, token string) error {
	return c.Call(ctx, "fs.uploadAbort", map[string]any{"share": share, "path": dir, "token": token}, nil)
}

// USB storage (W4.2). Mounting waits for systemd; ejecting unmounts every
// partition first.

func (c *Client) UsbList(ctx context.Context) (UsbList, error) {
	var r UsbList
	err := c.Call(ctx, "usb.list", struct{}{}, &r)
	return r, err
}

func (c *Client) UsbMount(ctx context.Context, diskID, part string, readOnly bool) (UsbList, error) {
	var r UsbList
	err := c.withTimeout(65*time.Second).Call(ctx, "usb.mount", map[string]any{"diskId": diskID, "partition": part, "readOnly": readOnly}, &r)
	return r, err
}

func (c *Client) UsbUnmount(ctx context.Context, diskID, part string) (UsbList, error) {
	var r UsbList
	err := c.withTimeout(65*time.Second).Call(ctx, "usb.unmount", map[string]any{"diskId": diskID, "partition": part}, &r)
	return r, err
}

func (c *Client) UsbEject(ctx context.Context, diskID string) (UsbList, error) {
	var r UsbList
	err := c.withTimeout(65*time.Second).Call(ctx, "usb.eject", map[string]any{"diskId": diskID}, &r)
	return r, err
}

// UsbCopyGet returns the destination of the COPY button.
func (c *Client) UsbCopyGet(ctx context.Context) (UsbCopyInfo, error) {
	var r UsbCopyInfo
	err := c.Call(ctx, "usb.copy.get", struct{}{}, &r)
	return r, err
}

// UsbCopySet sets (or, with an empty share, clears) it.
func (c *Client) UsbCopySet(ctx context.Context, share, folder string) (UsbCopyInfo, error) {
	var r UsbCopyInfo
	err := c.Call(ctx, "usb.copy.set", map[string]any{"share": share, "folder": folder}, &r)
	return r, err
}
