// Package evdev decodes Linux input events as delivered on the NSA310.
//
// The NAS is 32-bit ARM; the kernel's struct input_event there is two 32-bit
// time fields followed by type, code and value: 16 bytes, little endian.
package evdev

import (
	"encoding/binary"
	"fmt"
)

const (
	EventSize = 16

	TypeKey = 0x01

	KeyCopy  = 133 // KEY_COPY, front panel COPY/SYNC button
	KeyPower = 116
	KeyReset = 408 // KEY_RESTART
)

type Event struct {
	Type  uint16
	Code  uint16
	Value int32 // for keys: 1 press, 0 release, 2 autorepeat
}

// Decode parses one event; b must be exactly EventSize bytes.
func Decode(b []byte) (Event, error) {
	if len(b) != EventSize {
		return Event{}, fmt.Errorf("input event is %d bytes, want %d", len(b), EventSize)
	}
	return Event{
		Type:  binary.LittleEndian.Uint16(b[8:10]),
		Code:  binary.LittleEndian.Uint16(b[10:12]),
		Value: int32(binary.LittleEndian.Uint32(b[12:16])),
	}, nil
}

// IsPress reports a key going down (not release, not autorepeat).
func (e Event) IsPress(code uint16) bool {
	return e.Type == TypeKey && e.Code == code && e.Value == 1
}
