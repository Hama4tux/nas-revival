package smb

import (
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// testdata/smbstatus.json is Samba 4.22 output from the W2.5 e2e run on the NAS, addresses replaced
// with documentation ones, plus a second session and file.
func TestParseStatus(t *testing.T) {
	b, err := os.ReadFile("testdata/smbstatus.json")
	if err != nil {
		t.Fatal(err)
	}
	st, err := ParseStatus(b, map[string]string{"/srv/data/nrt-smb": "nrt-smb"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Session{
		{ID: "77", User: "nrt-mr", Machine: "192.0.2.11", Dialect: "SMB3_00", Encryption: "AES-128-CCM", Signing: "AES-128-CMAC",
			Since: "2026-11-26T08:10:00+01:00", Shares: []string{"dokumenty", "fotky"}},
		{ID: "3091240466", User: "nrt-mu", Machine: "192.0.2.10", Dialect: "SMB3_11", Encryption: "", Signing: "AES-128-GMAC",
			Since: "2026-11-26T08:17:08.512345+01:00", Shares: []string{"nrt-smb"}},
	}
	if !reflect.DeepEqual(st.Sessions, want) {
		t.Fatalf("sessions\n got %+v\nwant %+v", st.Sessions, want)
	}
	wantFiles := []OpenFile{
		// Unknown share path is shown as is; unknown uid as a number.
		{Share: "/srv/data/fotky", Path: "a.jpg", User: "4242", Access: "W", Since: "2026-11-26T08:12:00+01:00"},
		{Share: "/srv/data/fotky", Path: "a.jpg", User: "nrt-mr", Access: "R", Since: "2026-11-26T08:11:00+01:00"},
		{Share: "nrt-smb", Path: "e2e-upload.txt", User: "nrt-mu", Access: "RW", Since: "2026-11-26T08:17:08.687188+01:00"},
	}
	if !reflect.DeepEqual(st.OpenFiles, wantFiles) {
		t.Fatalf("files\n got %+v\nwant %+v", st.OpenFiles, wantFiles)
	}
	if st.Truncated {
		t.Fatal("truncated")
	}
}

func TestParseStatusEmptyAndCapped(t *testing.T) {
	st, err := ParseStatus([]byte(`{"sessions": {}, "tcons": {}}`), nil)
	if err != nil || st.Sessions == nil || st.OpenFiles == nil || len(st.Sessions) != 0 {
		t.Fatalf("%+v %v", st, err)
	}
	var b strings.Builder
	b.WriteString(`{"sessions": {`)
	for i := 0; i < MaxRows+5; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`"` + strconv.Itoa(i) + `": {"session_id": "` + strconv.Itoa(i) + `", "username": "u"}`)
	}
	b.WriteString(`}}`)
	st, err = ParseStatus([]byte(b.String()), nil)
	if err != nil || len(st.Sessions) != MaxRows || !st.Truncated {
		t.Fatalf("len %d truncated %v err %v", len(st.Sessions), st.Truncated, err)
	}
	if _, err := ParseStatus([]byte("not json"), nil); err == nil {
		t.Fatal("garbage accepted")
	}
}
