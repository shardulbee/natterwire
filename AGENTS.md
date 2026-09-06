# Mac verification from orbs

Use a live Mac runner for `swift test`. Create a runner thread and transfer a source archive that includes the orb's uncommitted changes; messages alone do not transfer files. Ask the thread to unpack into a unique temporary directory, run tests there, report the exit status and output, and clean up. Never build in or modify the Mac's shared checkout. Separate source and `.build` directories allow concurrent runs, but do not isolate user permissions or other shared Mac state. Do not install or restart the app as part of testing.

# Shipping the TUI

Shipping TUI changes includes rebuilding the pushed revision on the Mac and atomically replacing `~/Documents/natterwire/tui/bin/natterwire-tui`, not just pushing Git. Use an isolated source directory, preserve local checkout edits, and verify the installed binary's revision with `go version -m`. Existing TUI processes must be relaunched. Check that the running Mac API supports any newly required fields; report a required API deployment separately rather than claiming the feature is live.
