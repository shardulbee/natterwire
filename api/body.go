package main

import (
	"encoding/binary"
	"errors"
	"unicode/utf8"
)

// Decode the first backing NSString, not formatting attributes. Typedstream v4
// stores NSAttributedString's string before its attribute graph. Follow type and
// class references rather than searching for NSString or stripping control bytes.
// Wire format: github.com/dgelessus/python-typedstream, stream.py and
// types/foundation.py (NSString v1); see also that repository's issue #2.
func decodeBody(data []byte) (string, error) {
	p := bodyReader{data: data, order: binary.LittleEndian}
	if p.byte() != 4 || p.byte() != 11 {
		return "", errBody
	}
	sig := string(p.take(11))
	if sig == "typedstream" {
		p.order = binary.BigEndian
	} else if sig != "streamtyped" {
		return "", errBody
	}
	p.integer(false)
	if p.shared() != "@" {
		return "", errBody
	}
	classes := p.object()
	if version, ok := classes["NSAttributedString"]; ok && version == 0 {
		if p.shared() != "@" {
			return "", errBody
		}
		classes = p.object()
	}
	if classes["NSString"] != 1 || p.shared() != "+" {
		return "", errBody
	}
	text := p.unshared()
	if p.err != nil || !utf8.Valid(text) {
		return "", errBody
	}
	return string(text), nil
}

var errBody = errors.New("unsupported or malformed attributedBody")

type bodyReader struct {
	data    []byte
	order   binary.ByteOrder
	err     error
	strings []string
	objects []map[string]int64 // nil entries reserve object IDs; others are class chains.
}

func (p *bodyReader) take(n int) []byte {
	if p.err != nil || n < 0 || n > len(p.data) {
		p.err = errBody
		return nil
	}
	b := p.data[:n]
	p.data = p.data[n:]
	return b
}
func (p *bodyReader) byte() byte {
	b := p.take(1)
	if len(b) == 0 {
		return 0
	}
	return b[0]
}
func (p *bodyReader) integer(signed bool) int64 { return p.number(p.byte(), signed) }
func (p *bodyReader) number(head byte, signed bool) int64 {
	switch head {
	case 0x81:
		b := p.take(2)
		if b == nil {
			return 0
		}
		n := p.order.Uint16(b)
		if signed {
			return int64(int16(n))
		}
		return int64(n)
	case 0x82:
		b := p.take(4)
		if b == nil {
			return 0
		}
		n := p.order.Uint32(b)
		if signed {
			return int64(int32(n))
		}
		return int64(n)
	default:
		if head >= 0x80 && head <= 0x91 {
			p.err = errBody
			return 0
		}
		if signed {
			return int64(int8(head))
		}
		return int64(head)
	}
}
func (p *bodyReader) unshared() []byte {
	n := p.integer(false)
	if n > int64(len(p.data)) {
		p.err = errBody
		return nil
	}
	return p.take(int(n))
}
func (p *bodyReader) shared() string {
	h := p.byte()
	if h == 0x84 {
		s := string(p.unshared())
		p.strings = append(p.strings, s)
		return s
	}
	i := p.number(h, true) + 110
	if i < 0 || i >= int64(len(p.strings)) {
		p.err = errBody
		return ""
	}
	return p.strings[i]
}
func (p *bodyReader) object() map[string]int64 {
	if p.byte() != 0x84 {
		p.err = errBody
		return nil
	}
	p.objects = append(p.objects, nil)
	return p.class(0)
}
func (p *bodyReader) class(depth int) map[string]int64 {
	if depth > 32 || p.err != nil {
		p.err = errBody
		return nil
	}
	h := p.byte()
	if h == 0x85 {
		return map[string]int64{}
	}
	if h != 0x84 {
		i := p.number(h, true) + 110
		if i < 0 || i >= int64(len(p.objects)) || p.objects[i] == nil {
			p.err = errBody
			return nil
		}
		return p.objects[i]
	}
	name := p.shared()
	version := p.integer(true)
	index := len(p.objects)
	p.objects = append(p.objects, nil)
	chain := map[string]int64{name: version}
	for k, v := range p.class(depth + 1) {
		chain[k] = v
	}
	p.objects[index] = chain
	return chain
}
