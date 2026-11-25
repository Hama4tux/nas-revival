package web

import (
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestHandler(t *testing.T) {
	dist := fstest.MapFS{
		"index.html":      {Data: []byte("index")},
		"assets/app-1.js": {Data: []byte("js")},
	}
	h := Handler(dist)
	cases := []struct {
		path, body string
		code       int
		cache      string
	}{
		{"/", "index", 200, "no-cache"},
		{"/storage", "index", 200, "no-cache"},
		{"/assets/app-1.js", "js", 200, "public, max-age=31536000, immutable"},
		{"/assets/missing.js", "", 404, ""},
		{"/../../etc/passwd", "index", 200, "no-cache"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", c.path, nil))
		if rec.Code != c.code || (c.body != "" && rec.Body.String() != c.body) || rec.Header().Get("Cache-Control") != c.cache {
			t.Errorf("%s: %d %q cache=%q", c.path, rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
		}
	}
}
