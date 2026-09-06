//go:build !darwin || !cgo

package main

func nativeContacts() func(string) string { return nil }
