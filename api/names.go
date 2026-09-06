package main

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"unicode"

	"howett.net/plist"
)

type names map[string]string

func phoneKeys(handle string) []string {
	digits := strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, handle)
	if len([]rune(digits)) < 7 {
		return nil
	}
	keys := []string{}
	if strings.HasPrefix(strings.TrimSpace(handle), "+") {
		keys = append(keys, "+"+digits)
	}
	keys = append(keys, digits)
	r := []rune(digits)
	if len(r) > 10 {
		keys = append(keys, string(r[len(r)-10:]))
	}
	return keys
}
func (n names) lookup(handle string) string {
	handle = strings.TrimSpace(handle)
	if strings.Contains(handle, "@") {
		return n[strings.ToLower(handle)]
	}
	for _, k := range phoneKeys(handle) {
		if name := n[k]; name != "" {
			return name
		}
	}
	return ""
}
func loadNames(path string) (names, error) {
	b, err := os.ReadFile(expandHome(path))
	if err != nil {
		return nil, err
	}
	var raw map[string]string
	if err = json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	n := names{}
	handles := make([]string, 0, len(raw))
	for h := range raw {
		handles = append(handles, h)
	}
	sort.Strings(handles)
	for _, h := range handles {
		name := strings.TrimSpace(raw[h])
		if name == "" {
			continue
		}
		keys := phoneKeys(h)
		if strings.Contains(h, "@") {
			keys = []string{strings.ToLower(strings.TrimSpace(h))}
		}
		for _, k := range keys {
			if n[k] == "" {
				n[k] = name
			}
		}
	}
	return n, nil
}
func loadPins(path string) ([]string, error) {
	b, err := os.ReadFile(expandHome(path))
	if err != nil {
		return nil, err
	}
	var root struct {
		Pinning struct {
			Pins []string `plist:"pP"`
		} `plist:"pD"`
	}
	_, err = plist.Unmarshal(b, &root)
	return root.Pinning.Pins, err
}
