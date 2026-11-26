package smb

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// MaxRows caps the lists the UI gets; a busy NAS can have thousands of opens.
const MaxRows = 500

// Session is one connected client (SMB-02).
type Session struct {
	ID         string   `json:"id"`
	User       string   `json:"user"`
	Machine    string   `json:"machine"`
	Dialect    string   `json:"dialect"`
	Encryption string   `json:"encryption"`
	Signing    string   `json:"signing"`
	Since      string   `json:"since"`
	Shares     []string `json:"shares"`
}

// OpenFile is one file a client holds open.
type OpenFile struct {
	Share  string `json:"share"`
	Path   string `json:"path"`
	User   string `json:"user"`
	Access string `json:"access"` // "R", "W", "RW" or ""
	Since  string `json:"since"`
}

// Status is the parsed smbstatus --json.
type Status struct {
	Sessions  []Session  `json:"sessions"`
	OpenFiles []OpenFile `json:"openFiles"`
	Truncated bool       `json:"truncated"`
}

type rawCrypto struct {
	Cipher string `json:"cipher"`
	Degree string `json:"degree"`
}

type rawStatus struct {
	Sessions map[string]struct {
		SessionID     string    `json:"session_id"`
		UID           int       `json:"uid"`
		Username      string    `json:"username"`
		RemoteMachine string    `json:"remote_machine"`
		Dialect       string    `json:"session_dialect"`
		CreationTime  string    `json:"creation_time"`
		Encryption    rawCrypto `json:"encryption"`
		Signing       rawCrypto `json:"signing"`
	} `json:"sessions"`
	Tcons map[string]struct {
		Service   string `json:"service"`
		SessionID string `json:"session_id"`
	} `json:"tcons"`
	OpenFiles map[string]struct {
		ServicePath string `json:"service_path"`
		Filename    string `json:"filename"`
		Opens       map[string]struct {
			// Opens carry only the uid; the name comes from the sessions.
			UID        int    `json:"uid"`
			OpenedAt   string `json:"opened_at"`
			AccessMask struct {
				ReadData  bool `json:"READ_DATA"`
				WriteData bool `json:"WRITE_DATA"`
				Append    bool `json:"APPEND_DATA"`
			} `json:"access_mask"`
		} `json:"opens"`
	} `json:"open_files"`
}

func crypto(c rawCrypto) string {
	if c.Degree == "" || c.Degree == "none" {
		return ""
	}
	if c.Cipher == "" {
		return c.Degree
	}
	return c.Cipher
}

// ParseStatus parses smbstatus --json. sharePaths maps a share path to its
// name, so open files show the share rather than the folder on disk.
func ParseStatus(b []byte, sharePaths map[string]string) (Status, error) {
	var r rawStatus
	if err := json.Unmarshal(b, &r); err != nil {
		return Status{}, err
	}
	out := Status{Sessions: []Session{}, OpenFiles: []OpenFile{}}
	names := map[int]string{}
	for _, s := range r.Sessions {
		names[s.UID] = s.Username
	}
	byID := map[string][]string{}
	for _, t := range r.Tcons {
		if t.Service != "IPC$" {
			byID[t.SessionID] = append(byID[t.SessionID], t.Service)
		}
	}
	for key, s := range r.Sessions {
		id := s.SessionID
		if id == "" {
			id = key
		}
		sh := byID[id]
		if sh == nil {
			sh = []string{}
		}
		sort.Strings(sh)
		out.Sessions = append(out.Sessions, Session{ID: id, User: s.Username, Machine: s.RemoteMachine,
			Dialect: s.Dialect, Encryption: crypto(s.Encryption), Signing: crypto(s.Signing),
			Since: s.CreationTime, Shares: sh})
	}
	sort.Slice(out.Sessions, func(i, j int) bool {
		a, b := out.Sessions[i], out.Sessions[j]
		if a.User != b.User {
			return a.User < b.User
		}
		return a.ID < b.ID
	})
	for _, f := range r.OpenFiles {
		share := sharePaths[f.ServicePath]
		if share == "" {
			share = f.ServicePath
		}
		for _, o := range f.Opens {
			acc := ""
			if o.AccessMask.ReadData {
				acc += "R"
			}
			if o.AccessMask.WriteData || o.AccessMask.Append {
				acc += "W"
			}
			out.OpenFiles = append(out.OpenFiles, OpenFile{Share: share, Path: strings.TrimPrefix(f.Filename, "./"),
				User: userName(names, o.UID), Access: acc, Since: o.OpenedAt})
		}
	}
	sort.Slice(out.OpenFiles, func(i, j int) bool {
		a, b := out.OpenFiles[i], out.OpenFiles[j]
		if a.Share != b.Share {
			return a.Share < b.Share
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.User < b.User
	})
	if len(out.Sessions) > MaxRows {
		out.Sessions, out.Truncated = out.Sessions[:MaxRows], true
	}
	if len(out.OpenFiles) > MaxRows {
		out.OpenFiles, out.Truncated = out.OpenFiles[:MaxRows], true
	}
	return out, nil
}

func userName(names map[int]string, uid int) string {
	if n, ok := names[uid]; ok {
		return n
	}
	return strconv.Itoa(uid)
}
