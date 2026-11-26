// Command e2e-smb is the NAS side of the W2.5 end-to-end check (scripts/webui/e2e-smb.sh runs it as
// nas-revival and talks SMB to the NAS with smbclient from the workstation in between).
//
//	setup   reads four passwords (admin, rw, ro, none) as lines on stdin, creates the throw-away
//	        accounts nrt-ma/nrt-mu/nrt-mr/nrt-mn and the shares nrt-smb (recycle bin) and nrt-smbh
//	        (hidden, read-only) through the helper, then enables SMB over HTTPS as nrt-ma
//	status  logs in as nrt-ma (password on stdin) and prints the SMB sessions as JSON
//	verify  checks the files the client left on disk (upload, recycle bin); runs as root because
//	        share folders are 0770 for nas-users
//	cleanup restores the SMB settings saved by setup and removes shares, data and accounts
//
// Passwords are never printed.
package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
)

const (
	volume   = "/srv/data"
	share    = "nrt-smb"
	hidden   = "nrt-smbh"
	origFile = "/tmp/nrt-smb-orig.json"
)

var (
	fails    int
	accounts = []string{"nrt-ma", "nrt-mu", "nrt-mr", "nrt-mn"}
)

func helper(op string, args any) (json.RawMessage, error) {
	c, err := net.Dial("unix", "/run/nas-revival-helper/helper.sock")
	if err != nil {
		return nil, err
	}
	defer c.Close()
	a, _ := json.Marshal(args)
	b, _ := json.Marshal(map[string]any{"op": op, "args": json.RawMessage(a), "user": "w25-e2e"})
	c.Write(append(b, '\n'))
	var r struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  any             `json:"error"`
	}
	if err := json.NewDecoder(c).Decode(&r); err != nil {
		return nil, err
	}
	if !r.OK {
		return nil, fmt.Errorf("%s: %v", op, r.Error)
	}
	return r.Result, nil
}

func must(op string, args any) json.RawMessage {
	r, err := helper(op, args)
	if err != nil {
		panic(err)
	}
	return r
}

var cl = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}

func req(method, path string, body any, cookie string) (int, []byte, string) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r, _ := http.NewRequest(method, "https://127.0.0.1:8443/api/v1"+path, rd)
	r.Header.Set("X-Requested-With", "nas-revival")
	r.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		r.Header.Set("Cookie", cookie)
	}
	resp, err := cl.Do(r)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	ck, _, _ := strings.Cut(resp.Header.Get("Set-Cookie"), ";")
	return resp.StatusCode, b, ck
}

func check(what string, got, want any) {
	ok := reflect.DeepEqual(got, want)
	if !ok {
		fails++
	}
	fmt.Println(map[bool]string{true: "ok  ", false: "FAIL"}[ok], what, got)
}

func passwords(n int) []string {
	sc := bufio.NewScanner(os.Stdin)
	var out []string
	for len(out) < n && sc.Scan() {
		out = append(out, sc.Text())
	}
	if len(out) != n {
		panic("expected passwords on stdin")
	}
	return out
}

func login(pw string) string {
	code, _, ck := req("POST", "/session", map[string]string{"username": "nrt-ma", "password": pw}, "")
	check("login nrt-ma", code, 204)
	return ck
}

type smbService struct {
	Installed, Running, NetbiosRunning, Managed bool
	Settings                                    map[string]any
}

func setup() {
	pw := passwords(4)
	orig := must("smb.get", nil)
	if err := os.WriteFile(origFile, orig, 0o600); err != nil {
		panic(err)
	}
	roles := []string{"admin", "user", "user", "user"}
	for i, n := range accounts {
		must("user.create", map[string]any{"name": n, "password": pw[i], "role": roles[i]})
	}
	var ul struct {
		Users []struct {
			Name     string
			Password struct{ Samba string }
		}
	}
	json.Unmarshal(must("user.list", nil), &ul)
	for _, u := range ul.Users {
		if strings.HasPrefix(u.Name, "nrt-m") {
			check("samba password "+u.Name, u.Password.Samba, "set")
		}
	}
	perm := func(n, a string) map[string]string { return map[string]string{"kind": "user", "name": n, "access": a} }
	must("share.create", map[string]any{"share": map[string]any{"name": share, "volume": volume, "folder": share,
		"permissions": []any{perm("nrt-mu", "rw"), perm("nrt-mr", "ro"), perm("nrt-mn", "none")},
		"recycle":     map[string]any{"enabled": true, "days": 30}}})
	must("share.create", map[string]any{"share": map[string]any{"name": hidden, "volume": volume, "folder": hidden,
		"hidden": true, "readOnly": true, "permissions": []any{perm("nrt-mu", "rw")}}})

	ck := login(pw[0])
	code, _, _ := req("PUT", "/services/smb", map[string]any{"enabled": true, "workgroup": "WG", "minProtocol": "NT1"}, ck)
	check("SMB1 rejected", code, 400)
	code, b, _ := req("PUT", "/services/smb", map[string]any{"enabled": true, "workgroup": "nrtest",
		"description": "NAS Revival e2e", "minProtocol": "SMB3_00"}, ck)
	check("enable SMB", code, 200)
	var st smbService
	json.Unmarshal(b, &st)
	check("running+managed", []bool{st.Running, st.Managed, st.NetbiosRunning}, []bool{true, true, false})
	check("workgroup normalised", st.Settings["workgroup"], "NRTEST")
	var sl struct {
		Smb struct{ Installed, Included bool }
	}
	json.Unmarshal(must("share.list", nil), &sl)
	check("shares included in smb.conf", sl.Smb.Included, true)
}

func status() {
	pw := passwords(1)
	ck := login(pw[0])
	code, b, _ := req("GET", "/services/smb/status", nil, ck)
	check("status", code, 200)
	var st struct {
		Sessions []struct {
			User    string
			Dialect string
			Shares  []string
		}
		OpenFiles []struct{ Share, Path, User, Access string }
	}
	json.Unmarshal(b, &st)
	found := false
	for _, s := range st.Sessions {
		if s.User == "nrt-mu" {
			found = true
			check("session dialect is SMB3", strings.HasPrefix(s.Dialect, "SMB3"), true)
			check("session share", s.Shares, []string{share})
		}
	}
	check("session of nrt-mu listed", found, true)
	open := false
	for _, f := range st.OpenFiles {
		if f.User == "nrt-mu" && f.Path == "e2e-upload.txt" {
			open = true
			check("open file share", f.Share, share)
			check("open file access has read", strings.Contains(f.Access, "R"), true)
		}
	}
	check("open file of nrt-mu listed", open, true)
	if !found || len(st.OpenFiles) == 0 {
		fmt.Println("     status:", string(b))
	}
}

func verify() {
	_, err := os.Stat(volume + "/" + share + "/e2e-upload.txt")
	check("upload on disk", err == nil, true)
	_, err = os.Stat(volume + "/" + share + "/.recycle/nrt-mu/e2e-delete.txt")
	check("deleted file in recycle bin", err == nil, true)
	_, err = os.Stat(volume + "/" + share + "/e2e-delete.txt")
	check("deleted file gone", os.IsNotExist(err), true)
}

func cleanup() {
	if b, err := os.ReadFile(origFile); err == nil {
		var o struct{ Settings json.RawMessage }
		if json.Unmarshal(b, &o) == nil {
			if _, err := helper("smb.set", map[string]any{"settings": o.Settings}); err != nil {
				fmt.Println("FAIL restore smb settings:", err)
				fails++
			}
		}
		os.Remove(origFile)
	}
	for _, s := range []string{share, hidden} {
		helper("share.delete", map[string]any{"name": s, "deleteData": true, "confirmName": s})
	}
	for _, n := range accounts {
		helper("user.delete", map[string]any{"name": n})
	}
	fmt.Println("cleanup done")
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: e2e-smb setup|status|verify|cleanup")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "setup":
		setup()
	case "status":
		status()
	case "verify":
		verify()
	case "cleanup":
		cleanup()
	default:
		os.Exit(2)
	}
	if fails > 0 {
		os.Exit(1)
	}
}
