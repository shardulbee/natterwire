//go:build darwin

package main

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

func sendText(guid, text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	// Arguments are data, never AppleScript source or shell input. Match only an
	// existing exact chat ID, never a buddy, display name, or participant list.
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", "-", guid, text)
	cmd.Stdin = strings.NewReader(`on run argv
tell application "Messages"
set targetID to item 1 of argv
set targets to every chat whose id is targetID
if (count of targets) is not 1 then error "Exact chat unavailable"
set targetChat to item 1 of targets
set actualID to id of targetChat
considering case
if actualID is not targetID then error "Exact chat unavailable"
end considering
send (item 2 of argv) to targetChat
end tell
end run`)
	if cmd.Run() != nil {
		return errors.New("send outcome unknown: check Messages and Natterwire's Automation permission; do not resend as a new request")
	}
	return nil
}
