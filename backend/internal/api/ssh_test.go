package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"nas-revival/backend/internal/helper"
	"nas-revival/backend/internal/sshd"
)

type fakeSsh struct {
	calls []string
	got   sshd.Settings
}

func (f *fakeSsh) SshGet(context.Context) (helper.SshService, error) {
	f.calls = append(f.calls, "get")
	return helper.SshService{Settings: sshd.Default()}, nil
}
func (f *fakeSsh) SshSet(_ context.Context, s sshd.Settings) (helper.SshService, error) {
	f.calls = append(f.calls, "set")
	f.got = s
	return helper.SshService{Settings: s}, nil
}

func (f *fakeSsh) SshKeys(context.Context) (helper.SshKeys, error) {
	f.calls = append(f.calls, "keys")
	return helper.SshKeys{Users: []helper.SshKeyUser{}}, nil
}
func (f *fakeSsh) SshKeyAdd(_ context.Context, name, key string) (helper.SshKeys, error) {
	f.calls = append(f.calls, "add "+name)
	return helper.SshKeys{}, nil
}
func (f *fakeSsh) SshKeyRemove(_ context.Context, name, fp string) (helper.SshKeys, error) {
	f.calls = append(f.calls, "remove "+name+" "+fp)
	return helper.SshKeys{}, nil
}

func TestSshEndpointsAndRoles(t *testing.T) {
	s, _, _, _ := newRoleServer(t)
	f := &fakeSsh{}
	s.Ssh = f
	h := s.Handler()
	cookies := map[string]*http.Cookie{"jana": loginAs(t, h, "jana"), "petr": loginAs(t, h, "petr"), "eva": loginAs(t, h, "eva")}
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	sk, _ := ssh.NewPublicKey(pub)
	key, _ := json.Marshal(map[string]string{"key": strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sk))) + " jana@pc"})
	ok := `{"port":2222,"passwordAuth":false,"sftpGroups":["nas-users","foto","nas-users"]}`
	cases := []struct {
		method, path, body  string
		admin, viewer, user int
		call                string
	}{
		{"GET", "/api/v1/services/ssh", "", 200, 200, 403, "get"},
		{"PUT", "/api/v1/services/ssh", ok, 200, 403, 403, "set"},
		// Rejected before the helper is asked.
		{"PUT", "/api/v1/services/ssh", `{"port":23}`, 400, 403, 403, ""},
		{"PUT", "/api/v1/services/ssh", `{"port":22,"sftpGroups":["root"]}`, 400, 403, 403, ""},
		{"PUT", "/api/v1/services/ssh", `{"port":22,"permitRootLogin":"yes"}`, 400, 403, 403, ""},
		{"GET", "/api/v1/services/ssh/keys", "", 200, 200, 403, "keys"},
		{"POST", "/api/v1/services/ssh/keys/jana", string(key), 200, 403, 403, "add jana"},
		{"DELETE", "/api/v1/services/ssh/keys/jana", `{"fingerprint":"SHA256:abc"}`, 200, 403, 403, "remove jana SHA256:abc"},
		// Rejected before the helper is asked.
		{"POST", "/api/v1/services/ssh/keys/jana", `{"key":"ssh-rsa AAAA"}`, 400, 403, 403, ""},
		{"POST", "/api/v1/services/ssh/keys/jana", `{"key":"x","options":"y"}`, 400, 403, 403, ""},
		{"POST", "/api/v1/services/ssh/keys/Bad..Name", string(key), 404, 403, 403, ""},
		{"DELETE", "/api/v1/services/ssh/keys/jana", `{"fingerprint":"MD5:aa"}`, 400, 403, 403, ""},
	}
	for _, c := range cases {
		for name, want := range map[string]int{"jana": c.admin, "petr": c.viewer, "eva": c.user} {
			f.calls = nil
			rec := do(h, c.method, c.path, c.body, xrw, cookies[name])
			if rec.Code != want {
				t.Errorf("%s %s %s as %s = %d, want %d (%s)", c.method, c.path, c.body, name, rec.Code, want, rec.Body)
			}
			wantCall := ""
			if want == 200 {
				wantCall = c.call
			}
			if (len(f.calls) == 0 && wantCall != "") || (len(f.calls) > 0 && f.calls[0] != wantCall) || len(f.calls) > 1 {
				t.Errorf("%s %s as %s: calls %v, want %q", c.method, c.path, name, f.calls, wantCall)
			}
		}
	}
	if f.got.Port != 2222 || len(f.got.SftpGroups) != 2 || f.got.SftpGroups[0] != "foto" {
		t.Fatalf("settings not normalised: %+v", f.got)
	}
}
