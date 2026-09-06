package main

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// Synthetic wire prefixes for the three Foundation archive forms. NSObject
// references count the root object and every class, independently of type tags.
func archive(text string, kind string, big bool) []byte {
	header := "\x04\x0bstreamtyped\x81\xe8\x03"
	var order binary.AppendByteOrder = binary.LittleEndian
	if big {
		header = "\x04\x0btypedstream\x81\x03\xe8"
		order = binary.BigEndian
	}
	prefix := "\x84\x01@\x84"
	switch kind {
	case "immutable":
		prefix += "\x84\x84\x12NSAttributedString\x00\x84\x84\x08NSObject\x00\x85\x92\x84\x84\x84\x08NSString\x01\x94"
	case "mutable":
		prefix += "\x84\x84\x19NSMutableAttributedString\x00\x84\x84\x12NSAttributedString\x00\x84\x84\x08NSObject\x00\x85\x92\x84\x84\x84\x0fNSMutableString\x01\x84\x84\x08NSString\x01\x95"
	case "string":
		prefix += "\x84\x84\x08NSString\x01\x84\x84\x08NSObject\x00\x85"
	}
	b := []byte(header + prefix + "\x84\x01+")
	n := len(text)
	if n < 128 {
		b = append(b, byte(n))
	} else if n <= 65535 {
		b = append(b, 0x81)
		b = order.AppendUint16(b, uint16(n))
	} else {
		b = append(b, 0x82)
		b = order.AppendUint32(b, uint32(n))
	}
	return append(append(b, []byte(text)...), 0x86)
}
func TestBody(t *testing.T) {
	for _, kind := range []string{"immutable", "mutable", "string"} {
		for _, big := range []bool{false, true} {
			for _, text := range []string{"", "hello\n👨‍👩‍👧‍👦\x00\ufffc", "NSString NSDictionary + are text", strings.Repeat("界", 100), strings.Repeat("x", 70000)} {
				b := archive(text, kind, big)
				got, err := decodeBody(b)
				if err != nil || got != text {
					t.Fatalf("%s big=%t length=%d: %q %v", kind, big, len(text), got, err)
				}
			}
		}
	}
}
func TestMalformedBody(t *testing.T) {
	b := archive("do not lose emoji 🙂", "mutable", false)
	for i := 0; i < len(b)-1; i++ {
		if _, err := decodeBody(b[:i]); err == nil {
			t.Fatalf("accepted truncated text at %d", i)
		}
	}
	for _, bad := range [][]byte{nil, {}, []byte("NSString fake"), bytes.Replace(b, []byte("NSString\x01"), []byte("NSString\x02"), 1), bytes.Replace(b, []byte{0x95}, []byte{0xff}, 1), archive("\xff", "string", false)} {
		if _, err := decodeBody(bad); err == nil {
			t.Fatal("accepted malformed body")
		}
	}
}
func FuzzBody(f *testing.F) {
	f.Add(archive("Hello 🙂", "mutable", false))
	f.Add(archive("", "string", true))
	f.Add([]byte{1, 2, 3})
	f.Fuzz(func(t *testing.T, b []byte) { decodeBody(b) })
}
