package metasploit

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Minimal MessagePack codec covering the subset msfrpcd uses: nil, bool,
// integers, strings, binary, arrays, and maps. Kept in-house (like the Sliver
// protowire codec) so the adapter stays dependency-light.
//
// msfrpcd is a Ruby service: it encodes most strings as the MessagePack "bin"
// family and map keys as their native type. To keep callers simple we decode
// both str and bin families to Go strings and normalize map keys to strings.

var errShortMsgpack = errors.New("metasploit: truncated msgpack data")

// msgpackEncode serializes a value tree (nil/bool/int/int64/uint64/string/
// []byte/[]any/map[string]any) to MessagePack.
func msgpackEncode(v any) []byte {
	return appendValue(nil, v)
}

func appendValue(b []byte, v any) []byte {
	switch x := v.(type) {
	case nil:
		return append(b, 0xc0)
	case bool:
		if x {
			return append(b, 0xc3)
		}
		return append(b, 0xc2)
	case int:
		return appendInt(b, int64(x))
	case int64:
		return appendInt(b, x)
	case uint64:
		return appendUint(b, x)
	case string:
		return appendStr(b, x)
	case []byte:
		return appendBin(b, x)
	case []any:
		b = appendArrayHeader(b, len(x))
		for _, e := range x {
			b = appendValue(b, e)
		}
		return b
	case map[string]any:
		b = appendMapHeader(b, len(x))
		for k, val := range x {
			b = appendStr(b, k)
			b = appendValue(b, val)
		}
		return b
	default:
		// We control all encoded inputs; fall back to the string form.
		return appendStr(b, fmt.Sprint(x))
	}
}

func appendInt(b []byte, n int64) []byte {
	if n >= 0 {
		return appendUint(b, uint64(n))
	}
	switch {
	case n >= -32:
		return append(b, byte(0xe0|(n+32)))
	case n >= -128:
		return append(b, 0xd0, byte(int8(n)))
	case n >= -32768:
		b = append(b, 0xd1)
		return binary.BigEndian.AppendUint16(b, uint16(int16(n)))
	case n >= -2147483648:
		b = append(b, 0xd2)
		return binary.BigEndian.AppendUint32(b, uint32(int32(n)))
	default:
		b = append(b, 0xd3)
		return binary.BigEndian.AppendUint64(b, uint64(n))
	}
}

func appendUint(b []byte, n uint64) []byte {
	switch {
	case n < 128:
		return append(b, byte(n))
	case n < 1<<8:
		return append(b, 0xcc, byte(n))
	case n < 1<<16:
		b = append(b, 0xcd)
		return binary.BigEndian.AppendUint16(b, uint16(n))
	case n < 1<<32:
		b = append(b, 0xce)
		return binary.BigEndian.AppendUint32(b, uint32(n))
	default:
		b = append(b, 0xcf)
		return binary.BigEndian.AppendUint64(b, n)
	}
}

func appendStr(b []byte, s string) []byte {
	n := len(s)
	switch {
	case n < 32:
		b = append(b, byte(0xa0|n))
	case n < 1<<8:
		b = append(b, 0xd9, byte(n))
	case n < 1<<16:
		b = append(b, 0xda)
		b = binary.BigEndian.AppendUint16(b, uint16(n))
	default:
		b = append(b, 0xdb)
		b = binary.BigEndian.AppendUint32(b, uint32(n))
	}
	return append(b, s...)
}

func appendBin(b []byte, v []byte) []byte {
	n := len(v)
	switch {
	case n < 1<<8:
		b = append(b, 0xc4, byte(n))
	case n < 1<<16:
		b = append(b, 0xc5)
		b = binary.BigEndian.AppendUint16(b, uint16(n))
	default:
		b = append(b, 0xc6)
		b = binary.BigEndian.AppendUint32(b, uint32(n))
	}
	return append(b, v...)
}

func appendArrayHeader(b []byte, n int) []byte {
	switch {
	case n < 16:
		return append(b, byte(0x90|n))
	case n < 1<<16:
		b = append(b, 0xdc)
		return binary.BigEndian.AppendUint16(b, uint16(n))
	default:
		b = append(b, 0xdd)
		return binary.BigEndian.AppendUint32(b, uint32(n))
	}
}

func appendMapHeader(b []byte, n int) []byte {
	switch {
	case n < 16:
		return append(b, byte(0x80|n))
	case n < 1<<16:
		b = append(b, 0xde)
		return binary.BigEndian.AppendUint16(b, uint16(n))
	default:
		b = append(b, 0xdf)
		return binary.BigEndian.AppendUint32(b, uint32(n))
	}
}

// msgpackDecode decodes a single MessagePack value, returning it and the unread
// remainder.
func msgpackDecode(b []byte) (any, []byte, error) {
	if len(b) == 0 {
		return nil, nil, errShortMsgpack
	}
	c := b[0]
	b = b[1:]
	switch {
	case c <= 0x7f:
		return int64(c), b, nil // positive fixint
	case c >= 0xe0:
		return int64(int8(c)), b, nil // negative fixint
	case c >= 0xa0 && c <= 0xbf:
		return decodeRaw(b, int(c&0x1f)) // fixstr
	case c >= 0x90 && c <= 0x9f:
		return decodeArray(b, int(c&0x0f)) // fixarray
	case c >= 0x80 && c <= 0x8f:
		return decodeMap(b, int(c&0x0f)) // fixmap
	}
	switch c {
	case 0xc0:
		return nil, b, nil
	case 0xc2:
		return false, b, nil
	case 0xc3:
		return true, b, nil
	case 0xcc:
		return uintN(b, 1)
	case 0xcd:
		return uintN(b, 2)
	case 0xce:
		return uintN(b, 4)
	case 0xcf:
		return uintN(b, 8)
	case 0xd0:
		v, rest, err := uintN(b, 1)
		return castInt(v, int8Cast), rest, err
	case 0xd1:
		v, rest, err := uintN(b, 2)
		return castInt(v, int16Cast), rest, err
	case 0xd2:
		v, rest, err := uintN(b, 4)
		return castInt(v, int32Cast), rest, err
	case 0xd3:
		v, rest, err := uintN(b, 8)
		return castInt(v, int64Cast), rest, err
	case 0xd9, 0xc4:
		return lenPrefixedRaw(b, 1)
	case 0xda, 0xc5:
		return lenPrefixedRaw(b, 2)
	case 0xdb, 0xc6:
		return lenPrefixedRaw(b, 4)
	case 0xdc:
		n, rest, err := lenPrefix(b, 2)
		if err != nil {
			return nil, nil, err
		}
		return decodeArray(rest, n)
	case 0xdd:
		n, rest, err := lenPrefix(b, 4)
		if err != nil {
			return nil, nil, err
		}
		return decodeArray(rest, n)
	case 0xde:
		n, rest, err := lenPrefix(b, 2)
		if err != nil {
			return nil, nil, err
		}
		return decodeMap(rest, n)
	case 0xdf:
		n, rest, err := lenPrefix(b, 4)
		if err != nil {
			return nil, nil, err
		}
		return decodeMap(rest, n)
	default:
		return nil, nil, fmt.Errorf("metasploit: unsupported msgpack byte 0x%02x", c)
	}
}

type intCast int

const (
	int8Cast intCast = iota
	int16Cast
	int32Cast
	int64Cast
)

func castInt(v any, c intCast) any {
	u, ok := v.(int64)
	if !ok {
		return v
	}
	switch c {
	case int8Cast:
		return int64(int8(u))
	case int16Cast:
		return int64(int16(u))
	case int32Cast:
		return int64(int32(u))
	default:
		return u
	}
}

func uintN(b []byte, n int) (any, []byte, error) {
	if len(b) < n {
		return nil, nil, errShortMsgpack
	}
	var u uint64
	for i := 0; i < n; i++ {
		u = u<<8 | uint64(b[i])
	}
	return int64(u), b[n:], nil
}

func lenPrefix(b []byte, n int) (int, []byte, error) {
	if len(b) < n {
		return 0, nil, errShortMsgpack
	}
	var l int
	for i := 0; i < n; i++ {
		l = l<<8 | int(b[i])
	}
	return l, b[n:], nil
}

func lenPrefixedRaw(b []byte, prefix int) (any, []byte, error) {
	l, rest, err := lenPrefix(b, prefix)
	if err != nil {
		return nil, nil, err
	}
	return decodeRaw(rest, l)
}

// decodeRaw reads n bytes as a string (str and bin both map to string).
func decodeRaw(b []byte, n int) (any, []byte, error) {
	if len(b) < n {
		return nil, nil, errShortMsgpack
	}
	return string(b[:n]), b[n:], nil
}

func decodeArray(b []byte, n int) (any, []byte, error) {
	arr := make([]any, 0, n)
	for i := 0; i < n; i++ {
		var v any
		var err error
		v, b, err = msgpackDecode(b)
		if err != nil {
			return nil, nil, err
		}
		arr = append(arr, v)
	}
	return arr, b, nil
}

func decodeMap(b []byte, n int) (any, []byte, error) {
	m := make(map[string]any, n)
	for i := 0; i < n; i++ {
		var k, v any
		var err error
		k, b, err = msgpackDecode(b)
		if err != nil {
			return nil, nil, err
		}
		v, b, err = msgpackDecode(b)
		if err != nil {
			return nil, nil, err
		}
		m[asString(k)] = v
	}
	return m, b, nil
}

// asString coerces a decoded msgpack value to a string.
func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}
