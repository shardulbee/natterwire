When shipping is authorized, push to `origin/main`; [Deploy](../.github/workflows/deploy.yml) tests and deploys the API and TUI on TurboGadget.

- Commit uncommitted changes, fetch origin, and rebase onto `origin/main`. Ask before resolving substantive conflicts. Run the full relevant Go test suites before pushing. Only ignore failures reproduced on unchanged `origin/main`.
- Push to `origin/main`. If rejected, fetch and rebase again; repeat tests only if conflict resolution or other local edits changed files after the successful run.
- Check the Actions result for the pushed revision. Do not deploy from an orb or modify the Mac's personal checkout.
- Report signing, keychain, GUI-session, or privacy blockers; failed installation can leave the app stopped. After deployment, verify the visible app window and fox icon on the Mac. Tell the user to relaunch existing TUI processes using `~/.local/bin/natterwire-tui`.
- Report the revision, test results, Actions run, and any remaining step. Do not archive threads unless asked.
