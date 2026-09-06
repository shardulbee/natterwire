Ship the changes to `origin/main` and deploy the TUI on the Mac.

- Commit uncommitted changes, fetch origin, and rebase onto `origin/main`. Ask before resolving substantive conflicts. Run the full relevant test suite before pushing, using the isolated Mac workflow in `AGENTS.md` for Swift tests. Only ignore failures reproduced on unchanged `origin/main`.
- Push to `origin/main`. If rejected, fetch and rebase again; repeat tests only if conflict resolution or other local edits changed files after the successful run.
- For TUI changes, use the Mac runner to build the pushed revision in an isolated directory and atomically replace `~/Documents/natterwire/tui/bin/natterwire-tui`. Preserve the Mac's local checkout and running sessions. Verify the installed revision with `go version -m` and check `--help`. Tell the user to relaunch existing TUI processes.
- Check that the running API supports newly required fields. If it needs a separate deployment, report that explicitly; do not claim the feature is live just because the TUI was rebuilt.
- Clean temporary build directories and report the pushed revision, test results, deployed binary, and any remaining deployment step. Do not archive threads unless the current request asks for it.
