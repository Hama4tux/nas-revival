package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nas-revival/backend/internal/helper"
)

// fakeFiles records calls with the session user and writes uploads to dir.
type fakeFiles struct {
	mu        sync.Mutex
	dir       string
	calls     []string
	err       error
	block     chan struct{} // FileUploadOpen waits on it when set
	aborted   int
	committed []string
}

func (f *fakeFiles) log(ctx context.Context, s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, _ := ctx.Value(ctxKey{}).(sessionCtx)
	f.calls = append(f.calls, u.username+" "+s)
}

func (f *fakeFiles) FileShares(ctx context.Context) (helper.FileShareList, error) {
	f.log(ctx, "shares")
	return helper.FileShareList{Shares: []helper.FileShare{{Name: "data", Access: "rw"}}}, f.err
}
func (f *fakeFiles) FileList(ctx context.Context, share, path string, offset int) (helper.FileList, error) {
	f.log(ctx, "list "+share+" "+path)
	return helper.FileList{Share: share}, f.err
}
func (f *fakeFiles) FileOpen(ctx context.Context, share, path string) (*os.File, helper.BackupFileMeta, error) {
	f.log(ctx, "open "+share+" "+path)
	if f.err != nil {
		return nil, helper.BackupFileMeta{}, f.err
	}
	p := filepath.Join(f.dir, "dl")
	os.WriteFile(p, []byte("content"), 0o600)
	fh, err := os.Open(p)
	return fh, helper.BackupFileMeta{Name: "Zpráva č.1.pdf", Size: 7, Modified: time.Unix(1700000000, 0)}, err
}
func (f *fakeFiles) FileMkdir(ctx context.Context, share, dir, name string) error {
	f.log(ctx, "mkdir "+share+" "+dir+" "+name)
	return f.err
}
func (f *fakeFiles) FileRename(ctx context.Context, share, path, name string) error {
	f.log(ctx, "rename "+share+" "+path+" "+name)
	return f.err
}
func (f *fakeFiles) FileMove(ctx context.Context, share string, paths []string, dest string) (helper.FileBatch, error) {
	f.log(ctx, "move "+share+" "+strings.Join(paths, ",")+" "+dest)
	return helper.FileBatch{Done: len(paths), Failed: []helper.FileItemError{}}, f.err
}
func (f *fakeFiles) FileDelete(ctx context.Context, share string, paths []string) (helper.FileBatch, error) {
	f.log(ctx, "delete "+share+" "+strings.Join(paths, ","))
	return helper.FileBatch{Done: len(paths), Failed: []helper.FileItemError{}}, f.err
}
func (f *fakeFiles) FileUploadOpen(ctx context.Context, share, dir, name string, size int64, overwrite bool) (*os.File, string, error) {
	f.log(ctx, "upload "+share+" "+dir+" "+name)
	if f.block != nil {
		<-f.block
	}
	if f.err != nil {
		return nil, "", f.err
	}
	fh, err := os.Create(filepath.Join(f.dir, "tmp"))
	return fh, "0123456789abcdef0123456789abcdef", err
}
func (f *fakeFiles) FileUploadCommit(ctx context.Context, share, dir, token, name string, overwrite bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.committed = append(f.committed, name)
	return nil
}
func (f *fakeFiles) FileUploadAbort(ctx context.Context, share, dir, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.aborted++
	return nil
}

func TestFilesRoutesForEveryRole(t *testing.T) {
	s, h, _, _ := newRoleServer(t)
	ff := &fakeFiles{dir: t.TempDir()}
	s.Files = ff
	ck := loginAs(t, h, "eva")
	cases := []struct {
		method, path, body string
		want               int
		call               string
	}{
		{"GET", "/api/v1/files", "", 200, "eva shares"},
		{"GET", "/api/v1/files/data/list?path=docs%2Fsub&offset=200", "", 200, "eva list data docs/sub"},
		{"GET", "/api/v1/files/data/list?offset=-1", "", 400, ""},
		{"POST", "/api/v1/files/data/mkdir", `{"path":"docs","name":"nová"}`, 204, "eva mkdir data docs nová"},
		{"POST", "/api/v1/files/data/rename", `{"path":"a.txt","name":"b.txt"}`, 204, "eva rename data a.txt b.txt"},
		{"POST", "/api/v1/files/data/move", `{"paths":["a","b"],"dest":"c"}`, 200, "eva move data a,b c"},
		{"POST", "/api/v1/files/data/delete", `{"paths":["a"]}`, 200, "eva delete data a"},
		{"POST", "/api/v1/files/data/delete", `{"paths":["a"],"dest":"x"}`, 400, ""},
		{"POST", "/api/v1/files/data/mkdir", `{"path":"docs","name":"x","extra":1}`, 400, ""},
		{"POST", "/api/v1/files/data/delete", `{"paths":["` + strings.Repeat("x", fileBodyLimit) + `"]}`, 413, ""},
	}
	for i, c := range cases {
		n := len(ff.calls)
		rec := do(h, c.method, c.path, c.body, xrw, ck)
		if rec.Code != c.want {
			t.Errorf("case %d %s %s = %d want %d (%s)", i, c.method, c.path, rec.Code, c.want, rec.Body)
		}
		got := ""
		if len(ff.calls) > n {
			got = ff.calls[n]
		}
		if got != c.call {
			t.Errorf("case %d call = %q want %q", i, got, c.call)
		}
	}
	// Without a session nothing reaches the helper; without the header neither.
	n := len(ff.calls)
	if rec := do(h, "GET", "/api/v1/files", "", xrw, nil); rec.Code != 401 {
		t.Errorf("no session = %d", rec.Code)
	}
	if rec := do(h, "POST", "/api/v1/files/data/delete", `{"paths":["a"]}`, map[string]string{"Content-Type": "application/json"}, ck); rec.Code != 403 {
		t.Errorf("no CSRF header = %d", rec.Code)
	}
	if len(ff.calls) != n {
		t.Errorf("calls = %v", ff.calls[n:])
	}
}

func TestFilesHelperErrors(t *testing.T) {
	s, h, _, _ := newRoleServer(t)
	ff := &fakeFiles{dir: t.TempDir()}
	s.Files = ff
	ck := loginAs(t, h, "eva")
	for code, want := range map[string]int{helper.CodeForbidden: 403, helper.CodeFileExists: 409, helper.CodeNotFound: 404, helper.CodeInvalid: 400} {
		ff.err = &helper.Error{Code: code, Message: "x"}
		if rec := do(h, "POST", "/api/v1/files/data/mkdir", `{"path":"","name":"a"}`, xrw, ck); rec.Code != want {
			t.Errorf("%s = %d want %d", code, rec.Code, want)
		}
	}
}

func TestFilesDownload(t *testing.T) {
	s, h, _, _ := newRoleServer(t)
	s.Files = &fakeFiles{dir: t.TempDir()}
	rec := do(h, "GET", "/api/v1/files/data/download?path=a%2Fb.pdf", "", xrw, loginAs(t, h, "eva"))
	if rec.Code != 200 || rec.Body.String() != "content" {
		t.Fatalf("%d %q", rec.Code, rec.Body)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "filename*=UTF-8''Zpr%C3%A1va%20%C4%8D.1.pdf") {
		t.Errorf("Content-Disposition = %s", cd)
	}
}

func upload(h http.Handler, ck *http.Cookie, query string, body io.Reader, size int64) *httptest.ResponseRecorder {
	req := httptest.NewRequest("PUT", "https://nas.test/api/v1/files/data/upload?"+query, body)
	req.ContentLength = size
	req.Header.Set("X-Requested-With", "nas-revival")
	req.AddCookie(ck)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestFilesUploadStreamsAndCommits(t *testing.T) {
	s, h, _, _ := newRoleServer(t)
	ff := &fakeFiles{dir: t.TempDir()}
	s.Files = ff
	ck := loginAs(t, h, "eva")
	rec := upload(h, ck, "path=docs&name=a.bin", strings.NewReader("abcdef"), 6)
	if rec.Code != 204 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if b, _ := os.ReadFile(filepath.Join(ff.dir, "tmp")); string(b) != "abcdef" || len(ff.committed) != 1 {
		t.Errorf("stored %q commits %v", b, ff.committed)
	}
	// A body shorter than announced is aborted, never committed.
	rec = upload(h, ck, "name=b.bin", strings.NewReader("abc"), 6)
	if rec.Code != 400 || ff.aborted != 1 || len(ff.committed) != 1 {
		t.Errorf("short body = %d aborted %d commits %v", rec.Code, ff.aborted, ff.committed)
	}
	if rec := upload(h, ck, "name=a%2Fb", strings.NewReader("a"), 1); rec.Code != 400 {
		t.Errorf("bad name = %d", rec.Code)
	}
	if rec := upload(h, ck, "name=c", strings.NewReader("a"), -1); rec.Code != 411 {
		t.Errorf("no length = %d", rec.Code)
	}
}

func TestFilesUploadLimit(t *testing.T) {
	s, h, _, _ := newRoleServer(t)
	ff := &fakeFiles{dir: t.TempDir(), block: make(chan struct{}), err: &helper.Error{Code: helper.CodeInvalid, Message: "x"}}
	s.Files = ff
	ck := loginAs(t, h, "eva")
	var wg sync.WaitGroup
	for range MaxUploads {
		wg.Add(1)
		go func() { defer wg.Done(); upload(h, ck, "name=a", strings.NewReader("a"), 1) }()
	}
	for s.uploads.Load() < MaxUploads {
		time.Sleep(time.Millisecond)
	}
	if rec := upload(h, ck, "name=a", strings.NewReader("a"), 1); rec.Code != 429 {
		t.Errorf("third upload = %d", rec.Code)
	}
	close(ff.block)
	wg.Wait()
	if s.uploads.Load() != 0 {
		t.Errorf("slots left = %d", s.uploads.Load())
	}
}
