// Command e2e-files is the W4.1 File Station end-to-end check on the NAS (scripts/webui/e2e-files.sh runs
// it as nas-revival). It creates throw-away users nrt-fu (rw) and nrt-fr (ro) and the share nrt-files (with
// a recycle bin) through the helper, then over HTTPS lists, creates, uploads, downloads, renames, moves and
// deletes, checks refused escapes and the read-only user, and streams a big upload while it watches the RSS
// of the web process (TEST_PLAN S3). With -keep it leaves everything for the root-side checks of the script;
// -cleanup removes it. Random passwords, never printed.
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const (
	volume = "/srv/data"
	share  = "nrt-files"
)

var fails int

func helper(op string, args any) json.RawMessage {
	c, err := net.Dial("unix", "/run/nas-revival-helper/helper.sock")
	if err != nil {
		panic(err)
	}
	defer c.Close()
	a, _ := json.Marshal(args)
	b, _ := json.Marshal(map[string]any{"op": op, "args": json.RawMessage(a), "user": "w41-e2e"})
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

func do(method, path string, body io.Reader, size int64, cookie string, json bool) (int, []byte, string) {
	r, _ := http.NewRequest(method, "https://127.0.0.1:8443/api/v1"+path, body)
	r.Header.Set("X-Requested-With", "nas-revival")
	if json {
		r.Header.Set("Content-Type", "application/json")
	}
	if size >= 0 {
		r.ContentLength = size
	}
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

func req(method, path string, body any, cookie string) (int, []byte) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	st, b, _ := do(method, path, rd, -1, cookie, true)
	return st, b
}

func upload(dir, name string, body io.Reader, size int64, cookie string) int {
	st, _, _ := do("PUT", "/files/"+share+"/upload?path="+url.QueryEscape(dir)+"&name="+url.QueryEscape(name), body, size, cookie, false)
	return st
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

func names(b []byte) string {
	var l struct {
		Access  string
		Entries []struct{ Name, Type string }
	}
	json.Unmarshal(b, &l)
	var s []string
	for _, e := range l.Entries {
		s = append(s, e.Name+":"+e.Type)
	}
	return l.Access + " " + strings.Join(s, ",")
}

// rssKiB is VmRSS of the web process.
func rssKiB() int {
	out, err := exec.Command("systemctl", "show", "-p", "MainPID", "--value", "nas-revival").Output()
	if err != nil {
		return -1
	}
	b, err := os.ReadFile("/proc/" + strings.TrimSpace(string(out)) + "/status")
	if err != nil {
		return -1
	}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == "VmRSS:" {
			n, _ := strconv.Atoi(f[1])
			return n
		}
	}
	return -1
}

// failing breaks the upload body like a lost connection.
type failing struct{}

func (failing) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// zeros is an endless reader that never holds the upload in memory.
type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func cleanup() {
	func() {
		defer func() { recover() }()
		helper("share.delete", map[string]any{"name": share, "deleteData": true, "confirmName": share})
	}()
	for _, u := range []string{"nrt-fu", "nrt-fr"} {
		func() { defer func() { recover() }(); helper("user.delete", map[string]string{"name": u}) }()
	}
	fmt.Println("cleanup done")
}

func main() {
	keep := flag.Bool("keep", false, "leave users, share and files for the root-side checks")
	onlyCleanup := flag.Bool("cleanup", false, "only remove what a -keep run left")
	bigMiB := flag.Int64("big", 0, "size of the streamed upload in MiB (0 skips it; heavy on the NAS CPU, run only with the owner's OK)")
	flag.Parse()
	if *onlyCleanup {
		cleanup()
		return
	}
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("PANIC", r)
			fails++
		}
		if !*keep {
			cleanup()
		}
		if fails > 0 {
			os.Exit(1)
		}
	}()
	pw := map[string]string{"nrt-fu": rnd(), "nrt-fr": rnd()}
	for u, p := range pw {
		helper("user.create", map[string]any{"name": u, "fullName": "", "password": p, "groups": []string{}, "role": "user"})
	}
	helper("share.create", map[string]any{"share": map[string]any{"name": share, "description": "e2e", "volume": volume, "folder": share,
		"readOnly": false, "hidden": false, "recycle": map[string]any{"enabled": true, "days": 30},
		"permissions": []map[string]string{{"kind": "user", "name": "nrt-fu", "access": "rw"}, {"kind": "user", "name": "nrt-fr", "access": "ro"}}}})
	ck := map[string]string{}
	for u, p := range pw {
		st, _, c := do("POST", "/session", strings.NewReader(`{"username":"`+u+`","password":"`+p+`"}`), -1, "", true)
		check("login "+u, st, 204)
		ck[u] = c
	}
	rw, ro := ck["nrt-fu"], ck["nrt-fr"]

	st, b := req("GET", "/files", nil, rw)
	check("GET /files as rw user", st == 200 && strings.Contains(string(b), `{"name":"nrt-files","description":"e2e","access":"rw","recycle":true}`), true)
	st, b = req("GET", "/files", nil, ro)
	check("GET /files as ro user", st == 200 && strings.Contains(string(b), `"access":"ro"`), true)
	st, b = req("GET", "/files/"+share+"/list", nil, rw)
	check("empty share", fmt.Sprint(st, " ", names(b)), "200 rw ")

	st, _ = req("POST", "/files/"+share+"/mkdir", map[string]string{"path": "", "name": "Složka"}, rw)
	check("mkdir", st, 204)
	st, _ = req("POST", "/files/"+share+"/mkdir", map[string]string{"path": "", "name": "Složka"}, rw)
	check("mkdir again", st, 409)
	data := make([]byte, 1<<20+123)
	rand.Read(data)
	check("upload 1 MiB", upload("Složka", "data č.bin", bytes.NewReader(data), int64(len(data)), rw), 204)
	check("upload existing", upload("Složka", "data č.bin", bytes.NewReader(data), int64(len(data)), rw), 409)
	// The client dies after 10 of 100 bytes: nothing may stay behind.
	r, _ := http.NewRequest("PUT", "https://127.0.0.1:8443/api/v1/files/"+share+"/upload?path=&name=short.bin", io.MultiReader(bytes.NewReader(data[:10]), failing{}))
	r.Header.Set("X-Requested-With", "nas-revival")
	r.Header.Set("Cookie", rw)
	r.ContentLength = 100
	_, err := cl.Do(r)
	check("interrupted upload fails on the client", err != nil, true)
	time.Sleep(time.Second)
	st, b = req("GET", "/files/"+share+"/list?path="+url.QueryEscape("Složka"), nil, rw)
	check("list after upload", fmt.Sprint(st, " ", names(b)), "200 rw data č.bin:file")
	st, b = req("GET", "/files/"+share+"/list", nil, rw)
	check("short upload left nothing", fmt.Sprint(st, " ", names(b)), "200 rw Složka:dir")
	st, b, _ = do("GET", "/files/"+share+"/download?path="+url.QueryEscape("Složka/data č.bin"), nil, -1, ro, false)
	check("download by ro user", st == 200 && sha256.Sum256(b) == sha256.Sum256(data), true)

	for _, p := range []string{"..", "../..", "/etc", "Složka/../..", ".recycle"} {
		st, _ = req("GET", "/files/"+share+"/list?path="+url.QueryEscape(p), nil, rw)
		check("list "+p+" refused", st, 400)
	}
	st, _, _ = do("GET", "/files/"+share+"/download?path="+url.QueryEscape("../../../etc/shadow"), nil, -1, rw, false)
	check("download escape refused", st, 400)
	st, _ = req("GET", "/files/data/list", nil, rw)
	check("foreign share", st == 404 || st == 200, true)

	st, _ = req("POST", "/files/"+share+"/mkdir", map[string]string{"path": "", "name": "x"}, ro)
	check("ro user mkdir", st, 403)
	check("ro user upload", upload("", "x", bytes.NewReader([]byte("x")), 1, ro), 403)
	st, _ = req("POST", "/files/"+share+"/delete", map[string]any{"paths": []string{"Složka"}}, ro)
	check("ro user delete", st, 403)

	st, _ = req("POST", "/files/"+share+"/rename", map[string]string{"path": "Složka/data č.bin", "name": "renamed.bin"}, rw)
	check("rename", st, 204)
	st, _ = req("POST", "/files/"+share+"/mkdir", map[string]string{"path": "", "name": "Cíl"}, rw)
	check("mkdir target", st, 204)
	st, b = req("POST", "/files/"+share+"/move", map[string]any{"paths": []string{"Složka/renamed.bin", "Složka/none"}, "dest": "Cíl"}, rw)
	check("move (one missing)", fmt.Sprint(st, " ", strings.Contains(string(b), `"done":1`)), "200 true")
	st, b = req("POST", "/files/"+share+"/move", map[string]any{"paths": []string{"Cíl"}, "dest": "Cíl"}, rw)
	check("move into itself refused", fmt.Sprint(st, " ", strings.Contains(string(b), `"done":0`)), "200 true")
	st, b = req("POST", "/files/"+share+"/delete", map[string]any{"paths": []string{"Cíl/renamed.bin"}}, rw)
	check("delete to recycle bin", fmt.Sprint(st, " ", strings.Contains(string(b), `"recycled":true`)), "200 true")
	st, b = req("GET", "/files/"+share+"/list", nil, rw)
	check("bin hidden", fmt.Sprint(st, " ", names(b)), "200 rw Cíl:dir,Složka:dir")

	if *bigMiB > 0 {
		size := *bigMiB << 20
		before := rssKiB()
		peak := before
		done := make(chan int)
		go func() { done <- upload("", "big.bin", io.LimitReader(zeros{}, size), size, rw) }()
		start := time.Now()
	wait:
		for {
			select {
			case st = <-done:
				break wait
			case <-time.After(500 * time.Millisecond):
				peak = max(peak, rssKiB())
			}
		}
		secs := time.Since(start).Seconds()
		check(fmt.Sprintf("big upload %d MiB (%.0f s, %.1f MiB/s)", *bigMiB, secs, float64(*bigMiB)/secs), st, 204)
		fmt.Printf("info web RSS before %d KiB, peak %d KiB\n", before, peak)
		check("RSS growth < 4 MiB", before > 0 && peak-before < 4096, true)
		st, b = req("GET", "/files/"+share+"/list", nil, rw)
		check("big file listed", strings.Contains(string(b), fmt.Sprintf(`"name":"big.bin","type":"file","size":%d`, size)), true)
	}
}
