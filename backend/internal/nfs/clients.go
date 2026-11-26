package nfs

import (
	"bufio"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ClientsDir lists the NFSv4 clients the kernel server knows (Linux 5.3+).
const ClientsDir = "/proc/fs/nfsd/clients"

// Connected is one NFSv4 client with a lease on the server.
type Connected struct {
	Address string `json:"address"` // IP without the source port
	Name    string `json:"name"`    // client-chosen id, e.g. "Linux NFSv4.2 pve"
	Version string `json:"version"` // "4.2"
	// Confirmed is false while the client is still setting up its lease.
	Confirmed bool `json:"confirmed"`
}

// ParseClientInfo reads one clients/<n>/info file.
func ParseClientInfo(b []byte) Connected {
	var c Connected
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case "address":
			addr := strings.Trim(v, `"`)
			// "192.168.1.10:833" or "[fd00::1]:833".
			if i := strings.LastIndexByte(addr, ':'); i > 0 {
				addr = addr[:i]
			}
			c.Address = strings.Trim(addr, "[]")
		case "name":
			c.Name = unquote(v)
		case "minor version":
			if n, err := strconv.Atoi(v); err == nil {
				c.Version = "4." + strconv.Itoa(n)
			}
		case "status":
			c.Confirmed = v == "confirmed"
		}
	}
	return c
}

// unquote decodes the C-style quoting of the kernel (\x41 bytes).
func unquote(v string) string {
	if s, err := strconv.Unquote(v); err == nil {
		return s
	}
	return strings.Trim(v, `"`)
}

// ReadClients lists the clients below dir; a missing dir (server stopped)
// is an empty list.
func ReadClients(dir string) ([]Connected, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []Connected{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Connected{}
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(dir, e.Name(), "info"))
		if err != nil {
			continue // the client went away meanwhile
		}
		if c := ParseClientInfo(b); c.Address != "" {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out, nil
}
