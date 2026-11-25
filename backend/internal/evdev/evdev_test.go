package evdev

import (
	"encoding/binary"
	"testing"
)

func raw(typ, code uint16, value int32) []byte {
	b := make([]byte, EventSize)
	binary.LittleEndian.PutUint32(b[0:4], 1764000000) // seconds, ignored
	binary.LittleEndian.PutUint32(b[4:8], 123456)     // microseconds, ignored
	binary.LittleEndian.PutUint16(b[8:10], typ)
	binary.LittleEndian.PutUint16(b[10:12], code)
	binary.LittleEndian.PutUint32(b[12:16], uint32(value))
	return b
}

func TestDecodeCopyPress(t *testing.T) {
	e, err := Decode(raw(TypeKey, KeyCopy, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !e.IsPress(KeyCopy) {
		t.Fatalf("copy press not recognised: %+v", e)
	}
}

func TestIsPressRejectsOthers(t *testing.T) {
	cases := map[string][]byte{
		"release":    raw(TypeKey, KeyCopy, 0),
		"autorepeat": raw(TypeKey, KeyCopy, 2),
		"other key":  raw(TypeKey, KeyReset, 1),
		"sync event": raw(0, 0, 0),
		"negative":   raw(TypeKey, KeyCopy, -1),
	}
	for name, b := range cases {
		e, err := Decode(b)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if e.IsPress(KeyCopy) {
			t.Errorf("%s: unexpectedly treated as copy press", name)
		}
	}
}

func TestDecodeWrongSize(t *testing.T) {
	for _, n := range []int{0, 15, 17, 24} {
		if _, err := Decode(make([]byte, n)); err == nil {
			t.Errorf("size %d accepted", n)
		}
	}
}
