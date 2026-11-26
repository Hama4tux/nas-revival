// Command e2e-roles is the W2.2 end-to-end check on the NAS (scripts/webui/e2e-roles.sh runs it as
// nas-revival). It creates three throw-away accounts (admin, viewer, user) through the helper, logs
// in over HTTPS, checks the role policy, own password change, role change and disable, and deletes
// the accounts again. Random passwords, never printed.
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
)

var fails int

func helper(op string, args any) json.RawMessage {
	c, err := net.Dial("unix", "/run/nas-revival-helper/helper.sock")
	if err != nil {
		panic(err)
	}
	defer c.Close()
	a, _ := json.Marshal(args)
	b, _ := json.Marshal(map[string]any{"op": op, "args": json.RawMessage(a), "user": "w22-e2e"})
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

func check(name string, got, want any) {
	ok := reflect.DeepEqual(got, want)
	if !ok {
		fails++
	}
	fmt.Println(map[bool]string{true: "ok  ", false: "FAIL"}[ok], name, got)
}

func rnd() string {
	b := make([]byte, 18)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func main() {
	roles := map[string]string{"nrt-a": "admin", "nrt-v": "viewer", "nrt-u": "user"}
	pw := map[string]string{}
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("PANIC", r)
			fails++
		}
		for u := range roles {
			func() { defer func() { recover() }(); helper("user.delete", map[string]string{"name": u}) }()
		}
		var l struct{ Users []struct{ Name string } }
		json.Unmarshal(helper("user.list", struct{}{}), &l)
		left := []string{}
		for _, x := range l.Users {
			if roles[x.Name] != "" {
				left = append(left, x.Name)
			}
		}
		check("cleanup", left, []string{})
		if fails > 0 {
			os.Exit(1)
		}
	}()
	for u, r := range roles {
		pw[u] = rnd()
		helper("user.create", map[string]any{"name": u, "fullName": "", "password": pw[u], "groups": []string{}, "role": r})
	}
	var l struct{ Users []struct{ Name, Role string } }
	json.Unmarshal(helper("user.list", struct{}{}), &l)
	got := map[string]string{}
	for _, x := range l.Users {
		if roles[x.Name] != "" {
			got[x.Name] = x.Role
		}
	}
	check("roles listed", got, roles)
	ck := map[string]string{}
	for u := range roles {
		st, _, c := req("POST", "/session", map[string]string{"username": u, "password": pw[u]}, "")
		check("login "+u, st, 204)
		ck[u] = c
	}
	_, b, _ := req("GET", "/session", nil, ck["nrt-v"])
	var s struct{ Role string }
	json.Unmarshal(b, &s)
	check("session role viewer", s.Role, "viewer")
	for u, want := range map[string]int{"nrt-a": 200, "nrt-v": 200, "nrt-u": 403} {
		st, _, _ := req("GET", "/users", nil, ck[u])
		check("GET /users as "+u, st, want)
	}
	st, _, _ := req("POST", "/groups", map[string]string{"name": "nrt-g"}, ck["nrt-v"])
	check("POST /groups as viewer", st, 403)
	st, _, _ = req("PUT", "/settings/time", map[string]any{"ntpServers": []string{}}, ck["nrt-u"])
	check("PUT time as user", st, 403)
	st, _, _ = req("PUT", "/session/password", map[string]string{"current": strings.Repeat("x", 12), "password": strings.Repeat("y", 14)}, ck["nrt-u"])
	check("wrong own password", st, 400)
	nw := rnd()
	st, _, _ = req("PUT", "/session/password", map[string]string{"current": pw["nrt-u"], "password": nw}, ck["nrt-u"])
	check("own password change", st, 200)
	st, _, _ = req("GET", "/session", nil, ck["nrt-u"])
	check("session after own change", st, 401)
	st, _, ck["nrt-u"] = req("POST", "/session", map[string]string{"username": "nrt-u", "password": nw}, "")
	check("login with new password", st, 204)
	st, _, _ = req("PUT", "/users/nrt-v", map[string]any{"fullName": "", "groups": []string{}, "role": "user"}, ck["nrt-a"])
	check("admin demotes viewer", st, 200)
	st, _, _ = req("GET", "/session", nil, ck["nrt-v"])
	check("viewer session ended", st, 401)
	st, _, _ = req("PUT", "/users/nrt-a", map[string]any{"fullName": "", "groups": []string{}, "role": "viewer"}, ck["nrt-a"])
	check("self demotion refused", st, 400)
	helper("user.setDisabled", map[string]any{"name": "nrt-u", "disabled": true})
	st, _, _ = req("GET", "/session", nil, ck["nrt-u"])
	check("disabled user's session", st, 401)
	st, _, _ = req("POST", "/session", map[string]string{"username": "nrt-u", "password": nw}, "")
	check("disabled user login", st, 401)
}
