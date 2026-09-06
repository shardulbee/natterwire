# Mac verification from orbs

Use a live Mac runner for `swift test`. Create a runner thread and transfer a source archive that includes the orb's uncommitted changes; messages alone do not transfer files. Ask the thread to unpack into a unique temporary directory, run tests there, report the exit status and output, and clean up. Never build in or modify the Mac's shared checkout. Separate source and `.build` directories allow concurrent runs, but do not isolate user permissions or other shared Mac state. Do not install or restart the app as part of testing.

# Shipping

Follow [.agents/ship.md](.agents/ship.md), including deploying the TUI binary on the Mac.
