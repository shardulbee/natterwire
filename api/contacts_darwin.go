//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework Contacts
#include <stdlib.h>
int nw_contacts_start(void);
char *nw_contacts_snapshot(void);
*/
import "C"

import (
	"log"
	"sync"
	"unsafe"
)

// Permission is requested asynchronously. Until granted, lookups return raw
// handles. Require a bundle usage description, never prompt from a bare CLI/test.
func nativeContacts() func(string) string {
	if C.nw_contacts_start() == 0 {
		log.Print("native Contacts unavailable outside Natterwire.app; use --contacts for a JSON override")
		return nil
	}
	var mu sync.Mutex
	var snapshot names
	return func(handle string) string {
		mu.Lock()
		defer mu.Unlock()
		if data := C.nw_contacts_snapshot(); data != nil {
			b := C.GoString(data)
			C.free(unsafe.Pointer(data))
			if n, err := parseNames([]byte(b)); err == nil {
				snapshot = n
			}
		}
		return snapshot.lookup(handle)
	}
}
