When shipping is authorized, push to `origin/main`; [Deploy](../.github/workflows/deploy.yml) tests and deploys the API and TUI on TurboGadget.

- Commit, fetch, and rebase onto `origin/main`. Ask about substantive conflicts. Run the [required checks](../AGENTS.md); ignore only failures reproduced on unchanged `origin/main`.
- Push. If rejected, fetch/rebase and retry; rerun tests if local files changed.
- Check Actions for the pushed revision. Never deploy from an orb or modify the Mac's personal checkout.
- Verify quiet startup, the fox icon, and opening the window from its menu. Report signing, keychain, GUI-session, or privacy blockers; failed installs may leave the app stopped.
- Report revision, tests, Actions run, and remaining steps. Remind the user to relaunch `~/.local/bin/natterwire-tui`. Archive only if asked.
