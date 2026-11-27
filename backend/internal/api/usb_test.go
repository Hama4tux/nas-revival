package api

import (
	"context"
	"net/http"
	"testing"

	"nas-revival/backend/internal/helper"
)

type fakeUsb struct{ calls []string }

func (f *fakeUsb) UsbList(context.Context) (helper.UsbList, error) {
	f.calls = append(f.calls, "list")
	return helper.UsbList{Disks: []helper.UsbDisk{}}, nil
}
func (f *fakeUsb) UsbMount(_ context.Context, d, p string, ro bool) (helper.UsbList, error) {
	if ro {
		p += " ro"
	}
	f.calls = append(f.calls, "mount "+d+" "+p)
	return helper.UsbList{}, nil
}
func (f *fakeUsb) UsbUnmount(_ context.Context, d, p string) (helper.UsbList, error) {
	f.calls = append(f.calls, "unmount "+d+" "+p)
	return helper.UsbList{}, nil
}
func (f *fakeUsb) UsbEject(_ context.Context, d string) (helper.UsbList, error) {
	f.calls = append(f.calls, "eject "+d)
	return helper.UsbList{}, &helper.Error{Code: helper.CodeUsbBusy, Message: "busy"}
}

func (f *fakeUsb) UsbCopyGet(context.Context) (helper.UsbCopyInfo, error) {
	f.calls = append(f.calls, "copy.get")
	return helper.UsbCopyInfo{Share: "data", Folder: "usb-backups", Dest: "/srv/data/data/usb-backups"}, nil
}
func (f *fakeUsb) UsbCopySet(_ context.Context, s, d string) (helper.UsbCopyInfo, error) {
	f.calls = append(f.calls, "copy.set "+s+"/"+d)
	return helper.UsbCopyInfo{Share: s, Folder: d}, nil
}

func TestUsbRoutesRoles(t *testing.T) {
	s, h, _, _ := newRoleServer(t)
	fu := &fakeUsb{}
	s.Usb = fu
	ck := map[string]*http.Cookie{"jana": loginAs(t, h, "jana"), "petr": loginAs(t, h, "petr"), "eva": loginAs(t, h, "eva")}
	cases := []struct {
		method, path, body  string
		admin, viewer, user int
	}{
		{"GET", "/api/v1/storage/usb", "", 200, 200, 403},
		{"GET", "/api/v1/storage/usb/copy", "", 200, 200, 403},
		{"PUT", "/api/v1/storage/usb/copy", `{"share":"data","folder":"usb-backups"}`, 200, 403, 403},
		{"PUT", "/api/v1/storage/usb/copy", `{"share":"data","x":1}`, 400, 403, 403},
		{"POST", "/api/v1/storage/usb/mount", `{"diskId":"usb-X","partition":"sdb1","readOnly":true}`, 200, 403, 403},
		{"POST", "/api/v1/storage/usb/mount", `{"diskId":"usb-X"}`, 400, 403, 403},
		{"POST", "/api/v1/storage/usb/unmount", `{"diskId":"usb-X","partition":"sdb1"}`, 200, 403, 403},
		{"POST", "/api/v1/storage/usb/eject", `{"diskId":"usb-X"}`, 409, 403, 403},
		{"POST", "/api/v1/storage/usb/eject", `{"diskId":"usb-X","x":1}`, 400, 403, 403},
	}
	for i, c := range cases {
		for who, want := range map[string]int{"jana": c.admin, "petr": c.viewer, "eva": c.user} {
			if rec := do(h, c.method, c.path, c.body, xrw, ck[who]); rec.Code != want {
				t.Errorf("case %d %s %s as %s = %d want %d (%s)", i, c.method, c.path, who, rec.Code, want, rec.Body)
			}
		}
	}
	want := []string{"list", "list", "copy.get", "copy.get", "copy.set data/usb-backups", "mount usb-X sdb1 ro", "unmount usb-X sdb1", "eject usb-X"}
	if len(fu.calls) != len(want) {
		t.Fatalf("calls = %v", fu.calls)
	}
	for i := range want {
		if fu.calls[i] != want[i] {
			t.Errorf("calls = %v", fu.calls)
		}
	}
}
