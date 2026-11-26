// Command e2e-ssh is the end-to-end check of SSH/SFTP (W2.7) on the deployed NAS; scripts/webui/e2e-ssh.sh
// copies it to the NAS and runs it as nas-revival. It creates the throw-away group nrt-sftp and the
// accounts nrt-sa (admin), nrt-su (member of nrt-sftp) and nrt-sn (not a member) through the helper,
// changes the SSH settings over HTTPS as nrt-sa and connects to sshd on 127.0.0.1 with a real SSH
// client: SFTP in the chroot, no TTY and no command for SFTP users, AllowGroups, password login off,
// a port change and back. At the end it restores the previous SSH settings and removes the accounts.
// Passwords are random and never printed. Exit code 0 = all checks passed.
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const group = "nrt-sftp"

var (
	fails    int
	accounts = []string{"nrt-sa", "nrt-su", "nrt-sn"}
	hostKey  ssh.PublicKey
)

func helper(op string, args any) (json.RawMessage, error) {
	c, err := net.Dial("unix", "/run/nas-revival-helper/helper.sock")
	if err != nil {
		return nil, err
	}
	defer c.Close()
	a, _ := json.Marshal(args)
	b, _ := json.Marshal(map[string]any{"op": op, "args": json.RawMessage(a), "user": "w27-e2e"})
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

var cl = &http.Client{Timeout: 60 * time.Second,
	Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}

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

func randomPassword() string {
	b := make([]byte, 18)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

type sshService struct {
	Settings  map[string]any `json:"settings"`
	Managed   bool           `json:"managed"`
	Running   bool           `json:"running"`
	Effective struct {
		Ports        []int `json:"ports"`
		PasswordAuth bool  `json:"passwordAuth"`
	} `json:"effective"`
}

// setSSH changes the settings over HTTPS like the web UI and returns the new state.
func setSSH(cookie string, port int, password bool, groups []string) (int, sshService) {
	code, body, _ := req("PUT", "/services/ssh", map[string]any{"port": port, "passwordAuth": password, "sftpGroups": groups}, cookie)
	var st sshService
	json.Unmarshal(body, &st)
	if code != 200 {
		fmt.Println("     response:", strings.TrimSpace(string(body)))
	}
	return code, st
}

func dial(port int, user, pw string) (*ssh.Client, error) {
	return ssh.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), &ssh.ClientConfig{
		User: user, Auth: []ssh.AuthMethod{ssh.Password(pw)},
		HostKeyCallback: ssh.FixedHostKey(hostKey), HostKeyAlgorithms: []string{hostKey.Type()}, Timeout: 10 * time.Second})
}

// sftpRealpath opens the SFTP subsystem, does the version handshake and
// resolves "." – "/" proves the chroot.
func sftpRealpath(c *ssh.Client) (string, error) {
	s, err := c.NewSession()
	if err != nil {
		return "", err
	}
	defer s.Close()
	in, _ := s.StdinPipe()
	out, _ := s.StdoutPipe()
	if err := s.RequestSubsystem("sftp"); err != nil {
		return "", err
	}
	packet := func(typ byte, payload []byte) {
		h := make([]byte, 5)
		binary.BigEndian.PutUint32(h, uint32(len(payload)+1))
		h[4] = typ
		in.Write(append(h, payload...))
	}
	read := func() (byte, []byte, error) {
		h := make([]byte, 5)
		if _, err := io.ReadFull(out, h); err != nil {
			return 0, nil, err
		}
		b := make([]byte, binary.BigEndian.Uint32(h)-1)
		_, err := io.ReadFull(out, b)
		return h[4], b, err
	}
	packet(1, []byte{0, 0, 0, 3}) // SSH_FXP_INIT v3
	if typ, _, err := read(); err != nil || typ != 2 {
		return "", fmt.Errorf("no SFTP version reply (%d, %v)", typ, err)
	}
	packet(16, []byte{0, 0, 0, 1, 0, 0, 0, 1, '.'}) // SSH_FXP_REALPATH id 1 "."
	typ, b, err := read()
	if err != nil || typ != 104 || len(b) < 12 { // SSH_FXP_NAME
		return "", fmt.Errorf("no SFTP name reply (%d, %v)", typ, err)
	}
	n := binary.BigEndian.Uint32(b[8:12])
	if int(n) > len(b)-12 {
		return "", errors.New("short SFTP name reply")
	}
	return string(b[12 : 12+n]), nil
}

// commandOutput runs a command; for SFTP users ForceCommand replaces it.
func commandOutput(c *ssh.Client, cmd string) string {
	s, err := c.NewSession()
	if err != nil {
		return "session: " + err.Error()
	}
	defer s.Close()
	out, _ := s.Output(cmd)
	return string(out)
}

func ptyRefused(c *ssh.Client) bool {
	s, err := c.NewSession()
	if err != nil {
		return false
	}
	defer s.Close()
	return s.RequestPty("xterm", 24, 80, ssh.TerminalModes{}) != nil
}

// loginFails: the server refused the authentication (not a network or host key problem).
func loginFails(port int, user, pw string) bool {
	c, err := dial(port, user, pw)
	if err == nil {
		c.Close()
		return false
	}
	return strings.Contains(err.Error(), "unable to authenticate")
}

func listening(port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 3*time.Second)
	if err == nil {
		c.Close()
	}
	return err == nil
}

func run() {
	pub, err := os.ReadFile("/etc/ssh/ssh_host_ed25519_key.pub")
	if err != nil {
		panic(err)
	}
	if hostKey, _, _, _, err = ssh.ParseAuthorizedKey(pub); err != nil {
		panic(err)
	}
	var orig sshService
	json.Unmarshal(must("ssh.get", nil), &orig)
	port := int(orig.Settings["port"].(float64))
	pw := map[string]string{}
	defer cleanup(orig)

	must("group.create", map[string]any{"name": group})
	for i, n := range accounts {
		pw[n] = randomPassword()
		role := map[bool]string{true: "admin", false: "user"}[i == 0]
		must("user.create", map[string]any{"name": n, "password": pw[n], "role": role})
	}
	must("group.setMembers", map[string]any{"name": group, "members": []string{"nrt-su"}})

	code, _, ck := req("POST", "/session", map[string]string{"username": "nrt-sa", "password": pw["nrt-sa"]}, "")
	check("login nrt-sa", code, 204)
	code, st := setSSH(ck, port, true, []string{group})
	check("enable SFTP for "+group, code, 200)
	check("managed after save", st.Managed, true)
	code, _ = setSSH(ck, port, true, []string{"sudo"})
	check("SFTP for a system group refused", code, 400)
	code, _ = setSSH(ck, 8443, true, nil)
	check("web UI port refused", code, 400)

	c, err := dial(port, "nrt-su", pw["nrt-su"])
	check("SFTP user logs in with password", fmt.Sprint(err), "<nil>")
	if err == nil {
		p, err := sftpRealpath(c)
		check("SFTP works, chroot is /", fmt.Sprint(p, err), "/<nil>")
		check("no command for SFTP user", strings.Contains(commandOutput(c, "id"), "uid="), false)
		check("no TTY for SFTP user", ptyRefused(c), true)
		c.Close()
	}
	check("wrong password refused", loginFails(port, "nrt-su", "wrong-password-123"), true)
	check("user outside SFTP groups refused", loginFails(port, "nrt-sn", pw["nrt-sn"]), true)

	code, _ = setSSH(ck, port, false, []string{group})
	check("password login off", code, 200)
	check("SFTP password refused when off", loginFails(port, "nrt-su", pw["nrt-su"]), true)

	alt := 2222
	if port == alt {
		alt = 2223
	}
	code, st = setSSH(ck, alt, true, []string{group})
	check(fmt.Sprintf("port %d", alt), code, 200)
	time.Sleep(time.Second)
	check("sshd listens on the new port", listening(alt), true)
	check("effective port", st.Effective.Ports, []int{alt})
	code, _ = setSSH(ck, port, true, []string{group})
	check(fmt.Sprintf("port back to %d", port), code, 200)
	time.Sleep(time.Second)
	check("sshd listens on the old port again", listening(port), true)
	check("new port closed again", listening(alt), false)
}

func cleanup(orig sshService) {
	s := orig.Settings
	if _, err := helper("ssh.set", map[string]any{"settings": s}); err != nil {
		fails++
		fmt.Println("FAIL restore ssh settings:", err)
	}
	for _, n := range accounts {
		helper("user.delete", map[string]any{"name": n})
	}
	helper("group.delete", map[string]any{"name": group})
	fmt.Println("cleanup done")
}

func main() {
	func() {
		defer func() {
			if r := recover(); r != nil {
				fails++
				fmt.Println("FAIL", r)
			}
		}()
		run()
	}()
	if fails > 0 {
		fmt.Printf("%d check(s) failed\n", fails)
		os.Exit(1)
	}
	fmt.Println("all checks passed")
}
