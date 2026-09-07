//go:build !darwin

package main

import "errors"

func sendText(string, string) error {
	return errors.New("sending unsupported: requires macOS and Messages.app")
}
