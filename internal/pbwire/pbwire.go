// Package pbwire decodes the protocol buffers wire format, enough for the
// messages GitView reads (pprof profiles, OTLP traces) without generated
// code.
package pbwire

import (
	"errors"
	"fmt"
)

// Fields iterates over the fields of a protobuf message.
func Fields(b []byte, f func(num, wireType int, v uint64, data []byte) error) error {
	for len(b) > 0 {
		key, n := Uvarint(b)
		if n <= 0 {
			return errors.New("bad field key")
		}
		b = b[n:]
		num, wt := int(key>>3), int(key&7)
		var v uint64
		var data []byte
		switch wt {
		case 0:
			v, n = Uvarint(b)
			if n <= 0 {
				return errors.New("bad varint")
			}
			b = b[n:]
		case 1:
			if len(b) < 8 {
				return errors.New("truncated fixed64")
			}
			for i := 7; i >= 0; i-- {
				v = v<<8 | uint64(b[i])
			}
			b = b[8:]
		case 2:
			l, n := Uvarint(b)
			if n <= 0 || uint64(len(b)-n) < l {
				return errors.New("truncated field")
			}
			data = b[n : n+int(l)]
			b = b[n+int(l):]
		case 5:
			if len(b) < 4 {
				return errors.New("truncated fixed32")
			}
			v = uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24
			b = b[4:]
		default:
			return fmt.Errorf("unsupported wire type %d", wt)
		}
		if num == 0 {
			return errors.New("field number 0")
		}
		if err := f(num, wt, v, data); err != nil {
			return err
		}
	}
	return nil
}

// Packed iterates over a packed repeated varint field.
func Packed(b []byte, f func(uint64)) error {
	for len(b) > 0 {
		v, n := Uvarint(b)
		if n <= 0 {
			return errors.New("bad packed varint")
		}
		f(v)
		b = b[n:]
	}
	return nil
}

// Uvarint decodes a varint; n <= 0 on error.
func Uvarint(b []byte) (uint64, int) {
	var x uint64
	var s uint
	for i, c := range b {
		if i == 10 {
			return 0, -1
		}
		if c < 0x80 {
			return x | uint64(c)<<s, i + 1
		}
		x |= uint64(c&0x7f) << s
		s += 7
	}
	return 0, 0
}
