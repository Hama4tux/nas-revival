package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"nas-revival/backend/internal/atomicfile"
)

// Session lifetimes.
const (
	IdleTimeout     = 30 * time.Minute
	AbsoluteTimeout = 12 * time.Hour
	maxSessions     = 32
	maxAgentLen     = 160
)

// Meta describes where a session was created; shown in the Security Center.
type Meta struct {
	Client string
	Agent  string
}

type session struct {
	id       string
	username string
	client   string
	agent    string
	created  time.Time
	lastSeen time.Time
}

// SessionInfo is one session as the Security Center lists it. ID is random
// and unrelated to the cookie, so listing sessions never reveals a token.
type SessionInfo struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Client    string    `json:"client"`
	Agent     string    `json:"agent"`
	Created   time.Time `json:"createdAt"`
	LastSeen  time.Time `json:"lastSeenAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Sessions stores only a SHA-256 of each token, so a memory dump of the map
// (or the state file) does not yield usable cookies.
//
// With a Path the sessions survive a restart of the web service (every
// deployment). The file is written on login, logout and revocation and on
// exit (Save), never on ordinary requests, so an idle NAS does not wake its
// disk; after a crash the last activity may be older than in memory, which
// can only end a session earlier.
type Sessions struct {
	Now  func() time.Time
	Path string
	// OnSaveError receives failures of saves that have no caller to report
	// to (logout, revocation); the in-memory state stays authoritative.
	OnSaveError func(error)
	mu          sync.Mutex
	m           map[[32]byte]*session
	gen         uint64

	saveMu  sync.Mutex
	written uint64
}

func NewSessions() *Sessions {
	return &Sessions{Now: time.Now, m: map[[32]byte]*session{}}
}

// OpenSessions loads the sessions saved at path that have not expired by
// now(). A missing file is an empty store; an unreadable one is reported and
// replaced on the next save.
func OpenSessions(path string, now func() time.Time) (*Sessions, error) {
	s := NewSessions()
	s.Path, s.Now = path, now
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var f sessionFile
	if err := json.Unmarshal(b, &f); err != nil {
		return s, fmt.Errorf("parse %s: %w", path, err)
	}
	t := s.Now()
	for _, r := range f.Sessions {
		raw, err := hex.DecodeString(r.Hash)
		if err != nil || len(raw) != 32 || r.ID == "" || r.Username == "" {
			continue
		}
		sess := &session{id: r.ID, username: r.Username, client: r.Client, agent: cleanAgent(r.Agent),
			created: r.Created, lastSeen: r.LastSeen}
		if s.expired(sess, t) || sess.lastSeen.After(t) {
			continue
		}
		var k [32]byte
		copy(k[:], raw)
		s.m[k] = sess
	}
	for len(s.m) > maxSessions {
		s.evictOldestLocked()
	}
	return s, nil
}

type sessionFile struct {
	Version  int             `json:"version"`
	Sessions []sessionRecord `json:"sessions"`
}

type sessionRecord struct {
	Hash     string    `json:"tokenSha256"`
	ID       string    `json:"id"`
	Username string    `json:"username"`
	Client   string    `json:"client,omitempty"`
	Agent    string    `json:"agent,omitempty"`
	Created  time.Time `json:"createdAt"`
	LastSeen time.Time `json:"lastSeenAt"`
}

func randomString(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// cleanAgent keeps the User-Agent printable and short: it is shown in the UI
// and must not carry control characters into logs or the state file.
func cleanAgent(a string) string {
	a = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, a)
	if r := []rune(a); len(r) > maxAgentLen {
		a = string(r[:maxAgentLen])
	}
	return a
}

func (s *Sessions) Create(username string, meta Meta) (string, error) {
	token, err := randomString(32)
	if err != nil {
		return "", err
	}
	id, err := randomString(12)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	now := s.Now()
	s.expireLocked(now)
	if len(s.m) >= maxSessions {
		s.evictOldestLocked()
	}
	s.m[sha256.Sum256([]byte(token))] = &session{id: id, username: username, client: meta.Client,
		agent: cleanAgent(meta.Agent), created: now, lastSeen: now}
	s.changedLocked()
	return token, s.save()
}

// Lookup validates a token, refreshes its idle timer and returns the user and
// the time the session will expire if left idle.
func (s *Sessions) Lookup(token string) (string, time.Time, bool) {
	if token == "" {
		return "", time.Time{}, false
	}
	key := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	sess, ok := s.m[key]
	if !ok {
		return "", time.Time{}, false
	}
	if s.expired(sess, now) {
		delete(s.m, key)
		return "", time.Time{}, false
	}
	sess.lastSeen = now
	return sess.username, s.expiry(sess), true
}

// ID returns the public ID of the session behind token ("" if none).
func (s *Sessions) ID(token string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.m[sha256.Sum256([]byte(token))]; ok {
		return sess.id
	}
	return ""
}

// List returns the live sessions, most recently active first.
func (s *Sessions) List() []SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	out := make([]SessionInfo, 0, len(s.m))
	for _, v := range s.m {
		if s.expired(v, now) {
			continue
		}
		out = append(out, SessionInfo{ID: v.id, Username: v.username, Client: v.client, Agent: v.agent,
			Created: v.created, LastSeen: v.lastSeen, ExpiresAt: s.expiry(v)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}

func (s *Sessions) Delete(token string) {
	s.mu.Lock()
	delete(s.m, sha256.Sum256([]byte(token)))
	s.changedLocked()
	s.saveLogged()
}

// Revoke ends the session with the public id and returns its user.
func (s *Sessions) Revoke(id string) (string, bool) {
	s.mu.Lock()
	for k, v := range s.m {
		if v.id == id {
			delete(s.m, k)
			s.changedLocked()
			s.saveLogged()
			return v.username, true
		}
	}
	s.mu.Unlock()
	return "", false
}

// RevokeOthers ends every session except the one behind token and returns
// how many ended.
func (s *Sessions) RevokeOthers(token string) int {
	keep := sha256.Sum256([]byte(token))
	s.mu.Lock()
	n := 0
	for k := range s.m {
		if k != keep {
			delete(s.m, k)
			n++
		}
	}
	s.changedLocked()
	s.saveLogged()
	return n
}

// DeleteUser ends every session of username (password change, disable,
// delete – ADR 0004).
func (s *Sessions) DeleteUser(username string) {
	s.mu.Lock()
	for k, v := range s.m {
		if v.username == username {
			delete(s.m, k)
		}
	}
	s.changedLocked()
	s.saveLogged()
}

// Save writes the current state, including the last activity of each
// session; called on exit.
func (s *Sessions) Save() error {
	s.mu.Lock()
	s.changedLocked()
	return s.save()
}

// changedLocked bumps the generation; the caller holds mu.
func (s *Sessions) changedLocked() { s.gen++ }

// save marshals under mu (held by the caller, released here) and writes
// outside it, so a slow or sleeping disk never blocks request lookups. The
// generation makes sure an older snapshot never overwrites a newer one.
func (s *Sessions) save() error {
	if s.Path == "" {
		s.mu.Unlock()
		return nil
	}
	gen := s.gen
	f := sessionFile{Version: 1, Sessions: make([]sessionRecord, 0, len(s.m))}
	for k, v := range s.m {
		f.Sessions = append(f.Sessions, sessionRecord{Hash: hex.EncodeToString(k[:]), ID: v.id, Username: v.username,
			Client: v.client, Agent: v.agent, Created: v.created, LastSeen: v.lastSeen})
	}
	s.mu.Unlock()
	sort.Slice(f.Sessions, func(i, j int) bool { return f.Sessions[i].ID < f.Sessions[j].ID })
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if gen <= s.written {
		return nil
	}
	if err := atomicfile.Write(s.Path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("save sessions: %w", err)
	}
	s.written = gen
	return nil
}

func (s *Sessions) saveLogged() {
	if err := s.save(); err != nil && s.OnSaveError != nil {
		s.OnSaveError(err)
	}
}

func (s *Sessions) expired(sess *session, now time.Time) bool {
	return now.Sub(sess.lastSeen) >= IdleTimeout || now.Sub(sess.created) >= AbsoluteTimeout
}

func (s *Sessions) expiry(sess *session) time.Time {
	idle := sess.lastSeen.Add(IdleTimeout)
	abs := sess.created.Add(AbsoluteTimeout)
	if abs.Before(idle) {
		return abs
	}
	return idle
}

func (s *Sessions) expireLocked(now time.Time) {
	for k, v := range s.m {
		if s.expired(v, now) {
			delete(s.m, k)
		}
	}
}

func (s *Sessions) evictOldestLocked() {
	var oldestKey [32]byte
	var oldest *session
	for k, v := range s.m {
		if oldest == nil || v.lastSeen.Before(oldest.lastSeen) {
			oldestKey, oldest = k, v
		}
	}
	delete(s.m, oldestKey)
}
