//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework AppKit
#include <stdlib.h>
int nw_app_prepare(void);
void nw_app_run(void);
void nw_app_update(const char *title, const char *detail, int messagesAvailable);
void nw_app_stop(void);
*/
import "C"

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// AppKit must run on the process's original main thread.
func init() { runtime.LockOSThread() }

func nativeApplication(run func(context.Context, func()) error) bool {
	if C.nw_app_prepare() == 0 {
		return false
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	update := func(title, detail string, messagesAvailable C.int) {
		t, d := C.CString(title), C.CString(detail)
		C.nw_app_update(t, d, messagesAvailable)
		C.free(unsafe.Pointer(t))
		C.free(unsafe.Pointer(d))
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			err := run(ctx, func() {
				update("API running", "You can close this window. Natterwire keeps running until you choose Quit Natterwire.", 1)
			})
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, errMessagesAccess) {
				update("Allow Messages access", "Open Full Disk Access and enable Natterwire. This app checks again automatically; leave it open.", 0)
			} else {
				update("API unavailable", "Another copy may be using port 8741. Quit it and leave this window open; Natterwire will retry automatically.", 1)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
	go func() { <-ctx.Done(); C.nw_app_stop() }()
	C.nw_app_run()
	cancel()
	<-done
	return true
}
