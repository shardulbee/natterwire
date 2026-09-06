//go:build !darwin || !cgo

package main

import "context"

func nativeContacts() func(string) string                        { return nil }
func nativeApplication(func(context.Context, func()) error) bool { return false }
