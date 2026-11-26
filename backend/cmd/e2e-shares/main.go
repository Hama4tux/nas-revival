// Command e2e-shares is the W2.3 end-to-end check on the NAS (scripts/webui/e2e-shares.sh runs it as
// nas-revival). It creates a throw-away admin (nrt-sa) and user (nrt-su) through the helper, then over
// HTTPS creates, edits and deletes the share nrt-e2e on /srv/data (data included, typed confirmation),
// checks the folder on disk and the role policy, and removes everything again. Random passwords, never printed.
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
	"syscall"
	"time"
)

const (
	volume = "/srv/data"
	name   = "nrt-e2e"
	dir    = volume + "/" + name
)

var fails int

func helper(op string, args any) json.RawMessage {
	c, err := net.Dial("unix", "/run/nas-revival-helper/helper.sock")
	if err != nil {
		panic(err)
	}
	defer c.Close()
	a, _ := json.Marshal(args)
	b, _ := json.Marshal(map[string]any{"op": op, "args": json.RawMessage(a), "user": "w23-e2e"})
	c.Write(append(b, '\n'))
	var r struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  any             `json:"error"`
	}
	if err := json.NewDecoder(c).Decode(&r); err != nil || !r.OK {
		panic(fmt.Sprintf("helper %s: %v %v", op, err, r.Error))
	}
	return r.Result
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

func rnd() string {
	b := make([]byte, 18)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

type shareInfo struct {
	Name        string
	Hidden      bool
	Exists      bool
	Enforcement string
	Recycle     struct {
		Enabled bool
		Days    int
	}
	Effective []struct{ User, Access string }
}

func findShare(b []byte) *shareInfo {
	var l struct{ Shares []shareInfo }
	json.Unmarshal(b, &l)
	for i := range l.Shares {
		if l.Shares[i].Name == name {
			return &l.Shares[i]
		}
	}
	return nil
}

func accessOf(s *shareInfo, user string) string {
	for _, e := range s.Effective {
		if e.User == user {
			return e.Access
		}
	}
	return "missing"
}

func main() {
	roles := map[string]string{"nrt-sa": "admin", "nrt-su": "user"}
	pw := map[string]string{}
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("PANIC", r)
			fails++
		}
		func() {
			defer func() { recover() }()
			helper("share.delete", map[string]any{"name": name, "deleteData": true, "confirmName": name})
		}()
		for u := range roles {
			func() { defer func() { recover() }(); helper("user.delete", map[string]string{"name": u}) }()
		}
		time.Sleep(time.Second)
		_, err := os.Lstat(dir)
		check("cleanup: folder gone", os.IsNotExist(err), true)
		if fails > 0 {
			os.Exit(1)
		}
	}()
	for u, r := range roles {
		pw[u] = rnd()
		helper("user.create", map[string]any{"name": u, "fullName": "", "password": pw[u], "groups": []string{}, "role": r})
	}
	ck := map[string]string{}
	for u := range roles {
		st, _, c := req("POST", "/session", map[string]string{"username": u, "password": pw[u]}, "")
		check("login "+u, st, 204)
		ck[u] = c
	}
	a := ck["nrt-sa"]
	st, b, _ := req("GET", "/shares", nil, a)
	check("GET /shares as admin", st, 200)
	var l struct{ Volumes []struct{ Path string } }
	json.Unmarshal(b, &l)
	vols := []string{}
	for _, v := range l.Volumes {
		vols = append(vols, v.Path)
	}
	check("data volume listed", strings.Contains(strings.Join(vols, " "), volume), true)
	st, _, _ = req("GET", "/shares", nil, ck["nrt-su"])
	check("GET /shares as user", st, 403)

	share := map[string]any{"name": name, "description": "e2e", "volume": volume, "folder": name,
		"readOnly": false, "hidden": false, "recycle": map[string]any{"enabled": false, "days": 30},
		"permissions": []map[string]string{{"kind": "user", "name": "nrt-su", "access": "ro"}}}
	st, _, _ = req("POST", "/shares", map[string]any{"name": "../etc", "volume": volume}, a)
	check("traversal name refused", st, 400)
	st, _, _ = req("POST", "/shares", map[string]any{"name": "x", "volume": "/", "folder": "x",
		"recycle": map[string]any{"enabled": false, "days": 30}, "permissions": []any{}}, a)
	check("root FS volume refused", st, 400)
	st, b, _ = req("POST", "/shares", share, a)
	check("create share", st, 201)
	s := findShare(b)
	if s == nil {
		panic("share not listed")
	}
	check("folder exists", s.Exists, true)
	check("nrt-su effective", accessOf(s, "nrt-su"), "ro")
	check("nrt-sa effective", accessOf(s, "nrt-sa"), "none")
	if fi, err := os.Lstat(dir); err == nil {
		st := fi.Sys().(*syscall.Stat_t)
		check("folder owner uid", st.Uid, uint32(0))
		check("folder mode ("+s.Enforcement+")", fmt.Sprintf("%o", fi.Mode().Perm()), "770")
		if s.Enforcement == "smb" {
			check("folder group nas-users", st.Gid == st.Uid, false)
		}
	} else {
		check("folder on disk", err.Error(), "")
	}
	st, _, _ = req("POST", "/shares", share, a)
	check("duplicate refused", st, 400)

	share["hidden"] = true
	share["recycle"] = map[string]any{"enabled": true, "days": 7}
	share["permissions"] = []map[string]string{{"kind": "group", "name": "nas-users", "access": "rw"},
		{"kind": "user", "name": "nrt-su", "access": "none"}}
	st, b, _ = req("PUT", "/shares/"+name, share, a)
	check("update share", st, 200)
	if s = findShare(b); s != nil {
		check("hidden + recycle 7", []any{s.Hidden, s.Recycle.Enabled, s.Recycle.Days}, []any{true, true, 7})
		check("nrt-su denied", accessOf(s, "nrt-su"), "none")
		check("nrt-sa via nas-users", accessOf(s, "nrt-sa"), "rw")
	}
	st, _, _ = req("POST", "/shares/recycle-cleanup", nil, a)
	check("recycle cleanup", st, 200)

	st, _, _ = req("DELETE", "/shares/"+name, map[string]any{"deleteData": true, "confirmName": "NRT-E2E"}, a)
	check("delete with wrong confirmation", st, 400)
	_, err := os.Lstat(dir)
	check("folder kept after refusal", err == nil, true)
	st, b, _ = req("DELETE", "/shares/"+name, map[string]any{"deleteData": true, "confirmName": name}, a)
	check("delete with data", st, 200)
	var d struct{ DataRemoval string }
	json.Unmarshal(b, &d)
	check("data removal", d.DataRemoval, "started")
	check("share gone from list", findShare(b) == nil, true)
}
