// Package auth implements the single administrator login: an argon2id password
// file, in-memory sessions and a per-client failed-login limiter.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters sized for a 256 MiB ARMv5 device: 16 MiB per hash, and
// Verify allows only one hash at a time so parallel logins cannot exhaust RAM.
const (
	argonTime    = 3
	argonMemory  = 16 * 1024
	argonThreads = 1
	argonKeyLen  = 32
	saltLen      = 16
)

var ErrInvalidCredentials = errors.New("invalid credentials")

// Credentials is the on-disk administrator record.
type Credentials struct {
	Username string `json:"username"`
	Hash     string `json:"hash"`
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

type argonHash struct {
	memory  uint32
	time    uint32
	threads uint8
	salt    []byte
	key     []byte
}

func parseHash(h string) (argonHash, error) {
	var a argonHash
	parts := strings.Split(h, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return a, errors.New("unsupported hash format")
	}
	var v int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return a, errors.New("unsupported argon2 version")
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &a.memory, &a.time, &a.threads); err != nil {
		return a, errors.New("bad argon2 parameters")
	}
	// Refuse parameters that would exhaust the device if the file were tampered with.
	if a.memory == 0 || a.memory > 64*1024 || a.time == 0 || a.time > 10 || a.threads == 0 {
		return a, errors.New("argon2 parameters out of range")
	}
	var err error
	enc := base64.RawStdEncoding
	if a.salt, err = enc.DecodeString(parts[4]); err != nil {
		return a, errors.New("bad salt")
	}
	if a.key, err = enc.DecodeString(parts[5]); err != nil || len(a.key) == 0 {
		return a, errors.New("bad key")
	}
	return a, nil
}

// LoadCredentials reads the administrator file. It must not be readable by others.
func LoadCredentials(path string) (Credentials, error) {
	var c Credentials
	st, err := os.Stat(path)
	if err != nil {
		return c, err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return c, fmt.Errorf("%s must not be accessible by group or others (mode %v)", path, st.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.Username == "" {
		return c, fmt.Errorf("%s: empty username", path)
	}
	if _, err := parseHash(c.Hash); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// SaveCredentials writes the file atomically with mode 0600.
func SaveCredentials(path string, c Credentials) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".admin-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Verifier checks a username/password pair against stored credentials.
type Verifier struct {
	creds Credentials
	dummy argonHash
	sem   chan struct{}
}

func NewVerifier(c Credentials) (*Verifier, error) {
	if _, err := parseHash(c.Hash); err != nil {
		return nil, err
	}
	// A dummy hash keeps the timing of unknown usernames equal to wrong passwords.
	dummy, err := HashPassword("dummy-password-for-timing")
	if err != nil {
		return nil, err
	}
	d, _ := parseHash(dummy)
	return &Verifier{creds: c, dummy: d, sem: make(chan struct{}, 1)}, nil
}

func (v *Verifier) Verify(username, password string) error {
	v.sem <- struct{}{}
	defer func() { <-v.sem }()
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(v.creds.Username)) == 1
	h, _ := parseHash(v.creds.Hash)
	if !userOK {
		h = v.dummy
	}
	key := argon2.IDKey([]byte(password), h.salt, h.time, h.memory, h.threads, uint32(len(h.key)))
	if subtle.ConstantTimeCompare(key, h.key) == 1 && userOK {
		return nil
	}
	return ErrInvalidCredentials
}

// Session lifetimes.
const (
	IdleTimeout     = 30 * time.Minute
	AbsoluteTimeout = 12 * time.Hour
	maxSessions     = 32
)

type session struct {
	username string
	created  time.Time
	lastSeen time.Time
}

// Sessions stores only a SHA-256 of each token, so a memory dump of the map
// does not yield usable cookies.
type Sessions struct {
	Now func() time.Time
	mu  sync.Mutex
	m   map[[32]byte]*session
}

func NewSessions() *Sessions {
	return &Sessions{Now: time.Now, m: map[[32]byte]*session{}}
}

func (s *Sessions) Create(username string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	s.expireLocked(now)
	if len(s.m) >= maxSessions {
		s.evictOldestLocked()
	}
	s.m[sha256.Sum256([]byte(token))] = &session{username: username, created: now, lastSeen: now}
	return token, nil
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

func (s *Sessions) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, sha256.Sum256([]byte(token)))
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

// Limiter blocks a client after too many failed logins within a window.
type Limiter struct {
	Max    int
	Window time.Duration
	Now    func() time.Time
	mu     sync.Mutex
	fails  map[string][]time.Time
}

func NewLimiter() *Limiter {
	return &Limiter{Max: 5, Window: 5 * time.Minute, Now: time.Now, fails: map[string][]time.Time{}}
}

// Blocked reports whether the client must wait, and for how long.
func (l *Limiter) Blocked(client string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Now()
	f := l.pruneLocked(client, now)
	if len(f) < l.Max {
		return 0, false
	}
	return f[0].Add(l.Window).Sub(now), true
}

func (l *Limiter) Fail(client string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Now()
	l.fails[client] = append(l.pruneLocked(client, now), now)
	if len(l.fails) > 1024 { // bound memory under a scan from many addresses
		for k := range l.fails {
			if len(l.pruneLocked(k, now)) == 0 {
				delete(l.fails, k)
			}
		}
	}
}

func (l *Limiter) Reset(client string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, client)
}

func (l *Limiter) pruneLocked(client string, now time.Time) []time.Time {
	f := l.fails[client]
	i := 0
	for i < len(f) && now.Sub(f[i]) >= l.Window {
		i++
	}
	f = f[i:]
	if len(f) == 0 {
		delete(l.fails, client)
		return nil
	}
	l.fails[client] = f
	return f
}
