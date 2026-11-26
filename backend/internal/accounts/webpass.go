package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"nas-revival/backend/internal/atomicfile"
)

// DefaultWebPasswords is the web password store read by the web process
// (owned by the service account, mode 0600) and written only by the helper.
const DefaultWebPasswords = "/etc/nas-revival/credentials.json"

// WebPassword is the web record of one NAS user.
type WebPassword struct {
	// Hash is the argon2id hash used by the web login (W2.2).
	Hash     string `json:"hash"`
	Disabled bool   `json:"disabled,omitempty"`
	// Changed is when the password was last set through the UI (RFC 3339).
	Changed string `json:"changed"`
	// Shadow is the fingerprint of the shadow hash written together with
	// Hash; a different current fingerprint means "changed outside the UI".
	Shadow string `json:"shadow"`
}

type webFile struct {
	Version int                    `json:"version"`
	Users   map[string]WebPassword `json:"users"`
}

// WebPasswords is the on-disk store; Owner is applied after every write so
// the web process can read the file.
type WebPasswords struct {
	Path               string
	OwnerUID, OwnerGID int // -1 keeps the current owner
}

func (w WebPasswords) Load() (map[string]WebPassword, error) {
	b, err := os.ReadFile(w.Path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]WebPassword{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f webFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", w.Path, err)
	}
	if f.Users == nil {
		f.Users = map[string]WebPassword{}
	}
	return f.Users, nil
}

func (w WebPasswords) Save(users map[string]WebPassword) error {
	b, err := json.MarshalIndent(webFile{Version: 1, Users: users}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(w.Path), 0o700); err != nil {
		return err
	}
	if err := atomicfile.Write(w.Path, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if w.OwnerUID >= 0 || w.OwnerGID >= 0 {
		return os.Chown(w.Path, w.OwnerUID, w.OwnerGID)
	}
	return nil
}
