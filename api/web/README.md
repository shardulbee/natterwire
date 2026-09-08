# Frontend

Vanilla client embedded in the API. Chats and messages stay in memory, and drafts disappear on reload.

After opening the fixture client in `agent-browser`, run `agent-browser eval "$(cat api/tests/frontend.js)"` from the repository root. It checks cached chat switching, drafts, search, refresh, and image reuse, and reports synchronous switch/layout timings for 100-message chats. Reload afterward. Timings exclude display presentation and network latency.
